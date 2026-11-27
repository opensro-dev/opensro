/*
===========================================================================

fortress_structure_pulse.go - fortress towers and the fort stone at war

A standing tower's or the fort stone's authored skill is a standing
effect: every pulse period it strikes the enemies inside its authored
area for a percent of their maximum hit points, rolling the row's
statuses like any other monster hit (the strikes ride the ordinary
monster strike doors, so walls, reflect, linked shares and the death
settlement all see an ordinary hit by the structure). The victims are
the world's residents and their companions that the fortress's side
test admits: the holder's guild and its union allies are refused, and
players in body modes 2..4 are refused, as the native target check
refuses them. Outside a war nothing pulses. The lane runs on the siege
tick beside the structures' persistence.

===========================================================================
*/

package action

import (
	"math"

	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/fortress"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
advanceFortressStructurePulses

Every division at war: each standing tower or fort stone whose skill
carries the pulse takes the pulse its standing time has reached - at
most one per tick, the clock re-anchored to the tick, so a stall
cannot burst. A structure first seen standing in a live war starts
its clock then, so its first pulse lands one period in. A structure
that falls, is replaced or leaves the war loses its clock.
================
*/
func (rt *Runtime) advanceFortressStructurePulses(nowMs int64) []simulation.DivisionFrames {
	if rt.Fortresses == nil || rt.Monsters == nil {
		return nil
	}
	if rt.structurePulses == nil {
		rt.structurePulses = map[string]map[uint32]int64{}
	}
	var out []simulation.DivisionFrames
	for _, division := range rt.Fortresses.Divisions() {
		if !rt.Fortresses.WarActive(division) {
			delete(rt.structurePulses, division)
			continue
		}
		next := rt.structurePulses[division]
		if next == nil {
			next = map[uint32]int64{}
			rt.structurePulses[division] = next
		}
		standing := map[uint32]bool{}
		for fortressID, world := range rt.fortressWorlds() {
			record, ok := rt.Fortresses.Get(division, fortressID)
			if !ok {
				continue
			}
			for _, structure := range rt.Monsters.WorldStructures(division, world) {
				if structure.CurrentHP == 0 {
					continue
				}
				skill, ok := rt.structurePulseSkill(structure)
				if !ok {
					continue
				}
				standing[structure.Gid] = true
				due, seen := next[structure.Gid]
				if !seen {
					next[structure.Gid] = nowMs + int64(skill.Abnormal.Pulse)
					continue
				}
				if nowMs < due {
					continue
				}
				next[structure.Gid] = nowMs + int64(skill.Abnormal.Pulse)
				out = append(out, rt.pulseFortressStructure(division, record, structure, skill, nowMs)...)
			}
		}
		for gid := range next {
			if !standing[gid] {
				delete(next, gid)
			}
		}
	}
	return out
}

/*
================
structurePulseSkill

The structure's pulse skill: the first of its authored default skills
whose row carries the pulse, the ratio record and the standing area
together. Towers and the fort stone carry exactly one; the positions,
headquarters and flags carry none - their standing effect is an aura,
not a pulse.
================
*/
func (rt *Runtime) structurePulseSkill(structure monster.Instance) (enterworld.SkillRow, bool) {
	skills := rt.deps.SkillData()
	if skills == nil {
		return enterworld.SkillRow{}, false
	}
	for _, id := range structure.Ref.DefaultSkillIDs {
		if id == 0 {
			continue
		}
		row, ok := skills.SkillByID(id)
		if ok && row.RatioDamage.Present && row.Abnormal.PulsePresent && row.Abnormal.PersistentArea.Present {
			return row, true
		}
	}
	return enterworld.SkillRow{}, false
}

/*
================
pulseFortressStructure

One pulse of one structure: the admitted residents and companions
inside its area, each struck as the structure's own hit, published
under one pulse result. The candidates come in roster order, each
player followed by its companions, and the area's authored cap
bounds the whole pulse. A victim already dead, out of the plane or
past the reach (the area's radius with both body radii added, as the
shape-1 selection measures) is skipped, never blocking the rest.
================
*/
func (rt *Runtime) pulseFortressStructure(division string, record fortress.Record, structure monster.Instance, skill enterworld.SkillRow, nowMs int64) []simulation.DivisionFrames {
	attacker, err := combat.MonsterInstanceStats(structure)
	if err != nil {
		return nil
	}
	area := skill.Abnormal.PersistentArea
	in := monsterStrikeInput{division: division, instance: structure, skill: skill, attacker: attacker,
		from:    monster.Pose{RegionID: structure.Spawn.RegionID, X: structure.Spawn.X, Y: structure.Spawn.Y, Z: structure.Spawn.Z},
		percent: fullAreaPercent, now: nowMs}
	center := in.origin()
	var targets []wire.SkillAreaTarget
	var public []wire.Frame
	var out []simulation.DivisionFrames
	struck := 0
	for _, victim := range rt.monsterAreaCandidates(in) {
		if struck >= int(area.MaxTargets) {
			break
		}
		if !worldgeom.SamePlane(victim.pose.RegionID, center.RegionID) {
			continue
		}
		if !victim.companion && victim.hostility.NativeBodyStatus >= 2 && victim.hostility.NativeBodyStatus <= 4 {
			continue
		}
		var guildID int64
		if victim.owner != nil && victim.owner.GuildID != nil {
			guildID = *victim.owner.GuildID
		}
		if rt.fortressDefender(division, record, guildID) {
			continue
		}
		if math.Hypot(simulation.WorldDistance2D(center, victim.pose), victim.pose.Y-center.Y) >
			float64(area.Radius)+float64(structure.Ref.BodyRadius)+victim.radius {
			continue
		}
		outcome := rt.strikeMonsterAreaVictim(in, victim)
		if !outcome.committed {
			continue
		}
		struck++
		targets = append(targets, wire.SkillAreaTarget{GID: outcome.strike.gid, Impacts: outcome.strike.impacts})
		public = append(public, outcome.strike.public...)
		if len(outcome.strike.private) > 0 && outcome.strike.owner != nil {
			out = append(out, hawkDivisionFrames(division, 0, outcome.strike.owner.ID, outcome.strike.private))
		}
	}
	if len(targets) == 0 {
		return nil
	}
	frames := []wire.Frame{wire.SkillPulseFrame(structure.Gid, skill.ID, targets)}
	frames = append(frames, public...)
	return append([]simulation.DivisionFrames{hawkDivisionFrames(division, structure.Gid, 0, frames)}, out...)
}
