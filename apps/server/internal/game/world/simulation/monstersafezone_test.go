/*
===========================================================================

monstersafezone_test.go - monsters vanish when they step into a town

===========================================================================
*/

package simulation

import (
	"testing"
	"time"
)

/*
================
TestSafeZoneRegionReadsTheBattlefieldTable

0x62A6 is a battlefield, 0x62A7 is not; an unknown region is not a town.
Region 25000 (0x61A8) lies inside Jangan.
================
*/
func TestSafeZoneRegionReadsTheBattlefieldTable(t *testing.T) {
	for _, tc := range []struct {
		region uint16
		safe   bool
	}{{0x62a6, false}, {0x62a7, true}, {25000, true}, {0, false}} {
		if got := SafeZoneRegion(tc.region); got != tc.safe {
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
	now := time.Now().UnixMilli()
	ops.SafeZone = func(region uint16) bool { return region != 25000 }
	ops.vanishInSafeZone(monsterTestDivision, instance.Gid, now)
	if _, ok := ops.Monsters.Mover(monsterTestDivision, instance.Gid); !ok {
		t.Fatal("a monster outside every town vanished")
	}
	ops.SafeZone = SafeZoneRegion
	ops.vanishInSafeZone(monsterTestDivision, instance.Gid, now)
	if _, ok := ops.Monsters.Mover(monsterTestDivision, instance.Gid); ok {
		t.Fatal("a monster standing in Jangan stayed")
	}
	if _, ok := ops.Monsters.Get(monsterTestDivision, instance.Gid); ok {
		t.Fatal("the vanished monster is still registered")
	}
}
