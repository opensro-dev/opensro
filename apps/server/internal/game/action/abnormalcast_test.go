/*
===========================================================================

abnormalcast_test.go - the abnormal killer capture reads the cast view

captureAbnormalSource detached a whole character record - inventory
included - for every distinct status source on the player and COS
abnormal lanes (#1943). The capture now takes the cast view: combat
rows, the whole avatar inventory and every companion-carrying bag row.
Each arm here runs on a bag loaded with plain rows, once on the cast
view and once forced onto the full copy, and every fact a killer
reader weighs must agree: existence, death, the striker level, the
kill classification and the death kind. A reader that reached a plain
bag row through its copy would diverge.

===========================================================================
*/

package action

import (
	"reflect"
	"testing"

	"opensro.online/server/internal/game/enterworld"
)

/*
================
TestCaptureAbnormalSourceCastMatchesFull
================
*/
func TestCaptureAbnormalSourceCastMatchesFull(t *testing.T) {
	rt, _, c, _ := newCombatTestRuntime(t, 100000)
	equipCombatTestPet(t, rt, c, cosBandAttackPet)
	pet := c.ActiveCOS
	pet.Level = 7
	petPeriodicFatten(c)
	victim := nearbyCharacter(rt, c, 21, "cast-victim", 50)
	gid := enterworld.ObjectIDForCharacter(c)

	castPlayer := rt.captureAbnormalSource(testDivision, gid)
	castCOS := rt.captureAbnormalSource(testDivision, pet.GID)
	forceFullCastCopy(rt)
	fullPlayer := rt.captureAbnormalSource(testDivision, gid)
	fullCOS := rt.captureAbnormalSource(testDivision, pet.GID)

	for name, pair := range map[string][2]abnormalSourceState{
		"player": {castPlayer, fullPlayer},
		"cos":    {castCOS, fullCOS},
	} {
		castState, fullState := pair[0], pair[1]
		if castState.exists != fullState.exists || castState.dead != fullState.dead ||
			castState.killer.strikerLevel != fullState.killer.strikerLevel {
			t.Fatalf("%s source cast %+v full %+v", name, castState, fullState)
		}
		castKiller, fullKiller := castState.killer.player, fullState.killer.player
		if castKiller == nil || fullKiller == nil || castKiller.ID != fullKiller.ID || castKiller.Name != fullKiller.Name {
			t.Fatalf("%s killer cast %+v full %+v", name, castKiller, fullKiller)
		}
		if kind := rt.deathKind(testDivision, victim, castState.killer); kind != rt.deathKind(testDivision, victim, fullState.killer) {
			t.Fatalf("%s death kind cast %v full %v", name, kind, rt.deathKind(testDivision, victim, fullState.killer))
		}
		castKill := rt.classifyPlayerKill(testDivision, castKiller, victim)
		fullKill := rt.classifyPlayerKill(testDivision, fullKiller, victim)
		if !reflect.DeepEqual(castKill, fullKill) {
			t.Fatalf("%s kill cast %+v full %+v", name, castKill, fullKill)
		}
		if pet := castKiller.CompanionByGID(pet.GID); pet == nil || pet.Level != fullKiller.CompanionByGID(pet.GID).Level {
			t.Fatalf("%s companion resolution diverged", name)
		}
	}
	if len(castPlayer.killer.player.MissionInventory) >= len(fullPlayer.killer.player.MissionInventory) {
		t.Fatal("cast capture kept the plain bag rows")
	}

	rt.castCopy = nil
	castAllocs := testing.AllocsPerRun(20, func() { rt.captureAbnormalSource(testDivision, gid) })
	forceFullCastCopy(rt)
	fullAllocs := testing.AllocsPerRun(20, func() { rt.captureAbnormalSource(testDivision, gid) })
	t.Logf("abnormal capture cast %.1f allocs vs full %.1f", castAllocs, fullAllocs)
	if castAllocs >= fullAllocs/2 {
		t.Fatalf("cast capture allocs %.1f, want under half the %.1f-alloc full capture", castAllocs, fullAllocs)
	}
}

/*
================
BenchmarkCaptureAbnormalSourceCast
================
*/
func BenchmarkCaptureAbnormalSourceCast(b *testing.B) {
	rt, _, c, _ := newCombatTestRuntime(b, 100000)
	petPeriodicFatten(c)
	gid := enterworld.ObjectIDForCharacter(c)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = rt.captureAbnormalSource(testDivision, gid)
	}
}

/*
================
BenchmarkCaptureAbnormalSourceFull

The pre-#1943 capture, forced through the cast seam: the same capture
on the full record.
================
*/
func BenchmarkCaptureAbnormalSourceFull(b *testing.B) {
	rt, _, c, _ := newCombatTestRuntime(b, 100000)
	petPeriodicFatten(c)
	forceFullCastCopy(rt)
	gid := enterworld.ObjectIDForCharacter(c)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = rt.captureAbnormalSource(testDivision, gid)
	}
}
