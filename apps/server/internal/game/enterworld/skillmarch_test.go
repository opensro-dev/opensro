/*
===========================================================================

skillmarch_test.go - complete admission of party movement programs

Malformed and mixed unsupported programs must never enable movement merely
because their parameter stream contains a haste word.

===========================================================================
*/

package enterworld

import (
	"strconv"
	"testing"
)

/*
================
marchProgramFields

Build the native envelope with a supplied instruction stream, without relying
on installed retail assets for the malformed-program tests.
================
*/
func marchProgramFields(tail []uint32) []string {
	fields := make([]string, 118)
	for i := range fields {
		fields[i] = "0"
	}
	fields[0], fields[8], fields[68] = "1", "2", "3"
	fields[50], fields[51] = "14", "255"
	for i, value := range tail {
		fields[skilldataColEncodedTail+i] = strconv.FormatUint(uint64(value), 10)
	}
	return fields
}

/*
================
TestMarchProgramAdmissionIsAtomic

Exercise whole-program failure independently of names and IDs. Valid haste
variants share the timed-area owner; malformed envelopes stay unsupported.
================
*/
func TestMarchProgramAdmissionIsAtomic(t *testing.T) {
	const duration = 600000
	for _, tc := range []struct {
		name  string
		tail  []uint32
		valid bool
	}{
		{"march", []uint32{tagTimedHaste, 20, tagGetv, parameterBardMP, tagGetv, parameterMusicArea}, true},
		{"override", []uint32{tagTimedOverride, 40}, true},
		{"independent", []uint32{tagTimedIndependent, 60}, true},
		{"zero haste", []uint32{tagTimedHaste, 0}, false},
		{"duplicate haste", []uint32{tagTimedHaste, 20, tagTimedHaste, 30}, false},
		{"unknown operation", []uint32{tagTimedHaste, 20, 0x61626364}, false},
		{"unsupported parameter", []uint32{tagTimedHaste, 20, tagGetv, 0x61626364}, false},
		{"persistent party job", []uint32{tagTimedHaste, 20, tagCbuf}, false},
		{"missing modifier", []uint32{tagGetv, parameterBardMP}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tail := append([]uint32{tagDura, duration, tagEfr, 1, 1, 300, 8, 0, SelectCaster | SelectParty}, tc.tail...)
			fields := marchProgramFields(tail)
			row := SkillRow{
				Consumption: SkillConsumption{Pinned: true}, TimingPinned: true,
				ActionCastingTimePinned: true, ActionDurationPinned: true, ReplacementPinned: true,
				EffectDurationMs: duration, MovementModifier: encodedMovementModifier(fields),
			}
			parseSkillTimedEffect(fields, &row)
			if row.TimedEffect.Pinned != tc.valid || row.MovementModifier.Supported != tc.valid {
				t.Fatalf("admission %v, movement %v; want %v", row.TimedEffect.Pinned, row.MovementModifier.Supported, tc.valid)
			}
			if row.InstantSelfEffectPinned || row.TimedJobExecutable() {
				t.Fatal("party movement leaked into another producer")
			}
		})
	}
}
