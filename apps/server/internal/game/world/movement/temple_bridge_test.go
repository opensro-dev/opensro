/*
===========================================================================

temple_bridge_test.go - Fertility Temple's two authored bridge connections

Exercise real navigation data at both entrances, in both directions. The
receiving triangle owns height; the terrain underneath never owns the seam.

===========================================================================
*/
package movement

import (
	"fmt"
	"math"
	"testing"

	"opensro.online/server/internal/game/world/simulation"
)

const templeBridgeAssetID = 1300

/*
================
TestFertilityTempleUnresolvedLinkStops
================
*/
func TestFertilityTempleUnresolvedLinkStops(t *testing.T) {
	v := realAuthorityValidator(t)
	const region = 0x6687
	set := v.objectNavSetForOffset(v.surfaceForRegion(region), 0, 0)
	bridge := set[0]
	link := bridge.placement.links[0]
	from := templePortalStand(region, bridge, link.edge)
	to := templePortalStand(region, set[link.target], link.targetEdge)
	owner, _, ok := v.ResolveNavOwner(from, simulation.NavOwner{})
	if !ok {
		t.Fatal("missing starting deck")
	}
	for i := range set {
		set[i].placement.links = nil
	}
	got := v.ClipMovementPathFrom(from, owner, to)
	if got.Outcome != ClipBlocked {
		t.Fatalf("unresolved source link crossed: %+v", got)
	}
}

/*
================
templePortalStand

An interior point in the triangle adjoining an authored portal.
================
*/
func templePortalStand(region uint16, obj resolvedObjectNav, edge int) simulation.Spawn {
	p, m := obj.placement, obj.meshes[0]
	cell := int(m.outline.srcCell[edge])
	x, z := objectCellCentroid2D(m, cell)
	y, _ := objectCellPlaneYAt(m, cell, x, z)
	return simulation.NormalizeSpawnFrame(simulation.Spawn{
		RegionID: region,
		X:        math.Cos(p.yaw)*x - math.Sin(p.yaw)*z + p.x,
		Y:        y + p.y,
		Z:        math.Sin(p.yaw)*x + math.Cos(p.yaw)*z + p.z,
	})
}

/*
================
TestFertilityTempleBridgeConnections
================
*/
func TestFertilityTempleBridgeConnections(t *testing.T) {
	v := realAuthorityValidator(t)
	for _, region := range []uint16{0x6687, 0x6686} {
		t.Run(fmt.Sprintf("%04x", region), func(t *testing.T) {
			surface := v.surfaceForRegion(region)
			if surface == nil {
				t.Fatal("missing temple surface")
			}
			set := v.objectNavSetForOffset(surface, 0, 0)
			crossings := 0
			for _, bridge := range set {
				if bridge.placement.assetID != templeBridgeAssetID {
					continue
				}
				for _, link := range bridge.placement.links {
					if link.target == 0xffff {
						continue
					}
					for _, temple := range set {
						if temple.placement.ordinal != link.target {
							continue
						}
						crossings++
						a := templePortalStand(region, bridge, link.edge)
						b := templePortalStand(region, temple, link.targetEdge)
						for direction, pair := range [][2]simulation.Spawn{{a, b}, {b, a}} {
							t.Run(fmt.Sprint(direction), func(t *testing.T) {
								owner, _, ok := v.ResolveNavOwner(pair[0], simulation.NavOwner{})
								if !ok || owner.Kind != simulation.NavOwnerObject {
									t.Fatal("missing starting deck")
								}
								got := v.ClipMovementPathFrom(pair[0], owner, pair[1])
								if got.Outcome != ClipArrived || got.RestOwner.Kind != simulation.NavOwnerObject || math.Abs(got.Rest.Y-pair[1].Y) > 0.01 || math.Hypot(got.Rest.X-pair[1].X, got.Rest.Z-pair[1].Z) > 0.01 {
									t.Fatalf("bridge crossing from %+v to %+v: %+v", pair[0], pair[1], got)
								}
								for _, dt := range []int64{16, 100} {
									t.Run(fmt.Sprint(dt), func(t *testing.T) { templeFiniteCrossing(t, v, pair, dt) })
								}
							})
						}
					}
				}
			}
			if crossings != 1 {
				t.Fatalf("expected one connected bridge, got %d", crossings)
			}
		})
	}
}

/*
================
templeFiniteCrossing
================
*/
func templeFiniteCrossing(t *testing.T, v *WaterValidator, pair [2]simulation.Spawn, dt int64) {
	t.Helper()
	rt := &Runtime{Nav: v, ClientClip: &ClientClip{Mode: ClipApply, Validator: v}, PathGuard: &PathGuard{Mode: PathGuardEnforce, Validator: v}}
	now := int64(1000)
	store := simulation.NewWorldStore()
	store.ConfigureGroundWalk(simulation.GroundWalkConfig{Now: func() int64 { return now }, Step: func(from simulation.Spawn, owner simulation.NavOwner, to simulation.Spawn) (simulation.Spawn, simulation.NavOwner, bool) {
		accepted, next, blocked := rt.groundStep(from, owner, to)
		return accepted, next, blocked
	}})
	seed := func() simulation.WorldState { return simulation.DefaultWorldState(pair[0]) }
	store.Update("temple", seed, func(w *simulation.WorldState) {
		w.Run = 100
		w.Spawn = pair[1]
		w.MoveSegment = w.TravelSegment(pair[0], pair[1], simulation.RunMode, now)
	})
	for i := 0; i < 200; i++ {
		now += dt
		state := store.Snapshot("temple", seed)
		pose := state.PersistedSpawn()
		if pose.Y < math.Min(pair[0].Y, pair[1].Y)-1 {
			t.Fatalf("finite walk fell under connection: %+v", pose)
		}
		if !state.GroundActive() {
			if math.Hypot(pose.X-pair[1].X, pose.Z-pair[1].Z) > .5 || state.GoalOwner().Kind != simulation.NavOwnerObject {
				t.Fatalf("finite walk failed to cross: %+v owner=%+v target=%+v", pose, state.GoalOwner(), pair[1])
			}
			return
		}
	}
	t.Fatal("finite walk did not finish")
}
