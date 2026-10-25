/*
===========================================================================
petgearelement_test.go - distinct elemental hits and exact poison pulses
===========================================================================
*/
package action

import (
	"encoding/binary"
	"opensro.online/server/internal/game/companion"
	"strings"
	"testing"
)

/*
================
TestPetGearPoisonTicksAndRefresh
================
*/
func TestPetGearPoisonTicksAndRefresh(t *testing.T) {
	rt, clock, c, mob := wolfFieldFixture(t, 1)
	step, ok := rt.wolfCompanion(testDivision, c, c.ActiveCOS.GID, clock.NowMs())
	if !ok {
		t.Fatal("no companion")
	}
	step.trained = &petTrainedStrike{mods: companion.Modifiers{PoisonAdd: 12}}
	rt.petGearElements(step, mob.Gid)
	key := petOwnerKey{division: testDivision, name: strings.ToLower(c.Name), gid: c.ActiveCOS.GID}
	start := step.nowMs
	rt.advancePetGearPoison(key, start+999)
	before, _ := rt.Monsters.Get(testDivision, mob.Gid)
	if before.CurrentHP != mob.CurrentHP {
		t.Fatal("early poison")
	}
	step.nowMs = start + 500
	rt.petGearElements(step, mob.Gid)
	for tick := int64(1); tick <= 4; tick++ {
		rt.advancePetGearPoison(key, start+tick*1000)
		after, _ := rt.Monsters.Get(testDivision, mob.Gid)
		if mob.CurrentHP-after.CurrentHP != uint32(tick*3) {
			t.Fatalf("tick %d damage=%d", tick, mob.CurrentHP-after.CurrentHP)
		}
	}
	if len(step.state.trained.poison) != 0 {
		t.Fatal("poison did not expire")
	}
	count := 0
	for _, batch := range rt.drainMonsterLegRecipients() {
		for _, frame := range batch.Frames {
			if frame.Opcode != abnormalDamageCreditOpcode {
				continue
			}
			if batch.OnlyCharacterID != c.ID || binary.LittleEndian.Uint32(frame.Payload[4:]) != 3 {
				t.Fatal("yellow poison number or recipient")
			}
			count++
		}
	}
	if count != 4 {
		t.Fatal("yellow poison tick count", count)
	}
}

/*
================
TestPetGearElementsSeparateAndDoNotProc
================
*/
func TestPetGearElementsSeparateAndDoNotProc(t *testing.T) {
	rt, clock, c, mob := wolfFieldFixture(t, 1)
	step, ok := rt.wolfCompanion(testDivision, c, c.ActiveCOS.GID, clock.NowMs())
	if !ok {
		t.Fatal("no companion")
	}
	step.trained = &petTrainedStrike{mods: companion.Modifiers{FireAdd: 7, ColdAdd: 11, ShockAdd: 13}}
	rt.petGearElements(step, mob.Gid)
	after, _ := rt.Monsters.Get(testDivision, mob.Gid)
	if mob.CurrentHP-after.CurrentHP != 31 {
		t.Fatal("elemental rider total", mob.CurrentHP-after.CurrentHP)
	}
	if step.state.trained.attacks != 0 || step.state.trained.procReadyAt != 0 {
		t.Fatal("recursive counters/procs")
	}
}
