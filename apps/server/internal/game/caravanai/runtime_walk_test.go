/*
===========================================================================

runtime_walk_test.go - AI caravan route walking tests (port-only)

Drives agents along the highway anchor chains through a straight-line
fake Walker (grid space, no nav surface) and proves the runtime's side
of the contract: timed steps, the ferry hop between two Ferry anchors,
delivery at the far terminal, and route rotation. The world-data proof
that every anchor hop is marchable lives in internal/game/world/movement.

===========================================================================
*/
package caravanai

import (
	"context"
	"math"
	"testing"

	worldgeom "opensro.online/server/internal/game/world"
)

// straightWalker is a test Walker: straight-line steps in grid space.
type straightWalker struct{}

/*
================
straightWalker.Step

Steps from toward to by at most maxDist grid units; arrival when the
remaining distance fits in the step.
================
*/
func (straightWalker) Step(from, to Pose, maxDist float64) (Pose, bool) {
	fromGrid := worldgeom.ExpandGrid(worldgeom.RegionXZ{RegionID: from.RegionID, X: float64(from.X), Z: float64(from.Z)})
	toGrid := worldgeom.ExpandGrid(worldgeom.RegionXZ{RegionID: to.RegionID, X: float64(to.X), Z: float64(to.Z)})
	dx, dz := toGrid.X-fromGrid.X, toGrid.Z-fromGrid.Z
	dist := math.Hypot(dx, dz)
	if dist <= maxDist {
		return to, true
	}
	ratio := maxDist / dist
	grid := worldgeom.GridXZ{X: fromGrid.X + dx*ratio, Z: fromGrid.Z + dz*ratio}
	regionID := uint16(int(grid.X/worldgeom.OutdoorRegionSize) | int(grid.Z/worldgeom.OutdoorRegionSize)<<8)
	local := worldgeom.LocalFromGrid(regionID, grid)
	return Pose{RegionID: regionID, X: float32(local.X), Y: from.Y, Z: float32(local.Z)}, false
}

/*
==================
TestAgentWalksRouteFerryHopAndDelivery

Trader_Jin walks Jangan->Donwhang anchor by anchor: the pose moves at
walk speed, the main crossing is one dock-to-dock hop (no walking
between the two Ferry anchors), and reaching the Donwhang terminal
completes the run and rotates the route back toward Jangan.
==================
*/
func TestAgentWalksRouteFerryHopAndDelivery(t *testing.T) {
	rt := NewRuntime(
		Config{Enabled: true, BrainMode: BrainHardcoded},
		nil,
		WorldHooks{Walker: straightWalker{}},
	)
	const div = "div"
	rt.EnsureDivisionRoster(div)

	ctx := context.Background()
	var prev Pose
	var hopped, reachedDock bool
	var delivered *AgentUnit
	nowMs := int64(0)
	for step := 0; step < 6000; step++ {
		nowMs += 1000
		_, snap, ok := rt.StepAgent(ctx, div, "[AI]Trader_Jin", nil, nowMs)
		if !ok || snap == nil {
			t.Fatalf("step %d: agent missing", step)
		}
		pose := Pose{RegionID: snap.RegionID, X: snap.X, Y: snap.Y, Z: snap.Z}
		if prev.RegionID == RegionJanganFerry && pose.RegionID == RegionDonwhangFerry {
			hopped = true
			if pose.X != waypointDonwhangFerry.X || pose.Z != waypointDonwhangFerry.Z {
				t.Fatalf("ferry hop landed at %+v, want the far dock %+v", pose, waypointDonwhangFerry)
			}
		}
		if pose.RegionID == RegionJanganFerry {
			reachedDock = true
		}
		prev = pose
		if snap.State == StateDelivering {
			delivered = snap
			break
		}
	}
	if delivered == nil {
		t.Fatalf("agent never delivered; last pose %+v", prev)
	}
	if !reachedDock {
		t.Fatal("agent never reached the Jangan ferry dock region on foot")
	}
	if !hopped {
		t.Fatal("agent never took the ferry hop between the dock anchors")
	}
	if delivered.RegionID != RegionDonwhangTown {
		t.Fatalf("delivered in region %d, want %d", delivered.RegionID, RegionDonwhangTown)
	}
	if delivered.OriginTown != TownDonwhang || delivered.DestTown != TownJangan {
		t.Fatalf("route did not rotate: %s -> %s", delivered.OriginTown, delivered.DestTown)
	}
}

/*
==================
TestAgentHoldsPositionWithoutWalker

With no Walker wired the runtime invents no movement: the agent keeps
its spawn pose and route index however long it is stepped.
==================
*/
func TestAgentHoldsPositionWithoutWalker(t *testing.T) {
	rt := NewRuntime(Config{Enabled: true, BrainMode: BrainHardcoded}, nil, WorldHooks{})
	const div = "div"
	roster := rt.EnsureDivisionRoster(div)
	var jin *AgentUnit
	for _, u := range roster {
		if u.Name == "[AI]Trader_Jin" {
			jin = u
		}
	}
	if jin == nil {
		t.Fatal("roster has no [AI]Trader_Jin")
	}

	ctx := context.Background()
	nowMs := int64(0)
	for step := 0; step < 50; step++ {
		nowMs += 1000
		_, snap, ok := rt.StepAgent(ctx, div, "[AI]Trader_Jin", nil, nowMs)
		if !ok || snap == nil {
			t.Fatalf("step %d: agent missing", step)
		}
		if snap.RegionID != jin.RegionID || snap.X != jin.X || snap.Z != jin.Z || snap.WaypointIndex != 0 {
			t.Fatalf("agent moved without a walker: %+v (spawn %+v)", snap, jin)
		}
	}
}

/*
==================
TestCaravansFlagOnAndOff

SRO_AI_CARAVANS off (unset, or any non-switch value) leaves the runtime
disabled; only an explicit switch value enables it.
==================
*/
func TestCaravansFlagOnAndOff(t *testing.T) {
	t.Setenv(EnvAICaravans, "")
	if cfg := ConfigFromEnv(); cfg.Enabled {
		t.Fatalf("unset %s enabled the caravans: %+v", EnvAICaravans, cfg)
	}
	t.Setenv(EnvAICaravans, "off")
	if cfg := ConfigFromEnv(); cfg.Enabled {
		t.Fatalf("%s=off enabled the caravans: %+v", EnvAICaravans, cfg)
	}
	t.Setenv(EnvAICaravans, "on")
	if cfg := ConfigFromEnv(); !cfg.Enabled {
		t.Fatalf("%s=on left the caravans disabled: %+v", EnvAICaravans, cfg)
	}
}
