/*
===========================================================================

gearstats_test.go - flat equipment and lower-biased blue rolls

===========================================================================
*/
package companion

import (
	"opensro.online/server/internal/domain"
	"testing"
)

/*
================
TestFlatPetEquipmentAndBiasedBlue
================
*/
func TestFlatPetEquipmentAndBiasedBlue(t *testing.T) {
	d := D.Gear[8]
	p := domain.CharacterCOS{Codename: "COS_P_WOLF_090", Level: 90}
	for i, g := range d.Items {
		p.Training.Gear[i] = domain.PetGear{RefObjID: g.ID}
	}
	m := ModifiersFor(&p)
	if m.GearStats.MaxHPPercent != 900 {
		t.Fatalf("full D9 maximum-HP bonus = %v, want 900%%", m.GearStats.MaxHPPercent)
	}
	if m.AttackPct != 0 || m.DefensePct != 0 || m.CooldownPct != 0 || m.GearStats.PhysicalAttack <= 0 || m.GearStats.MagicalAttack <= 0 || m.GearStats.PhysicalDefense <= 0 || m.GearStats.MagicalDefense <= 0 || m.GearStats.HitRatio <= 0 || m.GearStats.ParryRatio <= 0 {
		t.Fatalf("flat equipment: %+v", m)
	}
	p.Training.Gear = [3]domain.PetGear{}
	if ModifiersFor(&p).GearStats != (GearStats{}) {
		t.Fatal("unequipped stats remained")
	}
	d.MagicChance, d.RareChance, d.RollFloor = 1, 0, 0
	a := D.Affixes[0]
	a.Min, a.Max, a.Weight, a.RollPower = 1, 50, 1, 3
	a.Status, a.MinDegree, a.Slots = "Implemented", 1, []string{d.Items[0].Slot}
	for _, tc := range []struct {
		draw float64
		want uint32
	}{{0, 1}, {0.5, 7}, {0.999999, 50}} {
		r := fixedRoller{0.5, 0.999999, 0, tc.draw}
		got := RollGearWithAffixes(d, 0, 0, &r, []AffixDef{a})
		if len(got) != 1 || uint32(got[0]>>32) != tc.want {
			t.Fatalf("biased draw %v: %v", tc.draw, got)
		}
	}
}

/*
================
TestStackedPetCooldownCap
================
*/
func TestStackedPetCooldownCap(t *testing.T) {
	p := domain.CharacterCOS{Codename: "COS_P_WOLF_090", Level: 90}
	d := D.Gear[8]
	var code uint16
	for _, a := range D.Affixes {
		if a.Group == "Cooldown" {
			code = a.Code
			break
		}
	}
	if code == 0 {
		t.Fatal("missing cooldown affix")
	}
	for i, g := range d.Items {
		p.Training.Gear[i] = domain.PetGear{RefObjID: g.ID, Options: [6]uint64{EncodeAffix(code, 50)}}
	}
	m := ModifiersFor(&p)
	if m.CooldownPct != 80 {
		t.Fatalf("stacked cooldown reduction = %v, want 80", m.CooldownPct)
	}
	s := skillNamed(t, "Rending Bite")
	StartRow(&p.Training, s.Row, s, 1, 1000, m)
	if got, want := p.Training.Rows[s.Row].ReadyAtMs, int64(1000)+int64(s.CooldownAt(1)*200); got != want {
		t.Fatalf("capped cooldown ends at %d, want %d", got, want)
	}
	p.Training.Gear[1], p.Training.Gear[2] = domain.PetGear{}, domain.PetGear{}
	if got := ModifiersFor(&p).CooldownPct; got != 50 {
		t.Fatalf("single item = %v, want 50", got)
	}
	p.Training.Gear[0] = domain.PetGear{}
	if got := ModifiersFor(&p).CooldownPct; got != 0 {
		t.Fatalf("unequipped = %v, want 0", got)
	}
}
