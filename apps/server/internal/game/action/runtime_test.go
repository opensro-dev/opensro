/*
===========================================================================

runtime_test.go - tests for runtime.go

===========================================================================
*/

package action

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
==================
TestHandlerGroundDropMidMoveLandsUnderfoot

THE BUG D INTEGRATION PIN, type-7 twin: a mid-run ground drop lands at the
live interpolated point, never at the pathing destination. This is the
HANDLER calling LiveSpawnAt, not just the planner.
==================
*/
func TestHandlerGroundDropMidMoveLandsUnderfoot(t *testing.T) {
	character := testCharacter()
	rt, clock := newTestRuntime(character, testItems())
	goal := installMidMove(rt, character, clock)

	result := rt.HandleItemMove(testDivision, character, encodeMove(t, wire.ItemMoveRequest{
		MovementType: wire.MoveTypeGroundDrop,
		SourceSlot:   20,
	}))

	assertOpcodes(t, result.Frames, wire.OpItemMoveResponse, wire.OpSingleObjectSpawn)
	assertOpcodes(t, result.Broadcast, wire.OpSingleObjectSpawn)

	drops := rt.Ground.All(testDivision)
	if len(drops) != 1 {
		t.Fatalf("registry holds %d drops, want 1", len(drops))
	}
	drop := drops[0]
	if math.Abs(float64(drop.Position.X)-1010) > 0.5 {
		t.Fatalf("drop X = %v, want the live midpoint ~1010", drop.Position.X)
	}
	if math.Abs(float64(drop.Position.X)-goal.X) < 25 {
		t.Fatalf("drop X = %v landed at the move GOAL %v - the bug D regression", drop.Position.X, goal.X)
	}
	if drop.DroppedAt.IsZero() || drop.StackCount != 1 {
		t.Fatalf("drop bookkeeping = %+v, want DroppedAt + stack 1", drop)
	}
	if drop.VarianceBits != 0x8000000000000000 {
		t.Fatalf("drop variance = %X, want the row's re-armed u64", drop.VarianceBits)
	}

	// The row left the authoritative inventory.
	for _, row := range character.MissionInventory {
		if row.Slot == 20 {
			t.Fatal("the dropped row survived on the character")
		}
	}
}

// The 0x0A twin at handler level: gold lands underfoot and the burst carries
// the debited balance.
/*
================
TestHandlerGoldDropMidMoveLandsUnderfoot
================
*/
func TestHandlerGoldDropMidMoveLandsUnderfoot(t *testing.T) {
	character := testCharacter()
	rt, clock := newTestRuntime(character, testItems())
	goal := installMidMove(rt, character, clock)

	result := rt.HandleItemMove(testDivision, character, encodeMove(t, wire.ItemMoveRequest{
		MovementType: wire.MoveTypeGoldDrop,
		GoldAmount:   1500,
	}))

	assertOpcodes(t, result.Frames,
		wire.OpItemMoveResponse, wire.OpPointsUpdate, wire.OpSingleObjectSpawn)

	refresh, err := wire.DecodeGoldRefresh(result.Frames[1].Payload)
	if err != nil || refresh.Balance != 3500 {
		t.Fatalf("balance refresh = %+v (%v), want 3500", refresh, err)
	}
	if character.Gold == nil || *character.Gold != 3500 {
		t.Fatalf("character gold = %v, want 3500", character.Gold)
	}

	drops := rt.Ground.All(testDivision)
	if len(drops) != 1 || !drops[0].IsGold() || drops[0].GoldAmount != 1500 {
		t.Fatalf("registry = %+v, want one 1500-gold heap", drops)
	}
	if math.Abs(float64(drops[0].Position.X)-1010) > 0.5 {
		t.Fatalf("heap X = %v, want the live midpoint ~1010", drops[0].Position.X)
	}
	if math.Abs(float64(drops[0].Position.X)-goal.X) < 25 {
		t.Fatal("the heap landed at the move goal - the bug D regression")
	}
	if drops[0].Codename != "ITEM_ETC_GOLD_02" {
		t.Fatalf("tier = %s, want the 1000..9999 medium heap", drops[0].Codename)
	}
}

/*
==================
TestPickupReachMeasuresFromLivePosition

THE PICKUP REACH PIN: reach is measured from the LIVE position. An item at
the live midpoint grants immediately even though the move GOAL is far; an
item at the goal walks even though the goal is "underfoot" on the goal
plane.
==================
*/
func TestPickupReachMeasuresFromLivePosition(t *testing.T) {
	t.Run("item at the live point grants now", func(t *testing.T) {
		character := testCharacter()
		rt, clock := newTestRuntime(character, testItems())
		installMidMove(rt, character, clock)

		heap := rt.Ground.Add(testDivision, PlanGoldDrop(
			GoldHeapRef{RefObjID: 62, Codename: "ITEM_ETC_GOLD_02", Tid1: 3, Tid2: 3, Tid3: 5, Tid4: 2},
			777,
			simulation.Spawn{RegionID: 0x62A8, X: 1012, Y: 20, Z: 458}, // ~2u from live, ~48u from goal
			"someone", clock.Now()))

		result := rt.HandleTargetInteract(testDivision, character,
			wire.TargetInteract{Gid: heap.Gid}.Encode())

		assertOpcodes(t, result.Frames,
			wire.OpActionState, wire.OpPickupAnim, wire.OpItemMoveResponse,
			wire.OpPointsUpdate, wire.OpObjectDespawn)
		if character.Gold == nil || *character.Gold != 5777 {
			t.Fatalf("gold = %v, want 5000+777", character.Gold)
		}
		if rt.Ground.Count(testDivision) != 0 {
			t.Fatal("the granted heap survived in the registry")
		}
	})

	t.Run("item at the goal walks first", func(t *testing.T) {
		character := testCharacter()
		rt, clock := newTestRuntime(character, testItems())
		goal := installMidMove(rt, character, clock)

		heap := rt.Ground.Add(testDivision, PlanGoldDrop(
			GoldHeapRef{RefObjID: 62, Codename: "ITEM_ETC_GOLD_02", Tid1: 3, Tid2: 3, Tid3: 5, Tid4: 2},
			777,
			simulation.Spawn{RegionID: goal.RegionID, X: goal.X, Y: goal.Y, Z: goal.Z},
			"someone", clock.Now()))

		result := rt.HandleTargetInteract(testDivision, character,
			wire.TargetInteract{Gid: heap.Gid}.Encode())

		// Reading the GOAL plane would call this in range (0u); the live
		// plane is ~50u away, so the server walks the character there.
		assertOpcodes(t, result.Frames, wire.OpActionState, simulation.OpMovementAck)
		if result.Pending == nil || result.Pending.ItemGid != heap.Gid {
			t.Fatalf("pending = %+v, want the armed approach", result.Pending)
		}
		if got := result.Frames[0].Payload; got[0] != 0x01 || got[1] != 0x01 {
			t.Fatalf("latch frame = % X, want the 01 01 arm", got)
		}
		if rt.Ground.Count(testDivision) != 1 {
			t.Fatal("an approach consumed the ground item")
		}
	})
}

/*
================
TestPickupApproachCompletesOnServerTickWithoutClientReplay
================
*/
func TestPickupApproachCompletesOnServerTickWithoutClientReplay(t *testing.T) {
	character := testCharacter()
	rt, clock := newTestRuntime(character, testItems())
	var private, peers []wire.Frame
	rt.PushCharacterFrames = func(divisionID, characterName string, frames []wire.Frame) {
		if divisionID != testDivision || characterName != character.Name {
			t.Fatalf("private route = %q/%q", divisionID, characterName)
		}
		private = append(private, frames...)
	}
	rt.PushDivisionPeerFrames = func(divisionID, exceptCharacterName string, frames []wire.Frame) {
		if divisionID != testDivision || exceptCharacterName != character.Name {
			t.Fatalf("peer route = %q except %q", divisionID, exceptCharacterName)
		}
		peers = append(peers, frames...)
	}

	start := simulation.SeedWorldState(character).Spawn
	heap := rt.Ground.Add(testDivision, PlanGoldDrop(
		GoldHeapRef{RefObjID: 62, Codename: "ITEM_ETC_GOLD_02", Tid1: 3, Tid2: 3, Tid3: 5, Tid4: 2},
		1000,
		simulation.Spawn{RegionID: start.RegionID, X: start.X + 100, Y: start.Y, Z: start.Z},
		"someone", clock.Now()))

	armed := rt.HandleTargetInteract(
		testDivision,
		character,
		wire.TargetInteract{Gid: heap.Gid}.Encode(),
	)
	assertOpcodes(t, armed.Frames, wire.OpActionState, simulation.OpMovementAck)
	if armed.Pending == nil || armed.Pending.Eta != 2*time.Second {
		t.Fatalf("pending = %+v, want a 2s server-owned approach", armed.Pending)
	}
	clock.Advance(armed.Pending.Eta + 10*time.Millisecond)
	if routed := rt.TickHook()(clock.NowMs()); len(routed) != 0 {
		t.Fatalf("pickup completion escaped through division-wide tick route: %+v", routed)
	}
	assertOpcodes(t, private,
		wire.OpActionState, wire.OpPickupAnim, wire.OpItemMoveResponse,
		wire.OpPointsUpdate, wire.OpObjectDespawn)
	assertOpcodes(t, peers, wire.OpPickupAnim, wire.OpObjectDespawn)
	if character.Gold == nil || *character.Gold != 6000 {
		t.Fatalf("gold = %v, want 5000+1000", character.Gold)
	}
	if _, ok := rt.Ground.Get(testDivision, heap.Gid); ok {
		t.Fatal("server-completed pickup left the heap in ground authority")
	}

	privateCount, peerCount := len(private), len(peers)
	rt.TickHook()(clock.NowMs() + 1)
	if len(private) != privateCount || len(peers) != peerCount {
		t.Fatalf("completed pickup replayed: private %d->%d peers %d->%d",
			privateCount, len(private), peerCount, len(peers))
	}
}

/*
==================
TestPickupApproachMaturesIntoGrant

The approach is itself a live-plane move, so a mid-approach drop lands
along the walk, not at the destination. Duplicate clicks are silent while
travel is active; the request handler still revalidates a superseded path.
==================
*/
func TestPickupApproachMaturesIntoGrant(t *testing.T) {
	character := testCharacter()
	rt, clock := newTestRuntime(character, testItems())

	// Settled at the start profile; the heap is 100u east: 2s run.
	start := simulation.SeedWorldState(character).Spawn
	heap := rt.Ground.Add(testDivision, PlanGoldDrop(
		GoldHeapRef{RefObjID: 62, Codename: "ITEM_ETC_GOLD_02", Tid1: 3, Tid2: 3, Tid3: 5, Tid4: 2},
		1000,
		simulation.Spawn{RegionID: start.RegionID, X: start.X + 100, Y: start.Y, Z: start.Z},
		"someone", clock.Now()))

	interact := wire.TargetInteract{Gid: heap.Gid}.Encode()

	armed := rt.HandleTargetInteract(testDivision, character, interact)
	assertOpcodes(t, armed.Frames, wire.OpActionState, simulation.OpMovementAck)
	if armed.Pending == nil || armed.Pending.Eta != 2*time.Second {
		t.Fatalf("pending = %+v, want a 2s approach", armed.Pending)
	}

	// A duplicate click while the approach is active is silent and reports
	// the same server-owned pending travel.
	clock.Advance(1 * time.Second)
	duplicate := rt.HandleTargetInteract(testDivision, character, interact)
	if len(duplicate.Frames) != 0 || duplicate.Pending == nil {
		t.Fatalf("mid-approach duplicate = %+v, want silence + pending", duplicate)
	}

	// A drop mid-approach lands along the walk (the approach is a real
	// live-plane segment), roughly halfway to the item.
	dropResult := rt.HandleItemMove(testDivision, character, encodeMove(t, wire.ItemMoveRequest{
		MovementType: wire.MoveTypeGroundDrop,
		SourceSlot:   20,
	}))
	assertOpcodes(t, dropResult.Frames, wire.OpItemMoveResponse, wire.OpSingleObjectSpawn)
	var swordDrop bool
	for _, entry := range rt.Ground.All(testDivision) {
		if entry.RefObjID == 11459 {
			swordDrop = true
			if math.Abs(float64(entry.Position.X)-(start.X+50)) > 1.5 {
				t.Fatalf("mid-approach drop X = %v, want ~%v (halfway along the approach)", entry.Position.X, start.X+50)
			}
		}
	}
	if !swordDrop {
		t.Fatal("the mid-approach drop never reached the registry")
	}

	// The item op superseded the approach: the next interact re-approaches
	// from the CURRENT live point rather than granting a stale pending.
	rearmed := rt.HandleTargetInteract(testDivision, character, interact)
	if rearmed.Pending == nil {
		t.Fatalf("re-interact after supersede = %+v, want a fresh approach", rearmed)
	}

	// The request path also tolerates a duplicate arriving just after maturity;
	// the simulation tick is the normal owner of this transition.
	clock.Advance(rearmed.Pending.Eta + 10*time.Millisecond)
	granted := rt.HandleTargetInteract(testDivision, character, interact)
	assertOpcodes(t, granted.Frames,
		wire.OpActionState, wire.OpPickupAnim, wire.OpItemMoveResponse,
		wire.OpPointsUpdate, wire.OpObjectDespawn)
	if character.Gold == nil || *character.Gold != 6000 {
		t.Fatalf("gold = %v, want 5000+1000", character.Gold)
	}
}

/*
================
TestPickupRefusesCrossWorldTarget
================
*/
func TestPickupRefusesCrossWorldTarget(t *testing.T) {
	character := testCharacter()
	rt, clock := newTestRuntime(character, testItems())
	start := simulation.SeedWorldState(character).Spawn
	dungeon := start
	dungeon.RegionID |= grounditem.DungeonSectorBit
	heap := rt.Ground.Add(testDivision, PlanGoldDrop(
		GoldHeapRef{RefObjID: 62, Codename: "ITEM_ETC_GOLD_02", Tid1: 3, Tid2: 3, Tid3: 5, Tid4: 2},
		1000,
		dungeon,
		"someone", clock.Now()))

	result := rt.HandleTargetInteract(
		testDivision,
		character,
		wire.TargetInteract{Gid: heap.Gid}.Encode(),
	)
	if result.Pending != nil {
		t.Fatalf("cross-world target armed an approach: %+v", result.Pending)
	}
	if len(result.Frames) == 0 {
		t.Fatal("cross-world target was not refused")
	}
	if rt.Ground.Count(testDivision) != 1 {
		t.Fatal("cross-world refusal consumed the ground item")
	}
	if character.Gold == nil || *character.Gold != 5000 {
		t.Fatalf("cross-world refusal changed gold to %v", character.Gold)
	}
}

/*
================
TestPickupMaturityRechecksAuthoritativeDistance
================
*/
func TestPickupMaturityRechecksAuthoritativeDistance(t *testing.T) {
	character := testCharacter()
	rt, clock := newTestRuntime(character, testItems())
	start := simulation.SeedWorldState(character).Spawn
	heap := rt.Ground.Add(testDivision, PlanGoldDrop(
		GoldHeapRef{RefObjID: 62, Codename: "ITEM_ETC_GOLD_02", Tid1: 3, Tid2: 3, Tid3: 5, Tid4: 2},
		1000,
		simulation.Spawn{RegionID: start.RegionID, X: start.X + 100, Y: start.Y, Z: start.Z},
		"someone", clock.Now()))
	interact := wire.TargetInteract{Gid: heap.Gid}.Encode()

	armed := rt.HandleTargetInteract(testDivision, character, interact)
	if armed.Pending == nil {
		t.Fatalf("initial pickup did not arm: %+v", armed)
	}
	clock.Advance(armed.Pending.Eta + time.Millisecond)

	// Model a server-side correction that kept the character at the start.
	// The old code trusted only the elapsed timer and granted from here.
	rt.Worlds.Update(
		simulation.WorldKey(testDivision, character.Name),
		func() simulation.WorldState { return simulation.SeedWorldState(character) },
		func(world *simulation.WorldState) {
			world.Spawn = start
			world.MoveSegment = nil
		},
	)

	rechecked := rt.HandleTargetInteract(testDivision, character, interact)
	if rechecked.Pending == nil {
		t.Fatalf("distance recheck granted instead of re-approaching: %+v", rechecked)
	}
	if rt.Ground.Count(testDivision) != 1 {
		t.Fatal("distance recheck consumed the ground item")
	}
	if character.Gold == nil || *character.Gold != 5000 {
		t.Fatalf("distance recheck changed gold to %v", character.Gold)
	}
}

/*
================
TestPickupApproachCannotBypassMovementConstraint
================
*/
func TestPickupApproachCannotBypassMovementConstraint(t *testing.T) {
	character := testCharacter()
	rt, clock := newTestRuntime(character, testItems())
	start := simulation.SeedWorldState(character).Spawn
	heap := rt.Ground.Add(testDivision, PlanGoldDrop(
		GoldHeapRef{RefObjID: 62, Codename: "ITEM_ETC_GOLD_02", Tid1: 3, Tid2: 3, Tid3: 5, Tid4: 2},
		1000,
		simulation.Spawn{RegionID: start.RegionID, X: start.X + 100, Y: start.Y, Z: start.Z},
		"someone", clock.Now()))
	rt.ConstrainMovement = func(_ string, from, _ simulation.Spawn) (simulation.Spawn, *simulation.MoveError) {
		return from, nil
	}

	result := rt.HandleTargetInteract(
		testDivision,
		character,
		wire.TargetInteract{Gid: heap.Gid}.Encode(),
	)
	if result.Pending != nil {
		t.Fatalf("blocked pickup armed an approach: %+v", result.Pending)
	}
	if len(result.Frames) == 0 {
		t.Fatal("blocked pickup was not refused")
	}
	if rt.Ground.Count(testDivision) != 1 {
		t.Fatal("blocked pickup consumed the ground item")
	}
}

/*
================
TestPickupCancelReleasesTheLatch
================
*/
func TestPickupCancelReleasesTheLatch(t *testing.T) {
	character := testCharacter()
	rt, clock := newTestRuntime(character, testItems())

	start := simulation.SeedWorldState(character).Spawn
	heap := rt.Ground.Add(testDivision, PlanGoldDrop(
		GoldHeapRef{RefObjID: 62, Codename: "ITEM_ETC_GOLD_02", Tid1: 3, Tid2: 3, Tid3: 5, Tid4: 2},
		500,
		simulation.Spawn{RegionID: start.RegionID, X: start.X + 100, Y: start.Y, Z: start.Z},
		"someone", clock.Now()))

	rt.HandleTargetInteract(testDivision, character, wire.TargetInteract{Gid: heap.Gid}.Encode())

	cancel := rt.HandleTargetInteract(testDivision, character, wire.TargetInteract{Cancel: true}.Encode())
	assertOpcodes(t, cancel.Frames, wire.OpObjectSourceCorrection, wire.OpActionState)
	if got := cancel.Frames[1].Payload; got[0] != 0x02 || got[1] != 0x00 {
		t.Fatalf("cancel latch frame = % X, want the 02 00 release", got)
	}
	if _, armed := rt.Pending.Peek(simulation.WorldKey(testDivision, character.Name)); armed {
		t.Fatal("the cancelled approach is still armed")
	}
	clock.Advance(3 * time.Second)
	rt.TickHook()(clock.NowMs())
	if _, ok := rt.Ground.Get(testDivision, heap.Gid); !ok {
		t.Fatal("the cancelled approach later granted from a stale tick snapshot")
	}
	if character.Gold == nil || *character.Gold != 5000 {
		t.Fatalf("cancelled approach changed gold to %v", character.Gold)
	}
}

/*
==================
TestM1VisualsAlwaysRideBehindSocketTransfers

THE M1 EMIT PIN: every transfer touching a socket < 13 appends its visual
pushes behind the 0xB06D row - equip, unequip and swap; a bag-only move
appends nothing. Viewers already holding the spawn row receive the same
visual pushes, or they keep drawing the old gear until it respawns.
==================
*/
func TestM1VisualsAlwaysRideBehindSocketTransfers(t *testing.T) {
	equipWord := wire.PackTypeFlags(3, 1, 6, 2)
	objectID := uint32(100003)

	newRT := func() (*Runtime, *enterworld.Character) {
		character := testCharacter()
		character.MissionInventory = []enterworld.InventoryRow{
			{Slot: 20, RefObjID: 11459, Codename: "ITEM_CH_SWORD_01_A_RARE",
				TypeFlags: equipWord, Plus: 5, VarianceBits: "0", Durability: 96, StackCount: 1},
		}
		rt, _ := newTestRuntime(character, testItems())
		return rt, character
	}

	t.Run("equip pushes 0x3314 with OptLevel=Plus", func(t *testing.T) {
		rt, character := newRT()
		result := rt.HandleItemMove(testDivision, character, encodeMove(t, wire.ItemMoveRequest{
			MovementType: wire.MoveTypeInventory, SourceSlot: 20, DestSlot: 6, Quantity: 1,
		}))
		assertOpcodes(t, result.Frames, wire.OpItemMoveResponse, wire.OpEquipVisual, wire.OpBaseStats)
		assertOpcodes(t, result.Broadcast, wire.OpEquipVisual)
		if !bytes.Equal(result.Broadcast[0].Payload, result.Frames[1].Payload) {
			t.Fatal("viewers were sent a different equip visual than the owner")
		}
		stats := result.Frames[2].Payload
		if got := binary.LittleEndian.Uint32(stats[0x00:]); got != 33 {
			t.Fatalf("equipped physical attack minimum = %d, want base+weapon projection 33", got)
		}
		if got := binary.LittleEndian.Uint16(stats[0x14:]); got != 35 {
			t.Fatalf("equipped hit ratio = %d, want base+weapon projection 35", got)
		}

		visual, err := wire.DecodeEquipVisual(result.Frames[1].Payload, equipWord)
		if err != nil {
			t.Fatalf("equip visual did not decode: %v", err)
		}
		if visual.Gid != objectID || visual.RefObjID != 11459 || visual.OptLevel != 5 {
			t.Fatalf("visual = %+v, want gid %d ref 11459 opt 5 (the item's Plus)", visual, objectID)
		}
	})

	t.Run("unequip pushes the 0x377C clear", func(t *testing.T) {
		rt, character := newRT()
		rt.HandleItemMove(testDivision, character, encodeMove(t, wire.ItemMoveRequest{
			MovementType: wire.MoveTypeInventory, SourceSlot: 20, DestSlot: 6, Quantity: 1,
		}))
		result := rt.HandleItemMove(testDivision, character, encodeMove(t, wire.ItemMoveRequest{
			MovementType: wire.MoveTypeInventory, SourceSlot: 6, DestSlot: 25, Quantity: 1,
		}))
		assertOpcodes(t, result.Frames, wire.OpItemMoveResponse, wire.OpUnequipVisual, wire.OpBaseStats)
		assertOpcodes(t, result.Broadcast, wire.OpUnequipVisual)
		stats := result.Frames[2].Payload
		if got := binary.LittleEndian.Uint32(stats[0x00:]); got != 6 {
			t.Fatalf("unequipped physical attack minimum = %d, want base projection 6", got)
		}
		if got := binary.LittleEndian.Uint16(stats[0x14:]); got != 11 {
			t.Fatalf("unequipped hit ratio = %d, want base projection 11", got)
		}

		clear, err := wire.DecodeUnequipVisual(result.Frames[1].Payload)
		if err != nil {
			t.Fatalf("unequip visual did not decode: %v", err)
		}
		if clear.Gid != objectID || clear.Slot != 6 || clear.RefObjID != 0 {
			t.Fatalf("clear = %+v, want gid %d slot 6 ref 0", clear, objectID)
		}
	})

	t.Run("weapon swap through the bag pushes the new occupant", func(t *testing.T) {
		rt, character := newRT()
		character.MissionInventory = append(character.MissionInventory, enterworld.InventoryRow{
			Slot: 6, RefObjID: 11460, Codename: "ITEM_CH_SWORD_02_A_RARE",
			TypeFlags: equipWord, Plus: 2, VarianceBits: "0", StackCount: 1,
		})
		result := rt.HandleItemMove(testDivision, character, encodeMove(t, wire.ItemMoveRequest{
			MovementType: wire.MoveTypeInventory, SourceSlot: 6, DestSlot: 20, Quantity: 1,
		}))
		assertOpcodes(t, result.Frames, wire.OpItemMoveResponse, wire.OpEquipVisual, wire.OpBaseStats)
		assertOpcodes(t, result.Broadcast, wire.OpEquipVisual)

		visual, _ := wire.DecodeEquipVisual(result.Broadcast[0].Payload, equipWord)
		if visual.RefObjID != 11459 || visual.OptLevel != 5 {
			t.Fatalf("viewer visual = %+v, want the swapped-in 11459 opt 5", visual)
		}
	})

	t.Run("bag-only move pushes nothing", func(t *testing.T) {
		rt, character := newRT()
		result := rt.HandleItemMove(testDivision, character, encodeMove(t, wire.ItemMoveRequest{
			MovementType: wire.MoveTypeInventory, SourceSlot: 20, DestSlot: 30, Quantity: 1,
		}))
		assertOpcodes(t, result.Frames, wire.OpItemMoveResponse)
		assertOpcodes(t, result.Broadcast)
	})

	t.Run("a refused equip pushes nothing", func(t *testing.T) {
		rt, character := newRT()
		result := rt.HandleItemMove(testDivision, character, encodeMove(t, wire.ItemMoveRequest{
			MovementType: wire.MoveTypeInventory, SourceSlot: 20, DestSlot: 7, Quantity: 1, // sword into shield socket
		}))
		assertOpcodes(t, result.Frames, wire.OpItemMoveResponse)
		result0, err := wire.DecodeItemMoveResult(result.Frames[0].Payload, 0)
		if err != nil || result0.Result != wire.ResultError {
			t.Fatalf("refusal = %+v (%v), want the error row", result0, err)
		}
	})
}

/*
================
TestFramesFromSocketVisualsUnit
================
*/
func TestFramesFromSocketVisualsUnit(t *testing.T) {
	word := wire.PackTypeFlags(3, 1, 6, 2)
	frames := FramesFromSocketVisuals(100003, []inventory.SocketVisual{
		{Socket: 6, Worn: true, Item: inventory.Item{RefObjID: 11459, TypeFlags: word, Plus: 7}},
		{Socket: 4},
	})
	assertOpcodes(t, frames, wire.OpEquipVisual, wire.OpUnequipVisual)

	visual, err := wire.DecodeEquipVisual(frames[0].Payload, word)
	if err != nil || visual.OptLevel != 7 {
		t.Fatalf("worn visual = %+v (%v), want OptLevel = Plus = 7", visual, err)
	}
	clear, err := wire.DecodeUnequipVisual(frames[1].Payload)
	if err != nil || clear.RefObjID != 0 || clear.Slot != 4 {
		t.Fatalf("clear = %+v (%v), want slot 4 ref 0", clear, err)
	}
}

// Over-cap pickup: the heap keeps its gid with the remainder and the despawn
// is withheld on both the burst and the broadcast.
/*
================
TestPickupOverCapLeavesRemainderOnGround
================
*/
func TestPickupOverCapLeavesRemainderOnGround(t *testing.T) {
	character := testCharacter()
	character.MissionInventory = nil
	rt, clock := newTestRuntime(character, testItems())

	start := simulation.SeedWorldState(character).Spawn
	drop := PlanItemDrop(inventory.Item{
		RefObjID: 3630, Codename: "ITEM_ETC_HP_POTION_01",
		TypeFlags: wire.PackTypeFlags(3, 3, 1, 1), Quantity: 60,
	}, 60, start, "someone", clock.Now())
	added := rt.Ground.Add(testDivision, drop)

	result := rt.HandleTargetInteract(testDivision, character,
		wire.TargetInteract{Gid: added.Gid}.Encode())

	// No despawn: the heap still lives with the remainder.
	assertOpcodes(t, result.Frames,
		wire.OpActionState, wire.OpPickupAnim, wire.OpItemMoveResponse)
	assertOpcodes(t, result.Broadcast, wire.OpPickupAnim)

	remaining, ok := rt.Ground.Get(testDivision, added.Gid)
	if !ok || remaining.StackCount != 10 {
		t.Fatalf("ground remainder = %+v, want stack 10 under the same gid", remaining)
	}
	var bagged bool
	for _, row := range character.MissionInventory {
		if row.Slot == 13 && row.StackCount == 50 {
			bagged = true
		}
	}
	if !bagged {
		t.Fatalf("inventory rows = %+v, want 50 units in slot 13", character.MissionInventory)
	}
}

/*
================
TestPickupFullBagRefuses
================
*/
func TestPickupFullBagRefuses(t *testing.T) {
	character := testCharacter()
	character.MissionInventory = nil
	for slot := int64(13); slot < 45; slot++ {
		character.MissionInventory = append(character.MissionInventory, enterworld.InventoryRow{
			Slot: slot, RefObjID: 11459, Codename: "ITEM_CH_SWORD_01_A_RARE",
			TypeFlags: wire.PackTypeFlags(3, 1, 6, 2), VarianceBits: "0", StackCount: 1,
		})
	}
	rt, clock := newTestRuntime(character, testItems())

	start := simulation.SeedWorldState(character).Spawn
	added := rt.Ground.Add(testDivision, PlanItemDrop(inventory.Item{
		RefObjID: 999, Codename: "ITEM_CH_RING_01_A_RARE",
		TypeFlags: wire.PackTypeFlags(3, 1, 5, 3), Quantity: 1,
	}, 1, start, "someone", clock.Now()))

	result := rt.HandleTargetInteract(testDivision, character,
		wire.TargetInteract{Gid: added.Gid}.Encode())

	assertOpcodes(t, result.Frames, wire.OpActionState, wire.OpItemMoveResponse)
	if got := result.Frames[1].Payload; got[0] != 0x02 || got[1] != wire.ErrCodeStorageFull {
		t.Fatalf("refusal = % X, want [02 07] inventory-full", got)
	}
	if rt.Ground.Count(testDivision) != 1 {
		t.Fatal("a refused pickup consumed the ground item")
	}
}

/*
================
TestPickupMissingItemAnswersCannotBePicked
================
*/
func TestPickupMissingItemAnswersCannotBePicked(t *testing.T) {
	character := testCharacter()
	rt, _ := newTestRuntime(character, testItems())

	result := rt.HandleTargetInteract(testDivision, character,
		wire.TargetInteract{Gid: 300999}.Encode())

	assertOpcodes(t, result.Frames, wire.OpActionState, wire.OpItemMoveResponse)
	if got := result.Frames[1].Payload; got[1] != wire.ErrCodeCannotBePicked {
		t.Fatalf("refusal = % X, want the 0x39 cannot-be-picked notice", got)
	}
}

// The TTL sweep through the tick hook: expired drops despawn per division
// with the same 0x36AB the pickup path uses.
/*
================
TestSweepExpiredEmitsDespawnFrames
================
*/
func TestSweepExpiredEmitsDespawnFrames(t *testing.T) {
	character := testCharacter()
	rt, clock := newTestRuntime(character, testItems())

	start := simulation.SeedWorldState(character).Spawn
	old := rt.Ground.Add(testDivision, PlanItemDrop(inventory.Item{
		RefObjID: 11459, Codename: "ITEM_CH_SWORD_01_A_RARE",
		TypeFlags: wire.PackTypeFlags(3, 1, 6, 2), Quantity: 1,
	}, 1, start, "someone", clock.Now()))

	// Not yet expired.
	if frames := rt.TickHook()(clock.At(time.Minute).UnixMilli()); len(frames) != 0 {
		t.Fatalf("a 1-minute-old drop swept early: %+v", frames)
	}

	// Past the fixture lifetime.
	result := rt.TickHook()(clock.At(4 * time.Minute).UnixMilli())
	if len(result) != 1 || result[0].DivisionID != testDivision || len(result[0].Frames) != 1 {
		t.Fatalf("sweep = %+v, want one despawn for %s", result, testDivision)
	}
	if result[0].Frames[0].Opcode != wire.OpObjectDespawn {
		t.Fatalf("sweep opcode = 0x%04X, want 0x36AB", result[0].Frames[0].Opcode)
	}
	despawn, err := wire.DecodeObjectDespawn(result[0].Frames[0].Payload)
	if err != nil || despawn.Gid != old.Gid {
		t.Fatalf("despawn = %+v (%v), want gid %d", despawn, err, old.Gid)
	}
	if rt.Ground.Count(testDivision) != 0 {
		t.Fatal("the swept drop survived in the registry")
	}
}

/*
==================
TestSweepHonoursSweepInterval

The sweep honours grounditem.SweepInterval: the shared tick calls the hook
constantly, but only one peek+sweep runs per declared 5s slot. Expiry
itself is unchanged - the drop's DroppedAt deadline decides eligibility,
the cadence only decides which sweep broadcasts the despawn.
==================
*/
func TestSweepHonoursSweepInterval(t *testing.T) {
	character := testCharacter()
	rt, clock := newTestRuntime(character, testItems())

	start := simulation.SeedWorldState(character).Spawn
	rt.Ground.Add(testDivision, PlanItemDrop(inventory.Item{
		RefObjID: 11459, Codename: "ITEM_CH_SWORD_01_A_RARE",
		TypeFlags: wire.PackTypeFlags(3, 1, 6, 2), Quantity: 1,
	}, 1, start, "someone", clock.Now()))

	// A peek 1ms shy of the lifetime finds nothing and consumes the slot.
	if frames := rt.SweepExpired(clock.At(grounditem.FixtureLifetime - time.Millisecond).UnixMilli()); len(frames) != 0 {
		t.Fatalf("swept before the lifetime: %+v", frames)
	}
	// The next tick (the drop IS expired now) lands inside the cadence
	// window: rate-limited, registry untouched.
	if frames := rt.SweepExpired(clock.At(grounditem.FixtureLifetime + 249*time.Millisecond).UnixMilli()); frames != nil {
		t.Fatalf("a sweep inside the %v cadence window ran anyway: %+v", grounditem.SweepInterval, frames)
	}
	if rt.Ground.Count(testDivision) != 1 {
		t.Fatal("a rate-limited sweep visit touched the registry")
	}
	// The next slot reaps it.
	swept := rt.SweepExpired(clock.At(grounditem.FixtureLifetime - time.Millisecond + grounditem.SweepInterval).UnixMilli())
	if len(swept) != 1 || len(swept[0].Frames) != 1 {
		t.Fatalf("the next sweep slot = %+v, want the expired drop's despawn", swept)
	}
	if rt.Ground.Count(testDivision) != 0 {
		t.Fatal("the swept drop survived in the registry")
	}
}

/*
==================
TestIdleSweepPeeksWithoutRuntimeMutex

An idle sweep (nothing expired) must complete without the maintenance
barrier: the peek
holds only the ground registry's own lock, so the tick's sweep visit
cannot stall behind a long-held item/movement handler.
==================
*/
func TestIdleSweepPeeksWithoutRuntimeMutex(t *testing.T) {
	character := testCharacter()
	rt, clock := newTestRuntime(character, testItems())

	start := simulation.SeedWorldState(character).Spawn
	rt.Ground.Add(testDivision, PlanItemDrop(inventory.Item{
		RefObjID: 11459, Codename: "ITEM_CH_SWORD_01_A_RARE",
		TypeFlags: wire.PackTypeFlags(3, 1, 6, 2), Quantity: 1,
	}, 1, start, "someone", clock.Now()))

	unlock := rt.lockDivision(testDivision)
	defer unlock()

	done := make(chan []simulation.DivisionFrames, 1)
	go func() {
		done <- rt.SweepExpired(clock.At(time.Minute).UnixMilli())
	}()
	select {
	case frames := <-done:
		if frames != nil {
			t.Fatalf("idle sweep = %+v, want nil", frames)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("an idle sweep blocked on item-operation locks")
	}
}

// Row bridging keeps identity: variance survives the string round trip.
/*
================
TestInventoryRowRoundTrip
================
*/
func TestInventoryRowRoundTrip(t *testing.T) {
	rows := []enterworld.InventoryRow{
		{Slot: 6, RefObjID: 107, Codename: "ITEM_CH_BLADE_01_A",
			TypeFlags: 0x132C, Plus: 3, VarianceBits: "9223372036854775808",
			Durability: 69, StackCount: 1},
	}
	items := invItemsFromRows(rows)
	if len(items) != 1 || items[0].VarianceBits != 0x8000000000000000 {
		t.Fatalf("armed rows = %+v, want the top-bit variance", items)
	}
	back := rowsFromInvItems(items)
	if back[0].VarianceBits != "9223372036854775808" {
		t.Fatalf("persisted variance = %q, want the decimal string back", back[0].VarianceBits)
	}
}

/*
================
TestCommerceLayoutsDoNotBypassTransactionAuthority
================
*/
func TestCommerceLayoutsDoNotBypassTransactionAuthority(t *testing.T) {
	character := testCharacter()
	rt, _ := newTestRuntime(character, testItems())
	before := goldOf(character)
	for _, kind := range []uint8{wire.MoveTypeShopBuy, wire.MoveTypeShopSell, wire.MoveTypeCosShopBuy, wire.MoveTypeCosShopSell} {
		payload, err := (wire.ItemMoveRequest{MovementType: kind, NpcGID: 17, CosGID: 42, SourceSlot: 20, Quantity: 1}).Encode()
		if err != nil {
			t.Fatal(err)
		}
		result := rt.HandleItemMove(testDivision, character, payload)
		if len(result.Frames) != 1 || len(result.Frames[0].Payload) != 2 || result.Frames[0].Payload[0] != 2 {
			t.Fatalf("unsupported authority accepted %x: %+v", kind, result)
		}
		if goldOf(character) != before || len(character.MissionInventory) != 1 || character.MissionInventory[0].Slot != 20 {
			t.Fatal("refused commerce changed state")
		}
	}
}

/*
==================
TestPickupApproachMaturesAtTheWalksOwnSpeed

A character faster than the base run speed (a speed scroll) reaches the
drop sooner; the pickup must mature when its walk arrives, not when a
base-speed estimate of it would.
==================
*/
func TestPickupApproachMaturesAtTheWalksOwnSpeed(t *testing.T) {
	character := testCharacter()
	rt, clock := newTestRuntime(character, testItems())
	worldKey := simulation.WorldKey(testDivision, character.Name)
	rt.Worlds.Update(worldKey,
		func() simulation.WorldState { return simulation.SeedWorldState(character) },
		func(world *simulation.WorldState) { world.Walk, world.Run = 20, 100 })
	start := simulation.SeedWorldState(character).Spawn
	heap := rt.Ground.Add(testDivision, PlanGoldDrop(
		GoldHeapRef{RefObjID: 62, Codename: "ITEM_ETC_GOLD_02", Tid1: 3, Tid2: 3, Tid3: 5, Tid4: 2},
		1000,
		simulation.Spawn{RegionID: start.RegionID, X: start.X + 100, Y: start.Y, Z: start.Z},
		"someone", clock.Now()))
	armed := rt.HandleTargetInteract(testDivision, character, wire.TargetInteract{Gid: heap.Gid}.Encode())
	if armed.Pending == nil || armed.Pending.Eta != time.Second {
		t.Fatalf("pending = %+v, want the 1s walk at 100 units/s", armed.Pending)
	}
}
