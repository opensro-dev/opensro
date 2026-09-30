/*
===========================================================================

definitions_test.go - quest definitions

The curated definition table loads, fails loudly when broken, and resolves
against the shipped media.

===========================================================================
*/
package quest

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/testsupport/gamedatatest"
	"opensro.online/server/internal/testsupport/licensed"
)

// fakeItems is an ItemRefSource stand-in resolving exactly the curated
// collect item (the shipped itemdata row 3674).
type fakeItems struct{}

var (
	tutorialItemsOnce sync.Once
	tutorialItems     *enterworld.TextdataItems
)

/*
================
tutorialFixtureItems
================
*/
// tutorialFixtureItems is the shared shipped itemdata loader. It resolves on
// first use, which is always inside a test: a package-level resolution runs
// before the test log starts, and Go's test cache would never see the data.
func tutorialFixtureItems() *enterworld.TextdataItems {
	tutorialItemsOnce.Do(func() {
		tutorialItems = enterworld.NewTextdataItems(gamedatatest.TextdataDirOrEmpty())
	})
	return tutorialItems
}

/*
================
ItemRefByCodename
================
*/
func (fakeItems) ItemRefByCodename(codename string) (*enterworld.ItemRef, bool) {
	if codename == "ITEM_QNO_WC_ARMOR_1" {
		return tutorialFixtureItems().ItemRefByCodename(codename)
	}
	if codename == "ITEM_CH_M_LIGHT_01_AA_A" || codename == "ITEM_CH_W_LIGHT_01_AA_A" || codename == "ITEM_CH_RING_01_A" {
		return tutorialFixtureItems().ItemRefByCodename(codename)
	}
	if id, ok := map[string]uint32{"ITEM_QNO_CH_SPECIAL_1_01": 3955, "ITEM_QNO_CH_POTION_3_01": 3958, "ITEM_QNO_CH_GENARAL_BO_2_01": 3961, "ITEM_QNO_CH_FERRY2_1_01": 3966}[codename]; ok {
		return &enterworld.ItemRef{RefObjID: id, Codename: codename, TypeIDs: [4]int64{3, 3, 8, 0}, NativeFields: enterworld.NewNativeFields(map[string]float64{"maxStack": 250})}, true
	}
	if codename == "ITEM_ETC_MP_POTION_01" {
		return &enterworld.ItemRef{RefObjID: 3631, Codename: codename, TypeIDs: [4]int64{3, 3, 1, 2}, NativeFields: enterworld.NewNativeFields(map[string]float64{"maxStack": 50})}, true
	}
	if codename == "ITEM_QNO_CH_CHEF_1" {
		return &enterworld.ItemRef{RefObjID: 2201, Codename: codename, TypeIDs: [4]int64{3, 3, 8, 0}, NativeFields: enterworld.NewNativeFields(map[string]float64{"maxStack": 1})}, true
	}
	if codename == "ITEM_QNO_CH_SMITH_1" {
		return &enterworld.ItemRef{RefObjID: 3657, Codename: codename, TypeIDs: [4]int64{3, 3, 8, 0}, NativeFields: enterworld.NewNativeFields(map[string]float64{"maxStack": 1})}, true
	}
	if codename == "ITEM_ETC_HP_POTION_01" {
		return &enterworld.ItemRef{RefObjID: 3630, Codename: codename, TypeIDs: [4]int64{3, 3, 1, 1}, NativeFields: enterworld.NewNativeFields(map[string]float64{"maxStack": 50})}, true
	}
	if codename == "ITEM_QSP_ALL_POTION_1_01" {
		return &enterworld.ItemRef{RefObjID: 3673, Codename: codename, TypeIDs: [4]int64{3, 3, 9, 0}, NativeFields: enterworld.NewNativeFields(map[string]float64{"maxStack": 250})}, true
	}
	if codename == "ITEM_QSP_ALL_POTION_1_02" {
		return &enterworld.ItemRef{RefObjID: 3674, Codename: codename}, true
	}
	return nil, false
}

/*
================
writeTestCatalog
================
*/
// writeTestCatalog writes a minimal UTF-8 questdata/questcontentsdata
// pair carrying the curated rows (the shipped column shapes; the reader
// tolerates UTF-8 - enterworld.ReadTextdataFile).
func writeTestCatalog(t *testing.T, withContents bool) *Catalog {
	t.Helper()
	dir := t.TempDir()
	questRows := []string{
		"1\t10\tQNO_CH_GENARAL_BO_1\t10\tname\tSN_QNO_CH_GENARAL_BO_1\txxx\txxx\txxx\txxx\txxx",
		"1\t11\tQNO_CH_GENARAL_SP_1\t13\tname\tSN_QNO_CH_GENARAL_SP_1\txxx\txxx\txxx\txxx\txxx",
		"1\t6\tQNO_CH_POTION_1\t5\tname\tSN_QNO_CH_POTION_1\txxx\txxx\txxx\txxx\txxx",
		"1\t5\tQNO_CH_SOLDIER_EA1_1\t3\tname\tSN_QNO_CH_SOLDIER_EA1_1\txxx\txxx\txxx\txxx\txxx",
		"1\t7\tQNO_CH_SOLDIER_EA2_1\t7\tname\tSN_QNO_CH_SOLDIER_EA2_1\txxx\txxx\txxx\txxx\txxx",
		"1\t143\tQNO_EU_TUTORIAL_1\t1\tname\tSN_QNO_EU_TUTORIAL_1\tSN_PAY_QNO_EU_TUTORIAL_1\txxx\tSN_PAYCON_QNO_EU_TUTORIAL_1\tSN_NN_QNO_EU_TUTORIAL_1\tSN_NC_QNO_EU_TUTORIAL_1",
		"1\t2\tQTUTORIAL_CH\t0\tname\tSN_QTUTORIAL_CH\tSN_PAY_QTUTORIAL_CH\txxx\tSN_PAYCON_QTUTORIAL_CH\tSN_NN_QTUTORIAL_CH\tSN_NC_QTUTORIAL_CH",
		"1\t3\tQNO_CH_SMITH_1\t0\tname\tSN_QNO_CH_SMITH_1\tSN_PAY_QNO_CH_SMITH_1\txxx\tSN_PAYCON_QNO_CH_SMITH_1\tSN_NN_QNO_CH_SMITH_1\tSN_NC_QNO_CH_SMITH_1",
		"1\t4\tQNO_CH_CHEF_1\t0\tname\tSN_QNO_CH_CHEF_1\tSN_PAY_QNO_CH_CHEF_1\txxx\tSN_PAYCON_QNO_CH_CHEF_1\tSN_NN_QNO_CH_CHEF_1\tSN_NC_QNO_CH_CHEF_1",
		"1\t29\tQSP_ALL_POTION_1\t20\tname\tSN_QSP_ALL_POTION_1\tSN_PAY_QSP_ALL_POTION_1\txxx\tSN_PAYCON_QSP_ALL_POTION_1\tSN_NN_QSP_ALL_POTION_1\tSN_NC_QSP_ALL_POTION_1",
	}
	for _, row := range collectionCatalogFixture {
		questRows = append(questRows, fmt.Sprintf("1\t%d\t%s\t%d\tname\tSN_%s\txxx\txxx\txxx\txxx\txxx", row.id, row.code, row.level, row.code))
	}
	if err := os.WriteFile(filepath.Join(dir, "questdata.txt"), []byte(strings.Join(questRows, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}
	if withContents {
		contentsRows := []string{
			"QNO_CH_GENARAL_BO_1\tname\t0\tQNO_CH_GENARAL_SP_1\t1\tSN_CON_QNO_CH_GENARAL_BO_1\txxx\txxx\txxx\txxx\txxx\txxx\txxx",
			"QNO_CH_GENARAL_SP_1\tname\t0\txxx\t1\tSN_CON_QNO_CH_GENARAL_SP_1\txxx\txxx\txxx\txxx\txxx\txxx\txxx",
			"QNO_CH_POTION_1\tname\t0\txxx\t1\tSN_CON_QNO_CH_POTION_1\txxx\txxx\txxx\txxx\txxx\txxx\txxx",
			"QNO_CH_SOLDIER_EA1_1\tname\t0\txxx\t1\tSN_CON_QNO_CH_SOLDIER_EA1_1\txxx\txxx\txxx\txxx\txxx\txxx\txxx",
			"QNO_CH_SOLDIER_EA2_1\tname\t0\txxx\t1\tSN_CON_QNO_CH_SOLDIER_EA2_1\txxx\txxx\txxx\txxx\txxx\txxx\txxx",
			"QNO_EU_TUTORIAL_1\tname\t1\tQNO_EU_TUTORIAL_2\t0\tSN_CON_QNO_EU_TUTORIAL_1\txxx\txxx\txxx\txxx\txxx\txxx\txxx",
			"QTUTORIAL_CH\tname\t0\tQNO_CH_SMITH_1\t0\tSN_CON_QTUTORIAL_CH_01\txxx\txxx\txxx\txxx\txxx\txxx\txxx",
			"QNO_CH_SMITH_1\tname\t0\txxx\t1\tSN_CON_QNO_CH_SMITH_1\txxx\txxx\txxx\txxx\txxx\txxx\txxx",
			"QNO_CH_CHEF_1\tname\t0\txxx\t1\tSN_CON_QNO_CH_CHEF_1\txxx\txxx\txxx\txxx\txxx\txxx\txxx",
			"QSP_ALL_POTION_1\tname\t0\txxx\t1\tSN_CON_QSP_ALL_POTION_1\txxx\txxx\txxx\txxx\txxx\txxx\txxx",
		}
		for _, row := range collectionCatalogFixture {
			contentsRows = append(contentsRows, fmt.Sprintf("%s\tname\t0\txxx\t1\tSN_CON_%s\txxx\txxx\txxx\txxx\txxx\txxx\txxx", row.code, row.code))
		}
		if err := os.WriteFile(filepath.Join(dir, "questcontentsdata.txt"), []byte(strings.Join(contentsRows, "\n")), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return NewCatalog(dir)
}

/*
================
loadTestDefinitions
================
*/
// loadTestDefinitions is the shared fixture loader for the runtime and
// seed suites.
func loadTestDefinitions(t *testing.T) *Definitions {
	t.Helper()
	defs, err := LoadDefinitions(writeTestCatalog(t, true), fakeItems{})
	if err != nil {
		t.Fatalf("LoadDefinitions: %v", err)
	}
	return defs
}

/*
================
TestLoadDefinitionsResolvesTheCuratedTable
================
*/
func TestLoadDefinitionsResolvesTheCuratedTable(t *testing.T) {
	licensed.RequireGameData(t)
	t.Parallel()
	defs := loadTestDefinitions(t)
	if defs.Len() != len(curatedQuestSpecs) {
		t.Fatalf("loaded %d definitions, want %d", defs.Len(), len(curatedQuestSpecs))
	}

	tutorial, ok := defs.ByCodename("QTUTORIAL_CH")
	if !ok || tutorial.RefID != 2 || tutorial.KindByte != 1 || tutorial.Objective != ObjectiveTalk {
		t.Fatalf("QTUTORIAL_CH = %+v, want id 2 / kind 1 / talk", tutorial)
	}
	if tutorial.ContentsSymbol != "SN_CON_QTUTORIAL_CH_01" {
		t.Fatalf("tutorial contents symbol = %q (the shipped SN_CON_* join)", tutorial.ContentsSymbol)
	}
	if len(tutorial.NextQuests) != 1 || tutorial.NextQuests[0] != "QNO_CH_SMITH_1" {
		t.Fatalf("tutorial chain = %v, want the questcontentsdata col-3 next quest", tutorial.NextQuests)
	}

	potion, ok := defs.ByRefID(29)
	if !ok || potion.Codename != "QSP_ALL_POTION_1" || potion.KindByte != 2 {
		t.Fatalf("id 29 = %+v, want the kind-2 collection quest", potion)
	}
	if potion.CollectItemRefID != 3674 || potion.CollectCount != 10 {
		t.Fatalf("collect resolution = item %d x %d, want 3674 x 10", potion.CollectItemRefID, potion.CollectCount)
	}

	chef, _ := defs.ByCodename("QNO_CH_CHEF_1")
	if chef.RewardExp != 375 || chef.RewardGold != 475 {
		t.Fatalf("chef reward = %d exp / %d gold, want the v1.150 text-pinned 375/475", chef.RewardExp, chef.RewardGold)
	}
}

/*
================
TestLoadDefinitionsFailsLoud
================
*/
func TestLoadDefinitionsFailsLoud(t *testing.T) {
	t.Parallel()
	t.Run("missing questcontentsdata refuses", func(t *testing.T) {
		t.Parallel()
		_, err := LoadDefinitions(writeTestCatalog(t, false), fakeItems{})
		if err == nil || !strings.Contains(err.Error(), "questcontentsdata") {
			t.Fatalf("want the loud questcontentsdata refusal, got %v", err)
		}
	})
	t.Run("unresolvable collect item refuses", func(t *testing.T) {
		t.Parallel()
		_, err := LoadDefinitions(writeTestCatalog(t, true), nil)
		if err == nil || !strings.Contains(err.Error(), "ItemRefSource") {
			t.Fatalf("want the loud item-source refusal, got %v", err)
		}
	})
	t.Run("absent media loads an EMPTY set (the TextdataSkills degradation)", func(t *testing.T) {
		t.Parallel()
		defs, err := LoadDefinitions(NewCatalog(filepath.Join(t.TempDir(), "missing")), fakeItems{})
		if err != nil {
			t.Fatalf("an absent catalog must degrade, not refuse: %v", err)
		}
		if defs.Len() != 0 {
			t.Fatalf("loaded %d definitions from a missing dir, want 0", defs.Len())
		}
	})
}

/*
================
TestLoadDefinitionsAgainstShippedMedia
================
*/
// Canary against the REAL shipped media (the leveldata_test posture):
// if a re-extraction ever moves the questdata/questcontentsdata columns
// or renames a curated codename, the definitions would silently drift,
// so the shipped resolution is asserted here. Skips when this checkout
// has no media.
func TestLoadDefinitionsAgainstShippedMedia(t *testing.T) {
	t.Parallel()
	devDefault := licensed.RetailTextdataDir(t)
	dir := ""
	for _, candidate := range []string{devDefault, filepath.Join("..", devDefault), filepath.Join("..", "..", devDefault)} {
		if _, err := os.Stat(filepath.Join(candidate, "questdata.txt")); err == nil {
			dir = candidate
			break
		}
	}
	if dir == "" {
		t.Skip("shipped questdata.txt not present in this checkout")
	}
	catalog := NewCatalog(dir)
	if catalog.Len() != 224 {
		t.Fatalf("shipped questdata rows = %d, want 224", catalog.Len())
	}
	defs, err := LoadDefinitions(catalog, enterworld.NewTextdataItems(dir))
	if err != nil {
		t.Fatalf("the curated table must resolve against the shipped media: %v", err)
	}
	potion, ok := defs.ByCodename("QSP_ALL_POTION_1")
	if !ok || potion.RefID != 29 || potion.CollectItemRefID != 3674 {
		t.Fatalf("shipped QSP_ALL_POTION_1 = %+v, want id 29 / item 3674", potion)
	}
	tutorial, ok := defs.ByCodename("QTUTORIAL_CH")
	if !ok || tutorial.RefID != 2 {
		t.Fatalf("shipped QTUTORIAL_CH = %+v, want id 2", tutorial)
	}
}
