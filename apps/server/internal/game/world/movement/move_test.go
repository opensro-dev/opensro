/*
===========================================================================

move_test.go - the 0x7738 handler: ground destinations, turns, refusals

===========================================================================
*/
package movement

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/transport/worldsession"
)

const testStartMs = int64(1_784_000_000_000)

/*
================
testCharacter
================
*/
func testCharacter() *enterworld.Character {
	return &enterworld.Character{ID: 7, Name: "Asd", ModelCodename: "CHAR_EU_MAN1"}
}

/*
================
testRuntime
================
*/
func testRuntime(character *enterworld.Character) *Runtime {
	deps := &enterworld.Deps{Characters: enterworld.StaticCharacterSource{"0": {character}}}
	rt := NewRuntime(deps, simulation.NewWorldStore())
	nowMs := testStartMs
	rt.Now = func() time.Time { return time.UnixMilli(nowMs) }
	// Pin the env-derived gates so an ambient MISSION_SPAWN_NPCS=1 cannot
	// flake the suite.
	rt.npcsEnabled = false
	rt.npcsAtPlayer = false
	return rt
}

/*
================
encodeMoveBody

encodeMoveBody builds the native 9-byte 0x7738 body (height in the
middle i16, the RE-pinned order).
================
*/
func encodeMoveBody(mode uint8, regionID uint16, x, y, z int16) []byte {
	out := make([]byte, 9)
	out[0] = mode
	binary.LittleEndian.PutUint16(out[1:3], regionID)
	binary.LittleEndian.PutUint16(out[3:5], uint16(x))
	binary.LittleEndian.PutUint16(out[5:7], uint16(y))
	binary.LittleEndian.PutUint16(out[7:9], uint16(z))
	return out
}

/*
================
TestHandleMoveAcksAndUpdatesWorld
================
*/
func TestHandleMoveAcksAndUpdatesWorld(t *testing.T) {
	character := testCharacter()
	rt := testRuntime(character)

	start := simulation.EuropeStartProfile()
	body := encodeMoveBody(1, start.RegionID, int16(start.X)+200, int16(start.Y), int16(start.Z))
	outcome := rt.HandleMove("0", character, body)

	if outcome.Refusal != nil {
		t.Fatalf("move refused: %v", outcome.Refusal)
	}
	if len(outcome.Frames) != 1 || outcome.Frames[0].Opcode != simulation.OpMovementAck {
		t.Fatalf("frames = %+v, want one 0xB738", outcome.Frames)
	}

	// The ack must be byte-identical to the parity-tested builder, with the
	// one-shot source block on a first-ever move (movementSourceSeeded off).
	source := simulation.MovementSourceFromSpawn(start)
	wantAck := simulation.BuildMovementAckPayload(
		enterworld.ObjectIDForCharacter(character),
		simulation.MovementRequest{Mode: 1, RegionID: start.RegionID, X: start.X + 200, Y: start.Y, Z: start.Z},
		&source,
	)
	if !bytes.Equal(outcome.Frames[0].Payload, wantAck) {
		t.Errorf("ack payload\n got % X\nwant % X", outcome.Frames[0].Payload, wantAck)
	}
	if !outcome.Result.SourceIncluded {
		t.Error("first move must ship the one-shot source block")
	}

	// The shared world plane holds the goal + a live in-flight segment.
	key := simulation.WorldKey("0", character.Name)
	world := rt.Worlds.Snapshot(key, func() simulation.WorldState { return simulation.SeedWorldState(character) })
	if world.Spawn.X != start.X+200 {
		t.Errorf("goal spawn x = %v, want %v", world.Spawn.X, start.X+200)
	}
	if !world.MoveSegment.Valid() {
		t.Fatal("expected an in-flight segment (200u at 50 u/s = 4000ms)")
	}
	if got := world.MoveSegment.ArrivesAtMs - world.MoveSegment.StartedAtMs; got != 4000 {
		t.Errorf("segment travel = %dms, want 4000", got)
	}

	// Mid-flight, the live plane sits between start and goal - never at the
	// goal (bug D on the movement lane).
	live := world.LiveSpawnAt(testStartMs + 1000)
	if live.X != start.X+50 {
		t.Errorf("live x at t+1s = %v, want %v", live.X, start.X+50)
	}

	// The goal plane persisted onto the character record (segment stays
	// runtime-only).
	if character.World == nil || character.World.Spawn == nil || character.World.Spawn.X == nil {
		t.Fatal("world write-back missing")
	}
	if *character.World.Spawn.X != start.X+200 || !character.World.SpawnSet {
		t.Errorf("persisted spawn x = %v spawnSet = %v, want %v/true",
			*character.World.Spawn.X, character.World.SpawnSet, start.X+200)
	}

	// A second move omits the source block (seeded latch).
	second := rt.HandleMove("0", character, encodeMoveBody(1, start.RegionID, int16(start.X), int16(start.Y), int16(start.Z)+150))
	if second.Refusal != nil {
		t.Fatalf("second move refused: %v", second.Refusal)
	}
	if second.Result.SourceIncluded {
		t.Error("second move must not repeat the source block")
	}
}

/*
================
TestHandleMoveRefusalsAreSilentOnTheWire
================
*/
func TestHandleMoveRefusalsAreSilentOnTheWire(t *testing.T) {
	character := testCharacter()
	rt := testRuntime(character)

	// Malformed body: refusal with zero frames (the reference failure
	// envelope ships packets: []).
	outcome := rt.HandleMove("0", character, []byte{0x01, 0x02})
	if outcome.Refusal == nil || len(outcome.Frames) != 0 {
		t.Errorf("malformed body: refusal=%v frames=%d, want refusal + 0 frames", outcome.Refusal, len(outcome.Frames))
	}

	character.DeletePending = true
	outcome = rt.HandleMove("0", character, encodeMoveBody(1, 0x6B4F, 1205, 80, 396))
	if outcome.Refusal == nil || outcome.Refusal.NativeErrorCode != 0x02 {
		t.Errorf("deletePending: refusal = %v, want 0x02", outcome.Refusal)
	}

	character.DeletePending = false
	deadHP := int64(0)
	character.CurrentHP = &deadHP
	outcome = rt.HandleMove("0", character, encodeMoveBody(1, 0x6B4F, 1205, 80, 396))
	if outcome.Refusal == nil || outcome.Refusal.Reason != "characterDead" || len(outcome.Frames) != 0 {
		t.Errorf("dead character: refusal = %v frames=%d, want characterDead + 0 frames", outcome.Refusal, len(outcome.Frames))
	}

	outcome = (&Runtime{}).HandleMove("0", nil, encodeMoveBody(1, 0x6B4F, 1205, 80, 396))
	if outcome.Refusal == nil || outcome.Refusal.NativeErrorCode != 0x10 {
		t.Errorf("nil character: refusal = %v, want 0x10", outcome.Refusal)
	}
}

/*
================
TestHandleMoveChecksAccessAfterCanonicalizingDestination
================
*/
func TestHandleMoveChecksAccessAfterCanonicalizingDestination(t *testing.T) {
	character := testCharacter()
	rt := testRuntime(character)
	rt.CanEnterRegion = func(candidate *enterworld.Character, regionID uint16) bool {
		return regionID != 0x7e7e || candidate.GMPrivilege
	}

	// The request names the western neighbor but overflows east into 0x7e7e.
	// Policy must see the canonical goal, not the untrusted wire region.
	body := encodeMoveBody(1, 0x7e7d, 2000, 0, 920)
	refused := rt.HandleMove("0", character, body)
	if refused.Refusal == nil || refused.Refusal.Reason != "areaAccessDenied" || len(refused.Frames) != 0 {
		t.Fatalf("ordinary character outcome = %+v, want silent areaAccessDenied", refused)
	}

	character.GMPrivilege = true
	accepted := rt.HandleMove("0", character, body)
	if accepted.Refusal != nil || accepted.Result == nil || accepted.Result.NextSpawn.RegionID != 0x7e7e {
		t.Fatalf("GM outcome = %+v, want canonical 0x7e7e destination", accepted)
	}
}

/*
================
TestHandleMoveClearsPickupLatchBeforeCoercion

TestHandleMoveClearsPickupLatchBeforeCoercion pins the reference order:
the pending-pickup latch clears BEFORE the movement body is coerced, so a
malformed move still releases it - but a deletePending character never
reaches the clear.
================
*/
func TestHandleMoveClearsPickupLatchBeforeCoercion(t *testing.T) {
	character := testCharacter()
	rt := testRuntime(character)
	pickupCleared := 0
	combatCleared := 0
	rt.ClearPendingPickup = func(divisionID, characterName string) {
		if divisionID != "0" || characterName != character.Name {
			t.Errorf("clear key = (%s, %s), want (0, %s)", divisionID, characterName, character.Name)
		}
		pickupCleared++
	}
	rt.ClearCombatIntent = func(divisionID, characterName string) {
		if divisionID != "0" || characterName != character.Name {
			t.Errorf("combat clear key = (%s, %s), want (0, %s)", divisionID, characterName, character.Name)
		}
		combatCleared++
	}

	if outcome := rt.HandleMove("0", character, []byte{0xFF}); outcome.Refusal == nil {
		t.Fatal("expected malformed-body refusal")
	}
	if pickupCleared != 1 || combatCleared != 1 {
		t.Errorf("cleared pickup/combat = %d/%d, want 1/1 before coercion (reference command-ownership order)", pickupCleared, combatCleared)
	}

	character.DeletePending = true
	rt.HandleMove("0", character, []byte{0xFF})
	if pickupCleared != 1 || combatCleared != 1 {
		t.Errorf("cleared pickup/combat = %d/%d, want still 1/1 (deletePending refuses before the clear)", pickupCleared, combatCleared)
	}
}

/*
================
recordedWorld
================
*/
type recordedWorld struct {
	divisionID string
	snapshot   any
}

/*
================
recordedWorld.SetWorldSnapshot
================
*/
func (world *recordedWorld) SetWorldSnapshot(divisionID string, snapshot any) {
	world.divisionID = divisionID
	world.snapshot = snapshot
}

/*
================
TestWorldBoundInstallsTickGlue

TestWorldBoundInstallsTickGlue proves the enter-world glue: division key +
SnapshotProvider land on the session, and the provider snapshot carries
the SHARED world plane at the live segment.
================
*/
func TestWorldBoundInstallsTickGlue(t *testing.T) {
	character := testCharacter()
	rt := testRuntime(character)
	values := &recordedWorld{}

	rt.WorldBound(values, "0", character)

	if values.divisionID != "0" {
		t.Errorf("division id = %q, want \"0\"", values.divisionID)
	}
	provider, ok := values.snapshot.(worldsession.SnapshotProvider)
	if !ok {
		t.Fatalf("snapshot = %T, want a worldsession.SnapshotProvider", values.snapshot)
	}

	// Before any move: the seeded start profile, tick-invisible NPCs
	// (MISSION_SPAWN_NPCS unset in tests).
	snap := provider.WorldSnapshot()
	if snap.DivisionID != "0" || snap.CharacterID != character.ID {
		t.Errorf("snapshot identity = (%s, %d), want (0, %d)", snap.DivisionID, snap.CharacterID, character.ID)
	}
	if snap.World.Spawn != simulation.EuropeStartProfile() {
		t.Errorf("seeded spawn = %+v, want the Europe start profile", snap.World.Spawn)
	}
	if snap.NpcsEnabled {
		t.Error("NpcsEnabled must default off (MISSION_SPAWN_NPCS unset)")
	}

	// After a move, the SAME provider sees the fresh segment (shared store,
	// no copies of the plane).
	start := simulation.EuropeStartProfile()
	if outcome := rt.HandleMove("0", character, encodeMoveBody(1, start.RegionID, int16(start.X)+100, int16(start.Y), int16(start.Z))); outcome.Refusal != nil {
		t.Fatalf("move refused: %v", outcome.Refusal)
	}
	snap = provider.WorldSnapshot()
	if !snap.World.MoveSegment.Valid() {
		t.Fatal("provider snapshot must see the in-flight segment")
	}
}

/*
================
divisionPush
================
*/
type divisionPush struct {
	divisionID string
	frames     []simulation.Frame
	except     string
}

/*
================
recordingPusher
================
*/
type recordingPusher struct {
	toSession  []simulation.Frame
	toDivision []divisionPush
}

/*
================
recordingPusher.PushToSession
================
*/
func (p *recordingPusher) PushToSession(sessionID string, frames []simulation.Frame) {
	p.toSession = append(p.toSession, frames...)
}

/*
================
recordingPusher.PushToDivision
================
*/
func (p *recordingPusher) PushToDivision(divisionID string, frames []simulation.Frame, exceptSessionID string) {
	p.toDivision = append(p.toDivision, divisionPush{divisionID, frames, exceptSessionID})
}

/*
================
staticSource
================
*/
type staticSource struct{ snapshots []simulation.SessionSnapshot }

/*
================
staticSource.SnapshotSessions
================
*/
func (s *staticSource) SnapshotSessions() []simulation.SessionSnapshot { return s.snapshots }

/*
================
TestMoveThenTickBroadcastsLivePosition

TestMoveThenTickBroadcastsLivePosition is the proof chain the resume
brief asks for: client 0x7738 -> WorldState mover updated -> the tick
broadcasts the mover's interpolated 0x30E3 (never the goal) and settles
with one 0xB2F5.
================
*/
func TestMoveThenTickBroadcastsLivePosition(t *testing.T) {
	character := testCharacter()
	rt := testRuntime(character)

	start := simulation.EuropeStartProfile()
	outcome := rt.HandleMove("0", character, encodeMoveBody(1, start.RegionID, int16(start.X)+200, int16(start.Y), int16(start.Z)))
	if outcome.Refusal != nil {
		t.Fatalf("move refused: %v", outcome.Refusal)
	}

	values := &recordedWorld{}
	rt.WorldBound(values, "0", character)
	provider := values.snapshot.(worldsession.SnapshotProvider)

	snapshot := provider.WorldSnapshot()
	snapshot.SessionID = "42"
	viewer := snapshot
	viewer.SessionID = "43"
	viewer.CharacterID = character.ID + 1
	viewer.World.MoveSegment = nil
	viewer.Appearance = nil
	source := &staticSource{snapshots: []simulation.SessionSnapshot{snapshot, viewer}}
	push := &recordingPusher{}
	ticker := simulation.NewTicker(source, push)

	probeMs := testStartMs + 1000 // mid-flight of the 4000ms segment
	ticker.RunTick(probeMs)

	if len(recordedPeerMovement(push)) != 1 {
		t.Fatalf("division broadcasts = %d, want 1", len(recordedPeerMovement(push)))
	}
	broadcast := recordedPeerMovement(push)[0]
	if len(push.toDivision) != 0 {
		t.Fatal("movement escaped peer visibility")
	}
	decoded, err := wire.DecodeObjectSourceMove(broadcast.Payload)
	if err != nil {
		t.Fatalf("decoding 0x30E3: %v", err)
	}
	if decoded.Gid != enterworld.ObjectIDForCharacter(character) {
		t.Errorf("gid = %d, want %d", decoded.Gid, enterworld.ObjectIDForCharacter(character))
	}
	wantX := float32(start.X + 50) // 200u / 4000ms * 1000ms
	if decoded.X != wantX {
		t.Errorf("broadcast x = %v, want live %v", decoded.X, wantX)
	}
	if decoded.X == float32(start.X+200) {
		t.Error("broadcast carries the GOAL - bug D regressed on the tick plane")
	}

	// Settled tick: refresh the snapshot (the real bridge re-snapshots each
	// tick) and expect exactly one 0xB2F5 at the goal.
	settled := provider.WorldSnapshot()
	settled.SessionID = "42"
	source.snapshots = []simulation.SessionSnapshot{settled, viewer}
	ticker.RunTick(testStartMs + 4100)

	if len(recordedPeerMovement(push)) != 2 {
		t.Fatalf("division broadcasts = %d, want 2 (live + settle)", len(recordedPeerMovement(push)))
	}
	correction, err := wire.DecodeObjectSourceCorrection(recordedPeerMovement(push)[1].Payload)
	if err != nil {
		t.Fatalf("decoding 0xB2F5: %v", err)
	}
	if correction.X != float32(start.X+200) {
		t.Errorf("settle x = %v, want the goal %v", correction.X, float32(start.X+200))
	}
}

/*
================
TestHandleMoveDungeonRegionSameNineByteForm

TestHandleMoveDungeonRegionSameNineByteForm pins the dungeon behavior end
to end: v1.150's client serializer (sub_877cc0) has NO s32 dungeon arm -
dungeon clicks ride the SAME 9-byte i16 form - so the handler must accept
a dungeon-region body, ack it byte-identically, and interpolate the live
plane with the dungeon bit intact, including across a sector crossing.
================
*/
func TestHandleMoveDungeonRegionSameNineByteForm(t *testing.T) {
	dungeonRegion := uint16(0x8000) | simulation.EuropeStartProfile().RegionID // 0xEB4F

	character := testCharacter()
	// Persisted world already inside the dungeon plane (the way a session
	// re-enters mid-dungeon after the fixture's write-back).
	regionID := int64(dungeonRegion)
	x, y, z := 500.0, 0.0, 500.0
	angle := int64(0)
	mode := int64(simulation.RunMode)
	character.World = &enterworld.CharacterWorld{
		Spawn:        &enterworld.WorldSpawn{RegionID: &regionID, X: &x, Y: &y, Z: &z, Angle: &angle},
		MovementMode: &mode,
		SpawnSet:     true,
	}
	rt := testRuntime(character)

	// Same-region dungeon click: 500,0,500 -> 700,0,900.
	outcome := rt.HandleMove("0", character, encodeMoveBody(1, dungeonRegion, 700, 0, 900))
	if outcome.Refusal != nil {
		t.Fatalf("dungeon move refused: %v", outcome.Refusal)
	}
	// The ack echoes the dungeon region and is byte-identical to the
	// parity-tested builder (SpawnSet=true seeds the source latch off).
	wantAck := simulation.BuildMovementAckPayload(
		enterworld.ObjectIDForCharacter(character),
		simulation.MovementRequest{Mode: 1, RegionID: dungeonRegion, X: 700, Y: 0, Z: 900},
		nil,
	)
	if !bytes.Equal(outcome.Frames[0].Payload, wantAck) {
		t.Errorf("dungeon ack\n got % X\nwant % X", outcome.Frames[0].Payload, wantAck)
	}

	key := simulation.WorldKey("0", character.Name)
	world := rt.Worlds.Snapshot(key, func() simulation.WorldState { return simulation.SeedWorldState(character) })
	if !world.MoveSegment.Valid() {
		t.Fatal("dungeon move must produce a live segment")
	}
	// Mid-flight the live plane stays on the dungeon plane.
	midMs := (world.MoveSegment.StartedAtMs + world.MoveSegment.ArrivesAtMs) / 2
	live := world.LiveSpawnAt(midMs)
	if !simulation.IsDungeonRegion(live.RegionID) || live.RegionID != dungeonRegion {
		t.Errorf("mid-flight region = 0x%04X, want dungeon 0x%04X", live.RegionID, dungeonRegion)
	}
	if live.X == 700 && live.Z == 900 {
		t.Error("mid-flight live position equals the goal - bug D on the dungeon plane")
	}

	// Cross-region dungeon resteer: from the in-flight point toward the
	// diagonal neighbor sector; the bit must survive the sector crossing
	// (parity vector dungeonCrossRegionMid pins the same math).
	neighbor := uint16(((int(dungeonRegion)>>8)+1)&0xff)<<8 | uint16((int(dungeonRegion)+1)&0xff)
	second := rt.HandleMove("0", character, encodeMoveBody(1, neighbor, 100, 40, 50))
	if second.Refusal != nil {
		t.Fatalf("dungeon cross-region move refused: %v", second.Refusal)
	}
	if !simulation.IsDungeonRegion(second.Result.NextSpawn.RegionID) {
		t.Errorf("cross-region goal = 0x%04X lost the dungeon bit", second.Result.NextSpawn.RegionID)
	}
	world = rt.Worlds.Snapshot(key, func() simulation.WorldState { return simulation.SeedWorldState(character) })
	lateMs := world.MoveSegment.ArrivesAtMs - 10
	liveLate := world.LiveSpawnAt(lateMs)
	if !simulation.IsDungeonRegion(liveLate.RegionID) {
		t.Errorf("late in-flight region = 0x%04X lost the dungeon bit across the crossing", liveLate.RegionID)
	}
}

/*
================
TestHandleMoveCannotForgeOutdoorDungeonTransition
================
*/
func TestHandleMoveCannotForgeOutdoorDungeonTransition(t *testing.T) {
	character := testCharacter()
	rt := testRuntime(character)
	start := simulation.EuropeStartProfile()
	dungeonRegion := start.RegionID | simulation.DungeonSectorBit

	outcome := rt.HandleMove(
		"0",
		character,
		encodeMoveBody(1, dungeonRegion, int16(start.X), int16(start.Y), int16(start.Z)),
	)
	if outcome.Refusal == nil {
		t.Fatal("outdoor character crossed into the dungeon plane with a ground-move packet")
	}
	if outcome.Refusal.NativeErrorCode != simulation.NativeErrorInvalidRequest {
		t.Fatalf("refusal code = 0x%02X, want invalid request", outcome.Refusal.NativeErrorCode)
	}
}

/*
================
encodeTurnBody

encodeTurnBody builds the native 4-byte angular 0x7738 body
(sub_877cc0's mode-0 arm: [u8 0][u8 angularMode][u16 headingWord LE]).
================
*/
func encodeTurnBody(angularMode uint8, headingWord uint16) []byte {
	out := make([]byte, 4)
	out[0] = 0x00
	out[1] = angularMode
	binary.LittleEndian.PutUint16(out[2:4], headingWord)
	return out
}

/*
================
TestHandleMoveAngularTurnInPlace

TestHandleMoveAngularTurnInPlace drives the stationary angular arm end to
end: a 4-byte angular body WITHOUT AngularFlagGo (the SetCommand arm that
latches the angle but not the moving flag) is accepted, acked on 0xB738
with the angular arm sub_776170 parses, and the heading lands on the
settled plane without starting any travel. The GO form walks instead
(direction_test.go).
================
*/
func TestHandleMoveAngularTurnInPlace(t *testing.T) {
	character := testCharacter()
	rt := testRuntime(character)
	start := simulation.EuropeStartProfile()

	outcome := rt.HandleMove("0", character, encodeTurnBody(0, 0x4000))
	if outcome.Refusal != nil {
		t.Fatalf("angular turn refused: %v", outcome.Refusal)
	}
	if len(outcome.Frames) != 1 || outcome.Frames[0].Opcode != simulation.OpMovementAck {
		t.Fatalf("frames = %+v, want one 0xB738", outcome.Frames)
	}

	// First-ever move: the ack carries the one-shot source block.
	gid := enterworld.ObjectIDForCharacter(character)
	source := simulation.MovementSourceFromSpawn(start)
	wantAck := simulation.BuildMovementAckPayload(
		gid,
		simulation.MovementRequest{Mode: 0, AngularMode: 0, HeadingWord: 0x4000},
		&source,
	)
	if !bytes.Equal(outcome.Frames[0].Payload, wantAck) {
		t.Errorf("ack payload\n got % X\nwant % X", outcome.Frames[0].Payload, wantAck)
	}
	// Raw wire-shape pin, independent of the builder: [u32 gid][u8 0]
	// [u8 angularMode][u16 headingWord][u8 srcFlag=1]... - exactly what
	// sub_776170's mode-0 read consumes.
	head := []byte{byte(gid), byte(gid >> 8), byte(gid >> 16), byte(gid >> 24), 0x00, 0x00, 0x00, 0x40, 0x01}
	if !bytes.HasPrefix(outcome.Frames[0].Payload, head) {
		t.Errorf("ack head\n got % X\nwant prefix % X", outcome.Frames[0].Payload, head)
	}
	if !outcome.Result.SourceIncluded {
		t.Error("first accepted turn must ship the one-shot source block")
	}

	// The settled plane holds the facing; position untouched; NO segment.
	key := simulation.WorldKey("0", character.Name)
	world := rt.Worlds.Snapshot(key, func() simulation.WorldState { return simulation.SeedWorldState(character) })
	if world.Spawn.Angle != 0x4000 {
		t.Errorf("spawn angle = 0x%04X, want 0x4000", world.Spawn.Angle)
	}
	if world.Spawn.RegionID != start.RegionID || world.Spawn.X != start.X || world.Spawn.Y != start.Y || world.Spawn.Z != start.Z {
		t.Errorf("turn moved the position: %+v, want %+v", world.Spawn, start)
	}
	if world.MoveSegment != nil {
		t.Error("turn-in-place must not start a travel segment")
	}
	// Run/walk separation: a turn carries no speed semantics.
	if world.MovementMode != simulation.RunMode {
		t.Errorf("movementMode = %d, want untouched %d", world.MovementMode, simulation.RunMode)
	}
	if !world.SpawnSet {
		t.Error("an accepted turn latches spawnSet like every accepted move")
	}
	// The facing persisted onto the character record.
	if character.World == nil || character.World.Spawn == nil || character.World.Spawn.Angle == nil {
		t.Fatal("world write-back missing after a turn")
	}
	if *character.World.Spawn.Angle != 0x4000 {
		t.Errorf("persisted angle = %d, want 0x4000", *character.World.Spawn.Angle)
	}

	// Second turn: the source latch is consumed - the ack is the bare
	// angular echo. Boundary word 0xFFFF (the full-circle top) stores raw.
	second := rt.HandleMove("0", character, encodeTurnBody(0, 0xFFFF))
	if second.Refusal != nil {
		t.Fatalf("second turn refused: %v", second.Refusal)
	}
	if second.Result.SourceIncluded {
		t.Error("second turn must not repeat the source block")
	}
	wantSecond := simulation.BuildMovementAckPayload(
		gid,
		simulation.MovementRequest{Mode: 0, AngularMode: 0, HeadingWord: 0xFFFF},
		nil,
	)
	if !bytes.Equal(second.Frames[0].Payload, wantSecond) {
		t.Errorf("second ack\n got % X\nwant % X", second.Frames[0].Payload, wantSecond)
	}
	world = rt.Worlds.Snapshot(key, func() simulation.WorldState { return simulation.SeedWorldState(character) })
	if world.Spawn.Angle != 0xFFFF {
		t.Errorf("boundary angle = 0x%04X, want 0xFFFF (the wire word stores raw, no float round trip)", world.Spawn.Angle)
	}
}

/*
================
TestHandleMoveAngularTurnMidMoveSettlesAtLivePoint

TestHandleMoveAngularTurnMidMoveSettlesAtLivePoint pins the stationary
angular arm mid-travel: a turn without GO settles the character at the
interpolated live point - the bug-D plane - facing the new heading, and
clears the in-flight segment.
================
*/
func TestHandleMoveAngularTurnMidMoveSettlesAtLivePoint(t *testing.T) {
	character := testCharacter()
	rt := testRuntime(character)
	start := simulation.EuropeStartProfile()

	first := rt.HandleMove("0", character, encodeMoveBody(1, start.RegionID, int16(start.X)+200, int16(start.Y), int16(start.Z)))
	if first.Refusal != nil {
		t.Fatalf("setup move refused: %v", first.Refusal)
	}

	// 1s into the 4000ms run (200u at 50 u/s): live x = start.X + 50.
	rt.Now = func() time.Time { return time.UnixMilli(testStartMs + 1000) }
	turn := rt.HandleMove("0", character, encodeTurnBody(0, 0x8000))
	if turn.Refusal != nil {
		t.Fatalf("mid-move turn refused: %v", turn.Refusal)
	}

	key := simulation.WorldKey("0", character.Name)
	world := rt.Worlds.Snapshot(key, func() simulation.WorldState { return simulation.SeedWorldState(character) })
	if world.MoveSegment != nil {
		t.Error("mid-move turn must clear the in-flight segment")
	}
	if world.Spawn.X != start.X+50 {
		t.Errorf("settled x = %v, want the live point %v (never the abandoned goal %v)",
			world.Spawn.X, start.X+50, start.X+200)
	}
	if world.Spawn.Angle != 0x8000 {
		t.Errorf("settled angle = 0x%04X, want 0x8000", world.Spawn.Angle)
	}
	if turn.Result.Segment != nil || turn.Result.NextSpawn.X != start.X+50 {
		t.Errorf("result witnesses disagree: segment=%v nextSpawn=%+v", turn.Result.Segment, turn.Result.NextSpawn)
	}
}

/*
================
TestHandleMoveAngularMalformedAndUnknownModesStayDistinct

TestHandleMoveAngularMalformedAndUnknownModesStayDistinct preserves the
deliberate refusal split now that the turn lane is real: a WRONG-LENGTH
angular body is a malformed frame, a mode byte the client serializer
cannot emit (sub_877cc0 produces only 0/1) is the loud unsupported-mode
refusal, and both stay wire-silent without touching the world plane.
================
*/
func TestHandleMoveAngularMalformedAndUnknownModesStayDistinct(t *testing.T) {
	character := testCharacter()
	rt := testRuntime(character)

	cases := []struct {
		name    string
		payload []byte
		reason  string
	}{
		{"angularShort", []byte{0x00, 0x01, 0x00}, "malformedMovementRequest"},
		{"angularNineByte", encodeMoveBody(0, 0x6B4F, 1201, 80, 355), "malformedMovementRequest"},
		{"unknownMode2", encodeMoveBody(2, 0x6B4F, 1201, 80, 355), "unsupportedMovementMode"},
	}
	for _, tc := range cases {
		outcome := rt.HandleMove("0", character, tc.payload)
		if outcome.Refusal == nil {
			t.Errorf("%s: expected refusal", tc.name)
			continue
		}
		if outcome.Refusal.NativeErrorCode != 0x02 {
			t.Errorf("%s: nativeErrorCode = 0x%02X, want 0x02", tc.name, outcome.Refusal.NativeErrorCode)
		}
		if !strings.Contains(outcome.Refusal.Reason, tc.reason) {
			t.Errorf("%s: reason %q must carry %q", tc.name, outcome.Refusal.Reason, tc.reason)
		}
		if len(outcome.Frames) != 0 {
			t.Errorf("%s: refusal must stay wire-silent, got %d frames", tc.name, len(outcome.Frames))
		}
	}

	// The world plane stays untouched by every refusal.
	key := simulation.WorldKey("0", character.Name)
	world := rt.Worlds.Snapshot(key, func() simulation.WorldState { return simulation.SeedWorldState(character) })
	if world.SpawnSet || world.MoveSegment != nil {
		t.Errorf("refusals mutated the world plane: %+v", world)
	}
}

/*
================
TestHandleMoveDeepWaterRefusal

TestHandleMoveDeepWaterRefusal wires a stub validator to prove the gate
sits in the accepted-move path (the asset-backed validator has its own
suite in water_test.go).
================
*/
func TestHandleMoveDeepWaterRefusal(t *testing.T) {
	character := testCharacter()
	rt := testRuntime(character)
	rt.Validator = stubValidator{refuse: true}

	outcome := rt.HandleMove("0", character, encodeMoveBody(1, 0x6B4F, 1205, 80, 396))
	if outcome.Refusal == nil || outcome.Refusal.NativeErrorCode != 0x02 {
		t.Fatalf("refusal = %v, want deep-water 0x02", outcome.Refusal)
	}
	if len(outcome.Frames) != 0 {
		t.Errorf("frames = %d, want 0 (refusals are wire-silent)", len(outcome.Frames))
	}
	// The world plane must be untouched by a refused move.
	key := simulation.WorldKey("0", character.Name)
	world := rt.Worlds.Snapshot(key, func() simulation.WorldState { return simulation.SeedWorldState(character) })
	if world.SpawnSet || world.MoveSegment != nil {
		t.Errorf("refused move mutated the world plane: %+v", world)
	}
}

/*
================
stubValidator
================
*/
type stubValidator struct{ refuse bool }

/*
================
stubValidator.ValidateMovement
================
*/
func (s stubValidator) ValidateMovement(simulation.MovementRequest) *simulation.MoveError {
	if s.refuse {
		return &simulation.MoveError{NativeErrorCode: 0x02, Reason: "deepWaterDestination"}
	}
	return nil
}

/*
================
recordedPeerMovement
================
*/
func recordedPeerMovement(p *recordingPusher) []simulation.Frame {
	var out []simulation.Frame
	for _, f := range p.toSession {
		if f.Opcode == wire.OpObjectSourceMove || f.Opcode == wire.OpObjectSourceCorrection {
			out = append(out, f)
		}
	}
	return out
}
