/*
===========================================================================

abnormal_sources.go - source facts admitted before a status-effect write.

Status callbacks execute under the authority store's write lock. Resolve
cross-character and monster reads before that transaction; callbacks receive
values and never reenter the store. The division owner serializes game actions
across this read/commit boundary.

===========================================================================
*/

package action

import (
	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/enterworld"
)

/*
================
abnormalSourceState

Existence and life state remain distinct: a dead source still exists and
latches the status's source-death flag; a vanished source receives no credit.
================
*/
type abnormalSourceState struct {
	exists bool
	dead   bool
	// killer is the source as a death's killer (pkdeath.go), captured
	// here because the fatal status hit runs inside the victim's write.
	killer deathKiller
	// kill carries external relation facts only for player recipients;
	// capturePlayerAbnormalSources prepares it before their store write.
	kill playerKill
}

/*
================
captureAbnormalSources

Call while owning the division, before entering a character write transaction.
Read each active source once so every status in this tick sees the same facts.

Player victims take this full capture: capturePlayerAbnormalSources
classifies the killer copy before the write, and the fatal transition
settles from it inside the write, so the copy cannot be taken lazily at
the death - the write door is already held there and a read would
reenter it. Owners that never read a killer (a COS owner's callbacks, the
monster context) capture through captureAbnormalSourcesLife instead.
================
*/
func (rt *Runtime) captureAbnormalSources(division string, block *abnormal.Block, records []abnormal.Record) map[uint32]abnormalSourceState {
	return rt.captureAbnormalSourcesWith(division, block, records, rt.captureAbnormalSource)
}

/*
================
captureAbnormalSourcesLife

The life-facts capture for owners that never read a killer; same map
shape, no detached records. See captureAbnormalSourceLife for the audit.
================
*/
func (rt *Runtime) captureAbnormalSourcesLife(division string, block *abnormal.Block, records []abnormal.Record) map[uint32]abnormalSourceState {
	return rt.captureAbnormalSourcesWith(division, block, records, rt.captureAbnormalSourceLife)
}

/*
================
captureAbnormalSourcesWith

One pass over the block's slots and the tick's records, deduplicated by
source gid, so every status in this tick sees the same facts.
================
*/
func (rt *Runtime) captureAbnormalSourcesWith(division string, block *abnormal.Block, records []abnormal.Record, capture func(string, uint32) abnormalSourceState) map[uint32]abnormalSourceState {
	sources := make(map[uint32]abnormalSourceState)
	for _, slot := range block.Slots {
		if !slot.Active || slot.SourceGID == 0 {
			continue
		}
		if _, captured := sources[slot.SourceGID]; captured {
			continue
		}
		sources[slot.SourceGID] = capture(division, slot.SourceGID)
	}
	for _, record := range records {
		if record.SourceGID == 0 {
			continue
		}
		if _, captured := sources[record.SourceGID]; !captured {
			sources[record.SourceGID] = capture(division, record.SourceGID)
		}
	}
	return sources
}

/*
================
captureAbnormalSource

The source lookup may take the authority read lock, including when a vanished
monster falls through to the character registry. Never call from a write.
================
*/
func (rt *Runtime) captureAbnormalSource(division string, gid uint32) abnormalSourceState {
	if rt.Monsters != nil {
		if instance, exists := rt.Monsters.Get(division, gid); exists {
			return abnormalSourceState{exists: true, dead: instance.CurrentHP == 0, killer: deathKiller{monster: &instance}}
		}
	}
	character := rt.findCharacterByGid(division, gid)
	if character == nil {
		owner := rt.characterByCosGID(division, gid)
		if owner == nil {
			return abnormalSourceState{}
		}
		snapshot := rt.characterSnapshot(division, owner)
		if snapshot == nil {
			return abnormalSourceState{}
		}
		pet := snapshot.CompanionByGID(gid)
		if pet == nil {
			return abnormalSourceState{}
		}
		level := pet.Level
		if level == 0 {
			if ref, ok := rt.cosReference(pet); ok {
				level = ref.Level
			}
		}
		// The reference server attributes a COS kill to its owner but scores the COS level.
		return abnormalSourceState{exists: true, dead: pet.CurrentHP == 0,
			killer: deathKiller{player: snapshot, strikerLevel: int64(level)}}
	}
	snapshot := rt.characterSnapshot(division, character)
	return abnormalSourceState{exists: true, dead: snapshot == nil || !enterworld.CharacterAlive(snapshot), killer: deathKiller{player: snapshot}}
}

/*
================
captureAbnormalSourceLife

The life facts of captureAbnormalSource without the killer copy, for the
owners that never read one. A COS owner's callbacks weigh existence,
source death and whether the source is a monster (the shield test); the
monster owner's context weighs existence and death alone. The killer
snapshot - and with it the COS source's striker level - is consumed only
on the player lane, where captureAbnormalSources keeps it eager for the
reason its banner gives. The monster arm needs no change: it never
copied. Like the full capture, never call from a write.
================
*/
func (rt *Runtime) captureAbnormalSourceLife(division string, gid uint32) abnormalSourceState {
	if rt.Monsters != nil {
		if instance, exists := rt.Monsters.Get(division, gid); exists {
			return abnormalSourceState{exists: true, dead: instance.CurrentHP == 0, killer: deathKiller{monster: &instance}}
		}
	}
	character := rt.findCharacterByGid(division, gid)
	if character == nil {
		owner := rt.characterByCosGID(division, gid)
		if owner == nil {
			return abnormalSourceState{}
		}
		exists, dead := false, false
		rt.deps.Read(division, func() {
			if pet := owner.CompanionByGID(gid); pet != nil {
				exists, dead = true, pet.CurrentHP == 0
			}
		})
		if !exists {
			return abnormalSourceState{}
		}
		return abnormalSourceState{exists: true, dead: dead}
	}
	alive, _, _ := rt.characterLifeState(division, character)
	return abnormalSourceState{exists: true, dead: !alive}
}
