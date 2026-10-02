/*
===========================================================================

buffmodifier_test.go - dru, odar and hr parameter writes

===========================================================================
*/

package action

import (
	"reflect"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/paramkeeper"
)

// 596004 tests two bits per parameter: 4+1 0xAE, 4+2 0xAF, 8+1 0xB0,
// 8+2 0xB1. Bits here are post-parse (588C3B / 588C52 already applied).
func TestOdarWritesFollowBitPairs(t *testing.T) {
	for _, tc := range []struct {
		bits uint32
		want []uint16
	}{
		{0xf, []uint16{0xae, 0xaf, 0xb0, 0xb1}}, // shipped kind 15
		{0x7, []uint16{0xae, 0xaf}},             // kind 4 after |= 1|2
		{0xb, []uint16{0xb0, 0xb1}},             // kind 8 after |= 1|2
		{0xd, []uint16{0xae, 0xb0}},             // kind 1 after |= 4|8
		{0x6, []uint16{0xaf}},                   // shipped kind 6, no fix-up
	} {
		var got []uint16
		for _, w := range buffModifierWrites(enterworld.SkillBuffModifiers{Odar: true, OdarBits: tc.bits, OdarWord: 20}, false) {
			if w.Channel != paramkeeper.PercentProduct || w.Value != -20 {
				t.Fatalf("bits %#x write %+v", tc.bits, w)
			}
			got = append(got, w.Parameter)
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("bits %#x: %x want %x", tc.bits, got, tc.want)
		}
	}
}

// Both blocks read their words unsigned (fild + 2^32).
func TestBuffModifierWordsAreUnsigned(t *testing.T) {
	writes := buffModifierWrites(enterworld.SkillBuffModifiers{
		Dru: true, DruWords: [2]uint32{0xffffffff, 0},
		Odar: true, OdarBits: 0x7, OdarWord: 0x80000000,
	}, false)
	if writes[0].Value != float32(4294967295) || writes[4].Value != -float32(2147483648) {
		t.Fatalf("writes %+v", writes)
	}
}

/*
================
TestHrWritesHitRatePercentThenFlat

594AC0 files hr on parameter 11: word 1 in the percent-sum channel, then
word 0 flat, both unsigned. An item program that already files the block
owns it, so the skill-side copy is dropped.
================
*/
func TestHrWritesHitRatePercentThenFlat(t *testing.T) {
	hr := enterworld.SkillBuffModifiers{Hr: true, HrFlat: 0xffffffff, HrRate: 7}
	want := []paramkeeper.Write{
		{Parameter: itemParamAccuracy, Channel: paramkeeper.PercentSum, Value: 7},
		{Parameter: itemParamAccuracy, Channel: paramkeeper.Flat, Value: float32(4294967295)},
	}
	if got := buffModifierWrites(hr, false); !reflect.DeepEqual(got, want) {
		t.Fatalf("hr writes %+v, want %+v", got, want)
	}
	if got := buffModifierWrites(hr, true); len(got) != 0 {
		t.Fatalf("item-owned hr filed again: %+v", got)
	}
}
