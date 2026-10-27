/*
===========================================================================

refitem_snapshot_bench_test.go - the division-wide ref-item seed cost

buildRefItemSnapshot stocks every division peer's worn rows at one
login. The seed used to detach every peer's full record to read two
fields per inventory row; this benchmark prices that walk over a full
division so the read-view replacement reports its drop against it
(#1820 part C).

===========================================================================
*/
package enterworld

import (
	"fmt"
	"testing"
)

/*
================
benchPeerInventory

A realistic peer bag for the seed benchmark: forty rows, the first few
resolving in the test catalog like worn equipment does.
================
*/
func benchPeerInventory() []InventoryRow {
	rows := make([]InventoryRow, 0, 40)
	for slot := int64(0); slot < 40; slot++ {
		rows = append(rows, InventoryRow{
			Slot:      slot,
			RefObjID:  uint32(1000 + slot),
			Codename:  fmt.Sprintf("ITEM_BENCH_%02d", slot),
			TypeFlags: uint16(0x1b2c + slot),
		})
	}
	rows[6].Codename = "ITEM_CH_BLADE_01_A"
	return rows
}

/*
================
benchPeerSkills

The learned-skill list a played character carries into the clone.
================
*/
func benchPeerSkills() []uint32 {
	skills := make([]uint32, 0, 24)
	for id := uint32(0); id < 24; id++ {
		skills = append(skills, 2000+id)
	}
	return skills
}

/*
================
BenchmarkBuildRefItemSnapshotDivisionPeers

One login's reference seed over a 64-peer division.
================
*/
func BenchmarkBuildRefItemSnapshotDivisionPeers(b *testing.B) {
	viewer := chinaSpearman()
	viewer.Name = "Viewer"
	characters := []*Character{viewer}
	for i := 0; i < 64; i++ {
		peer := chinaSpearman()
		peer.Name = fmt.Sprintf("Peer%03d", i)
		peer.MissionInventory = benchPeerInventory()
		peer.Skills = benchPeerSkills()
		characters = append(characters, peer)
	}
	deps := testDeps(characters...)
	b.ReportAllocs()
	for b.Loop() {
		if rows := buildRefItemSnapshot(deps, DefaultDivisionID, viewer); len(rows) == 0 {
			b.Fatal("ref item snapshot is empty")
		}
	}
}
