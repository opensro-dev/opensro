package simulation

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"opensro.online/server/internal/game/item/wire"
)

type fakeSource struct {
	sessions []SessionSnapshot
}

func (f *fakeSource) SnapshotSessions() []SessionSnapshot {
	return f.sessions
}

type pushedToSession struct {
	sessionID string
	frames    []Frame
}

type pushedToDivision struct {
	divisionID string
	frames     []Frame
	except     string
}

type fakePusher struct {
	toSession  []pushedToSession
	toDivision []pushedToDivision
}

func (f *fakePusher) PushToSession(sessionID string, frames []Frame) {
	f.toSession = append(f.toSession, pushedToSession{sessionID: sessionID, frames: frames})
}

func (f *fakePusher) PushToDivision(divisionID string, frames []Frame, exceptSessionID string) {
	f.toDivision = append(f.toDivision, pushedToDivision{divisionID: divisionID, frames: frames, except: exceptSessionID})
}

func newTestTicker(source SessionSource, push Pusher) *Ticker {
	ticker := NewTicker(source, push)
	ticker.Roster = DefaultNpcRoster()
	return ticker
}

func TestProductionTickMeetsTargetRangeNavigatorDeadline(t *testing.T) {
	if DefaultTickInterval != time.Duration(referenceChaseRefreshMinMs)*time.Millisecond {
		t.Fatalf(
			"production tick interval = %s, want the %dms target-range refresh deadline",
			DefaultTickInterval,
			referenceChaseRefreshMinMs,
		)
	}
}

func movingWorld(t *testing.T, nowMs int64) WorldState {
	t.Helper()
	start := EuropeStartProfile()
	world := DefaultWorldState(start)
	result := ApplyMove(&world, PlayerObjectID(1), MovementRequest{
		Mode: MovementAckDestinationMode, RegionID: start.RegionID,
		X: start.X + 200, Y: start.Y, Z: start.Z,
	}, RunMode, nowMs)
	if result.Segment == nil {
		t.Fatal("expected an in-flight segment")
	}
	return world
}

func TestTickerPushesNpcPatrolPerSession(t *testing.T) {
	const startMs = int64(1_784_000_000_000)
	source := &fakeSource{sessions: []SessionSnapshot{
		{SessionID: "s1", DivisionID: "DIV_A", CharacterID: 3, World: DefaultWorldState(EuropeStartProfile()), NpcAnchor: NpcShopSpawn(), NpcsEnabled: true},
		{SessionID: "s2", DivisionID: "DIV_A", CharacterID: 4, World: DefaultWorldState(EuropeStartProfile()), NpcAnchor: NpcShopSpawn(), NpcsEnabled: false},
	}}
	push := &fakePusher{}
	ticker := newTestTicker(source, push)

	ticker.RunTick(startMs)
	ticker.RunTick(startMs + 250)

	if len(push.toSession) != 2 {
		t.Fatalf("NPC pushes = %d, want 2 (one per tick for the enabled session only)", len(push.toSession))
	}
	for i, pushed := range push.toSession {
		if pushed.sessionID != "s1" {
			t.Errorf("push %d went to %q, want s1 (s2 has NPCs disabled)", i, pushed.sessionID)
		}
		wantPatrols := 0
		for _, npc := range DefaultNpcRoster() {
			if npc.Patrol {
				wantPatrols++
			}
		}
		if len(pushed.frames) != wantPatrols {
			t.Errorf("push %d: %d frames, want %d moving NPC(s)", i, len(pushed.frames), wantPatrols)
		}
		if pushed.frames[0].Opcode != wire.OpObjectSourceMove {
			t.Errorf("push %d: opcode 0x%04X, want 0x30E3", i, pushed.frames[0].Opcode)
		}
	}

	// Tick indexes advance the patrol: tick 0 and tick 1 poses differ.
	tick0 := NpcMoveFrames(DefaultNpcRoster(), NpcShopSpawn(), 0)
	tick1 := NpcMoveFrames(DefaultNpcRoster(), NpcShopSpawn(), 1)
	if string(push.toSession[0].frames[0].Payload) != string(tick0[0].Payload) {
		t.Error("first tick payload does not match the tick-0 patrol pose")
	}
	if string(push.toSession[1].frames[0].Payload) != string(tick1[0].Payload) {
		t.Error("second tick payload does not match the tick-1 patrol pose")
	}
}

func TestTickerBroadcastsLiveMoverPositionNotGoal(t *testing.T) {
	const startMs = int64(1_784_000_000_000)
	world := movingWorld(t, startMs)
	source := &fakeSource{sessions: []SessionSnapshot{
		{SessionID: "mover", DivisionID: "DIV_A", CharacterID: 1, World: CloneWorldState(world), Appearance: &PeerAppearance{RefObjID: 1907, Name: "Mover"}},
		peerSession("viewer", "DIV_A", 2, "Viewer"),
	}}
	push := &fakePusher{}
	ticker := newTestTicker(source, push)

	probeMs := startMs + 1000 // mid-flight (200u at 50 u/s = 4000ms)
	ticker.RunTick(probeMs)

	if len(peerMovementPushes(push)) != 1 {
		t.Fatalf("division broadcasts = %d, want 1", len(peerMovementPushes(push)))
	}
	broadcast := peerMovementPushes(push)[0]
	if broadcast.sessionID != "viewer" {
		t.Errorf("movement routed to %q, want viewer", broadcast.sessionID)
	}
	if len(broadcast.frames) != 1 || broadcast.frames[0].Opcode != wire.OpObjectSourceMove {
		t.Fatalf("broadcast frames = %+v, want one 0x30E3", broadcast.frames)
	}

	decoded, err := wire.DecodeObjectSourceMove(broadcast.frames[0].Payload)
	if err != nil {
		t.Fatalf("decoding broadcast 0x30E3: %v", err)
	}
	if decoded.Gid != PlayerObjectID(1) {
		t.Errorf("gid = %d, want %d", decoded.Gid, PlayerObjectID(1))
	}

	// The broadcast position is the LIVE interpolated point (bug D plane).
	wantLive := world.LiveSpawnAt(probeMs)
	if decoded.RegionID != wantLive.RegionID {
		t.Errorf("regionId = %d, want %d", decoded.RegionID, wantLive.RegionID)
	}
	if decoded.X != float32(wantLive.X) || decoded.Y != float32(wantLive.Y) || decoded.Z != float32(wantLive.Z) {
		t.Errorf("live pos = (%v, %v, %v), want (%v, %v, %v)",
			decoded.X, decoded.Y, decoded.Z, float32(wantLive.X), float32(wantLive.Y), float32(wantLive.Z))
	}
	// And never the goal.
	goal := world.Spawn
	if decoded.X == float32(goal.X) && decoded.Z == float32(goal.Z) {
		t.Error("broadcast position equals the move goal - bug D regressed on the tick plane")
	}
}

func TestTickerSettleCorrectionExactlyOnce(t *testing.T) {
	const startMs = int64(1_784_000_000_000)
	world := movingWorld(t, startMs)
	arrival := world.MoveSegment.ArrivesAtMs
	source := &fakeSource{sessions: []SessionSnapshot{
		{SessionID: "mover", DivisionID: "DIV_A", CharacterID: 1, World: CloneWorldState(world), Appearance: &PeerAppearance{RefObjID: 1907, Name: "Mover"}},
		peerSession("viewer", "DIV_A", 2, "Viewer"),
	}}
	push := &fakePusher{}
	ticker := newTestTicker(source, push)

	ticker.RunTick(arrival + 10)  // matured: one 0xB2F5 settle
	ticker.RunTick(arrival + 260) // still matured: quiet
	ticker.RunTick(arrival + 510) // quiet

	if len(peerMovementPushes(push)) != 1 {
		t.Fatalf("division broadcasts = %d, want exactly 1 settle correction", len(peerMovementPushes(push)))
	}
	frame := peerMovementPushes(push)[0].frames[0]
	if frame.Opcode != wire.OpObjectSourceCorrection {
		t.Fatalf("settle opcode = 0x%04X, want 0xB2F5", frame.Opcode)
	}
	decoded, err := wire.DecodeObjectSourceCorrection(frame.Payload)
	if err != nil {
		t.Fatalf("decoding settle 0xB2F5: %v", err)
	}
	goal := world.Spawn
	if decoded.Gid != PlayerObjectID(1) || decoded.RegionID != goal.RegionID ||
		decoded.X != float32(goal.X) || decoded.Z != float32(goal.Z) || decoded.Heading != goal.Angle {
		t.Errorf("settle = %+v, want goal %+v with gid %d", decoded, goal, PlayerObjectID(1))
	}
}

func TestTickerResteerReleasesSettleLatch(t *testing.T) {
	const startMs = int64(1_784_000_000_000)
	world := movingWorld(t, startMs)
	arrival := world.MoveSegment.ArrivesAtMs
	source := &fakeSource{sessions: []SessionSnapshot{
		{SessionID: "mover", DivisionID: "DIV_A", CharacterID: 1, World: CloneWorldState(world), Appearance: &PeerAppearance{RefObjID: 1907, Name: "Mover"}},
		peerSession("viewer", "DIV_A", 2, "Viewer"),
	}}
	push := &fakePusher{}
	ticker := newTestTicker(source, push)

	ticker.RunTick(arrival + 10) // settle #1

	// A new move replaces the segment; the mover flies again.
	ApplyMove(&world, PlayerObjectID(1), MovementRequest{
		Mode: MovementAckDestinationMode, RegionID: world.Spawn.RegionID,
		X: world.Spawn.X - 150, Y: world.Spawn.Y, Z: world.Spawn.Z + 90,
	}, RunMode, arrival+500)
	source.sessions[0].World = CloneWorldState(world)

	ticker.RunTick(arrival + 750)                       // in flight: 0x30E3
	ticker.RunTick(world.MoveSegment.ArrivesAtMs + 100) // settle #2

	if len(peerMovementPushes(push)) != 3 {
		t.Fatalf("division broadcasts = %d, want 3 (settle, live move, settle)", len(peerMovementPushes(push)))
	}
	if peerMovementPushes(push)[0].frames[0].Opcode != wire.OpObjectSourceCorrection ||
		peerMovementPushes(push)[1].frames[0].Opcode != wire.OpObjectSourceMove ||
		peerMovementPushes(push)[2].frames[0].Opcode != wire.OpObjectSourceCorrection {
		t.Errorf("opcode sequence = [0x%04X 0x%04X 0x%04X], want [0xB2F5 0x30E3 0xB2F5]",
			peerMovementPushes(push)[0].frames[0].Opcode, peerMovementPushes(push)[1].frames[0].Opcode, peerMovementPushes(push)[2].frames[0].Opcode)
	}
}

func TestTickerCleansDepartedSessions(t *testing.T) {
	const startMs = int64(1_784_000_000_000)
	world := movingWorld(t, startMs)
	arrival := world.MoveSegment.ArrivesAtMs
	source := &fakeSource{sessions: []SessionSnapshot{
		{SessionID: "mover", DivisionID: "DIV_A", CharacterID: 1, World: CloneWorldState(world)},
	}}
	push := &fakePusher{}
	ticker := newTestTicker(source, push)

	ticker.RunTick(arrival + 10)
	if got := len(ticker.states["DIV_A"].settled); got != 1 {
		t.Fatalf("settled entries = %d, want 1", got)
	}

	source.sessions = nil // session departs
	ticker.RunTick(arrival + 260)
	if _, retained := ticker.states["DIV_A"]; retained {
		t.Error("departed division state retained, want no leak")
	}
}

func TestCloneWorldStateIsolatesSegment(t *testing.T) {
	world := movingWorld(t, 1_784_000_000_000)
	clone := CloneWorldState(world)
	if clone.MoveSegment == world.MoveSegment {
		t.Fatal("clone shares the MoveSegment pointer; tick snapshots must be independent")
	}
	if *clone.MoveSegment != *world.MoveSegment {
		t.Fatal("clone segment values differ from the original")
	}
}

// TestTickerRunConcurrentWithMovingWorld drives Ticker.Run for real while a
// writer goroutine resteers the character every millisecond, mimicking move
// handlers racing the tick (REV-4 C27-3). The session source hands out deep
// copies under its own lock - the snapshot contract - so `go test -race`
// proves the ownership model, not luck.
func TestTickerRunConcurrentWithMovingWorld(t *testing.T) {
	const startMs = int64(1_784_000_000_000)

	world := movingWorld(t, startMs)
	var mu sync.Mutex

	source := snapshotFunc(func() []SessionSnapshot {
		mu.Lock()
		defer mu.Unlock()
		return []SessionSnapshot{
			{SessionID: "mover", DivisionID: "DIV_A", CharacterID: 1, World: CloneWorldState(world), NpcAnchor: NpcShopSpawn(), NpcsEnabled: true},
		}
	})
	push := &countingPusher{}
	ticker := newTestTicker(source, push)
	ticker.Interval = time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker.Run(ctx)
	}()

	deadline := time.Now().Add(60 * time.Millisecond)
	for at := startMs; time.Now().Before(deadline); at += 40 {
		mu.Lock()
		ApplyMove(&world, PlayerObjectID(1), MovementRequest{
			Mode: MovementAckDestinationMode, RegionID: world.Spawn.RegionID,
			X: clampFloat(world.Spawn.X+7, 0, 0xffff), Y: world.Spawn.Y, Z: clampFloat(world.Spawn.Z+3, 0, 0xffff),
		}, RunMode, at)
		mu.Unlock()
		time.Sleep(time.Millisecond) //nolint:forbidigo // concurrency stress: moves interleave with the real ticker
	}
	cancel()
	<-done

	if push.sessionPushes.Load() == 0 || push.divisionPushes.Load() != 0 {
		t.Errorf("expected NPC session pushes and no unobserved movement broadcasts (session=%d division=%d)",
			push.sessionPushes.Load(), push.divisionPushes.Load())
	}
}

type snapshotFunc func() []SessionSnapshot

func (f snapshotFunc) SnapshotSessions() []SessionSnapshot { return f() }

type countingPusher struct {
	sessionPushes  atomic.Int64
	divisionPushes atomic.Int64
}

func (c *countingPusher) PushToSession(string, []Frame) { c.sessionPushes.Add(1) }

func (c *countingPusher) PushToDivision(string, []Frame, string) { c.divisionPushes.Add(1) }

func TestTickerRunsHooksOnTickClock(t *testing.T) {
	source := &fakeSource{sessions: []SessionSnapshot{}}
	push := &fakePusher{}
	ticker := newTestTicker(source, push)

	var hookNows []int64
	despawn := NpcDespawnFrames(DefaultNpcRoster())
	ticker.Hooks = append(ticker.Hooks,
		NamedTickHook{Hook: func(nowMs int64) []DivisionFrames {
			hookNows = append(hookNows, nowMs)
			return []DivisionFrames{{DivisionID: "DIV_A", Frames: despawn}}
		}},
		NamedTickHook{Hook: func(int64) []DivisionFrames {
			return []DivisionFrames{{DivisionID: "DIV_B", Frames: nil}} // empty: not pushed
		}},
	)

	ticker.RunTick(1000)
	ticker.RunTick(1250)

	if len(hookNows) != 2 || hookNows[0] != 1000 || hookNows[1] != 1250 {
		t.Errorf("hook clock = %v, want [1000 1250]", hookNows)
	}
	if len(push.toDivision) != 2 {
		t.Fatalf("division pushes = %d, want 2 (empty hook results are skipped)", len(push.toDivision))
	}
	for _, pushed := range push.toDivision {
		if pushed.divisionID != "DIV_A" || len(pushed.frames) != len(despawn) {
			t.Errorf("hook push = %+v, want DIV_A with %d frames", pushed, len(despawn))
		}
	}
}

type orderedHookPush struct {
	kind     string
	target   string
	opcode   uint16
	excluded string
}

type orderedHookPusher struct {
	rows []orderedHookPush
}

func (p *orderedHookPusher) PushToSession(sessionID string, frames []Frame) {
	for _, frame := range frames {
		p.rows = append(p.rows, orderedHookPush{kind: "session", target: sessionID, opcode: frame.Opcode})
	}
}

func (p *orderedHookPusher) PushToDivision(divisionID string, frames []Frame, exceptSessionID string) {
	for _, frame := range frames {
		p.rows = append(p.rows, orderedHookPush{
			kind: "division", target: divisionID, opcode: frame.Opcode, excluded: exceptSessionID,
		})
	}
}

func TestHookTargetRoutePreservesPublicThenActorPrivateOrder(t *testing.T) {
	push := &orderedHookPusher{}
	ticker := newTestTicker(&fakeSource{}, push)
	ticker.Hooks = []NamedTickHook{{Hook: func(int64) []DivisionFrames {
		return []DivisionFrames{
			{DivisionID: "DIV_A", Frames: []Frame{{Opcode: wire.OpSkillCastResult}}},
			{DivisionID: "DIV_A", OnlyCharacterID: 7, Frames: []Frame{{Opcode: wire.OpExpUpdate}}},
		}
	}}}
	work := []divisionTickWork{{divisionID: "DIV_A", sessions: []SessionSnapshot{
		{SessionID: "actor", DivisionID: "DIV_A", CharacterID: 7},
		{SessionID: "peer", DivisionID: "DIV_A", CharacterID: 8},
	}}}
	ticker.runHooks(1000, work)

	if len(push.rows) != 2 ||
		push.rows[0].kind != "division" || push.rows[0].target != "DIV_A" ||
		push.rows[0].opcode != wire.OpSkillCastResult ||
		push.rows[1].kind != "session" || push.rows[1].target != "actor" ||
		push.rows[1].opcode != wire.OpExpUpdate {
		t.Fatalf("hook route order = %+v, want public B245 then actor-only 30D2", push.rows)
	}
}

func TestHookTargetRouteFailsClosedWhenMixedWithExclusion(t *testing.T) {
	push := &orderedHookPusher{}
	ticker := newTestTicker(&fakeSource{}, push)
	ticker.Hooks = []NamedTickHook{{Hook: func(int64) []DivisionFrames {
		return []DivisionFrames{{
			DivisionID: "DIV_A", OnlyCharacterID: 7, ExceptSessionID: "peer",
			Frames: []Frame{{Opcode: wire.OpExpUpdate}},
		}}
	}}}
	ticker.runHooks(1000, []divisionTickWork{{divisionID: "DIV_A", sessions: []SessionSnapshot{
		{SessionID: "actor", DivisionID: "DIV_A", CharacterID: 7},
	}}})
	if len(push.rows) != 0 {
		t.Fatalf("invalid mixed target/exclusion route escaped: %+v", push.rows)
	}
}

/*
================
TestExceptCharacterRouteFailsClosedWithoutSource

ExceptCharacterID applies to a SourceGID route only. A division broadcast
cannot leave a character out, so such a batch is dropped rather than
reaching the very character it excludes.
================
*/
func TestExceptCharacterRouteFailsClosedWithoutSource(t *testing.T) {
	push := &orderedHookPusher{}
	ticker := newTestTicker(&fakeSource{}, push)
	ticker.Hooks = []NamedTickHook{{Hook: func(int64) []DivisionFrames {
		return []DivisionFrames{{DivisionID: "DIV_A", ExceptCharacterID: 7, Frames: []Frame{{Opcode: wire.OpSkillCastResult}}}}
	}}}
	ticker.runHooks(1000, []divisionTickWork{{divisionID: "DIV_A", sessions: []SessionSnapshot{
		{SessionID: "owner", DivisionID: "DIV_A", CharacterID: 7},
	}}})
	if len(push.rows) != 0 {
		t.Fatalf("an excluding route without a source escaped: %+v", push.rows)
	}
}

func TestNormalizeDropsInvalidSegment(t *testing.T) {
	start := EuropeStartProfile()
	world := DefaultWorldState(start)
	world.MoveSegment = &MoveSegment{From: start, StartedAtMs: 10, ArrivesAtMs: 10}
	world.MovementMode = 0
	world.SpawnSet = true
	world.Normalize()
	if world.MoveSegment != nil {
		t.Error("arrivesAtMs == startedAtMs must degrade to settled (nil segment)")
	}
	if world.MovementMode != RunMode {
		t.Errorf("movementMode = %d, want run fallback %d", world.MovementMode, RunMode)
	}
	if !world.MovementSourceSeeded {
		t.Error("spawnSet must imply movementSourceSeeded")
	}
}

func peerMovementPushes(push *fakePusher) []pushedToSession {
	var out []pushedToSession
	for _, p := range push.toSession {
		for _, f := range p.frames {
			if f.Opcode == wire.OpObjectSourceMove || f.Opcode == wire.OpObjectSourceCorrection {
				out = append(out, pushedToSession{sessionID: p.sessionID, frames: []Frame{f}})
			}
		}
	}
	return out
}
