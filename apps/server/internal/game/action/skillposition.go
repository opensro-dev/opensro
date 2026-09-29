/*
===========================================================================

skillposition.go - position skills: teleports and dashes (5862E0 / 593540)

===========================================================================
*/

package action

import (
	"math"
	"sync/atomic"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/wire"
	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
positionSkillGoal

Clamp a destination in one compatible coordinate plane before navigation.
================
*/
func positionSkillGoal(from, requested simulation.Spawn, limit uint32) (simulation.Spawn, bool) {
	if !worldgeom.SamePlane(from.RegionID, requested.RegionID) || worldgeom.IsDungeonRegion(from.RegionID) && from.RegionID != requested.RegionID {
		return simulation.Spawn{}, false
	}
	a := worldgeom.RegionXZ{RegionID: from.RegionID, X: from.X, Z: from.Z}
	b := worldgeom.RegionXZ{RegionID: requested.RegionID, X: requested.X, Z: requested.Z}
	dx, dz := worldgeom.Delta(a, b)
	dy := requested.Y - from.Y
	distance := math.Sqrt(dx*dx + dy*dy + dz*dz)
	if math.IsNaN(distance) || math.IsInf(distance, 0) {
		return simulation.Spawn{}, false
	}
	fraction := 1.0
	if distance > float64(limit) {
		fraction = float64(limit) / distance
	}
	p := worldgeom.Interpolate(a, b, fraction)
	return simulation.Spawn{RegionID: p.RegionID, X: p.X, Y: from.Y + dy*fraction, Z: p.Z, Angle: from.Angle}, true
}

/*
================
acceptPositionSkill

Called under the division action lock. Native 5862E0 plans and navigates;
593540 commits the destination with the vitals debit before publication.
================
*/
func (rt *Runtime) acceptPositionSkill(division string, character, snapshot *enterworld.Character, cast wire.SkillAction, skill enterworld.SkillRow) OpResult {
	now := rt.Now().UnixMilli()
	groundCast := skill.PositionEffect.Pinned && cast.HasGroundTarget && !cast.HasTarget
	casterReady := enterworld.SkillLearned(snapshot, skill.ID) && enterworld.CharacterAlive(snapshot)
	if !groundCast || !casterReady {
		return OpResult{DiagnosticRefusal: "position-skill-admission-refused"}
	}
	if snapshot.ActiveCOS != nil && snapshot.ActiveCOS.Mounted {
		return OpResult{DiagnosticRefusal: "position-skill-mounted"}
	}
	if rt.skillCastPostureBlocked(division, snapshot, now) || rt.hasOpenSkillCast(division, snapshot.Name) {
		return OpResult{DiagnosticRefusal: "position-skill-action-busy"}
	}
	if code := rt.skillAdmission(division, snapshot, skill, now, nil, nil, admitExecution); code != 0 {
		return offensiveRefusal(code)
	}
	// There is no unguarded movement fallback when navigation is unavailable.
	if rt.ConstrainMovement == nil && rt.ConstrainWalk == nil {
		return OpResult{DiagnosticRefusal: "position-skill-navigation-unavailable"}
	}
	key := simulation.WorldKey(division, snapshot.Name)
	var point wire.SkillCastFacingPoint
	var refusal uint16
	var vitals wire.Frame
	gid := enterworld.ObjectIDForCharacter(snapshot)
	if !rt.deps.Update(character, "skill-position", func() bool {
		mounted := character.ActiveCOS != nil && character.ActiveCOS.Mounted
		if character.DeletePending || character.NativeTeleportMode == 1 || mounted {
			return false
		}
		if !enterworld.CharacterAlive(character) || !enterworld.SkillLearned(character, skill.ID) || rt.skillCastPostureBlocked(division, character, now) {
			return false
		}
		cost, code := rt.offensivePhaseCost(division, character, skill, now, nil)
		refusal = code
		if code != 0 {
			return false
		}
		from, fromOwner := rt.liveNav(key, character, now)
		to, ok := positionSkillGoal(from, simulation.Spawn{RegionID: cast.Region, X: float64(int16(cast.GroundX)), Y: float64(int16(cast.GroundY)), Z: float64(int16(cast.GroundZ))}, skill.PositionEffect.Range)
		if !ok {
			return false
		}
		plan, ok := rt.planSkillTravel(character.Name, from, fromOwner, to)
		if !ok {
			return false
		}
		point = plan.point
		rt.startSkillCast(division, character, now)
		rt.commitOffensivePhaseCost(division, character, skill, cost, now, false)
		rt.commitSkillTravel(key, character, plan)
		vitals = wire.Frame{Opcode: simulation.OpVitalsUpdate, Payload: simulation.VitalsRefreshWithSourcePayload(gid, simulation.VitalsSourceSkillRecovery, rt.publishedVitals(division, character))}
		return true
	}) {
		if refusal != 0 {
			return offensiveRefusal(refusal)
		}
		return OpResult{DiagnosticRefusal: "position-skill-commit-refused"}
	}
	rt.Pending.Clear(grounditem.PendingKey(division, snapshot.Name))
	rt.bindResidentRegion(key, now)
	token := atomic.AddUint32(&rt.castTokenCounter, 1)
	open := wire.SkillCastTravelFrame(wire.SkillCastSuccess{SkillId: skill.ID, CasterGid: gid, InstanceToken: token}, point)
	// Instant native handler emits release (58701F); guided arrival owns the
	// client close (8DD550). A zero-time finalize would truncate the dash.
	release := wire.SkillCastReleaseFrame(token, gid)
	return OpResult{Frames: []wire.Frame{open, release, vitals}, Broadcast: []wire.Frame{open, release}, ActorPrivate: []wire.Frame{vitals}}
}
