/*
===========================================================================

owner_follow_test.go - original-machine steering and formation boundaries

===========================================================================
*/
package monster

import (
	"encoding/json"
	"math"
	worldgeom "opensro.online/server/internal/game/world"
	"os"
	"testing"
)

/*
================
TestOwnerFollowOriginalMachineSteering

549F80 and its original vector/CRT helpers execute in the fixture generator.
Only the timer, formation goal provider and command sinks are injected.
================
*/
func TestOwnerFollowOriginalMachineSteering(t *testing.T) {
	data, err := os.ReadFile("testdata/owner_follow_native.json")
	if err != nil {
		t.Fatal(err)
	}
	var corpus struct {
		Cases []struct {
			Owner, Slot, Old, OwnerGoal [2]float64
			Heading                     uint16
			Moving, OwnerMoving         bool
			Result                      int
			Motion                      []float32
		}
	}
	if err := json.Unmarshal(data, &corpus); err != nil {
		t.Fatal(err)
	}
	if len(corpus.Cases) != 384 {
		t.Fatal("incomplete native corpus", len(corpus.Cases))
	}
	pose := func(v [2]float64) Pose {
		return Pose{RegionID: 0x62a8, X: float64(float32(v[0])), Z: float64(float32(v[1]))}
	}
	for i, c := range corpus.Cases {
		live := pose([2]float64{})
		live.Heading = c.Heading
		got := NativeOwnerFollowMotion(OwnerFollowInput{Live: live, Owner: pose(c.Owner), SlotGoal: pose(c.Slot), OldGoal: pose(c.Old), OwnerGoal: pose(c.OwnerGoal), Moving: c.Moving, OwnerMoving: c.OwnerMoving})
		if got.Satisfied != (c.Result == 2) || got.Move != (len(c.Motion) != 0) {
			t.Fatalf("case %d: %+v native %+v", i, got, c)
		}
		if !got.Move {
			continue
		}
		values := []float32{float32(got.Motion.X), 0, float32(got.Motion.Z), float32(got.Motion.Distance)}
		for j, v := range values {
			if math.Float32bits(v) != math.Float32bits(c.Motion[j]) {
				t.Fatalf("case %d component %d: %08x != %08x", i, j, math.Float32bits(v), math.Float32bits(c.Motion[j]))
			}
		}
	}
}

/*
================
TestOwnerFollowReservationsRetainUnreservedDirection
================
*/
func TestOwnerFollowReservationsRetainUnreservedDirection(t *testing.T) {
	var slots ApproachSlots
	live, owner := Pose{RegionID: 0x62a8, X: 200}, Pose{RegionID: 0x62a8}
	want := []int{0, 1, 7, 2, 6, 3, 5, 4}
	for i, slot := range want {
		if got := slots.AssignOwnerFollow(uint32(i+1), -1, live, owner); got != slot {
			t.Fatalf("member %d slot %d", i, got)
		}
	}
	before := slots
	if got := slots.AssignOwnerFollow(9, -1, live, owner); got != -1 || slots != before {
		t.Fatal("full formation replaced remembered slot", got, slots)
	}
	slots.Release(3)
	if got := slots.AssignOwnerFollow(9, -1, live, owner); got != 7 {
		t.Fatal("released slot not reused", got)
	}
	if got := slots.AssignOwnerFollow(1, 0, live, owner); got != 0 {
		t.Fatal("reassignment leaked own reservation", got)
	}
}

/*
================
TestOwnerFollowSurfaceProbeAndRounding
================
*/
func TestOwnerFollowSurfaceProbeAndRounding(t *testing.T) {
	owner := Pose{RegionID: 0x62a8, X: 100, Y: 0, Z: 100}
	normalize := func(p Pose) Pose { return p }
	for _, refuse := range []int{0, 3, 50} {
		calls := 0
		goal := NativeOwnerFollowGoal(OwnerFormationGoal{Owner: owner, Slot: 0, BodyRadius: 10, Surface: func(p Pose) (Pose, bool) { calls++; return p, calls > refuse }, Normalize: normalize})
		if calls > 50 || calls <= refuse && refuse < 50 {
			t.Fatal("probe bound", refuse, calls)
		}
		if refuse == 50 && calls != 50 {
			t.Fatal("incomplete exhaustion", calls)
		}
		if math.IsNaN(goal.X) || math.IsNaN(goal.Z) {
			t.Fatal("invalid goal", goal)
		}
	}
	for _, c := range []struct {
		coordinate, relative float32
		want                 float64
	}{{10.5, 2, 11}, {10.5, -2, 9}, {-10.5, 2, -10}, {-10.5, -2, -12}, {10.001, 2, 10}, {10.5, 0.01, 9}} {
		if got := ownerFollowRound(c.coordinate, c.relative); got != c.want {
			t.Fatal(c, got)
		}
	}
}

/*
================
TestOwnerFollowCatchUpThreshold
================
*/
func TestOwnerFollowCatchUpThreshold(t *testing.T) {
	for _, c := range []struct {
		distance float32
		running  bool
		want     float32
	}{{79.999, true, 100}, {80, true, 125}, {80, false, 20}} {
		current := float32(100)
		if !c.running {
			current = 20
		}
		got := NativeOwnerFollowRunFactor(OwnerFollowSpeed{Distance: c.distance, OwnerCurrent: current, OwnerRun: 100, AuthoredRun: 100, OwnerRunning: c.running})
		if got != c.want {
			t.Fatal(c, got)
		}
	}
}

/*
================
TestOwnerFollowOriginalMachineSurface
================
*/
func TestOwnerFollowOriginalMachineSurface(t *testing.T) {
	data, err := os.ReadFile("testdata/owner_follow_surface_native.json")
	if err != nil {
		t.Fatal(err)
	}
	var corpus struct {
		Cases []struct {
			Slot, Radius, Refuse, Probes int
			Height                       float64
			Owner, Goal                  [3]float64
			Region                       uint16
		}
	}
	if err := json.Unmarshal(data, &corpus); err != nil {
		t.Fatal(err)
	}
	if len(corpus.Cases) != 432 {
		t.Fatal("incomplete surface corpus", len(corpus.Cases))
	}
	for i, c := range corpus.Cases {
		owner := Pose{RegionID: 0x62a8, X: c.Owner[0], Y: c.Owner[1], Z: c.Owner[2]}
		probes := 0
		got := NativeOwnerFollowGoal(OwnerFormationGoal{Owner: owner, Slot: c.Slot, BodyRadius: float32(c.Radius), Surface: func(p Pose) (Pose, bool) {
			probes++
			valid := probes > c.Refuse
			if valid {
				p.Y = c.Height
			}
			return p, valid
		}, Normalize: func(p Pose) Pose {
			v := worldgeom.NormalizeOutdoor(worldgeom.RegionXZ{RegionID: p.RegionID, X: p.X, Z: p.Z})
			p.RegionID, p.X, p.Z = v.RegionID, v.X, v.Z
			return p
		}})
		if probes != c.Probes || got.RegionID != c.Region {
			t.Fatalf("case %d probes %d/%d region %x/%x", i, probes, c.Probes, got.RegionID, c.Region)
		}
		for axis, v := range []float64{got.X, got.Y, got.Z} {
			if math.Float32bits(float32(v)) != math.Float32bits(float32(c.Goal[axis])) {
				t.Fatalf("case %d axis %d got %08x native %08x input %+v", i, axis, math.Float32bits(float32(v)), math.Float32bits(float32(c.Goal[axis])), c)
			}
		}
	}
}
