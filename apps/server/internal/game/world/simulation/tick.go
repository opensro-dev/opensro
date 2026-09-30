/*
===========================================================================

tick.go - the simulation tick: legs, hooks and frame routing

===========================================================================
*/

package simulation

import (
	"context"
	"opensro.online/server/internal/domain"
	"runtime/debug"
	"sort"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/instance"
)

/*
==================
Frame

Frame is one native packet ready for the transport envelope
{nativeOpcode, payload}. The transport layer (GO-1) owns wrapping it for
WebTransport/WebSocket delivery; this package only guarantees the bytes.
==================
*/
type Frame struct {
	Scope   []domain.ObjectScopeChange `json:"-"`
	Opcode  uint16
	Payload []byte
	// ScopeGID identifies creation/removal committed with this complete push.
	// It is publication metadata, never part of the native packet payload.
	ScopeGID     uint32 `json:"-"`
	ScopeVisible bool   `json:"-"`
	// Current is a delivery-time admission predicate for replaceable movement.
	// Such frames use the ordered lane so death/rebirth cannot be overtaken.
	Current func() bool `json:"-"`
}

// DefaultTickInterval is the single entity/AI coordination cadence. The
// reference target-range navigator refuses refreshes before 100 ms, but is
// eligible on the first owner tick at or after that deadline. A slower owner
// lets short moving-target stand-off legs expire and visibly stop before the
// next goal. Keep scheduling here; chase planners declare deadlines but never
// create independent timers.
const DefaultTickInterval = 100 * time.Millisecond

// DefaultTickShards bounds tick parallelism while giving each division one
// stable owner. A division is never split across workers: peer visibility,
// settle bookkeeping, and monster AI therefore retain their strict order.
const DefaultTickShards = 4

/*
==================
SessionSnapshot

SessionSnapshot is one connected in-world session at tick time.

CONCURRENCY CONTRACT: simulation inputs are VALUE COPIES taken under the
source's own lock (CloneWorldState for the world). Move handlers must
REPLACE WorldState.MoveSegment, never mutate it in place. MovementCurrent
is the exception: the transport invokes this read-only, internally locked
predicate at delivery time to discard work invalidated since the snapshot.
==================
*/
type SessionSnapshot struct {
	SessionID     string
	DivisionID    string
	CharacterID   int64
	WorldInstance uint32
	Population    instance.Lease
	// Non-nil is the transport's detached, revision-checked publication set.
	PublishedObjects []uint32
	// CombatEligible reports whether this session represents a living,
	// targetable player (LIFE == 1 in native terms). Dead players or sessions
	// with pending deletion retain their viewer ring for world visibility,
	// but are excluded from AI combat candidates and sight acquisition.
	CombatEligible bool
	// Raw character-state byte, evaluated against each observer's TID/flags.
	// Global viewer membership must not be filtered by observer detection.
	NativeBodyStatus uint8
	World            WorldState
	MovementCurrent  func() bool `json:"-"`
	// BodyRadius is the character model's RefObjChar BCRadius. It is copied
	// under the character-authority read door so monster combat never reaches
	// back into mutable session or presentation state.
	BodyRadius BodyRadius
	// NpcAnchor is the resolved NPC spawn anchor for this session's viewer
	// (player start profile or the Constantinople shop anchor).
	NpcAnchor Spawn
	// NpcsEnabled gates the roster like MISSION_SPAWN_NPCS=1.
	NpcsEnabled bool
	// Appearance is this session's character appearance for the peer
	// visibility leg (a fresh value copy per snapshot, like everything
	// else here). nil = the session never spawns on other clients.
	Appearance *PeerAppearance
	COS        *PeerCOS
}

/*
==================
CloneWorldState

CloneWorldState deep-copies a WorldState for a SessionSnapshot: the
MoveSegment pointer is copied by value so a concurrent resteer (pointer
replacement) cannot race the tick's read.
==================
*/
func CloneWorldState(w WorldState) WorldState {
	if w.MoveSegment != nil {
		segment := *w.MoveSegment
		w.MoveSegment = &segment
	}
	return w
}

// SessionSource lists the sessions a tick serves. Implemented by GO-1's
// session registry; must return safe copies (see SessionSnapshot).
type SessionSource interface {
	SnapshotSessions() []SessionSnapshot
}

/*
==================
Pusher

Pusher delivers frames to sessions. Implemented by GO-1's transport.
PushToDivision excludes exceptSessionID (the origin session already got
its own ack); empty string excludes nobody.

DELIVERY CONTRACT (REV-4 C26/C28, REV-2 reliability classes):
  - Push* MUST NOT block on network I/O: RunTick calls them synchronously,
    so a direct Conn write would let one slow peer stall the global tick.
    Enqueue to per-session bounded queues and return.
  - Payload ownership transfers to the Pusher. Every Frame this package
    pushes carries a freshly allocated payload (the encoders never reuse
    buffers), so the Pusher may retain it without copying.
  - Movement-only batches (unguarded 0x30E3/0xB2F5) use the unreliable,
    coalesced lane. A batch containing an action, life, HP, scope or guarded
    frame is one ordered transaction, including any movement prelude. Do
    not split its frames across queues or separate enqueue transactions.

==================
*/
type Pusher interface {
	PushToSession(sessionID string, frames []Frame)
	PushToDivision(divisionID string, frames []Frame, exceptSessionID string)
}

// Clock abstracts time for deterministic tests.
type Clock func() time.Time

/*
==================
DivisionFrames

DivisionFrames routes one causally ordered frame batch. The ordinary route
is a division broadcast, optionally excluding one session. A non-zero
OnlyCharacterID instead targets the one live session bound to that
character in DivisionID. The targeted arm exists for actor-private packet
tails produced by tick-owned gameplay: it keeps B245/public presentation
and the following private progression in the SAME coordinator turn rather
than laundering the private tail through a future-tick queue.

ExceptSessionID and OnlyCharacterID are mutually exclusive. Invalid mixed
routes fail closed in runHooks.
==================
*/
type DivisionFrames struct {
	// SourceGID routes public object events through current observed scope.
	// The source PC receives its own public event as well.
	SourceGID       uint32
	DivisionID      string
	Frames          []Frame
	ExceptSessionID string
	OnlyCharacterID int64
}

/*
==================
TickHook

TickHook runs on the coordinator once per tick, after every division shard
completes. It exists so periodic world jobs share THIS clock instead of
spawning second tickers (REV-4 C30) - e.g. the ground-item TTL sweep
returning its 0x36AB despawn broadcasts. Implementations must not block
and must take their own locks around shared state; returned frames follow
the Pusher payload-ownership contract.
==================
*/
type TickHook func(nowMs int64) []DivisionFrames

/*
==================
Ticker

Ticker is the server push tick: the capability the HTTP GET/POST transport
never had and the reason for the migration. Each tick it

 1. pushes one 0x30E3 patrol move per roster NPC to each NPC-enabled
    session (the exact bytes the reference npc-tick pull seam served, now
    server-driven with a server-clock tick index);
 2. broadcasts each in-flight mover's LIVE interpolated position (bug D
    plane, LiveSpawnAt) to division peers as 0x30E3 with the player gid -
    never the move goal;
 3. on the tick a segment completes, broadcasts one 0xB2F5 settle
    correction at the goal (the lenient PathCtl hard-stop + reposition),
    then goes quiet for that mover;
 4. pushes the player character-appearance rows (the once-open GO-4 lane,
    closed by runPeerVisibility): one 0x30D7 CICUser spawn row per
    (viewer, peer) pair when a same-division peer first becomes visible,
    and one 0x36AB despawn when its session leaves. This is what resolves
    the leg-2/3 mover gids into visible CICUsers - before a spawn row
    lands the client parks the unresolved gid in the object-track table,
    which is the native 0x30E3 behavior.

==================
*/
type Ticker struct {
	Source   SessionSource
	Push     Pusher
	Roster   []NpcDef
	Interval time.Duration
	// ShardCount is the fixed worker count. Values <= 0 use
	// DefaultTickShards. Division IDs are assigned by a stable hash.
	ShardCount int
	Now        Clock
	// Hooks run once at the end of every coordinated tick (see TickHook).
	Hooks []TickHook
	// BeforeHooks settle already accepted monster actions before division AI
	// makes another decision at this clock. They run on the coordinator and
	// use the same scoped publication path as end-of-tick hooks.
	BeforeHooks []TickHook
	// Monsters is the monster mover + scope-stream leg (monstertick.go).
	// Wired whenever the population is enabled, which is the DEFAULT;
	// nil only under the MISSION_SPAWN_MONSTERS=0 kill switch (then the
	// leg costs nothing).
	Monsters *MonsterMoverOps
	// PlayerMap enables the beta world map roster leg (playermap.go).
	PlayerMap bool

	// tickIndex is the coordinator-owned server-clock tick counter fed to
	// the patrol function.
	tickIndex int64
	// states is coordinator-owned. Each value is used only by the worker that
	// owns its division, so hot-path state needs no cross-shard mutex.
	states map[string]*divisionTickState
	done   chan struct{}
	once   sync.Once
}

/*
================
divisionTickState
================
*/
type divisionTickState struct {
	settled             map[string]int64
	shownPeers          map[string]map[uint32]bool
	peerPaths           map[string]map[uint32]peerPath
	peerVisibilityKeys  []peerVisibilityKey
	peerVisibilityValid bool
	shownCOS            map[string]map[uint32]shownCOS
	monsters            *MonsterMoverOps
	playerMapDueMs      int64
}

/*
================
divisionTickWork
================
*/
type divisionTickWork struct {
	divisionID string
	sessions   []SessionSnapshot
	state      *divisionTickState
}

/*
================
shardTickBatch
================
*/
type shardTickBatch struct {
	tick  int64
	nowMs int64
	work  []divisionTickWork
	done  chan struct{}
}

// NewTicker wires a Ticker with the production cadence. The composition root
// must install the same validated NPC roster used by enter-world bootstrap.
/*
================
NewTicker
================
*/
func NewTicker(source SessionSource, push Pusher) *Ticker {
	return &Ticker{
		Source:     source,
		Push:       push,
		Interval:   DefaultTickInterval,
		ShardCount: DefaultTickShards,
		Now:        time.Now,
		done:       make(chan struct{}),
	}
}

// Done closes after Run has stopped and joined every shard worker.
/*
================
Done
================
*/
func (t *Ticker) Done() <-chan struct{} {
	return t.done
}

/*
==================
Run

Run drives one aligned coordinator clock and a fixed division-sharded
worker set until ctx is done. time.Ticker's single-slot channel coalesces
overruns: the scheduler never builds a catch-up queue.
==================
*/
func (t *Ticker) Run(ctx context.Context) {
	t.once.Do(func() {
		defer close(t.done)
		t.run(ctx)
	})
}

/*
================
run
================
*/
func (t *Ticker) run(ctx context.Context) {
	interval := t.Interval
	if interval <= 0 {
		interval = DefaultTickInterval
	}
	shardCount := t.ShardCount
	if shardCount <= 0 {
		shardCount = DefaultTickShards
	}

	inboxes := make([]chan shardTickBatch, shardCount)
	var workers sync.WaitGroup
	workers.Add(shardCount)
	for i := range inboxes {
		inboxes[i] = make(chan shardTickBatch)
		go func(inbox <-chan shardTickBatch) {
			defer workers.Done()
			t.runShard(ctx, inbox)
		}(inboxes[i])
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	defer workers.Wait()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			t.runScheduledTick(ctx, now.UnixMilli(), inboxes)
		}
	}
}

/*
================
runShard
================
*/
func (t *Ticker) runShard(ctx context.Context, inbox <-chan shardTickBatch) {
	for {
		select {
		case <-ctx.Done():
			return
		case batch := <-inbox:
			for _, work := range batch.work {
				t.runDivision(work, batch.tick, batch.nowMs)
			}
			close(batch.done)
		}
	}
}

/*
==================
recoverTickPanic

recoverTickPanic logs a recovered tick panic with its stack and lets the
coordinator and shard workers live on: a panic in one division, hook, or
session must cost at most that scope, never the process.
==================
*/
func recoverTickPanic(scope string) {
	if r := recover(); r != nil {
		log.Errorf("simulation: %s panicked (recovered, ticker continues): %v\n%s", scope, r, debug.Stack())
	}
}

/*
==================
RunTick

RunTick executes one coordinated tick synchronously. It is exposed for
deterministic tests and maintenance callers; production Run uses the same
prepared division work on the fixed shard workers.
==================
*/
func (t *Ticker) RunTick(nowMs int64) {
	defer recoverTickPanic("tick")

	tick, work := t.prepareTick()
	t.runHookList(t.BeforeHooks, nowMs, work)
	for _, division := range work {
		t.runDivision(division, tick, nowMs)
	}
	t.runHooks(nowMs, work)
}

/*
================
runScheduledTick
================
*/
func (t *Ticker) runScheduledTick(ctx context.Context, nowMs int64, inboxes []chan shardTickBatch) {
	defer recoverTickPanic("scheduled tick")

	tick, work := t.prepareTick()
	t.runHookList(t.BeforeHooks, nowMs, work)
	batches := make([]shardTickBatch, len(inboxes))
	for i := range batches {
		batches[i] = shardTickBatch{tick: tick, nowMs: nowMs, done: make(chan struct{})}
	}
	for _, division := range work {
		owner := tickShardForDivision(division.divisionID, len(inboxes))
		batches[owner].work = append(batches[owner].work, division)
	}

	dispatched := make([]shardTickBatch, 0, len(batches))
	for i, batch := range batches {
		if len(batch.work) == 0 {
			continue
		}
		select {
		case <-ctx.Done():
			for _, sent := range dispatched {
				<-sent.done
			}
			return
		case inboxes[i] <- batch:
			dispatched = append(dispatched, batch)
		}
	}
	for _, batch := range dispatched {
		<-batch.done
	}
	if ctx.Err() == nil {
		t.runHooks(nowMs, work)
	}
}

/*
================
prepareTick
================
*/
func (t *Ticker) prepareTick() (int64, []divisionTickWork) {
	tick := t.tickIndex
	t.tickIndex++

	sessions := t.Source.SnapshotSessions()
	sort.SliceStable(sessions, func(i, j int) bool {
		if sessions[i].DivisionID != sessions[j].DivisionID {
			return sessions[i].DivisionID < sessions[j].DivisionID
		}
		if sessions[i].SessionID != sessions[j].SessionID {
			return sessions[i].SessionID < sessions[j].SessionID
		}
		return sessions[i].CharacterID < sessions[j].CharacterID
	})

	if t.states == nil {
		t.states = make(map[string]*divisionTickState)
	}
	liveDivisions := make(map[string]bool)
	work := make([]divisionTickWork, 0)
	for i := 0; i < len(sessions); {
		j := i + 1
		for j < len(sessions) && sessions[j].DivisionID == sessions[i].DivisionID {
			j++
		}
		divisionID := sessions[i].DivisionID
		liveDivisions[divisionID] = true
		state := t.states[divisionID]
		if state == nil {
			state = &divisionTickState{
				settled:    make(map[string]int64),
				shownPeers: make(map[string]map[uint32]bool),
				monsters:   cloneMonsterMoverOps(t.Monsters),
			}
			t.states[divisionID] = state
		}
		if state.monsters != nil {
			state.monsters.divisionID = divisionID
		}
		work = append(work, divisionTickWork{
			divisionID: divisionID,
			sessions:   sessions[i:j],
			state:      state,
		})
		i = j
	}
	// Actor lifetimes belong to allocated worlds. Removing the last session
	// must not delete the division's AI work before it reaches PENDING.
	if t.Monsters != nil && t.Monsters.Monsters != nil {
		for _, divisionID := range t.Monsters.Monsters.behaviorDivisions() {
			if liveDivisions[divisionID] {
				continue
			}
			liveDivisions[divisionID] = true
			state := t.states[divisionID]
			if state == nil {
				state = &divisionTickState{settled: make(map[string]int64), shownPeers: make(map[string]map[uint32]bool), monsters: cloneMonsterMoverOps(t.Monsters)}
				t.states[divisionID] = state
			}
			state.monsters.divisionID = divisionID
			work = append(work, divisionTickWork{divisionID: divisionID, state: state})
		}
	}
	for divisionID := range t.states {
		if !liveDivisions[divisionID] {
			delete(t.states, divisionID)
		}
	}
	return tick, work
}

/*
================
tickShardForDivision
================
*/
func tickShardForDivision(divisionID string, shardCount int) int {
	const (
		offset = uint32(2166136261)
		prime  = uint32(16777619)
	)
	hash := offset
	for i := 0; i < len(divisionID); i++ {
		hash ^= uint32(divisionID[i])
		hash *= prime
	}
	return int(hash % uint32(shardCount))
}

/*
================
cloneMonsterMoverOps
================
*/
func cloneMonsterMoverOps(source *MonsterMoverOps) *MonsterMoverOps {
	if source == nil {
		return nil
	}
	clone := *source
	clone.shownMonsters = nil
	clone.breachLogged = nil
	return &clone
}

/*
================
runDivision
================
*/
func (t *Ticker) runDivision(work divisionTickWork, tick, nowMs int64) {
	defer recoverTickPanic("division " + work.divisionID)

	live := make(map[string]bool, len(work.sessions))
	for _, session := range work.sessions {
		live[session.SessionID] = true
	}
	t.runPeerVisibility(work.state, nowMs, work.sessions, live)
	t.runPeerCOSVisibility(work.state, nowMs, work.sessions, live)
	t.runPlayerMap(work.state, nowMs, work.sessions)
	for _, session := range work.sessions {
		t.runSessionLegs(work.state, session, tick, nowMs)
	}
	for sessionID := range work.state.settled {
		if !live[sessionID] {
			delete(work.state.settled, sessionID)
		}
	}
	work.state.monsters.RunMonsterLeg(nowMs, work.sessions, t.Push)
}

/*
================
runHooks
================
*/
func (t *Ticker) runHooks(nowMs int64, work []divisionTickWork) {
	t.runHookList(t.Hooks, nowMs, work)
}

/*
================
runHookList
================
*/
func (t *Ticker) runHookList(hooks []TickHook, nowMs int64, work []divisionTickWork) {
	for _, hook := range hooks {
		func() {
			defer recoverTickPanic("hook")
			for _, routed := range hook(nowMs) {
				if len(routed.Frames) == 0 {
					continue
				}
				if routed.OnlyCharacterID != 0 {
					if routed.ExceptSessionID != "" || routed.SourceGID != 0 {
						continue
					}
					for _, division := range work {
						if division.divisionID != routed.DivisionID {
							continue
						}
						for _, session := range division.sessions {
							if session.CharacterID == routed.OnlyCharacterID {
								t.Push.PushToSession(session.SessionID, routed.Frames)
								break
							}
						}
						break
					}
					continue
				}
				if routed.SourceGID != 0 {
					for _, division := range work {
						if division.divisionID != routed.DivisionID {
							continue
						}
						for _, viewer := range division.sessions {
							if viewer.SessionID == routed.ExceptSessionID {
								continue
							}
							peer := PlayerObjectID(viewer.CharacterID) == routed.SourceGID || division.state.shownPeers[viewer.SessionID][routed.SourceGID]
							published := false
							if routed.SourceGID > domain.GroundItemGIDBase && routed.SourceGID <= domain.GroundItemGIDLimit {
								for _, gid := range viewer.PublishedObjects {
									if gid == routed.SourceGID {
										published = true
										break
									}
								}
							}
							_, cos := division.state.shownCOS[viewer.SessionID][routed.SourceGID]
							monster := false
							if ops := division.state.monsters; ops != nil {
								monster = ops.shownMonsters[viewer.SessionID][routed.SourceGID]
							}
							if peer || published || cos || monster {
								t.Push.PushToSession(viewer.SessionID, routed.Frames)
							}
						}
						break
					}
					continue
				}
				t.Push.PushToDivision(routed.DivisionID, routed.Frames, routed.ExceptSessionID)
			}
		}()
	}
}

/*
==================
runSessionLegs

runSessionLegs runs legs 1-3 for one session behind its own recover, so
one bad session (a corrupt snapshot, a panicking pusher) cannot blank
the rest of the world's tick.
==================
*/
func (t *Ticker) runSessionLegs(state *divisionTickState, session SessionSnapshot, tick, nowMs int64) {
	defer recoverTickPanic("session " + session.SessionID)

	// All authored NPCs (including ferry managers and city gates) must enter
	// and leave with the viewer throughout travel, not only during login.
	if frames := NpcScopeFrames(t.Roster, session, nowMs); len(frames) != 0 {
		t.Push.PushToSession(session.SessionID, frames)
	}

	// Leg 1: the per-session NPC entity tick.
	if session.NpcsEnabled && len(t.Roster) > 0 {
		frames := NpcMoveFrames(t.Roster, session.NpcAnchor, tick)
		t.Push.PushToSession(session.SessionID, frames)
	}

	// Legs 2+3: the other-player movement broadcast.
	segment := session.World.MoveSegment
	if !segment.Valid() {
		delete(state.settled, session.SessionID)
		return
	}
	gid := PlayerObjectID(session.CharacterID)
	if nowMs >= segment.ArrivesAtMs {
		// Segment matured: one settle correction at the goal, then quiet.
		if state.settled[session.SessionID] == segment.ArrivesAtMs {
			return
		}
		state.settled[session.SessionID] = segment.ArrivesAtMs
		goal := session.World.Spawn
		correction := wire.ObjectSourceCorrection{
			Gid: gid,
			Position: wire.Position{
				RegionID: goal.RegionID,
				X:        float32(goal.X),
				Y:        float32(goal.Y),
				Z:        float32(goal.Z),
				Heading:  goal.Angle,
			},
		}
		t.pushPeerMovement(state, gid, []Frame{
			{Opcode: wire.OpObjectSourceCorrection, Payload: correction.Encode(), Current: session.MovementCurrent},
		})
		return
	}

	// Destination admission precedes the source sample: retail 30E3 only
	// reseeds the path; it cannot start one for an already visible peer.
	t.publishPeerPath(state, session, nowMs)

	// In flight: the LIVE interpolated position, never the goal (bug D).
	delete(state.settled, session.SessionID)
	liveSpawn := session.World.LiveSpawnAt(nowMs)
	move := wire.ObjectSourceMove{
		Position: wire.Position{
			RegionID: liveSpawn.RegionID,
			X:        float32(liveSpawn.X),
			Y:        float32(liveSpawn.Y),
			Z:        float32(liveSpawn.Z),
			Heading:  liveSpawn.Angle,
		},
		Gid: gid,
	}
	t.pushPeerMovement(state, gid, []Frame{
		{Opcode: wire.OpObjectSourceMove, Payload: move.Encode(), Current: session.MovementCurrent},
	})
}

/*
================
pushPeerMovement
================
*/
func (t *Ticker) pushPeerMovement(state *divisionTickState, gid uint32, frames []Frame) {
	for viewer, shown := range state.shownPeers {
		if shown[gid] {
			t.Push.PushToSession(viewer, frames)
		}
	}
}
