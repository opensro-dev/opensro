package simulation

import "opensro.online/server/internal/game/item/wire"

// v1.150 74d451 registers 3058 -> 7508c0. Subtypes 5/6 read a
// reference ID, followed for death by a length-prefixed narrow killer name.
func uniqueNotice(kind uint8, ref uint32, killer string) Frame {
	w := wire.NewWriter(32).U8(kind).U32(ref)
	if kind == 6 {
		w.U16(uint16(len([]byte(killer)))).Bytes([]byte(killer))
	}
	return Frame{Opcode: 0x3058, Payload: w.Payload()}
}

// RecordUniqueKiller attaches the committed combat kill owner once. Visibility
// changes and subsequent hits on zero HP cannot create another announcement.
// It reports the unique's reference id when this call announced the kill.
func (s *MonsterState) RecordUniqueKiller(division string, gid uint32, name string) (uint32, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.populationForObject(division, gid)
	instance, ok := state.instances.lookup(gid)
	if !ok || instance.CurrentHP != 0 || instance.Rarity()&15 != 3 || state.uniqueDeaths[gid] {
		return 0, false
	}
	if state.uniqueDeaths == nil {
		state.uniqueDeaths = map[uint32]bool{}
	}
	state.uniqueDeaths[gid] = true
	if name == "" {
		name = "???"
	}
	state.uniqueNotices = append(state.uniqueNotices, uniqueNotice(6, instance.Ref.RefObjID, name))
	return instance.Ref.RefObjID, true
}

// One tick-owned drain fans events out to the division, including distant
// viewers. Bootstrap/reference reads never replay appearance notifications.
func (s *MonsterState) DrainUniqueNotices(division string) []Frame {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.divs[division]
	if state == nil {
		return nil
	}
	frames := state.uniqueNotices
	state.uniqueNotices = nil
	return frames
}
