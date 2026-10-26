/*
===========================================================================

mover.go - monster movement state schema, segment interpolation, and progress

Owns the shared monster mover state (MoverState), in-flight segment
interpolation (LivePoseAt), and native ten-failure stuck progress tracking.
Wire emission is layered on by the simulation tick leg.

===========================================================================
*/
package monster

import worldgeom "opensro.online/server/internal/game/world"

/*
================
MoverMode

MoverMode is the destination-selection input currently driving an
instance (never a wire value).
================
*/
type MoverMode uint8

const (
	// MoverSpawning: visible at its generated point, holding the native spawn
	// state before ordinary idle decisions or proximity aggro are eligible.
	MoverSpawning MoverMode = iota
	// MoverIdle: waiting for the next decision. A timed-out WANDER can
	// leave its movement channel running; use LivePoseAt for position.
	MoverIdle
	// MoverWandering: a facing-relative wander request with its own deadline.
	MoverWandering
	// MoverChasing: following an acquired target's movement guidance
	// (Q5; same mover, different destination input).
	MoverChasing
	// MoverReturning: leash exceeded - walking home to the nest anchor,
	// target dropped (the give-up guard, coordinator seq247 fixture item).
	MoverReturning
	// MoverAttacking: settled in action range with a live target. No movement
	// segment is active; NextAttackMs drives the server-owned repeat cadence.
	MoverAttacking
	// MoverRecovering: a terminal attack was accepted and its authored action
	// duration still owns presentation. The defeated target is released, but
	// return/wander movement cannot begin before BehaviorDeadlineMs.
	MoverRecovering
	// MoverFollowing owns a leader-selected destination, without an attack
	// target. Arrival retains FOLLOW; its callback or an explicit event exits.
	MoverFollowing
	// A native BATTLE -> BATTLE re-entry runs OnExit AFTER SetTarget and
	// therefore clears that target. The empty BATTLE survives until its tick.
	MoverBattleReentry
	// Native PENDING (14) preserves the movement channel, but suspends AI
	// callbacks until the independent activity cadence observes a player.
	MoverPending
	moverModeCount
)

/*
================
Pose

Pose is a region-local position + heading.
================
*/
type Pose struct {
	RegionID uint16
	X, Y, Z  float64
	Heading  uint16
}

/*
================
ChaseGuidance

ChaseGuidance is the target-owned movement state last observed by the chase
planner. Its destination identifies the accepted target command; targetMoving
records whether that command is still in flight. Neither value is approach
geometry: combat and stand-off calculations always use the target's live pose.

Keeping this as a distinct value prevents three equal-shaped coordinate
domains from aliasing: target intent, sampled target position, and the
monster's derived movement goal.
================
*/
type ChaseGuidance struct {
	destination  Pose
	targetMoving bool
}

/*
================
NewChaseGuidance

NewChaseGuidance adopts one immutable target movement snapshot.
================
*/
func NewChaseGuidance(destination Pose, targetMoving bool) ChaseGuidance {
	return ChaseGuidance{destination: destination, targetMoving: targetMoving}
}

/*
================
ChaseGuidance.Destination

Destination returns the immutable destination owned by this command.
================
*/
func (guidance ChaseGuidance) Destination() Pose {
	return guidance.destination
}

/*
================
ChaseGuidance.TargetMoving

TargetMoving reports whether the target command was in flight when captured.
================
*/
func (guidance ChaseGuidance) TargetMoving() bool {
	return guidance.targetMoving
}

/*
================
MoverState

MoverState is the value schema and transition policy for one monster's
movement state. simulation.MonsterState owns and commits live values.
================
*/
type MoverState struct {
	idleEntryPending bool
	control          actorControl
	Activity         ActivityCadence
	intent           navigationIntent
	navigation       *NavigationPath
	helpLatched      bool
	mode             MoverMode
	// Pose is the settled position while idle; while a segment is in
	// flight the live position interpolates From->To by wall clock.
	Pose Pose
	// From/To/DepartMs/ArriveMs describe the in-flight segment (walk
	// speed distance/time computed by the committer). Valid only when
	// ArriveMs > DepartMs.
	From, To Pose
	DepartMs int64
	ArriveMs int64
	// BehaviorDeadlineMs is the deadline owned by the CURRENT behavior state:
	// spawn hold while spawning, idle decision while idle, and wander-state
	// timeout while wandering. Combat/return transitions clear or replace it.
	BehaviorDeadlineMs int64
	// targetGID is the chased/attacked player gid. It is private so behavior
	// changes must name a MoverEvent and pass the transition invariants.
	targetGID uint32
	// followLeaderGID binds a FOLLOW leg to the relationship that admitted it.
	// It is never a combat target and is cleared by every exit from FOLLOW.
	followLeaderGID uint32
	// RetaliationPending marks a target assigned by an authoritative player
	// hit rather than passive sight acquisition. A wandering monster keeps
	// its live segment until the simulation tick can publish the replacement
	// chase goal; this bit makes that re-aim immediate instead of waiting for
	// the ordinary chase packet throttle.
	retaliationPending bool
	// RetaliationRevision changes whenever damage assigns a new retaliation
	// target. Whole-value mover commits carry the revision they were planned
	// from, allowing CommitMover to reject only genuinely stale tick writes
	// without blocking legitimate chase -> return/idle transitions.
	retaliationRevision uint64
	// Retaliating distinguishes damage-owned pursuit from an aggressive
	// monster's ordinary sight target. It stays set through chase/attack and
	// clears only when the target is released.
	retaliating bool
	// chaseGuidance is the target movement state captured with the current leg.
	// It is intentionally separate from both the target's sampled live pose and
	// To: live position owns geometry; command identity and in-flight state own
	// immediate invalidation and eligibility for the bounded refresh deadline.
	chaseGuidance    ChaseGuidance
	hasChaseGuidance bool
	// The two event slots plus transitionSerial retain the causal edges that
	// produced this tuple, making an invariant failure actionable in tick
	// diagnostics without an unbounded per-monster history.
	previousEvent    MoverEvent
	lastEvent        MoverEvent
	transitionSerial uint64
	// AttackSkillID/Reach/Cooldown are one selected RefObjChar default action.
	// They stay stable while approaching; after a strike the next choice is
	// resolved from the same shipped row instead of a hardcoded mob attack.
	// Summons own an action without a target-distance requirement. Preserve
	// that distinction through mover validation; zero reach is not a melee plan.
	AttackSummon     bool
	AttackSelfEffect bool
	AttackSkillID    uint32
	AttackReach      float64
	AttackCooldownMs int64
	// AI strategy +48: cooldown plus selection jitter, retained between
	// selections. Opponent expiry uses twice this interval (the
	// native opponent-policy choice).
	AttackIntervalMs uint32
	NextAttackMs     int64
	// Channel is the run/walk speed channel the CLIENT currently holds
	// for this monster (the 0x3122 MOVE state; 2=walk, 3=run). Spawn rows
	// ship the walk channel; the tick leg pushes 0x3122 when a leg needs
	// the other channel - keeping the client's integrator speed equal to
	// the server's segment timing (the BUG-7 speed-half fix).
	Channel uint8
	// The native pursuit-distance classification and the COS command
	// state retain a combat channel decision across approach replans.
	PursuitChannel       uint8
	LastBattleActivityMs uint32
	HomingStartedMs      uint32
	HomingAcquireAfterMs uint32
	// progress is the native stuck count (the tactics record's
	// +184/+19C/+148): each stop that leaves the actor within 20
	// of where its move sequence began counts, and from the tenth
	// the stuck event is raised (the move-stopped counter and its
	// dispatch).
	progress MovementProgress
	// stuck is latched when NoteMoveStopped crosses nativeStuckFailureLimit so
	// the simulation tick can answer event 0x28 on the current state.
	stuck bool
}

/*
================
MoverState.InFlight

InFlight reports whether a segment is still maturing at nowMs.
================
*/
func (m MoverState) InFlight(nowMs int64) bool {
	return m.ArriveMs > m.DepartMs && nowMs < m.ArriveMs
}

/*
================
MoverState.Progress

Progress returns the native movement progress tracker (the
tactics record's +184/+19C/+148).
================
*/
func (m MoverState) Progress() MovementProgress {
	return m.progress
}

/*
================
MoverState.Stuck

Stuck reports whether the last stop reached the ten-failure stuck threshold
(the native move-stopped stuck counter -> event 0x28).
================
*/
func (m MoverState) Stuck() bool {
	return m.stuck
}

/*
================
MoverState.ClearStuck

ClearStuck clears the pending event-0x28 latch after the simulation tick
answers it, leaving the underlying failure counter untouched
(matching the BATTLE stuck handler).
================
*/
func (m *MoverState) ClearStuck() {
	m.stuck = false
}

/*
================
MoverState.ResetStuck

ResetStuck clears both the failure counter (the tactics record's
+0x148 = 0) and the pending event-0x28 latch (the native state
change and command release).
================
*/
func (m *MoverState) ResetStuck() {
	m.progress.ResetFailures()
	m.stuck = false
}

/*
================
MoverState.NoteMoveIssued

NoteMoveIssued records a step command before the mover executes or clips it
(the native tactics step).
================
*/
func (m *MoverState) NoteMoveIssued(live Pose, alreadyMoving bool) {
	m.progress.Issued(live, alreadyMoving)
}

/*
================
MoverState.NoteMoveStopped

NoteMoveStopped records a movement stop at live (the native move
stop -> event 8 -> the move-stopped stuck counter) and
latches Stuck when the tenth short stop is reached.
================
*/
func (m *MoverState) NoteMoveStopped(live Pose) bool {
	if m.progress.Arrived(live) {
		m.stuck = true
		return true
	}
	return false
}

/*
================
GroundResolver

GroundResolver resolves the walkable surface height at one live pose
on the mover's plane (Plane.HeightAt through MonsterMoverOps.GroundHeight
- monster must not import mission, so the height source arrives as a
parameter; board seq743/seq779 shape). The pose carries the chord
height at its XZ: the caller height the plane's nearest-surface rule
reads. It is consulted only off an adopted path - an adopted path owns
its segment's heights outright - so no surface owner is carried: the
chord height alone selects the surface (#1695).
================
*/
type GroundResolver func(pose Pose) (float64, bool)

/*
================
MoverState.LivePoseAt

LivePoseAt interpolates the in-flight segment at nowMs (the live plane;
callers must never read Pose while a segment is maturing - the bug D
discipline the player plane already enforces).

Y CONTRACT (board seq735/seq779, BUG-8 second injection path): the
segment ENDPOINTS carry surface-resolved heights, but a straight Y-lerp
between them describes a CHORD - below ground over a rise, above it over
a dip. Mid-flight the interpolated XZ is resolved through `ground` so a
scope-enter seed or a segment departure never inherits a chord height.
The settled (To) and idle (Pose) branches are returned UNTOUCHED: To.Y
is already the surface height at its own quantised XZ (the BUG-12
commitSegment block) and an idle Pose is either that settled value or
the bootstrap-lifted nest anchor - re-resolving them would change
settle-adjacent wire values for no correctness gain. A nil resolver (or
a miss) keeps the lerped Y, exactly the pre-injection behaviour.
================
*/
func (m MoverState) LivePoseAt(nowMs int64, ground GroundResolver) Pose {
	if !m.InFlight(nowMs) {
		if m.ArriveMs > m.DepartMs {
			return m.To
		}
		return m.Pose
	}
	t := float64(nowMs-m.DepartMs) / float64(m.ArriveMs-m.DepartMs)
	if t < 0 {
		t = 0
	} else if t > 1 {
		t = 1
	}
	planar := worldgeom.Interpolate(
		worldgeom.RegionXZ{RegionID: m.From.RegionID, X: m.From.X, Z: m.From.Z},
		worldgeom.RegionXZ{RegionID: m.To.RegionID, X: m.To.X, Z: m.To.Z},
		t,
	)
	pose := Pose{
		RegionID: planar.RegionID,
		X:        planar.X,
		Y:        m.From.Y + (m.To.Y-m.From.Y)*t,
		Z:        planar.Z,
		Heading:  m.To.Heading,
	}
	if m.navigationMatches() {
		if y, ok := m.navigation.height(t, pose); ok {
			pose.Y = y
		}
		return pose
	}
	if ground != nil {
		if y, ok := ground(pose); ok {
			pose.Y = y
		}
	}
	return pose
}

/*
================
NewSpawnMover

NewSpawnMover constructs the policy-valid initial movement value for a live
instance. simulation owns the returned value from activation onward.
================
*/
func NewSpawnMover(instance Instance, spawnedAtMs int64) MoverState {
	return MoverState{
		mode:               MoverSpawning,
		BehaviorDeadlineMs: spawnedAtMs + RetailSpawnHoldMs,
		Pose: Pose{
			RegionID: instance.Spawn.RegionID,
			X:        instance.Spawn.X,
			Y:        instance.Spawn.Y,
			Z:        instance.Spawn.Z,
			Heading:  instance.SpawnHeading,
		},
	}
}
