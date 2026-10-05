/*
===========================================================================

monsterlifetime_test.go - tests for monsterlifetime.go: 4C10C0's timers

===========================================================================
*/

package simulation

import (
	"testing"

	"opensro.online/server/internal/game/world/monster"
)

/*
================
TestSpawnLifetimeTimers
================
*/
func TestSpawnLifetimeTimers(t *testing.T) {
	for _, tc := range []struct {
		name      string
		ref       monster.MonsterRef
		until     int64
		refreshed bool
		armed     bool
	}{
		{"wing tribe", monster.MonsterRef{Codename: "MOB_QT_01_WINGTRIBE"}, 1000 + 180000, false, true},
		{"punisher clone", monster.MonsterRef{Codename: "mob_qt_02_punisher_clon"}, 1000 + 300000, false, true},
		{"grade seven", monster.MonsterRef{Codename: "MOB_X", MonsterType: 7}, 1000 + 300000, false, true},
		{"thief", monster.MonsterRef{Codename: "MOB_THIEF_NPC_0001", TidWord: 0x00c6, TypeID4: 2}, 1000 + 20000, true, true},
		{"ordinary", monster.MonsterRef{Codename: "MOB_CH_MANGNYANG"}, 0, false, false},
	} {
		lifetime, armed := spawnLifetime(monster.Instance{Ref: tc.ref}, 1000)
		if armed != tc.armed || lifetime.untilMs != tc.until || lifetime.refreshed != tc.refreshed {
			t.Fatalf("%s: %+v armed %v", tc.name, lifetime, armed)
		}
	}
}
