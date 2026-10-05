/*
===========================================================================

pkrecord.go - the PK record's clocks: the daily reset and total decay

Two clocks run on a player's PK record (pk/record.go). The local day's
rollover empties the daily count (CGame_TickPlayers -> CGObjPC_ResetDailyPK
4EB200), and the penalty keeper drops the total by one every 48 h of wall
time once the penalty is gone (CTJ_PenaltyKeeper, 652080; offline time
counts, 651D30). Entry also repairs a positive total that has lost its
keeper (4E1120). Every owner with a record is checked each action tick;
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

Entry: the decay elapsed offline, a stale daily count, and 4E1120's
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
================
*/
func (rt *Runtime) advancePKRecords() {
	now := rt.Now()
	for _, key := range rt.pkOwners.keys() {
		c := rt.findCharacter(key.division, key.name)
		view := rt.characterSnapshot(key.division, c)
		if view == nil || view.PK == nil || view.PK.TotalDecayAt == 0 && view.PK.DailyCount == 0 {
			rt.pkOwners.forget(key)
			continue
		}
		if pk.ClockDue(view, now) {
			rt.tickPKRecord(key.division, c, false)
		}
	}
}

/*
================
tickPKRecord
================
*/
func (rt *Runtime) tickPKRecord(division string, c *enterworld.Character, entry bool) {
	now := rt.Now()
	var actor, public []wire.Frame
	rt.deps.Update(c, "pk-record-clock", func() bool {
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
