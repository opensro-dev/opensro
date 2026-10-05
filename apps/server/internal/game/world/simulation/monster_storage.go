/*
===========================================================================

monster_storage.go - compact resident storage for monsters

===========================================================================
*/

package simulation

import (
	"iter"
	"opensro.online/server/internal/game/abnormal"
	"unique"

	"opensro.online/server/internal/game/world/monster"
)

/*
==================
residentMonster

Resident storage shares immutable catalogue values, never mutable actor state.
Only MonsterState accesses this under its population mutex. Public reads
reconstruct the existing comparable, detached Instance value.
==================
*/
type residentMonster struct {
	conditionalUsed     uint8
	ref                 unique.Handle[monster.MonsterRef]
	nest                unique.Handle[monster.NestRow]
	help                monster.HelpInbox
	motion              monster.MotionHold
	spawn               monster.SpawnPoint
	nestDetached        bool
	spawnHeading        uint16
	tradeVariant        uint8
	currentHP           uint32
	damageSinceSummon   uint32
	lastSummonCommandMs uint32
	opponents           [2]monster.Opponent
	summonActionUntilMs int64
	summonerGID         uint32
	summonSightRange    float64
	summonerFollowRange float64
}

/*
================
monsterStorage

Cold records omit transient effects; active effect owners keep actors resident.
================
*/
type monsterStorage struct {
	linkedEffects map[uint32]*monster.EffectSnapshot
	selfEffects   map[uint32]monster.SelfEffects
	hot           map[uint32]residentMonster
	abnormal      map[uint32]*abnormal.Block // immutable non-nil blocks
	cold          map[uint32]archivedMonster
	archive       *monsterArchive
}

/*
================
newMonsterStorage

Detach mutable actor state from the population's initial catalog projection.
================
*/
func newMonsterStorage(rows map[uint32]monster.Instance) monsterStorage {
	s := monsterStorage{hot: make(map[uint32]residentMonster, len(rows))}
	for gid, row := range rows {
		s.set(gid, row)
	}
	return s
}

/*
================
set

Publish scalar state and immutable effect snapshots together under the owner lock.
================
*/
func (s *monsterStorage) set(gid uint32, row monster.Instance) {
	if row.LinkedEffects != nil {
		if s.linkedEffects == nil {
			s.linkedEffects = make(map[uint32]*monster.EffectSnapshot)
		}
		s.linkedEffects[gid] = row.LinkedEffects
	} else {
		delete(s.linkedEffects, gid)
	}
	if row.SelfEffects != (monster.SelfEffects{}) {
		if s.selfEffects == nil {
			s.selfEffects = make(map[uint32]monster.SelfEffects)
		}
		s.selfEffects[gid] = row.SelfEffects
	} else {
		delete(s.selfEffects, gid)
	}
	if s.hot == nil {
		s.hot = make(map[uint32]residentMonster)
	}
	s.removeCold(gid)
	if row.Abnormal != nil {
		if s.abnormal == nil {
			s.abnormal = make(map[uint32]*abnormal.Block)
		}
		s.abnormal[gid] = row.Abnormal
	} else {
		delete(s.abnormal, gid)
	}
	old, exists := s.hot[gid]
	ref, nest := old.ref, old.nest
	if !exists || ref.Value() != row.Ref {
		ref = unique.Make(row.Ref)
	}
	if !exists || nest.Value() != row.Nest {
		nest = unique.Make(row.Nest)
	}
	s.hot[gid] = residentMonster{row.ConditionalUsed, ref, nest, row.Help, row.Motion, row.Spawn,
		row.NestDetached, row.SpawnHeading, row.TradeVariant, row.CurrentHP, row.DamageSinceSummon, row.LastSummonCommandMs,
		row.Opponents, row.SummonActionUntilMs, row.SummonerGID,
		row.SummonSightRange, row.SummonerFollowRange}
}

/*
================
value

Expand shared catalog handles without exposing mutable population storage.
================
*/
func (r residentMonster) value(gid uint32) monster.Instance {
	return monster.Instance{ConditionalUsed: r.conditionalUsed, Gid: gid, Ref: r.ref.Value(), Nest: r.nest.Value(), Help: r.help,
		Motion: r.motion, Spawn: r.spawn, NestDetached: r.nestDetached,
		SpawnHeading: r.spawnHeading, TradeVariant: r.tradeVariant, CurrentHP: r.currentHP, DamageSinceSummon: r.damageSinceSummon,
		LastSummonCommandMs: r.lastSummonCommandMs,
		Opponents:           r.opponents, SummonActionUntilMs: r.summonActionUntilMs, SummonerGID: r.summonerGID,
		SummonSightRange: r.summonSightRange, SummonerFollowRange: r.summonerFollowRange}
}

/*
================
hotValue

Join sparse transient state onto a detached resident value.
================
*/
func (s *monsterStorage) hotValue(gid uint32, r residentMonster) monster.Instance {
	row := r.value(gid)
	row.LinkedEffects = s.linkedEffects[gid]
	row.Abnormal = s.abnormal[gid]
	row.SelfEffects = s.selfEffects[gid]
	return row
}

/*
================
lookup

Read either residency tier without waking the actor or allocating a world.
================
*/
func (s *monsterStorage) lookup(gid uint32) (monster.Instance, bool) {
	row, ok := s.hot[gid]
	if !ok {
		if r, exists := s.cold[gid]; exists {
			return s.archive.get(gid, r), true
		}
		return monster.Instance{}, false
	}
	return s.hotValue(gid, row), true
}

/*
================
get

Missing actors yield the zero snapshot; admission must establish existence.
================
*/
func (s *monsterStorage) get(gid uint32) monster.Instance {
	row, _ := s.lookup(gid)
	return row
}

/*
================
values

Visit each actor once across the mutually exclusive residency tiers.
================
*/
func (s *monsterStorage) values() iter.Seq2[uint32, monster.Instance] {
	return func(yield func(uint32, monster.Instance) bool) {
		for gid, row := range s.hot {
			if !yield(gid, s.hotValue(gid, row)) {
				return
			}
		}
		for gid, r := range s.cold {
			if !yield(gid, s.archive.get(gid, r)) {
				return
			}
		}
	}
}

/*
================
len

Population size includes sleeping actors.
================
*/
func (s *monsterStorage) len() int {
	return len(s.hot) + len(s.cold)
}

/*
================
contains

Test identity without reconstructing a catalog-backed actor.
================
*/
func (s *monsterStorage) contains(gid uint32) bool {
	_, hot := s.hot[gid]
	_, cold := s.cold[gid]
	return hot || cold
}

/*
================
removeCold

Forget a sleeping monster's cold row.
================
*/
func (s *monsterStorage) removeCold(gid uint32) {
	delete(s.cold, gid)
}

/*
================
remove

Retire all projections owned by an actor, including sparse effect mirrors.
================
*/
func (s *monsterStorage) remove(gid uint32) {
	delete(s.linkedEffects, gid)
	delete(s.selfEffects, gid)
	delete(s.hot, gid)
	delete(s.abnormal, gid)
	s.removeCold(gid)
}

/*
================
release

Return this population's archive slots before the population is discarded.
================
*/
func (s *monsterStorage) release() {
	for gid := range s.cold {
		s.removeCold(gid)
	}
}

/*
================
wake

Move a sleeping actor into resident storage before a live mutation.
================
*/
func (s *monsterStorage) wake(gid uint32) {
	if r, ok := s.cold[gid]; ok {
		row := s.archive.get(gid, r)
		s.set(gid, row)
	}
}

/*
================
freeze

Archive only state represented by the cold record. Active effects and conditional
command history must remain resident until their owners retire them.
================
*/
func (s *monsterStorage) freeze(gid uint32) {
	if s.linkedEffects[gid] != nil || s.selfEffects[gid] != (monster.SelfEffects{}) {
		return
	}
	if s.archive == nil || s.abnormal[gid] != nil {
		return
	}
	row, ok := s.hot[gid]
	if !ok {
		return
	}
	if row.conditionalUsed != 0 {
		return
	}
	if r, ok := s.archive.put(row.value(gid)); ok {
		if s.cold == nil {
			s.cold = make(map[uint32]archivedMonster)
		}
		s.cold[gid] = r
		delete(s.hot, gid)
	}
}

/*
================
metadata

Read targeting metadata without hydrating cold actor state.
================
*/
func (s *monsterStorage) metadata(gid uint32) (uint32, float64) {
	if r, ok := s.cold[gid]; ok {
		ref := r.ref.Value()
		return ref.RefObjID, float64(ref.BodyRadius)
	}
	if r, ok := s.hot[gid]; ok {
		ref := r.ref.Value()
		return ref.RefObjID, float64(ref.BodyRadius)
	}
	return 0, 0
}

/*
================
ids

Enumerate identities without reconstructing actor snapshots.
================
*/
func (s *monsterStorage) ids() iter.Seq[uint32] {
	return func(yield func(uint32) bool) {
		for gid := range s.hot {
			if !yield(gid) {
				return
			}
		}
		for gid := range s.cold {
			if !yield(gid) {
				return
			}
		}
	}
}

/*
================
projection

Expose the compact inspection tuple consistently from either residency tier.
================
*/
func (s *monsterStorage) projection(gid uint32) (monster.MonsterRef, uint8, uint32, uint32) {
	if r, ok := s.cold[gid]; ok {
		return r.ref.Value(), r.rarity, r.hp, r.maxHP
	}
	row := s.get(gid)
	return row.Ref, row.Rarity(), row.CurrentHP, row.EffectiveMaxHP()
}
