/*
===========================================================================

petformation_bench_test.go - the owner-follow formation cost on the real clip

Live, advancePets leads the tick (2026-11-03 profile, #2139): most of it
is the formation step paying the full movement clip for every pet
trailing a moving owner. The pet fixtures stub that constraint away, so
this bench wires the real authority validator (the production
ConstrainMovement and WalkableSpawnHeightAt) under one follower
trailing a running owner it can never catch: every 100 ms formation
step pays what production pays. One op is one pet's formation step.

Production preloads the validator's surface and object-nav caches at
boot, so the warm-up runs the chase until the cache-miss counter stays
flat for a full pass of the strip; the timed loop then prices the warm
path, and reports any timed-loop miss as cachemiss/op (steady state is
zero).

===========================================================================
*/
package action

import (
	"testing"

	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/movement"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/testsupport/gamedatatest"
)

// The chase shape: the owner runs east at 40 units a second, the pet
// follows at 30, so the pet trails forever and no formation step is
// ever satisfied. Both run on the Jangan field (0x62A8) the fixtures
// stand on; the chase restarts before the owner leaves the sector.
const (
	formationChaseStartMs = 1_000_000
	formationChaseStepMs  = cosFollowIntervalMs
	formationOwnerStep    = 4.0
	formationPetSpeed     = 30.0
	formationRestartX     = 1700.0
)

/*
================
BenchmarkPetFormationChase

Reports ns, bytes and allocations per 100 ms formation step of one
pet trailing a moving owner on the real movement clip.
================
*/
func BenchmarkPetFormationChase(b *testing.B) {
	validator := movement.NewAuthorityValidator(gamedatatest.Paths(b).WorldAuthorityDir)
	clipRuntime := &movement.Runtime{ClientClip: &movement.ClientClip{Mode: movement.ClipApply, Validator: validator}}
	surfaceCalls, constrainCalls := 0, 0
	surface := func(region uint16, x, y, z float64) (float64, bool) {
		surfaceCalls++
		return validator.WalkableSpawnHeightAt(region, x, y, z)
	}
	constrain := func(from, to simulation.Spawn) (simulation.Spawn, *simulation.MoveError) {
		constrainCalls++
		return clipRuntime.ConstrainMovement("formation-bench", from, to)
	}

	const gid = 7
	slots := &monster.ApproachSlots{}
	slot := 0
	relocated := false
	ownerStart := simulation.Spawn{RegionID: 0x62A8, X: 300, Z: 500}
	petStart := simulation.Spawn{RegionID: 0x62A8, X: 200, Z: 500}
	owner := simulation.WorldState{Spawn: ownerStart, Walk: 20, Run: 100, MovementMode: simulation.RunMode}
	follower := simulation.NewPetFollower(gid, petStart)

	step := func(now int64) []simulation.Frame {
		live := owner.LiveSpawnAt(now)
		if live.X > formationRestartX {
			owner = simulation.WorldState{Spawn: ownerStart, Walk: 20, Run: 100, MovementMode: simulation.RunMode}
			slots.Release(gid)
			follower = simulation.NewPetFollower(gid, petStart)
			live = ownerStart
		}
		next := live
		next.X += formationOwnerStep
		owner.Spawn = next
		owner.MoveSegment = simulation.MoveSegmentForTravel(live, next, simulation.RunMode, now)
		return follower.FollowFormation(simulation.PetFormationStep{
			Relocated: &relocated, Owner: owner, Slots: slots, Slot: &slot,
			BodyRadius: 10, Speed: formationPetSpeed, Now: now,
			Surface: surface, Constrain: constrain,
		})
	}

	now := int64(formationChaseStartMs)
	issued := 0
	// Warm-up: run passes of the strip until the validator stops
	// resolving surfaces and nav sets from disk (the boot preload's
	// steady state), so the timed loop never pays a cold build.
	lastMisses := validator.NavCacheMisses()
	quietSteps := 0
	for i := 0; i < 20000 && quietSteps < 400; i++ {
		now += formationChaseStepMs
		if frames := step(now); len(frames) > 0 {
			issued++
		}
		if misses := validator.NavCacheMisses(); misses != lastMisses {
			lastMisses = misses
			quietSteps = 0
		} else {
			quietSteps++
		}
	}
	// The bench must measure the chase, not a settled formation: the
	// pet issued moves during warm-up (the seeds-reach-the-tick guard).
	if issued == 0 {
		b.Fatal("the pet never moved during warm-up: the bench would measure a settled formation")
	}
	if quietSteps < 400 {
		b.Fatal("the validator caches never warmed: the bench would measure cold disk builds")
	}
	missesBefore := validator.NavCacheMisses()
	surfaceBefore, constrainBefore := surfaceCalls, constrainCalls
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		now += formationChaseStepMs
		step(now)
	}
	b.StopTimer()
	// What one formation step pays its two navigation owners: the goal
	// probe's surface resolutions and the mover's clips.
	b.ReportMetric(float64(surfaceCalls-surfaceBefore)/float64(b.N), "surface/op")
	b.ReportMetric(float64(constrainCalls-constrainBefore)/float64(b.N), "constrain/op")
	if delta := validator.NavCacheMisses() - missesBefore; delta > 0 {
		b.ReportMetric(float64(delta)/float64(b.N), "cachemiss/op")
	}
}
