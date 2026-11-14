/*
===========================================================================

monster_vitals.go - the cause words behind a monster's vitals publication

The original server publishes a monster's vitals once per tick, and only
when they moved. Every HP change ORs its cause into the object's cause
word (+0xA0E: CGObjChar_ApplyHealthAndManaOffset 4A88D5 when the HP or
MP changed; AbnormalState_SendVitalInfo30D2 ORs 0x100 at a status change,
4A5C73). CGObjChar_OnTick (4A8A0E-4A8A24) then calls the publisher while
the word is non-zero and clears it. The publisher,
CGObjChar_PublishChangedVitals (4A8510), hands the change to the relations
observing the monster with the vitals bit (mask 2, 4A85CB), and
CGObjPC_SendObjectSupplementTo (4A73F0) writes each of their players a
private 0x3057 (v1.150 0x33A6): the gid, the cause word, then the body
sub_4C3E00 writes for flags 7 with MP dropped - flags 5, the current HP
and the status block.

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
	// VitalsSourceStatus is the cause a status change ORs in
	// (AbnormalState_SendVitalInfo30D2, 4A5C73).
	VitalsSourceStatus VitalsSourceFlags = 0x0100
	// VitalsSourceStatusDamage is an abnormal state's periodic damage,
	// the cause the port's status ticks publish (playerabnormal.go
	// abnormalStatusVitalsSource). It lies inside the client's delta set
	// (0x32, client 77A1E9), so the client applies it as a difference.
	VitalsSourceStatusDamage VitalsSourceFlags = 0x0002
)

// monsterVitalsFlags is the private body's flags byte: HP (1) and the
// status block (4). CGObjPC_SendObjectSupplementTo asks for flags 7
// (4A7494) and the monster's body writer sub_4C3E00 clears MP (4C3E00).
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
calls only for a change that took place: the original ORs the cause in
only when the HP or MP moved (4A88C6-4A88D9), so a hit that took nothing
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
drain to dst, in gid order, and forget the changes: CGObjChar_OnTick
clears the cause word once the publisher ran (4A8A24). A monster that left
the population meanwhile publishes nothing.
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
