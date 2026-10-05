/*
===========================================================================

monstertick.go - the monster movement tick leg (wander and aggro)

===========================================================================
*/

package simulation

import (
	"math"
	"opensro.online/server/internal/domain"
	"time"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/game/item/wire"
	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/monster"
)

// Monster movement tick leg (Q4 wander + Q5 aggro, monster-live wave).
//
// ONE MOVER for both behaviours (coordinator seq247 risk #3): every
// emitted segment funnels through commitSegment/emitSegmentFrames - the
// wander planner and the aggro planner only choose the DESTINATION (and
// the run/walk channel). A chase-specific packet builder is a design
// violation (G-WHATIF A14, G-SRV seq263 #3); do not add one.
//
// Wire contract (all v1.150-pinned):
//   - goal legs are 0xB738 via BuildMovementAckPayload - the SAME
//     encoder the player ack path uses, byte-verified against RZ's
//     binary widths (seq272 P2; board seq291 verification note);
//   - in-flight glide re-syncs are 0x30E3 ObjectSourceMove (lenient
//     re-seed, RZ seq159/seq272);
//   - arrival settles once with 0xB2F5 (stop + face, REV seq116) -
//     mirroring the player mover legs in tick.go.
//
// Per-nest behavior values resolve from each instance's evidence-backed
// Nest/Tactics row. Unmatched v1.150 anchors remain passive, while mobile
// RefObj rows retain the class-wide idle-wander primitive.

/*
==================
MonsterMoverOps

MonsterMoverOps is the tick leg's dependency set, wired in server.go
whenever the monster population is enabled - which is the DEFAULT
(MISSION_SPAWN_MONSTERS=0 is the kill switch; nil ops = leg skipped).
==================
*/
type MonsterMoverOps struct {
	divisionID     string // set by the owning ticker shard; empty only in direct fixtures
	MessageBlockAt func(Spawn) (worldgeom.MessageBlock, bool)
	activity       *monsterActivitySnapshot
	Monsters       *MonsterState
	TacticsFor     monster.TacticsResolver
	// TerrainHeight resolves the navmesh ground height for a candidate
	// destination (movement TerrainHeightAt; nil = keep the anchor Y).
	// Must be non-blocking: it reads preloaded region bundles.
	TerrainHeight func(regionID uint16, x, z float64) (float64, bool)
	// Production supplies the same navigation owner for ALL monster legs.
	// nil is reserved for geometry-free unit fixtures; an unavailable region
	// is a nil RESULT and never authorizes a straight-line fallback.
	PlanPath func(from, goal monster.Pose) *monster.NavigationPath
	// PlanPathFrom is PlanPath from the monster's retained surface owner
	// (native source pNavCell). Production wires it; when set, every leg that
	// departs from the monster's own live pose uses it (planPath below).
	PlanPathFrom func(from monster.Pose, fromOwner NavOwner, goal monster.Pose) *monster.NavigationPath
	// AI route policy, separate from raw clipping used by sight/help probes.
	PlanRoute func(from, goal monster.Pose) *monster.NavigationRoute
	// Rand returns a uniform [0,1) sample (seam for deterministic tests).
	Rand func() float64
	// AttackPlan resolves one of RefObjChar's ten default-skill ids against
	// the shipped v1.150 skill table. requestedSkillID preserves a choice
	// while the mover approaches; zero chooses from the valid authored set.
	// Retained-ID resolution receives a zero pick and must not reselect based
	// on changed health/damage. New selections alone consume choice entropy,
	// and they carry the target the weighted choice (561B00) measures.
	AttackPlan func(instance monster.Instance, requestedSkillID uint32, pick AttackPick) (MonsterAttackPlan, bool)
	// BasicAttack commits one monster->player hit and returns the same B245
	// action bracket the browser already uses for player attacks.
	BasicAttack func(divisionID string, instance monster.Instance, targetGid, skillID uint32, nowMs int64) MonsterAttackResult
	// RunAction keeps one actor's decision, damage and non-blocking publication
	// in the action owner's division transaction. The supplied attack capability
	// already owns that transaction and must not acquire it again. Production
	// installs this; BasicAttack alone is the detached simulation test seam.
	RunAction func(divisionID string, run func(MonsterAttackOperation))
	// FirstAttackGuard reads a player's live first-attack protection (the
	// Bard's Noise) from the effect owner. nil protects nobody.
	FirstAttackGuard func(divisionID string, playerGID uint32, nowMs int64) monster.FirstAttackGuard
	// Companions lists a player's summoned, living, unmounted companions in
	// its owner's container order (CCOSManager_AppendOwnedActorsInContainerOrder).
	// nil means no companion is ever a target.
	Companions func(divisionID string, ownerGID uint32, nowMs int64) []CompanionTarget

	// shownMonsters tracks which monster gids each viewer session has
	// been sent a spawn for (the peervis shownPeers pattern). On first sight,
	// the bootstrap object list already owns the create set and live pose;
	// this plane publishes only any in-flight mover continuation. Only the
	// owning division shard touches it.
	shownMonsters map[string]map[uint32]bool
	// breachLogged keeps the A19-b budget-breach log to once per
	// division (a per-tick log line would be its own flood).
	breachLogged map[string]bool
}

/*
==================
mustMoverTransition

mustMoverTransition turns a behavior bug into one causal failure at the
mutation site. The simulation tick owns these events; an illegal edge is a
programmer error, not a packet condition that should be silently ignored.
==================
*/
func mustMoverTransition(mover *monster.MoverState, event monster.MoverEvent, targetGID uint32) {
	if err := mover.Transition(event, targetGID); err != nil {
		panic(err)
	}
}

/*
================
logScopeBudgetBreach
================
*/
func (ops *MonsterMoverOps) logScopeBudgetBreach(divisionID string, count int) {
	if ops.breachLogged == nil {
		ops.breachLogged = make(map[string]bool)
	}
	if ops.breachLogged[divisionID] {
		return
	}
	ops.breachLogged[divisionID] = true
	log.Warnf("simulation: monster scope ring holds %d instances in division %s, beyond the %d-instance operational soft limit", count, divisionID, monsterScopeSoftLimit)
}

const (
	// monsterMovementSourceTurnThreshold is the retail server's optional
	// source-block gate for an already-moving entity. The v1.188
	// EntityMovement_UpdateAndBroadcastB021 body normalizes the old and new
	// directions, converts their dot product to an angular delta, and writes
	// the source only when the delta is GREATER than qword[0x00b460e0]:
	// 0.785398185253143 radians (Rizin 0x004b1109..0x004b118f).
	//
	// This is version-adjacent server evidence applied to the byte-identical
	// v1.150 client source block. It replaces the incorrect player-plane
	// "one source ever" policy that made sharp monster replans diverge.
	monsterMovementSourceTurnThreshold = math.Pi / 4
)

// monsterScopeSoftLimit is operational detection, not a gameplay gate.
// The deterministic per-frame audit over the evidence-backed population is
// pinned below this value; exceeding it signals data or scope drift while
// still serving the complete retail-derived population.
const monsterScopeSoftLimit = 700

// Object-list bracket opcodes (the bootstrap package owns the login-path
// constants of the same values; mission cannot import bootstrap).
const (
	opObjectListStart    uint16 = 0x30CB
	opObjectListFinalize uint16 = 0x330A
)

/*
==================
RegionScopeRing

RegionScopeRing is the 3x3 sector ring around the viewer's region. It is
the monster materialization and AI-activity candidate set (POLICY: native
AI activity scoping is unpinned), not the visibility radius: what a viewer
holds is the native 320-unit block neighbourhood (worldgeom.InterestVisible),
which always lies inside this ring. DUNGEON regions (sector-bit ids) scope
to exactly themselves: ring math over the dungeon bit is meaningless.
==================
*/
func RegionScopeRing(regionID uint16) []uint16 {
	if IsDungeonRegion(regionID) {
		return []uint16{regionID}
	}
	sx, sy := SectorX(regionID), SectorY(regionID)
	ring := make([]uint16, 0, 9)
	for dy := -1; dy <= 1; dy++ {
		for dx := -1; dx <= 1; dx++ {
			ring = append(ring, RegionIDForSectors(sx+dx, sy+dy))
		}
	}
	return ring
}

/*
==================
MonsterDespawnBracketFrames

MonsterDespawnBracketFrames encodes a byListSub=2 object-list group:
0x30CB {0x02, u16 count} begin, one 0x3417 chunk of 4-byte gids (the
sub_777310 despawn bodies the finalize loop consumes), empty 0x330A
finalize. The native bulk-eviction shape (byListSub=2 pinned by
RZ seq134/DUMP seq91; client-side handling sabotage-proven by WIP's
ensureDespawn4cBinding chain, board seq359/373).
==================
*/
func MonsterDespawnBracketFrames(gids []uint32) []Frame {
	w := wire.NewWriter(len(gids) * 4)
	changes := make([]domain.ObjectScopeChange, 0, len(gids))
	for _, gid := range gids {
		w.U32(gid)
		changes = append(changes, domain.ObjectScopeChange{GID: gid})
	}
	return []Frame{
		{Opcode: opObjectListStart, Payload: []byte{0x02, byte(len(gids) & 0xff), byte(len(gids) >> 8)}},
		{Opcode: wire.OpObjectListChunk, Payload: w.Payload()},
		{Opcode: opObjectListFinalize, Payload: []byte{}, Scope: changes},
	}
}

/*
==================
playerMovementIntent

playerMovementIntent is one immutable target-command snapshot. It is kept
separate from playerPose.Pose because the command destination is an
invalidation input, while the live pose owns combat and approach geometry.
==================
*/
type playerMovementIntent struct {
	destination Spawn
	inFlight    bool
	present     bool
}

/*
================
capturePlayerMovementIntent
================
*/
func capturePlayerMovementIntent(world WorldState, nowMs int64) playerMovementIntent {
	return playerMovementIntent{
		destination: world.Spawn,
		inFlight:    world.MoveSegment.Valid() && nowMs < world.MoveSegment.ArrivesAtMs,
		present:     true,
	}
}

// playerPose is one live player position inside a division at tick time.
/*
================
playerPose
================
*/
type playerPose struct {
	NativeBodyStatus uint8
	Gid              uint32
	Pose             Spawn
	MovementIntent   playerMovementIntent
	BodyRadius       BodyRadius
	// Guard is the player's first-attack protection (the Bard's Noise),
	// read from the action owner when the leg samples its players.
	Guard monster.FirstAttackGuard
	// OwnerGid names the player a companion entry belongs to; zero for a
	// player. Companions are targets, never acquisition candidates.
	OwnerGid uint32
	// Band is a companion's COS band (TypeID 4).
	Band uint8
}

/*
================
CompanionTarget

One summoned, living companion a monster may strike: the action owner's
projection of its world pose, body and status.
================
*/
type CompanionTarget struct {
	Gid              uint32
	Pose             Spawn
	BodyRadius       BodyRadius
	NativeBodyStatus uint8
	Band             uint8
}

/*
==================
chaseGuidance

chaseGuidance returns the movement state the pursuit owner last observed.
Direct test fixtures and stationary actors may omit MovementIntent; their
live pose then acts as a settled command destination.
==================
*/
func (player playerPose) chaseGuidance() monster.ChaseGuidance {
	intent := player.MovementIntent
	destination := player.Pose
	if intent.present {
		destination = intent.destination
	}
	return monster.NewChaseGuidance(monster.Pose{
		RegionID: destination.RegionID,
		X:        destination.X,
		Y:        destination.Y,
		Z:        destination.Z,
		Heading:  destination.Angle,
	}, intent.present && intent.inFlight)
}

// RunMonsterLeg advances actors independently of observer interest.
/*
================
RunMonsterLeg
================
*/
func (ops *MonsterMoverOps) RunMonsterLeg(nowMs int64, sessions []SessionSnapshot, push Pusher) {
	if ops == nil || ops.Monsters == nil {
		return
	}

	viewers := make(map[string]worldgeom.RegionXZ, len(sessions))
	live := make(map[string]bool, len(sessions))
	for _, session := range sessions {
		live[session.SessionID] = true
		pose := session.World.LiveSpawnAt(nowMs)
		viewers[session.SessionID] = worldgeom.RegionXZ{RegionID: pose.RegionID, X: pose.X, Z: pose.Z}

	}

	// Scope visibility FIRST, mover frames SECOND: a viewer must have
	// received a monster's spawn before any 0xB738/0xB2F5 for its gid
	// (the movement handlers ResolveGidObjectOrAssert - an unknown gid
	// ASSERTS in the native client, REV seq116 / WIP seq140 #3; only
	// 0x30E3 is find-or-skip safe). Delivering mover frames per-session
	// against the just-updated shown sets makes spawn-before-goal and
	// never-goal-after-despawn true by construction.
	ops.Monsters.prepareDormancy(nowMs, ops.divisionID, sessions)
	ops.runScopeVisibility(nowMs, sessions, viewers, live, push)

	ops.activity = ops.captureActivity(sessions, nowMs)
	defer func() { ops.activity = nil }()
	divisionSet := make(map[string]bool)
	// One dispatch for the whole leg. A variable captured by the closure
	// handed to RunAction escapes through that indirect call: captured per
	// actor, every monster's Instance snapshot and the closure itself were a
	// heap allocation per behaviour tick (1 GB a minute with one player
	// online, which kept the collector busy on three cores). RunAction runs
	// its closure before returning, so the slots are reused safely.
	var dispatch struct {
		division string
		instance monster.Instance
		players  []playerPose
	}
	act := func(attack MonsterAttackOperation) {
		// Bind the capability to a value copy, not the shard's dependency
		// set. Mutable actor state remains in MonsterState.
		owned := *ops
		owned.BasicAttack = attack
		owned.advanceAndPublish(dispatch.division, dispatch.instance, dispatch.players, nowMs, sessions, push)
	}
	for _, batch := range ops.Monsters.behaviorBatchesForDivision(nowMs, ops.divisionID) {
		divisionID := batch.key.division
		divisionSet[divisionID] = true
		ops.activity.world = activityWorld{divisionID, uint32(batch.key.lease.ID), batch.key.lease.Generation}
		var players []playerPose
		for _, session := range sessions {
			if session.DivisionID != divisionID || session.Population != batch.key.lease || !session.CombatEligible {
				continue
			}
			player := playerPose{Gid: PlayerObjectID(session.CharacterID), Pose: session.World.LiveSpawnAt(nowMs), MovementIntent: capturePlayerMovementIntent(session.World, nowMs), BodyRadius: session.BodyRadius, NativeBodyStatus: session.NativeBodyStatus}
			if ops.FirstAttackGuard != nil {
				player.Guard = ops.FirstAttackGuard(divisionID, player.Gid, nowMs)
			}
			players = append(players, player)
			players = appendCompanionTargets(players, ops.companionTargets(divisionID, player, nowMs))
		}
		for _, gid := range batch.actors {
			// The scheduler carries identities, not a second copy of the world.
			// Read a value snapshot at dispatch so despawn/retaliation between
			// scheduling and dispatch cannot revive an obsolete actor snapshot.
			instance, exists := ops.Monsters.behaviorActor(batch.key, gid)
			if !exists {
				continue
			}
			if ops.RunAction != nil {
				dispatch.division, dispatch.instance, dispatch.players = divisionID, instance, players
				ops.RunAction(divisionID, act)
				continue
			}
			ops.advanceAndPublish(divisionID, instance, players, nowMs, sessions, push)
		}
	}
	for divisionID := range divisionSet {
		if frames := ops.Monsters.DrainUniqueNotices(divisionID); len(frames) > 0 {
			push.PushToDivision(divisionID, frames, "")
		}
	}
}

/*
================
advanceAndPublish
================
*/
func (ops *MonsterMoverOps) advanceAndPublish(divisionID string, instance monster.Instance, players []playerPose, nowMs int64, sessions []SessionSnapshot, push Pusher) {
	frames, targeted := ops.advanceInstance(divisionID, instance, players, nowMs)
	for _, session := range sessions {
		if len(frames) > 0 && session.DivisionID == divisionID && ops.shownMonsters[session.SessionID][instance.Gid] {
			push.PushToSession(session.SessionID, frames)
		}
	}
	// Private consequences follow the public result in the same operation.
	deliverMonsterTargetFrames(divisionID, targeted, sessions, push)
	ops.vanishInSafeZone(divisionID, instance.Gid, nowMs)
}

/*
================
vanishInSafeZone

CGObjMob_SetRegionLeavingSafeZone (CGObjMob vtable +0x3AC, 4C1270): a
monster whose region changes to one that is not a battlefield (a town,
_RefRegion.IsBattleField 0) is set to life state 3 through vtable +0x1F0
(CGObjChar_SetLifeStateAndNotify 4A9C80). From alive that state skips the
death broadcast: the monster vanishes without a kill or a reward, and its
nest respawns it (560D00). This is why monsters never walk into a town.
================
*/
func (ops *MonsterMoverOps) vanishInSafeZone(divisionID string, gid uint32, nowMs int64) {
	mover, ok := ops.Monsters.Mover(divisionID, gid)
	if ok && SafeZoneRegion(mover.LivePoseAt(nowMs, nil).RegionID) {
		ops.Monsters.Defeat(divisionID, gid, time.UnixMilli(nowMs))
	}
}

/*
================
SafeZoneRegion

A region _RefRegion marks as no battlefield. A region the table does not
know is not one, as 52943E refuses unknown regions separately.
================
*/
func SafeZoneRegion(region uint16) bool {
	allowed, known := worldgeom.RegionPlayerCombat(region)
	return known && !allowed
}

/*
==================
monsterInFlightSnapshotFrames

monsterInFlightSnapshotFrames is the single wire projection of an active
mover segment. Bootstrap reconciliation supplies a live source because its
create row and this first tick are separate snapshots; ordinary scope-enter
creates at the same tick's live pose and therefore needs only the goal.
==================
*/
func monsterInFlightSnapshotFrames(
	instance monster.Instance,
	mover monster.MoverState,
	source *MovementSource,
) []Frame {
	frames := make([]Frame, 0, 2)
	if currentChannel(mover.Channel) != wire.MoveStateWalk {
		refresh := wire.ObjectStateRefresh{
			Gid:       instance.Gid,
			StateType: wire.StateChannelMove,
			Value:     mover.Channel,
		}
		frames = append(frames, Frame{Opcode: wire.OpObjectStateRefresh, Payload: refresh.Encode()})
	}
	destination := mover.MovementGoal()
	goal := BuildMovementAckPayload(instance.Gid, MovementRequest{
		Mode:     MovementAckDestinationMode,
		RegionID: destination.RegionID,
		X:        destination.X,
		Y:        destination.Y,
		Z:        destination.Z,
	}, source)
	return append(frames, Frame{Opcode: OpMovementAck, Payload: goal})
}

// Scope and bootstrap use the same live-state projection.
/*
================
monsterWireDefFromInstance
================
*/
func monsterWireDefFromInstance(instance monster.Instance, nowMs int64) MonsterDef {
	return MonsterWireDefFromInstance(instance, nowMs)
}

/*
================
resolveTactics
================
*/
func (ops *MonsterMoverOps) resolveTactics(instance monster.Instance) monster.Tactics {
	if ops.TacticsFor != nil {
		return ops.TacticsFor(instance)
	}
	return monster.ResolveTactics(instance)
}

// startWanderLeg probes the facing-relative direction, then requests the
// independently sampled travel distance from the actor's current position.
/*
================
startWanderLeg
================
*/
func (ops *MonsterMoverOps) startWanderLeg(divisionID string, instance monster.Instance, tactics monster.Tactics, mover monster.MoverState, nowMs int64) []Frame {
	before := ops.Monsters.prepareNavigation(divisionID, instance.Gid)
	if !monster.CanEnterWander(instance.Ref.TidWord, instance.Ref.RunSpeed) || instance.Ref.WalkSpeed <= 0 || tactics.WanderProbeDistance <= 0 {
		// Zero-speed types never wander.
		mustMoverTransition(&mover, monster.MoverEventEntryRefused, 0)
		mover.BehaviorDeadlineMs = nowMs + ops.idleDelayMs()
		mover, frames := ops.planIdleEntry(instance, mover, nowMs)
		return ops.Monsters.commitNavigation(divisionID, before, mover, frames)
	}
	// Nest containment, probe origin and movement goal are separate inputs.
	live := mover.LivePoseAt(nowMs, ops.TerrainHeight)
	motion := monster.NativeWanderMotion(live.Heading, func() uint32 { return monster.SummonRandomWord(ops.rand()) })
	if mover.ControllerGID() != 0 {
		if controller, ok := ops.Monsters.Mover(divisionID, mover.ControllerGID()); ok {
			motion = motion.TowardController(live, controller.LivePoseAt(nowMs, ops.TerrainHeight))
		}
	}
	if ops.hasPlanner() {
		probe := normalizeMonsterPose(motion.Destination(live, tactics.WanderProbeDistance))
		path := ops.planPath(live, mover.LiveNavOwner(nowMs), probe)
		if path == nil {
			// Missing geometry cannot be interpreted as a successful probe.
			mustMoverTransition(&mover, monster.MoverEventStartWander, 0)
			mover, frames := ops.holdNavigation(instance.Gid, mover, live, nowMs)
			return ops.Monsters.commitNavigation(divisionID, before, mover, frames)
		}
		motion = motion.AfterProbe(path.Result())
	}
	dest := normalizeMonsterPose(motion.Destination(live, motion.Distance))
	mustMoverTransition(&mover, monster.MoverEventStartWander, 0)
	mover.BehaviorDeadlineMs = nowMs + monster.NativeWanderDelayMs(func() uint32 { return monster.SummonRandomWord(ops.rand()) })
	mover, frames := ops.planSegment(instance, mover, dest, instance.WalkSpeed(), wire.MoveStateWalk, nowMs)
	return ops.Monsters.commitNavigation(divisionID, before, mover, frames)
}

// startReturnLeg uses the native movement-channel consumer (5489B0), not
// the unrelated obstacle rotation sign at CTactics+164.
/*
================
startReturnLeg
================
*/
func (ops *MonsterMoverOps) startReturnLeg(divisionID string, instance monster.Instance, tactics monster.Tactics, mover monster.MoverState, event monster.MoverEvent, nowMs int64) []Frame {
	before := ops.Monsters.prepareNavigation(divisionID, instance.Gid)
	mover, frames := ops.planReturnLeg(instance, mover, event, nowMs)
	return ops.Monsters.commitNavigation(divisionID, before, mover, frames)
}

/*
================
planReturnLeg
================
*/
func (ops *MonsterMoverOps) planReturnLeg(instance monster.Instance, mover monster.MoverState, event monster.MoverEvent, nowMs int64) (monster.MoverState, []Frame) {
	speed := instance.WalkSpeed()
	channel := wire.MoveStateWalk
	if instance.Nest.HasControls && monster.HomingRuns(instance.Ref.RunSpeed) {
		speed, channel = instance.RunSpeed(), wire.MoveStateRun
	}
	live := mover.LivePoseAt(nowMs, ops.TerrainHeight)
	mover.Pose = live
	mustMoverTransition(&mover, event, 0)
	mover.BehaviorDeadlineMs = 0
	if instance.Nest.HasControls {
		mover.HomingStartedMs = uint32(nowMs)
		// 55A60E..55A63E: (effective sight / run speed) * 1000, truncate.
		// A stationary actor cannot complete this travel-dependent gate.
		mover.HomingAcquireAfterMs = ^uint32(0)
		if monster.HomingRuns(instance.Ref.RunSpeed) {
			mover.HomingAcquireAfterMs = uint32(float64(float32(instance.Nest.SightRange+instance.BodyRadius())) / float64(float32(instance.RunSpeed())) * 1000)
		}
	}
	dest := anchorPose(instance)
	if instance.Nest.HasControls {
		dest = normalizeMonsterPose(monster.HomingCandidate(dest, live, float32(instance.Nest.Radius), instance.Ref, func() uint32 { return monster.SummonRandomWord(ops.rand()) }))
		if ops.hasPlanner() {
			// 545E14: clip from HOME, not the actor's current position. The
			// anchor has no walked cell: the teleport rule resolves it.
			path := ops.planPath(anchorPose(instance), NavOwner{}, dest)
			if path == nil {
				if ops.PlanRoute != nil {
					mover.SetNavigationMotion(speed, channel)
					return ops.waitForNavigation(instance.Gid, mover, live, dest, nowMs)
				}
				return ops.holdNavigation(instance.Gid, mover, live, nowMs)
			}
			dest = path.Rest()
		}
	}
	return ops.planSegment(instance, mover, dest, speed, channel, nowMs)
}

/*
==================
commitSegment

commitSegment is THE single emit seam: computes the timed segment from
the live departure pose, commits the mover, and builds the frames -
an optional 0x3122 MOVE channel push (only when the run/walk channel
changes; sub_777b60 case 1 -> SetRunWalkMode, the pinned wire that
keeps the client integrator at the server's segment speed - BUG-7
speed-half fix) followed by the 0xB738 goal. The optional source follows
the retail autonomous-entity turn gate, not the player ack's first-move
lifecycle.
==================
*/
func (ops *MonsterMoverOps) commitSegment(divisionID string, instance monster.Instance, mover monster.MoverState, dest monster.Pose, speed float64, channel uint8, nowMs int64) []Frame {
	before := ops.Monsters.prepareNavigation(divisionID, instance.Gid)
	return ops.commitPreparedSegment(divisionID, before, instance, mover, dest, speed, channel, nowMs)
}

// Approach allocation and geometry must publish in the same navigation transaction.
/*
================
commitPreparedSegment
================
*/
func (ops *MonsterMoverOps) commitPreparedSegment(divisionID string, before navigationAdmission, instance monster.Instance, mover monster.MoverState, dest monster.Pose, speed float64, channel uint8, nowMs int64) []Frame {
	mover, frames := ops.planSegment(instance, mover, dest, speed, channel, nowMs)
	return ops.Monsters.commitNavigation(divisionID, before, mover, frames)
}

// Plan only: terrain sampling may run concurrently with damage/lifecycle work.
// The caller must admit the result before publishing any returned frame.
/*
================
planDirectSegment
================
*/
func (ops *MonsterMoverOps) planDirectSegment(instance monster.Instance, mover monster.MoverState, dest monster.Pose, speed float64, channel uint8, nowMs int64) (monster.MoverState, []Frame) {
	// Quantise the destination to the grid the WIRE can express, before
	// anything derives from it. The 0xB738 goal is the only destination the
	// client ever learns and it carries x/y/z as ROUNDED u16
	// (BuildMovementAckPayload, move.go) - so the client can only ever walk
	// to an INTEGER position. Keeping a fractional destination as server
	// truth means the 0xB2F5 settle, which encodes f32, hands the client the
	// UNROUNDED pose AFTER it has already stopped on the rounded one, and it
	// slides the residual (up to 0.5u per axis) off after halting - BUG-12,
	// "they always slide a 1cm after stop". Quantise the truth rather than
	// widening the wire: the u16 destination is the native contract, and the
	// deliberate precision split is visible in move.go, where the SOURCE
	// block carries roundU16(x*10) for 0.1u while the DESTINATION is raw u16.
	//
	// Y is deliberately NOT quantised. The client re-resolves ground height
	// per integrated step, so its true arrival height is the terrain under
	// the arrival XZ rather than the goal's y; an integer y here would put
	// the settle in a fight with that resolver. Re-sample at the quantised
	// XZ instead - the ground under a rounded destination is not the ground
	// under the fractional one the caller picked.
	dest.X = math.Round(dest.X)
	dest.Z = math.Round(dest.Z)
	dest = normalizeMonsterPose(dest)
	if !ops.hasPlanner() && ops.TerrainHeight != nil {
		if y, ok := ops.TerrainHeight(dest.RegionID, dest.X, dest.Z); ok {
			dest.Y = y
		}
	}

	// Capture the old leg before replacing it. The v1.188 server's movement
	// writer includes the live source only for a >45-degree discontinuity.
	// Shallow chase re-aims therefore preserve the client's PATH source cursor;
	// reversals and hard cuts re-anchor that logical cursor at this live pose.
	// This says nothing about animation playback: v1.150 sub_776200 re-enters
	// state 9 for every B738 destination, and the cyclic mixer reset clears its
	// authored cursor. The browser presentation adapter separately retains the
	// visible walk/run phase while that locomotion lane remains continuously live.
	wasInFlight := mover.InFlight(nowMs)

	// Segment departure: `from` becomes mover.From - the interpolation BASE
	// of the whole next segment - so a chord height here would propagate
	// into every later scope-enter read (BUG-8 second injection path).
	from := mover.LivePoseAt(nowMs, ops.TerrainHeight)
	goal := dest
	var navigation *monster.NavigationPath
	if ops.hasPlanner() {
		// Walk from the cell under the monster (native source pNavCell).
		navigation = ops.planPath(from, mover.LiveNavOwner(nowMs), goal)
		if navigation == nil {
			return ops.holdNavigation(instance.Gid, mover, from, nowMs)
		}
		dest = navigation.Rest()
	}
	distance := planarDistance(from, dest)
	if distance < 0.01 || speed <= 0 {
		mover.Pose = from
		mover.From, mover.To = monster.Pose{}, monster.Pose{}
		mover.DepartMs, mover.ArriveMs = 0, 0
		mustMoverTransition(&mover, monster.MoverEventSegmentArrived, 0)
		if mover.IdleEntryPending() {
			mover.BehaviorDeadlineMs = nowMs + ops.idleDelayMs()
		}
		if wasInFlight {
			return mover, []Frame{correctionFrame(instance.Gid, from)}
		}
		return mover, nil
	}

	sourceRequired := monsterMovementSourceRequired(mover, from, dest, wasInFlight)
	headingWord := headingWordToward(from, dest)
	dest.Heading = headingWord
	mover.From = from
	mover.To = dest
	mover.DepartMs = nowMs
	mover.ArriveMs = nowMs + int64(distance/speed*1000)
	if mover.ArriveMs <= nowMs {
		mover.ArriveMs = nowMs + 1
	}
	mover.AdoptNavigation(navigation)

	var frames []Frame
	if currentChannel(mover.Channel) != channel {
		refresh := wire.ObjectStateRefresh{
			Gid:       instance.Gid,
			StateType: wire.StateChannelMove,
			Value:     channel,
		}
		frames = append(frames, Frame{Opcode: wire.OpObjectStateRefresh, Payload: refresh.Encode()})
	}
	mover.Channel = channel

	var source *MovementSource
	if sourceRequired {
		source = &MovementSource{RegionID: from.RegionID, X: from.X, Y: from.Y, Z: from.Z}
	}
	payload := BuildMovementAckPayload(instance.Gid, MovementRequest{
		Mode:     MovementAckDestinationMode,
		RegionID: goal.RegionID,
		X:        goal.X,
		Y:        goal.Y,
		Z:        goal.Z,
	}, source)
	return mover, append(frames, Frame{Opcode: OpMovementAck, Payload: payload})
}

// A missing navigation result settles the old segment in the SAME admitted
// transaction. Sending nothing would leave the client walking the old goal.
/*
================
holdNavigation
================
*/
func (ops *MonsterMoverOps) holdNavigation(gid uint32, mover monster.MoverState, live monster.Pose, now int64) (monster.MoverState, []Frame) {
	mover.Pose = live
	mover.From, mover.To = monster.Pose{}, monster.Pose{}
	mover.DepartMs, mover.ArriveMs = 0, 0
	mover.AdoptNavigation(nil)
	mustMoverTransition(&mover, monster.MoverEventSegmentArrived, 0)
	if mover.IdleEntryPending() {
		mover.BehaviorDeadlineMs = now + ops.idleDelayMs()
	}
	return mover, []Frame{correctionFrame(gid, live)}
}

/*
==================
monsterMovementSourceRequired

monsterMovementSourceRequired mirrors the v1.188 GameServer's positional
movement source gate at 0x004b1109..0x004b118f. A settled entity's current
facing is its last committed heading; an in-flight entity's facing is the
old segment direction. Degenerate vectors source defensively because they
cannot establish continuous steering.
==================
*/
func monsterMovementSourceRequired(mover monster.MoverState, from, dest monster.Pose, wasInFlight bool) bool {
	var oldFrom, oldTo monster.Pose
	if wasInFlight {
		oldFrom, oldTo = mover.From, mover.To
	} else {
		oldFrom = from
		oldTo = poseOneUnitAlongHeading(from, mover.Pose.Heading)
	}

	oldDX, oldDZ := monsterPoseDelta(oldFrom, oldTo)
	newDX, newDZ := monsterPoseDelta(from, dest)
	oldLength := math.Hypot(oldDX, oldDZ)
	newLength := math.Hypot(newDX, newDZ)
	if oldLength <= 0 || newLength <= 0 {
		return true
	}

	dot := (oldDX*newDX + oldDZ*newDZ) / (oldLength * newLength)
	dot = clampFloat(dot, -1, 1)
	return math.Acos(dot) > monsterMovementSourceTurnThreshold
}

// poseOneUnitAlongHeading reconstructs Math_YawToDirVec's planar convention
// after 8535A0 converts the wire bearing: heading 0 faces +X.
/*
================
poseOneUnitAlongHeading
================
*/
func poseOneUnitAlongHeading(from monster.Pose, heading uint16) monster.Pose {
	yaw := float64(heading)/65535*2*math.Pi + math.Pi/2
	to := from
	to.X += math.Sin(yaw)
	to.Z -= math.Cos(yaw)
	return to
}

// currentChannel maps the zero value (fresh mover) to the walk channel
// the create row ships (MonsterSpawnSpeedChannel).
/*
================
currentChannel
================
*/
func currentChannel(channel uint8) uint8 {
	if channel == 0 {
		return wire.MoveStateWalk
	}
	return channel
}

// hasPlanner reports whether a world-surface planner is wired.
/*
================
hasPlanner
================
*/
func (ops *MonsterMoverOps) hasPlanner() bool {
	return ops.PlanPathFrom != nil || ops.PlanPath != nil
}

/*
==================
planPath

planPath plans from `from` walking from fromOwner. Legs departing from the
monster's live pose pass mover.LiveNavOwner; a leg from the nest anchor
passes the zero owner (native teleport rule, 545E14 clips from HOME).
==================
*/
func (ops *MonsterMoverOps) planPath(from monster.Pose, fromOwner NavOwner, goal monster.Pose) *monster.NavigationPath {
	if ops.PlanPathFrom != nil {
		return ops.PlanPathFrom(from, fromOwner, goal)
	}
	return ops.PlanPath(from, goal)
}

/*
================
rand
================
*/
func (ops *MonsterMoverOps) rand() float64 {
	if ops.Rand == nil {
		return 0.5
	}
	return ops.Rand()
}

// ---- pure helpers ----

/*
================
anchorPose
================
*/
func anchorPose(instance monster.Instance) monster.Pose {
	if instance.Nest.HasControls {
		// 545F70/545C50 use CNest+10, not the randomized spawn candidate.
		// Using Spawn shifts the home boundary separately for every sibling.
		return monster.Pose{RegionID: instance.Nest.RegionID, X: instance.Nest.X, Y: instance.Nest.Y, Z: instance.Nest.Z}
	}
	return monster.Pose{
		RegionID: instance.Spawn.RegionID,
		X:        instance.Spawn.X,
		Y:        instance.Spawn.Y,
		Z:        instance.Spawn.Z,
	}
}

/*
================
poseToSpawn
================
*/
func poseToSpawn(p monster.Pose) Spawn {
	return Spawn{RegionID: p.RegionID, X: p.X, Y: p.Y, Z: p.Z, Angle: p.Heading}
}

/*
================
spawnToPose
================
*/
func spawnToPose(p Spawn) monster.Pose {
	return monster.Pose{RegionID: p.RegionID, X: p.X, Y: p.Y, Z: p.Z, Heading: p.Angle}
}

/*
================
planarDistance
================
*/
func planarDistance(a, b monster.Pose) float64 {
	return WorldDistance2D(poseToSpawn(a), poseToSpawn(b))
}

/*
================
planarDistanceSpawn
================
*/
func planarDistanceSpawn(a Spawn, b Spawn) float64 {
	return WorldDistance2D(a, b)
}

// All movement producers encode wire bearings through the shared inverse of
// native 8535A0. Native model yaw and wire heading differ by pi/2.
/*
================
headingWordToward
================
*/
func headingWordToward(from, to monster.Pose) uint16 {
	dx, dz := monsterPoseDelta(from, to)
	return headingWordFromDelta(dx, dz)
}

/*
================
correctionFrame
================
*/
func correctionFrame(gid uint32, pose monster.Pose) Frame {
	correction := wire.ObjectSourceCorrection{
		Gid: gid,
		Position: wire.Position{
			RegionID: pose.RegionID,
			X:        float32(pose.X),
			Y:        float32(pose.Y),
			Z:        float32(pose.Z),
			Heading:  pose.Heading,
		},
	}
	return Frame{Opcode: wire.OpObjectSourceCorrection, Payload: correction.Encode()}
}

/*
================
idleDelayMs
================
*/
func (ops *MonsterMoverOps) idleDelayMs() int64 {
	return monster.NativeIdleDelayMs(func() uint32 { return monster.SummonRandomWord(ops.rand()) })
}
