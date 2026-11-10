/*
===========================================================================

worldstore_existing_test.go - the no-seed snapshot read into a retained slot

SnapshotExistingInto is SnapshotExisting for a per-tick reader that
keeps its slot across fills (the judged pet tick): same refusal on a
missing key, same ground advance, no clone allocated per fill.

===========================================================================
*/
package simulation

import (
	"testing"
)

/*
================
TestSnapshotExistingIntoRefillsRetainedSlot

A missing key refuses and leaves the slot untouched. A held key fills
the slot whole, and a refill reuses the slot's Ground and MoveSegment
buffers instead of replacing them - the no-allocation contract the
judged pet tick keeps its owner world under.
================
*/
func TestSnapshotExistingIntoRefillsRetainedSlot(t *testing.T) {
	st := NewWorldStore()
	key := WorldKey("division", "Jalk")
	dst := WorldState{Spawn: Spawn{RegionID: 7}}
	if st.SnapshotExistingInto(key, &dst) {
		t.Fatalf("missing key filled the slot")
	}
	if dst.Spawn.RegionID != 7 || dst.Ground != nil || dst.MoveSegment != nil {
		t.Fatalf("missing key touched the slot: %+v", dst)
	}
	spawn := Spawn{RegionID: 0x679A, X: 640, Z: 61.5}
	st.Snapshot(key, func() WorldState {
		return WorldState{Spawn: spawn, Ground: &GroundWalkState{Pose: spawn}, MoveSegment: &MoveSegment{From: spawn, StartedAtMs: 1000, ArrivesAtMs: 2000}}
	})
	if !st.SnapshotExistingInto(key, &dst) {
		t.Fatalf("held key refused the fill")
	}
	if dst.Spawn != spawn || dst.Ground == nil || dst.Ground.Pose != spawn || dst.MoveSegment == nil {
		t.Fatalf("fill: spawn %+v ground %+v segment %v", dst.Spawn, dst.Ground, dst.MoveSegment)
	}
	ground, segment := dst.Ground, dst.MoveSegment
	if !st.SnapshotExistingInto(key, &dst) {
		t.Fatalf("held key refused the refill")
	}
	if dst.Ground != ground || dst.MoveSegment != segment {
		t.Fatalf("refill replaced the slot buffers: ground %p (was %p), segment %p (was %p)", dst.Ground, ground, dst.MoveSegment, segment)
	}
	if dst.Spawn != spawn || dst.Ground.Pose != spawn {
		t.Fatalf("refill: spawn %+v ground %+v", dst.Spawn, dst.Ground)
	}
}
