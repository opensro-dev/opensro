/*
===========================================================================

mover_interest_index_test.go - candidate coverage and storage lifecycle

Compare indexed candidates with the actual live-pose visibility relation,
including segment interiors, displaced pending actors and original frames.

===========================================================================
*/

package simulation

import (
	"reflect"
	"slices"
	"testing"

	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/monster"
)

/*
================
TestMoverInterestCandidatesMatchLivePose
================
*/
func TestMoverInterestCandidatesMatchLivePose(t *testing.T) {
	const home = uint16(0x6060)
	const duration = int64(10000)
	const edge = worldgeom.OutdoorRegionSize
	cases := []struct {
		name string
		row  monster.MoverState
	}{
		{"multiple regions", monster.MoverState{
			Pose:     monster.Pose{RegionID: home, X: 100, Z: 100},
			From:     monster.Pose{RegionID: home, X: 100, Z: 100},
			To:       monster.Pose{RegionID: home + 6, X: 100, Z: 100},
			ArriveMs: duration,
		}},
		{"diagonal reverse", monster.MoverState{
			Pose:     monster.Pose{RegionID: home, X: 100, Z: 100},
			From:     monster.Pose{RegionID: home + 0x0606, X: 100, Z: 100},
			To:       monster.Pose{RegionID: home, X: 100, Z: 100},
			ArriveMs: duration,
		}},
		{"original frame overflow", monster.MoverState{
			Pose:     monster.Pose{RegionID: home, X: 100, Z: 100},
			From:     monster.Pose{RegionID: home, X: -3*edge + 100, Z: -2*edge + 100},
			To:       monster.Pose{RegionID: home, X: 5*edge + 100, Z: 4*edge + 100},
			ArriveMs: duration,
		}},
		{"settled displaced pending", monster.PendingMover{
			Pose: monster.Pose{RegionID: home, X: 5*edge + 100, Z: -2*edge + 100},
		}.Expand()},
		{"dungeon unbounded locals", monster.MoverState{
			Pose:     monster.Pose{RegionID: home | worldgeom.DungeonRegionBit},
			From:     monster.Pose{RegionID: home | worldgeom.DungeonRegionBit, X: -5 * edge},
			To:       monster.Pose{RegionID: home | worldgeom.DungeonRegionBit, X: 5 * edge},
			ArriveMs: duration,
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			storage := newMoverStorage(map[uint32]monster.MoverState{1: tc.row})
			storage.set(2, monster.PendingMover{Pose: monster.Pose{RegionID: 0x2020}}.Expand())
			for _, now := range []int64{-1, 0, 1000, 2500, 5000, 7500, duration, duration + 1} {
				actualLivePose := tc.row.LivePoseAt(now, nil)
				for _, offset := range []float64{-edge, -worldgeom.InterestBlockSize, 0, worldgeom.InterestBlockSize, edge} {
					viewer := worldgeom.RegionXZ{RegionID: actualLivePose.RegionID, X: actualLivePose.X + offset, Z: actualLivePose.Z}
					assertMoverInterestMatchesLivePose(t, storage, viewer, now)
				}
				viewer := worldgeom.RegionXZ{RegionID: actualLivePose.RegionID, X: actualLivePose.X, Z: actualLivePose.Z}
				if got := filteredMoverInterest(storage, viewer, now); !slices.Contains(got, uint32(1)) {
					t.Fatalf("at %d actual live pose %+v missing from candidates %v", now, actualLivePose, got)
				}
			}
		})
	}
}

/*
================
TestMoverInterestBoundsFalsePositivesAndDeduplication
================
*/
func TestMoverInterestBoundsFalsePositivesAndDeduplication(t *testing.T) {
	row := monster.MoverState{
		Pose:     monster.Pose{RegionID: 0x6060, X: 100, Z: 100},
		From:     monster.Pose{RegionID: 0x6060, X: 100, Z: 100},
		To:       monster.Pose{RegionID: 0x6666, X: 100, Z: 100},
		ArriveMs: 10000,
	}
	storage := newMoverStorage(map[uint32]monster.MoverState{1: row})
	corner := worldgeom.RegionXZ{RegionID: 0x6066, X: 100, Z: 100}
	if got := slices.Collect(storage.candidates(corner)); !reflect.DeepEqual(got, []uint32{1}) {
		t.Fatalf("bounds must include conservative corner once: %v", got)
	}
	if got := filteredMoverInterest(storage, corner, 5000); len(got) != 0 {
		t.Fatalf("actual live-pose filter admitted false positive: %v", got)
	}
	midpoint := worldgeom.RegionXZ{RegionID: 0x6363, X: 100, Z: 100}
	if got := slices.Collect(storage.candidates(midpoint)); !reflect.DeepEqual(got, []uint32{1}) {
		t.Fatalf("overlapping candidate buckets yielded duplicates: %v", got)
	}
	assertMoverInterestMatchesLivePose(t, storage, midpoint, 5000)
}

/*
================
TestMoverInterestUpdateRemoveAndPendingStorage
================
*/
func TestMoverInterestUpdateRemoveAndPendingStorage(t *testing.T) {
	const gid = uint32(7)
	original := monster.PendingMover{Pose: monster.Pose{RegionID: 0x6060, X: 10, Z: 20}, Channel: 2}.Expand()
	storage := newMoverStorage(map[uint32]monster.MoverState{gid: original})
	if !storage.compact(gid) || storage.get(gid) != original {
		t.Fatal("index changed compact pending storage semantics")
	}
	near := worldgeom.RegionXZ{RegionID: 0x6060, X: 10, Z: 20}
	if got := slices.Collect(storage.candidates(near)); !reflect.DeepEqual(got, []uint32{gid}) {
		t.Fatalf("pending candidate missing: %v", got)
	}
	moving := original
	moving.From, moving.To = original.Pose, monster.Pose{RegionID: 0x6068, X: 10, Z: 20}
	moving.ArriveMs = 10000
	storage.set(gid, moving)
	if storage.compact(gid) || storage.get(gid) != moving {
		t.Fatal("pending to live transition lost state")
	}
	settled := monster.PendingMover{Pose: moving.To, Channel: 3}.Expand()
	storage.set(gid, settled)
	if !storage.compact(gid) || storage.get(gid) != settled {
		t.Fatal("live to displaced pending transition lost state")
	}
	if got := slices.Collect(storage.candidates(near)); len(got) != 0 {
		t.Fatalf("old pose/segment memberships survived replacement: %v", got)
	}
	far := worldgeom.RegionXZ{RegionID: settled.Pose.RegionID, X: settled.Pose.X, Z: settled.Pose.Z}
	storage.set(gid, settled)
	if got := slices.Collect(storage.candidates(far)); !reflect.DeepEqual(got, []uint32{gid}) {
		t.Fatalf("unchanged bounds update lost candidate: %v", got)
	}
	storage.remove(gid)
	storage.remove(gid)
	if _, exists := storage.lookup(gid); exists || storage.len() != 0 || len(storage.spatial) != 0 {
		t.Fatal("removal retained row or empty spatial buckets")
	}
	if got := slices.Collect(storage.candidates(far)); len(got) != 0 {
		t.Fatalf("removed identity survived: %v", got)
	}
	storage.set(gid, moving)
	storage.remove(gid)
	if len(storage.spatial) != 0 {
		t.Fatal("removal retained segment interior buckets")
	}
}

/*
================
TestMoverInterestLocalCandidatesAndNilStorage
================
*/
func TestMoverInterestLocalCandidatesAndNilStorage(t *testing.T) {
	var storage *moverStorage
	viewer := worldgeom.RegionXZ{RegionID: 0x6060}
	if storage.len() != 0 || len(slices.Collect(storage.candidates(viewer))) != 0 || storage.get(1) != (monster.MoverState{}) {
		t.Fatal("nil storage changed empty-map semantics")
	}
	storage.remove(1)
	for range storage.values() {
		t.Fatal("nil values yielded a row")
	}
	storage = newMoverStorage(nil)
	for gid := uint32(1); gid <= 4096; gid++ {
		storage.set(gid, monster.PendingMover{Pose: monster.Pose{RegionID: 0x2020}}.Expand())
	}
	storage.set(5000, monster.PendingMover{Pose: monster.Pose{RegionID: viewer.RegionID}}.Expand())
	storage.set(0, monster.PendingMover{Pose: monster.Pose{RegionID: viewer.RegionID}}.Expand())
	if got := slices.Collect(storage.candidates(viewer)); !reflect.DeepEqual(got, []uint32{5000}) {
		t.Fatalf("local candidates included remote dormant movers or zero identity: %v", got)
	}
	storage.set(5001, monster.PendingMover{Pose: monster.Pose{RegionID: viewer.RegionID}}.Expand())
	count := 0
	for range storage.candidates(viewer) {
		count++
		break
	}
	if count != 1 {
		t.Fatalf("candidate iterator did not stop: %d", count)
	}
	for _, dungeon := range []uint16{0xe060, 0xe061} {
		storage.set(uint32(dungeon), monster.PendingMover{Pose: monster.Pose{RegionID: dungeon}}.Expand())
	}
	if got := slices.Collect(storage.candidates(worldgeom.RegionXZ{RegionID: 0xe060, X: -10000})); !reflect.DeepEqual(got, []uint32{0xe060}) {
		t.Fatalf("dungeon candidates crossed region or outdoor plane: %v", got)
	}
}

/*
================
filteredMoverInterest
================
*/
func filteredMoverInterest(storage *moverStorage, viewer worldgeom.RegionXZ, now int64) []uint32 {
	var out []uint32
	for gid := range storage.candidates(viewer) {
		row, exists := storage.lookup(gid)
		if !exists {
			continue
		}
		actualLivePose := row.LivePoseAt(now, nil)
		position := worldgeom.RegionXZ{RegionID: actualLivePose.RegionID, X: actualLivePose.X, Z: actualLivePose.Z}
		if worldgeom.InterestVisible(viewer, position) {
			out = append(out, gid)
		}
	}
	slices.Sort(out)
	return out
}

/*
================
assertMoverInterestMatchesLivePose
================
*/
func assertMoverInterestMatchesLivePose(t *testing.T, storage *moverStorage, viewer worldgeom.RegionXZ, now int64) {
	t.Helper()
	var want []uint32
	for gid, row := range storage.values() {
		actualLivePose := row.LivePoseAt(now, nil)
		position := worldgeom.RegionXZ{RegionID: actualLivePose.RegionID, X: actualLivePose.X, Z: actualLivePose.Z}
		if worldgeom.InterestVisible(viewer, position) {
			want = append(want, gid)
		}
	}
	slices.Sort(want)
	if got := filteredMoverInterest(storage, viewer, now); !reflect.DeepEqual(got, want) {
		t.Fatalf("at %d viewer %+v indexed %v, actual live-pose scan %v", now, viewer, got, want)
	}
}
