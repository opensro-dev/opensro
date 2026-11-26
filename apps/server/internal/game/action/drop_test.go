package action

import (
	"errors"
	"math"
	"testing"
	"time"

	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

// THE BUG D REGRESSION PIN. Gold dropped mid-run lands where the character
// IS - the interpolated live position - never at the pathing destination.
// The drop is planned from LiveSpawnAt exactly as the handler must call it.
func TestGoldDropMidMoveLandsUnderfoot(t *testing.T) {
	from := simulation.Spawn{RegionID: 0x62A8, X: 960, Y: 20, Z: 458, Angle: 300}
	goal := simulation.Spawn{RegionID: 0x62A8, X: 1060, Y: 20, Z: 458, Angle: 300}
	startedAtMs := int64(1_000_000)

	segment := simulation.MoveSegmentForTravel(from, goal, simulation.RunMode, startedAtMs)
	if segment == nil {
		t.Fatal("the 100u hop produced no segment")
	}
	world := simulation.WorldState{Spawn: goal, MoveSegment: segment, MovementMode: simulation.RunMode}

	// Halfway through the 2s run: 50 units past the departure point.
	live := world.LiveSpawnAt(startedAtMs + 1000)

	ref := GoldHeapRef{RefObjID: 62, Codename: inventory.GoldHeapTier(1500), Tid1: 3, Tid2: 3, Tid3: 5, Tid4: 0}
	heap := PlanGoldDrop(ref, 1500, live, "asd", time.UnixMilli(startedAtMs+1000))

	if math.Abs(float64(heap.Position.X)-1010) > 0.5 {
		t.Fatalf("heap X = %v, want the live midpoint ~1010", heap.Position.X)
	}
	if math.Abs(float64(heap.Position.X)-goal.X) < 25 {
		t.Fatalf("heap X = %v landed at the move GOAL %v - the bug D regression", heap.Position.X, goal.X)
	}
	if heap.Position.RegionID != 0x62A8 || heap.Heading != 300 {
		t.Fatalf("heap placement = %+v, want the live region and the dropper's facing", heap)
	}
}

// A settled character (no segment) drops at the spawn plane - the two planes
// agree when nothing is in flight.
func TestGoldDropSettledLandsAtSpawn(t *testing.T) {
	spawn := simulation.Spawn{RegionID: 0x62A8, X: 960.5, Y: 20, Z: 458.25, Angle: 12}
	world := simulation.WorldState{Spawn: spawn, MovementMode: simulation.RunMode}

	live := world.LiveSpawnAt(5_000_000)
	heap := PlanGoldDrop(GoldHeapRef{RefObjID: 62, Tid1: 3, Tid2: 3, Tid3: 5}, 999, live, "asd", time.Now())

	if heap.Position.X != 960.5 || heap.Position.Z != 458.25 || heap.Y != 20 {
		t.Fatalf("settled drop = %+v, want the spawn placement", heap)
	}
}

func TestPlanGoldDropBuildsTheTierHeap(t *testing.T) {
	ref := GoldHeapRef{RefObjID: 62, Codename: "ITEM_ETC_GOLD_02", Tid1: 3, Tid2: 3, Tid3: 5, Tid4: 0}
	heap := PlanGoldDrop(ref, 1500, simulation.Spawn{RegionID: 0x62A8, X: 1, Z: 2}, "asd", time.Unix(1000, 0))

	if !wire.IsGoldBand(heap.TypeFlags) {
		t.Fatalf("heap word 0x%04X is not in the gold band", heap.TypeFlags)
	}
	if !heap.IsGold() || heap.GoldAmount != 1500 {
		t.Fatalf("heap = %+v, want a 1500-gold heap", heap)
	}
	if heap.Gid != 0 {
		t.Fatal("the plan pre-assigned a gid; the registry is the sole allocator")
	}
	if heap.StackCount != 0 {
		t.Fatal("a gold heap carries no stack count")
	}
	if inventory.GoldHeapTier(1500) != "ITEM_ETC_GOLD_02" {
		t.Fatal("the 1500-gold tier is not the medium heap")
	}

	// The registry stamps the 300000+ band gid.
	registry := grounditem.NewRegistry()
	stored := registry.Add("1", heap)
	if stored.Gid <= grounditem.GidBase {
		t.Fatalf("stored gid = %d, want above the %d band base", stored.Gid, grounditem.GidBase)
	}
	if stored.DroppedAt.IsZero() {
		t.Fatal("the stored heap lost its drop timestamp; the expiry sweep needs it")
	}
}

func TestPlanItemDropCarriesTheRow(t *testing.T) {
	row := inventory.Item{
		Slot: 20, RefObjID: 3630, Codename: "ITEM_ETC_HP_POTION_01",
		TypeFlags: wire.PackTypeFlags(3, 3, 1, 1), Plus: 2,
		VarianceBits: 0xABCD, Durability: 7, Quantity: 20,
	}
	at := simulation.Spawn{RegionID: 0x62A8, X: 960, Y: 20, Z: 458, Angle: 44}
	now := time.Unix(9000, 0)

	dropped := PlanItemDrop(row, 5, at, "asd", now)

	if dropped.RefObjID != 3630 || dropped.Codename != row.Codename || dropped.TypeFlags != row.TypeFlags {
		t.Fatalf("dropped = %+v, want the row's identity fields", dropped)
	}
	if dropped.Plus != 2 || dropped.VarianceBits != 0xABCD || dropped.Durability != 7 {
		t.Fatalf("dropped = %+v, want the row's variance fields", dropped)
	}
	if dropped.StackCount != 5 {
		t.Fatalf("stack count = %d, want the 5 dropped units", dropped.StackCount)
	}
	if !dropped.DroppedAt.Equal(now) || dropped.DroppedBy != "asd" {
		t.Fatalf("dropped = %+v, want the drop bookkeeping", dropped)
	}
	if dropped.Heading != 44 {
		t.Fatalf("heading = %d, want the dropper's facing 44", dropped.Heading)
	}

	// A zero count reads as the native minimum of one unit.
	if single := PlanItemDrop(row, 0, at, "asd", now); single.StackCount != 1 {
		t.Fatalf("zero-count drop carries %d, want 1", single.StackCount)
	}
}

func TestDropCoordinateClamp(t *testing.T) {
	at := simulation.Spawn{RegionID: 0x62A8, X: 1e9, Y: math.NaN(), Z: -1e9}
	heap := PlanGoldDrop(GoldHeapRef{RefObjID: 62, Tid1: 3, Tid2: 3, Tid3: 5}, 1, at, "asd", time.Now())

	if heap.Position.X != 0xFFFF {
		t.Fatalf("X = %v, want the 0xFFFF ceiling", heap.Position.X)
	}
	if heap.Position.Z != -0x8000 {
		t.Fatalf("Z = %v, want the -0x8000 floor", heap.Position.Z)
	}
	if heap.Y != 0 {
		t.Fatalf("Y = %v, want NaN read as 0", heap.Y)
	}
}

func TestMonsterDropScatterRarityBoundsAndIndependentDirections(t *testing.T) {
	at := simulation.Spawn{RegionID: 0x62A8, X: 960, Y: 20, Z: 458}
	item := PlanItemDrop(inventory.Item{RefObjID: 1}, 1, at, "Player", time.Unix(1, 0))
	for _, tc := range []struct {
		rarity   uint8
		min, max float64
	}{{0, 8, 20}, {1, 8, 20}, {3, 10, 30}, {4, 10, 40}, {5, 20, 100}, {8, 10, 30}} {
		for _, bound := range []struct {
			roll   uint32
			radius float64
		}{{0, tc.min}, {32767, tc.max}} {
			rt := &Runtime{DropRoll: dropRollSequence(0, bound.roll, 0)}
			got := rt.scatterMonsterDrop(item, at, tc.rarity)
			if got.Position.X != float32(at.X+bound.radius) || got.Position.Z != float32(at.Z) || got.Y != 20 {
				t.Fatalf("rarity %d: %+v", tc.rarity, got)
			}
		}
	}
	rt := &Runtime{DropRoll: dropRollSequence(0, 0, 0, 8192, 0, 16384)}
	first, second := rt.scatterMonsterDrop(item, at, 0), rt.scatterMonsterDrop(item, at, 0)
	if first.Position == second.Position || first.Heading == second.Heading {
		t.Fatal("independent drops collapsed")
	}
	if first.RefObjID != item.RefObjID || first.DroppedAt != item.DroppedAt {
		t.Fatal("placement changed item identity")
	}
}

func TestMonsterDropScatterNormalizesRegionAndFallsBackOnBlockedPlacement(t *testing.T) {
	at := simulation.Spawn{RegionID: 0x62A8, X: 1919, Y: 20, Z: 458}
	item := PlanItemDrop(inventory.Item{RefObjID: 1}, 1, at, "Player", time.Unix(1, 0))
	rt := &Runtime{DropRoll: dropRollSequence(0, 0, 0)}
	got := rt.scatterMonsterDrop(item, at, 0)
	if got.Position.RegionID != 0x62A9 || got.Position.X != 7 {
		t.Fatalf("cross-region drop %+v", got.Position)
	}
	// A refused placement is redrawn once from the death position: the
	// second attempt's spot (angle ~half a turn, least radius) is kept.
	rt.DropRoll = dropRollSequence(0, 0, 0, 16384, 0, 0)
	constrainCalls := 0
	rt.ConstrainMovement = func(_ string, from, to simulation.Spawn) (simulation.Spawn, *simulation.MoveError) {
		constrainCalls++
		if constrainCalls == 1 {
			return from, &simulation.MoveError{Reason: "blocked"}
		}
		return to, nil
	}
	got = rt.scatterMonsterDrop(item, at, 0)
	if got.Position.RegionID != at.RegionID || got.Position.X != float32(at.X-8) || math.Abs(float64(got.Position.Z)-at.Z) > 0.01 {
		t.Fatalf("retry placement %+v, want the second attempt's spot", got.Position)
	}

	// Both placements refused: the death record is kept whole.
	rt.DropRoll = dropRollSequence(0, 0, 0, 0, 0, 0)
	rt.ConstrainMovement = func(_ string, from, to simulation.Spawn) (simulation.Spawn, *simulation.MoveError) {
		return from, &simulation.MoveError{Reason: "blocked"}
	}
	got = rt.scatterMonsterDrop(item, at, 0)
	if got.Position != item.Position || got.Y != item.Y || got.Heading != item.Heading {
		t.Fatalf("fallback %+v", got)
	}
	rt.DropRoll = func() (uint32, error) { return 0, errors.New("entropy unavailable") }
	got = rt.scatterMonsterDrop(item, at, 0)
	if got.Position != item.Position {
		t.Fatal("failed RNG moved item")
	}
}
