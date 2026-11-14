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

A pet's bonus hit queues its damage label on the monster leg, and the HP
it took reaches the monster's holder with the tick's other changes. Both
leave in the tick that committed the hit: held for the next tick they
arrive behind the hits that tick commits first.
================
*/
func TestPetBonusHitFramesLeaveInTheTickThatStruck(t *testing.T) {
	rt, clock, c, mob := wolfFieldFixture(t, 1)
	rt.Selected.Set(testDivision, c.Name, mob.Gid)
	step, ok := rt.wolfCompanion(testDivision, c, c.ActiveCOS.GID, clock.NowMs())
	if !ok {
		t.Fatal("no companion")
	}
	step.trained = &petTrainedStrike{mods: companion.Modifiers{PoisonAdd: 12}}
	rt.petGearElements(step, mob.Gid)
	rt.Monsters.DrainChangedVitals(nil)
	clock.now = clock.now.Add(time.Second)
	label, held := false, uint32(0)
	for _, batch := range rt.TickHook()(clock.NowMs()) {
		for _, frame := range batch.Frames {
			if frame.Opcode == abnormalDamageCreditOpcode && binary.LittleEndian.Uint32(frame.Payload) == mob.Gid {
				label = true
			}
			if frame.Opcode == simulation.OpVitalsUpdate && binary.LittleEndian.Uint32(frame.Payload) == mob.Gid && frame.Payload[6]&1 != 0 {
				if batch.OnlyCharacterID != c.ID {
					t.Fatal("the bonus hit's HP reached a non-holder")
				}
				held = binary.LittleEndian.Uint32(frame.Payload[7:])
			}
		}
	}
	after, _ := rt.Monsters.Get(testDivision, mob.Gid)
	if after.CurrentHP >= mob.CurrentHP {
		t.Fatal("the poison tick did not land in this tick")
	}
	if !label {
		t.Fatal("the tick that dealt the bonus damage did not publish its label")
	}
	if held != after.CurrentHP {
		t.Fatal("the holder did not receive the HP the bonus hit left", held, after.CurrentHP)
	}
}
