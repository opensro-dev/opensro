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
rideStateRoute

One 0xB4B5 ride state travelling under its rider, away from the
shared publication it was found in.
================
*/
type rideStateRoute struct {
	gid    uint32
	frames []simulation.Frame
}

/*
================
splitRideStates

Partition a shared publication into the frames every viewer of its
source may take and the 0xB4B5 ride states inside it. A ride state
names its rider and the client applies it with no absent-actor arm
(ride_state_scope_test.go), so each ride state travels under its
payload rider, keyed exactly as the action lane's frameSourceGID keys
it: the tick router then admits it only to viewers gated on the rider,
never to viewers gated on the shared source alone. A publication whose
source already is the rider gets its ride states back under the same
key and the tick's coalescing folds them in again. A ride state whose
payload does not decode stays with the shared frames, as the action
lane's fallback keeps it under the caller's source.
================
*/
func splitRideStates(in []simulation.Frame) ([]simulation.Frame, []rideStateRoute) {
	shared := make([]simulation.Frame, 0, len(in))
	var rides []rideStateRoute
	for _, f := range in {
		rider, ok := uint32(0), false
		if f.Opcode == wire.OpCosRideState {
			rider, ok = wire.CosRideStateRider(f.Payload)
		}
		if !ok {
			shared = append(shared, f)
			continue
		}
		if len(rides) == 0 || rides[len(rides)-1].gid != rider {
			rides = append(rides, rideStateRoute{gid: rider})
		}
		rides[len(rides)-1].frames = append(rides[len(rides)-1].frames, f)
	}
	return shared, rides
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
		shared, rides := splitRideStates(convert(frames.public))
		for _, ride := range rides {
			out = append(out, simulation.DivisionFrames{
				DivisionID:        division,
				SourceGID:         ride.gid,
				ExceptCharacterID: except,
				Frames:            ride.frames,
			})
		}
		if len(shared) > 0 {
			out = append(out, simulation.DivisionFrames{
				DivisionID:        division,
				SourceGID:         frames.sourceGID,
				ExceptCharacterID: except,
				Frames:            shared,
			})
		}
	}
	if len(frames.actor) > 0 {
		out = append(out, simulation.DivisionFrames{DivisionID: division, OnlyCharacterID: actorID, Frames: convert(frames.actor)})
	}
	for _, p := range frames.sources {
		out = append(out, simulation.DivisionFrames{DivisionID: division, OnlyCharacterID: p.id, Frames: convert(p.frames)})
	}
	return out
}
