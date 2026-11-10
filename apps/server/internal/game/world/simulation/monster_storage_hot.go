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
