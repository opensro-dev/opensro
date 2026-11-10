/*
===========================================================================

monster_storage_hot.go - the hot tier's read doors

The resident (hot) monster rows' read side: the gid and carried-slot
resolves the batch build, the scheduler and the leg snapshot walk
each tick. The doors differ by what the caller may pay - the whole
row by value (hotRow), three verdict scalars in place (hotProbeAt),
or the row itself in place under the state lock (hotRowPtrAt).
monster_storage.go owns the rows and every write door; these only
read.

===========================================================================
*/

package simulation

import (
	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/world/monster"
)

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
hotRowPtrAt

The scheduler's read of the hot row in place. hotRowAt copies the
216-byte row out, and the due computation reads five scalars and,
rarely, the nest off it; the detach guard reads the step's mover
the same way. The pointer is a working read only: the caller
holds the state lock, reads, and never retains it - a set landing
a new row or removeHot's swap can move what it names (#3027). A
miss answers nil, and the scheduler reads a nil row as the zero
row hotRowAt's miss used to hand it.
================
*/
func (s *monsterStorage) hotRowPtrAt(slot uint32, gid uint32) (*residentMonster, bool) {
	if slot >= uint32(len(s.hot)) || s.hot[slot].gid != gid {
		return nil, false
	}
	return &s.hot[slot].row, true
}

/*
================
hotRowPtr

hotRowPtrAt resolved by gid.
================
*/
func (s *monsterStorage) hotRowPtr(gid uint32) (*residentMonster, bool) {
	slot, ok := s.hotSlotOf(gid)
	if !ok {
		return nil, false
	}
	return &s.hot[slot].row, true
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
hotProbeAt

The behaviour batch build's per-ref verdicts - the row's hit
points, its reference's max and its summon deadline - read where
they sit. hotRowAt copies the resident row (216 bytes) out for
them and the filter then copies the interned reference (320
bytes) on top for the max alone; the slot caches that max where
the row lands, so a probe reads the three scalars in place
(#3027). A miss is the signal, exactly hotRowAt's contract.
================
*/
func (s *monsterStorage) hotProbeAt(slot uint32, gid uint32) (currentHP uint32, maxHP uint32, summonActionUntilMs int64, ok bool) {
	if slot >= uint32(len(s.hot)) || s.hot[slot].gid != gid {
		return 0, 0, 0, false
	}
	held := &s.hot[slot]
	return held.row.currentHP, held.maxHP, held.row.summonActionUntilMs, true
}

/*
================
restFactsAt

The instance half of the rest-facts door (MonsterState.restFactsAt):
the resident facts the settled verdict reads, projected where they
sit instead of paying residentMonster.fill plus hotFill to expand
the 2 KB instance. The slot hint is verified against the gid, then
resolved by gid, exactly the snapshot fill's own resolution; a cold
row answers from the hydration lookup itself falls back to,
projected the same way. The confusion face is the one sparse join a
verdict input touches (Tempted, behind the target guard): it reads
the abnormal block the fill would join, never a copy of it. False
only when the actor is gone.
================
*/
func (s *monsterStorage) restFactsAt(slot uint32, gid uint32, facts *restFacts) bool {
	if slot < uint32(len(s.hot)) && s.hot[slot].gid == gid {
		s.hot[slot].row.projectRestFacts(s, gid, facts)
		facts.monsterSlot = slot
		return true
	}
	if row, resolved, ok := s.hotRowSlot(gid); ok {
		row.projectRestFacts(s, gid, facts)
		facts.monsterSlot = resolved
		return true
	}
	instance, ok := s.lookup(gid)
	if !ok {
		return false
	}
	facts.projectInstance(instance)
	// lookup leaves the snapshot's monster slot at the caller's hint
	// for a cold row (behaviorActorMoverInto), and so does this door.
	facts.monsterSlot = slot
	return true
}

/*
================
residentMonster.projectRestFacts

The resident row's verdict facts, read off the row and its interned
handles: motion, help and hit points as they sit; the nest facts the
detach test and the tactics projection read (ResolveTactics derives
Aggressive from the nest's own flags - the door projects that one
derivation); temptation from the sparse abnormal join.
================
*/
func (r residentMonster) projectRestFacts(s *monsterStorage, gid uint32, facts *restFacts) {
	facts.motion = r.motion
	facts.help = r.help
	facts.currentHP = r.currentHP
	facts.refMaxHP = r.ref.Value().MaxHP
	facts.nestDetached = r.nestDetached
	nest := r.nest.Value()
	facts.nestRegionID = nest.RegionID
	facts.aggressive = nest.Aggressive && (nest.PolicyPinned || nest.RetailEvidence || nest.HasControls)
	facts.tempted = r.currentHP != 0 && r.sparse&sparseAbnormal != 0 && s.abnormal[gid] != nil &&
		s.abnormal[gid].Slots[abnormal.Confusion].Active
}

/*
================
restFacts.projectInstance

projectRestFacts for a hydrated cold row: the same facts read off
the expanded instance the fill would have built, through the same
methods (Tempted is the instance's own).
================
*/
func (facts *restFacts) projectInstance(instance monster.Instance) {
	facts.motion = instance.Motion
	facts.help = instance.Help
	facts.currentHP = instance.CurrentHP
	facts.refMaxHP = instance.Ref.MaxHP
	facts.nestDetached = instance.NestDetached
	facts.nestRegionID = instance.Nest.RegionID
	facts.aggressive = instance.Nest.Aggressive && (instance.Nest.PolicyPinned || instance.Nest.RetailEvidence || instance.Nest.HasControls)
	facts.tempted = instance.Tempted()
}

/*
================
hotProbe

hotProbeAt resolved by gid, with the dense slot the row sits in
for the caller to carry (#2133).
================
*/
func (s *monsterStorage) hotProbe(gid uint32) (slot uint32, currentHP uint32, maxHP uint32, summonActionUntilMs int64, ok bool) {
	slot, ok = s.hotSlotOf(gid)
	if !ok {
		return 0, 0, 0, 0, false
	}
	currentHP, maxHP, summonActionUntilMs, ok = s.hotProbeAt(slot, gid)
	return slot, currentHP, maxHP, summonActionUntilMs, ok
}
