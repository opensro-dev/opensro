/*
===========================================================================

skilltrapfield.go - hostile fields: the Rogue's Poison Trap pulses its
statuses on the enemies standing in it

CGSkillObject_SpawnAtOwner (48CCC0) gives a hostile skill object mode 1.
Every 48CEA0 pass (300 ms) collects up to the area's most-targets hostiles
around the object (48D690 mode 1), and
CGSkillObject_ExecuteCollectedAttackRecipients (48D9B0) strikes the
collection once puls has passed since its last strike; the collection is
dropped either way. Without the trap word (+0x4A0) the result is the
planter's B0BC (v1.150 B3C6 mode 2, wire.SkillPulseFrame) and the object
lives on; with it, the object detonates once (skillcombattrap.go). The
row carries no att: each strike is the status cast's zero-damage record
with the 590680 roll. The cast and the plant are the combat trap's.

===========================================================================
*/

package action

import (
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/game/world/skillobject"
)

/*
================
advanceTrapField

One 48CEA0 pass for a hostile field. The caller holds the division lock.
An empty collection leaves the pulse clock alone (48D9B0 returns before
reading it), so the first enemy to step in after a quiet spell is struck
at once.
================
*/
func (rt *Runtime) advanceTrapField(object skillobject.Object, c, snapshot *enterworld.Character, ownerPresent bool, lease instance.Lease, nowMs int64) []simulation.DivisionFrames {
	object, due, retired := rt.SkillObjects.Advance(object.Spawn.GID, nowMs, ownerPresent)
	if retired || !due || !skillobject.PulseDue(object, nowMs) {
		return nil
	}
	skill, known := rt.deps.SkillData().SkillByID(object.Program.SkillID)
	if !known || !skill.TrapField.Pinned {
		return nil
	}
	strike := trapFieldStrike(skill)
	victims := rt.trapFieldVictims(object, snapshot, strike, lease, nowMs)
	if len(victims) == 0 || !rt.SkillObjects.Pulsed(object.Spawn.GID, nowMs) {
		return nil
	}
	source := object.OwnerGID
	return rt.strikeFromObject(objectStrike{object: object, c: c, snapshot: snapshot, skill: strike, victims: victims,
		result: func(targets []wire.SkillAreaTarget) wire.Frame {
			return wire.SkillPulseFrame(source, skill.ID, targets)
		}}, nowMs)
}

/*
================
trapFieldStrike

The row as the status cast's strike: no att, one zero-damage record a
victim whose only consequence is the status roll (58E5F0), around the
object with the field's selector.
================
*/
func trapFieldStrike(skill enterworld.SkillRow) enterworld.SkillRow {
	field := skill.TrapField
	strike := skill
	strike.StatusCast = true
	strike.Attack = enterworld.SkillAttack{ImpactCount: 1}
	strike.OffensiveArea = enterworld.SkillOffensiveArea{Shape: 1, Radius: field.Radius, MaxTargets: uint8(field.MaxTargets), Select: field.Select}
	return strike
}

/*
================
trapFieldVictims

The living monsters and attackable players around the object, up to the
area's most-targets. INFERENCE: nearest first, as every other area here;
the native collection walks the object's cells in map order, which no
player can observe.
================
*/
func (rt *Runtime) trapFieldVictims(object skillobject.Object, planter *enterworld.Character, strike enterworld.SkillRow, lease instance.Lease, now int64) []combatTarget {
	area := strike.OffensiveArea
	q := areaQuery{selects: area.Select, division: object.Division, caster: planter, skill: strike, lease: lease, center: fieldCenter(object),
		reach: float64(area.Radius), nearest: true, now: now}
	var out []combatTarget
	for _, candidate := range rt.areaCandidates(q) {
		if len(out) >= int(area.MaxTargets) {
			break
		}
		out = append(out, candidate.target)
	}
	return out
}
