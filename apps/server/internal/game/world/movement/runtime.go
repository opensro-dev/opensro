/*
===========================================================================

runtime.go - the movement lane: ground clicks and peer appearance

Package movement wires the movement lane onto the transport Hub: the
0x7738 handler (a ground click -> simulation.ApplyMove -> 0xB738 ack, or a
direction walk, direction.go), the 0x72CF/0x72F5 steer and stop pair
(steer.go), the deep-water destination gate, and the enter-world session
glue that makes a player visible to the simulation tick (sessionworld.go).

The world plane is SHARED with the item lane: both read and write the
same simulation.WorldStore states, so a mid-move gold drop lands under the
mover's interpolated position (bug D) and a resteer departs from the live
point the drop handlers saw.

===========================================================================
*/
package movement

import (
	"fmt"
	"time"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/transport"
)

/*
==================
Runtime

Runtime is the movement-lane orchestrator.

CONCURRENCY: commands serialize per character. Unrelated movers execute
independently; the shared WorldStore protects cross-lane reads.
==================
*/
type Runtime struct {
	deps   Dependencies
	Worlds *simulation.WorldStore
	// Validator is the deep-water destination gate; nil accepts everything
	// (the reference behavior when the surface asset is unreadable).
	Validator simulation.MovementValidator
	// PathGuard is the navmesh path-walkability observer (pathguard.go):
	// it classifies the interpolated chord of every accepted-so-far ground
	// move against the walkability plane. The production composition requires
	// enforce mode; nil is allowed only in narrow tests.
	PathGuard *PathGuard
	// ClientClip replicates the client's hard-stop at the first blocking
	// terrain contact (clip.go). The production composition requires apply
	// mode so the server never commits a through-wall destination.
	ClientClip *ClientClip
	// Nav owns surface ownership (navowner.go): every walked move starts
	// from the mover's retained owner and commits the owner it reached, the
	// native source-pNavCell contract. Nil leaves ownership unresolved.
	Nav NavAuthority
	// CanEnterRegion is the world-area policy gate. Movement owns enforcement
	// at the canonical destination boundary; the injected catalog owns access
	// rules and may use authoritative character privileges. Nil is accepted
	// only by narrow tests with no restricted-area catalog.
	CanEnterRegion func(character *enterworld.Character, regionID uint16) bool
	// ClearPendingPickup, when set, clears the character's pickup-approach
	// latch before a ground move applies (the reference clears
	// missionPendingPickups on every accepted move; the tracker itself
	// belongs to the item lane). Wired in main to GO-3's pending tracker.
	ClearPendingPickup func(divisionID, characterName string)
	// ClearCombatIntent releases the server-owned basic-attack continuation.
	// A manual ground command supersedes auto-approach even when its body is
	// malformed/refused, matching the native one-command interaction latch.
	ClearCombatIntent func(divisionID, characterName string)
	// MovementBlocked is the abnormal-state gate of 4B0EA0: a frozen, asleep,
	// rooted or stunned mover's command is dropped silently. Nil admits.
	MovementBlocked func(divisionID, characterName string) bool
	// AttackLocked is 4EF880's attack lock in the same gate: while a skill
	// action holds the casting instance (char+C08) the command is dropped,
	// not queued. Nil admits.
	AttackLocked func(divisionID, characterName string) bool
	// AdvanceResidentRegion commits a crossed live region before this command
	// replaces the segment. The population owner owns the saved-return effect.
	AdvanceResidentRegion func(divisionID, characterName string, nowMs int64)
	// PetPresentation copies the independent pet plane outside the character
	// read door, preserving action -> character lock ordering.
	PetPresentation func(divisionID, characterName string) *simulation.PeerCOS
	// SpawnSkills is the character's active effect projection (the same one
	// the local entry snapshot carries); peer spawn rows publish it.
	SpawnSkills func(divisionID, characterName string) []enterworld.EntrySkill
	// Now abstracts the clock for deterministic tests.
	Now func() time.Time

	npcsEnabled  bool
	npcsAtPlayer bool

	operations characterOperationLocks
	// directions is the registry of direction walks (direction.go).
	directions directionWalks
}

/*
==================
NewRuntime

NewRuntime assembles the movement runtime over the authoritative character
source and shared world store. The source must be the one enter-world uses;
a separate world store would fork the live-position plane.
==================
*/
func NewRuntime(deps Dependencies, worlds *simulation.WorldStore) *Runtime {
	npcPolicy := enterworld.NpcSpawnConfig{}
	if source, ok := deps.(interface {
		NpcSpawnPolicy() enterworld.NpcSpawnConfig
	}); ok {
		npcPolicy = source.NpcSpawnPolicy()
	}
	return &Runtime{
		deps:         deps,
		Worlds:       worlds,
		Now:          time.Now,
		npcsEnabled:  npcPolicy.Enabled,
		npcsAtPlayer: npcPolicy.AtPlayer,
	}
}

/*
==================
ValidateSecurityPolicy

ValidateSecurityPolicy refuses a production composition whose movement
authority is only observing. Unit tests may assemble narrower runtimes,
but the live server must enforce both destination and chord geometry.
==================
*/
func (rt *Runtime) ValidateSecurityPolicy() error {
	if rt.PathGuard == nil || rt.PathGuard.Validator == nil || rt.PathGuard.Mode != PathGuardEnforce {
		return fmt.Errorf("movement: path guard must run in enforce mode")
	}
	if rt.ClientClip == nil || rt.ClientClip.Validator == nil || rt.ClientClip.Mode != ClipApply {
		return fmt.Errorf("movement: client clip must run in apply mode")
	}
	if rt.CanEnterRegion == nil {
		return fmt.Errorf("movement: world-area access policy is required")
	}
	return nil
}

/*
==================
Register

Wires the 0x7738 movement handler, the 0x7017 motion-state handler
(motionstate.go) and the 0x72CF/0x72F5 direction pair (steer.go) onto the
hub.
==================
*/
func (rt *Runtime) Register(hub *transport.Hub) {
	rt.registerPredictedMovement(hub)
	rt.registerMotionState(hub)
	rt.registerDirectionCommands(hub)
	hub.Handle(simulation.OpClientMovementRequest, func(s *transport.Session, _ uint16, payload []byte) {
		character, divisionID, bound := enterworld.SessionCharacter(rt.deps, s)
		if !bound {
			// No enter-world bind. The reference move failure ships no
			// packets (missionMoveFailure -> packets: []), so the wire
			// stays silent; the refusal reason lives in the log.
			log.Debug("movement: 0x7738 before enter-world bind ignored")
			return
		}
		outcome := rt.HandleMove(divisionID, character, payload)
		if outcome.Refusal != nil {
			// Retail keeps movement refusals silent on the wire. Keep the
			// authority verdict observable here, at the one live transport
			// boundary, so every early return in HandleMove is covered without
			// teaching the pure handler about sessions or duplicating logs in
			// individual gates.
			log.WithFields(log.Fields{
				"character": character.Name,
				"reason":    outcome.Refusal.Reason,
			}).Info("movement: 0x7738 refused")
		}
		for _, frame := range outcome.Frames {
			if err := s.SendBatch([]transport.Frame{{Opcode: frame.Opcode, Payload: frame.Payload, Current: frame.Current, Scope: transport.ScopeChanges(frame.Scope)}}); err != nil {
				return
			}
		}
	})
}

/*
==================
MoveOutcome

MoveOutcome is one handled 0x7738: the frames for the acting session
(empty on a refusal - the reference failure envelope carries no packets)
and the refusal, if any, for logging and tests.
==================
*/
type MoveOutcome struct {
	// Captured under the character operation lock, including refused moves.
	Authority    simulation.WorldState
	ServerTimeMs int64
	Frames       []wire.Frame
	Refusal      *simulation.MoveError
	// Result carries the accepted-move state witnesses (nil on refusal).
	Result *simulation.MoveResult
}

/*
==================
refusedMove
==================
*/
func refusedMove(err *simulation.MoveError) MoveOutcome {
	return MoveOutcome{Refusal: err}
}

/*
==================
HandleMove

HandleMove ports moveMissionCharacter over the native 0x7738 body:
decode, deep-water validation, ApplyMove on the shared world plane,
goal-plane write-back, and the 0xB738 ack (reliable lane).

Transport-free so tests drive it without a Hub; Register is the only
glue.

RUN/WALK - CLOSED ANSWER: the world's current mode times the travel.
The native 0x7738 has no mode field (sub_877cc0 serializes only
region+xyz after the hasDestination byte). The browser DID send
characterMovementMode on the JSON path, but its value is a closed loop
with the server: seeded from the bootstrap spawn block, sent back
verbatim, re-applied from the server's echo - the browser has NO
client-side walk toggle (applyLocalMissionMovementMode's only writers
are the bootstrap seed and the move-response echo, CPSMission.tsx), so
the request field always equals the server's stored mode and world-mode
timing is behaviorally identical for every producible flow. When a real
toggle lands, the native wire is a C->S toggle exchange answered by an
0x3122 move-channel push for the local gid (S->C half RE-pinned:
sub_777b60 -> sub_573b70 HUD icon refresh; the C->S opcode is not yet
pinned in the reconstruct evidence) - NOT a new field on 0x7738.
==================
*/
func (rt *Runtime) HandleMove(divisionID string, character *enterworld.Character, payload []byte) MoveOutcome {
	if character != nil && rt.AdvanceResidentRegion != nil {
		rt.AdvanceResidentRegion(divisionID, character.Name, rt.Now().UnixMilli())
	}
	return rt.handleMove(divisionID, character, payload, 0)
}

/*
==================
HandleCOSMove

The 0x769E tag-1 mount move shares validation, collision, persistence and
delivery with ordinary moves. The claimed owner is rechecked while holding
the movement operation lock.
==================
*/
func (rt *Runtime) HandleCOSMove(divisionID string, character *enterworld.Character, gid uint32, payload []byte) []wire.Frame {
	if gid == 0 {
		return nil
	}
	return rt.handleMove(divisionID, character, payload, gid).Frames
}

/*
==================
handleMove

The shared body of HandleMove and HandleCOSMove: admission, the command
latches, decode, then one of the three 0x7738 arms - a direction walk
(direction.go), a ground destination (deep-water gate, geometry, commit) or
a turn in place. Any accepted command other than a direction walk ends the
mover's direction walk.
==================
*/
func (rt *Runtime) handleMove(divisionID string, character *enterworld.Character, payload []byte, cosGID uint32) (outcome MoveOutcome) {
	if character == nil {
		return refusedMove(&simulation.MoveError{NativeErrorCode: simulation.NativeErrorUnknownCharacter, Reason: "characterNotFound"})
	}

	unlock := rt.lockCharacter(divisionID, character.Name)
	defer unlock()
	defer func() {
		outcome.ServerTimeMs = rt.Now().UnixMilli()
		rt.deps.Read(divisionID, func() {
			outcome.Authority = rt.Worlds.Snapshot(simulation.WorldKey(divisionID, character.Name), func() simulation.WorldState { return simulation.SeedWorldState(character) })
		})
	}()

	admission, refusal := rt.admitMove(divisionID, character, cosGID)
	if refusal != nil {
		return refusedMove(refusal)
	}
	worldKey := admission.worldKey

	// A new ground move supersedes any pickup approach in flight (native:
	// the target-move latch clears on the next click command, sub_67b0e0).
	// Reference order preserved: the latch clears BEFORE the movement body
	// is even coerced, so a malformed or refused move still released it.
	if rt.ClearPendingPickup != nil {
		rt.ClearPendingPickup(divisionID, character.Name)
	}
	if rt.ClearCombatIntent != nil {
		rt.ClearCombatIntent(divisionID, character.Name)
	}

	request, decodeErr := simulation.DecodeClientMovementRequest(payload)
	if decodeErr != nil {
		return refusedMove(decodeErr)
	}
	if request.IsDirectionWalk() {
		return rt.startDirectionWalk(divisionID, character, cosGID, admission, request)
	}

	// The deep-water gate (retail agent-server authority): refuse mode-1
	// destinations submerged past the wade depth. A nil validator accepts,
	// which is also the reference behavior when the surface is unreadable.
	if rt.Validator != nil {
		if refusal := rt.Validator.ValidateMovement(request); refusal != nil {
			return refusedMove(refusal)
		}
	}

	nowMs := rt.Now().UnixMilli()

	// The path-walkability guard classifies the chord the accepted move
	// would interpolate: liveBefore -> normalized goal, the exact segment
	// ApplyMove builds below. Action can end a life during geometry work;
	// the commit fence below rejects that old request, even after revival.
	// Turns in place carry no destination and never reach the guard.
	// Production always enforces; observe exists only for explicit
	// diagnostics.
	// walk is the surface ownership of the committed chord, walked from the
	// live position's retained owner (native source pNavCell). It is
	// committed with the move below so the next move starts from it.
	var walk simulation.NavWalk
	var walkFrom simulation.Spawn
	walked := false
	if request.Mode == simulation.MovementAckDestinationMode {
		// The seed closure's first touch reads character.World, which the
		// item lane's door writes concurrently (its writeBackWorld), so
		// the snapshot read takes the store's read door. Lock order holds:
		// character operation -> store -> WorldStore, the same nesting the
		// mutation door below commits with.
		var state simulation.WorldState
		rt.deps.Read(divisionID, func() {
			state = rt.Worlds.Snapshot(worldKey,
				func() simulation.WorldState { return simulation.SeedWorldState(character) },
			)
		})
		live := state.LiveSpawnAt(nowMs)
		goal := simulation.SpawnFromMovement(request, live)
		if rt.CanEnterRegion != nil && !rt.CanEnterRegion(character, goal.RegionID) {
			return refusedMove(&simulation.MoveError{
				NativeErrorCode: simulation.NativeErrorInvalidRequest,
				Reason:          "areaAccessDenied",
			})
		}
		committed, committedWalk, refusal := rt.ConstrainMovementFrom(character.Name, live, state.LiveOwnerAt(nowMs), goal)
		if refusal != nil {
			return refusedMove(refusal)
		}
		walk, walkFrom, walked = committedWalk, live, true
		// Geometry may clip the requested goal into a different canonical
		// region. Authorize the actual committed endpoint as well as the
		// untrusted requested endpoint so policy cannot be bypassed through a
		// cross-region blocker intersection.
		if rt.CanEnterRegion != nil && !rt.CanEnterRegion(character, committed.RegionID) {
			return refusedMove(&simulation.MoveError{
				NativeErrorCode: simulation.NativeErrorInvalidRequest,
				Reason:          "areaAccessDenied",
			})
		}
		request.RegionID = committed.RegionID
		request.X, request.Y, request.Z = committed.X, committed.Y, committed.Z
	}

	var result simulation.MoveResult
	committedWorld, refusal := rt.commitMove(character, admission, func(world *simulation.WorldState) {
		result = simulation.ApplyMove(world, enterworld.ObjectIDForCharacter(character), request, 0, nowMs)
		// The walk was resolved from the admission-time live point; the
		// commit re-samples the same clock, so it departs from the same
		// point. Anything else leaves ownership to the teleport rule rather
		// than attaching spans to a different chord.
		if walked && result.LiveBefore == walkFrom {
			world.CommitWalk(walk.Spans, walk.Rest)
		}
	})
	if refusal != nil {
		return refusedMove(refusal)
	}
	rt.directions.clear(worldKey)

	return MoveOutcome{
		Frames: []wire.Frame{
			{Opcode: simulation.OpMovementAck, Payload: result.AckPayload, Current: func() bool { return rt.Worlds.MovementCurrent(worldKey, committedWorld) }},
		},
		Result: &result,
	}
}

/*
==================
moveAdmission

What admitMove captured in one authority read: the character snapshot and
the world whose LifeRevision fences the commit.
==================
*/
type moveAdmission struct {
	worldKey string
	snapshot *enterworld.Character
	world    simulation.WorldState
}

/*
==================
admitMove

The admission gates every movement command meets before it may touch the
world plane: the claimed mount, a pending delete, a teleport cast, death
and the abnormal-state gate. Admission and its lifecycle fence are captured
in the same read, because the movement operation lock does not serialize
the action/death lane. The caller holds the character operation lock.
==================
*/
func (rt *Runtime) admitMove(divisionID string, character *enterworld.Character, cosGID uint32) (moveAdmission, *simulation.MoveError) {
	admission := moveAdmission{worldKey: simulation.WorldKey(divisionID, character.Name)}
	rt.deps.Read(divisionID, func() {
		admission.snapshot = character.Snapshot()
		admission.world = rt.Worlds.Snapshot(admission.worldKey, func() simulation.WorldState { return simulation.SeedWorldState(character) })
	})
	snapshot := admission.snapshot
	riding := snapshot != nil && snapshot.ActiveCOS != nil && snapshot.ActiveCOS.GID == cosGID &&
		snapshot.ActiveCOS.Mounted && snapshot.ActiveCOS.Summoned && snapshot.ActiveCOS.CurrentHP != 0
	if cosGID != 0 && !riding {
		return admission, &simulation.MoveError{NativeErrorCode: simulation.NativeErrorInvalidRequest, Reason: "invalidCosOwner"}
	}
	if snapshot == nil || snapshot.DeletePending {
		return admission, &simulation.MoveError{NativeErrorCode: simulation.NativeErrorInvalidRequest, Reason: "deletePending"}
	}
	if snapshot.NativeTeleportMode == 1 {
		return admission, &simulation.MoveError{NativeErrorCode: simulation.NativeErrorInvalidRequest, Reason: "teleportCasting"}
	}
	if !enterworld.CharacterAlive(snapshot) {
		// Native never serializes 0x7738 while motion state 1 (death) is set.
		// Mirror the same rule at authority so an older/stale client cannot
		// move a persisted corpse before its LIFE-dead replay arrives.
		return admission, &simulation.MoveError{NativeErrorCode: simulation.NativeErrorInvalidRequest, Reason: "characterDead"}
	}
	if rt.MovementBlocked != nil && rt.MovementBlocked(divisionID, character.Name) {
		return admission, &simulation.MoveError{NativeErrorCode: simulation.NativeErrorInvalidRequest, Reason: "abnormalState"}
	}
	if rt.AttackLocked != nil && rt.AttackLocked(divisionID, character.Name) {
		return admission, &simulation.MoveError{NativeErrorCode: simulation.NativeErrorInvalidRequest, Reason: "attackLocked"}
	}
	return admission, nil
}

/*
==================
commitMove

Runs apply on the world plane and persists the goal as one unit (ADR-1 S1:
every ACCEPTED command persists the goal plane; WorldStore.mu nests under
store.mu per the lock table). The commit re-checks what admission saw: a
delete, a teleport cast, death, or a death/rebirth since admission refuses
the old command, even after revival.
==================
*/
func (rt *Runtime) commitMove(character *enterworld.Character, admission moveAdmission, apply func(*simulation.WorldState)) (simulation.WorldState, *simulation.MoveError) {
	var committedWorld simulation.WorldState
	reason := "deletePending"
	committed := rt.deps.Update(character, "move", func() bool {
		if character.DeletePending {
			return false
		}
		if character.NativeTeleportMode == 1 {
			reason = "teleportCasting"
			return false
		}
		if !enterworld.CharacterAlive(character) {
			reason = "characterDead"
			return false
		}
		current := rt.Worlds.Snapshot(admission.worldKey, func() simulation.WorldState { return simulation.SeedWorldState(character) })
		if current.LifeRevision != admission.world.LifeRevision {
			reason = "characterLifeChanged"
			return false
		}
		committedWorld = rt.Worlds.Update(admission.worldKey,
			func() simulation.WorldState { return simulation.SeedWorldState(character) },
			apply,
		)
		writeBackWorld(character, committedWorld)
		return true
	})
	if !committed {
		return committedWorld, &simulation.MoveError{NativeErrorCode: simulation.NativeErrorInvalidRequest, Reason: reason}
	}
	return committedWorld, nil
}

/*
==================
ConstrainMovement

ConstrainMovement is the single server-authoritative geometry seam used by
player clicks and system-generated movement such as pickup approaches.
It first refuses destinations the stock client cannot compose, then clips
otherwise valid chords to the first blocking contact.
==================
*/
func (rt *Runtime) ConstrainMovement(characterName string, from, to simulation.Spawn) (simulation.Spawn, *simulation.MoveError) {
	committed, _, refusal := rt.ConstrainMovementFrom(characterName, from, simulation.NavOwner{}, to)
	return committed, refusal
}

/*
==================
LineOfSight

CRegionManagerBody_CheckLineOfSight (98D880), the region-manager vfunc
+0x78 that Skill_ValidatePrerequisitesAndCost asks for every action target
in phase 0x40. It runs the ground move test (vfunc +0x30, at most six
region legs) from the caster to the target and fails only on a blocking
contact. The clip validator is that move test, walked from the caster's
retained owner. Nobody moves, so no clip statistics are counted.

Only an applied clip is authority: with the clip off or observing, walls
do not stop walks either, so they do not stop skills.
==================
*/
func (rt *Runtime) LineOfSight(from simulation.Spawn, fromOwner simulation.NavOwner, to simulation.Spawn) bool {
	if simulation.IsDungeonRegion(from.RegionID) != simulation.IsDungeonRegion(to.RegionID) {
		return false
	}
	clip := rt.ClientClip
	if clip == nil || clip.Validator == nil || clip.Mode != ClipApply {
		return true
	}
	from, to = simulation.NormalizeSpawnFrame(from), simulation.NormalizeSpawnFrame(to)
	var report ClipReport
	if owned, ok := clip.Validator.(ownerClipValidator); ok {
		report = owned.ClipMovementPathFrom(from, fromOwner, to)
	} else {
		report = clip.Validator.ClipMovementPath(from, to)
	}
	return report.Outcome != ClipBlocked
}

/*
==================
ConstrainMovementFrom

ConstrainMovementFrom is ConstrainMovement for a mover that retains its
surface owner (navowner.go, native source pNavCell). The walk starts from
fromOwner, and the committed goal stands on the surface it reached: its Y
is that surface's height, and the returned walk carries the owner spans the
caller commits with WorldState.CommitWalk. A caller without a retained owner
passes the zero owner and gets the native teleport rule at the start.
==================
*/
func (rt *Runtime) ConstrainMovementFrom(characterName string, from simulation.Spawn, fromOwner simulation.NavOwner, to simulation.Spawn) (simulation.Spawn, simulation.NavWalk, *simulation.MoveError) {
	if simulation.IsDungeonRegion(from.RegionID) != simulation.IsDungeonRegion(to.RegionID) {
		return from, simulation.NavWalk{}, &simulation.MoveError{
			NativeErrorCode: simulation.NativeErrorInvalidRequest,
			Reason:          "ground movement cannot cross the outdoor/dungeon authority boundary",
		}
	}
	if rt.PathGuard != nil {
		if refusal := rt.PathGuard.InspectMoveFrom(characterName, from, fromOwner, to); refusal != nil {
			return from, simulation.NavWalk{}, refusal
		}
	}
	if rt.ClientClip != nil {
		to = rt.ClientClip.ProcessMoveFrom(characterName, from, fromOwner, to)
	}
	to, walk := rt.walkOwners(from, fromOwner, to)
	return to, walk, nil
}

/*
==================
walkOwners

The surface ownership of an already constrained chord: the owner spans
walked from fromOwner, and the goal lifted onto the surface the walk
reached. Without a nav authority ownership stays unresolved.
==================
*/
func (rt *Runtime) walkOwners(from simulation.Spawn, fromOwner simulation.NavOwner, to simulation.Spawn) (simulation.Spawn, simulation.NavWalk) {
	if rt.Nav == nil {
		return to, simulation.NavWalk{}
	}
	walk := rt.Nav.WalkOwners(from, fromOwner, to)
	if owner, y, ok := rt.Nav.ResolveNavOwner(to, walk.Rest); ok {
		to.Y, walk.Rest = y, owner
	}
	return to, walk
}

/*
==================
characterSnapshot

characterSnapshot copies mutable character fields while the authority read
door is held. ID and Name are immutable identity; every other field used by
movement must come from this detached view or an Update closure.
==================
*/
func (rt *Runtime) characterSnapshot(divisionID string, character *enterworld.Character) *enterworld.Character {
	if character == nil {
		return nil
	}
	var snapshot *enterworld.Character
	rt.deps.Read(divisionID, func() {
		snapshot = character.Snapshot()
	})
	return snapshot
}

/*
==================
UsePendingTracker

UsePendingTracker wires the item lane's pickup-approach tracker so an
accepted (or even malformed) ground move releases the approach latch. The
tracker locks itself, so the cross-lane call is race-safe.
==================
*/
func (rt *Runtime) UsePendingTracker(tracker *grounditem.PendingTracker) {
	if tracker == nil {
		rt.ClearPendingPickup = nil
		return
	}
	rt.ClearPendingPickup = func(divisionID, characterName string) {
		tracker.Clear(grounditem.PendingKey(divisionID, characterName))
	}
}

/*
==================
writeBackWorld

writeBackWorld persists the goal plane of the runtime world state onto
the character record (the segment plane is runtime-only, like the
fixture's in-memory moveSegment across restarts). Mirror of the item
lane's write-back so both lanes leave the same persisted shape.

The write-back owns ONLY spawn/movementMode/spawnSet; every other world
field (dungeonMinimap, movementSourceSeeded, updatedAt, moveSegment echo,
and any future record keys) copies through untouched, like the Node move
path's {...world} spread (SCOUT-B FINDING 500). Copy-then-swap so aliases
of the old record (the character snapshot's shallow copy) stay unchanged.
==================
*/
func writeBackWorld(character *enterworld.Character, state simulation.WorldState) {
	regionID := int64(state.Spawn.RegionID)
	x, y, z := state.Spawn.X, state.Spawn.Y, state.Spawn.Z
	angle := int64(state.Spawn.Angle)
	mode := int64(state.MovementMode)
	next := enterworld.CharacterWorld{}
	if character.World != nil {
		next = *character.World
	}
	next.Spawn = &enterworld.WorldSpawn{RegionID: &regionID, X: &x, Y: &y, Z: &z, Angle: &angle}
	next.MovementMode = &mode
	next.SpawnSet = state.SpawnSet
	character.World = &next
}
