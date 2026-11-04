/*
===========================================================================

skilluireplace_test.go - every press row publishes its replacement descriptor

The client replays statuseffect.DecideReplacement from SkillUiReplace and
never derives it; a row the replay cannot decide says so.

===========================================================================
*/
package enterworld

import (
	"testing"

	"opensro.online/server/internal/game/item/statuseffect"
	"opensro.online/server/internal/testsupport/clientdata"
	"opensro.online/server/internal/testsupport/gamedatatest"
)

/*
================
TestSkillUiReplaceMarksWhatTheClientCannotDecide
================
*/
func TestSkillUiReplaceMarksWhatTheClientCannotDecide(t *testing.T) {
	if skillUiReplace(SkillRow{}) != nil {
		t.Fatal("a row without a descriptor published one")
	}
	plain := statuseffect.ReplacementDescriptor{Category: 3, Group: 336, Rank: 2, PackedStates: 13, Ovl2Present: true, Ovl2: 64, Cbuf: true}
	got := skillUiReplace(SkillRow{ReplacementPinned: true, Replacement: plain})
	if *got != (SkillUiReplace{Category: 3, Group: 336, Rank: 2, States: 13, Ovl2: 64, Cbuf: true}) {
		t.Fatalf("published %+v", *got)
	}
	for name, flag := range map[string]func(*statuseffect.ReplacementDescriptor){
		"dttp": func(d *statuseffect.ReplacementDescriptor) { d.DttpPresent = true },
		"efr2": func(d *statuseffect.ReplacementDescriptor) { d.Efr2 = true },
		"lnks": func(d *statuseffect.ReplacementDescriptor) { d.Lnks = true },
		"lks2": func(d *statuseffect.ReplacementDescriptor) { d.Lks2 = true },
		"msch": func(d *statuseffect.ReplacementDescriptor) { d.MschPresent = true },
	} {
		d := plain
		flag(&d)
		if !skillUiReplace(SkillRow{ReplacementPinned: true, Replacement: d}).Undecided {
			t.Errorf("%s is not marked undecided", name)
		}
	}
}

/*
================
TestConcentrationTiersPublishTheirDescriptors

The report's case on the shipped data: Concentration 2nd and 3rd publish
different groups and the same packed state, which is why the server refuses
one over the other.
================
*/
func TestConcentrationTiersPublishTheirDescriptors(t *testing.T) {
	clientdata.RequireGameData(t)
	rows := map[uint32]*SkillUiReplace{}
	for _, row := range NewTextdataSkills(gamedatatest.TextdataDir(t)).SpawnSkillRows() {
		if row.UI != nil && row.UI.Admit != nil {
			rows[row.ID] = row.UI.Admit.Replace
		}
	}
	second, third := rows[1342], rows[3410]
	if second == nil || third == nil {
		t.Fatal("Concentration rows publish no descriptor")
	}
	if second.Group == third.Group || second.States != 13 || third.States != 13 || second.Undecided || third.Undecided {
		t.Fatalf("Concentration 2nd %+v, 3rd %+v", *second, *third)
	}
}
