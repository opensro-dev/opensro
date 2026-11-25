/*
===========================================================================

kind_randomunique_test.go - Random Unique on a frozen clock (#3537)

===========================================================================
*/
package eventbot

import (
	"strings"
	"testing"
	"time"
)

const uniqueFixture = `# a nightly unique
20:00 randomunique rounds=1 round=30m reward=ITEM_ETC_SILK_SCROLLx10 unique=MOB_RM_TAHOMET unique=MOB_CH_NIGHTMARE spot=Hotan|2500|100|200 spot=Jangan|2499|300|400 title=Random Unique
`

// uniqueDoors records the host's world play for a kill round: the
// spawn door's event (its Hider field carries the picked unique) and
// spot, and how many spawns left the world.
type uniqueDoors struct {
	fakeDoors
	spawns   []Event
	spots    []Spot
	despawns int
}

func (f *uniqueDoors) deps() Dependencies {
	deps := f.fakeDoors.deps()
	deps.SpawnHider = func(event Event, spot Spot, at time.Time) (Hider, bool) {
		f.spawns = append(f.spawns, event)
		f.spots = append(f.spots, spot)
		return Hider{
			ObjectID: 888,
			RegionID: spot.RegionID,
			X:        spot.X,
			Z:        spot.Z,
			Despawn:  func() { f.despawns++ },
		}, true
	}
	return deps
}

func parseUnique(t *testing.T) Schedule {
	t.Helper()
	schedule, err := ParseSchedule(uniqueFixture, func(string) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	return schedule
}

func uniqueStart() time.Time { return day.Add(20 * time.Hour) }

/*
================
TestParseUniqueFixture

The unique row carries its monster codenames and spots; the
reference 30-minute window parses and a longer one refuses, and a
row without a unique or a spot refuses the file.
================
*/
func TestParseUniqueFixture(t *testing.T) {
	event := parseUnique(t).Events[0]
	if len(event.Uniques) != 2 || len(event.Spots) != 2 || event.RoundLength != 30*time.Minute {
		t.Fatalf("event = %+v, want two uniques, two spots, a 30m window", event)
	}
	for _, bad := range []string{
		"20:00 randomunique rounds=1 round=30m reward=ITEM_ETC_SILK_SCROLLx10 spot=Hotan|2500|1|2 title=X",
		"20:00 randomunique rounds=1 round=30m reward=ITEM_ETC_SILK_SCROLLx10 unique=MOB_X title=X",
		"20:00 randomunique rounds=1 round=31m reward=ITEM_ETC_SILK_SCROLLx10 unique=MOB_X spot=Hotan|2500|1|2 title=X",
		"20:00 randomunique rounds=1 round=30m reward=ITEM_ETC_SILK_SCROLLx10 unique=MOB_X spot=Hotan|0|1|2 title=X",
	} {
		if _, err := ParseSchedule(bad, func(string) bool { return true }); err == nil {
			t.Fatalf("bad row parsed: %q", bad)
		}
	}
}

/*
================
TestUniqueKillerWinsAndPays

The round spawns the picked unique at the picked spot; an entered
killer takes the prize through the grant door, the spawn leaves the
world, and reaching the unique without slaying it wins nothing.
================
*/
func TestUniqueKillerWinsAndPays(t *testing.T) {
	doors := &uniqueDoors{fakeDoors: fakeDoors{wins: map[string]int{}, pick: 1}}
	host := New(true, parseUnique(t), doors.deps())
	start := uniqueStart()

	host.Tick(start)
	if len(doors.spawns) != 1 || doors.spawns[0].Hider != "MOB_CH_NIGHTMARE" {
		t.Fatalf("spawns = %+v, want the picked unique", doors.spawns)
	}
	if len(doors.spots) != 1 || doors.spots[0].Name != "Jangan" {
		t.Fatalf("spots = %+v, want the picked spot", doors.spots)
	}
	if reply := host.HandleWhisper(entrant("Ana", "a1"), "join"); len(reply) != 1 || !strings.Contains(reply[0], "Slay the unique") {
		t.Fatalf("entry reply = %v", reply)
	}
	host.HandleWhisper(entrant("Bo", "a2"), "join")
	if host.ReportFound(entrant("Bo", "a2").Character) {
		t.Fatal("reaching the unique won a kill round")
	}
	if !host.ReportSlain("Bo") {
		t.Fatal("the kill was refused")
	}
	if len(doors.grants) != 1 || doors.grants[0].name != "Bo" || doors.grants[0].count != 10 {
		t.Fatalf("grants = %v, want Bo paid the file's 10", doors.grants)
	}
	if doors.despawns != 1 {
		t.Fatalf("despawns = %d, want the spawn gone", doors.despawns)
	}
	joined := strings.Join(doors.announces, " ")
	if !strings.Contains(joined, "slew the unique") {
		t.Fatalf("announces = %v", doors.announces)
	}
}

/*
================
TestUniqueOutsiderKillEndsWithNoWinner

A killer who never entered ends the round unpaid: the unique is
gone either way, so the hunt cannot run on.
================
*/
func TestUniqueOutsiderKillEndsWithNoWinner(t *testing.T) {
	doors := &uniqueDoors{fakeDoors: fakeDoors{wins: map[string]int{}}}
	host := New(true, parseUnique(t), doors.deps())
	start := uniqueStart()

	host.Tick(start)
	host.HandleWhisper(entrant("Ana", "a1"), "join")
	if !host.ReportSlain("Ghost") {
		t.Fatal("an outsider's kill did not end the round")
	}
	if len(doors.grants) != 0 {
		t.Fatalf("grants = %v, an outsider was paid", doors.grants)
	}
	if _, live := host.LiveHider(); live {
		t.Fatal("the round runs on after its unique fell")
	}
	joined := strings.Join(doors.announces, " ")
	if !strings.Contains(joined, "no winner") {
		t.Fatalf("announces = %v", doors.announces)
	}
}

/*
================
TestUniqueCappedKillerEndsWithNoWinner

A capped account's kill cannot count and the round still ends:
the unique is gone, so nobody else can take it either.
================
*/
func TestUniqueCappedKillerEndsWithNoWinner(t *testing.T) {
	doors := &uniqueDoors{fakeDoors: fakeDoors{wins: map[string]int{"a1": MaxWinsPerAccountPerDay}}}
	host := New(true, parseUnique(t), doors.deps())
	start := uniqueStart()

	host.Tick(start)
	host.HandleWhisper(entrant("Ana", "a1"), "join")
	if !host.ReportSlain("Ana") {
		t.Fatal("a capped kill did not end the round")
	}
	if len(doors.grants) != 0 {
		t.Fatalf("grants = %v, a capped account was paid", doors.grants)
	}
	if _, live := host.LiveHider(); live {
		t.Fatal("the round runs on after its unique fell")
	}
}

/*
================
TestUniqueSurvivesTheWindow

The timer runs out with the unique standing: no winner, no grant,
and the spawn leaves the world at round end.
================
*/
func TestUniqueSurvivesTheWindow(t *testing.T) {
	doors := &uniqueDoors{fakeDoors: fakeDoors{wins: map[string]int{}}}
	host := New(true, parseUnique(t), doors.deps())
	start := uniqueStart()

	host.Tick(start)
	host.HandleWhisper(entrant("Ana", "a1"), "join")
	host.Tick(start.Add(30 * time.Minute))
	if len(doors.grants) != 0 {
		t.Fatalf("grants = %v, a survived window paid out", doors.grants)
	}
	if doors.despawns != 1 {
		t.Fatalf("despawns = %d, want the unique gone at round end", doors.despawns)
	}
	joined := strings.Join(doors.announces, " ")
	if !strings.Contains(joined, "ends with no winner") {
		t.Fatalf("announces = %v", doors.announces)
	}
}
