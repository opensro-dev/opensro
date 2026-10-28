/*
===========================================================================

commit_skip_test.go - the commit does not rewrite a character whose
bytes did not change

Doors mark characters dirty with no change signal, so the commit is
the first place that can see a touch changed nothing. These tests pin
the digest skip in commitTxLocked: a same-value commit writes no
character row, a changed one does, and a failed commit retains no
digest that would make a later commit skip a row the disk lacks. The
last test pins the other writer: every offline door that writes a
character row in its own transaction (characterRowTx) retains the
digest of the bytes it wrote, so a later revert to the pre-door bytes
is written, not skipped against a stale digest.

===========================================================================
*/

package store

import (
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/social/guildwar"
)

/*
================
commitSkipFixture
================
*/
func commitSkipFixture(t *testing.T) (*Store, *enterworld.Character) {
	t.Helper()
	s, err := Open(t.TempDir(), Options{Now: newTestClock().Now, DefaultSkills: testSkillSeeder})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	c := &enterworld.Character{Name: "Skipper", Gold: int64Ptr(100)}
	if err := s.CreateCharacter(testDivision, "skip-account", c); err != nil {
		t.Fatal(err)
	}
	return s, c
}

/*
================
TestCommitSkipsUnchangedCharacterRows
================
*/
func TestCommitSkipsUnchangedCharacterRows(t *testing.T) {
	s, c := commitSkipFixture(t)

	before := s.LockTiming()
	s.MutateCharacter(c, "noop touch", func() {})
	after := s.LockTiming()
	if got := after.CharacterRowsSkipped - before.CharacterRowsSkipped; got != 1 {
		t.Fatalf("unchanged commit skipped %d rows, want 1 (written %d)", got, after.CharacterRowsWritten-before.CharacterRowsWritten)
	}
	if got := after.CharacterRowsWritten - before.CharacterRowsWritten; got != 0 {
		t.Fatalf("unchanged commit wrote %d rows, want 0", got)
	}

	before = after
	s.MutateCharacter(c, "gold change", func() { c.Gold = int64Ptr(*c.Gold + 1) })
	after = s.LockTiming()
	if got := after.CharacterRowsWritten - before.CharacterRowsWritten; got != 1 {
		t.Fatalf("changed commit wrote %d rows, want 1 (skipped %d)", got, after.CharacterRowsSkipped-before.CharacterRowsSkipped)
	}

	// The skipped write never loses a real change: after a reopen the
	// changed gold is what the disk holds.
	s.Close()
	s2, err := Open(s.dir, Options{Now: newTestClock().Now, DefaultSkills: testSkillSeeder})
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	var loaded *enterworld.Character
	s2.ReadCharacters(testDivision, func(characters []*enterworld.Character) {
		for _, candidate := range characters {
			if candidate.Name == c.Name {
				loaded = candidate
			}
		}
	})
	if loaded == nil || *loaded.Gold != *c.Gold {
		t.Fatalf("reloaded gold = %v, want %d", loaded, *c.Gold)
	}
}

/*
================
TestFailedCommitRetainsNoDigest

A commit that fails must not stage a digest: the next commit writes
the row even though its bytes match the failed attempt's.
================
*/
func TestFailedCommitRetainsNoDigest(t *testing.T) {
	s, c := commitSkipFixture(t)

	// Fail inside the transaction, after the character row was staged:
	// the rollback must take the staged digest with it.
	s.beforeSQLiteCommit = func() { panic("injected commit failure") }
	func() {
		defer func() { _ = recover() }()
		s.MutateCharacter(c, "doomed gold", func() { c.Gold = int64Ptr(*c.Gold + 5) })
	}()
	s.beforeSQLiteCommit = nil

	before := s.LockTiming()
	s.MutateCharacter(c, "noop after failure", func() {})
	after := s.LockTiming()
	if got := after.CharacterRowsWritten - before.CharacterRowsWritten; got != 1 {
		t.Fatalf("commit after failure wrote %d rows, want 1: the failed attempt must retain no digest (skipped %d)", got, after.CharacterRowsSkipped-before.CharacterRowsSkipped)
	}
}

//============================================================================

/*
================
offlineDoorFixture

One offline door's revert witness: the door writes the character in
its own transaction and installs the change in memory; revert returns
the live record to its exact pre-door bytes; probe reads the changed
field back off any copy of the record (live or reopened).
================
*/
type offlineDoorFixture struct {
	dir    string
	s      *Store
	c      *domain.Character
	door   func(t *testing.T)
	revert func(c *domain.Character)
	probe  func(c *domain.Character) int64
}

/*
================
goldProbe
================
*/
func goldProbe(c *domain.Character) int64 {
	if c.Gold == nil {
		return -1
	}
	return *c.Gold
}

/*
================
runOfflineDoorRevert

opensro2's repro shape for one door: the door writes new bytes in its
own transaction; an in-memory revert to the exact pre-door bytes then
commits. That commit must WRITE the row - if the door left the
pre-door digest retained, the revert hashes to it, is skipped, and
the door's bytes come back on reopen, which the reopen probe catches.
================
*/
func runOfflineDoorRevert(t *testing.T, fx offlineDoorFixture) {
	t.Helper()
	before := fx.probe(fx.c)
	fx.door(t)
	if got := fx.probe(fx.c); got == before {
		t.Fatalf("the door did not change the character in memory (probe still %d)", got)
	}
	timing := fx.s.LockTiming()
	fx.s.MutateCharacter(fx.c, "door-revert", func() { fx.revert(fx.c) })
	after := fx.s.LockTiming()
	if got := after.CharacterRowsWritten - timing.CharacterRowsWritten; got < 1 {
		t.Fatalf("revert commit wrote %d character rows, want at least 1: the door left a stale retained digest (skipped %d)",
			got, after.CharacterRowsSkipped-timing.CharacterRowsSkipped)
	}
	if got := fx.probe(fx.c); got != before {
		t.Fatalf("reverted probe = %d, want %d", got, before)
	}
	fx.s.Close()
	reopened := openTest(t, fx.dir, newTestClock())
	for _, loaded := range reopened.Characters().CharactersForDivision(testDivision) {
		if loaded.ID == fx.c.ID {
			if got := fx.probe(loaded); got != before {
				t.Fatalf("reopened probe = %d, want %d: the door's bytes came back", got, before)
			}
			return
		}
	}
	t.Fatal("character missing after reopen")
}

/*
================
TestOfflineDoorsRetainTheirWrittenDigests

Every characterRowTx caller in its revert shape: storage, the mall,
the offline item swap, a guild war's stakes, fortress tax, fortress
staff, both fortress forge writes and the rename. (The seed writes
into a fresh database behind an empty digest map - no live store, so
no retained digest can be stale; db_write.go says so at the call.)
================
*/
func TestOfflineDoorsRetainTheirWrittenDigests(t *testing.T) {
	t.Run("storage", func(t *testing.T) {
		dir := t.TempDir()
		s := openTest(t, dir, newTestClock())
		c := seededCharacter()
		if err := s.CreateCharacter(testDivision, "storage-account", c); err != nil {
			t.Fatal(err)
		}
		runOfflineDoorRevert(t, offlineDoorFixture{dir: dir, s: s, c: c,
			door: func(t *testing.T) {
				if _, err := s.TransactStorage(c, func(next *domain.Character, _ *domain.AccountStorage) error {
					next.Gold = int64Ptr(424242)
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			},
			revert: func(c *domain.Character) { c.Gold = int64Ptr(12345) },
			probe:  goldProbe})
	})

	t.Run("item mall", func(t *testing.T) {
		dir := t.TempDir()
		s := openTest(t, dir, newTestClock())
		c := seededCharacter()
		if err := s.CreateCharacter(testDivision, "mall-account", c); err != nil {
			t.Fatal(err)
		}
		if _, err := s.db.Exec("INSERT INTO mall_accounts VALUES (?, ?, ?, ?)", "mall-account", 100, 20, 10); err != nil {
			t.Fatal(err)
		}
		runOfflineDoorRevert(t, offlineDoorFixture{dir: dir, s: s, c: c,
			door: func(t *testing.T) {
				if _, err := s.PurchaseMall(c, domain.MallBalance{Silk: 30}, func(rows []domain.InventoryRow) ([]domain.InventoryRow, error) {
					rows[0].StackCount = 2
					return rows, nil
				}); err != nil {
					t.Fatal(err)
				}
			},
			revert: func(c *domain.Character) { c.MissionInventory[0].StackCount = 1 },
			probe:  func(c *domain.Character) int64 { return int64(c.MissionInventory[0].StackCount) }})
	})

	t.Run("offline item swap", func(t *testing.T) {
		dir := t.TempDir()
		s := openTest(t, dir, newTestClock())
		c := &domain.Character{Name: "Swapper", MissionInventory: []domain.InventoryRow{
			{Slot: 13, RefObjID: 1, Codename: "SUN", StackCount: 1}}}
		if err := s.CreateCharacter(testDivision, "swap-account", c); err != nil {
			t.Fatal(err)
		}
		runOfflineDoorRevert(t, offlineDoorFixture{dir: dir, s: s, c: c,
			door: func(t *testing.T) {
				if _, err := s.SwapItemsOffline(testDivision, []string{"Swapper"}, sunToMoon, true); err != nil {
					t.Fatal(err)
				}
			},
			revert: func(c *domain.Character) {
				c.MissionInventory[0].Codename = "SUN"
				c.MissionInventory[0].RefObjID = 1
			},
			probe: func(c *domain.Character) int64 { return int64(c.MissionInventory[0].RefObjID) }})
	})

	t.Run("guild war stakes", func(t *testing.T) {
		dir := t.TempDir()
		s := openTest(t, dir, newTestClock())
		var masters [2]*domain.Character
		var guilds [2]int64
		for side, name := range []string{"Red", "Blue"} {
			c := guildTestCharacter(name + "master")
			c.Gold = int64Ptr(1000)
			if err := s.CreateCharacter(testDivision, name, c); err != nil {
				t.Fatal(err)
			}
			id, err := s.Guilds().CreateGuild(testDivision, domain.GuildRecord{Name: name, Level: 1},
				domain.GuildMemberRecord{CharID: c.ID, JID: uint32(c.ID), Name: c.Name, Grade: 0}, c)
			if err != nil {
				t.Fatal(err)
			}
			masters[side], guilds[side] = c, id
		}
		a, err := guildwar.New(testDivision, s.GuildWars())
		if err != nil {
			t.Fatal(err)
		}
		start := domain.GuildWarStart{Masters: [2]int64{masters[0].ID, masters[1].ID},
			Record: domain.GuildWarRecord{Guilds: guilds, Stake: 1000, ScoreIndex: 1, EndMs: 2000}}
		runOfflineDoorRevert(t, offlineDoorFixture{dir: dir, s: s, c: masters[0],
			door: func(t *testing.T) {
				if _, code, err := a.Begin(start); code != 0 || err != nil {
					t.Fatalf("begin %d %v", code, err)
				}
			},
			revert: func(c *domain.Character) { c.Gold = int64Ptr(1000) },
			probe:  goldProbe})
	})

	t.Run("fortress tax", func(t *testing.T) {
		f := newFortressTaxFixture(t, 100, 10)
		runOfflineDoorRevert(t, offlineDoorFixture{dir: f.dir, s: f.s, c: f.master,
			door: func(t *testing.T) {
				if _, code, err := f.a.CollectTax(testDivision, 1, f.master.ID, 25); code != 0 || err != nil {
					t.Fatalf("collect %d %v", code, err)
				}
			},
			revert: func(c *domain.Character) { c.Gold = int64Ptr(10) },
			probe:  goldProbe})
	})

	t.Run("fortress staff", func(t *testing.T) {
		f := newFortressTaxFixture(t, 100, 40000)
		if !updateTestGuild(f.s, f.master.ID, "staff-fixture-gp", func(g domain.GuildRecord, m []domain.GuildMemberRecord) (domain.GuildRecord, []domain.GuildMemberRecord) {
			g.GP = 10000
			return g, m
		}) {
			t.Fatal("fixture guild points refused")
		}
		runOfflineDoorRevert(t, offlineDoorFixture{dir: f.dir, s: f.s, c: f.master,
			door: func(t *testing.T) {
				if code, err := f.a.HireStaff(testDivision, 1, f.master.ID, 0x04); code != 0 || err != nil {
					t.Fatalf("hire %d %v", code, err)
				}
			},
			revert: func(c *domain.Character) { c.Gold = int64Ptr(40000) },
			probe:  goldProbe})
	})

	t.Run("fortress forge start", func(t *testing.T) {
		dir := t.TempDir()
		s, a, leader, _, _ := forgeFixture(t, dir)
		setForgeRole(t, s, leader, leader, 0x08, 1500)
		if !s.UpdateCharacters([]*domain.Character{leader}, "forge-fixture", func() bool {
			leader.Gold = int64Ptr(400000)
			return true
		}) {
			t.Fatal("fixture gold refused")
		}
		order := domain.FortressItemForgeRecord{FortressID: 1, ItemRefID: 19227, Count: 10, StartedAtMs: 1000, EndsAtMs: 601000}
		runOfflineDoorRevert(t, offlineDoorFixture{dir: dir, s: s, c: leader,
			door: func(t *testing.T) {
				if code, err := a.StartItemForge(testDivision, forgeTestSmith,
					domain.FortressItemForgeStart{Forge: order, ActorID: leader.ID, Role: 0x08, Gold: 1000, GP: 500}); code != 0 || err != nil {
					t.Fatalf("start %d %v", code, err)
				}
			},
			revert: func(c *domain.Character) { c.Gold = int64Ptr(400000) },
			probe:  goldProbe})
	})

	t.Run("fortress forge collect", func(t *testing.T) {
		dir := t.TempDir()
		s, a, leader, _, _ := forgeFixture(t, dir)
		setForgeRole(t, s, leader, leader, 0, 0)
		// A real balance, so the zero-charge start below writes bytes
		// identical to the retained ones: the collect's revert is then
		// byte-exact against them, as the repro shape requires.
		if !s.UpdateCharacters([]*domain.Character{leader}, "forge-fixture", func() bool {
			leader.Gold = int64Ptr(5000)
			return true
		}) {
			t.Fatal("fixture gold refused")
		}
		order := domain.FortressItemForgeRecord{FortressID: 1, ItemRefID: 19227, Count: 10, StartedAtMs: 1000, EndsAtMs: 601000}
		if code, err := a.StartItemForge(testDivision, forgeTestSmith,
			domain.FortressItemForgeStart{Forge: order, ActorID: leader.ID, Role: 0x08}); code != 0 || err != nil {
			t.Fatalf("start %d %v", code, err)
		}
		a.AdvanceItemForges(601000)
		rows := len(leader.MissionInventory)
		runOfflineDoorRevert(t, offlineDoorFixture{dir: dir, s: s, c: leader,
			door: func(t *testing.T) {
				collect := domain.FortressItemForgeCollect{Forge: order, ActorID: leader.ID, Role: 0x08,
					Item: domain.InventoryRow{Slot: 20, RefObjID: 19227, Codename: "ITEM_ETC_SIEGE_TEST", StackCount: 3}}
				if code, err := a.CollectItemForge(testDivision, forgeTestSmith, collect, 3); code != 0 || err != nil {
					t.Fatalf("collect %d %v", code, err)
				}
			},
			revert: func(c *domain.Character) {
				// Nil, not an empty slice: the pre-door bytes say
				// null, and the repro shape needs them byte-exact.
				if rows == 0 {
					c.MissionInventory = nil
				} else {
					c.MissionInventory = c.MissionInventory[:rows]
				}
			},
			probe: func(c *domain.Character) int64 { return int64(len(c.MissionInventory)) }})
	})

	t.Run("rename", func(t *testing.T) {
		dir := t.TempDir()
		s := openTest(t, dir, newTestClock())
		c := &domain.Character{Name: "Test2", MissionInventory: []domain.InventoryRow{{Slot: 13, RefObjID: 24198, StackCount: 2}}}
		if err := s.CreateCharacter(testDivision, "test-account", c); err != nil {
			t.Fatal(err)
		}
		runOfflineDoorRevert(t, offlineDoorFixture{dir: dir, s: s, c: c,
			door: func(t *testing.T) {
				if err := s.RenameCharacterOffline(testDivision, "Test2", "[GM]Test2"); err != nil {
					t.Fatal(err)
				}
			},
			revert: func(c *domain.Character) { c.Name = "Test2" },
			probe:  func(c *domain.Character) int64 { return int64(len(c.Name)) }})
	})
}
