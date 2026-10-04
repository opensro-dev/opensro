/*
===========================================================================

follow.go - player Trace admission and persistent pursuit

Native command family 3 (4AE3D0) follows a living player. Its navigator
(4B0490) shares normal movement collision and publication, but never owns
a combat skill. The existing command slot provides cancellation ordering.

===========================================================================
*/
package action

import (
	"math"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	followInnerGap        = 50
	followOuterGap        = 80
	followMaximumDistance = 1000
	followOverlapInset    = 4
	followTurnThreshold   = math.Pi / 12
)

/*
================
stopFollowMovement

Command replacement must settle the issued leg as well as retire the
continuation. Other action families retain their own movement lifecycle.
The caller holds the division lock while checking and settling the command.
================
*/
func (rt *Runtime) stopFollowMovement(division string, character, snapshot *enterworld.Character) OpResult {
	key := simulation.WorldKey(division, character.Name)
	rt.basicAttackIntentsMu.Lock()
	intent := rt.basicAttackIntents[key]
	rt.basicAttackIntentsMu.Unlock()
	if !intent.FollowTarget {
		return OpResult{}
	}
	nowMs := rt.Now().UnixMilli()
	from := rt.liveSpawn(key, snapshot, nowMs)
	stopped, _ := rt.enterBasicAttackRange(character, snapshot, key, from, nowMs)
	return stopped
}

/*
================
beginFollow

Keep both actors in the same admitted world lifetime. Persisted characters
are not live native objects and cannot be followed while offline.
================
*/
func (rt *Runtime) beginFollow(division string, character *enterworld.Character, request wire.FollowTarget, nowMs int64) OpResult {
	snapshot := rt.characterSnapshot(division, character)
	if snapshot == nil || snapshot.DeletePending || !enterworld.CharacterAlive(snapshot) || snapshot.NativeTeleportMode != 0 {
		return OpResult{DiagnosticRefusal: "follow-character-unavailable"}
	}
	if mountedOnCOS(snapshot) {
		return mountedCommandRefusal()
	}
	if stand, seated := rt.standForSeatedCommand(division, character, nowMs); seated {
		return stand
	}
	if rt.hasOpenSkillCast(division, snapshot.Name) || rt.skillCastPostureBlocked(division, snapshot, nowMs) {
		return OpResult{DiagnosticRefusal: "follow-character-busy"}
	}
	intent := basicAttackIntent{
		FollowTarget: true, DivisionID: division, CharacterName: snapshot.Name, TargetGid: request.TargetGid,
	}
	target, _, ok := rt.followActors(snapshot, intent, nowMs)
	if !ok {
		return OpResult{DiagnosticRefusal: "follow-target-unavailable"}
	}
	admission, exists := rt.characterAdmissions.Load(simulation.WorldKey(division, target.Name))
	if !exists {
		return OpResult{DiagnosticRefusal: "follow-target-unavailable"}
	}
	intent.FollowSession = admission.(populationAdmission).session
	rt.Pending.Clear(grounditem.PendingKey(division, snapshot.Name))
	rt.setCombatIntent(intent)
	return rt.advanceFollowIntent(character, intent, nowMs)
}

/*
================
followActors

4AD030 family 3 checks player type, life and adjacent sectors. The navigator
also retires at more than 1,000 units (4B05A5). Lease equality adds the
port's explicit world-instance boundary to those native object checks.
================
*/
func (rt *Runtime) followActors(actor *enterworld.Character, intent basicAttackIntent, nowMs int64) (*enterworld.Character, simulation.CombatSpacing, bool) {
	var spacing simulation.CombatSpacing
	if actor == nil || actor.DeletePending || !enterworld.CharacterAlive(actor) || actor.NativeTeleportMode != 0 || mountedOnCOS(actor) {
		return nil, spacing, false
	}
	target := rt.characterSnapshot(intent.DivisionID, rt.findCharacterByGid(intent.DivisionID, intent.TargetGid))
	if target == nil || target.ID == actor.ID || target.DeletePending || !enterworld.CharacterAlive(target) || target.NativeTeleportMode != 0 {
		return nil, spacing, false
	}
	actorLease, actorOnline := rt.EntryPopulationLease(intent.DivisionID, actor.Name)
	targetLease, targetOnline := rt.EntryPopulationLease(intent.DivisionID, target.Name)
	if !actorOnline || !targetOnline || actorLease != targetLease {
		return nil, spacing, false
	}
	if intent.FollowSession != 0 {
		if _, current := rt.CharacterPopulationLease(intent.DivisionID, target.Name, intent.FollowSession); !current {
			return nil, spacing, false
		}
	}
	from := rt.liveSpawn(simulation.WorldKey(intent.DivisionID, actor.Name), actor, nowMs)
	to := rt.liveSpawn(simulation.WorldKey(intent.DivisionID, target.Name), target, nowMs)
	if !samePlaneAdjacent(from, to) || relative(from, to).length() > followMaximumDistance {
		return nil, spacing, false
	}
	spacing, ok := rt.playerToPlayerCombatSpacing(actor, target, followInnerGap)
	return target, spacing, ok
}

/*
================
followGoal

4B0490 holds inside the outer band and aims for the inner band. Overlapping
bodies back away by the sum of their radii. The native direction is 3D,
but movement keeps the actor's height until navigation resolves the floor.
================
*/
func followGoal(from, target simulation.Spawn, spacing simulation.CombatSpacing) (simulation.Spawn, bool) {
	delta := relative(from, target)
	distance := delta.length()
	bodies := float32(int32(spacing.ActorBodyRadius) + int32(spacing.TargetBodyRadius))
	travel := distance - bodies - followInnerGap
	if distance < bodies-followOverlapInset {
		travel = -bodies
	} else if distance <= bodies+followOuterGap {
		return from, false
	}
	if distance == 0 || !spacing.Valid() {
		return from, false
	}
	// The native navigator stores the normalized vector, displacement and
	// translated goal as float32 before truncating packet coordinates.
	direction := delta.normalized()
	x := float32(float64(from.X) + float64(direction.x*travel))
	z := float32(float64(from.Z) + float64(direction.z*travel))
	point := simulation.Vec3{X: float64(x), Y: from.Y, Z: float64(z)}
	region := simulation.RegionIDFromSeedLocal(from.RegionID, point.X, point.Z, simulation.NativeRegionSize)
	point = simulation.SeedLocalToRegionLocal(from.RegionID, region, point, simulation.NativeRegionSize)
	return simulation.NormalizeSpawnFrame(simulation.Spawn{
		RegionID: region, X: math.Trunc(point.X), Y: math.Trunc(point.Y), Z: math.Trunc(point.Z), Angle: from.Angle,
	}), true
}

/*
================
advanceFollowIntent

No cast lookup, cooldown or damage path can run for this command. Retain
the intent while waiting in range so a moving target can lead again.
================
*/
func (rt *Runtime) advanceFollowIntent(character *enterworld.Character, intent basicAttackIntent, nowMs int64) OpResult {
	snapshot := rt.characterSnapshot(intent.DivisionID, character)
	target, spacing, ok := rt.followActors(snapshot, intent, nowMs)
	if !ok || rt.skillCastPostureBlocked(intent.DivisionID, snapshot, nowMs) {
		rt.ClearCombatIntent(intent.DivisionID, intent.CharacterName)
		if snapshot != nil && snapshot.NativeTeleportMode == 0 && enterworld.CharacterAlive(snapshot) {
			key := simulation.WorldKey(intent.DivisionID, snapshot.Name)
			from := rt.liveSpawn(key, snapshot, nowMs)
			stopped, _ := rt.enterBasicAttackRange(character, snapshot, key, from, nowMs)
			return stopped
		}
		return OpResult{}
	}
	key := simulation.WorldKey(intent.DivisionID, snapshot.Name)
	from := rt.liveSpawn(key, snapshot, nowMs)
	to := rt.liveSpawn(simulation.WorldKey(intent.DivisionID, target.Name), target, nowMs)
	goal, move := followGoal(from, to, spacing)
	if !move || !rt.pursuitSteerDue(intent, key, snapshot, to, nowMs) {
		return OpResult{}
	}
	world := rt.Worlds.Snapshot(key, func() simulation.WorldState { return simulation.SeedWorldState(snapshot) })
	if world.MoveSegment.Valid() && nowMs < world.MoveSegment.ArrivesAtMs {
		heading, valid := simulation.HeadingFromMovement(from, goal)
		turn := float64(int16(heading-from.Angle)) * 2 * math.Pi / 65536
		if valid && math.Abs(turn) <= followTurnThreshold {
			return OpResult{}
		}
	}
	return rt.commitIntentMovement(character, snapshot, intentMovement{
		intent: intent, from: from, target: to, goal: goal, nowMs: nowMs,
	})
}
