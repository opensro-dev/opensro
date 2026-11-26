/*
===========================================================================

gmunique_test.go - GM kills are not character attacks

/MOBKILL and /ZOE settle a kill through the ordinary HP door, so a
build-locked unique's admission rule (SRO_STR_INT_UNIQUES,
uniquevariant.go) never stands between the GM and a copy. Both commands
must work whatever the GM's own allocation is.

===========================================================================
*/
package action

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/staff"
	"opensro.online/server/internal/game/world/monster"
)

/*
================
TestGMKillIgnoresTheBuildLock

A magical GM kills a STR copy and a physical GM kills an INT copy, with
rewards on: the commands choose the victim, not the master's build.
================
*/
func TestGMKillIgnoresTheBuildLock(t *testing.T) {
	for _, tc := range []struct {
		name            string
		build           monster.UniqueBuild
		strength, magic int64
		zoe             bool
	}{
		{"mobkill magical GM on a STR copy", monster.UniqueBuildSTR, 100, 300, false},
		{"mobkill physical GM on an INT copy", monster.UniqueBuildINT, 300, 100, false},
		{"zoe magical GM on a STR copy", monster.UniqueBuildSTR, 100, 300, true},
		{"zoe physical GM on an INT copy", monster.UniqueBuildINT, 300, 100, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rt, _, gm, target := newCombatTestRuntimeMonsterTuned(t, 500, 20, 0, func(ref *monster.MonsterRef) {
				ref.MonsterType = 3
				ref.UniqueBuild = tc.build
				ref.Codename = "MOB_CH_TIGERWOMAN_STR"
			})
			gm.GMPrivilege, gm.GMCapabilities = true, uint32(staff.All)
			gm.Strength, gm.Intellect = testInt64(tc.strength), testInt64(tc.magic)
			if err := rt.admitPopulationSession(testDivision, gm.Name, 1); err != nil {
				t.Fatal(err)
			}
			rt.UpdateExperience = func(*enterworld.Character, int64, int64, uint32) ([]wire.Frame, bool) {
				return nil, true
			}
			var killed bool
			if tc.zoe {
				killed = rt.KillGMZoe(testDivision, gm.Name, target.Ref.RefObjID, 1)
			} else {
				killed = rt.KillGMMonster(testDivision, gm.Name, target.Gid, true)
			}
			if !killed {
				t.Fatal("the GM kill was refused by the copy's build lock")
			}
		})
	}
}
