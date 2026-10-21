/*
===========================================================================
petskillcounter_test.go - private stack snapshots use actual combat memory.
===========================================================================
*/
package action

import (
	"encoding/binary"
	"testing"

	"opensro.online/server/internal/game/companion"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
TestPetSkillCounterFrame
================
*/
func TestPetSkillCounterFrame(t *testing.T) {
	rt, clock, c, _ := newPetCombatRuntime(t, 100000, attackPetBand)
	makeTrainedPet(rt, c, "COS_P_BEAR_090", 90)
	rt.AttackPetExpansion = true
	roarField(t, rt, c, clock)
	learnPetBook(t, c, 24)
	learnPetBook(t, c, 30)
	pet := c.ActiveCOS
	state := rt.petSessionFor(testDivision, c.Name, pet.GID)
	if state == nil {
		t.Fatal("missing session")
	}
	for range 9 {
		rt.gainPetSpirit(testDivision, c, pet, clock.NowMs())
	}
	// The regular hit-door tests verify admission; this checks publication of
	// both independent counters and eight/ten-attack cycles without truncation.
	state.trained.attacks = 79
	state.trained.counterHits = [2]int{9, 8}
	f := petSkillCounterFrame(state, pet.GID)
	if f.Opcode != petSkillCounterOpcode || len(f.Payload) != 17 || f.Payload[4] != 39 || f.Payload[5] != 9 || f.Payload[6] != 8 || f.Payload[7] != 9 {
		t.Fatalf("counters: %x", f.Payload)
	}
	key := petOwnerKey{division: testDivision, name: c.Name, gid: pet.GID}
	for _, check := range []struct {
		enabled bool
		count   int
	}{{false, 0}, {true, 1}, {true, 0}} {
		rt.AttackPetExpansion = check.enabled
		_, frames, _, _, _, _, _ := rt.advancePetInDivision(key, clock.NowMs())
		count := 0
		for _, frame := range frames {
			if frame.Opcode == petSkillCounterOpcode {
				count++
			}
		}
		if count != check.count {
			t.Fatalf("counter pushes enabled=%v: %d, want %d", check.enabled, count, check.count)
		}
	}
	pet.CurrentHP = 0
	f = petSkillCounterFrame(state, pet.GID)
	if string(f.Payload[4:]) != string(make([]byte, 13)) {
		t.Fatalf("dead pet retained counters: %x", f.Payload)
	}
	if len(petSkillCounterFrame(state, pet.GID+1).Payload) != 0 || len(petSkillCounterFrame(nil, pet.GID).Payload) != 0 {
		t.Fatal("wrong pet was published")
	}
}

/*
================
TestPetSkillPreviewIncludesGear
================
*/
func TestPetSkillPreviewIncludesGear(t *testing.T) {
	rt, _, c, _ := newPetCombatRuntime(t, 100000, attackPetBand)
	makeTrainedPet(rt, c, "COS_P_BEAR_090", 90)
	rt.AttackPetExpansion = true
	skill := learnPetBook(t, c, 24)
	c.ActiveCOS.Training.Levels[skill.Index()] = 1
	c.ActiveCOS.Training.Gear[2].RefObjID = companion.D.Gear[7].Items[2].ID
	c.ActiveCOS.Training.Gear[2].Options[0] = companion.EncodeAffix(65, 2) // of the Bulwark: +2 Bear skills.
	for _, f := range rt.petTrainingStateFrames(c.ActiveCOS, 0) {
		if f.Opcode != wire.OpPetTrainingState {
			continue
		}
		p, at := f.Payload, 9
		for range 2 {
			at += 7 + int(p[at+6])
		}
		at += 3
		n := int(p[at])
		at++
		for range n {
			if p[at] == skill.ID {
				if p[at+2] != 3 {
					t.Fatalf("gear bonus not applied: %d", p[at+2])
				}
				want := uint16(skill.ValueAt(4) * 10)
				if got := binary.LittleEndian.Uint16(p[at+7:]); got != want {
					t.Fatalf("preview %d, want %d", got, want)
				}
				return
			}
			at += 9
		}
	}
	t.Fatal("missing skill preview")
}
