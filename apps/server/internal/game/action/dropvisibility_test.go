package action

// DROP-VANISH wave (BOOT 1371), DROP-3 lane: the store half of the vanish
// repro. These tests pin the ground-item LIFECYCLE - if the store dropped,
// mis-keyed or insta-reaped a drop, the item would leave the bag and never be
// seen again even with a perfect wire emit. Verdict pinned here: it does not.

import (
	"math"
	"opensro.online/server/internal/domain"
	"reflect"
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/wire"
)

// One storyline over the REAL 0x706D handler: a type-7 drop
//
//	(a) lands division-keyed under the session's resolved division,
//	(b) at the live interpolated position (bug D plane),
//	(c) survives sweeps until the fixture lifetime (never insta-reaped),
//	(d) is IMMEDIATELY visible to both the bootstrap object list and a
//	    pickup of the exact gid the 0x30D7 reply advertised.
func TestGroundDropStoreLifecycleImmediatelyVisible(t *testing.T) {
	character := testCharacter()
	rt, clock := newTestRuntime(character, testItems())
	goal := installMidMove(rt, character, clock)

	result := rt.HandleItemMove(testDivision, character, encodeMove(t, wire.ItemMoveRequest{
		MovementType: wire.MoveTypeGroundDrop,
		SourceSlot:   20,
	}))
	assertOpcodes(t, result.Frames, wire.OpItemMoveResponse, wire.OpSingleObjectSpawn)

	// The gid the client learns rides the 0x30D7 payload; everything below
	// must be visible under exactly that id.
	equipWord := wire.PackTypeFlags(3, 1, 6, 2)
	row, err := wire.DecodeGroundItemRow(result.Frames[1].Payload, equipWord, true)
	if err != nil {
		t.Fatalf("0x30D7 payload did not decode: %v", err)
	}

	// (a) Division keying: the session's division and nothing else.
	if divisions := rt.Ground.DivisionIDs(); len(divisions) != 1 || divisions[0] != testDivision {
		t.Fatalf("registry divisions = %v, want exactly [%q]", divisions, testDivision)
	}
	stored, ok := rt.Ground.Get(testDivision, row.Gid)
	if !ok {
		t.Fatalf("the wire-advertised gid %d is not retrievable from the store", row.Gid)
	}

	// (b) The live interpolated point, never the move goal (bug D), and the
	// store agrees with what the wire advertised.
	if math.Abs(float64(stored.Position.X)-1010) > 0.5 {
		t.Fatalf("stored X = %v, want the live midpoint ~1010", stored.Position.X)
	}
	if math.Abs(float64(stored.Position.X)-goal.X) < 25 {
		t.Fatalf("stored X = %v is the move goal %v - the bug D regression", stored.Position.X, goal.X)
	}
	if row.X != stored.Position.X || row.Y != stored.Y || row.Z != stored.Position.Z ||
		row.RegionID != stored.Position.RegionID {
		t.Fatalf("wire row position %+v diverges from the stored entry (%+v, y=%v)",
			row.Position, stored.Position, stored.Y)
	}

	// (c) Sweeps at the drop instant and 1ms shy of the lifetime leave it.
	if frames := rt.TickHook()(clock.NowMs()); len(frames) != 0 {
		t.Fatalf("a sweep at the drop instant reaped the drop: %+v", frames)
	}
	if frames := rt.TickHook()(clock.At(grounditem.FixtureLifetime - time.Millisecond).UnixMilli()); len(frames) != 0 {
		t.Fatalf("a sweep 1ms before the fixture lifetime reaped the drop: %+v", frames)
	}
	if _, ok := rt.Ground.Get(testDivision, row.Gid); !ok {
		t.Fatal("the drop vanished from the store between sweeps")
	}

	// (d) A bootstrap object list built NOW carries the row: the 0x3417
	// chunk is the same bytes as the 0x30D7 body minus the appear tail.
	listRows := enterworld.GroundObjectListRows(rt.Ground.All(testDivision))
	if len(listRows) != 1 {
		t.Fatalf("object list rows = %d, want 1 immediately after the drop", len(listRows))
	}
	spawnBody := result.Frames[1].Payload
	wantChunk := enterworld.NewPacket(enterworld.OpcodeObjectListChunk, spawnBody[:len(spawnBody)-1])
	wantChunk.Scope = []domain.ObjectScopeChange{{GID: row.Gid, Visible: true}}
	if !reflect.DeepEqual(listRows[0], wantChunk) {
		t.Fatalf("object list chunk = %+v, want the 0x30D7 body minus the appear tail (%+v)",
			listRows[0], wantChunk)
	}

	// (d) A pickup of the advertised gid at the same instant grants: the
	// drop is underfoot, so no approach and no "cannot be picked".
	pick := rt.HandleTargetInteract(testDivision, character,
		wire.TargetInteract{Gid: row.Gid}.Encode())
	assertOpcodes(t, pick.Frames,
		wire.OpPickupAnim, wire.OpItemMoveResponse,
		wire.OpObjectDespawn, wire.OpActionState)
	despawn, err := wire.DecodeObjectDespawn(pick.Frames[2].Payload)
	if err != nil || despawn.Gid != row.Gid {
		t.Fatalf("grant despawn = %+v (%v), want gid %d", despawn, err, row.Gid)
	}
	if rt.Ground.Count(testDivision) != 0 {
		t.Fatal("the granted drop survived in the store")
	}
	var regained bool
	for _, invRow := range character.MissionInventory {
		if invRow.RefObjID == 11459 {
			regained = true
		}
	}
	if !regained {
		t.Fatal("the picked-up sword never returned to the inventory")
	}
}

// The Go TTL is Node's: missionGroundItemFixtureLifetimeMs = 180_000 applied
// on the same inclusive boundary (server.mjs sweep keeps now-droppedAtMs <
// lifetime, reaps >=). A handler drop lives the FULL lifetime and reaps on
// the first sweep at or after +180s - never sooner, never instantly. Sweeps
// run on Node's 5s cadence (SweepInterval), so the probe 1ms shy of the
// lifetime consumes one sweep slot and the reap lands on the next one.
func TestGroundDropLifetimeMatchesNodeFixture(t *testing.T) {
	if grounditem.FixtureLifetime != 180*time.Second {
		t.Fatalf("FixtureLifetime = %v, want Node's 180s missionGroundItemFixtureLifetimeMs", grounditem.FixtureLifetime)
	}
	if grounditem.SweepInterval != 5*time.Second {
		t.Fatalf("SweepInterval = %v, want Node's 5s missionGroundItemSweepIntervalMs", grounditem.SweepInterval)
	}

	character := testCharacter()
	rt, clock := newTestRuntime(character, testItems())

	result := rt.HandleItemMove(testDivision, character, encodeMove(t, wire.ItemMoveRequest{
		MovementType: wire.MoveTypeGroundDrop,
		SourceSlot:   20,
	}))
	assertOpcodes(t, result.Frames, wire.OpItemMoveResponse, wire.OpSingleObjectSpawn)
	drops := rt.Ground.All(testDivision)
	if len(drops) != 1 {
		t.Fatalf("registry holds %d drops, want 1", len(drops))
	}
	gid := drops[0].Gid

	if frames := rt.TickHook()(clock.At(grounditem.FixtureLifetime - time.Millisecond).UnixMilli()); len(frames) != 0 {
		t.Fatalf("reaped 1ms before the lifetime: %+v", frames)
	}

	swept := rt.TickHook()(clock.At(grounditem.FixtureLifetime + grounditem.SweepInterval).UnixMilli())
	if len(swept) != 1 || swept[0].DivisionID != testDivision || len(swept[0].Frames) != 1 {
		t.Fatalf("first sweep past +lifetime = %+v, want one %q despawn", swept, testDivision)
	}
	despawn, err := wire.DecodeObjectDespawn(swept[0].Frames[0].Payload)
	if err != nil || despawn.Gid != gid {
		t.Fatalf("despawn = %+v (%v), want gid %d", despawn, err, gid)
	}
	if rt.Ground.Count(testDivision) != 0 {
		t.Fatal("the reaped drop survived in the store")
	}
}
