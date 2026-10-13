/*
===========================================================================

monster_interest_scan_test.go - the interest scan keeps its answers

populationInterestGIDs keeps a seen set only for movers the index marks
as listed in more than one region, and contains checks the hot tier
first. referenceInterestGIDs is the scan as it was; random populations and
viewers must get the same slice from both, and the benchmarks time the
average crowd and the worst case for duplicates.

===========================================================================
*/
package simulation

import (
	"fmt"
	"math/rand"
	"slices"
	"sort"
	"testing"

	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/monster"
)

// interestScanRegion is the centre of the test area; its neighbours make
// up the nine regions every outdoor viewer scans.
const interestScanRegion = 0x62a8

/*
================
referenceInterestGIDs

The scan before this change: candidates' seen set, both-tier contains.
================
*/
func referenceInterestGIDs(state *divisionMonsterState, viewer worldgeom.RegionXZ, nowMs int64) []uint32 {
	var out []uint32
	for gid := range state.movers.candidates(viewer) {
		_, hot := state.instances.hot[gid]
		_, cold := state.instances.cold[gid]
		if !hot && !cold {
			continue
		}
		pose, _ := state.movers.livePoseAt(gid, nowMs, nil)
		if worldgeom.InterestVisible(viewer, worldgeom.RegionXZ{RegionID: pose.RegionID, X: pose.X, Z: pose.Z}) {
			out = append(out, gid)
		}
	}
	for _, region := range RegionScopeRing(viewer.RegionID) {
		for _, gid := range state.byRegion[region] {
			if state.movers.has(gid) {
				continue
			}
			actor := state.instances.get(gid)
			position := worldgeom.RegionXZ{RegionID: actor.Spawn.RegionID, X: actor.Spawn.X, Z: actor.Spawn.Z}
			if worldgeom.InterestVisible(viewer, position) {
				out = append(out, gid)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

/*
================
interestScanPose

A pose in one of the 5x5 regions around the centre, or in a dungeon.
================
*/
func interestScanPose(r *rand.Rand) monster.Pose {
	if r.Intn(12) == 0 {
		return monster.Pose{RegionID: 0x8000 | uint16(1+r.Intn(2)), X: r.Float64() * 1920, Z: r.Float64() * 1920}
	}
	region := uint16(interestScanRegion + r.Intn(5) - 2 + (r.Intn(5)-2)*0x100)
	return monster.Pose{RegionID: region, X: r.Float64() * 1920, Z: r.Float64() * 1920}
}

/*
================
interestScanPopulation

Hot and archived actors with resting and travelling movers (segments up
to four regions long), static entries without a mover, a duplicate static
listing, and actors removed with their index rows left behind.
================
*/
func interestScanPopulation(r *rand.Rand, count int) *divisionMonsterState {
	state := &divisionMonsterState{instances: newMonsterStorage(nil), movers: newMoverStorage(nil),
		byRegion: map[uint16][]uint32{}}
	state.instances.cold = map[uint32]archivedMonster{}
	for gid := uint32(1); gid <= uint32(count); gid++ {
		pose := interestScanPose(r)
		switch kind := r.Intn(10); {
		case kind < 2:
			// A static entry: no mover, its spawn is its position.
			spawn := monster.SpawnPoint{RegionID: pose.RegionID, X: pose.X, Z: pose.Z}
			state.instances.set(gid, monster.Instance{Gid: gid, CurrentHP: 1, Spawn: spawn})
			state.byRegion[pose.RegionID] = append(state.byRegion[pose.RegionID], gid)
			if r.Intn(20) == 0 {
				state.byRegion[pose.RegionID] = append(state.byRegion[pose.RegionID], gid)
			}
			continue
		case kind < 4:
			state.instances.cold[gid] = archivedMonster{}
		case kind < 5:
			// Removed: the mover row stays, the actor does not.
		default:
			state.instances.set(gid, monster.Instance{Gid: gid, CurrentHP: 1})
		}
		row := monster.MoverState{Pose: pose}
		if r.Intn(2) == 0 {
			to := pose
			to.X += float64(r.Intn(4)-1) * 1920 * r.Float64()
			to.Z += float64(r.Intn(4)-1) * 1920 * r.Float64()
			row.From, row.To = pose, to
			row.DepartMs = 1000
			row.ArriveMs = 1000 + int64(r.Intn(8000))
		}
		state.movers.set(gid, row)
	}
	return state
}

/*
================
interestScanViewer
================
*/
func interestScanViewer(r *rand.Rand) worldgeom.RegionXZ {
	pose := interestScanPose(r)
	if r.Intn(4) == 0 {
		pose.X = float64(r.Intn(2)) * 1919.9
	}
	return worldgeom.RegionXZ{RegionID: pose.RegionID, X: pose.X, Z: pose.Z}
}

/*
================
TestPopulationInterestScanMatchesTheSeenSetScan
================
*/
func TestPopulationInterestScanMatchesTheSeenSetScan(t *testing.T) {
	r := rand.New(rand.NewSource(13))
	visible, duplicates := 0, 0
	for population := 0; population < 40; population++ {
		state := interestScanPopulation(r, 300+r.Intn(900))
		for v := 0; v < 50; v++ {
			viewer, nowMs := interestScanViewer(r), int64(r.Intn(10000))
			want := referenceInterestGIDs(state, viewer, nowMs)
			got := populationInterestGIDs(state, viewer, nowMs)
			if !slices.Equal(got, want) {
				t.Fatalf("population %d viewer %+v at %d:\n got  %v\n want %v", population, viewer, nowMs, got, want)
			}
			visible += len(got)
			for i := 1; i < len(got); i++ {
				if got[i] == got[i-1] {
					duplicates++
				}
			}
		}
	}
	// The walk must reach visible actors and the static duplicates the
	// scan keeps, or equality proves little.
	if visible < 10000 || duplicates == 0 {
		t.Fatalf("only %d visible actors and %d kept duplicates", visible, duplicates)
	}
}

/*
================
interestScanCrowd

count resting movers spread over the nine regions around the centre, and
span of them travelling across every region boundary they can (the worst
case for duplicates: each is listed in up to nine regions).
================
*/
func interestScanCrowd(count, span int) *divisionMonsterState {
	r := rand.New(rand.NewSource(int64(count + span)))
	state := &divisionMonsterState{instances: newMonsterStorage(nil), movers: newMoverStorage(nil)}
	for gid := uint32(1); gid <= uint32(count); gid++ {
		region := uint16(interestScanRegion + r.Intn(3) - 1 + (r.Intn(3)-1)*0x100)
		pose := monster.Pose{RegionID: region, X: r.Float64() * 1920, Z: r.Float64() * 1920}
		row := monster.MoverState{Pose: pose}
		if int(gid) <= span {
			row.From = monster.Pose{RegionID: interestScanRegion - 0x101, X: 1, Z: 1}
			row.To = monster.Pose{RegionID: interestScanRegion + 0x101, X: 1919, Z: 1919}
			row.DepartMs, row.ArriveMs = 0, 1<<40
		}
		state.instances.set(gid, monster.Instance{Gid: gid, CurrentHP: 1})
		state.movers.set(gid, row)
	}
	return state
}

/*
================
BenchmarkPopulationInterestScan

One viewer's scan of 3000 movers, none, a tenth or all of them spanning
the nine regions: the seen-set scan against the marked scan.
================
*/
func BenchmarkPopulationInterestScan(b *testing.B) {
	viewer := worldgeom.RegionXZ{RegionID: interestScanRegion, X: 960, Z: 960}
	for _, span := range []int{0, 300, 3000} {
		state := interestScanCrowd(3000, span)
		if !slices.Equal(populationInterestGIDs(state, viewer, 500), referenceInterestGIDs(state, viewer, 500)) {
			b.Fatal("the scans disagree")
		}
		b.Run(fmt.Sprintf("seen-set/span%d", span), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				referenceInterestGIDs(state, viewer, 500)
			}
		})
		b.Run(fmt.Sprintf("marked/span%d", span), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				populationInterestGIDs(state, viewer, 500)
			}
		})
	}
}
