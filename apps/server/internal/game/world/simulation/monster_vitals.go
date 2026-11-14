/*
===========================================================================

monster_vitals.go - the cause words behind a monster's vitals publication

The reference server publishes a monster's vitals once per tick, and only
when they moved. Every HP or MP change ORs its cause into the monster's
cause word, and a status change ORs 0x100. On its tick the monster runs
its vitals publisher while the word is non-zero, then clears the word.
The publisher broadcasts the status alone and hands HP and status to the
relations that hold the monster with the vitals bit: each of their players
receives a private 0x33A6 with the gid, the cause word and a flags-5 body
(the current HP and the status block; the relation asks for flags 7 and
the monster's body writer drops MP).

MonsterState owns the cause words: the hit and abnormal commits record
them here, and DrainChangedVitals hands the action tick one publication
per changed monster and forgets them. Which players hold a monster is the
action lane's selection plane (action/monstervitals.go).

===========================================================================
*/

package simulation

import (
	"sort"

	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
)

// Vitals causes beside the ones gameready.go names.
const (
	// VitalsSourceStatus is the cause the reference server ORs in when a
	// status changes.
	VitalsSourceStatus VitalsSourceFlags = 0x0100
	// VitalsSourceStatusDamage is an abnormal state's periodic damage,
	// the cause the port's status ticks publish (playerabnormal.go
	// abnormalStatusVitalsSource). It lies inside the client's delta set
	// (0x32, client 77A1E9), so the client applies it as a difference.
	VitalsSourceStatusDamage VitalsSourceFlags = 0x0002
)

// monsterVitalsFlags is the private body's flags byte: HP (1) and the
// status block (4). The relation asks for flags 7, and the reference
// server's monster body writer clears MP (2).
const monsterVitalsFlags uint8 = 0x05

// monsterVitalsPayloadBytes is the body without status levels: gid,
// cause, flags, HP and the status mask.
const monsterVitalsPayloadBytes = 4 + 2 + 1 + 4 + 4

/*
================
monsterVitalsKey
================
*/
type monsterVitalsKey struct {
	division string
	gid      uint32
}

/*
================
MonsterVitals

One monster's changed vitals for one tick: the division and gid name the
monster, Payload is the private 0x33A6 its holders receive.
================
*/
type MonsterVitals struct {
	DivisionID string
	Gid        uint32
	Payload    []byte
}

/*
================
noteVitalsLocked

Record that gid's vitals changed for cause. The caller holds s.mu and
calls only for a change that took place: the reference server ORs the
cause in only when the HP or MP moved, so a hit that took nothing
publishes nothing.
================
*/
func (s *MonsterState) noteVitalsLocked(division string, gid uint32, cause VitalsSourceFlags) {
	if s.vitals == nil {
		s.vitals = make(map[monsterVitalsKey]VitalsSourceFlags)
	}
	s.vitals[monsterVitalsKey{division: division, gid: gid}] |= cause
}

/*
================
DrainChangedVitals

Append one publication per monster whose vitals changed since the last
drain to dst, in gid order, and forget the changes: the reference server's
tick clears the cause word once the publisher ran. A monster that left the
population meanwhile publishes nothing.
================
*/
func (s *MonsterState) DrainChangedVitals(dst []MonsterVitals) []MonsterVitals {
	s.mu.Lock()
	defer s.mu.Unlock()
	start := len(dst)
	for key, cause := range s.vitals {
		state := s.populationForObject(key.division, key.gid)
		if state == nil {
			continue
		}
		instance, ok := state.instances.lookup(key.gid)
		if !ok {
			continue
		}
		dst = append(dst, MonsterVitals{DivisionID: key.division, Gid: key.gid, Payload: MonsterVitalsPayload(instance, cause)})
	}
	clear(s.vitals)
	added := dst[start:]
	sort.Slice(added, func(i, j int) bool {
		if added[i].Gid != added[j].Gid {
			return added[i].Gid < added[j].Gid
		}
		return added[i].DivisionID < added[j].DivisionID
	})
	return dst
}

/*
================
MonsterVitalsPayload

The private 0x33A6 a monster's holder receives: {gid, cause, flags 5, HP,
status mask, grade bytes}.
================
*/
func MonsterVitalsPayload(instance monster.Instance, cause VitalsSourceFlags) []byte {
	w := wire.NewWriter(monsterVitalsPayloadBytes).U32(instance.Gid).U16(uint16(cause)).U8(monsterVitalsFlags).U32(instance.CurrentHP)
	writeMonsterStatus(w, instance)
	return w.Payload()
}

/*
================
writeMonsterStatus

The status block of a monster's vitals body: the mask, then one grade byte
per graded bit in ascending bit order over 017FCFC0. A dead monster
carries none.
================
*/
func writeMonsterStatus(w *wire.Writer, instance monster.Instance) {
	var mask uint32
	var grades []uint8
	if instance.CurrentHP > 0 && instance.Abnormal != nil {
		mask, grades = instance.Abnormal.Mask, instance.Abnormal.Grades()
	}
	w.U32(mask)
	for _, g := range grades {
		w.U8(g)
	}
}
