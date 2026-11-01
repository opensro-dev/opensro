/*
===========================================================================

acquisition_churn_test.go - the acquisition queries and NPC move frames
without per-call garbage (#1820 part C)

The ordinary and job acquisition queries used to copy every in-sight
player pose into a slice sized to the whole division and sort it through
sort.Slice; the job query also built a GID table of the division. They
now hold candidate indices in a stack buffer. These tests pin the result
against the previous implementation, kept here verbatim as the
reference, across random populations (companions, jobs, statuses, poses
inside and outside the sampled blocks, more candidates than the stack
buffer holds), and pin the allocation counts the change exists for.

===========================================================================
*/
package simulation

import (
	"math"
	"math/rand"
	"reflect"
	"sort"
	"testing"

	"opensro.online/server/internal/game/item/wire"
	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/monster"
)

/*
================
referenceOrdinaryPlayerAcquisition

ordinaryPlayerAcquisition as it stood on main at 07a8ca07c, the
behaviour the index rewrite must keep.
================
*/
func referenceOrdinaryPlayerAcquisition(actor monster.Instance, from monster.Pose, players []playerPose, sightRange float64) (playerPose, bool) {
	if len(players) == 0 {
		return playerPose{}, false
	}
	sightFloat := float32(sightRange)
	if math.IsNaN(float64(sightFloat)) || sightFloat < 0 || sightFloat > 1920 {
		return playerPose{}, false
	}
	sight := uint32(sightFloat)
	blocks := acquisitionBlocks(from, sight)
	type candidate struct {
		player playerPose
		block  int
	}
	ordered := make([]candidate, 0, len(players))
	for _, player := range players {
		if player.OwnerGid != 0 {
			continue
		}
		block := worldgeom.InterestBlockAt(worldgeom.RegionXZ{
			RegionID: player.Pose.RegionID, X: float64(float32(player.Pose.X)), Z: float64(float32(player.Pose.Z)),
		})
		if order, exists := blocks.order(block); exists && player.Gid != 0 &&
			!actor.JobTargetRefused(player.JobState) &&
			monster.AllowsTargetStatus(actor.Ref.TidWord, actor.Nest.NativeTacticsFlags, player.NativeBodyStatus) &&
			ordinaryPlayerHostility(actor, player) {
			ordered = append(ordered, candidate{player: player, block: order})
		}
	}
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].block != ordered[j].block {
			return ordered[i].block < ordered[j].block
		}
		return ordered[i].player.Gid < ordered[j].player.Gid
	})
	var best playerPose
	var bestDistance uint32
	for _, candidate := range ordered {
		target, distance := nearestOwnerOrCompanion(actor, from, candidate.player, players)
		if acquisitionRankAccepts(best.Gid, bestDistance, distance, sight) {
			best, bestDistance = target, uint32(distance)
		}
	}
	if best.Gid == 0 {
		return playerPose{}, false
	}
	return mountedOwnerRedirect(best, players), true
}

/*
================
referenceJobQueryAcquisition

jobQueryAcquisition as it stood on main at 07a8ca07c.
================
*/
func referenceJobQueryAcquisition(actor monster.Instance, from monster.Pose, players []playerPose, sightRange float64, query jobQuery) (playerPose, bool) {
	sightFloat := float32(sightRange)
	if math.IsNaN(float64(sightFloat)) || sightFloat < 0 || sightFloat > 1920 {
		return playerPose{}, false
	}
	sight := uint32(sightFloat)
	blocks := acquisitionBlocks(from, sight)
	jobs := make(map[uint32]uint8, len(players))
	for _, p := range players {
		if p.OwnerGid == 0 {
			jobs[p.Gid] = p.JobState
		}
	}
	type candidate struct {
		player    playerPose
		block     int
		companion bool
		job       uint8
	}
	ordered := make([]candidate, 0, len(players))
	for _, p := range players {
		block := worldgeom.InterestBlockAt(worldgeom.RegionXZ{RegionID: p.Pose.RegionID, X: float64(float32(p.Pose.X)), Z: float64(float32(p.Pose.Z))})
		order, inside := blocks.order(block)
		if !inside || p.Gid == 0 || !monster.AllowsTargetStatus(actor.Ref.TidWord, actor.Nest.NativeTacticsFlags, p.NativeBodyStatus) {
			continue
		}
		job, companion := p.JobState, p.OwnerGid != 0
		if companion {
			owner, known := jobs[p.OwnerGid]
			if !known || !ordinaryCompanionHostility(actor, p) {
				continue
			}
			job = owner
		} else if !ordinaryPlayerHostility(actor, p) {
			continue
		}
		if !query.admits(job) || actor.JobTargetRefused(job) {
			continue
		}
		ordered = append(ordered, candidate{player: p, block: order, companion: companion, job: job})
	}
	sort.Slice(ordered, func(i, j int) bool {
		a, b := ordered[i], ordered[j]
		if a.block != b.block {
			return a.block < b.block
		}
		if a.companion != b.companion {
			return !a.companion
		}
		return a.player.Gid < b.player.Gid
	})
	var best candidate
	var bestDistance uint32
	held := false
	for _, c := range ordered {
		distance := float64(acquisitionDistance(from, c.player.Pose))
		if float64(sight) < distance {
			continue
		}
		take := !held
		if held {
			preferred, heldPreferred := query.prefers(c.job), query.prefers(best.job)
			take = preferred && !heldPreferred || preferred == heldPreferred && distance < float64(bestDistance)
		}
		if take {
			best, bestDistance, held = c, uint32(distance), true
		}
	}
	if !held {
		return playerPose{}, false
	}
	return mountedOwnerRedirect(best.player, players), true
}

// churnJobStates are the job states the random populations draw from.
var churnJobStates = [...]uint8{jobStateTrader, jobStateThief, jobStateHunter, jobStateSpecial}

/*
================
churnPopulation

A random division population around from: players with unique GIDs,
each with zero to two companions, scattered so some sit inside the
sampled blocks and some outside, with mixed jobs, body statuses and
mounts.
================
*/
func churnPopulation(rng *rand.Rand, from monster.Pose, owners int) []playerPose {
	players := make([]playerPose, 0, owners*2)
	nextGid := uint32(1)
	for range owners {
		owner := playerPose{
			Gid:              nextGid,
			JobState:         churnJobStates[rng.Intn(len(churnJobStates))],
			NativeBodyStatus: uint8(rng.Intn(3)),
			Mounted:          rng.Intn(4) == 0,
			Pose: Spawn{RegionID: from.RegionID,
				X: from.X + rng.Float64()*600 - 300, Z: from.Z + rng.Float64()*600 - 300},
		}
		nextGid++
		players = append(players, owner)
		for range rng.Intn(3) {
			players = append(players, playerPose{
				Gid:              nextGid,
				OwnerGid:         owner.Gid,
				Band:             uint8(rng.Intn(4)),
				NativeBodyStatus: uint8(rng.Intn(3)),
				Pose: Spawn{RegionID: from.RegionID,
					X: owner.Pose.X + rng.Float64()*20 - 10, Z: owner.Pose.Z + rng.Float64()*20 - 10},
			})
			nextGid++
		}
	}
	rng.Shuffle(len(players), func(i, j int) { players[i], players[j] = players[j], players[i] })
	return players
}

/*
================
churnActor

A monster drawn from the job families (ordinary, thief, hunter) the
acquisition admission distinguishes.
================
*/
func churnActor(rng *rand.Rand) monster.Instance {
	var actor monster.Instance
	actor.Ref.TidWord = 0x00c6
	actor.Ref.TypeID4 = uint8(1 + rng.Intn(3))
	return actor
}

/*
================
TestAcquisitionQueriesMatchTheReference

Every query answers exactly as the previous implementation did, over
random populations from empty to past the stack buffer's capacity.
================
*/
func TestAcquisitionQueriesMatchTheReference(t *testing.T) {
	rng := rand.New(rand.NewSource(1820))
	from := monster.Pose{RegionID: monsterTestRegion, X: 1000, Z: 1000}
	queries := []jobQuery{{}, {thieves: true}}
	for trial := range 4000 {
		owners := rng.Intn(3 * acquisitionCandidateStack)
		players := churnPopulation(rng, from, owners)
		actor := churnActor(rng)
		sight := float64(rng.Intn(400))
		want, wantOK := referenceOrdinaryPlayerAcquisition(actor, from, players, sight)
		got, gotOK := ordinaryPlayerAcquisition(actor, from, players, sight)
		if got != want || gotOK != wantOK {
			t.Fatalf("trial %d ordinary: got %d/%v, reference %d/%v", trial, got.Gid, gotOK, want.Gid, wantOK)
		}
		for _, query := range queries {
			want, wantOK = referenceJobQueryAcquisition(actor, from, players, sight, query)
			got, gotOK = jobQueryAcquisition(actor, from, players, sight, query)
			if got != want || gotOK != wantOK {
				t.Fatalf("trial %d job %+v: got %d/%v, reference %d/%v", trial, query, got.Gid, gotOK, want.Gid, wantOK)
			}
		}
	}
}

/*
================
TestAcquisitionQueriesDoNotAllocate

A crowded division with fewer in-sight candidates than the stack buffer
holds costs the queries no heap allocation.
================
*/
func TestAcquisitionQueriesDoNotAllocate(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	from := monster.Pose{RegionID: monsterTestRegion, X: 1000, Z: 1000}
	players := churnPopulation(rng, from, acquisitionCandidateStack/2)
	var actor monster.Instance
	actor.Ref.TidWord, actor.Ref.TypeID4 = 0x00c6, 1
	if allocs := testing.AllocsPerRun(100, func() { ordinaryPlayerAcquisition(actor, from, players, 300) }); allocs != 0 {
		t.Fatalf("ordinary acquisition allocated %.1f times per call", allocs)
	}
	if allocs := testing.AllocsPerRun(100, func() { jobQueryAcquisition(actor, from, players, 300, jobQuery{}) }); allocs != 0 {
		t.Fatalf("job acquisition allocated %.1f times per call", allocs)
	}
}

/*
================
TestNpcFrameBuildersSkipARosterWithoutPatrols

A roster in which no NPC patrols (the authored world roster) builds no
frames and allocates nothing; a patrolling roster is unchanged.
================
*/
func TestNpcFrameBuildersSkipARosterWithoutPatrols(t *testing.T) {
	still := DefaultNpcRoster()
	for index := range still {
		still[index].Patrol = false
	}
	if frames := NpcMoveFrames(still, NpcShopSpawn(), 3); frames != nil {
		t.Fatalf("still roster built %d move frames", len(frames))
	}
	if frames := NpcCorrectionFrames(still, NpcShopSpawn(), 3); frames != nil {
		t.Fatalf("still roster built %d corrections", len(frames))
	}
	if frames := NpcMoveStateFrames(still, 3); frames != nil {
		t.Fatalf("still roster built %d state refreshes", len(frames))
	}
	if allocs := testing.AllocsPerRun(100, func() { NpcMoveFrames(still, NpcShopSpawn(), 3) }); allocs != 0 {
		t.Fatalf("still roster allocated %.1f times per call", allocs)
	}
	roster := DefaultNpcRoster()
	frames := NpcMoveFrames(roster, NpcShopSpawn(), 3)
	want := referenceNpcMoveFrames(roster, NpcShopSpawn(), 3)
	if !reflect.DeepEqual(frames, want) {
		t.Fatalf("patrol roster frames differ from the reference: %+v vs %+v", frames, want)
	}
	if len(frames) != npcPatrolCount(roster) || len(frames) == 0 || cap(frames) != len(frames) {
		t.Fatalf("patrol roster built %d frames (cap %d), want %d", len(frames), cap(frames), npcPatrolCount(roster))
	}
}

/*
================
BenchmarkOrdinaryPlayerAcquisitionCrowd

One acquisition query over a 40-owner division (about 80 entries with
companions), the shape of the live shard's busy divisions.
================
*/
func BenchmarkOrdinaryPlayerAcquisitionCrowd(b *testing.B) {
	rng := rand.New(rand.NewSource(40))
	from := monster.Pose{RegionID: monsterTestRegion, X: 1000, Z: 1000}
	players := churnPopulation(rng, from, 40)
	var actor monster.Instance
	actor.Ref.TidWord, actor.Ref.TypeID4 = 0x00c6, 1
	b.ReportAllocs()
	for b.Loop() {
		ordinaryPlayerAcquisition(actor, from, players, 300)
	}
}

/*
================
BenchmarkJobQueryAcquisitionCrowd
================
*/
func BenchmarkJobQueryAcquisitionCrowd(b *testing.B) {
	rng := rand.New(rand.NewSource(40))
	from := monster.Pose{RegionID: monsterTestRegion, X: 1000, Z: 1000}
	players := churnPopulation(rng, from, 40)
	var actor monster.Instance
	actor.Ref.TidWord, actor.Ref.TypeID4 = 0x00c6, 1
	b.ReportAllocs()
	for b.Loop() {
		jobQueryAcquisition(actor, from, players, 300, jobQuery{})
	}
}

/*
================
BenchmarkNpcMoveFramesStillWorldRoster

The per-session NPC entity tick over a 1000-NPC roster in which nothing
patrols, the live world roster's shape.
================
*/
func BenchmarkNpcMoveFramesStillWorldRoster(b *testing.B) {
	template := DefaultNpcRoster()[0]
	template.Patrol = false
	roster := make([]NpcDef, 1000)
	for index := range roster {
		roster[index] = template
	}
	b.ReportAllocs()
	for b.Loop() {
		NpcMoveFrames(roster, NpcShopSpawn(), 3)
	}
}

/*
================
referenceNpcMoveFrames

NpcMoveFrames as it stood on main at 07a8ca07c: a slice sized to the
whole roster, patrolling or not.
================
*/
func referenceNpcMoveFrames(roster []NpcDef, anchor Spawn, tick int64) []Frame {
	frames := make([]Frame, 0, len(roster))
	for index := range roster {
		if !roster[index].Patrol {
			continue
		}
		npcAnchor := anchor
		npcAnchor.X += roster[index].SpawnOffsetX
		npcAnchor.Z += roster[index].SpawnOffsetZ
		state := ComputeNpcPatrolState(npcAnchor, tick)
		move := wire.ObjectSourceMove{Position: npcPatrolPose(state), Gid: roster[index].ObjectID}
		frames = append(frames, Frame{Opcode: wire.OpObjectSourceMove, Payload: move.Encode()})
	}
	return frames
}

/*
================
BenchmarkReferenceOrdinaryPlayerAcquisitionCrowd

The before column for BenchmarkOrdinaryPlayerAcquisitionCrowd.
================
*/
func BenchmarkReferenceOrdinaryPlayerAcquisitionCrowd(b *testing.B) {
	rng := rand.New(rand.NewSource(40))
	from := monster.Pose{RegionID: monsterTestRegion, X: 1000, Z: 1000}
	players := churnPopulation(rng, from, 40)
	var actor monster.Instance
	actor.Ref.TidWord, actor.Ref.TypeID4 = 0x00c6, 1
	b.ReportAllocs()
	for b.Loop() {
		referenceOrdinaryPlayerAcquisition(actor, from, players, 300)
	}
}

/*
================
BenchmarkReferenceJobQueryAcquisitionCrowd

The before column for BenchmarkJobQueryAcquisitionCrowd.
================
*/
func BenchmarkReferenceJobQueryAcquisitionCrowd(b *testing.B) {
	rng := rand.New(rand.NewSource(40))
	from := monster.Pose{RegionID: monsterTestRegion, X: 1000, Z: 1000}
	players := churnPopulation(rng, from, 40)
	var actor monster.Instance
	actor.Ref.TidWord, actor.Ref.TypeID4 = 0x00c6, 1
	b.ReportAllocs()
	for b.Loop() {
		referenceJobQueryAcquisition(actor, from, players, 300, jobQuery{})
	}
}

/*
================
BenchmarkReferenceNpcMoveFramesStillWorldRoster

The before column for BenchmarkNpcMoveFramesStillWorldRoster.
================
*/
func BenchmarkReferenceNpcMoveFramesStillWorldRoster(b *testing.B) {
	template := DefaultNpcRoster()[0]
	template.Patrol = false
	roster := make([]NpcDef, 1000)
	for index := range roster {
		roster[index] = template
	}
	b.ReportAllocs()
	for b.Loop() {
		referenceNpcMoveFrames(roster, NpcShopSpawn(), 3)
	}
}
