/*
===========================================================================

exitgate.go - the quest instance exit gate, as the quest programmes see it

When a quest instance's boss dies, the reference death handler raises an
exit gate (INS_QUEST_TELEPORT) beside the fall and starts the room's
sixty-second event timer (#1878). The gate itself - its registry, select
answer and travel - belongs to the action package; this file owns the
programmes' half: where each boss fell, and the calls that raise and take
down a layer's gates. Each programme keeps its own state under its own
lock; the recorders here only write the site into that programme's layer.

===========================================================================
*/

package quest

import (
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
NoteKillSite

Where a monster fell, reported by the monster owner just before the kill
update: a quest instance programme keeps its boss's site to raise the
exit gate beside it. Records only.
================
*/
func (rt *Runtime) NoteKillSite(character *enterworld.Character, codename string, fell simulation.Spawn) {
	if character == nil {
		return
	}
	rt.noteRahid6BossSite(character, codename, fell)
	rt.noteEasteu21BossSite(character, codename, fell)
	rt.noteGoria7BossSite(character, codename, fell)
	rt.noteIvy3BossSite(character, codename, fell)
}

/*
================
raiseExitGate

The gate rises beside the boss's recorded fall. Each programme's
FinishKill calls it where the reference handler spawns the gate, just
before its sixty-second event timer: the gate lets the room leave
early, the timer still returns whoever stays. A kill whose owner
reported no site (no fall pose) raises nothing.
================
*/
func (rt *Runtime) raiseExitGate(world instance.ID, site simulation.Spawn, known bool) {
	if known && rt.SpawnExitGate != nil {
		rt.SpawnExitGate(world, site)
	}
}

/*
================
removeExitGates

A layer's gates go with the layer, on either close path.
================
*/
func (rt *Runtime) removeExitGates(world instance.ID) {
	if rt.RemoveExitGates != nil {
		rt.RemoveExitGates(world)
	}
}

/*
================
noteRahid6BossSite

Where Tahomet fell in its layer (NoteKillSite, just before the kill
note): the exit gate rises beside it. Records only.
================
*/
func (rt *Runtime) noteRahid6BossSite(c *enterworld.Character, codename string, fell simulation.Spawn) {
	if codename != rahid6TahometCodename || c.World == nil || c.World.InstanceReturn == nil {
		return
	}
	world := instance.ID(domain.CharacterWorldInstance(c))
	if world.Definition() != rahid6Definition {
		return
	}
	rt.rahid6.mu.Lock()
	if layer := rt.rahid6.layers[world]; layer != nil && !layer.killed && !layer.bossSiteKnown {
		layer.bossSite, layer.bossSiteKnown = fell, true
	}
	rt.rahid6.mu.Unlock()
}

/*
================
noteEasteu21BossSite

Where Kerberos fell in its layer (NoteKillSite, just before the kill
note): the exit gate rises beside it. Records only.
================
*/
func (rt *Runtime) noteEasteu21BossSite(c *enterworld.Character, codename string, fell simulation.Spawn) {
	if codename != easteu21BossCodename || c.World == nil || c.World.InstanceReturn == nil {
		return
	}
	world := instance.ID(domain.CharacterWorldInstance(c))
	if world.Definition() != easteu21Definition {
		return
	}
	rt.easteu21.mu.Lock()
	if layer := rt.easteu21.layers[world]; layer != nil && !layer.killed && !layer.bossSiteKnown {
		layer.bossSite, layer.bossSiteKnown = fell, true
	}
	rt.easteu21.mu.Unlock()
}

/*
================
noteGoria7BossSite

Where the lancer fell in its layer (NoteKillSite, just before the kill
note): the exit gate rises beside it. Records only.
================
*/
func (rt *Runtime) noteGoria7BossSite(c *enterworld.Character, codename string, fell simulation.Spawn) {
	if codename != goria7BossCodename || c.World == nil || c.World.InstanceReturn == nil {
		return
	}
	world := instance.ID(domain.CharacterWorldInstance(c))
	if world.Definition() != goria7Definition {
		return
	}
	rt.goria7.mu.Lock()
	if layer := rt.goria7.layers[world]; layer != nil && !layer.killed && !layer.bossSiteKnown {
		layer.bossSite, layer.bossSiteKnown = fell, true
	}
	rt.goria7.mu.Unlock()
}

/*
================
noteIvy3BossSite

Where Ivy fell in her layer (NoteKillSite, just before the kill note):
the exit gate rises beside it. Records only.
================
*/
func (rt *Runtime) noteIvy3BossSite(c *enterworld.Character, codename string, fell simulation.Spawn) {
	if codename != ivy3BossCodename || c.World == nil || c.World.InstanceReturn == nil {
		return
	}
	world := instance.ID(domain.CharacterWorldInstance(c))
	if world.Definition() != ivy3Definition {
		return
	}
	rt.ivy3.mu.Lock()
	if layer := rt.ivy3.layers[world]; layer != nil && !layer.bossGone && !layer.bossSiteKnown {
		layer.bossSite, layer.bossSiteKnown = fell, true
	}
	rt.ivy3.mu.Unlock()
}
