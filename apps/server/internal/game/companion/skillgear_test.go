/*
===========================================================================

skillgear_test.go - individual skill blues apply to learned books only

===========================================================================
*/
package companion

import (
	"opensro.online/server/internal/domain"
	"testing"
)

/*
================
TestIndividualSkillBlues
================
*/
func TestIndividualSkillBlues(t *testing.T) {
	count := 0
	for _, a := range D.Affixes {
		if a.SkillID == 0 {
			continue
		}
		count++
		for _, pet := range D.Pets {
			p := domain.CharacterCOS{Codename: pet.CodenamePrefix + "_090", Level: 90}
			p.Training.TreeVersion = 2
			for slot, gear := range D.Gear[8].Items {
				roll := fixedRoller{0, 0.99, 0, 0.999999}
				degree := D.Gear[8]
				degree.MagicChance, degree.RareChance = 1, 0
				options := RollGearWithAffixes(degree, slot, 0, &roll, []AffixDef{a})
				if len(options) != 1 {
					t.Fatalf("%s slot %d: cannot roll skill blue", a.ID, slot)
				}
				decoded, value, ok := DecodeAffix(options[0])
				if !ok || decoded.SkillID != a.SkillID || value != 5 {
					t.Fatalf("skill roll: %+v %d", decoded, value)
				}
				p.Training.Gear[slot] = domain.PetGear{RefObjID: gear.ID}
				p.Training.Gear[slot].Options[0] = options[0]
			}
			m := ModifiersFor(&p)
			for _, s := range D.TreeSkills {
				if s.Pet != pet.Name {
					continue
				}
				if EffectiveLevel(&p, s, m) != 0 {
					t.Fatal("gear learned a skill")
				}
				p.Training.Levels[s.Index()] = 1
				want := uint8(1)
				if pet.Name == a.Pets && s.Row == int((a.SkillID-1)%15)/3 {
					want = 16
				}
				if got := EffectiveLevel(&p, s, m); got != want {
					t.Fatalf("%s on %s skill %s = %d, want %d", a.ID, pet.Name, s.Name, got, want)
				}
				p.Training.Levels[s.Index()] = 0
			}
		}
	}
	if count != 20 {
		t.Fatalf("individual skill families=%d", count)
	}
}

/*
================
TestRaisedElementAndCriticalCaps
================
*/
func TestRaisedElementAndCriticalCaps(t *testing.T) {
	for code, want := range map[uint16]float64{11: 30, 15: 775, 19: 775, 23: 1525, 30: 50, 50: 100} {
		a, ok := AffixByCode(code)
		if !ok || a.Max != want {
			t.Fatalf("affix %d: %+v, want maximum %v", code, a, want)
		}
	}
}

/*
================
TestElementalCapsByDegree
================
*/
func TestElementalCapsByDegree(t *testing.T) {
	for _, group := range []string{"Fire", "Cold", "Lightning", "Poison"} {
		caps := []uint32{100, 213, 325, 438, 550, 663, 775, 888, 1000}
		if group == "Lightning" {
			caps = []uint32{100, 338, 575, 813, 1050, 1288, 1525, 1763, 2000}
		}
		if group == "Poison" {
			caps = []uint32{300, 1388, 2475, 3563, 4650, 5738, 6825, 7913, 9000}
		}
		for degree, want := range caps {
			var eligible []AffixDef
			for _, a := range D.Affixes {
				if a.Group == group && a.MinDegree <= degree+1 {
					eligible = append(eligible, a)
				}
			}
			highest := uint32(0)
			for _, a := range eligible {
				d := D.Gear[degree]
				d.MagicChance, d.RareChance = 1, 0
				for slot := 0; slot < 2; slot++ {
					r := fixedRoller{0, 0.99, 0, 0.999999}
					options := RollGearWithAffixes(d, slot, 0, &r, []AffixDef{a})
					if len(options) != 1 {
						t.Fatalf("%s D%d slot%d missing roll", group, degree+1, slot)
					}
					_, value, _ := DecodeAffix(options[0])
					if value > want {
						t.Fatalf("%s D%d rolled%d above%d", group, degree+1, value, want)
					}
					highest = max(highest, value)
				}
			}
			if highest != want {
				t.Fatalf("%s D%d max=%d,want%d", group, degree+1, highest, want)
			}
		}
	}
}
