/*
===========================================================================

eventbot_test.go - the host on a frozen clock

===========================================================================
*/
package eventbot

import (
	"errors"
	"strings"
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
)

var errStoreDown = errors.New("store down")

const fixtureSchedule = `# an evening raffle
19:30 raffle rounds=2 round=1m reward=ITEM_ETC_SILK_SCROLLx10 title=Evening Raffle
`

// day is the fixture's day: 2026-11-23, server-local.
var day = time.Date(2026, 11, 23, 0, 0, 0, 0, time.Local)

type fakeDoors struct {
	whispers   []string
	announces  []string
	grants     []grantCall
	wins       map[string]int
	pick       int
	grantFails bool
}

type grantCall struct {
	division string
	name     string
	codename string
	count    int
}

func (f *fakeDoors) deps() Dependencies {
	return Dependencies{
		Whisper: func(division, name, text string) {
			f.whispers = append(f.whispers, name+": "+text)
		},
		Announce: func(text string) { f.announces = append(f.announces, text) },
		Grant: func(division string, character *enterworld.Character, codename string, count int) bool {
			f.grants = append(f.grants, grantCall{division: division, name: character.Name, codename: codename, count: count})
			if f.grantFails {
				return false
			}
			f.wins[character.AccountID]++
			return true
		},
		WinsToday: func(accountID string, _ time.Time) int { return f.wins[accountID] },
		Pick:      func(n int) int { return f.pick % n },
	}
}

func parseFixture(t *testing.T) Schedule {
	t.Helper()
	schedule, err := ParseSchedule(fixtureSchedule, func(string) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	return schedule
}

func entrant(name, account string) WhisperSender {
	return WhisperSender{Division: "d1", Character: &enterworld.Character{Name: name, AccountID: account}}
}

/*
================
TestParseScheduleFixture

The fixture parses to one daily raffle; a bad row refuses the file.
================
*/
func TestParseScheduleFixture(t *testing.T) {
	schedule := parseFixture(t)
	if len(schedule.Events) != 1 {
		t.Fatalf("events = %d, want 1", len(schedule.Events))
	}
	event := schedule.Events[0]
	if event.At != "19:30" || event.Kind != "raffle" || event.Rounds != 2 ||
		event.RoundLength != time.Minute || event.RewardCodename != "ITEM_ETC_SILK_SCROLL" || event.RewardCount != 10 ||
		event.DisplayTitle() != "Evening Raffle" {
		t.Fatalf("event = %+v", event)
	}
	for _, bad := range []string{
		"19:30 nosuchkind rounds=1 round=1m reward=ITEM_Xx1",
		"19:30 raffle rounds=0 round=1m reward=ITEM_Xx1",
		"19:30 raffle rounds=1 round=1s reward=ITEM_Xx1",
		"19:30 raffle rounds=1 round=1m reward=ITEM_Xx0",
		"19:30 raffle rounds=1 round=1m",
		"25:30 raffle rounds=1 round=1m reward=ITEM_Xx1",
		"19:30 raffle rounds=1 round=1m reward=ITEM_Xx1 bogus=2",
		"19:30 raffle rounds=1 rounds=2 round=1m reward=ITEM_Xx1",
	} {
		if _, err := ParseSchedule(bad, nil); err == nil {
			t.Errorf("bad row parsed: %q", bad)
		}
	}
	if _, err := ParseSchedule("19:30 raffle rounds=1 round=1m reward=ITEM_NOPE x1", nil); err == nil {
		t.Error("reward without a count parsed")
	}
	if _, err := ParseSchedule(fixtureSchedule, func(string) bool { return false }); err == nil {
		t.Error("unknown reward codename parsed")
	}
}

/*
================
TestRoundRunsOnTheClock

Tick opens the occurrence, whispers enter, the repeat is refused,
and the round's end draws and pays exactly the file's reward.
================
*/
func TestRoundRunsOnTheClock(t *testing.T) {
	doors := &fakeDoors{wins: map[string]int{}}
	host := New(true, parseFixture(t), doors.deps())
	start := day.Add(19*time.Hour + 30*time.Minute)

	host.Tick(start.Add(-time.Minute))
	if len(doors.announces) != 0 {
		t.Fatalf("announced before the start: %v", doors.announces)
	}
	host.Tick(start)
	if len(doors.announces) != 1 || !strings.Contains(doors.announces[0], "round 1 of 2") {
		t.Fatalf("announces = %v", doors.announces)
	}
	if reply := host.HandleWhisper(entrant("Ana", "a1"), "join"); len(reply) != 1 || !strings.Contains(reply[0], "You are in") {
		t.Fatalf("entry reply = %v", reply)
	}
	if reply := host.HandleWhisper(entrant("Bo", "a2"), "join"); len(reply) != 1 {
		t.Fatalf("entry reply = %v", reply)
	}
	repeat := host.HandleWhisper(entrant("Ana", "a1"), "join again")
	if len(repeat) != 1 || !strings.Contains(repeat[0], "already in") {
		t.Fatalf("repeat reply = %v", repeat)
	}
	doors.pick = 1 // Bo wins the draw.
	host.Tick(start.Add(30 * time.Second))
	if len(doors.grants) != 0 {
		t.Fatal("paid before the round closed")
	}
	host.Tick(start.Add(time.Minute))
	if len(doors.grants) != 1 {
		t.Fatalf("grants = %v", doors.grants)
	}
	grant := doors.grants[0]
	if grant.name != "Bo" || grant.codename != "ITEM_ETC_SILK_SCROLL" || grant.count != 10 || grant.division != "d1" {
		t.Fatalf("grant = %+v, want Bo paid exactly the file's 10x ITEM_ETC_SILK_SCROLL in d1", grant)
	}
	if len(doors.whispers) != 1 || !strings.HasPrefix(doors.whispers[0], "Bo: You won") {
		t.Fatalf("whispers = %v", doors.whispers)
	}
	// Round 2 opens on the next tick and can be won too; then the
	// occurrence is over and never restarts the same day.
	host.HandleWhisper(entrant("Cy", "a3"), "join")
	host.Tick(start.Add(2 * time.Minute))
	host.Tick(start.Add(3 * time.Minute))
	announces := len(doors.announces)
	host.Tick(start.Add(time.Hour))
	if len(doors.announces) != announces {
		t.Fatalf("occurrence restarted: %v", doors.announces[announces:])
	}
}

/*
================
TestDailyWinCapLeavesTheAccountOut

An account at the cap is skipped by the draw; its other character
winning for it changes nothing (the cap is per account).
================
*/
func TestDailyWinCapLeavesTheAccountOut(t *testing.T) {
	doors := &fakeDoors{wins: map[string]int{"a1": MaxWinsPerAccountPerDay}}
	host := New(true, parseFixture(t), doors.deps())
	start := day.Add(19*time.Hour + 30*time.Minute)
	host.Tick(start)
	host.HandleWhisper(entrant("Ana", "a1"), "join")
	host.HandleWhisper(entrant("AnaTwo", "a1"), "join")
	host.HandleWhisper(entrant("Bo", "a2"), "join")
	doors.pick = 0 // the capped account is drawn first - and skipped.
	host.Tick(start.Add(time.Minute))
	if len(doors.grants) != 1 || doors.grants[0].name != "Bo" {
		t.Fatalf("grants = %v, want Bo (a1 is at the cap)", doors.grants)
	}
}

/*
================
TestUnpaidWinIsNotCounted

A prize the bag cannot take (the grant door refuses) is announced as
not counted and never reaches the win count the cap reads.
================
*/
func TestUnpaidWinIsNotCounted(t *testing.T) {
	doors := &fakeDoors{wins: map[string]int{}, grantFails: true}
	host := New(true, parseFixture(t), doors.deps())
	start := day.Add(19*time.Hour + 30*time.Minute)
	host.Tick(start)
	host.HandleWhisper(entrant("Ana", "a1"), "join")
	host.Tick(start.Add(time.Minute))
	if len(doors.grants) != 1 {
		t.Fatalf("grants = %v, want the one attempted payment", doors.grants)
	}
	if doors.wins["a1"] != 0 {
		t.Fatalf("wins = %v, an unpaid win counted", doors.wins)
	}
	joined := strings.Join(doors.announces, " ")
	if !strings.Contains(joined, "not counted") {
		t.Fatalf("announces = %v, want the unpaid win announced as not counted", doors.announces)
	}
}

/*
================
TestWinsAfterReadFailsClosed

A failed cap read counts as the cap, never as zero wins.
================
*/
func TestWinsAfterReadFailsClosed(t *testing.T) {
	if got := WinsAfterRead(0, errStoreDown); got != MaxWinsPerAccountPerDay {
		t.Fatalf("errored read = %d, want the cap %d", got, MaxWinsPerAccountPerDay)
	}
	if got := WinsAfterRead(2, nil); got != 2 {
		t.Fatalf("clean read = %d, want 2", got)
	}
}

/*
================
TestOffSendsNothing

A disabled host never announces, answers or pays.
================
*/
func TestOffSendsNothing(t *testing.T) {
	doors := &fakeDoors{wins: map[string]int{}}
	host := New(false, parseFixture(t), doors.deps())
	start := day.Add(19*time.Hour + 30*time.Minute)
	host.Tick(start)
	if reply := host.HandleWhisper(entrant("Ana", "a1"), "join"); reply != nil {
		t.Fatalf("off host replied %v", reply)
	}
	host.Tick(start.Add(5 * time.Minute))
	if len(doors.announces) != 0 || len(doors.whispers) != 0 || len(doors.grants) != 0 {
		t.Fatalf("off host acted: %v %v %v", doors.announces, doors.whispers, doors.grants)
	}
	if events := host.ScheduledEvents(start); events != nil {
		t.Fatalf("off host listed %v", events)
	}
}

/*
================
TestIdleWhisperNamesTheNextEvent
================
*/
func TestIdleWhisperNamesTheNextEvent(t *testing.T) {
	doors := &fakeDoors{wins: map[string]int{}}
	deps := doors.deps()
	deps.Now = func() time.Time { return day.Add(12 * time.Hour) }
	host := New(true, parseFixture(t), deps)
	reply := host.HandleWhisper(entrant("Ana", "a1"), "hello")
	if len(reply) != 1 || !strings.Contains(reply[0], "Evening Raffle") || !strings.Contains(reply[0], "19:30") {
		t.Fatalf("idle reply = %v", reply)
	}
}

/*
================
TestScheduledEventsFeedRows

The live-or-next occurrence per event.
================
*/
func TestScheduledEventsFeedRows(t *testing.T) {
	host := New(true, parseFixture(t), Dependencies{})
	before := host.ScheduledEvents(day.Add(12 * time.Hour))
	if len(before) != 1 || before[0].Running || !before[0].Start.Equal(day.Add(19*time.Hour+30*time.Minute)) {
		t.Fatalf("before = %+v", before)
	}
	during := host.ScheduledEvents(day.Add(19*time.Hour + 31*time.Minute))
	if len(during) != 1 || !during[0].Running {
		t.Fatalf("during = %+v", during)
	}
	after := host.ScheduledEvents(day.Add(21 * time.Hour))
	if len(after) != 1 || after[0].Running || !after[0].Start.Equal(day.AddDate(0, 0, 1).Add(19*time.Hour+30*time.Minute)) {
		t.Fatalf("after = %+v", after)
	}
}
