/*
===========================================================================

skillitemeffect_test.go - whole-program admission for timed item effects

Unknown, duplicate and non-self instructions cannot turn a consumable into
a partial buff. Synthetic rows exercise the compiler without licensed data.

===========================================================================
*/
package enterworld

import (
	"strconv"
	"testing"
)

/*
================
itemEffectFields
================
*/
func itemEffectFields(words ...uint32) ([]string, SkillRow) {
	fields := make([]string, 118)
	for i := range fields {
		fields[i] = "0"
	}
	fields[0], fields[8], fields[68] = "1", "1", "3"
	for i, word := range words {
		fields[69+i] = strconv.FormatUint(uint64(word), 10)
	}
	return fields, SkillRow{ReplacementPinned: true, EffectDurationMs: 10000}
}

/*
================
TestTimedItemCompilerAdmitsCompleteCompoundStats
================
*/
func TestTimedItemCompilerAdmitsCompleteCompoundStats(t *testing.T) {
	fields, row := itemEffectFields(itemEffectOwnerJob, itemEffectDuration, 10000,
		itemEffectHP, 500, 10, itemEffectMP, 200, 20, itemEffectHit, 0, 30,
		itemEffectEvasion, 0, 30, itemEffectDamage, 20, 20, itemEffectAbsorb, 15, 20,
		itemEffectSTR, 3, 0, itemEffectINT, 3, 0, itemEffectRecovery, 500, 200)
	effect, ok := compileTimedItemEffect(fields, row)
	if !ok || !effect.ItemProgram || !effect.Persistent || !effect.Pinned ||
		effect.HP.Flat != 500 || effect.HP.Percent != 10 || effect.MP.Flat != 200 ||
		effect.Accuracy.Percent != 30 || effect.Evasion.Percent != 30 || effect.Strength.Value != 3 || effect.Intellect.Value != 3 ||
		!effect.Recovery.Present || effect.Recovery.HP != 500 || effect.Recovery.MP != 200 {
		t.Fatalf("compound descriptor: %+v, admitted %v", effect, ok)
	}
}

/*
================
TestTimedItemCompilerRefusesIncompletePrograms
================
*/
func TestTimedItemCompilerRefusesIncompletePrograms(t *testing.T) {
	for _, reason := range []string{"unknown", "duplicate", "target", "cost", "no owner", "no duration", "duration mismatch", "no effect", "chain"} {
		t.Run(reason, func(t *testing.T) {
			words := []uint32{itemEffectOwnerJob, itemEffectDuration, 10000, itemEffectHP, 500, 0}
			switch reason {
			case "unknown":
				words = append(words, 0x73736f75)
			case "duplicate":
				words = append(words, itemEffectHP, 1, 0)
			case "no owner":
				words = words[1:]
			case "no duration":
				words = []uint32{itemEffectOwnerJob, itemEffectHP, 500, 0}
			case "no effect":
				words = words[:3]
			}
			fields, row := itemEffectFields(words...)
			switch reason {
			case "target":
				fields[22] = "1"
			case "cost":
				fields[53] = "1"
			case "duration mismatch":
				row.EffectDurationMs++
			case "chain":
				row.ChainNext = 10
			}
			if _, ok := compileTimedItemEffect(fields, row); ok {
				t.Fatal("incomplete program was admitted")
			}
		})
	}
}
