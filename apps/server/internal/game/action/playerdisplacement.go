/*
===========================================================================

playerdisplacement.go - a struck player knocked down or back

SkillCombat_CalculateHitOutcome rolls a hit's displacement on any victim,
a player as much as a monster. A knockdown needs
SkillCombat_AllowsDisplacementOutcome (58E540): the model's RefObjChar
flag bit 0, not already down (motion 8) or in motion 0x12, not seated
(motion 4), not riding (state +0xE), no standing wall (+0xC0C). Its chance
is Formulae_CalculateStatusEffectProbability (40FC30, combat.KnockdownChance)
on the attacker's history key 0x44000000 | skill; it displaces twenty units
and holds motion 8 for KORecover + action duration + 0.5 s (58FF7A). A
knockback needs only the flag's bit 1 and no knockdown on the same record,
rolls the row's chance on key 0x45000000 | skill and pushes the authored
distance (590157).

While a player is down, CGObjChar_HandleMoveCommand (4B0EA0) drops its
ground commands and its commands wait (skillCastPostureBlocked); a hit on
it reads motion 8 (reqc). This file owns the state; the strike owners plan
and commit it.

===========================================================================
*/

package action

import (
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	// knockdownRollKey and knockbackRollKey are 58FF5C's and 5901B5's
	// probability history keys.
	knockdownRollKey uint32 = 0x44000000
	knockbackRollKey uint32 = 0x45000000
	// knockbackHoldMs is the knockback's displacement hold, as the monster
	// owner's (590157 schedules two seconds).
	knockbackHoldMs = 2000
	// displacementFlagKnockdown and displacementFlagKnockback are the
	// RefObjChar column 87 bits.
	displacementFlagKnockdown = 1
	displacementFlagKnockback = 2
	// motionKnockedDown is the motion state a knockdown holds.
	motionKnockedDown uint8 = 8
)

/*
================
playerDisplacement

A struck player's displacement: where it ends and until when it holds.
down is a knockdown (motion 8), else a knockback.
================
*/
type playerDisplacement struct {
	pose    simulation.Spawn
	untilMs int64
	down    bool
}

/*
================
displacementRoll

What one strike's displacement planning reads: the attacker's position
and roll history, the row, the victim and its pose.
================
*/
type displacementRoll struct {
	division string
	actor    criticalActor
	from     simulation.Spawn
	skill    enterworld.SkillRow
	victim   *enterworld.Character
	at       simulation.Spawn
	now      int64
}

/*
================
planPlayerDisplacement

58FF7A and 590157 for one surviving player impact. Nil when the row
displaces nothing or the rolls fail.
================
*/
func (rt *Runtime) planPlayerDisplacement(r displacementRoll) (*playerDisplacement, error) {
	if !r.skill.Knockdown.Present && !r.skill.Knockback.Present {
		return nil, nil
	}
	flags, recovery, ok := rt.deps.CharacterKnockdown(r.victim)
	if !ok {
		return nil, nil
	}
	return rt.planActorDisplacement(r, displacementTarget{flags: flags, recovery: recovery, level: levelByte(r.victim),
		allowed: rt.playerDisplaceable(r.division, r.victim, r.now)})
}

/*
================
displacementTarget

A creature uses its own authored flags and level, never its owner's model.
================
*/
type displacementTarget struct {
	flags    uint32
	recovery uint32
	level    uint8
	allowed  bool
}

/*
================
planActorDisplacement

58E540/58FF7A/590157 share the roll and displacement for every actor class.
================
*/
func (rt *Runtime) planActorDisplacement(r displacementRoll, target displacementTarget) (*playerDisplacement, error) {
	to := monster.Pose{RegionID: r.at.RegionID, X: r.at.X, Y: r.at.Y, Z: r.at.Z}
	if r.skill.Knockdown.Present && target.flags&displacementFlagKnockdown != 0 && target.allowed {
		chance := uint32(combat.KnockdownChance(r.skill.Knockdown.Rank, r.skill.Knockdown.Chance, target.level))
		proc, err := rt.effectOutcome(r.actor, knockdownRollKey|(r.skill.ID&0xffffff), chance)
		if err != nil {
			return nil, err
		}
		if proc {
			plan := knockdownConsequence(r.from, to, target.recovery, r.skill.ActionDurationMs, r.now)
			pose := simulation.Spawn{RegionID: plan.Pose.RegionID, X: plan.Pose.X, Y: plan.Pose.Y, Z: plan.Pose.Z}
			return &playerDisplacement{pose: pose, untilMs: plan.UntilMs, down: true}, nil
		}
	}
	if r.skill.Knockback.Present && int32(r.skill.Knockback.Chance) > 0 && target.flags&displacementFlagKnockback != 0 {
		proc, err := rt.effectOutcome(r.actor, knockbackRollKey|(r.skill.ID&0xffffff), r.skill.Knockback.Chance)
		if err != nil {
			return nil, err
		}
		if proc {
			point := displaceImpactPose(r.from, to, r.skill.Knockback.Distance)
			pose := simulation.Spawn{RegionID: point.RegionID, X: point.X, Y: point.Y, Z: point.Z}
			return &playerDisplacement{pose: pose, untilMs: r.now + knockbackHoldMs}, nil
		}
	}
	return nil, nil
}

/*
================
playerDisplaceable

58E540 for a player victim.
================
*/
func (rt *Runtime) playerDisplaceable(division string, victim *enterworld.Character, now int64) bool {
	if rt.PlayerKnockedDown(division, victim.Name, now) {
		return false
	}
	if victim.ActiveCOS != nil && victim.ActiveCOS.Mounted {
		return false
	}
	if rt.casterSitting(division, victim) {
		return false
	}
	return !rt.wallStanding(division, victim.Name)
}

/*
================
commitPlayerDisplacementInDoor

Inside the victim's door: the victim stops where the displacement ends,
its preparing casts and command end, and the hold starts. Returns the
impact's wire point and the victim's withdrawn casts.

The push walks as a move (593D24 -> CGObjPC_MoveByStepUnlessMounted 4EF300
-> CGObjChar_MoveByStep 48B920 -> CGObj_MoveTo 485740): the move query runs
from the victim's own cell, a refused walk leaves it where it stood, and an
admitted one ends where the walk came to rest, short of a blocked edge.
================
*/
func (rt *Runtime) commitPlayerDisplacementInDoor(division string, victim *enterworld.Character, d *playerDisplacement) (wire.SkillCastFacingPoint, []wire.Frame, bool) {
	key := simulation.WorldKey(division, victim.Name)
	from, fromOwner := rt.liveNav(key, victim, rt.Now().UnixMilli())
	landed, owner := rt.walkDisplacement(victim.Name, from, fromOwner, d.pose)
	d.pose = simulation.Spawn{RegionID: landed.RegionID, X: landed.X, Y: landed.Y, Z: landed.Z}
	point, valid := wire.NewSkillCastFacingPoint(d.pose.RegionID, d.pose.X, d.pose.Y, d.pose.Z)
	if !valid {
		return wire.SkillCastFacingPoint{}, nil, false
	}
	state := rt.Worlds.Update(key, func() simulation.WorldState { return simulation.SeedWorldState(victim) }, func(w *simulation.WorldState) {
		w.Spawn = simulation.Spawn{RegionID: d.pose.RegionID, X: d.pose.X, Y: d.pose.Y, Z: d.pose.Z, Angle: w.Spawn.Angle}
		w.MoveSegment = nil
		w.SetGoalOwner(owner)
		w.SpawnSet = true
		w.MovementSourceSeeded = true
	})
	writeBackWorld(victim, state)
	victim.World.MoveSegment = nil
	rt.ClearCombatIntent(division, victim.Name)
	withdrawn := rt.cancelPreparingProjectile(division, victim.Name)
	rt.playerDisplacements.Store(key, *d)
	return point, withdrawn, true
}

/*
================
walkDisplacement

The point and surface owner a pushed character's walk from its own cell
reaches. A refused walk (CGObj_MoveTo's blocked result) keeps the live
point and owner.
================
*/
func (rt *Runtime) walkDisplacement(name string, from simulation.Spawn, fromOwner simulation.NavOwner, to simulation.Spawn) (simulation.Spawn, simulation.NavOwner) {
	rest, walk, refused := rt.constrainWalk(name, from, fromOwner, to)
	if refused != nil {
		return from, fromOwner
	}
	owner := walk.Rest
	if rt.ResolveNavOwner != nil {
		if resolved, y, ok := rt.ResolveNavOwner(rest, walk.Rest); ok {
			owner, rest.Y = resolved, y
		}
	}
	return rest, owner
}

/*
================
PlayerKnockedDown

A knocked-down player's motion 8, until its hold ends.
================
*/
func (rt *Runtime) PlayerKnockedDown(division, name string, now int64) bool {
	value, ok := rt.playerDisplacements.Load(simulation.WorldKey(division, name))
	if !ok {
		return false
	}
	d := value.(playerDisplacement)
	if now >= d.untilMs {
		rt.playerDisplacements.Delete(simulation.WorldKey(division, name))
		return false
	}
	return d.down
}

/*
================
PlayerMotionLocked

4B0EA0's motion gate for the movement lane: a knocked-down player's
ground command is dropped.
================
*/
func (rt *Runtime) PlayerMotionLocked(division, name string) bool {
	return rt.PlayerKnockedDown(division, name, rt.Now().UnixMilli())
}

/*
================
playerTargetMotion

The motion a player target presents to a hit's reqc test: 8 while down.
================
*/
func (rt *Runtime) playerTargetMotion(division string, c *enterworld.Character, now int64) uint8 {
	if rt.PlayerKnockedDown(division, c.Name, now) {
		return motionKnockedDown
	}
	return 0
}
