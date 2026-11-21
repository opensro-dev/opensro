/*
===========================================================================

ledger_door_test.go - the door lifecycle both ledgers share (#3307)

Every character write is a door that snapshots what it changes and
notes the difference when it finishes (ledger_door.go): a converted
store writer keeps its own label, a refused door writes nothing, the
store keeps no per-character ledger state between doors, and a change
made outside any door is caught at commit, on both commit paths.

===========================================================================
*/
package store

import (
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
)

/*
================
openDoorCount
================
*/
func openDoorCount(s *Store) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.door.open)
}

/*
================
TestDoorLedgerStoreWriterKeepsItsLabel

A guild level-up is a store writer, not a gameplay door: the master's
fee is still one row under its own door.
================
*/
func TestDoorLedgerStoreWriterKeepsItsLabel(t *testing.T) {
	t.Parallel()

	s := openTest(t, t.TempDir(), newTestClock())
	leader := guildTestCharacter("doormaster")
	member := guildTestCharacter("doorhelper")
	for _, character := range []*enterworld.Character{leader, member} {
		if err := s.CreateCharacter(testDivision, "test-account", character); err != nil {
			t.Fatal(err)
		}
	}
	seedTestGuild(t, s, leader, member)
	gold := int64(9000100)
	setGoldIn(s, leader, "test-seed-gold", gold)
	if !updateTestGuild(s, leader.ID, "fixture-gp", func(g enterworld.GuildRecord, m []enterworld.GuildMemberRecord) (enterworld.GuildRecord, []enterworld.GuildMemberRecord) {
		g.GP = 60000
		return g, m
	}) {
		t.Fatal("fixture GP refused")
	}
	if _, refusal := s.Guilds().LevelUpGuildAs(testDivision, leader.ID); refusal.Refused() {
		t.Fatalf("level up refused: %v", refusal)
	}
	var levelUp []storedLedgerRow
	for _, row := range doorRows(t, s) {
		if row.door == "guild-level-up" {
			levelUp = append(levelUp, row)
		}
	}
	if len(levelUp) != 1 || levelUp[0].charID != leader.ID || levelUp[0].gold != 100-gold || levelUp[0].balance != 100 {
		t.Fatalf("level-up rows %+v", levelUp)
	}
	if open := openDoorCount(s); open != 0 {
		t.Fatalf("%d snapshots left open", open)
	}
}

/*
================
TestDoorLedgerRefusedDoorWritesNothing

A refused update closes its door: no row, no snapshot left behind.
================
*/
func TestDoorLedgerRefusedDoorWritesNothing(t *testing.T) {
	t.Parallel()

	s := openTest(t, t.TempDir(), newTestClock())
	c := newLedgerCharacter(t, s, "DoorRefuse", 100)
	if s.UpdateCharacter(c, "npc-repair", func() bool { return false }) {
		t.Fatal("the refusal committed")
	}
	if rows := doorRows(t, s); len(rows) != 0 {
		t.Fatalf("a refused door wrote %+v", rows)
	}
	if open := openDoorCount(s); open != 0 {
		t.Fatalf("%d snapshots left open", open)
	}
}

/*
================
TestDoorLedgerHoldsNothingBetweenDoors

The ledgers keep no per-character state: after loading a roster and
running doors that move gold and items, nothing is left open.
================
*/
func TestDoorLedgerHoldsNothingBetweenDoors(t *testing.T) {
	t.Parallel()

	dir, clock := t.TempDir(), newTestClock()
	s := openTest(t, dir, clock)
	var roster []*domain.Character
	for _, name := range []string{"DoorIdleA", "DoorIdleB", "DoorIdleC"} {
		roster = append(roster, newItemCharacter(t, s, name))
	}
	s.UpdateCharacters(roster[:2], "exchange", func() bool {
		sword := roster[0].MissionInventory[0]
		roster[0].MissionInventory = nil
		sword.Slot = 14
		roster[1].MissionInventory = append(roster[1].MissionInventory, sword)
		return true
	})
	if open := openDoorCount(s); open != 0 {
		t.Fatalf("%d snapshots left open after a door", open)
	}
	s.Close()
	reopened := openTest(t, dir, clock)
	if open := openDoorCount(reopened); open != 0 {
		t.Fatalf("%d snapshots open after load", open)
	}
}

/*
================
TestDoorLedgerCatchesAWriteOutsideAnyDoor

A change no door declared - gold and an item put straight onto the
record, then committed by an unrelated door - is written under the
undeclared door at commit, counted, and written once.
================
*/
func TestDoorLedgerCatchesAWriteOutsideAnyDoor(t *testing.T) {
	t.Parallel()

	s := openTest(t, t.TempDir(), newTestClock())
	c := newItemCharacter(t, s, "DoorSkip")
	setGoldIn(s, c, "test-seed-gold", 100)
	s.mu.Lock()
	gold := int64(175)
	c.Gold = &gold
	c.MissionInventory = append(c.MissionInventory, potionRow(14, 3))
	s.mu.Unlock()
	s.UpdateCharacter(c, "move", func() bool {
		c.Level = new(int64)
		return true
	})
	s.UpdateCharacter(c, "move", func() bool {
		level := int64(1)
		c.Level = &level
		return true
	})
	gold1 := doorRows(t, s)
	if len(gold1) != 1 || gold1[0].door != domain.EconomyDoorUndeclared || gold1[0].reason != domain.EconomyReasonUndeclared || gold1[0].gold != 75 {
		t.Fatalf("undeclared gold rows %+v", gold1)
	}
	items := itemLedgerRows(t, s)
	if len(items) != 1 || items[0].door != domain.EconomyDoorUndeclared || items[0].item != "ITEM_ETC_HP_POTION_01" || items[0].count != 3 {
		t.Fatalf("undeclared item rows %+v", items)
	}
	counters := s.EconomyCounters()
	if counters["gold.undeclared.gain"] != 75 || counters["items.undeclared"] != 1 {
		t.Fatalf("counters %v", counters)
	}
}

/*
================
TestDoorLedgerCatchesAnOutsideWriteInAnOwnTransaction

The same check on a door with its own transaction: a warehouse deposit
of a record that also took gold outside any door writes the deposit
under its door and the rest as undeclared, in the deposit's transaction.
================
*/
func TestDoorLedgerCatchesAnOutsideWriteInAnOwnTransaction(t *testing.T) {
	t.Parallel()

	s := openTest(t, t.TempDir(), newTestClock())
	c := newLedgerCharacter(t, s, "DoorOwnTx", 500)
	s.mu.Lock()
	gold := int64(540)
	c.Gold = &gold
	s.mu.Unlock()
	if _, err := s.TransactStorage(c, func(next *domain.Character, storage *domain.AccountStorage) error {
		remaining := *next.Gold - 120
		next.Gold = &remaining
		storage.Gold += 120
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	byDoor := map[string]int64{}
	for _, row := range doorRows(t, s) {
		byDoor[row.door] += row.gold
	}
	if len(byDoor) != 2 || byDoor["storage-transfer"] != -120 || byDoor[domain.EconomyDoorUndeclared] != 40 {
		t.Fatalf("own-transaction rows by door %v", byDoor)
	}
}

/*
================
TestDoorLedgerClosesADoorLeftOpen

A writer that begins a door and commits without finishing it: the
commit notes its change under the unclosed-door label instead of
leaving it to the next door.
================
*/
func TestDoorLedgerClosesADoorLeftOpen(t *testing.T) {
	t.Parallel()

	s := openTest(t, t.TempDir(), newTestClock())
	c := newLedgerCharacter(t, s, "DoorOpen", 100)
	s.mu.Lock()
	s.beginDoorLocked(c)
	gold := int64(130)
	c.Gold = &gold
	s.markCharacterLocked(c)
	s.commitLocked("test-unfinished")
	s.mu.Unlock()
	rows := doorRows(t, s)
	if len(rows) != 1 || rows[0].door != unclosedDoorLabel || rows[0].gold != 30 {
		t.Fatalf("unclosed door rows %+v", rows)
	}
	if open := openDoorCount(s); open != 0 {
		t.Fatalf("%d snapshots left open", open)
	}
}

//============================================================================

/*
================
benchItemCharacter

A full stored character for the door benchmarks: a 96-slot bag of
stacks, the equipment, and a pet with a 32-slot bag (142 rows).
================
*/
func benchItemCharacter(b *testing.B, s *Store, name string) *domain.Character {
	b.Helper()
	c := seededCharacter()
	c.Name = name
	if err := s.CreateCharacter(testDivision, "item-account-"+name, c); err != nil {
		b.Fatal(err)
	}
	pet := &domain.CharacterCOS{Codename: "COS_P_RABBIT", Container: &domain.COSContainer{Capacity: 32}}
	s.UpdateCharacter(c, "test-fill", func() bool {
		for slot := int64(0); slot < 13; slot++ {
			c.MissionInventory = append(c.MissionInventory, domain.InventoryRow{Slot: slot, Codename: "ITEM_GEAR_" + string(rune('A'+slot)), StackCount: 1})
		}
		for slot := int64(13); slot < 109; slot++ {
			c.MissionInventory = append(c.MissionInventory, domain.InventoryRow{Slot: slot, Codename: "ITEM_ETC_" + string(rune('A'+slot%40)), StackCount: 50})
		}
		for slot := int64(0); slot < 32; slot++ {
			pet.Container.Rows = append(pet.Container.Rows, domain.InventoryRow{Slot: slot, Codename: "ITEM_ETC_" + string(rune('a'+slot%20)), StackCount: 10})
		}
		c.ActiveCOS = pet
		return true
	})
	return c
}

/*
================
BenchmarkDoorLedgerUnchanged

One door's ledger work for a full character that moved no gold and no
item: the snapshot and the comparison, the cost every door pays.
================
*/
func BenchmarkDoorLedgerUnchanged(b *testing.B) {
	s := openTest(b, b.TempDir(), newTestClock())
	c := benchItemCharacter(b, s, "DoorBench")
	s.mu.Lock()
	defer s.mu.Unlock()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		s.beginDoorLocked(c)
		s.noteDoorLocked("move")
	}
}

/*
================
BenchmarkDoorLedgerChanged

A door that used one potion and spent gold from a full character: the
snapshot, the positional diff and the two rows.
================
*/
func BenchmarkDoorLedgerChanged(b *testing.B) {
	s := openTest(b, b.TempDir(), newTestClock())
	c := benchItemCharacter(b, s, "DoorMoved")
	s.mu.Lock()
	defer s.mu.Unlock()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		s.beginDoorLocked(c)
		c.MissionInventory[20].StackCount ^= 1
		gold := ledgerGold(c) ^ 1
		c.Gold = &gold
		s.noteDoorLocked("item-use")
		s.items.pending = s.items.pending[:0]
		s.ledger.pending = s.ledger.pending[:0]
	}
}

/*
================
BenchmarkCommitReconcile

The commit check for one staged full character whose notes add up (no
item noted): the cost a commit pays per written character, beside the
JSON encode of the same record.
================
*/
func BenchmarkCommitReconcile(b *testing.B) {
	s := openTest(b, b.TempDir(), newTestClock())
	c := benchItemCharacter(b, s, "DoorCheck")
	s.mu.Lock()
	defer s.mu.Unlock()
	key := committedCharacterKey{division: testDivision, id: c.ID}
	committed := s.committedRows[key]
	if committed == nil {
		b.Fatal("no committed copy")
	}
	written := domain.CloneCharacterStaged(c, committed)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, moved, items := s.reconcileWriteLocked(key, committed, written, nil); moved || len(items) > 0 {
			b.Fatal("a clean write reconciled to rows")
		}
	}
}
