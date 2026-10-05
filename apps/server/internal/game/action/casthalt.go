/*
===========================================================================

casthalt.go - a skill cast stops its caster's walk

CSkillManager_InitiateSkillCast (v1.188 59B480) stops a moving caster
before anything else happens to the cast. For every skill with activity 2
(column 8, ref +0x65: the ordinary cast that queues on the command actor,
4AD870) it calls the character's StopMove (vtable +0x4B0, CGObjChar_StopMove
4A9430) with the move angle and broadcast set. The caster stands at its live
point and its nearby sessions get the stop (v1.188 0xB023, v1.150 0xB2F5)
ahead of the cast. Instant activities (imbues, speed skills) keep the walk.

Targeted casts already reach this point settled: the approach ends in
enterBasicAttackRange. A self, party, area or position cast pressed while
walking used to cast on the move, which no original character can do.

===========================================================================
*/

package action

import (
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
sourceCorrectionFrame

The B2F5 stop of a character standing at spawn: path halt for its own
client, position correction for its peers.
================
*/
func sourceCorrectionFrame(c *enterworld.Character, spawn simulation.Spawn) wire.Frame {
	return wire.Frame{
		Opcode: wire.OpObjectSourceCorrection,
		Payload: wire.ObjectSourceCorrection{
			Gid: enterworld.ObjectIDForCharacter(c),
			Position: wire.Position{
				RegionID: spawn.RegionID,
				X:        float32(spawn.X), Y: float32(spawn.Y), Z: float32(spawn.Z),
				Heading: spawn.Angle,
			},
		}.Encode(),
	}
}

/*
================
haltCasterWalk

59B5F6..59B62C for one cast. The caller holds c's door; the stop is
published at once, so it reaches the caster and its peers before the cast
the caller returns. A mounted rider never walks itself (its vehicle does,
and skills are refused on a saddle), so it has nothing to stop.
================
*/
func (rt *Runtime) haltCasterWalk(division string, c *enterworld.Character, skill enterworld.SkillRow, now int64) {
	if !skill.HaltsWalk() || rt.Worlds == nil || mountedOnCOS(c) {
		return
	}
	halted := false
	state := rt.Worlds.Update(simulation.WorldKey(division, c.Name),
		func() simulation.WorldState { return simulation.SeedWorldState(c) },
		func(world *simulation.WorldState) {
			// CGObjChar IsMoving (vtable +0x4C0): a segment still in flight.
			if !world.MoveSegment.Valid() || now >= world.MoveSegment.ArrivesAtMs {
				return
			}
			world.SettleLive(now)
			halted = true
		})
	if !halted {
		return
	}
	writeBackWorld(c, state)
	rt.publishBodyStatus(division, c.Name, []wire.Frame{sourceCorrectionFrame(c, state.Spawn)})
}
