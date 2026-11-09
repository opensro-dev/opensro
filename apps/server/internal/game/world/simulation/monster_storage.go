/*
===========================================================================

monster_storage.go - compact resident storage for monsters

===========================================================================
*/

package simulation

import (
	"iter"
	"opensro.online/server/internal/game/abnormal"
	"sort"
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
monsterHotSlot

One hot row in dense slot order. The slot is the row's index in
monsterStorage.hot; byGID is the only map the hot tier keeps, and it
maps a gid to its slot. Slot order is insertion order except that
removeHot swaps the last row into the hole, so a walk sees an order
that is stable only within one mutation-free span: consumers that
need an order sort or count, they never inherit one.
================
*/
type monsterHotSlot struct {
	gid uint32
	row residentMonster
}

/*
================
monsterStorage

Cold records omit transient effects; active effect owners keep actors resident.
================
*/
type monsterStorage struct {
	// viewGeneration advances only when what the interest scan reads
	// here can change: an actor appears or leaves, or a spawn moves.
	viewGeneration uint64
	linkedEffects  map[uint32]*monster.EffectSnapshot
	selfEffects    map[uint32]monster.SelfEffects
	targetEffects  map[uint32]monster.TargetEffects
	hot            []monsterHotSlot
	byGID          map[uint32]uint32
	abnormal       map[uint32]*abnormal.Block // immutable non-nil blocks
	cold           map[uint32]archivedMonster
	archive        *monsterArchive
	maxBodyRadius  float64
}

/*
================
newMonsterStorage

Detach mutable actor state from the population's initial catalog projection.
================
*/
func newMonsterStorage(rows map[uint32]monster.Instance) monsterStorage {
	gids := make([]uint32, 0, len(rows))
	for gid := range rows {
		gids = append(gids, gid)
	}
	sort.Slice(gids, func(i, j int) bool { return gids[i] < gids[j] })
	s := monsterStorage{byGID: make(map[uint32]uint32, len(rows))}
	for _, gid := range gids {
		s.set(gid, rows[gid])
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
	// A row that is not hot is new or waking; a wake rewrites the same
	// spawn and costs one memo at worst.
	slot, exists := s.hotSlotOf(gid)
	if !exists || s.hot[slot].row.spawn != row.Spawn {
		s.viewGeneration++
	}
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
	if s.byGID == nil {
		s.byGID = make(map[uint32]uint32)
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
	var old residentMonster
	if exists {
		old = s.hot[slot].row
	}
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
	resident := residentMonster{row.ConditionalUsed, sparse, ref, nest, row.Help, row.Motion, row.Spawn,
		row.NestDetached, row.GMSpawned, row.SpawnHeading, row.TradeVariant, row.StructureState, row.CurrentHP, row.DamageSinceSummon, row.LastSummonCommandMs,
		row.Opponents, row.SummonActionUntilMs, row.SummonerGID,
		row.SummonSightRange, row.SummonerFollowRange}
	if exists {
		s.hot[slot].row = resident
		return
	}
	s.byGID[gid] = uint32(len(s.hot))
	s.hot = append(s.hot, monsterHotSlot{gid: gid, row: resident})
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
		for _, slot := range s.hot {
			if radius := float64(slot.row.ref.Value().BodyRadius); radius > s.maxBodyRadius {
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
	var row monster.Instance
	r.fill(&row, gid)
	return row
}

/*
================
fill

value written in place: the leg-start snapshot builds every due actor
straight into its view slot, so the 2 KB expansion is not copied again
through a return value (#2982).

Written field by field, not as one struct literal: the literal zeroes
the whole 2 KB Instance first, and 660 of those bytes are the four
sparse fields (LinkedEffects, SelfEffects, TargetEffects, Abnormal)
this function never writes - hotFill owns them and assigns every one
right after fill runs, so the literal's zero pass over them was pure
waste on top of the copies the fields themselves need.
================
*/
func (r residentMonster) fill(dst *monster.Instance, gid uint32) {
	dst.ConditionalUsed = r.conditionalUsed
	dst.Gid = gid
	dst.Ref = r.ref.Value()
	dst.Nest = r.nest.Value()
	dst.Help = r.help
	dst.Motion = r.motion
	dst.Spawn = r.spawn
	dst.NestDetached = r.nestDetached
	dst.GMSpawned = r.gmSpawned
	dst.SpawnHeading = r.spawnHeading
	dst.TradeVariant = r.tradeVariant
	dst.StructureState = r.structureState
	dst.CurrentHP = r.currentHP
	dst.DamageSinceSummon = r.damageSinceSummon
	dst.LastSummonCommandMs = r.lastSummonCommandMs
	dst.Opponents = r.opponents
	dst.SummonActionUntilMs = r.summonActionUntilMs
	dst.SummonerGID = r.summonerGID
	dst.SummonSightRange = r.summonSightRange
	dst.SummonerFollowRange = r.summonerFollowRange
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
	var row monster.Instance
	s.hotFill(&row, gid, r)
	return row
}

/*
================
hotFill

Patch the sparse fields onto a filled instance: the effect snapshots and
the abnormal block live behind sparse bits beside the value because they
are wide and usually absent. Every sparse field is assigned, bit set or
not: the view slots this fills are no longer zeroed between batches
(releaseLegSnapshot only drops the heap fields), so dst cannot be trusted
to arrive zero here.
================
*/
func (s *monsterStorage) hotFill(dst *monster.Instance, gid uint32, r residentMonster) {
	r.fill(dst, gid)
	if r.sparse&sparseLinked != 0 {
		dst.LinkedEffects = s.linkedEffects[gid]
	} else {
		dst.LinkedEffects = nil
	}
	if r.sparse&sparseAbnormal != 0 {
		dst.Abnormal = s.abnormal[gid]
	} else {
		dst.Abnormal = nil
	}
	if r.sparse&sparseSelf != 0 {
		dst.SelfEffects = s.selfEffects[gid]
	} else {
		dst.SelfEffects = monster.SelfEffects{}
	}
	if r.sparse&sparseTarget != 0 {
		dst.TargetEffects = s.targetEffects[gid]
	} else {
		dst.TargetEffects = monster.TargetEffects{}
	}
}

/*
================
hotSlotOf

The dense slot of gid's hot row, when the storage holds one.
================
*/
func (s *monsterStorage) hotSlotOf(gid uint32) (uint32, bool) {
	slot, ok := s.byGID[gid]
	return slot, ok
}

/*
================
hotRow

gid's resident row, when hot: lookup's probe without expanding the
detached Instance. The row is a copy; write it back through set.
================
*/
func (s *monsterStorage) hotRow(gid uint32) (residentMonster, bool) {
	slot, ok := s.hotSlotOf(gid)
	if !ok {
		return residentMonster{}, false
	}
	return s.hot[slot].row, true
}

/*
================
hotRowAt

hotRow through a slot carried from the last batch build, verified
against the gid before use (#2133): a miss is the signal, exactly
lookupAt's contract - the caller resolves by gid, so the read is
the storage's either way.
================
*/
func (s *monsterStorage) hotRowAt(slot uint32, gid uint32) (residentMonster, bool) {
	if slot >= uint32(len(s.hot)) || s.hot[slot].gid != gid {
		return residentMonster{}, false
	}
	return s.hot[slot].row, true
}

/*
================
hotRowSlot

hotRow with the dense slot the row sits in: the behaviour batch
build stamps the slot on the ref it carries to dispatch (#2133).
================
*/
func (s *monsterStorage) hotRowSlot(gid uint32) (residentMonster, uint32, bool) {
	slot, ok := s.hotSlotOf(gid)
	if !ok {
		return residentMonster{}, 0, false
	}
	return s.hot[slot].row, slot, true
}

/*
================
hotRows

Every hot row in slot order (see monsterHotSlot): the walk behind
values and the development fixture reads. A walk must not remove as
it goes; collect first, as DevelopmentRemoveFamily does.
================
*/
func (s *monsterStorage) hotRows() iter.Seq2[uint32, residentMonster] {
	return func(yield func(uint32, residentMonster) bool) {
		for _, slot := range s.hot {
			if !yield(slot.gid, slot.row) {
				return
			}
		}
	}
}

/*
================
lookup

Read either residency tier without waking the actor or allocating a world.
================
*/
func (s *monsterStorage) lookup(gid uint32) (monster.Instance, bool) {
	row, ok := s.hotRow(gid)
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
lookupAt

lookup through a slot carried from the behaviour batch build,
verified against the gid before use. removeHot swaps the last row
into the hole, so a carried slot can name another actor by dispatch
time; the caller falls back to lookup on a miss (#2133, the mover
read's contract). No fallback here: the miss is the signal.
================
*/
func (s *monsterStorage) lookupAt(slot uint32, gid uint32) (monster.Instance, bool) {
	var row monster.Instance
	ok := s.lookupAtInto(&row, slot, gid)
	return row, ok
}

/*
================
lookupAtInto

lookupAt written in place (#2982); dst is untouched on a miss.
================
*/
func (s *monsterStorage) lookupAtInto(dst *monster.Instance, slot uint32, gid uint32) bool {
	if slot >= uint32(len(s.hot)) || s.hot[slot].gid != gid {
		return false
	}
	s.hotFill(dst, gid, s.hot[slot].row)
	return true
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
	if r, ok := s.hotRow(gid); ok {
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
		for gid, row := range s.hotRows() {
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
	if _, hot := s.hotSlotOf(gid); hot {
		return true
	}
	_, cold := s.cold[gid]
	return cold
}

/*
================
containsAt

contains through a slot carried from the behaviour batch build,
verified against the gid before use: a hit answers from the row's
place in the slice; a miss (the row moved, went cold, or the hint
is the no-slot sentinel) delegates to contains, so the answer is
the storage's either way (#2133).
================
*/
func (s *monsterStorage) containsAt(slot uint32, gid uint32) bool {
	if slot < uint32(len(s.hot)) && s.hot[slot].gid == gid {
		return true
	}
	return s.contains(gid)
}

/*
================
isDead

get(gid).Ref.MaxHP > 0 && get(gid).CurrentHP == 0 without reconstructing the
full Instance snapshot.
================
*/
func (s *monsterStorage) isDead(gid uint32) bool {
	if r, ok := s.hotRow(gid); ok {
		return r.currentHP == 0 && r.ref.Value().MaxHP > 0
	}
	if r, ok := s.cold[gid]; ok {
		return r.hp == 0 && r.maxHP > 0
	}
	return false
}

/*
================
isDeadAt

isDead through a slot carried from the behaviour batch build,
verified against the gid before use: a hit reads the hot row in
its slot; a miss delegates to isDead, cold rows included, so the
answer is the storage's either way (#2133).
================
*/
func (s *monsterStorage) isDeadAt(slot uint32, gid uint32) bool {
	if slot < uint32(len(s.hot)) && s.hot[slot].gid == gid {
		row := s.hot[slot].row
		return row.currentHP == 0 && row.ref.Value().MaxHP > 0
	}
	return s.isDead(gid)
}

/*
================
currentHP

lookup(gid).CurrentHP and its existence without reconstructing the full
Instance snapshot: expanding the shared catalogue rows dominates a lookup.
================
*/
func (s *monsterStorage) currentHP(gid uint32) (uint32, bool) {
	if r, ok := s.hotRow(gid); ok {
		return r.currentHP, true
	}
	if r, ok := s.cold[gid]; ok {
		return r.hp, true
	}
	return 0, false
}

/*
================
squadApproach

ordinarySquadApproach (monsterapproach.go) read off the resident
row, without reconstructing the full Instance snapshot: the mover
commit's approach sync classifies its actor on every commit, and
expanding the shared catalogue rows dominates a lookup. The
predicate reads only the nest, the summoner and the detached
bit, so a hot row answers from those fields alone; a cold row
hydrates through get, and the answer is the classification's
either way.
================
*/
func (s *monsterStorage) squadApproach(gid uint32) bool {
	if r, ok := s.hotRow(gid); ok {
		return ordinarySquadApproach(&monster.Instance{Nest: r.nest.Value(), SummonerGID: r.summonerGID, NestDetached: r.nestDetached})
	}
	actor := s.get(gid)
	return ordinarySquadApproach(&actor)
}

/*
================
timerFacts

The AI timer check's facts — current HP and the nest tactics flags
the first-bank setup reads — without reconstructing the full
Instance snapshot: the check runs per due actor per leg, and
expanding the shared catalogue rows dominates its cost (#2029).
================
*/
func (s *monsterStorage) timerFacts(gid uint32) (uint32, uint32, bool) {
	if r, ok := s.hotRow(gid); ok {
		return r.currentHP, r.nest.Value().NativeTacticsFlags, true
	}
	if r, ok := s.cold[gid]; ok {
		return r.hp, r.nest.Value().NativeTacticsFlags, true
	}
	return 0, 0, false
}

/*
================
timerFactsAt

timerFacts through a slot carried from the behaviour batch build,
verified against the gid before use: a hit reads the hot row in its
slot; a miss delegates to timerFacts, cold rows included, so the
answer is the storage's either way (#2029).
================
*/
func (s *monsterStorage) timerFactsAt(slot uint32, gid uint32) (uint32, uint32, bool) {
	if slot < uint32(len(s.hot)) && s.hot[slot].gid == gid {
		row := s.hot[slot].row
		return row.currentHP, row.nest.Value().NativeTacticsFlags, true
	}
	return s.timerFacts(gid)
}

/*
================
leaderFacts

The FOLLOW transaction's leader facts — existence, current HP and body
radius — read off the row locate probed, without expanding the detached
Instance snapshot lookup builds for them. The radius is the instance's
own BodyRadius over the row's shared ref and nest rows, so it tracks
the method if its formula ever moves.
================
*/
func (s *monsterStorage) leaderFacts(gid uint32) (uint32, float64, bool) {
	slot := s.locate(gid)
	switch slot.residency {
	case slotHot:
		return slot.hot.currentHP, monster.Instance{Ref: slot.hot.ref.Value(), Nest: slot.hot.nest.Value()}.BodyRadius(), true
	case slotCold:
		return slot.cold.hp, monster.Instance{Ref: slot.cold.ref.Value(), Nest: slot.cold.nest.Value()}.BodyRadius(), true
	}
	return 0, 0, false
}

/*
================
removeHot

Swap-removes gid's hot row: the last row takes the hole and its
byGID entry follows it, so removal stays O(1) and slot order is
the only casualty (see monsterHotSlot). The caller owns
viewGeneration: remove counts a departure, freeze - a residency
change the interest scan does not read as one - does not.
================
*/
func (s *monsterStorage) removeHot(gid uint32) {
	slot, ok := s.hotSlotOf(gid)
	if !ok {
		return
	}
	last := uint32(len(s.hot) - 1)
	if slot != last {
		s.hot[slot] = s.hot[last]
		s.byGID[s.hot[slot].gid] = slot
	}
	s.hot[last] = monsterHotSlot{}
	s.hot = s.hot[:last]
	delete(s.byGID, gid)
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
	s.viewGeneration++
	delete(s.linkedEffects, gid)
	delete(s.selfEffects, gid)
	delete(s.targetEffects, gid)
	s.removeHot(gid)
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
	row, ok := s.hotRow(gid)
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
		s.removeHot(gid)
	}
}

/*
================
shrinkHotExcess

compactSparseMap's discipline for the dense rows, called by
compactDormantIndexes through the same peak cell (sparseMapPeaks[0]):
rebuild only after a large sustained contraction, at most 4096
surviving rows at a 4:1 threshold, so ordinary region churn never
pays the copy. byGID is rebuilt with the rows: it is the one map
the hot tier keeps, and its peak buckets are the same memory.
================
*/
func (s *monsterStorage) shrinkHotExcess(peak *int, allow bool) {
	n := len(s.hot)
	if n > *peak {
		*peak = n
	}
	if !allow || *peak < 8192 || n > 4096 || n*4 > *peak {
		return
	}
	hot := make([]monsterHotSlot, n)
	copy(hot, s.hot)
	s.hot = hot
	byGID := make(map[uint32]uint32, n)
	for slot, row := range s.hot {
		byGID[row.gid] = uint32(slot)
	}
	s.byGID = byGID
	*peak = n
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
	if r, ok := s.hotRow(gid); ok {
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
		for _, slot := range s.hot {
			if !yield(slot.gid) {
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
