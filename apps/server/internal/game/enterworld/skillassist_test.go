/*
===========================================================================

skillassist_test.go - which skills send the caster's attack pets in

===========================================================================
*/

package enterworld

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"opensro.online/server/internal/testsupport/clientdata"
	"opensro.online/server/internal/testsupport/gamedatatest"
)

/*
================
TestSkillAniSetAssistsReadsColumn24

Both skillaniset sections, a skillaniset2 row's service column (0 skips the
row), and the flag in column 24 (nonzero is set).
================
*/
func TestSkillAniSetAssistsReadsColumn24(t *testing.T) {
	dir := t.TempDir()
	row := func(prefix, name, flag string) string {
		cols := make([]string, 26)
		cols[1], cols[24] = name, flag
		return prefix + strings.Join(cols, "\t")
	}
	text := strings.Join([]string{
		"#section\tskillaniset",
		row("", "ATTACK", "1"),
		row("", "BUFF", "0"),
		"#section\tskillaniset2",
		row("1\t", "LATER_ATTACK", "1"),
		row("0\t", "SKIPPED", "1"),
		"#section\tskilleffectset",
		row("", "NOT_AN_ANISET", "1"),
	}, "\n")
	if err := os.WriteFile(filepath.Join(dir, "skilleffect.txt"), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	got := skillAniSetAssists(dir)
	want := map[string]bool{"ATTACK": true, "BUFF": false, "LATER_ATTACK": true}
	if len(got) != len(want) {
		t.Fatalf("assist sets = %v, want %v", got, want)
	}
	for name, assist := range want {
		if got[name] != assist {
			t.Fatalf("%s = %v, want %v (all %v)", name, got[name], assist, got)
		}
	}
}

/*
================
TestSkillAssistFollowsTheBasicOriginal

917240 takes a skill's record from its Basic_Original, ahead of its own,
when the original has one; otherwise the row's animation set, else its
codename.
================
*/
func TestSkillAssistFollowsTheBasicOriginal(t *testing.T) {
	sets := map[string]bool{"ATTACK": true, "BUFF": false, "PET_ATTACK_ROW": true}
	skills := &TextdataSkills{}
	for _, id := range []uint32{1, 2, 3, 4, 5} {
		skills.rows.set(id, SkillRow{ID: id})
	}
	sources := map[uint32]skillRecordSource{
		1: {key: skillRecordKey("ATTACK", "SKILL_A", sets)},
		2: {key: skillRecordKey("BUFF", "SKILL_B", sets)},
		// Its own record would not assist; its original's does.
		3: {key: skillRecordKey("BUFF", "SKILL_C", sets), original: 1},
		// An original without a record of its own leaves the row's record.
		4: {key: skillRecordKey("ATTACK", "SKILL_D", sets), original: 99},
		// No animation set row: the codename names it (a pet's levelled row).
		5: {key: skillRecordKey("", "PET_ATTACK_ROW", sets)},
	}
	skills.applySkillAssists(sources, sets)
	for id, want := range map[uint32]bool{1: true, 2: false, 3: true, 4: true, 5: true} {
		if got := skills.rows.get(id).AssistsCompanions; got != want {
			t.Fatalf("skill %d AssistsCompanions = %v, want %v", id, got, want)
		}
	}
}

/*
================
TestShippedBasicAttacksAssistAndShieldsDoNot

On the shipped data the sword and bow basic attacks send the pets in and a
shield buff does not, and the flag reaches the catalogue the client loads.
================
*/
func TestShippedBasicAttacksAssistAndShieldsDoNot(t *testing.T) {
	clientdata.RequireGameData(t)
	skills := NewTextdataSkills(gamedatatest.TextdataDir(t))
	byID := map[uint32]bool{}
	for _, row := range skills.SpawnSkillRows() {
		byID[row.ID] = row.AssistsCompanions
	}
	for codename, want := range map[string]bool{
		"SKILL_CH_SWORD_BASE_01":     true,
		"SKILL_CH_BOW_BASE_01":       true,
		"SKILL_CH_SWORD_SHIELD_A_01": false,
	} {
		row, ok := skills.SkillByCodename(codename)
		if !ok {
			t.Fatalf("%s missing from the shipped skilldata", codename)
		}
		if row.AssistsCompanions != want || byID[row.ID] != want {
			t.Fatalf("%s assists = %v (catalogue %v), want %v", codename, row.AssistsCompanions, byID[row.ID], want)
		}
	}
}
