/*
===========================================================================

skillabnormal_efr_test.go - only the kind-1 efr block is the action area

===========================================================================
*/

package enterworld

import (
	"strconv"
	"testing"
)

// efr kinds fill separate RefSkill slots; only kind 1 (+0x28C) is the action
// area instant-skill dispatches, so a kind-2 block never becomes it.
func TestEffectAreaIsKindOneSlotOnly(t *testing.T) {
	parse := func(words ...uint32) []string {
		fields := make([]string, skilldataColEncodedTail)
		for _, w := range words {
			fields = append(fields, strconv.FormatUint(uint64(w), 10))
		}
		return append(fields, "0")
	}
	area := encodedAbnormalParams(parse(0x656672, 1, 1, 300, 8, 0, 5)).EffectArea
	if !area.Present || area.Shape != 1 || area.Radius != 300 || area.Select != 5 {
		t.Fatalf("kind 1 area %+v", area)
	}
	if area := encodedAbnormalParams(parse(0x656672, 2, 1, 300, 8, 0, 5)).EffectArea; area.Present {
		t.Fatalf("kind 2 became the action area: %+v", area)
	}
	// A later kind-2 block leaves the kind-1 slot in place.
	area = encodedAbnormalParams(parse(0x656672, 1, 1, 300, 8, 0, 5, 0x656672, 2, 1, 50, 1, 0, 0)).EffectArea
	if !area.Present || area.Radius != 300 {
		t.Fatalf("kind 2 overwrote the kind 1 slot: %+v", area)
	}
}
