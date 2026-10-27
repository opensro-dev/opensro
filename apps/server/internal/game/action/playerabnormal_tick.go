/*
===========================================================================
playerabnormal_tick.go - simulation ticking and recipient routing for player statuses

The status owner resolves actors before its write; publication follows commit.
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
advancePlayerAbnormals

Run the native abnormal update for every retained block without holding the registry lock.
================
*/
func (rt *Runtime) advancePlayerAbnormals(now int64) []simulation.DivisionFrames {
	var out []simulation.DivisionFrames
	for _, entry := range rt.playerAbnormalCandidates() {
		out = append(out, rt.advancePlayerAbnormal(entry.division, entry.name, now)...)
	}
	return out
}

/*
================
advancePlayerAbnormal

Resolve source facts before the write, commit HP and status together, then
publish. A vanished source must not turn a tick into a recursive store read.
================
*/
func (rt *Runtime) advancePlayerAbnormal(division, name string, now int64) []simulation.DivisionFrames {
	unlock := rt.lockDivision(division)
	defer unlock()
	c := rt.findCharacter(division, name)
	if c == nil {
		rt.storePlayerAbnormal(division, name, nil)
		return nil
	}
	o := rt.newPlayerAbnormalOwner(division, c, now)
	o.sources = rt.capturePlayerAbnormalSources(division, c, o.block, nil)
	o.bond = rt.planPetBond(division, c, now)
	committed := rt.deps.UpdateDeferred(c, "player-abnormal", func() bool {
		if c.DeletePending {
			return false
		}
		if !enterworld.CharacterAlive(c) {
			o.changed = o.block.ClearAll(o)
		} else if result := o.block.Update(o, now); result.Changed {
			o.changed = true
		}
		o.commit()
		if o.statsChanged {
			hp, mp := rt.clampStoredGaugeToKeeper(division, c)
			o.hpChanged = o.hpChanged || hp
			o.mpChanged = o.mpChanged || mp
		}
		return o.changed || o.hpChanged || o.mpChanged || o.statsChanged || o.halted
	})
	if !committed {
		return nil
	}
	if o.fatal {
		rt.bindResidentRegion(simulation.WorldKey(division, c.Name), now)
	}
	frames := rt.playerAbnormalPublication(division, c, o)
	return playerAbnormalDivisionFrames(division, c.ID, frames)
}

/*
================
playerAbnormalDivisionFrames

Keep shared, victim-only and credited-source publications in separate routes.
================
*/
func playerAbnormalDivisionFrames(division string, actorID int64, frames playerAbnormalFrames) []simulation.DivisionFrames {
	var out []simulation.DivisionFrames
	convert := func(in []wire.Frame) []simulation.Frame {
		fs := make([]simulation.Frame, 0, len(in))
		for _, f := range in {
			fs = append(fs, simulation.Frame{Opcode: f.Opcode, Payload: f.Payload, Current: f.Current, Scope: f.Scope})
		}
		return fs
	}
	if len(frames.public) > 0 {
		var except int64
		if frames.exceptActor {
			except = actorID
		}
		out = append(out, simulation.DivisionFrames{
			DivisionID:        division,
			SourceGID:         frames.sourceGID,
			ExceptCharacterID: except,
			Frames:            convert(frames.public),
		})
	}
	if len(frames.actor) > 0 {
		out = append(out, simulation.DivisionFrames{DivisionID: division, OnlyCharacterID: actorID, Frames: convert(frames.actor)})
	}
	for _, p := range frames.sources {
		out = append(out, simulation.DivisionFrames{DivisionID: division, OnlyCharacterID: p.id, Frames: convert(p.frames)})
	}
	return out
}
