/*
===========================================================================

arabia_quests_collect_test.go - the Baghdad collects and deliveries

The table's shape needs no game data; the loadability run builds a
synthetic catalog with the rows the Arabia import brings and an item
source that resolves the quest items the items import ships.

===========================================================================
*/
package quest

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/testsupport/clientdata"
)

// arabiaCollectWanted is the authored set, in table order: 15
// deliveries and 26 collects (QNO_AR_091 is held: its NPC is unplaced).
var arabiaCollectWanted = []string{
	"QNO_AR_006", "QNO_AR_007", "QNO_AR_011", "QNO_AR_012", "QNO_AR_013",
	"QNO_AR_014", "QNO_AR_015", "QNO_AR_016", "QNO_AR_017", "QNO_AR_018",
	"QNO_AR_022", "QNO_AR_023", "QNO_AR_024", "QNO_AR_025",
	"QNO_AR_032", "QNO_AR_033", "QNO_AR_034", "QNO_AR_035", "QNO_AR_036",
	"QNO_AR_037", "QNO_AR_038", "QNO_AR_039", "QNO_AR_044", "QNO_AR_046",
	"QNO_AR_048", "QNO_AR_050", "QNO_AR_051", "QNO_AR_055", "QNO_AR_056",
	"QNO_AR_057", "QNO_AR_058", "QNO_AR_061", "QNO_AR_062", "QNO_AR_063",
	"QNO_AR_064", "QNO_AR_065", "QNO_AR_067", "QNO_AR_069", "QNO_AR_095",
	"QNO_AR_100", "QNO_AR_101",
}

/*
================
TestArabiaCollectTableIsTheEvidenceSet
================
*/
func TestArabiaCollectTableIsTheEvidenceSet(t *testing.T) {
	specs := arabiaCollectSpecs()
	if len(specs) != len(arabiaCollectWanted) {
		t.Fatalf("authored %d collects/deliveries, want %d", len(specs), len(arabiaCollectWanted))
	}
	known := make(map[string]QuestSpec, len(specs))
	for i, spec := range specs {
		if spec.Codename != arabiaCollectWanted[i] {
			t.Fatalf("table order broke at %d: %s, want %s", i, spec.Codename, arabiaCollectWanted[i])
		}
		if spec.KindByte != 1 {
			t.Errorf("%s kind byte %d, want the Arabia table's 1", spec.Codename, spec.KindByte)
		}
		known[spec.Codename] = spec
	}
	masks := known["QNO_AR_011"]
	if masks.Objective != ObjectiveCollect || masks.CollectItemCodename != "ITEM_QNO_AR_011_01" || masks.CollectCount != 180 ||
		masks.MaxCompletions != 2 || masks.MonsterDrop == nil || masks.MonsterDrop.ChancePercent != 100 ||
		len(masks.MonsterDrop.MonsterCodenames) != 1 || masks.MonsterDrop.MonsterCodenames[0] != "MOB_AR_BARUSWOLF_ADONIS1_CLONE" {
		t.Errorf("QNO_AR_011 %+v", masks)
	}
	coins := known["QNO_AR_034"]
	if coins.CollectCount != 500 || len(coins.MonsterDrop.MonsterCodenames) != 2 {
		t.Errorf("QNO_AR_034 %+v", coins)
	}
	essence := known["QNO_AR_061"]
	if !essence.Repeatable || essence.MaxCompletions != 0 || essence.CollectCount != 70 {
		t.Errorf("QNO_AR_061 %+v", essence)
	}
	general := known["QNO_AR_069"]
	if general.CollectCount != 1 || general.MonsterDrop.MonsterCodenames[0] != "MOB_ARABIA_MAD_GENERAL" {
		t.Errorf("QNO_AR_069 %+v", general)
	}
	for _, code := range []string{"QNO_AR_095", "QNO_AR_100"} {
		five := known[code]
		if five.MaxCompletions != 5 || five.CollectItemCodename != "ITEM_"+code || five.CollectCount != 300 {
			t.Errorf("%s %+v, want a 5-completion collect of 300 %s", code, five, "ITEM_"+code)
		}
	}
	deliver := known["QNO_AR_016"]
	if deliver.Objective != ObjectiveDelivery || len(deliver.RequiredQuests) != 1 || deliver.RequiredQuests[0] != "QNO_AR_011" ||
		len(deliver.DeliveryItems) != 1 || deliver.DeliveryItems[0].ItemCodename != "ITEM_QNO_AR_016_01" || deliver.DeliveryItems[0].Count != 1 ||
		deliver.InventoryFullSymbol != "SN_TALK_QNO_AR_016_04" || deliver.CompletePromptSymbol != "SN_TALK_QNO_AR_016_06" ||
		deliver.AchievedNowSymbol != "SN_TALK_QNO_AR_016_07" || deliver.EndNpcCodename != "NPC_AR_SPECIAL" {
		t.Errorf("QNO_AR_016 %+v", deliver)
	}
	// Evidence: QNO_AR_012 is the repeatable wolf collect the later
	// data marks ×2 (its contents next is QNO_AR_017); fields per
	// questdata's RefQuest + refqusetreward.
	ears := known["QNO_AR_012"]
	if ears.Objective != ObjectiveCollect || ears.CollectItemCodename != "ITEM_QNO_AR_012_01" || ears.CollectCount != 270 ||
		ears.MaxCompletions != 2 || ears.MonsterDrop == nil || ears.MonsterDrop.ChancePercent != 100 ||
		len(ears.MonsterDrop.MonsterCodenames) != 1 || ears.MonsterDrop.MonsterCodenames[0] != "MOB_AR_BARUSWOLF_ADONIS2_CLONE" {
		t.Errorf("QNO_AR_012 %+v", ears)
	}
	// Evidence: QNO_AR_018 is the one-shot thank-you delivery behind
	// QNO_AR_013 (its contents next is 018); fields per questdata's
	// RefQuest + refqusetreward.
	jewels := known["QNO_AR_018"]
	if jewels.Objective != ObjectiveDelivery || len(jewels.RequiredQuests) != 1 || jewels.RequiredQuests[0] != "QNO_AR_013" ||
		len(jewels.DeliveryItems) != 1 || jewels.DeliveryItems[0].ItemCodename != "ITEM_QNO_AR_018_01" ||
		jewels.CompletePromptSymbol != "SN_TALK_QNO_AR_018_06" || jewels.EndNpcCodename != "NPC_AR_CHOUKRI" {
		t.Errorf("QNO_AR_018 %+v", jewels)
	}
	opening := known["QNO_AR_006"]
	if len(opening.RequiredQuests) != 0 || opening.StartNpcCodename != "NPC_AR_SOLDIER_2" || opening.EndNpcCodename != "NPC_AR_FARMER3" {
		t.Errorf("QNO_AR_006 %+v", opening)
	}
	last := known["QNO_AR_101"]
	if len(last.RequiredQuests) != 1 || last.RequiredQuests[0] != "QNO_AR_100" {
		t.Errorf("QNO_AR_101 %+v", last)
	}
	// Held back, by the file's own evidence rule (the unplaced kinds,
	// the quest whose target kind no source carries, the unplaced
	// NPC): none may appear.
	for _, held := range []string{"QNO_AR_059", "QNO_AR_066", "QNO_AR_068", "QNO_AR_091"} {
		if _, ok := known[held]; ok {
			t.Errorf("%s is authored ahead of its evidence", held)
		}
	}
}

// arabiaItems is the fixture item source with the imported quest
// items resolved (their real rows ride the items import's shard).
type arabiaItems struct{ fakeItems }

/*
================
ItemRefByCodename
================
*/
func (arabiaItems) ItemRefByCodename(codename string) (*enterworld.ItemRef, bool) {
	if strings.HasPrefix(codename, "ITEM_QNO_AR_") {
		return &enterworld.ItemRef{RefObjID: 200001, Codename: codename, TypeIDs: [4]int64{3, 3, 9, 0}}, true
	}
	return fakeItems{}.ItemRefByCodename(codename)
}

/*
================
TestArabiaCollectSpecsLoadOnlyWithTheirRows
================
*/
func TestArabiaCollectSpecsLoadOnlyWithTheirRows(t *testing.T) {
	// LoadDefinitions validates the whole curated table against the
	// shipped itemdata (the tutorial armor resolves only there), so
	// this run needs the data - the same gate the curated-table test
	// carries. The table's shape above needs none and runs anywhere.
	clientdata.RequireGameData(t)
	catalog := writeTestCatalog(t, true)
	var questRows, contentsRows []string
	for i, spec := range arabiaCollectSpecs() {
		id := 1500 + i
		questRows = append(questRows, fmt.Sprintf("1\t%d\t%s\t121\tname\tSN_%s\txxx\txxx\txxx\txxx\txxx", id, spec.Codename, spec.Codename))
		row := []string{spec.Codename, "name", "3", "xxx", "1", "SN_CON_" + spec.Codename + "_1"}
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
	defs, err := LoadDefinitions(catalog, arabiaItems{})
	if err != nil {
		t.Fatalf("LoadDefinitions: %v", err)
	}
	for _, code := range arabiaCollectWanted {
		def, ok := defs.ByCodename(code)
		if !ok {
			t.Fatalf("%s did not resolve with its rows present", code)
		}
		if def.ContentsSymbol != "SN_CON_"+code+"_1" {
			t.Errorf("%s contents symbol %s", code, def.ContentsSymbol)
		}
	}
	// The same specs over a catalog without the Arabia rows: admission
	// skips them (a build without the layer stays native).
	bare := writeTestCatalog(t, true)
	defs, err = LoadDefinitions(bare, arabiaItems{})
	if err != nil {
		t.Fatalf("LoadDefinitions over the native fixture: %v", err)
	}
	if _, ok := defs.ByCodename("QNO_AR_011"); ok {
		t.Fatal("an Arabia collect loaded without its questdata row")
	}
}
