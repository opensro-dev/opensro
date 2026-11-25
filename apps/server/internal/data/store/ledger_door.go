/*
===========================================================================

ledger_door.go - when a gold or item change is attributed: the door
lifecycle both ledgers share (#3307)

Port-only, not native: the original keeps no gold or item journal. Every
character write is a door: beginDoorLocked snapshots the characters it
will touch (their gold and their item row counts), the door mutates, and
finishDoorLocked (tickbatch.go) notes each one's difference under the
door's label and closes the snapshot. Nothing stays resident between
doors, so the ledgers' memory is one door's characters, not the roster's
(the resident baselines this replaced held every stored character).

A door with its own transaction (characterRowTx, db_write.go) needs no
snapshot: it holds the live record and the copy it writes, and notes the
difference at the write (characterRowTx::noteWrite).

What the resident baseline used to guarantee - a change made outside any
door is still recorded - is a check at commit instead: each written
character with a committed copy must differ from it by exactly the rows
noted for it since. A difference is written under the reserved door
undeclared-write (domain/economy.go) and logged, so a writer that skips
the lifecycle shows instead of hiding. A character's first write since
load has no committed copy (committedRows fills on write) and is not
checked.

The two ledgers keep their tables, rows, flushes and reads
(economy_ledger.go, item_ledger.go); this file owns only when a change
is attributed, and the one diff every path takes (ledgerRowsLocked).

===========================================================================
*/
package store

import (
	"strings"

	log "github.com/sirupsen/logrus"

	"opensro.online/server/internal/domain"
)

// unclosedDoorLabel names the rows of a door left open at a commit: a
// wiring bug the commit closes rather than lets a later door absorb.
const unclosedDoorLabel = "unclosed-door"

/*
================
doorBefore

A character as a door found it: its gold and its item row counts. The
items buffer is kept across doors, so a steady-state door allocates
nothing.
================
*/
type doorBefore struct {
	c     *domain.Character
	gold  int64
	items []domain.ItemCount
}

/*
================
doorLedger

open holds the snapshots of the door in progress; scratch is the after
counts buffer the diff reuses, and expected the commit check's; warned
names the characters whose undeclared change was already logged, once
each.
================
*/
type doorLedger struct {
	open     []doorBefore
	scratch  []domain.ItemCount
	expected []domain.ItemCount
	warned   map[committedCharacterKey]bool
}

/*
================
openSlotLocked

The next snapshot slot, reusing a closed slot's item buffer.
================
*/
func (s *Store) openSlotLocked(c *domain.Character) *doorBefore {
	n := len(s.door.open)
	if n < cap(s.door.open) {
		s.door.open = s.door.open[:n+1]
	} else {
		s.door.open = append(s.door.open, doorBefore{})
	}
	slot := &s.door.open[n]
	slot.c = c
	return slot
}

/*
================
openedLocked
================
*/
func (s *Store) openedLocked(c *domain.Character) bool {
	for i := range s.door.open {
		if s.door.open[i].c == c {
			return true
		}
	}
	return false
}

/*
================
beginDoorLocked

Snapshots the characters a door is about to change, before it changes
them. A character already in the open door keeps its first snapshot,
so a door that declares one twice counts its change once.
================
*/
func (s *Store) beginDoorLocked(cs ...*domain.Character) {
	for _, c := range cs {
		if c == nil || s.openedLocked(c) {
			continue
		}
		slot := s.openSlotLocked(c)
		slot.gold = ledgerGold(c)
		slot.items = domain.AppendItemRowCounts(slot.items, c)
	}
}

/*
================
beginNewCharacterLocked

A new character's door: it starts from nothing, so the creation records
the starting gold and items as granted.
================
*/
func (s *Store) beginNewCharacterLocked(c *domain.Character) {
	if c == nil || s.openedLocked(c) {
		return
	}
	slot := s.openSlotLocked(c)
	slot.gold = 0
	slot.items = slot.items[:0]
}

/*
================
noteDoorLocked

The door is done: each snapshot's character is diffed against what it
holds now, its rows join the ledgers' pending rows, and the door closes.
A character the store no longer owns (deleted inside the door) writes
nothing.
================
*/
func (s *Store) noteDoorLocked(label string) {
	for i := range s.door.open {
		before := &s.door.open[i]
		if division, known := s.charDivision[before.c]; known {
			gold, moved, items := s.ledgerRowsLocked(label, division, before, before.c)
			if moved {
				s.ledger.pending = append(s.ledger.pending, gold)
			}
			s.items.pending = append(s.items.pending, items...)
		}
	}
	s.closeDoorLocked()
}

/*
================
abandonDoorLocked

A refused door changed nothing it will commit: it closes and writes no
row. Had it changed a value anyway, the commit check names it.
================
*/
func (s *Store) abandonDoorLocked() {
	s.closeDoorLocked()
}

/*
================
closeDoorLocked
================
*/
func (s *Store) closeDoorLocked() {
	for i := range s.door.open {
		// Drop the record pointer; keep the item buffer for the next door.
		s.door.open[i].c = nil
	}
	s.door.open = s.door.open[:0]
}

/*
================
closeUnclosedDoorLocked

A commit found a door still open: a writer began one and never finished
it. Its rows are noted under unclosedDoorLabel and the log names the
characters, rather than letting the next door absorb them.
================
*/
func (s *Store) closeUnclosedDoorLocked() {
	if len(s.door.open) == 0 {
		return
	}
	names := make([]string, 0, len(s.door.open))
	for i := range s.door.open {
		names = append(names, s.door.open[i].c.Name)
	}
	log.WithField("characters", strings.Join(names, ",")).Warn("ledger door: a door was left open at a commit")
	s.noteDoorLocked(unclosedDoorLabel)
}

/*
================
ledgerRowsLocked

The one diff every path takes: the gold row and the item rows of after
differing from before through the door. The store doors pass the live
record against its snapshot, an own transaction the copy it writes, and
the commit check the written copy against what the notes expected.
================
*/
func (s *Store) ledgerRowsLocked(label, division string, before *doorBefore, after *domain.Character) (ledgerRow, bool, []itemLedgerRow) {
	gold, moved := s.goldChangeRowLocked(label, division, after.ID, before.gold, ledgerGold(after))
	s.door.scratch = domain.AppendItemRowCounts(s.door.scratch, after)
	if domain.SameItemHoldings(before.items, s.door.scratch) {
		return gold, moved, nil
	}
	return gold, moved, s.itemChangeRowsLocked(label, division, after, before.items, s.door.scratch)
}

//============================================================================

/*
================
ledgerNoted

What one character's noted rows sum to since its committed copy: the
gold change and each item's change.
================
*/
type ledgerNoted struct {
	gold  int64
	items map[string]int64
}

/*
================
notedByCharacter

Sums a commit's noted rows per character.
================
*/
func notedByCharacter(gold []ledgerRow, items []itemLedgerRow) map[committedCharacterKey]*ledgerNoted {
	noted := map[committedCharacterKey]*ledgerNoted{}
	entry := func(key committedCharacterKey) *ledgerNoted {
		n := noted[key]
		if n == nil {
			n = &ledgerNoted{items: map[string]int64{}}
			noted[key] = n
		}
		return n
	}
	for _, row := range gold {
		entry(committedCharacterKey{division: row.division, id: row.charID}).gold += row.gold
	}
	for _, row := range items {
		entry(committedCharacterKey{division: row.division, id: row.charID}).items[row.item] += row.count
	}
	return noted
}

/*
================
expectedBeforeLocked

What a written character should have held before its noted rows: the
committed copy plus every noted change, as a snapshot the one diff
reads. A written copy that differs from it changed outside any door.
With no item noted - most written characters: a move, a level, a
buff - the committed row counts are the expectation as they are, and
the diff compares them by position; only a character whose items were
noted pays the fold.
================
*/
func (s *Store) expectedBeforeLocked(committed *domain.Character, noted *ledgerNoted) doorBefore {
	expected := doorBefore{gold: ledgerGold(committed)}
	s.door.expected = domain.AppendItemRowCounts(s.door.expected, committed)
	if noted != nil {
		expected.gold += noted.gold
	}
	if noted == nil || len(noted.items) == 0 {
		expected.items = s.door.expected
		return expected
	}
	for item, count := range noted.items {
		s.door.expected = append(s.door.expected, domain.ItemCount{Item: item, Count: count})
	}
	expected.items = domain.FoldItemCounts(s.door.expected)
	return expected
}

/*
================
reconcileWriteLocked

The rows of a written character's change that no note explains, under
the reserved undeclared door; none when the notes add up. Logged once
per character.
================
*/
func (s *Store) reconcileWriteLocked(key committedCharacterKey, committed, written *domain.Character, noted *ledgerNoted) (ledgerRow, bool, []itemLedgerRow) {
	expected := s.expectedBeforeLocked(committed, noted)
	gold, moved, items := s.ledgerRowsLocked(domain.EconomyDoorUndeclared, key.division, &expected, written)
	if (moved || len(items) > 0) && !s.door.warned[key] {
		if s.door.warned == nil {
			s.door.warned = map[committedCharacterKey]bool{}
		}
		s.door.warned[key] = true
		log.WithFields(log.Fields{"division": key.division, "character": written.Name, "gold": gold.gold, "items": len(items)}).
			Warn("ledger door: a character changed outside any door")
	}
	return gold, moved, items
}

/*
================
reconcileStagedLocked

The commit's check: every staged character with a committed copy, in
key order, against the rows pending for it. The undeclared rows join
the pending rows this commit writes; a failed commit keeps them, and its
retry counts them as noted, so nothing is written twice.
================
*/
func (s *Store) reconcileStagedLocked(staged map[committedCharacterKey]*domain.Character) {
	if len(staged) == 0 {
		return
	}
	noted := notedByCharacter(s.ledger.pending, s.items.pending)
	_ = visitSorted(staged, lessCharacterKey, func(key committedCharacterKey, written *domain.Character) error {
		committed, known := s.committedRows[key]
		if !known {
			return nil
		}
		gold, moved, items := s.reconcileWriteLocked(key, committed, written, noted[key])
		if moved {
			s.ledger.pending = append(s.ledger.pending, gold)
		}
		s.items.pending = append(s.items.pending, items...)
		return nil
	})
}

/*
================
lessCharacterKey
================
*/
func lessCharacterKey(a, b committedCharacterKey) bool {
	if a.division != b.division {
		return a.division < b.division
	}
	return a.id < b.id
}
