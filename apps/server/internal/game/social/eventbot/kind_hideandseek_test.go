/*
===========================================================================

kind_hideandseek_test.go - Hide & Seek on a frozen clock

===========================================================================
*/
package eventbot

import (
	"strings"
	"testing"
	"time"
)

const seekFixture = `# an evening hunt
19:30 hideandseek rounds=5 round=10m reward=ITEM_ETC_SILK_SCROLLx10 hider=MOB_EVENT_HIDER spot=West_Gate|2499|1234|9876 spot=East_Market|2500|555|666 title=Hide & Seek
`

// seekDoors records the host's world play: which spots were hidden
// at, how many hiders left the world, and the usual whisper/grant
// traffic (fakeDoors).
type seekDoors struct {
	fakeDoors
	spots    []Spot
	despawns int
}

func (f *seekDoors) deps() Dependencies {
	deps := f.fakeDoors.deps()
	deps.SpawnHider = func(event Event, spot Spot, at time.Time) (Hider, bool) {
		f.spots = append(f.spots, spot)
		return Hider{
			ObjectID: 777,
			RegionID: spot.RegionID,
			X:        spot.X,
			Z:        spot.Z,
			Despawn:  func() { f.despawns++ },
		}, true
	}
	return deps
}

func parseSeek(t *testing.T) Schedule {
	t.Helper()
	schedule, err := ParseSchedule(seekFixture, func(string) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	return schedule
}

func seekStart() time.Time { return day.Add(19*time.Hour + 30*time.Minute) }

/*
================
TestParseSeekFixture

The seek row carries its hider and spots; a row without them, or a
broken spot, refuses the file.
================
*/
func TestParseSeekFixture(t *testing.T) {
	event := parseSeek(t).Events[0]
	if event.Hider != "MOB_EVENT_HIDER" || len(event.Spots) != 2 {
		t.Fatalf("event = %+v, want the hider and two spots", event)
	}
	spot := event.Spots[0]
	if spot.Name != "West_Gate" || spot.RegionID != 2499 || spot.X != 1234 || spot.Z != 9876 {
		t.Fatalf("spot = %+v", spot)
	}
	if spot.DisplayName() != "West Gate" {
		t.Fatalf("display name = %q", spot.DisplayName())
	}
	for _, bad := range []string{
		"19:30 hideandseek rounds=5 round=10m reward=ITEM_ETC_SILK_SCROLLx10 spot=West_Gate|2499|1|2 title=X",
		"19:30 hideandseek rounds=5 round=10m reward=ITEM_ETC_SILK_SCROLLx10 hider=MOB_X title=X",
		"19:30 hideandseek rounds=5 round=10m reward=ITEM_ETC_SILK_SCROLLx10 hider=MOB_X spot=West|Gate|1|2 title=X",
		"19:30 hideandseek rounds=5 round=10m reward=ITEM_ETC_SILK_SCROLLx10 hider=MOB_X spot=West|0|1|2 title=X",
	} {
		if _, err := ParseSchedule(bad, func(string) bool { return true }); err == nil {
			t.Fatalf("bad row parsed: %q", bad)
		}
	}
}

/*
================
TestSeekFindWinsAndPays

The first entered finder ends the round: paid exactly the file's
reward, the hider leaves the world, and the next round hides at
the next spot.
================
*/
func TestSeekFindWinsAndPays(t *testing.T) {
	doors := &seekDoors{fakeDoors: fakeDoors{wins: map[string]int{}}}
	host := New(true, parseSeek(t), doors.deps())
	start := seekStart()

	host.Tick(start)
	if len(doors.spots) != 1 || doors.spots[0].Name != "West_Gate" {
		t.Fatalf("spots = %v, want the hider at West Gate", doors.spots)
	}
	if reply := host.HandleWhisper(entrant("Ana", "a1"), "join"); len(reply) != 1 || !strings.Contains(reply[0], "first to reach it") {
		t.Fatalf("entry reply = %v", reply)
	}
	host.HandleWhisper(entrant("Bo", "a2"), "join")
	if !host.ReportFound(entrant("Bo", "a2").Character) {
		t.Fatal("the find was refused")
	}
	if len(doors.grants) != 1 || doors.grants[0].name != "Bo" || doors.grants[0].count != 10 {
		t.Fatalf("grants = %v, want Bo paid the file's 10", doors.grants)
	}
	if doors.despawns != 1 {
		t.Fatalf("despawns = %d, want the hider gone", doors.despawns)
	}
	// The occurrence runs on: round 2 hides at the next spot.
	host.Tick(start.Add(time.Minute))
	if len(doors.spots) != 2 || doors.spots[1].Name != "East_Market" {
		t.Fatalf("spots = %v, want round 2 at East Market", doors.spots)
	}
}

/*
================
TestSeekRoundEndsWithNoWinner

The timer runs out unfound: no draw, no grant, the hider leaves.
================
*/
func TestSeekRoundEndsWithNoWinner(t *testing.T) {
	doors := &seekDoors{fakeDoors: fakeDoors{wins: map[string]int{}}}
	host := New(true, parseSeek(t), doors.deps())
	start := seekStart()

	host.Tick(start)
	host.HandleWhisper(entrant("Ana", "a1"), "join")
	host.Tick(start.Add(10 * time.Minute))
	if len(doors.grants) != 0 {
		t.Fatalf("grants = %v, an unfound round paid out", doors.grants)
	}
	if doors.despawns != 1 {
		t.Fatalf("despawns = %d, want the hider gone at round end", doors.despawns)
	}
	joined := strings.Join(doors.announces, " ")
	if !strings.Contains(joined, "ends with no winner") {
		t.Fatalf("announces = %v", doors.announces)
	}
}

/*
================
TestSeekCapLeavesTheHuntRunning

A capped account's find does not count and does not end the round;
another account can still win it.
================
*/
func TestSeekCapLeavesTheHuntRunning(t *testing.T) {
	doors := &seekDoors{fakeDoors: fakeDoors{wins: map[string]int{"a1": MaxWinsPerAccountPerDay}}}
	host := New(true, parseSeek(t), doors.deps())
	start := seekStart()

	host.Tick(start)
	host.HandleWhisper(entrant("Ana", "a1"), "join")
	host.HandleWhisper(entrant("Bo", "a2"), "join")
	if host.ReportFound(entrant("Ana", "a1").Character) {
		t.Fatal("a capped account's find counted")
	}
	if _, live := host.LiveHider(); !live {
		t.Fatal("the round ended on a capped find")
	}
	if !host.ReportFound(entrant("Bo", "a2").Character) {
		t.Fatal("the open find was refused")
	}
	if len(doors.grants) != 1 || doors.grants[0].name != "Bo" {
		t.Fatalf("grants = %v, want Bo", doors.grants)
	}
}

/*
================
TestSeekStrangerCannotWin

Only an entrant can find: a player who never whispered is refused
and the round runs on.
================
*/
func TestSeekStrangerCannotWin(t *testing.T) {
	doors := &seekDoors{fakeDoors: fakeDoors{wins: map[string]int{}}}
	host := New(true, parseSeek(t), doors.deps())
	start := seekStart()

	host.Tick(start)
	if host.ReportFound(entrant("Ghost", "a9").Character) {
		t.Fatal("a non-entrant won the round")
	}
	if _, live := host.LiveHider(); !live {
		t.Fatal("the round ended on a stranger's claim")
	}
}

/*
================
TestSeekHiderLostEndsTheRound

A slain hider ends the round with no winner; the next round hides
anew.
================
*/
func TestSeekHiderLostEndsTheRound(t *testing.T) {
	doors := &seekDoors{fakeDoors: fakeDoors{wins: map[string]int{}}}
	host := New(true, parseSeek(t), doors.deps())
	start := seekStart()

	host.Tick(start)
	if !host.HiderLost() {
		t.Fatal("no hider to lose")
	}
	if doors.despawns != 1 {
		t.Fatalf("despawns = %d", doors.despawns)
	}
	joined := strings.Join(doors.announces, " ")
	if !strings.Contains(joined, "hider was slain") {
		t.Fatalf("announces = %v", doors.announces)
	}
	host.Tick(start.Add(time.Minute))
	if len(doors.spots) != 2 {
		t.Fatalf("spots = %v, want round 2 hidden", doors.spots)
	}
}

/*
================
TestSeekShutdownDespawns

Shutdown takes the standing hider out of the world.
================
*/
func TestSeekShutdownDespawns(t *testing.T) {
	doors := &seekDoors{fakeDoors: fakeDoors{wins: map[string]int{}}}
	host := New(true, parseSeek(t), doors.deps())

	host.Tick(seekStart())
	host.Shutdown()
	if doors.despawns != 1 {
		t.Fatalf("despawns = %d, want the hider gone at shutdown", doors.despawns)
	}
	if _, live := host.LiveHider(); live {
		t.Fatal("a hider still stands after shutdown")
	}
}

/*
================
TestSeekHintsNeverCarryCoordinates

Every line the bot posts across a round names the area only; the
spot's coordinates (1234, 9876 in the fixture) appear in none.
================
*/
func TestSeekHintsNeverCarryCoordinates(t *testing.T) {
	doors := &seekDoors{fakeDoors: fakeDoors{wins: map[string]int{}}}
	host := New(true, parseSeek(t), doors.deps())
	start := seekStart()

	host.Tick(start)
	host.Tick(start.Add(2 * time.Minute)) // the timed hint
	host.Tick(start.Add(4 * time.Minute))
	joined := strings.Join(doors.announces, "\n")
	if !strings.Contains(joined, "West Gate") {
		t.Fatalf("announces name no area:\n%s", joined)
	}
	for _, coord := range []string{"1234", "9876"} {
		if strings.Contains(joined, coord) {
			t.Fatalf("a hint carried the coordinate %s:\n%s", coord, joined)
		}
	}
}
