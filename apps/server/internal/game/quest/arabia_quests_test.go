/*
===========================================================================

arabia_quests_test.go - the Baghdad quest specs (QNO_AR_*)

The table's shape needs no game data; the loadability run below builds a
synthetic catalog with the rows the Arabia import brings (they resolve
only there - an absent catalog admits none of the specs).

===========================================================================
*/
package quest

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"opensro.online/server/internal/testsupport/clientdata"
)

// arabiaWanted is the authored set, in table order: 9 talks, 20 hunts.
var arabiaWanted = []string{
	"QNO_AR_005", "QNO_AR_008", "QNO_AR_009", "QNO_AR_020", "QNO_AR_021",
	"QNO_AR_028", "QNO_AR_029", "QNO_AR_030", "QNO_AR_031", "QNO_AR_040",
	"QNO_AR_041", "QNO_AR_042", "QNO_AR_043", "QNO_AR_045", "QNO_AR_047",
	"QNO_AR_049", "QNO_AR_052", "QNO_AR_053", "QNO_AR_054", "QNO_AR_060",
	"QNO_AR_092", "QNO_AR_093", "QNO_AR_094", "QNO_AR_096", "QNO_AR_097",
	"QNO_AR_098", "QNO_AR_099", "QNO_AR_102", "QNO_AR_103",
}

/*
================
TestArabiaTableIsTheEvidenceSet
================
*/
func TestArabiaTableIsTheEvidenceSet(t *testing.T) {
	specs := arabiaSpecs()
	if len(specs) != len(arabiaWanted) {
		t.Fatalf("%d specs, want %d", len(specs), len(arabiaWanted))
	}
	known := map[string]QuestSpec{}
	for i, spec := range specs {
		if spec.Codename != arabiaWanted[i] {
			t.Fatalf("spec %d is %s, want %s (table order is source order)", i, spec.Codename, arabiaWanted[i])
		}
		if _, dup := known[spec.Codename]; dup {
			t.Fatalf("%s is authored twice", spec.Codename)
		}
		known[spec.Codename] = spec
		talk := "SN_TALK_" + spec.Codename + "_"
		if spec.KindByte != 1 || spec.MaxCompletions < 1 || spec.MaxCompletions > 5 || spec.Repeatable {
			t.Errorf("%s kind/completions %+v", spec.Codename, spec)
		}
		if spec.StartNpcCodename == "" || spec.EndNpcCodename == "" ||
			spec.OfferPromptSymbol != talk+"01" || spec.AcceptResponseSymbol != talk+"02" ||
			spec.DenyResponseSymbol != talk+"03" || spec.NotAchievedSymbol != talk+"04" ||
			spec.CompletePromptSymbol != talk+"05" {
			t.Errorf("%s dialog contract %+v", spec.Codename, spec)
		}
		if spec.RewardExp <= 0 || spec.RewardGold <= 0 || spec.RewardSkillExp <= 0 || len(spec.RewardItems) != 0 {
			t.Errorf("%s rewards %+v", spec.Codename, spec)
		}
		if len(spec.RequiredQuests) != 0 || len(spec.RequiredAnyQuests) != 0 {
			t.Errorf("%s chains inside the table; its chain is gated outside it", spec.Codename)
		}
		switch spec.Objective {
		case ObjectiveTalk:
			if spec.AchievedNowSymbol != "" {
				t.Errorf("%s talk quest carries an achieved-now banner", spec.Codename)
			}
		case ObjectiveKill:
			if spec.AchievedNowSymbol != talk+"06" || spec.KillCount == 0 || len(spec.KillMonsterCodenames) != 1 {
				t.Errorf("%s scalar hunt %+v", spec.Codename, spec)
			}
		case ObjectiveParallel:
			if spec.AchievedNowSymbol != talk+"06" || len(spec.Objectives) < 2 {
				t.Errorf("%s parallel hunt %+v", spec.Codename, spec)
			}
			for _, mission := range spec.Objectives {
				if mission.Objective != ObjectiveKill || mission.KillCount == 0 || len(mission.KillMonsterCodenames) != 1 ||
					!strings.HasPrefix(mission.ContentsSymbol, "SN_CON_"+spec.Codename+"_") {
					t.Errorf("%s mission %+v", spec.Codename, mission)
				}
			}
		default:
			t.Errorf("%s objective %d", spec.Codename, spec.Objective)
		}
	}
	// The evidence pins: the Kak Farm talk, the first hunt's two 390
	// lines twice over, the three-line 054 hunt, a 5-times hunt.
	farm := known["QNO_AR_005"]
	if farm.Objective != ObjectiveTalk || farm.StartNpcCodename != "NPC_AR_SOLDIER_1" || farm.EndNpcCodename != "NPC_AR_FARMER1" ||
		farm.MaxCompletions != 1 || farm.RewardExp != 34546860 || farm.RewardGold != 47050 || farm.RewardSkillExp != 671 {
		t.Errorf("QNO_AR_005 %+v", farm)
	}
	hunt := known["QNO_AR_008"]
	if hunt.Objective != ObjectiveParallel || hunt.MaxCompletions != 2 || len(hunt.Objectives) != 2 ||
		hunt.Objectives[0].KillMonsterCodenames[0] != "MOB_AR_BARUSWOLF_ADONIS1_CLONE" || hunt.Objectives[0].KillCount != 390 ||
		hunt.Objectives[1].KillMonsterCodenames[0] != "MOB_AR_BARUSWOLF_ADONIS2_CLONE" || hunt.Objectives[1].KillCount != 390 ||
		hunt.RewardExp != 165824929 || hunt.RewardGold != 366990 || hunt.RewardSkillExp != 3222 {
		t.Errorf("QNO_AR_008 %+v", hunt)
	}
	triple := known["QNO_AR_054"]
	if len(triple.Objectives) != 3 || triple.Objectives[2].KillMonsterCodenames[0] != "MOB_AR_KARMOS_CACTUS" ||
		triple.Objectives[2].KillCount != 380 {
		t.Errorf("QNO_AR_054 %+v", triple)
	}
	if known["QNO_AR_097"].MaxCompletions != 5 || known["QNO_AR_102"].MaxCompletions != 5 || known["QNO_AR_103"].MaxCompletions != 5 {
		t.Error("the 5-times hunts lost their limit")
	}
	// Held back, by the file's own evidence rule (dialog gaps, kinds
	// no source carries or places): none of these may appear.
	for _, held := range []string{"QNO_AR_001", "QNO_AR_002", "QNO_AR_003", "QNO_AR_004", "QNO_AR_010",
		"QNO_AR_019", "QNO_AR_026", "QNO_AR_027", "QNO_AR_059", "QNO_AR_063", "QNO_AR_066", "QNO_AR_068"} {
		if _, ok := known[held]; ok {
			t.Errorf("%s is authored ahead of its evidence", held)
		}
	}
}

/*
================
TestArabiaSpecsLoadOnlyWithTheirRows
================
*/
func TestArabiaSpecsLoadOnlyWithTheirRows(t *testing.T) {
	// LoadDefinitions validates the whole curated table against the
	// shipped itemdata (the tutorial armor resolves only there), so
	// this run needs the data - the same gate the curated-table test
	// carries. The table's shape above needs none and runs anywhere.
	clientdata.RequireGameData(t)
	// The native fixture catalog plus the rows the Arabia import brings:
	// the curated table resolves there (its own contract), and the
	// Arabia specs with it.
	catalog := writeTestCatalog(t, true)
	var questRows, contentsRows []string
	for i, spec := range arabiaSpecs() {
		id := 1448 + i
		questRows = append(questRows, fmt.Sprintf("1\t%d\t%s\t121\tname\tSN_%s\txxx\txxx\txxx\txxx\txxx", id, spec.Codename, spec.Codename))
		symbols := []string{"SN_CON_" + spec.Codename + "_1"}
		for _, mission := range spec.Objectives {
			symbols = append(symbols, mission.ContentsSymbol)
		}
		row := []string{spec.Codename, "name", "3", "xxx", "1"}
		row = append(row, symbols...)
		for len(row) < 13 {
			row = append(row, "xxx")
		}
		contentsRows = append(contentsRows, strings.Join(row, "\t"))
	}
	for name, rows := range map[string][]string{"questdata.txt": questRows, "questcontentsdata.txt": contentsRows} {
		path := filepath.Join(catalog.dir, name)
		prior, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(prior, []byte("\n"+strings.Join(rows, "\n"))...), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	defs, err := LoadDefinitions(catalog, fakeItems{})
	if err != nil {
		t.Fatalf("LoadDefinitions: %v", err)
	}
	for _, code := range arabiaWanted {
		def, ok := defs.ByCodename(code)
		if !ok {
			t.Fatalf("%s did not resolve with its rows present", code)
		}
		if def.ContentsSymbol != "SN_CON_"+code+"_1" {
			t.Errorf("%s contents symbol %s", code, def.ContentsSymbol)
		}
	}
	// The same specs over a catalog without the Arabia rows: admission
	// skips them (a build without the layer stays native, never a
	// short-table refusal).
	bare := writeTestCatalog(t, true)
	defs, err = LoadDefinitions(bare, fakeItems{})
	if err != nil {
		t.Fatalf("LoadDefinitions over the native fixture: %v", err)
	}
	if _, ok := defs.ByCodename("QNO_AR_005"); ok {
		t.Fatal("an Arabia spec loaded without its questdata row")
	}
	if !slices.Contains(authoredCodenamesMust(t), "QNO_AR_103") {
		t.Fatal("the metadata snapshot lost the Arabia table")
	}
}

// authoredCodenamesMust is the metadata export, failing the test.
func authoredCodenamesMust(t *testing.T) []string {
	t.Helper()
	codes, err := AuthoredQuestCodenames()
	if err != nil {
		t.Fatal(err)
	}
	return codes
}
