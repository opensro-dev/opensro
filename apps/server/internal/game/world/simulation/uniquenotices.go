/*
===========================================================================

uniquenotices.go - the shard's unique appearance and death notices

A rarity-3 monster's spawn and death are told to the whole division
(0x3058, kinds 5 and 6). Each population queues its own notices; the tick
drains them once per division. The overworld population always speaks.
An instance layer speaks only when its world announces its uniques
(AnnounceUniqueWorld): the v1.188 reference server asks each world
(IsControlNotifySpawnUniqueMonsterMsg), the Roc's two world classes answer
no control, and the data-driven worlds read a per-world switch the port
does not model, so every other layer keeps its notices to itself.

===========================================================================
*/

package simulation

import (
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/instance"
)

// The 0x3058 kinds: a unique appeared, a unique fell.
const (
	uniqueNoticeAppear = 5
	uniqueNoticeDeath  = 6
)

/*
================
uniqueNotice

v1.150 74d451 registers 3058 -> 7508c0. Subtypes 5/6 read a reference ID,
followed for death by a length-prefixed narrow killer name.
================
*/
func uniqueNotice(kind uint8, ref uint32, killer string) Frame {
	w := wire.NewWriter(32).U8(kind).U32(ref)
	if kind == uniqueNoticeDeath {
		w.U16(uint16(len([]byte(killer)))).Bytes([]byte(killer))
	}
	return Frame{Opcode: 0x3058, Payload: w.Payload()}
}

/*
================
RecordUniqueKiller

Attaches the committed combat kill owner once. Visibility changes and
subsequent hits on zero HP cannot create another announcement. It reports
the unique's reference id when this call announced the kill.
================
*/
func (s *MonsterState) RecordUniqueKiller(division string, gid uint32, name string) (uint32, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.populationForObject(division, gid)
	instance, ok := state.instances.lookup(gid)
	if !ok || instance.Arena || instance.CurrentHP != 0 || instance.Rarity()&15 != 3 || state.uniqueDeaths[gid] {
		return 0, false
	}
	if state.uniqueDeaths == nil {
		state.uniqueDeaths = map[uint32]bool{}
	}
	state.uniqueDeaths[gid] = true
	if name == "" {
		name = "???"
	}
	state.uniqueNotices = append(state.uniqueNotices, uniqueNotice(uniqueNoticeDeath, instance.Ref.RefObjID, name))
	return instance.Ref.RefObjID, true
}

/*
================
AnnounceUniqueWorld

Lets an instance world's layers tell the division of their uniques (on),
or keeps them silent again (off, the default for every world).
================
*/
func (s *MonsterState) AnnounceUniqueWorld(world instance.DefinitionID, on bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !on {
		delete(s.uniqueNoticeWorlds, world)
		return
	}
	if s.uniqueNoticeWorlds == nil {
		s.uniqueNoticeWorlds = map[instance.DefinitionID]bool{}
	}
	s.uniqueNoticeWorlds[world] = true
}

/*
================
DrainUniqueNotices

One tick-owned drain fans events out to the division, including distant
viewers. Bootstrap/reference reads never replay appearance notifications.
The overworld's notices come first, then each announcing layer's, in the
population keys' order.
================
*/
func (s *MonsterState) DrainUniqueNotices(division string) []Frame {
	s.mu.Lock()
	defer s.mu.Unlock()
	var frames []Frame
	if state := s.divs[division]; state != nil {
		frames, state.uniqueNotices = state.uniqueNotices, nil
	}
	if len(s.uniqueNoticeWorlds) == 0 {
		return frames
	}
	for _, key := range s.populationKeys() {
		if key.division != division || !s.uniqueNoticeWorlds[key.lease.ID.Definition()] {
			continue
		}
		if state := s.worldPopulations[key]; state != nil && len(state.uniqueNotices) != 0 {
			frames = append(frames, state.uniqueNotices...)
			state.uniqueNotices = nil
		}
	}
	return frames
}
