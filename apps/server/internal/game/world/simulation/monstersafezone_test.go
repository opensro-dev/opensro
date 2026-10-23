/*
===========================================================================

monstersafezone_test.go - monsters vanish when they step into a town

===========================================================================
*/

package simulation

import (
	"testing"
	"time"

	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/monster"
)

/*
================
testRegionCombat

A synthetic battlefield table: 0x62A6 and 0x62AA are battlefields, the run
between them is protected, and region 25000 (0x61A8) is a town.
================
*/
func testRegionCombat(t *testing.T) *worldgeom.RegionCombat {
	t.Helper()
	table, err := worldgeom.NewRegionCombat([]worldgeom.RegionCombatRange{
		{First: 0x61a8, Last: 0x61a8},
		{First: 0x62a6, Last: 0x62a6, Allowed: true},
		{First: 0x62a7, Last: 0x62a9},
		{First: 0x62aa, Last: 0x62aa, Allowed: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	return table
}

/*
================
TestSafeZoneRegionReadsTheBattlefieldTable

0x62A6 is a battlefield, 0x62A7 is not; an unknown region is not a town.
Region 25000 (0x61A8) lies inside Jangan.
================
*/
func TestSafeZoneRegionReadsTheBattlefieldTable(t *testing.T) {
	s := NewMonsterState(monster.Template{})
	s.SetRegionCombat(testRegionCombat(t))
	for _, tc := range []struct {
		region uint16
		safe   bool
	}{{0x62a6, false}, {0x62a7, true}, {25000, true}, {0, false}} {
		if got := s.SafeZoneRegion(tc.region); got != tc.safe {
			t.Fatalf("SafeZoneRegion(%#x) = %v, want %v", tc.region, got, tc.safe)
		}
	}
}

/*
================
TestMonsterInTownVanishes

4C1270: a monster whose region is a town is set to life state 3 and
leaves; its nest schedules a respawn. Outside a town it stays.
================
*/
func TestMonsterInTownVanishes(t *testing.T) {
	ops, instance := monsterLegFixture(t, passiveTactics())
	ops.Monsters.SetRegionCombat(testRegionCombat(t))
	now := time.Now().UnixMilli()
	ops.vanishInSafeZone(monsterTestDivision, instance.Gid, now)
	if _, ok := ops.Monsters.Mover(monsterTestDivision, instance.Gid); !ok {
		t.Fatal("a monster outside every town vanished")
	}
	mover, _ := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
	mover.Pose.RegionID = 25000 // inside Jangan
	ops.Monsters.CommitMover(monsterTestDivision, instance.Gid, mover)
	ops.vanishInSafeZone(monsterTestDivision, instance.Gid, now)
	if _, ok := ops.Monsters.Mover(monsterTestDivision, instance.Gid); ok {
		t.Fatal("a monster standing in Jangan stayed")
	}
	if _, ok := ops.Monsters.Get(monsterTestDivision, instance.Gid); ok {
		t.Fatal("the vanished monster is still registered")
	}
}
