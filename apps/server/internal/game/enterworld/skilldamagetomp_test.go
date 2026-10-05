/*
===========================================================================
skilldamagetomp_test.go - complete dgmp timed-program admission
===========================================================================
*/
package enterworld

import "testing"

/*
================
TestDamageToMPAdmission
================
*/
func TestDamageToMPAdmission(t *testing.T) {
	for _, mode := range []string{"valid", "zero", "max", "overflow", "duplicate", "unknown", "missing-duration"} {
		t.Run(mode, func(t *testing.T) {
			tail := []uint32{tagDura, 120000, tagTimedDamageToMP, 20}
			switch mode {
			case "zero":
				tail[3] = 0
			case "max":
				tail[3] = 100
			case "overflow":
				tail[3] = 101
			case "duplicate":
				tail = append(tail, tagTimedDamageToMP, 30)
			case "unknown":
				tail = append(tail, 0xffffffff)
			case "missing-duration":
				tail = tail[2:]
			}
			fields := marchProgramFields(tail)
			row := SkillRow{Consumption: SkillConsumption{Pinned: true}, ActionCastingTimePinned: true, ActionDurationPinned: true, TimingPinned: true, ReplacementPinned: true, EffectDurationMs: 120000}
			parseSkillTimedEffect(fields, &row)
			want := mode == "valid" || mode == "zero" || mode == "max"
			if row.TimedEffect.Pinned != want {
				t.Fatalf("admitted %v", row.TimedEffect.Pinned)
			}
			if want && (!row.TimedEffect.DamageToMP || row.TimedEffect.DamageToMPPercent != tail[3]) {
				t.Fatal("lost dgmp")
			}
		})
	}
}
