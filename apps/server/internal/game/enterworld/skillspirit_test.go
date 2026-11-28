/*
===========================================================================

skillspirit_test.go - the Devil's Spirit skills' programs compile

The Spirit's skills (port-only, SRO_NASRUN) are two shapes v1.150 never
authored: a self buff carrying dru and an hst3 movement lane, and an
instant buff carrying msid alone. The rows below are synthetic, built to
those shapes with the column layout the later client uses.

===========================================================================
*/

package enterworld

import (
	"strconv"
	"strings"
	"testing"
)

/*
================
spiritSkillRow

One 118-column skill row: id, activity (column 8), cooldown, and the
encoded tail from column 69.
================
*/
func spiritSkillRow(id uint32, activity string, tail ...uint32) string {
	fields := make([]string, 118)
	for i := range fields {
		fields[i] = "0"
	}
	fields[0], fields[1], fields[2] = "1", strconv.FormatUint(uint64(id), 10), "880"
	fields[3], fields[4], fields[5] = "SKILL_TEST_SPIRIT_"+strconv.FormatUint(uint64(id), 10), "x", "SKILL_TEST_SPIRIT"
	fields[7], fields[8], fields[14], fields[34], fields[47] = "1", activity, "1800000", "1", "3"
	for _, col := range []int{50, 51, 57, 58, 59} {
		fields[col] = "255"
	}
	if activity == "2" {
		fields[11], fields[12], fields[13], fields[18] = "166", "1", "800", "1795162206"
	} else {
		fields[14], fields[18], fields[60] = "1200000", "-872415232", "1"
	}
	fields[61], fields[62], fields[63], fields[64], fields[65] = "x.ddj", "SN_X", "xxx", "SN_X_TT_DESC", "xxx"
	fields[68] = "3"
	for i, word := range tail {
		fields[69+i] = strconv.FormatUint(uint64(word), 10)
	}
	return strings.Join(fields, "\t")
}

/*
================
TestSpiritDevilSkillIsASelfSpeedAndDamageBuff

dura, dru {20, 20}, hst3 10 and reqi 14 1 on a cast self buff: the
program pins, and its movement lane is supported, so the installer
writes both. Before, the movement rule took a party area only and
refused the row.
================
*/
func TestSpiritDevilSkillIsASelfSpeedAndDamageBuff(t *testing.T) {
	skills := loadDuplicateRows(t, spiritSkillRow(31142, "2",
		tagDura, 600000, tagTimedDamageRate, 20, 20, tagTimedIndependent, 10, tagReqi, 14, 1))
	row, ok := skills.SkillByID(31142)
	if !ok || !row.TimedEffect.Pinned || row.TimedEffect.Targeted || row.TimedEffect.Area.Present {
		t.Fatalf("Devil skill: %+v", row.TimedEffect)
	}
	if !row.MovementModifier.Supported || row.MovementModifier.Percent != 10 || !row.BuffModifiers.Dru {
		t.Fatalf("Devil skill lanes: movement %+v dru %v", row.MovementModifier, row.BuffModifiers.Dru)
	}
	// The self lane takes no other stat block beside it: a self hst3
	// with a hp block is still refused, as before.
	skills = loadDuplicateRows(t, spiritSkillRow(31143, "2",
		tagDura, 600000, tagTimedIndependent, 10, tagTimedMaxHP, 5, 5))
	if row, _ := skills.SkillByID(31143); row.TimedEffect.Pinned {
		t.Fatalf("a self lane beside hpi pinned: %+v", row.TimedEffect)
	}
}

/*
================
TestSpiritIgnoreSkillCarriesMsid

The instant Monster Defence Ignore row (dura, msid, reqi) pins with its
percent; an instant row with anything else still takes no buff program.
================
*/
func TestSpiritIgnoreSkillCarriesMsid(t *testing.T) {
	for _, tc := range []struct {
		id, percent uint32
	}{
		{31150, 1},
		{31151, 2},
	} {
		skills := loadDuplicateRows(t, spiritSkillRow(tc.id, "1", tagDura, 300000, tagTimedIgnoreDefense, tc.percent, tagReqi, 14, 1))
		row, ok := skills.SkillByID(tc.id)
		if !ok || !row.ReplacementPinned || row.Replacement.PackedStates != 0xCC000000 || row.CoolTimeGroup != 0xCC {
			t.Fatalf("ignore skill %d replacement/group = %+v group %#x (refusal %q)",
				tc.id, row.Replacement, row.CoolTimeGroup, row.ReplacementRefusal)
		}
		if !row.TimedEffect.Pinned || row.TimedEffect.IgnoreDefense != tc.percent {
			t.Fatalf("ignore skill %d timed = %+v, want IgnoreDefense=%d", tc.id, row.TimedEffect, tc.percent)
		}
	}
	skills := loadDuplicateRows(t, spiritSkillRow(31152, "1", tagDura, 300000, tagTimedIgnoreDefense, 2, tagTimedIndependent, 10))
	if row, _ := skills.SkillByID(31152); row.TimedEffect.Pinned && !row.TimedEffect.ItemProgram {
		t.Fatalf("an instant msid row with a lane took the buff program: %+v", row.TimedEffect)
	}
	// msid above a whole chance refuses.
	skills = loadDuplicateRows(t, spiritSkillRow(31153, "1", tagDura, 300000, tagTimedIgnoreDefense, 101, tagReqi, 14, 1))
	if row, _ := skills.SkillByID(31153); row.TimedEffect.Pinned {
		t.Fatalf("msid 101 pinned: %+v", row.TimedEffect)
	}
}
