/*
===========================================================================
gearproc_test.go - proc blues preserve direction, slot and degree power
===========================================================================
*/
package companion

import (
	"opensro.online/server/internal/domain"
	"strings"
	"testing"
)

/*
================
TestGearProcPairsAndPower
================
*/
func TestGearProcPairsAndPower(t *testing.T) {
	groups := map[string]bool{}
	for _, a := range D.Affixes {
		if !strings.HasPrefix(a.Group, "Proc") || a.Status != "Implemented" {
			continue
		}
		groups[a.Group] = true
		for _, degree := range []int{3, 9} {
			for slot, g := range D.Gear[degree-1].Items {
				if !hasSlot(a, g.Slot) {
					continue
				}
				p := domain.CharacterCOS{Codename: "COS_P_WOLF_090", Level: 90}
				p.Training.Gear[slot] = domain.PetGear{RefObjID: g.ID}
				p.Training.Gear[slot].Options[0] = EncodeAffix(a.Code, 5)
				m := ModifiersFor(&p)
				found := 0
				for _, pair := range m.Procs {
					for trigger, proc := range pair {
						if proc.Chance == 0 {
							continue
						}
						found++
						if (trigger == 1) != strings.HasSuffix(a.Group, "Taken") || proc.Chance != 5 || proc.Damage != D.Gear[degree-1].ProcDamage {
							t.Fatal("proc direction/power", a.ID, proc)
						}
					}
				}
				if found != 1 {
					t.Fatal("blue did not map to exactly one proc", a.ID)
				}
			}
		}
	}
	for _, group := range []string{"ProcChain", "ProcBlizzard", "ProcFire", "ProcPoison", "ProcMend"} {
		if !groups[group] || !groups[group+"Taken"] {
			t.Fatal("missing paired proc", group)
		}
	}
	for i, g := range D.Gear {
		if g.ProcDamage == 0 || i > 0 && g.ProcDamage <= D.Gear[i-1].ProcDamage {
			t.Fatal("degree power must increase", g.Degree)
		}
	}
}
