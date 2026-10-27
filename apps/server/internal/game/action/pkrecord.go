/*
===========================================================================

pkrecord.go - the PK record's clocks: the daily reset and total decay

Two clocks run on a player's PK record (pk/record.go). The local day's
rollover empties the daily count (natively on the players tick), and the
penalty keeper drops the total by one every 48 h of wall
time once the penalty is gone (the native penalty keeper; offline time
counts). Entry also repairs a positive total that has lost its
keeper. Every owner with a record is checked each action tick;
a change commits through the character door and reaches the player as its
v1.150 live update.

===========================================================================
*/

package action

import (
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/pk"
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
================
*/
func (rt *Runtime) advancePKRecords() {
	now := rt.Now()
	for _, key := range rt.pkOwners.keys() {
		c := rt.findCharacter(key.division, key.name)
		running, due := false, false
		if c != nil {
			rt.deps.Read(key.division, func() {
				r := c.PK
				running = r != nil && (r.TotalDecayAt != 0 || r.DailyCount != 0)
				due = running && pk.ClockDue(c, now)
			})
		}
		if !running {
			rt.pkOwners.forget(key)
			continue
		}
		if due {
			rt.tickPKRecord(key.division, c, false)
		}
	}
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
