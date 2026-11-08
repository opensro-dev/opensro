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
				for kind, pair := range m.Procs {
					for trigger, proc := range pair {
						if proc.Chance == 0 {
							continue
						}
						found++
						lo, hi := D.Gear[degree-1].ProcDamage, D.Gear[degree-1].ProcDamage
						if kind <= ProcFire {
							lo, hi = D.Gear[degree-1].ProcRanges[kind][0], D.Gear[degree-1].ProcRanges[kind][1]
						}
						if (trigger == 1) != strings.HasSuffix(a.Group, "Taken") || proc.Chance != 5 || proc.Damage != lo || proc.MaxDamage != hi {
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

/*
================
TestGearProcDegreeRangesAndSeals
================
*/
func TestGearProcDegreeRangesAndSeals(t *testing.T) {
	for kind, want := range [3][2]uint32{{1500, 2000}, {1000, 1500}, {1000, 3000}} {
		if D.Gear[8].ProcRanges[kind] != want || D.Gear[0].ProcRanges[kind] != [2]uint32{100, 100} {
			t.Fatal("D1/D9 proc endpoints", kind)
		}
		for i := 1; i < len(D.Gear); i++ {
			for end := range 2 {
				if D.Gear[i].ProcRanges[kind][end] <= D.Gear[i-1].ProcRanges[kind][end] {
					t.Fatal("proc range did not increase with degree")
				}
			}
		}
		sealed, _, ok := GearByID(D.Gear[8].Items[0].ID + 30)
		if !ok || sealed.ProcRanges[kind][0] != want[0]*5/2 || sealed.ProcRanges[kind][1] != want[1]*5/2 || D.Gear[8].ProcRanges[kind] != want {
			t.Fatal("seal scaling mutated or lost the proc range")
		}
	}
}
