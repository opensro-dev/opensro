/*
===========================================================================

parameterindex_test.go - the shared parameter index (587630) walk

noteParameterIndex records the getv slots and admission blocks every skill
kind reads. These tests pin two properties of that walk: it skips zero
padding words instead of stopping at them, and it records the reach
addends (WIRU, CBRA) on rows whose primary tag is not att.

===========================================================================
*/

package enterworld

import (
	"strconv"
	"strings"
	"testing"

	"opensro.online/server/internal/testsupport/gamedatatest"
)

const (
	testTagGetv = 0x67657476
	testTagOdar = 0x6f646172
	testTagReqi = 0x72657169
	testTagSsou = 0x73736f75
	testKeyWIMD = 0x57494d44
	testKeyWIRU = 0x57495255
	testKeyCBRA = 0x43425241
)

/*
================
parameterIndexFields

A full-width row whose encoded tail is the given words, zero padded.
================
*/
func parameterIndexFields(tail ...uint32) []string {
	fields := make([]string, 118)
	for i := range fields {
		fields[i] = "0"
	}
	for i, w := range tail {
		fields[skilldataColEncodedTail+i] = strconv.FormatUint(uint64(w), 10)
	}
	return fields
}

/*
================
TestParameterIndexSkipsZeroWords

Earth Barrier's shape: "odar 4 30 0 getv WIMD". The zero after odar is
padding; the getv behind it must still be indexed, and ssou still ends the
walk so a getv after it stays unrecorded.
================
*/
func TestParameterIndexSkipsZeroWords(t *testing.T) {
	var row SkillRow
	noteParameterIndex(parameterIndexFields(testTagOdar, 4, 30, 0, testTagGetv, testKeyWIMD, 0, testTagReqi, 4, 1), &row)
	if !row.Attack.Parameters.Has(ParameterWizardMPDecrease) {
		t.Fatalf("getv WIMD behind a zero word not indexed: %#x", row.Attack.Parameters)
	}
	if !row.BuffModifiers.Odar || row.Reqi.Count != 1 || row.Reqi.Pairs[0] != (SkillReqiPair{Kind: 4, Value: 1}) {
		t.Fatalf("blocks around the padding lost: odar %v reqi %+v", row.BuffModifiers.Odar, row.Reqi)
	}

	var stopped SkillRow
	noteParameterIndex(parameterIndexFields(testTagSsou, testTagGetv, testKeyWIMD), &stopped)
	if stopped.Attack.Parameters != 0 {
		t.Fatalf("walk continued past ssou: %#x", stopped.Attack.Parameters)
	}
}

/*
================
TestParameterIndexRecordsReachAddends

4AE87E adds the caster's WIRU (+0x4E8) and CBRA (+0x50C) to a cast's reach
when the row asks for them, whatever its primary tag.
================
*/
func TestParameterIndexRecordsReachAddends(t *testing.T) {
	var row SkillRow
	noteParameterIndex(parameterIndexFields(testTagGetv, testKeyWIRU, testTagGetv, testKeyCBRA), &row)
	if !row.Attack.Parameters.Has(ParameterWizardRange) || !row.Attack.Parameters.Has(ParameterCrossbowRange) {
		t.Fatalf("reach addends not indexed: %#x", row.Attack.Parameters)
	}
}

/*
================
TestShippedParameterIndexRows

The shipped rows the two walk properties reach: Root and Mesh Root (status
casts with getv WIRU), Earth Barrier and Earth Fence (getv WIMD behind the
zero after odar), and every SKILL_CH_FIRE_SHIELD_ row (reqi 4 1 behind the
zero after bgra).
================
*/
func TestShippedParameterIndexRows(t *testing.T) {
	source := NewTextdataSkills(gamedatatest.TextdataDir(t))
	if err := source.Load(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		code string
		slot SkillParameter
	}{
		{"SKILL_EU_WIZARD_EARTHA_ABNORMAL_A_01", ParameterWizardRange},
		{"SKILL_EU_WIZARD_EARTHA_ABNORMAL_B_02", ParameterWizardRange},
		{"SKILL_EU_WIZARD_EARTHA_GUARD_A_01", ParameterWizardMPDecrease},
		{"SKILL_EU_WIZARD_EARTHA_GUARD_B_01", ParameterWizardMPDecrease},
	} {
		row, ok := source.SkillByCodename(tc.code)
		if !ok {
			t.Fatalf("missing %s", tc.code)
		}
		if !row.Attack.Parameters.Has(tc.slot) {
			t.Errorf("%s parameters %#x lack slot %d", tc.code, row.Attack.Parameters, tc.slot)
		}
	}

	shields := 0
	for _, row := range source.rows.values() {
		if !strings.HasPrefix(row.Codename, "SKILL_CH_FIRE_SHIELD_") {
			continue
		}
		shields++
		if !row.Reqi.Present || row.Reqi.Count != 1 || row.Reqi.Pairs[0] != (SkillReqiPair{Kind: 4, Value: 1}) {
			t.Errorf("%s reqi %+v, want one {4, 1}", row.Codename, row.Reqi)
		}
		// #508: bgra pins the timed self-effect (mask 63, all six elements).
		if !row.TimedEffect.Pinned || row.TimedEffect.Bgra.Mask != 63 || row.TimedEffect.Bgra.Value == 0 {
			t.Errorf("%s timed effect pinned %v bgra %+v", row.Codename, row.TimedEffect.Pinned, row.TimedEffect.Bgra)
		}
	}
	if shields != 19 {
		t.Fatalf("%d SKILL_CH_FIRE_SHIELD_ rows, want 19", shields)
	}

	// #508: Concentration's er block pins its timed self-effect.
	concentration := 0
	for _, row := range source.rows.values() {
		if !strings.HasPrefix(row.Codename, "SKILL_CH_LIGHTNING_JIPJUNG_") {
			continue
		}
		concentration++
		if !row.BuffModifiers.Er || !row.TimedEffect.Parry || !row.TimedEffect.Pinned {
			t.Errorf("%s er %v parry %v pinned %v", row.Codename, row.BuffModifiers.Er, row.TimedEffect.Parry, row.TimedEffect.Pinned)
		}
	}
	if concentration != 21 {
		t.Fatalf("%d SKILL_CH_LIGHTNING_JIPJUNG_ rows, want 21", concentration)
	}

}
