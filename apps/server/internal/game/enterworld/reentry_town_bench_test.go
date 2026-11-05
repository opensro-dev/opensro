/*
===========================================================================

reentry_town_bench_test.go - one re-entry at town scale (experiment)

The stock BenchmarkReentryPackets character carries no inventory and
stands alone in its division. A live return-scroll arrival rebuilds the
entry for a full character in a populated town: this variant stocks the
viewer and sixty resident peers with full bags so the division-wide
reference seed and the blob encode pay production prices.

===========================================================================
*/
package enterworld

import (
	"fmt"
	"testing"
)

var townBenchItems = []InventoryRow{
	{RefObjID: 107, Codename: "ITEM_CH_BLADE_01_A", TypeFlags: 0x32c, VarianceBits: "0", Durability: 56, StackCount: 1},
	{RefObjID: 11, Codename: "ITEM_CH_M_CLOTHES_01_BA_A_DEF", TypeFlags: 0x8c, VarianceBits: "0", StackCount: 1},
	{RefObjID: 5049, Codename: "ITEM_CH_M_HEAVY_01_LA_A", TypeFlags: 0x8c, VarianceBits: "0", StackCount: 1},
}

/*
================
townBenchCharacter

One resident with rows inventory rows cycling the stocked items.
================
*/
func townBenchCharacter(name string, rows int) *Character {
	c := chinaSpearman()
	c.Name = name
	for slot := 0; slot < rows; slot++ {
		row := townBenchItems[slot%len(townBenchItems)]
		row.Slot = int64(slot)
		c.MissionInventory = append(c.MissionInventory, row)
	}
	return c
}

/*
================
BenchmarkReentryPacketsTown

One full re-entry packet rebuild for a stocked character arriving in a
division of sixty-one stocked residents.
================
*/
func BenchmarkReentryPacketsTown(b *testing.B) {
	viewer := townBenchCharacter("asd2", 90)
	chars := []*Character{viewer}
	for i := 0; i < 60; i++ {
		chars = append(chars, townBenchCharacter(fmt.Sprintf("peer%02d", i), 60))
	}
	deps := testDeps(chars...)
	deps.RefObjSnapshot = func() []RefObjRow {
		rows := make([]RefObjRow, 0, 600)
		for i := 0; i < 600; i++ {
			rows = append(rows, RefObjRow{RefObjID: uint32(1000 + i), Codename: fmt.Sprintf("MOB_BENCH_%03d", i), Kind: "monster"})
		}
		return rows
	}
	setupReentryBenchCache(deps)
	deps.BrowserReferences = &BrowserReferences{}
	packets, ok := deps.ReentryPackets(DefaultDivisionID, viewer.Name)
	if !ok {
		b.Fatal("fixture re-entry refused")
	}
	total := 0
	for _, p := range packets {
		total += len(p.Payload)
	}
	b.Logf("entry payload: %d packets, %d bytes", len(packets), total)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		packets, ok := deps.ReentryPackets(DefaultDivisionID, viewer.Name)
		if !ok || len(packets) == 0 {
			b.Fatal("re-entry refused")
		}
	}
}
