/*
===========================================================================

premiumclock_test.go - the premium clock's state writes

The reference server's premium keeper writes its state every 600 seconds
and the port commits the clock on the same cadence, on the tick
the allotment runs out, on the day's refill and at the period's end. A
spent allotment has nothing left to run out: it must not commit the
unchanged clock on every tick (2026-10-13 prod profile: one full character
commit per tick per spent premium player, 5% of the GameWorld).

===========================================================================
*/

package action

import (
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
)

/*
================
TestSpentPremiumAllotmentCommitsOnlyOnItsCadence

Three online hours spend the day's grant: the tick that empties it commits
once. Thirty more minutes, ticked every second, then commit only on the
600-second cadence (three times), and the next day's refill commits again.
================
*/
func TestSpentPremiumAllotmentCommitsOnlyOnItsCadence(t *testing.T) {
	c, rt, clock := premiumTicketInUse()
	commits := 0
	rt.deps.(*enterworld.Deps).UpdateCharacter = func(character *enterworld.Character, label string, update func() bool) bool {
		if label == "premium-clock" {
			commits++
		}
		return update()
	}
	tick := func(d time.Duration) {
		for step := time.Duration(0); step < d; step += time.Second {
			clock.Advance(time.Second)
			rt.advanceParamJobs(clock.NowMs())
		}
	}
	rt.advanceParamJobs(clock.NowMs()) // the owner's first tick starts its ledger row
	tick(3*time.Hour - time.Second)
	if c.PremiumClock == nil || c.PremiumClock.TodayMs == 0 {
		t.Fatalf("before the third hour ends the clock is %+v, want time left", c.PremiumClock)
	}
	commits = 0
	tick(time.Second)
	if c.PremiumClock.TodayMs != 0 || c.PremiumClock.CarriedMs != 0 || commits != 1 {
		t.Fatalf("the emptying tick: clock %+v after %d commits, want spent after exactly one", c.PremiumClock, commits)
	}
	commits = 0
	tick(30 * time.Minute)
	if commits != 3 {
		t.Fatalf("a spent allotment committed %d times in 30 minutes of 1 s ticks, want 3 (every 600 s)", commits)
	}
	// The next day refills the grant, and that commits.
	commits = 0
	clock.Advance(24 * time.Hour)
	rt.advanceParamJobs(clock.NowMs())
	if commits != 1 || c.PremiumClock.TodayMs != 3*3600000 {
		t.Fatalf("next day: %d commits, clock %+v, want one commit refilling the grant", commits, c.PremiumClock)
	}
}
