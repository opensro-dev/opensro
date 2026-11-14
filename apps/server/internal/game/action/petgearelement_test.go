/*
===========================================================================
petgearelement_test.go - distinct elemental hits and exact poison pulses
===========================================================================
*/
package action

import (
	"encoding/binary"
	"opensro.online/server/internal/game/companion"
	"opensro.online/server/internal/game/world/simulation"
	"strings"
	"testing"
	"time"
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

/*
================
TestPetBonusHitFramesLeaveInTheTickThatStruck

A pet's bonus hit queues its frames on the monster leg, and its HP frame
carries the HP that hit left. The frames must leave in the tick that
committed the hit: held for the next tick's drain they arrive behind the
hits that tick commits first (a prepared cast's release), with an HP those
hits have already lowered, and the client's bar jumps back up.
================
*/
func TestPetBonusHitFramesLeaveInTheTickThatStruck(t *testing.T) {
	rt, clock, c, mob := wolfFieldFixture(t, 1)
	step, ok := rt.wolfCompanion(testDivision, c, c.ActiveCOS.GID, clock.NowMs())
	if !ok {
		t.Fatal("no companion")
	}
	step.trained = &petTrainedStrike{mods: companion.Modifiers{PoisonAdd: 12}}
	rt.petGearElements(step, mob.Gid)
	clock.now = clock.now.Add(time.Second)
	published := false
	for _, batch := range rt.TickHook()(clock.NowMs()) {
		for _, frame := range batch.Frames {
			if frame.Opcode == simulation.OpVitalsUpdate && binary.LittleEndian.Uint32(frame.Payload) == mob.Gid {
				published = true
			}
		}
	}
	after, _ := rt.Monsters.Get(testDivision, mob.Gid)
	if after.CurrentHP >= mob.CurrentHP {
		t.Fatal("the poison tick did not land in this tick")
	}
	if !published {
		t.Fatal("the tick that dealt the bonus damage did not publish the monster's HP")
	}
}
