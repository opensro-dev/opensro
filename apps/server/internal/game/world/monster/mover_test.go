/*
===========================================================================

mover_test.go - MoverState.LivePoseAt interpolation and surface fallback tests

Verifies mid-flight terrain height resolution, chord fallback on missing
ground or mid-chord navigation gaps, and adjacent outdoor sector seams.

===========================================================================
*/
package monster

import (
	"math"
	"testing"
)

/*
================
inFlightMover

The BUG-8 second-injection-path contract (board seq735/seq779): a straight
Y-lerp between terrain-resolved endpoints describes a CHORD - below ground
over a rise, above it over a dip. Mid-flight poses must resolve the
interpolated XZ through the injected ground source; the settled and idle
branches stay untouched (their Y values are already terrain truth - the
BUG-12 commitSegment quantise block and the bootstrap nest lift).
================
*/
func inFlightMover() MoverState {
	return MoverState{
		mode: MoverWandering,
		Pose: Pose{RegionID: 25000, X: 100, Y: 10, Z: 100},
		From: Pose{RegionID: 25000, X: 100, Y: 10, Z: 100},
		To:   Pose{RegionID: 25000, X: 200, Y: 30, Z: 100, Heading: 0x4000},
		// 10s segment: nowMs=5000 sits exactly mid-chord.
		DepartMs: 0,
		ArriveMs: 10000,
	}
}

/*
================
TestLivePoseAtResolvesInterpolatedXZOntoTerrain
================
*/
func TestLivePoseAtResolvesInterpolatedXZOntoTerrain(t *testing.T) {
	mover := inFlightMover()

	var sampledRegion uint16
	var sampledX, sampledZ float64
	ground := func(pose Pose) (float64, bool) {
		sampledRegion, sampledX, sampledZ = pose.RegionID, pose.X, pose.Z
		return 55.5, true
	}

	pose := mover.LivePoseAt(5000, ground)
	if pose.X != 150 || pose.Z != 100 {
		t.Fatalf("mid-chord XZ = (%v, %v), want (150, 100)", pose.X, pose.Z)
	}
	if pose.Y != 55.5 {
		t.Errorf("mid-chord Y = %v, want the terrain sample 55.5 (chord Y 20 must not ship)", pose.Y)
	}
	if sampledRegion != 25000 || sampledX != 150 || sampledZ != 100 {
		t.Errorf("ground sampled at (%d, %v, %v), want the INTERPOLATED XZ (25000, 150, 100)",
			sampledRegion, sampledX, sampledZ)
	}
}

/*
================
TestLivePoseAtKeepsChordYOnNilOrMissingGround
================
*/
func TestLivePoseAtKeepsChordYOnNilOrMissingGround(t *testing.T) {
	mover := inFlightMover()

	// nil resolver = the pre-injection behaviour, byte-for-byte.
	if pose := mover.LivePoseAt(5000, nil); pose.Y != 20 {
		t.Errorf("nil resolver mid-chord Y = %v, want the lerped 20", pose.Y)
	}
	// A miss (unloaded region bundle) must also keep the lerp - a zeroed Y
	// here would floor-slam the monster on a region-load race.
	miss := func(Pose) (float64, bool) { return 0, false }
	if pose := mover.LivePoseAt(5000, miss); pose.Y != 20 {
		t.Errorf("resolver-miss mid-chord Y = %v, want the lerped 20", pose.Y)
	}
}

/*
================
TestLivePoseAtKeepsChordYWhenAdoptedNavigationSampleMissesMidChord
================
*/
func TestLivePoseAtKeepsChordYWhenAdoptedNavigationSampleMissesMidChord(t *testing.T) {
	mover := inFlightMover()
	mover.AdoptNavigation(NewNavigationPath(mover.From, mover.To, mover.To, 0, func(float64, Pose) (float64, bool) {
		return 0, false
	}))
	if pose := mover.LivePoseAt(5000, nil); pose.Y != 20 {
		t.Fatalf("navigation-miss mid-chord Y = %v, want the lerped 20", pose.Y)
	}
}

/*
================
TestLivePoseAtNeverResolvesSettledOrIdleBranches
================
*/
func TestLivePoseAtNeverResolvesSettledOrIdleBranches(t *testing.T) {
	poison := func(Pose) (float64, bool) {
		return math.Inf(1), true
	}

	// Settled branch: arrived segment returns To untouched - To.Y is the
	// terrain height at its own quantised XZ (BUG-12 block) and re-resolving
	// would change settle-adjacent wire values.
	settled := inFlightMover()
	if pose := settled.LivePoseAt(20000, poison); pose != settled.To {
		t.Errorf("settled pose = %+v, want To %+v untouched", pose, settled.To)
	}

	// Idle branch: no segment ever committed - Pose returns untouched (the
	// bootstrap-lifted nest anchor).
	idle := MoverState{mode: MoverIdle, Pose: Pose{RegionID: 25000, X: 1, Y: 2, Z: 3}}
	if pose := idle.LivePoseAt(5000, poison); pose != idle.Pose {
		t.Errorf("idle pose = %+v, want Pose %+v untouched", pose, idle.Pose)
	}
}

/*
================
TestLivePoseAtInterpolatesAcrossAdjacentRegionSeam
================
*/
func TestLivePoseAtInterpolatesAcrossAdjacentRegionSeam(t *testing.T) {
	const west = uint16(0x3230)
	const east = uint16(0x3231)
	mover := MoverState{
		mode:     MoverChasing,
		From:     Pose{RegionID: west, X: 1910, Y: 10, Z: 100},
		To:       Pose{RegionID: east, X: 10, Y: 30, Z: 100, Heading: 0x4000},
		DepartMs: 1000,
		ArriveMs: 3000,
	}

	mid := mover.LivePoseAt(2000, nil)
	if mid.RegionID != east || mid.X != 0 || mid.Y != 20 || mid.Z != 100 {
		t.Fatalf("cross-region midpoint = %+v, want east-region local (0,20,100)", mid)
	}
	before := mover.LivePoseAt(500, nil)
	if before.RegionID != west || before.X != 1910 || before.Y != 10 || before.Z != 100 {
		t.Fatalf("pre-departure pose = %+v, want exact departure pose", before)
	}
}

/*
================
TestLivePoseAtHandsTheResolverTheChordPose

The #1695 resolver reads the interpolated pose itself (its Y is the
chord height the plane's nearest-surface rule selects by), not the
segment endpoints.
================
*/
func TestLivePoseAtHandsTheResolverTheChordPose(t *testing.T) {
	mover := inFlightMover()

	var gotPose Pose
	ground := func(pose Pose) (float64, bool) {
		gotPose = pose
		return 55.5, true
	}
	pose := mover.LivePoseAt(5000, ground)
	// The chord midpoint of the in-flight segment: XZ (150, 100), chord
	// height 20 between the endpoint heights 10 and 30.
	if gotPose.RegionID != 25000 || gotPose.X != 150 || gotPose.Z != 100 || gotPose.Y != 20 {
		t.Errorf("resolver pose = %+v, want the chord pose (25000, 150, 20, 100)", gotPose)
	}
	if pose.Y != 55.5 {
		t.Errorf("live Y = %v, want the resolver's 55.5", pose.Y)
	}
}

/*
================
TestLivePoseAtAdoptedPathOwnsTheHeight

An adopted path owns its segment's heights outright: the resolver is
never consulted on one, which is why the resolver carries no surface
owner (#1695).
================
*/
func TestLivePoseAtAdoptedPathOwnsTheHeight(t *testing.T) {
	mover := inFlightMover()
	path := NewNavigationPath(mover.From, mover.To, mover.To, 0, func(float64, Pose) (float64, bool) {
		return 42, true
	})
	mover.AdoptNavigation(path)

	calls := 0
	ground := func(Pose) (float64, bool) {
		calls++
		return 55.5, true
	}
	pose := mover.LivePoseAt(5000, ground)
	if calls != 0 {
		t.Errorf("resolver consulted %d times on an adopted path, want 0", calls)
	}
	if pose.Y != 42 {
		t.Errorf("live Y = %v, want the adopted path's 42", pose.Y)
	}
}
