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
}

/*
================
captureAbnormalSources

Call while owning the division, before entering a character write transaction.
Read each active source once so every status in this tick sees the same facts.
================
*/
func (rt *Runtime) captureAbnormalSources(division string, block *abnormal.Block, records []abnormal.Record) map[uint32]abnormalSourceState {
	sources := make(map[uint32]abnormalSourceState)
	for _, slot := range block.Slots {
		if !slot.Active || slot.SourceGID == 0 {
			continue
		}
		if _, captured := sources[slot.SourceGID]; captured {
			continue
		}
		sources[slot.SourceGID] = rt.captureAbnormalSource(division, slot.SourceGID)
	}
	for _, record := range records {
		if record.SourceGID == 0 {
			continue
		}
		if _, captured := sources[record.SourceGID]; !captured {
			sources[record.SourceGID] = rt.captureAbnormalSource(division, record.SourceGID)
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
		// 5C7000 attributes a COS kill to its owner but scores the COS level.
		return abnormalSourceState{exists: true, dead: pet.CurrentHP == 0,
			killer: deathKiller{player: snapshot, strikerLevel: int64(level)}}
	}
	snapshot := rt.characterSnapshot(division, character)
	return abnormalSourceState{exists: true, dead: snapshot == nil || !enterworld.CharacterAlive(snapshot), killer: deathKiller{player: snapshot}}
}
