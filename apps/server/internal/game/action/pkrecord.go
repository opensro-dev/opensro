/*
===========================================================================

pkrecord.go - the PK record's clocks: the daily reset, total decay and drain

Three clocks run on a player's PK record (pk/record.go). The local day's
rollover empties the daily count (natively on the players tick), the
penalty keeper drops the total by one every 48 h of wall
time once the penalty is gone (the native penalty keeper; offline time
counts), and a living murderer loses HP on its own period
(murdererdrain.go, driven from this sweep). Entry also repairs a positive
total that has lost its keeper. Every owner with a record is checked each
action tick; a change commits through the character door and reaches the
player as its v1.150 live update.

===========================================================================
*/

package action

import (
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/pk"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
restorePKRecord

Entry: the decay elapsed offline, a stale daily count, and the native load's
keeper repair. The entry snapshot already carried the old record, so the
corrections go out as live updates.
================
*/
func (rt *Runtime) restorePKRecord(division, name string) {
	c := rt.findCharacter(division, name)
	if c == nil || c.PK == nil {
		return
	}
	rt.pkOwners.track(division, name)
	rt.tickPKRecord(division, c, true)
}

/*
================
advancePKRecords

The clock check reads only the PK record, so it reads it in place under
the read door instead of cloning the whole character (#1820 K): almost
every owner is not due, and the clone was the sweep's entire cost.

A positive penalty keeps an owner tracked even when both keeper clocks are
idle, because the murderer drain (murdererdrain.go) runs from the same
sweep. The drain also needs a living world resident: an offline record
keeps its keeper clocks but is not bled, and a corpse's clock is forgotten
so a revival starts a fresh full period. The returned frames are the
drain's; the keeper's own changes are pushed by the door below.
================
*/
func (rt *Runtime) advancePKRecords() []simulation.DivisionFrames {
	now := rt.Now()
	nowMs := now.UnixMilli()
	var out []simulation.DivisionFrames
	for _, key := range rt.pkOwners.keys() {
		c := rt.findCharacter(key.division, key.name)
		running, due, drainable := false, false, false
		if c != nil {
			rt.deps.Read(key.division, func() {
				r := c.PK
				running = r != nil && (r.TotalDecayAt != 0 || r.DailyCount != 0 || r.Penalty != 0)
				due = running && pk.ClockDue(c, now)
				drainable = murderer(c) && enterworld.CharacterAlive(c)
			})
		}
		if !running {
			rt.pkOwners.forget(key)
			rt.forgetMurdererDrain(key)
			continue
		}
		if due {
			rt.tickPKRecord(key.division, c, false)
		}
		if !drainable {
			rt.forgetMurdererDrain(key)
			continue
		}
		if !rt.playerOnline(key) {
			rt.forgetMurdererDrain(key)
			continue
		}
		out = append(out, rt.drainMurderer(key, nowMs)...)
	}
	return out
}

/*
================
tickPKRecord

The sweep commits through the tick-batched door; world entry keeps the
immediate one, since a handler answers only after its commit.
================
*/
func (rt *Runtime) tickPKRecord(division string, c *enterworld.Character, entry bool) {
	now := rt.Now()
	var actor, public []wire.Frame
	door := rt.deps.UpdateDeferred
	if entry {
		door = rt.deps.Update
	}
	door(c, "pk-record-clock", func() bool {
		if c.PK == nil {
			return false
		}
		before := c.PVPState()
		changed := pk.DecayTotal(c, now) | pk.ResetDailyIfStale(c, now)
		if entry {
			changed |= pk.RepairKeeper(c, now)
		}
		if changed == 0 {
			return false
		}
		actor = pkRecordFrames(c, changed)
		if before != c.PVPState() {
			public = append(public, playerPVPStateFrame(c))
		}
		return true
	})
	if len(public) != 0 {
		rt.publishBodyStatus(division, c.Name, public)
	}
	if len(actor) != 0 && rt.PushCharacterFrames != nil {
		rt.PushCharacterFrames(division, c.Name, actor)
	}
}

/*
================
notePKRecord

A record that just changed keeps its clocks running.
================
*/
func (rt *Runtime) notePKRecord(division string, c *enterworld.Character) {
	if c.PK != nil {
		rt.pkOwners.track(division, c.Name)
	}
}
