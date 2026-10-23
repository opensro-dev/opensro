/*
===========================================================================

parametergraph_test.go - tests for parametergraph.go

===========================================================================
*/

package combat

import (
	"encoding/json"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/paramkeeper"
	"opensro.online/server/internal/testsupport/licensed"
	"opensro.online/server/internal/testsupport/paramtest"
)

func TestCombatDefinitionsMatchExtractedNativeTable(t *testing.T) {
	// The installed table against the operator's extraction record; both are
	// source-data.
	paramtest.RequireOperatorTable(t)
	b := licensed.SourceDataFile(t, "fixtures/server/internal/game/combat/testdata/paramkeeper-definitions-188.json")
	var err error
	var report struct {
		Definitions []struct {
			ID                     uint16
			Min, Max, Base, Ignore float32
		}
	}
	if err = json.Unmarshal(b, &report); err != nil {
		t.Fatal(err)
	}
	table := make(map[uint16]paramkeeper.Definition)
	for _, d := range report.Definitions {
		table[d.ID] = paramkeeper.Definition{Minimum: d.Min, Maximum: d.Max, Base: d.Base, Ignore: d.Ignore}
	}
	definitions, err := playerParameterDefinitions()
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range definitions {
		want, ok := table[d.ID]
		if !ok || d.Definition != want {
			t.Fatalf("parameter %d: %+v; native %+v", d.ID, d.Definition, want)
		}
	}
}

func TestProductionGraphPreservesIndependentDefenseSources(t *testing.T) {
	g, writes, err := playerParameterGraph(1)
	if err != nil {
		t.Fatal(err)
	}
	writes = append(writes, paramkeeper.Write{Parameter: 1, Value: 100}, paramkeeper.Write{Parameter: 2, Value: 100},
		paramkeeper.Write{Parameter: 5, Source: 1024, Value: 50}, paramkeeper.Write{Parameter: 5, Source: 2048, Value: 20})
	if err = g.ApplyBatch(writes); err != nil {
		t.Fatal(err)
	}
	assertDefense := func(want float32) {
		t.Helper()
		v, e := g.Value(5)
		if e != nil || v != want {
			t.Fatalf("defense %v want %v: %v", v, want, e)
		}
	}
	assertDefense(89)
	if _, err = g.Apply(1, paramkeeper.Flat, 4096, 100); err != nil {
		t.Fatal(err)
	}
	assertDefense(108)
	if _, err = g.Remove(5, 2048); err != nil {
		t.Fatal(err)
	}
	assertDefense(88)
	if _, err = g.Remove(1, 4096); err != nil {
		t.Fatal(err)
	}
	assertDefense(69)
	// Param 12 is capped at 100 in the operator's definition, even if
	// distinct equipment and passive sources would sum above that cap.
	t.Run("critical cap", func(t *testing.T) {
		paramtest.RequireOperatorTable(t)
		if err := g.ApplyBatch([]paramkeeper.Write{{Parameter: 12, Source: 1024, Value: 80}, {Parameter: 12, Source: 2048, Value: 40}}); err != nil {
			t.Fatal(err)
		}
		if v, e := g.Value(12); e != nil || v != 100 {
			t.Fatalf("critical cap: %v %v", v, e)
		}
	})
}

func TestPlayerStatsRejectsDuplicateEquipmentSource(t *testing.T) {
	ref := &enterworld.ItemRef{RefObjID: 1, Codename: "TEST_ARMOR", TypeIDs: [4]int64{3, 1, 1, 1}, Combat: &enterworld.ItemCombatRef{}}
	row := domain.InventoryRow{Slot: 0, RefObjID: 1, Codename: ref.Codename, VarianceBits: "0"}
	c := &domain.Character{Level: pointer(1), Strength: pointer(20), Intellect: pointer(20), MissionInventory: []domain.InventoryRow{row, row}}
	if _, _, err := PlayerStats(c, Catalogs{Items: itemRefs{ref.Codename: ref}}); err == nil {
		t.Fatal("duplicate equipped source silently replaced")
	}
}
