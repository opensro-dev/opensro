/*
===========================================================================

monster_interest_scan_test.go - the interest scan keeps its answers

populationInterestGIDs answers from the mover block index.
referenceInterestGIDs walks the candidate regions the way the scan did before
that index; random populations and viewers, queried at instants in any
order, must get the same slice from both, and the benchmarks time a leg.

===========================================================================
*/
package simulation

import (
	"fmt"
	"math/rand"
	"reflect"
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

/*
================
interestScanRow

A random mover row: resting, or travelling across up to four regions.
================
*/
func interestScanRow(r *rand.Rand) monster.MoverState {
	pose := interestScanPose(r)
	row := monster.MoverState{Pose: pose}
	if r.Intn(2) == 0 {
		to := pose
		to.X += float64(r.Intn(4)-1) * 1920 * r.Float64()
		to.Z += float64(r.Intn(4)-1) * 1920 * r.Float64()
		row.From, row.To = pose, to
		row.DepartMs = 1000
		row.ArriveMs = 1000 + int64(r.Intn(8000))
	}
	return row
}

/*
================
TestPopulationInterestScanFollowsResetMovers

Movers are re-set between scans, flipping between one region and several,
and removed and re-added: the index's multi-region marks must follow every
change, so the scan still equals the reference after each step.
================
*/
/*
================
BenchmarkPopulationInterestLeg

One leg of interest scans: fifty viewers over one crowd at one instant,
the shape runScopeVisibility pays per tick. The first viewer's scan is
the marked scan's cost; the other forty-nine are where a shared pose
memo (or its absence) shows.
================
*/
func BenchmarkPopulationInterestLeg(b *testing.B) {
	state := interestScanCrowd(3000, 300)
	viewers := make([]worldgeom.RegionXZ, 50)
	r := rand.New(rand.NewSource(7))
	for i := range viewers {
		viewers[i] = interestScanViewer(r)
	}
	for _, viewer := range viewers {
		if !slices.Equal(populationInterestGIDs(state, viewer, 500), referenceInterestGIDs(state, viewer, 500)) {
			b.Fatal("the scans disagree")
		}
	}
	b.ReportAllocs()
	nowMs := int64(500)
	for b.Loop() {
		nowMs += 100
		for _, viewer := range viewers {
			populationInterestGIDs(state, viewer, nowMs)
		}
	}
}

/*
================
TestPopulationInterestScanRetiresPosesOnReset

A scan warms whatever pose sharing the scan keeps; a mover re-set to a
pose outside the viewer's ring must leave the next scan at the same
instant. A pose memo that outlives a mover commit fails this test.
================
*/
func TestPopulationInterestScanRetiresPosesOnReset(t *testing.T) {
	state := interestScanCrowd(0, 0)
	viewer := worldgeom.RegionXZ{RegionID: interestScanRegion, X: 960, Z: 960}
	near := monster.Pose{RegionID: interestScanRegion, X: 960, Z: 960}
	// Same region (so the mover stays a scan candidate) but three
	// blocks over: only the pose read can take it out of the scan.
	far := monster.Pose{RegionID: interestScanRegion, X: 100, Z: 100}
	state.instances.set(1, monster.Instance{Gid: 1, CurrentHP: 1})
	state.movers.set(1, monster.MoverState{Pose: near})
	if got := populationInterestGIDs(state, viewer, 500); !slices.Equal(got, []uint32{1}) {
		t.Fatalf("warm scan = %v, want [1]", got)
	}
	state.movers.set(1, monster.MoverState{Pose: far})
	if got := populationInterestGIDs(state, viewer, 500); len(got) != 0 {
		t.Fatalf("scan after re-set = %v, want no gids (stale pose?)", got)
	}
	state.movers.remove(1)
	if got := populationInterestGIDs(state, viewer, 500); len(got) != 0 {
		t.Fatalf("scan after remove = %v, want no gids", got)
	}
}

/*
================
TestPopulationInterestScanRetiresMembershipOnCommit

A scan warms the per-block memo; spawns and removals committed between
two scans at the same instant must change the next scan. A memo keyed
on the instant alone (or on one store's writes alone) fails this test.
================
*/
func TestPopulationInterestScanRetiresMembershipOnCommit(t *testing.T) {
	state := interestScanCrowd(0, 0)
	viewer := worldgeom.RegionXZ{RegionID: interestScanRegion, X: 960, Z: 960}
	if got := populationInterestGIDs(state, viewer, 500); len(got) != 0 {
		t.Fatalf("warm scan = %v, want no gids", got)
	}
	// A static spawn lands the production way: instance row, then the
	// region index entry (no mover, so the static ring walk owns it).
	state.instances.set(1, monster.Instance{Gid: 1, CurrentHP: 1, Spawn: monster.SpawnPoint{RegionID: interestScanRegion, X: 960, Z: 960}})
	if state.byRegion == nil {
		state.byRegion = make(map[uint16][]uint32)
	}
	state.listRegionGid(interestScanRegion, 1)
	if got := populationInterestGIDs(state, viewer, 500); !slices.Equal(got, []uint32{1}) {
		t.Fatalf("scan after static spawn = %v, want [1] (stale memo?)", got)
	}
	state.instances.remove(1)
	if got := populationInterestGIDs(state, viewer, 500); len(got) != 0 {
		t.Fatalf("scan after static remove = %v, want no gids (stale memo?)", got)
	}
	// A mover committed far away stays out; re-committed inside the
	// ring it must appear at the same instant.
	farPose := monster.Pose{RegionID: interestScanRegion + 0x303, X: 960, Z: 960}
	nearPose := monster.Pose{RegionID: interestScanRegion, X: 960, Z: 960}
	state.instances.set(2, monster.Instance{Gid: 2, CurrentHP: 1})
	state.movers.set(2, monster.MoverState{Pose: farPose})
	if got := populationInterestGIDs(state, viewer, 500); len(got) != 0 {
		t.Fatalf("scan with far mover = %v, want no gids", got)
	}
	state.movers.set(2, monster.MoverState{Pose: nearPose})
	if got := populationInterestGIDs(state, viewer, 500); !slices.Equal(got, []uint32{2}) {
		t.Fatalf("scan after mover commit = %v, want [2] (stale memo?)", got)
	}
}

/*
================
BenchmarkPopulationInterestLegClustered

The clustered leg: fifty viewers standing in the centre block's
neighbourhood over one crowd, the town and event-wave shape. Viewers in
one interest block share one visible set, so the leg's cost should
track occupied blocks, not sessions.
================
*/
func BenchmarkPopulationInterestLegClustered(b *testing.B) {
	state := interestScanCrowd(3000, 300)
	viewers := make([]worldgeom.RegionXZ, 50)
	for i := range viewers {
		viewers[i] = worldgeom.RegionXZ{RegionID: interestScanRegion, X: 900 + float64(i%7)*20, Z: 900 + float64(i/7)*20}
	}
	for _, viewer := range viewers {
		if !slices.Equal(populationInterestGIDs(state, viewer, 500), referenceInterestGIDs(state, viewer, 500)) {
			b.Fatal("the scans disagree")
		}
	}
	b.ReportAllocs()
	nowMs := int64(500)
	for b.Loop() {
		nowMs += 100
		for _, viewer := range viewers {
			populationInterestGIDs(state, viewer, nowMs)
		}
	}
}

func TestPopulationInterestScanFollowsResetMovers(t *testing.T) {
	r := rand.New(rand.NewSource(17))
	state := interestScanPopulation(r, 600)
	flips := 0
	for step := 0; step < 4000; step++ {
		gid := uint32(1 + r.Intn(700))
		switch r.Intn(5) {
		case 0:
			state.movers.remove(gid)
		default:
			before := state.movers.has(gid) && len(slices.Collect(moverInterestBoundsFor(state.movers.get(gid)).regions())) > 1
			row := interestScanRow(r)
			state.movers.set(gid, row)
			if !state.instances.contains(gid) && r.Intn(2) == 0 {
				state.instances.set(gid, monster.Instance{Gid: gid, CurrentHP: 1})
			}
			if after := len(slices.Collect(moverInterestBoundsFor(row).regions())) > 1; after != before {
				flips++
			}
		}
		for v := 0; v < 3; v++ {
			viewer, nowMs := interestScanViewer(r), int64(r.Intn(10000))
			want := referenceInterestGIDs(state, viewer, nowMs)
			if got := populationInterestGIDs(state, viewer, nowMs); !slices.Equal(got, want) {
				t.Fatalf("step %d gid %d viewer %+v at %d:\n got  %v\n want %v", step, gid, viewer, nowMs, got, want)
			}
		}
	}
	if flips < 500 {
		t.Fatalf("only %d single/multi-region flips", flips)
	}
}

/*
================
TestPopulationInterestScanSurvivesTheDirtyBound

Past maxMoverBlockDirty the notes are dropped and the next scan rebuilds;
a population nobody viewed for a long burst must answer exactly anyway.
================
*/
func TestPopulationInterestScanSurvivesTheDirtyBound(t *testing.T) {
	r := rand.New(rand.NewSource(23))
	state := interestScanPopulation(r, 600)
	viewer := interestScanViewer(r)
	populationInterestGIDs(state, viewer, 500)
	for i := 0; i <= maxMoverBlockDirty; i++ {
		state.movers.set(uint32(1+r.Intn(600)), interestScanRow(r))
	}
	if state.movers.blocks.built {
		t.Fatal("the bound did not drop the notes")
	}
	if got, want := populationInterestGIDs(state, viewer, 500), referenceInterestGIDs(state, viewer, 500); !slices.Equal(got, want) {
		t.Fatalf("after the bound:\n got  %v\n want %v", got, want)
	}
}

/*
================
TestPopulationInterestScanHoldsAcrossQuietTicks

The point of the index: a tick in which nobody crossed a block edge
serves the memo without a rescan, and any change that can alter the
answer retires it. The memo map's identity tells which happened.
================
*/
func TestPopulationInterestScanHoldsAcrossQuietTicks(t *testing.T) {
	state := &divisionMonsterState{instances: newMonsterStorage(nil), movers: newMoverStorage(nil),
		byRegion: map[uint16][]uint32{}}
	pose := monster.Pose{RegionID: interestScanRegion, X: 960, Z: 960}
	viewer := worldgeom.RegionXZ{RegionID: interestScanRegion, X: 960, Z: 960}
	state.instances.set(1, monster.Instance{Gid: 1, CurrentHP: 1})
	state.movers.set(1, monster.MoverState{Pose: pose})
	state.instances.set(2, monster.Instance{Gid: 2, CurrentHP: 1})
	state.movers.set(2, monster.MoverState{Pose: pose})
	if got := populationInterestGIDs(state, viewer, 500); !slices.Equal(got, []uint32{1, 2}) {
		t.Fatalf("first scan = %v", got)
	}

	// A new instant and a commit that stays in its block keep the memo.
	first := reflect.ValueOf(state.interestMemo).Pointer()
	state.movers.set(1, monster.MoverState{Pose: monster.Pose{RegionID: interestScanRegion, X: 961, Z: 960}})
	state.instances.set(2, monster.Instance{Gid: 2, CurrentHP: 7})
	populationInterestGIDs(state, viewer, 600)
	if reflect.ValueOf(state.interestMemo).Pointer() != first {
		t.Fatal("a quiet tick retired the memo")
	}

	// A removed actor's mover row stays; the answer must still lose it.
	state.instances.remove(2)
	if got := populationInterestGIDs(state, viewer, 600); !slices.Equal(got, []uint32{1}) {
		t.Fatalf("after the actor left: %v", got)
	}

	// A mover leaving the viewer's blocks retires it too.
	far := monster.Pose{RegionID: interestScanRegion + 2, X: 960, Z: 960}
	state.movers.set(1, monster.MoverState{Pose: far})
	if got := populationInterestGIDs(state, viewer, 600); len(got) != 0 {
		t.Fatalf("after the mover left: %v", got)
	}
}

/*
================
TestPopulationInterestScanFollowsStaticChanges

A static entry joins and leaves through the region list and through
gaining a mover, and a moved spawn is read fresh: none may be masked by
the memo or by the static lists.
================
*/
func TestPopulationInterestScanFollowsStaticChanges(t *testing.T) {
	state := &divisionMonsterState{instances: newMonsterStorage(nil), movers: newMoverStorage(nil),
		byRegion: map[uint16][]uint32{}}
	viewer := worldgeom.RegionXZ{RegionID: interestScanRegion, X: 960, Z: 960}
	spawn := monster.SpawnPoint{RegionID: interestScanRegion, X: 960, Z: 960}
	state.instances.set(5, monster.Instance{Gid: 5, CurrentHP: 1, Spawn: spawn})
	if got := populationInterestGIDs(state, viewer, 500); len(got) != 0 {
		t.Fatalf("unlisted static visible: %v", got)
	}
	state.listRegionGid(interestScanRegion, 5)
	if got := populationInterestGIDs(state, viewer, 500); !slices.Equal(got, []uint32{5}) {
		t.Fatalf("listed static = %v", got)
	}
	away := spawn
	away.X = 0
	state.instances.set(5, monster.Instance{Gid: 5, CurrentHP: 1, Spawn: away})
	if got := populationInterestGIDs(state, viewer, 500); len(got) != 0 {
		t.Fatalf("static read its old spawn: %v", got)
	}
	state.instances.set(5, monster.Instance{Gid: 5, CurrentHP: 1, Spawn: spawn})
	state.removeRegionGid(interestScanRegion, 5)
	if got := populationInterestGIDs(state, viewer, 500); len(got) != 0 {
		t.Fatalf("unlisted static stayed: %v", got)
	}
	state.listRegionGid(interestScanRegion, 5)
	populationInterestGIDs(state, viewer, 500)
	// With a mover far away, the listing no longer places it.
	state.movers.set(5, monster.MoverState{Pose: monster.Pose{RegionID: interestScanRegion + 2, X: 960, Z: 960}})
	if got := populationInterestGIDs(state, viewer, 500); len(got) != 0 {
		t.Fatalf("a static with a mover stayed at its spawn: %v", got)
	}
	state.movers.remove(5)
	if got := populationInterestGIDs(state, viewer, 500); !slices.Equal(got, []uint32{5}) {
		t.Fatalf("a mover-less actor did not return to its spawn: %v", got)
	}
}
