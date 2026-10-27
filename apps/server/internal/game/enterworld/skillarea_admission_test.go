/*
===========================================================================

skillarea_admission_test.go - area skill admission

===========================================================================
*/

package enterworld

import (
	"strconv"
	"testing"
)

func TestAreaAdmissionRetainsShapeAndRejectsUnimplementedEffects(t *testing.T) {
	for _, shape := range []int{0, 1, 2, 3, 4, 5, 6, 7, 255} {
		for _, tail := range []int64{0, 0x6b62, 0x6b64, 0x667a, 0x7073} {
			fields := make([]string, 118)
			for i := range fields {
				fields[i] = "0"
			}
			fields[0] = "1"
			values := []int64{skillAttackTag, 5, 100, 10, 20, 100, 0x656672, 1, int64(shape), 30, 5, 35, 24, tail}
			for i, value := range values {
				fields[69+i] = strconv.FormatInt(value, 10)
			}
			if tail == 0x6b62 {
				fields[83], fields[84] = "35", "50"
			}
			row := SkillRow{CombatPinned: true, TimingPinned: true, ActionRangePinned: true, TargetRequired: true, Attack: SkillAttack{ImpactCount: 1}}
			parseSkillOffense(fields, &row)
			// fz/ps are status blocks rolled by the server and executed by the
			// abnormal engine; 6B64 remains an unimplemented instruction.
			want := tail != 0x6b64 && (shape >= 1 && shape <= 4 || shape == 6)
			if row.OffensiveStagePinned != want || row.DirectOffensePinned != want {
				t.Fatalf("shape %d tail %x admission %+v", shape, tail, row)
			}
			if want && tail == 0x6b62 && row.Knockback != (SkillKnockback{Present: true, Chance: 35, Distance: 50}) {
				t.Fatal("lost knockback arguments")
			}
			if want && row.OffensiveArea.Shape != uint8(shape) {
				t.Fatal("area kind lost during parsing")
			}
		}
	}
}
