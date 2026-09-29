/*
===========================================================================

skillfrenzy_test.go - complete-program boundaries for Warrior Frenzy

Malformed tails must not turn into partial stat buffs or threat actions.
Fixtures deliberately avoid installed game assets so these boundaries run in CI.

===========================================================================
*/

package enterworld

import "testing"

/*
================
TestFrenzyAttributeAdmissionIsComplete

Deferred pmdg modes, mismatched lifetimes and unknown operations cannot be
silently dropped while admitting the health gain.
================
*/
func TestFrenzyAttributeAdmissionIsComplete(t *testing.T) {
	for _, mode := range []string{"valid", "deferred", "duration", "duplicate", "unknown", "targeted", "persistent", "area"} {
		t.Run(mode, func(t *testing.T) {
			tail := []uint32{tagDura, 900000, tagTimedMaxHP, 583, 0, tagTimedThreat, 0, 300, tagTimedDamagePenalty, 900000, 35, 35, 2}
			switch mode {
			case "deferred":
				tail[12] = 1
			case "duration":
				tail[9] = 5000
			case "duplicate":
				tail = append(tail, tagTimedMaxHP, 100, 0)
			case "unknown":
				tail = append(tail, 0xffffffff)
			case "persistent":
				tail = append(tail, tagCbuf)
			case "area":
				tail = append(tail, tagEfr, 1, 1, 100, 3, 0, 4)
			}
			fields := marchProgramFields(tail)
			if mode == "targeted" {
				fields[21], fields[22], fields[23], fields[27], fields[28] = "100", "1", "1", "1", "1"
			}
			row := SkillRow{Consumption: SkillConsumption{Pinned: true}, ActionCastingTimePinned: true,
				ActionDurationPinned: true, TimingPinned: true, ReplacementPinned: true, EffectDurationMs: 900000}
			parseSkillTimedEffect(fields, &row)
			if row.TimedEffect.Pinned != (mode == "valid") {
				t.Fatalf("admission=%v", row.TimedEffect.Pinned)
			}
			if mode == "valid" && (!row.TimedEffect.Attributes.MaxHP || !row.TimedEffect.Attributes.DamagePenalty) {
				t.Fatal("partial buff")
			}
		})
	}
}

/*
================
TestFrenzyTauntAdmissionIsComplete

The monster-only selector, required target envelope and physical weapon term
are part of the producer contract, not optional hints.
================
*/
func TestFrenzyTauntAdmissionIsComplete(t *testing.T) {
	for _, mode := range []string{"targeted", "untargeted", "wrong-selector", "missing-weapon", "duplicate", "unknown", "target-mismatch"} {
		t.Run(mode, func(t *testing.T) {
			tail := []uint32{tagEfr, 1, 2, 50, 3, 0, 16, tagThreat, 1016, 0, tagPhysicalWeaponThreat, 2000}
			if mode == "untargeted" {
				tail[2] = 1
			}
			if mode == "wrong-selector" {
				tail[6] = 24
			}
			if mode == "missing-weapon" {
				tail = tail[:10]
			}
			if mode == "duplicate" {
				tail = append(tail, tagThreat, 100, 0)
			}
			if mode == "unknown" {
				tail = append(tail, 0xffffffff)
			}
			fields := marchProgramFields(tail)
			fields[68] = "0"
			targeted := mode != "untargeted"
			if targeted {
				fields[21], fields[22], fields[23], fields[29], fields[30] = "150", "1", "1", "1", "1"
			}
			row := SkillRow{TimingPinned: true, Consumption: SkillConsumption{Pinned: true}, ActionRangePinned: true,
				ActionRange: 150, TargetRequired: targeted, ActionDurationMs: 500}
			if mode == "target-mismatch" {
				row.TargetRequired = false
			}
			got := compileSkillTaunt(fields, row)
			want := mode == "targeted" || mode == "untargeted"
			if got.Only != want {
				t.Fatalf("admission=%v, want %v", got.Only, want)
			}
		})
	}
}
