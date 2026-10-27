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

sparse names the sparse transient maps that hold an entry for this gid
(sparseLinked and friends). set and remove are the only writers of those
maps, and set rewrites the row with them, so the mask never goes stale; a
read then skips the probes for maps that cannot hold the actor, which most
actors in a busy population never need.
==================
*/
type residentMonster struct {
	conditionalUsed     uint8
	sparse              uint8
	ref                 unique.Handle[monster.MonsterRef]
	nest                unique.Handle[monster.NestRow]
	help                monster.HelpInbox
	motion              monster.MotionHold
	spawn               monster.SpawnPoint
	nestDetached        bool
	gmSpawned           bool
	spawnHeading        uint16
	tradeVariant        uint8
	structureState      uint16
	currentHP           uint32
	damageSinceSummon   uint32
	lastSummonCommandMs uint32
	opponents           [2]monster.Opponent
	summonActionUntilMs int64
	summonerGID         uint32
	summonSightRange    float64
	summonerFollowRange float64
}

// residentMonster.sparse bits: one per sparse transient map.
const (
	sparseLinked = 1 << iota
	sparseAbnormal
	sparseSelf
	sparseTarget
)

/*
================
monsterStorage

Cold records omit transient effects; active effect owners keep actors resident.
================
*/
type monsterStorage struct {
	linkedEffects map[uint32]*monster.EffectSnapshot
	selfEffects   map[uint32]monster.SelfEffects
	targetEffects map[uint32]monster.TargetEffects
	hot           map[uint32]residentMonster
	abnormal      map[uint32]*abnormal.Block // immutable non-nil blocks
	cold          map[uint32]archivedMonster
	archive       *monsterArchive
	maxBodyRadius float64
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
	var sparse uint8
	if row.LinkedEffects != nil {
		sparse |= sparseLinked
		if s.linkedEffects == nil {
			s.linkedEffects = make(map[uint32]*monster.EffectSnapshot)
		}
		s.linkedEffects[gid] = row.LinkedEffects
	} else {
		delete(s.linkedEffects, gid)
	}
	if row.SelfEffects != (monster.SelfEffects{}) {
		sparse |= sparseSelf
		if s.selfEffects == nil {
			s.selfEffects = make(map[uint32]monster.SelfEffects)
		}
		s.selfEffects[gid] = row.SelfEffects
	} else {
		delete(s.selfEffects, gid)
	}
	if row.TargetEffects != (monster.TargetEffects{}) {
		sparse |= sparseTarget
		if s.targetEffects == nil {
			s.targetEffects = make(map[uint32]monster.TargetEffects)
		}
		s.targetEffects[gid] = row.TargetEffects
	} else {
		delete(s.targetEffects, gid)
	}
	if s.hot == nil {
		s.hot = make(map[uint32]residentMonster)
	}
	s.removeCold(gid)
	if row.Abnormal != nil {
		sparse |= sparseAbnormal
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
	if radius := float64(row.Ref.BodyRadius); radius > s.maxBodyRadius {
		s.maxBodyRadius = radius
	}
	s.hot[gid] = residentMonster{row.ConditionalUsed, sparse, ref, nest, row.Help, row.Motion, row.Spawn,
		row.NestDetached, row.GMSpawned, row.SpawnHeading, row.TradeVariant, row.StructureState, row.CurrentHP, row.DamageSinceSummon, row.LastSummonCommandMs,
		row.Opponents, row.SummonActionUntilMs, row.SummonerGID,
		row.SummonSightRange, row.SummonerFollowRange}
}

/*
================
maxRadiusBound
================
*/
func (s *monsterStorage) maxRadiusBound() float64 {
	if s == nil {
		return 0
	}
	if s.maxBodyRadius <= 0 && (len(s.hot) > 0 || len(s.cold) > 0) {
		for _, r := range s.hot {
			if radius := float64(r.ref.Value().BodyRadius); radius > s.maxBodyRadius {
				s.maxBodyRadius = radius
			}
		}
		for _, r := range s.cold {
			if radius := float64(r.ref.Value().BodyRadius); radius > s.maxBodyRadius {
				s.maxBodyRadius = radius
			}
		}
	}
	return s.maxBodyRadius
}

/*
================
value

Expand shared catalog handles without exposing mutable population storage.
================
*/
func (r residentMonster) value(gid uint32) monster.Instance {
	return monster.Instance{ConditionalUsed: r.conditionalUsed, Gid: gid, Ref: r.ref.Value(), Nest: r.nest.Value(), Help: r.help,
		Motion: r.motion, Spawn: r.spawn, NestDetached: r.nestDetached, GMSpawned: r.gmSpawned,
		SpawnHeading: r.spawnHeading, TradeVariant: r.tradeVariant, StructureState: r.structureState, CurrentHP: r.currentHP, DamageSinceSummon: r.damageSinceSummon,
		LastSummonCommandMs: r.lastSummonCommandMs,
		Opponents:           r.opponents, SummonActionUntilMs: r.summonActionUntilMs, SummonerGID: r.summonerGID,
		SummonSightRange: r.summonSightRange, SummonerFollowRange: r.summonerFollowRange}
}

/*
================
hotValue

Join sparse transient state onto a detached resident value. A map whose bit
is clear holds no entry for gid, so its probe would return the zero value
r.value already carries.
================
*/
func (s *monsterStorage) hotValue(gid uint32, r residentMonster) monster.Instance {
	row := r.value(gid)
	if r.sparse == 0 {
		return row
	}
	if r.sparse&sparseLinked != 0 {
		row.LinkedEffects = s.linkedEffects[gid]
	}
	if r.sparse&sparseAbnormal != 0 {
		row.Abnormal = s.abnormal[gid]
	}
	if r.sparse&sparseSelf != 0 {
		row.SelfEffects = s.selfEffects[gid]
	}
	if r.sparse&sparseTarget != 0 {
		row.TargetEffects = s.targetEffects[gid]
	}
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

// monsterSlot.residency values.
const (
	slotMissing = iota
	slotHot
	slotCold
)

/*
================
monsterSlot

Where locate found an actor, with the row it read there: hot is valid for
slotHot and cold for slotCold.
================
*/
type monsterSlot struct {
	hot       residentMonster
	cold      archivedMonster
	residency uint8
}

/*
================
locate

contains that keeps the row it probed, so a caller that chose this storage
by residency hydrates without a second probe.
================
*/
func (s *monsterStorage) locate(gid uint32) monsterSlot {
	if r, ok := s.hot[gid]; ok {
		return monsterSlot{hot: r, residency: slotHot}
	}
	if r, ok := s.cold[gid]; ok {
		return monsterSlot{cold: r, residency: slotCold}
	}
	return monsterSlot{}
}

/*
================
slotValue

lookup's value for a slot locate returned, with no storage write between.
================
*/
func (s *monsterStorage) slotValue(gid uint32, slot *monsterSlot) monster.Instance {
	switch slot.residency {
	case slotHot:
		return s.hotValue(gid, slot.hot)
	case slotCold:
		return s.archive.get(gid, slot.cold)
	}
	return monster.Instance{}
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
	if _, hot := s.hot[gid]; hot {
		return true
	}
	_, cold := s.cold[gid]
	return cold
}

/*
================
isDead

get(gid).Ref.MaxHP > 0 && get(gid).CurrentHP == 0 without reconstructing the
full Instance snapshot.
================
*/
func (s *monsterStorage) isDead(gid uint32) bool {
	if r, ok := s.hot[gid]; ok {
		return r.currentHP == 0 && r.ref.Value().MaxHP > 0
	}
	if r, ok := s.cold[gid]; ok {
		return r.hp == 0 && r.maxHP > 0
	}
	return false
}

/*
================
currentHP

lookup(gid).CurrentHP and its existence without reconstructing the full
Instance snapshot: expanding the shared catalogue rows dominates a lookup.
================
*/
func (s *monsterStorage) currentHP(gid uint32) (uint32, bool) {
	if r, ok := s.hot[gid]; ok {
		return r.currentHP, true
	}
	if r, ok := s.cold[gid]; ok {
		return r.hp, true
	}
	return 0, false
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
	delete(s.targetEffects, gid)
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
	if s.linkedEffects[gid] != nil || s.selfEffects[gid] != (monster.SelfEffects{}) ||
		s.targetEffects[gid] != (monster.TargetEffects{}) {
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
