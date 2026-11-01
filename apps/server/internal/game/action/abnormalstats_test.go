/*
===========================================================================

abnormalstats_test.go - every abnormal status keeps player stats buildable

===========================================================================
*/

package action

import (
	"testing"

	"opensro.online/server/internal/game/abnormal"
)

/*
==================
TestEveryAbnormalStatusKeepsPlayerStatsBuildable

Every abnormal callback writes into the player keeper (reference server).
A write to a node the keeper lacks fails the whole stats projection, and
with it every cast, cost and item use of an afflicted player. Frostbite's
0x17/0x18 writes were such a hole.
==================
*/
func TestEveryAbnormalStatusKeepsPlayerStatsBuildable(t *testing.T) {
	for status := abnormal.Freeze; status <= abnormal.TimeBomb; status++ {
		rt, clock, caster, monster := newCombatTestRuntime(t, 100)
		seedPlayerStatus(rt, caster, status, 100000, clock.NowMs(), monster.Gid)
		if _, _, err := rt.playerCombatStats(testDivision, caster); err != nil {
			t.Fatalf("status %d: %v", status, err)
		}
	}
}
