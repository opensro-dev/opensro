/*
===========================================================================
gearseal_test.go - persistent identities and exactly-once seal scaling
===========================================================================
*/
package companion

import (
	"opensro.online/server/internal/domain"
	"testing"
)

/*
================
TestPetSealsScaleWhitesAndBlues
================
*/
func TestPetSealsScaleWhitesAndBlues(t *testing.T) {
	base := D.Gear[8]
	for tier := 1; tier <= 3; tier++ {
		for slot, item := range base.Items {
			id := item.ID + uint32(tier*10)
			d, gotSlot, ok := GearByID(id)
			if !ok || gotSlot != slot || d.Items[slot].ID != id {
				t.Fatal("seal identity")
			}
			byName, _, ok := GearByCodename(d.Items[slot].Codename)
			if !ok || byName.Items[slot].ID != id {
				t.Fatal("seal codename")
			}
			scale := 1 + float64(tier)*.5
			if d.Items[slot].BaseStats.PhysicalAttack != item.BaseStats.PhysicalAttack*scale {
				t.Fatal("white scaling")
			}
			options := []uint64{EncodeAffix(12, 100)}
			ScaleSealOptions(options, tier)
			p := domain.CharacterCOS{Codename: "COS_P_WOLF_090", Level: 90}
			p.Training.Gear[slot] = domain.PetGear{RefObjID: id}
			copy(p.Training.Gear[slot].Options[:], options)
			first, second := ModifiersFor(&p), ModifiersFor(&p)
			if first.FireAdd != 100*scale || first.FireAdd != second.FireAdd {
				t.Fatal("blue scaling", first.FireAdd)
			}
		}
	}
	if D.Gear[8].Items[0].ID != 30109 {
		t.Fatal("shared design mutated")
	}
	if _, _, ok := GearByID(30149); ok {
		t.Fatal("invalid seal admitted")
	}
}
