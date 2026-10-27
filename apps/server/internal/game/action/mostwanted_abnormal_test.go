/*
===========================================================================

mostwanted_abnormal_test.go - a damage-over-time job kill counts even when
the killer's side of the pay produces no frames

A fatal abnormal tick pays the killer through playerAbnormalPublication's
own door. When the kill pays no EXP frames (no progression writers wired),
no streak-progress notice (the notice rule off) and no level change, the
killer's frame lists are empty - but the Most Wanted count has already
happened inside the door. The door must commit on the count, not on the
frames, or a counted kill is thrown back (BR-261015-0844-66A1, H-B).

===========================================================================
*/
package action

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/pk"
	"opensro.online/server/internal/testsupport/gamedatatest"
)

/*
================
TestDamageOverTimeJobKillCommitsWithoutKillerFrames

A suited hunter's damage over time kills a suited thief. The runtime has
no EXP writers and the streak notice is off, so the pay door produces no
killer frames at all; the kill must still commit and the killer's counted
streak must reach the committed record.
================
*/
func TestDamageOverTimeJobKillCommitsWithoutKillerFrames(t *testing.T) {
	rt, _, hunter, thief := newPvpPair(t)
	rt.MostWanted = true
	rt.deps.(*enterworld.Deps).Levels = enterworld.NewTextdataLevels(gamedatatest.TextdataDir(t))
	hunter.AccountID, thief.AccountID = "hunter-account", "thief-account"
	dressForJob(rt, hunter, 9301, "ITEM_CH_M_TRADE_HUNTER_04", 3)
	dressForJob(rt, thief, 9302, "ITEM_CH_M_TRADE_THIEF_04", 2)
	kill := rt.classifyPlayerKill(testDivision, hunter, thief)
	if kill.kind != pk.DeathJob {
		t.Fatalf("suited hunter killing a suited thief classified %v, want DeathJob", kill.kind)
	}
	// A store-like door: the wrapped update runs against the live record,
	// and only a true return commits (marks the record for persistence).
	var committed *enterworld.Character
	rt.deps.(*enterworld.Deps).UpdateCharacter = func(c *enterworld.Character, label string, fn func() bool) bool {
		if !fn() {
			return false
		}
		committed = c.Snapshot()
		return true
	}
	owner := &playerAbnormalOwner{rt: rt, division: testDivision, c: thief, now: 1000,
		fatal: true, deathKiller: hunter, deathKill: kill}
	rt.playerAbnormalPublication(testDivision, thief, owner)
	if hunter.MostWanted == nil || hunter.MostWanted.Streak != 1 {
		t.Fatalf("the kill did not count in memory: %+v", hunter.MostWanted)
	}
	if committed == nil || committed.MostWanted == nil || committed.MostWanted.Streak != 1 {
		t.Fatalf("the counted kill never committed (committed record: %+v)", committed)
	}
}
