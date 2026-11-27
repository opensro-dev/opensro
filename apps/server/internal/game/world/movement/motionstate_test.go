package movement

import (
	"bytes"
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

// motionTestRuntime is testRuntime with a MUTABLE clock, for the mid-flight
// re-time leg.
func motionTestRuntime(character *enterworld.Character, nowMs *int64) *Runtime {
	rt := testRuntime(character)
	rt.Now = func() time.Time { return time.UnixMilli(*nowMs) }
	return rt
}

// want3122 is the exact 0x3122 body the client fold CPSMission_OnEntityStateUpdate0x3122 reads:
// [u32 gid][u8 stateType=1][u8 value]. gid for test character ID 7 is
// 100007 = 0x000186A7 little-endian.
func want3122(value uint8) []byte {
	return []byte{0xA7, 0x86, 0x01, 0x00, 0x01, value}
}

func assertMotionSuccess(t *testing.T, outcome MotionOutcome, value uint8) {
	t.Helper()
	if outcome.Refusal != "" {
		t.Fatalf("refused: %s", outcome.Refusal)
	}
	if len(outcome.Frames) != 2 {
		t.Fatalf("frames = %+v, want 0xB017 ack + 0x3122 push", outcome.Frames)
	}
	if outcome.Frames[0].Opcode != simulation.OpMotionStateAck ||
		!bytes.Equal(outcome.Frames[0].Payload, []byte{0x01}) {
		t.Errorf("ack = 0x%04X % X, want 0xB017 [01]",
			outcome.Frames[0].Opcode, outcome.Frames[0].Payload)
	}
	if outcome.Frames[1].Opcode != wire.OpObjectStateRefresh ||
		!bytes.Equal(outcome.Frames[1].Payload, want3122(value)) {
		t.Errorf("state push = 0x%04X % X, want 0x3122 % X",
			outcome.Frames[1].Opcode, outcome.Frames[1].Payload, want3122(value))
	}
	if len(outcome.Broadcast) != 1 ||
		outcome.Broadcast[0].Opcode != wire.OpObjectStateRefresh ||
		!bytes.Equal(outcome.Broadcast[0].Payload, want3122(value)) {
		t.Errorf("broadcast = %+v, want the same 0x3122 % X", outcome.Broadcast, want3122(value))
	}
}

func TestHandleMotionStateWalkRunSetsModeAndAnswers(t *testing.T) {
	character := testCharacter()
	nowMs := testStartMs
	rt := motionTestRuntime(character, &nowMs)
	key := simulation.WorldKey("0", character.Name)

	// Request walk (code 2 - see the label-swap FINDING: 2=walk/3=run in
	// BOTH directions, the client CICharactor_ApplyMovementMode mode table).
	assertMotionSuccess(t, rt.HandleMotionState("0", character, []byte{simulation.WalkMode}), wire.MoveStateWalk)
	world := rt.Worlds.Snapshot(key, func() simulation.WorldState { return simulation.SeedWorldState(character) })
	if world.MovementMode != simulation.WalkMode {
		t.Errorf("world mode = %d, want walk", world.MovementMode)
	}
	// The gait persists through the same write-back plane the move lane owns.
	if character.World == nil || character.World.MovementMode == nil || *character.World.MovementMode != int64(simulation.WalkMode) {
		t.Error("movementMode write-back missing")
	}

	// Back to run.
	assertMotionSuccess(t, rt.HandleMotionState("0", character, []byte{simulation.RunMode}), wire.MoveStateRun)
	world = rt.Worlds.Snapshot(key, func() simulation.WorldState { return simulation.SeedWorldState(character) })
	if world.MovementMode != simulation.RunMode {
		t.Errorf("world mode = %d, want run", world.MovementMode)
	}
}

func TestHandleMotionStateSitStandToggle(t *testing.T) {
	character := testCharacter()
	nowMs := testStartMs
	rt := motionTestRuntime(character, &nowMs)
	key := simulation.WorldKey("0", character.Name)

	// First toggle sits (value 4), second stands (value 0) - the two
	// SetRunWalkMode motion-state arms; sit-vs-stand is server-authoritative.
	assertMotionSuccess(t, rt.HandleMotionState("0", character, []byte{simulation.MotionToggleSitStand}), wire.MoveStateSit)
	world := rt.Worlds.Snapshot(key, func() simulation.WorldState { return simulation.SeedWorldState(character) })
	if !world.Sitting {
		t.Error("world not sitting after first toggle")
	}

	// Each toggle arms the posture-transition lockout, so the clock has to
	// clear it before the next request is heard at all.
	nowMs += PostureTransitionMs
	assertMotionSuccess(t, rt.HandleMotionState("0", character, []byte{simulation.MotionToggleSitStand}), wire.MoveStateStand)
	world = rt.Worlds.Snapshot(key, func() simulation.WorldState { return simulation.SeedWorldState(character) })
	if world.Sitting {
		t.Error("world still sitting after second toggle")
	}
	// Posture never reaches the persisted record (runtime-only contract).
	if character.World != nil && character.World.MovementMode != nil &&
		*character.World.MovementMode != int64(simulation.RunMode) {
		t.Errorf("sit toggle changed persisted movementMode to %d", *character.World.MovementMode)
	}

	// A gait request while seated is ignored whole (audit U8-17: the
	// original's motion-change request takes walk / run only from a
	// standing or moving motion): no ack, no state push, and neither
	// the mode nor the posture moves. An earlier reading from the
	// client's local speed channel answered it; the server's request
	// handler refuses it before the speed setter is ever reached.
	nowMs += PostureTransitionMs
	assertMotionSuccess(t, rt.HandleMotionState("0", character, []byte{simulation.MotionToggleSitStand}), wire.MoveStateSit)
	nowMs += PostureTransitionMs
	outcome := rt.HandleMotionState("0", character, []byte{simulation.WalkMode})
	if outcome.Refusal != "sitting" || len(outcome.Frames) != 0 || len(outcome.Broadcast) != 0 {
		t.Fatalf("seated walk = %+v, want a silent sitting refusal", outcome)
	}
	world = rt.Worlds.Snapshot(key, func() simulation.WorldState { return simulation.SeedWorldState(character) })
	if !world.Sitting {
		t.Error("seated walk request stood the character up")
	}
	if world.MovementMode == simulation.WalkMode {
		t.Error("seated walk request changed the movement mode")
	}

	// Standing first reopens the gait lane.
	nowMs += PostureTransitionMs
	assertMotionSuccess(t, rt.HandleMotionState("0", character, []byte{simulation.MotionToggleSitStand}), wire.MoveStateStand)
	nowMs += PostureTransitionMs
	assertMotionSuccess(t, rt.HandleMotionState("0", character, []byte{simulation.WalkMode}), wire.MoveStateWalk)
}

// TestHandleMotionStateSitTruncatesInFlightMove pins the Ruling-44 sit-cancel:
// sitting DOWN mid-travel stops the walk at the LIVE interpolated point, with
// the full must-have set - paired Spawn:=live + segment clear (A), a
// self-directed 0xB2F5 correction on the MOVEMENT channel AND a peer one (B,
// never on the 0xB017 sit ack B'), and persistence of the truncated point.
// Inputs cannot coincide: the live point (start+50) is distinct from both the
// goal (start+200) and the departure (start), so a stale-goal teleport or a
// no-op both fail loudly.
func TestHandleMotionStateSitTruncatesInFlightMove(t *testing.T) {
	character := testCharacter()
	nowMs := testStartMs
	rt := motionTestRuntime(character, &nowMs)
	key := simulation.WorldKey("0", character.Name)

	start := simulation.EuropeStartProfile()
	if move := rt.HandleMove("0", character, encodeMoveBody(1, start.RegionID, int16(start.X)+200, int16(start.Y), int16(start.Z))); move.Refusal != nil {
		t.Fatalf("move refused: %v", move.Refusal)
	}

	// 1s into the 200u run (50 u/s): live x = start+50, 150u remain.
	nowMs = testStartMs + 1000
	outcome := rt.HandleMotionState("0", character, []byte{simulation.MotionToggleSitStand})
	if outcome.Refusal != "" {
		t.Fatalf("sit refused: %s", outcome.Refusal)
	}

	// (A) the segment is cleared AND the goal is truncated to the live point,
	// paired. A goal left at start+200 with a nil segment would teleport.
	world := rt.Worlds.Snapshot(key, func() simulation.WorldState { return simulation.SeedWorldState(character) })
	if world.MoveSegment.Valid() {
		t.Error("sit did not clear the in-flight segment")
	}
	if world.Spawn.X != start.X+50 {
		t.Errorf("truncated goal x = %v, want the live point %v (start+200 would be a stale-goal teleport)", world.Spawn.X, start.X+50)
	}
	if !world.Sitting {
		t.Error("world not sitting after sit toggle")
	}
	// Persisted: the truncated point is the re-enter truth (write-back).
	if character.World == nil || character.World.Spawn == nil || *character.World.Spawn.X != start.X+50 {
		t.Errorf("truncated point not persisted for re-enter")
	}

	// (B) frames to SELF: ack, 0x3122 sit, and a 0xB2F5 correction at the live
	// point. (B') the correction is on 0xB2F5, NOT the 0xB017 sit ack.
	wantCorrection := wire.ObjectSourceCorrection{
		Gid: enterworld.ObjectIDForCharacter(character),
		Position: wire.Position{
			RegionID: world.Spawn.RegionID,
			X:        float32(world.Spawn.X),
			Y:        float32(world.Spawn.Y),
			Z:        float32(world.Spawn.Z),
			Heading:  world.Spawn.Angle,
		},
	}.Encode()
	if len(outcome.Frames) != 3 {
		t.Fatalf("frames = %d, want 3 (0xB017 ack + 0x3122 sit + 0xB2F5 self correction)", len(outcome.Frames))
	}
	if outcome.Frames[0].Opcode != simulation.OpMotionStateAck {
		t.Errorf("frame0 = 0x%04X, want 0xB017 ack", outcome.Frames[0].Opcode)
	}
	self := outcome.Frames[2]
	if self.Opcode != wire.OpObjectSourceCorrection || !bytes.Equal(self.Payload, wantCorrection) {
		t.Errorf("self correction = 0x%04X % X, want 0xB2F5 % X", self.Opcode, self.Payload, wantCorrection)
	}
	// The sit ack itself must NEVER carry the position (B').
	if outcome.Frames[0].Opcode == wire.OpObjectSourceCorrection {
		t.Error("correction rode the sit-ack slot (B' violated)")
	}
	// Peers get the 0x3122 AND the 0xB2F5.
	if len(outcome.Broadcast) != 2 || outcome.Broadcast[1].Opcode != wire.OpObjectSourceCorrection ||
		!bytes.Equal(outcome.Broadcast[1].Payload, wantCorrection) {
		t.Errorf("broadcast = %+v, want 0x3122 + 0xB2F5 correction", outcome.Broadcast)
	}
}

// TestHandleMotionStateSitClearsPickupLatchOnTruncate asserts Ruling-44
// must-have (F): a sit that truncates an in-flight move releases the
// pickup-approach latch (motionstate.go clears it, exactly as a fresh ground
// move does). It exists BECAUSE (F) is otherwise protected only by memory -
// delete the ClearPendingPickup call and every other test stays green, which
// is the silent-failure mode the (G) truncate test was written to prevent one
// level up. Witnessed-red target: this assertion, not merely the suite.
func TestHandleMotionStateSitClearsPickupLatchOnTruncate(t *testing.T) {
	character := testCharacter()
	nowMs := testStartMs
	rt := motionTestRuntime(character, &nowMs)

	cleared := 0
	var gotDivision, gotName string
	rt.ClearPendingPickup = func(divisionID, characterName string) {
		cleared++
		gotDivision, gotName = divisionID, characterName
	}

	start := simulation.EuropeStartProfile()
	if move := rt.HandleMove("0", character, encodeMoveBody(1, start.RegionID, int16(start.X)+200, int16(start.Y), int16(start.Z))); move.Refusal != nil {
		t.Fatalf("move refused: %v", move.Refusal)
	}
	// HandleMove itself clears the latch on the move request (reference order,
	// runtime.go); reset so this test isolates the SIT truncation's clear.
	cleared = 0

	// Sit mid-flight -> truncation -> must release the pickup latch (F). This
	// is the load-bearing assertion: removing the ClearPendingPickup call from
	// motionstate.go must make THIS line fail, not just the suite.
	nowMs = testStartMs + 1000
	if outcome := rt.HandleMotionState("0", character, []byte{simulation.MotionToggleSitStand}); outcome.Refusal != "" {
		t.Fatalf("sit refused: %s", outcome.Refusal)
	}
	if cleared != 1 {
		t.Fatalf("pickup latch cleared %d times on sit-truncate, want 1 (must-have F: a mid-approach sit must release the pickup ETA, else the character stops short with the approach still armed)", cleared)
	}
	if gotDivision != "0" || gotName != character.Name {
		t.Errorf("clear key = (%s, %s), want (0, %s)", gotDivision, gotName, character.Name)
	}

	// Pin the clear to the TRUNCATE path, not to every sit toggle: standing
	// back up has no in-flight segment, so it does not truncate and must NOT
	// clear the latch.
	cleared = 0
	// Past the posture-transition lockout: the sit above armed it, and while
	// it runs every 0x7017 is dropped, so a stand issued at the same instant
	// would never reach the latch logic this case is about.
	nowMs += PostureTransitionMs
	if outcome := rt.HandleMotionState("0", character, []byte{simulation.MotionToggleSitStand}); outcome.Refusal != "" {
		t.Fatalf("stand refused: %s", outcome.Refusal)
	}
	if cleared != 0 {
		t.Errorf("stand-up cleared the pickup latch %d times, want 0 (only a truncating sit clears)", cleared)
	}
}

// TestHandleMotionStateLocksOutDuringPostureTransition pins the retail
// posture-transition lockout: while a sit/stand change is settling, EVERY
// 0x7017 is dropped silently - the second posture request (no N-spam toggling)
// and a gait request alike, because the later retail build wraps its whole
// posture handler in "not already transitioning" rather than guarding only the
// posture arm. Deleting the gate in motionstate.go must fail THIS test.
func TestHandleMotionStateLocksOutDuringPostureTransition(t *testing.T) {
	character := testCharacter()
	nowMs := testStartMs
	rt := motionTestRuntime(character, &nowMs)
	key := simulation.WorldKey("0", character.Name)

	assertMotionSuccess(t, rt.HandleMotionState("0", character, []byte{simulation.MotionToggleSitStand}), wire.MoveStateSit)

	// Mid-transition: a second posture request is dropped, ships NO frames,
	// and above all does NOT toggle the posture back to standing.
	nowMs = testStartMs + PostureTransitionMs - 1
	outcome := rt.HandleMotionState("0", character, []byte{simulation.MotionToggleSitStand})
	if outcome.Refusal != "postureTransitionInFlight" {
		t.Fatalf("mid-transition toggle refusal = %q, want postureTransitionInFlight (N-spam must not re-toggle)", outcome.Refusal)
	}
	if len(outcome.Frames) != 0 || len(outcome.Broadcast) != 0 {
		t.Errorf("dropped request shipped %d frames / %d broadcast, want 0/0 (retail drops silently)", len(outcome.Frames), len(outcome.Broadcast))
	}
	world := rt.Worlds.Snapshot(key, func() simulation.WorldState { return simulation.SeedWorldState(character) })
	if !world.Sitting {
		t.Error("mid-transition toggle stood the character back up; the lockout must swallow it")
	}

	// A GAIT request in the same window is dropped too - the lockout is the
	// whole handler, not just the posture arm.
	if outcome := rt.HandleMotionState("0", character, []byte{simulation.WalkMode}); outcome.Refusal != "postureTransitionInFlight" {
		t.Errorf("mid-transition gait refusal = %q, want postureTransitionInFlight", outcome.Refusal)
	}
	world = rt.Worlds.Snapshot(key, func() simulation.WorldState { return simulation.SeedWorldState(character) })
	if world.MovementMode == simulation.WalkMode {
		t.Error("mid-transition gait request changed the movement mode; it should have been dropped")
	}

	// Once the window closes the lane reopens and the toggle lands.
	nowMs = testStartMs + PostureTransitionMs
	assertMotionSuccess(t, rt.HandleMotionState("0", character, []byte{simulation.MotionToggleSitStand}), wire.MoveStateStand)
}

// TestHandleMotionStateTransitionDoesNotGateMovement pins the OTHER half, and
// it is the half that is easy to get wrong: the in-progress state is a
// server-internal action lockout, never sent to the client, and the retail
// movement gate reads only life state - it never consults posture. So standing
// up and immediately moving MUST still be accepted. This test exists to stop a
// future reader "fixing" the walk-during-stand-up overlap, which is native.
func TestHandleMotionStateTransitionDoesNotGateMovement(t *testing.T) {
	character := testCharacter()
	nowMs := testStartMs
	rt := motionTestRuntime(character, &nowMs)

	assertMotionSuccess(t, rt.HandleMotionState("0", character, []byte{simulation.MotionToggleSitStand}), wire.MoveStateSit)
	nowMs += PostureTransitionMs
	assertMotionSuccess(t, rt.HandleMotionState("0", character, []byte{simulation.MotionToggleSitStand}), wire.MoveStateStand)

	// Immediately after the stand request, still inside the transition
	// window, a move must be accepted.
	start := simulation.EuropeStartProfile()
	if move := rt.HandleMove("0", character, encodeMoveBody(1, start.RegionID, int16(start.X)+200, int16(start.Y), int16(start.Z))); move.Refusal != nil {
		t.Fatalf("move during the stand-up transition was refused (%v); retail gates movement on LIFE state only, never on posture", move.Refusal)
	}
}

func TestHandleMotionStateRejectsMalformedSilently(t *testing.T) {
	character := testCharacter()
	nowMs := testStartMs
	rt := motionTestRuntime(character, &nowMs)

	cases := [][]byte{
		{},                         // empty
		{simulation.RunMode, 0x00}, // over-long
		{0x00},                     // stand is S->C only, never a request
		{0x01},                     // client CICharactor_ApplyMovementMode no-op band
		{0x05},                     // no-op band
		{0x0C},                     // motion state 0xe channel, not a request
		{0xFF},                     // out of table
	}
	for _, payload := range cases {
		outcome := rt.HandleMotionState("0", character, payload)
		if outcome.Refusal == "" || len(outcome.Frames) != 0 || len(outcome.Broadcast) != 0 {
			t.Errorf("payload % X: refusal=%q frames=%d broadcast=%d, want silent refusal",
				payload, outcome.Refusal, len(outcome.Frames), len(outcome.Broadcast))
		}
	}

	// A refused frame must not have touched the world plane.
	key := simulation.WorldKey("0", character.Name)
	world := rt.Worlds.Snapshot(key, func() simulation.WorldState { return simulation.SeedWorldState(character) })
	if world.MovementMode != simulation.RunMode || world.Sitting {
		t.Errorf("refusals mutated world: mode=%d sitting=%v", world.MovementMode, world.Sitting)
	}

	character.DeletePending = true
	if outcome := rt.HandleMotionState("0", character, []byte{simulation.WalkMode}); outcome.Refusal == "" {
		t.Error("deletePending accepted")
	}
	if outcome := (&Runtime{}).HandleMotionState("0", nil, []byte{simulation.WalkMode}); outcome.Refusal == "" {
		t.Error("nil character accepted")
	}
}

// TestHandleMotionStateRetimesInFlightSegment pins the server mirror of
// client CICharactor_ApplyMovementMode restarting an active path-follow on the new speed channel: a
// mid-move gait change re-times the segment from the LIVE interpolated
// point (bug-D plane), not the goal.
func TestHandleMotionStateRetimesInFlightSegment(t *testing.T) {
	character := testCharacter()
	nowMs := testStartMs
	rt := motionTestRuntime(character, &nowMs)
	key := simulation.WorldKey("0", character.Name)

	start := simulation.EuropeStartProfile()
	move := rt.HandleMove("0", character, encodeMoveBody(1, start.RegionID, int16(start.X)+200, int16(start.Y), int16(start.Z)))
	if move.Refusal != nil {
		t.Fatalf("move refused: %v", move.Refusal)
	}

	// 1s into the 200u run (50 u/s): live x = start+50, 150u remain.
	nowMs = testStartMs + 1000
	assertMotionSuccess(t, rt.HandleMotionState("0", character, []byte{simulation.WalkMode}), wire.MoveStateWalk)

	world := rt.Worlds.Snapshot(key, func() simulation.WorldState { return simulation.SeedWorldState(character) })
	if !world.MoveSegment.Valid() {
		t.Fatal("segment cleared by gait change")
	}
	if world.MoveSegment.From.X != start.X+50 {
		t.Errorf("re-timed departure x = %v, want live point %v", world.MoveSegment.From.X, start.X+50)
	}
	if world.MoveSegment.StartedAtMs != nowMs {
		t.Errorf("re-timed start = %d, want %d", world.MoveSegment.StartedAtMs, nowMs)
	}
	// 150u at walk speed 20 u/s = 7500ms.
	if got := world.MoveSegment.ArrivesAtMs - world.MoveSegment.StartedAtMs; got != 7500 {
		t.Errorf("re-timed travel = %dms, want 7500", got)
	}

	// Same-mode request while in flight: idempotent, segment untouched.
	before := *world.MoveSegment
	assertMotionSuccess(t, rt.HandleMotionState("0", character, []byte{simulation.WalkMode}), wire.MoveStateWalk)
	world = rt.Worlds.Snapshot(key, func() simulation.WorldState { return simulation.SeedWorldState(character) })
	if !world.MoveSegment.Valid() || *world.MoveSegment != before {
		t.Errorf("idempotent walk re-timed the segment: %+v -> %+v", before, world.MoveSegment)
	}
}

// A masked player (msch 1) is heard on no motion code at all (the
// server's motion-change request path drops it before any motion
// handling); a duplicate (msch 2) still sits and changes gait.
func TestHandleMotionStateIgnoresAMaskedPlayer(t *testing.T) {
	for _, mode := range []uint8{1, 2} {
		character := testCharacter()
		character.TransformMode = mode
		nowMs := testStartMs
		rt := motionTestRuntime(character, &nowMs)
		for _, code := range []byte{simulation.WalkMode, simulation.MotionToggleSitStand} {
			outcome := rt.HandleMotionState("0", character, []byte{code})
			if refused := outcome.Refusal == "transformed"; refused != (mode == 1) {
				t.Fatalf("mode %d code %d: outcome %+v", mode, code, outcome)
			}
			nowMs += PostureTransitionMs
		}
	}
}

// While the casting instance is held (a cast preparing or casting, a
// chain), the original ignores every motion-state request (audit
// U8-17). Deleting the MotionChangeLocked consultation must fail THIS
// test, including its sit arm: the lock is the handler's head gate,
// not a gait-only one.
func TestHandleMotionStateIgnoresRequestsWhileCasting(t *testing.T) {
	character := testCharacter()
	nowMs := testStartMs
	rt := motionTestRuntime(character, &nowMs)
	locked := true
	rt.MotionChangeLocked = func(string, string) bool { return locked }

	for _, code := range []byte{simulation.WalkMode, simulation.RunMode, simulation.MotionToggleSitStand} {
		outcome := rt.HandleMotionState("0", character, []byte{code})
		if outcome.Refusal != "motionChangeLocked" || len(outcome.Frames) != 0 || len(outcome.Broadcast) != 0 {
			t.Fatalf("code %d while casting = %+v, want a silent motionChangeLocked refusal", code, outcome)
		}
	}
	key := simulation.WorldKey("0", character.Name)
	world := rt.Worlds.Snapshot(key, func() simulation.WorldState { return simulation.SeedWorldState(character) })
	if world.Sitting || world.MovementMode == simulation.WalkMode {
		t.Errorf("locked requests mutated world: mode=%d sitting=%v", world.MovementMode, world.Sitting)
	}

	locked = false
	assertMotionSuccess(t, rt.HandleMotionState("0", character, []byte{simulation.WalkMode}), wire.MoveStateWalk)
}

// Walk / run is taken only from a standing or moving motion; while
// knocked down (motion 8) the original's request switch reaches no
// gait arm. Deleting the MotionLocked consultation must fail THIS test.
func TestHandleMotionStateRefusesGaitWhileKnockedDown(t *testing.T) {
	character := testCharacter()
	nowMs := testStartMs
	rt := motionTestRuntime(character, &nowMs)
	rt.MotionLocked = func(string, string) bool { return true }

	for _, code := range []byte{simulation.WalkMode, simulation.RunMode} {
		outcome := rt.HandleMotionState("0", character, []byte{code})
		if outcome.Refusal != "knockedDown" || len(outcome.Frames) != 0 || len(outcome.Broadcast) != 0 {
			t.Fatalf("code %d while knocked down = %+v, want a silent knockedDown refusal", code, outcome)
		}
	}
	key := simulation.WorldKey("0", character.Name)
	world := rt.Worlds.Snapshot(key, func() simulation.WorldState { return simulation.SeedWorldState(character) })
	if world.MovementMode == simulation.WalkMode {
		t.Error("knocked-down gait request changed the movement mode")
	}
}
