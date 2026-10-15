/*
===========================================================================

tunables_test.go - the port-only rules read their tunable values

Each rule reads game/tunables: a changed value changes what the rule
does, and a snapshot swapped between two actions reaches the second one
only (live values), while kills per level is read once at boot.

===========================================================================
*/
package action

import (
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/pk"
	"opensro.online/server/internal/game/tunables"
)

/*
================
swapTunables

Installs a snapshot parsed from values for the rest of the test.
================
*/
func swapTunables(t *testing.T, values map[string]string) {
	t.Helper()
	s, err := tunables.Parse(func(name string) string { return values[name] })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(tunables.Swap(s))
}

/*
================
TestJobKillNoticeCapFollowsItsTunable

SRO_JOB_KILL_NOTICES_PER_MINUTE=2: eight job kills of one pair (repeat
on) post two notices; the drop/plus cap is unchanged.
================
*/
func TestJobKillNoticeCapFollowsItsTunable(t *testing.T) {
	swapTunables(t, map[string]string{"SRO_JOB_KILL_NOTICES_PER_MINUTE": "2"})
	rt, _, thief, trader := newPvpPair(t)
	dressForJob(rt, thief, 9302, "ITEM_CH_M_TRADE_THIEF_04", 2)
	dressForJob(rt, trader, 9303, "ITEM_CH_M_TRADE_TRADER_04", 1)
	rt.Announce.JobKill, rt.Announce.JobKillRepeat = true, true
	for i := int64(0); i < 8; i++ {
		k := playerKill{kind: rt.playerKillKind(testDivision, trader, thief)}.withVictim(thief, trader)
		_, _, after := rt.payPlayerKillInDoor(testDivision, thief, trader, k, i*1000)
		rt.payJobKillShares(after)
	}
	if got := drainedNotices(t, rt); len(got) != 2 {
		t.Fatalf("%d job-kill notices, want 2", len(got))
	}
	if announceCap(tunables.Current(), announceSoxDrop) != announcementsPerMinute {
		t.Fatal("the drop notice cap moved with the job-kill tunable")
	}
}

/*
================
TestJobKillRepeatWindowFollowsItsTunable

SRO_JOB_KILL_REPEAT_MINUTES=1: the pair reopens after one minute, not ten.
================
*/
func TestJobKillRepeatWindowFollowsItsTunable(t *testing.T) {
	swapTunables(t, map[string]string{"SRO_JOB_KILL_REPEAT_MINUTES": "1"})
	rt, _ := newTestRuntime(testCharacter(), testItems())
	if !rt.jobKillPairQuiet("A", "B", 0) || rt.jobKillPairQuiet("A", "B", 59_999) || !rt.jobKillPairQuiet("A", "B", 60_000) {
		t.Fatal("the pair window is not one minute")
	}
}

/*
================
TestMostWantedTestBountyIsReadAtEachPayout

The test bounty is live: a snapshot swapped between two kills pays the
first kill at the old value and the second at the new one.
================
*/
func TestMostWantedTestBountyIsReadAtEachPayout(t *testing.T) {
	rt, _, killer, victim := newPvpPair(t)
	rt.MostWanted, rt.MostWantedTest = true, true
	pay := func() uint64 {
		victim.MostWanted = &domain.MostWanted{Streak: 20}
		before := goldOf(killer)
		kill := playerKill{kind: pk.DeathJob, mostWantedStrangers: true}.withVictim(killer, victim)
		rt.payPlayerKillInDoor(testDivision, killer, victim, kill, 0)
		return goldOf(killer) - before
	}
	if got := pay(); got != 2*1_000_000 {
		t.Fatalf("default test bounty paid %d, want 2,000,000", got)
	}
	swapTunables(t, map[string]string{"SRO_MOST_WANTED_TEST_BOUNTY": "500"})
	if got := pay(); got != 2*500 {
		t.Fatalf("swapped test bounty paid %d, want 1,000", got)
	}
}

/*
================
TestMostWantedTestKillsPerLevelIsReadFromTheEnvironment

SRO_MOST_WANTED_TEST_KILLS sets the streak step at boot (10 / kills); a
value that does not divide 10 is refused.
================
*/
func TestMostWantedTestKillsPerLevelIsReadFromTheEnvironment(t *testing.T) {
	for env, want := range map[string]int64{"": 2, "5": 2, "2": 5, "10": 1, "1": 10} {
		t.Setenv("SRO_MOST_WANTED_TEST_KILLS", env)
		if got, err := MostWantedTestStreakStepFromEnv(); err != nil || got != want {
			t.Fatalf("%q: step %d (%v), want %d", env, got, err, want)
		}
	}
	t.Setenv("SRO_MOST_WANTED_TEST_KILLS", "3")
	if _, err := MostWantedTestStreakStepFromEnv(); err == nil {
		t.Fatal("3 kills a level was accepted")
	}
	rt, _ := newTestRuntime(testCharacter(), testItems())
	rt.MostWantedTest, rt.MostWantedTestStreakStep = true, 5
	if rt.mostWantedStreakStep() != 5 {
		t.Fatal("the boot step is not used")
	}
}
