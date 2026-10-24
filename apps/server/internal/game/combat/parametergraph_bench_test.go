/*
===========================================================================

parametergraph_bench_test.go - player parameter graph cost at level 80

These benchmarks isolate the keeper operations behind a player stat
snapshot: building the linked graph, cloning it, and applying either the
base-write batch or one later write. They use the production definitions,
links and level-80 base writes.
===========================================================================
*/

package combat

import (
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/paramkeeper"
)

var benchmarkGraphSink *paramkeeper.Graph

var benchmarkStatsSink Stats

/*
================
benchmarkLevel80Graph

A production graph with the level-80 base writes and the snapshot's
level, STR and INT writes applied.
================
*/
func benchmarkLevel80Graph(b *testing.B) *paramkeeper.Graph {
	b.Helper()
	graph, writes, err := playerParameterGraph(80)
	if err != nil {
		b.Fatal(err)
	}
	writes = append(writes,
		paramkeeper.Write{Parameter: 0, Value: 80},
		paramkeeper.Write{Parameter: 1, Value: 200},
		paramkeeper.Write{Parameter: 2, Value: 200})
	if err := graph.ApplyBatch(writes); err != nil {
		b.Fatal(err)
	}
	return graph
}

/*
================
BenchmarkPlayerParameterGraphBuild

Building the graph is the production definition set, NewGraph and every
production link in order.
================
*/
func BenchmarkPlayerParameterGraphBuild(b *testing.B) {
	definitions, err := playerParameterDefinitions()
	if err != nil {
		b.Fatal(err)
	}
	links := playerParameterLinks()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		graph, err := paramkeeper.NewGraph(definitions)
		if err != nil {
			b.Fatal(err)
		}
		for _, link := range links {
			if err := graph.Link(link[0], link[1], paramkeeper.Channel(link[2])); err != nil {
				b.Fatal(err)
			}
		}
		benchmarkGraphSink = graph
	}
}

/*
================
BenchmarkPlayerParameterGraphCloneLevel80

Cloning a graph after its level-80 writes have been applied.
================
*/
func BenchmarkPlayerParameterGraphCloneLevel80(b *testing.B) {
	graph := benchmarkLevel80Graph(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchmarkGraphSink = graph.Clone()
	}
}

/*
================
BenchmarkPlayerParameterGraphApplyBatchLevel80

Applying the level-80 base-write batch to a fresh clone of the linked
template, the shape of a player stat snapshot's main write phase.
================
*/
func BenchmarkPlayerParameterGraphApplyBatchLevel80(b *testing.B) {
	graphs := make([]*paramkeeper.Graph, b.N)
	writeSets := make([][]paramkeeper.Write, b.N)
	for i := range graphs {
		graph, writes, err := playerParameterGraph(80)
		if err != nil {
			b.Fatal(err)
		}
		graphs[i] = graph
		writeSets[i] = append(writes,
			paramkeeper.Write{Parameter: 0, Value: 80},
			paramkeeper.Write{Parameter: 1, Value: 200},
			paramkeeper.Write{Parameter: 2, Value: 200})
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := graphs[i].ApplyBatch(writeSets[i]); err != nil {
			b.Fatal(err)
		}
		benchmarkGraphSink = graphs[i]
	}
}

/*
================
BenchmarkPlayerStatsWithModifiersLevel80

The end-to-end hot path the keeper cost lands in: a level-80 snapshot
with three equipped pieces, four effect writes and two abnormal writes.
================
*/
func BenchmarkPlayerStatsWithModifiersLevel80(b *testing.B) {
	sword := &enterworld.ItemRef{
		RefObjID: 71,
		Codename: "ITEM_CH_SWORD_01_A",
		TypeIDs:  [4]int64{3, 1, 6, 2},
		Combat: &enterworld.ItemCombatRef{
			ActionRange: 6,
			PhysicalAttack: enterworld.ItemAttackRange{
				Minimum: enterworld.ItemStatRange{Min: 15, Max: 16, PerPlus: 2.4000001},
				Maximum: enterworld.ItemStatRange{Min: 16, Max: 18, PerPlus: 2.4000001},
			},
			MagicalAttack: enterworld.ItemAttackRange{
				Minimum: enterworld.ItemStatRange{Min: 25, Max: 26, PerPlus: 4.0999999},
				Maximum: enterworld.ItemStatRange{Min: 28, Max: 31, PerPlus: 4.0999999},
			},
			HitRate:      enterworld.ItemStatRange{Min: 24, Max: 30},
			CriticalRate: enterworld.ItemStatRange{Min: 3, Max: 15},
		},
	}
	armor := &enterworld.ItemRef{
		RefObjID: 101,
		Codename: "ITEM_CH_M_HEAVY_01_BA_A",
		TypeIDs:  [4]int64{3, 1, 1, 1},
		Combat: &enterworld.ItemCombatRef{
			PhysicalDefense: enterworld.ItemStatRange{Min: 12, Max: 15, PerPlus: 1.5},
			MagicalDefense:  enterworld.ItemStatRange{Min: 8, Max: 10, PerPlus: 1.2},
			EvasionRate:     enterworld.ItemStatRange{Min: 5, Max: 8},
		},
	}
	armor2 := &enterworld.ItemRef{
		RefObjID: 102,
		Codename: "ITEM_CH_M_HEAVY_01_FA_A",
		TypeIDs:  [4]int64{3, 1, 1, 1},
		Combat: &enterworld.ItemCombatRef{
			PhysicalDefense: enterworld.ItemStatRange{Min: 9, Max: 12, PerPlus: 1.2},
			MagicalDefense:  enterworld.ItemStatRange{Min: 6, Max: 9, PerPlus: 1.0},
			EvasionRate:     enterworld.ItemStatRange{Min: 4, Max: 7},
		},
	}
	character := &domain.Character{
		Level:     pointer(80),
		MaxLevel:  pointer(80),
		Strength:  pointer(300),
		Intellect: pointer(200),
		MissionInventory: []domain.InventoryRow{
			{Slot: 1, RefObjID: armor.RefObjID, Codename: armor.Codename, TypeFlags: armor.TypeFlags(), VarianceBits: "0", Durability: 80, Plus: 5, StackCount: 1},
			{Slot: 2, RefObjID: armor2.RefObjID, Codename: armor2.Codename, TypeFlags: armor2.TypeFlags(), VarianceBits: "0", Durability: 70, Plus: 3, StackCount: 1},
			{Slot: 6, RefObjID: sword.RefObjID, Codename: sword.Codename, TypeFlags: sword.TypeFlags(), VarianceBits: "0", Durability: 76, Plus: 7, StackCount: 1},
		},
	}
	catalogs := Catalogs{Items: itemRefs{
		sword.Codename: sword, armor.Codename: armor, armor2.Codename: armor2,
	}}
	modifiers := []paramkeeper.Write{
		{Parameter: 1, Channel: paramkeeper.Flat, Source: 0x80000001, Value: 25},
		{Parameter: 5, Channel: paramkeeper.PercentSum, Source: 0x80000002, Value: 10},
		{Parameter: 13, Channel: paramkeeper.Flat, Source: 0x80000003, Value: 40},
		{Parameter: 0x8c, Channel: paramkeeper.PercentSum, Source: 0x80000004, Value: 20},
	}
	block := &abnormal.Block{}
	block.Modifiers[0] = abnormal.Modifier{Used: true, Param: 0x8c, Channel: 3, Source: 5, Value: 125}
	block.Modifiers[1] = abnormal.Modifier{Used: true, Param: 1, Channel: 0, Source: 5, Value: 50}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		stats, _, err := PlayerStatsWithModifiers(character, catalogs, modifiers, block)
		if err != nil {
			b.Fatal(err)
		}
		benchmarkStatsSink = stats
	}
}

/*
================
BenchmarkPlayerParameterGraphApplyLevel80

One later write to a level-80 graph, the shape of an abnormal modifier
landing after the snapshot's batch.
================
*/
func BenchmarkPlayerParameterGraphApplyLevel80(b *testing.B) {
	graphs := make([]*paramkeeper.Graph, b.N)
	for i := range graphs {
		graphs[i] = benchmarkLevel80Graph(b)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := graphs[i].Apply(5, paramkeeper.Flat, 4096, float32(i+1)); err != nil {
			b.Fatal(err)
		}
		benchmarkGraphSink = graphs[i]
	}
}
