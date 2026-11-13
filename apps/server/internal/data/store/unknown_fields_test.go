/*
===========================================================================

unknown_fields_test.go - every record kind keeps a newer release's fields

A store a newer release wrote may carry top-level record fields this
binary does not know (#3090, part 1 of #3082). Each record kind is
pinned the same way: the row is planted with an extra populated field,
the store loads it, an unrelated change rewrites the row through that
kind's own writer, and the row reads back with the field byte-equal
beside the change.

===========================================================================
*/
package store

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/social/guildwar"
)

// futureField is a newer release's field: nested, mixed and populated,
// in the compact form a store writes.
const futureField = `"futureField":{"nested":[1,"two",{"x":true}],"at":"2026-11-13"}`

/*
================
rowRecord

The record column of the one row query selects, read straight from the
closed store's file.
================
*/
func rowRecord(t *testing.T, dir, query string, args ...any) string {
	t.Helper()
	db, err := openDB(filepath.Join(dir, DBFileName))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var record string
	if err := db.QueryRow(query, args...).Scan(&record); err != nil {
		t.Fatalf("%s %v: %v", query, args, err)
	}
	return record
}

/*
================
plantFutureField

Appends futureField to the one row query selects, in the closed store's
file. update writes the record back and takes the record first, then
the query's arguments.
================
*/
func plantFutureField(t *testing.T, dir, query, update string, args ...any) {
	t.Helper()
	record := rowRecord(t, dir, query, args...)
	planted := withFutureField(t, record)
	rewriteRow(t, dir, update, append([]any{planted}, args...)...)
}

/*
================
withFutureField
================
*/
func withFutureField(t *testing.T, record string) string {
	t.Helper()
	if !strings.HasSuffix(record, "}") || strings.Contains(record, "futureField") {
		t.Fatalf("cannot plant into %s", record)
	}
	return record[:len(record)-1] + "," + futureField + "}"
}

/*
================
plantPlaneFutureField

plantFutureField for one entry of a meta plane row.
================
*/
func plantPlaneFutureField(t *testing.T, dir, metaKey, entry string) {
	t.Helper()
	var plane map[string]json.RawMessage
	if err := json.Unmarshal([]byte(rowRecord(t, dir, "SELECT value FROM meta WHERE key = ?", metaKey)), &plane); err != nil {
		t.Fatal(err)
	}
	if plane[entry] == nil {
		t.Fatalf("plane %s has no entry %q", metaKey, entry)
	}
	plane[entry] = json.RawMessage(withFutureField(t, string(plane[entry])))
	raw, err := json.Marshal(plane)
	if err != nil {
		t.Fatal(err)
	}
	rewriteRow(t, dir, "UPDATE meta SET value = ? WHERE key = ?", string(raw), metaKey)
}

/*
================
rewriteRow
================
*/
func rewriteRow(t *testing.T, dir, update string, args ...any) {
	t.Helper()
	db, err := openDB(filepath.Join(dir, DBFileName))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	result, err := db.Exec(update, args...)
	if err != nil {
		t.Fatal(err)
	}
	if n, _ := result.RowsAffected(); n != 1 {
		t.Fatalf("%s rewrote %d rows", update, n)
	}
	if _, err := db.Exec("PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		t.Fatal(err)
	}
}

/*
================
requireFutureField

The row still carries futureField, byte-equal and once, and holds want
(the unrelated change).
================
*/
func requireFutureField(t *testing.T, record, want string) {
	t.Helper()
	if strings.Count(record, futureField) != 1 {
		t.Fatalf("the newer release's field was lost: %s", record)
	}
	if !strings.Contains(record, want) {
		t.Fatalf("the unrelated change %s did not land: %s", want, record)
	}
	var probe map[string]json.RawMessage
	if err := decodeJSONStrict([]byte(record), &probe); err != nil {
		t.Fatalf("the row is no longer one JSON object: %v", err)
	}
}

/*
================
requirePlaneFutureField
================
*/
func requirePlaneFutureField(t *testing.T, dir, metaKey, entry, want string) {
	t.Helper()
	var plane map[string]json.RawMessage
	if err := json.Unmarshal([]byte(rowRecord(t, dir, "SELECT value FROM meta WHERE key = ?", metaKey)), &plane); err != nil {
		t.Fatal(err)
	}
	requireFutureField(t, string(plane[entry]), want)
}

/*
================
storedCharacter

The reopened store's character of the given name.
================
*/
func storedCharacter(t *testing.T, s *Store, name string) *domain.Character {
	t.Helper()
	for _, c := range s.Characters().CharactersForDivision(testDivision) {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("no character %q", name)
	return nil
}

//============================================================================

/*
================
TestCharacterKeepsUnknownFields

Through the commit's character writer, and through the delete reap's
archive, which copies the whole row.
================
*/
func TestCharacterKeepsUnknownFields(t *testing.T) {
	t.Parallel()
	dir, clock := t.TempDir(), newTestClock()
	s := openTest(t, dir, clock)
	c := seededCharacter()
	if err := s.CreateCharacter(testDivision, "account-a", c); err != nil {
		t.Fatal(err)
	}
	id := c.ID
	s.Close()
	plantFutureField(t, dir, "SELECT record FROM characters WHERE division = ? AND id = ?",
		"UPDATE characters SET record = ? WHERE division = ? AND id = ?", testDivision, id)

	s = openTest(t, dir, clock)
	c = storedCharacter(t, s, c.Name)
	gold := int64(4321)
	s.MutateCharacter(c, "unrelated gold", func() { c.Gold = &gold })
	s.Close()
	requireFutureField(t, rowRecord(t, dir, "SELECT record FROM characters WHERE division = ? AND id = ?", testDivision, id), `"gold":4321`)

	s = openTest(t, dir, clock)
	c = storedCharacter(t, s, c.Name)
	s.MutateCharacter(c, "delete-reserve", func() {
		c.DeletePending = true
		c.DeleteReservedAt = clock.Now().UTC().Format("2006-01-02T15:04:05Z07:00")
	})
	s.Close()
	clock.Advance(DeleteReservationWindow + 3600e9)
	s = openTest(t, dir, clock)
	if reaped := s.ReapMaturedDeletions(); len(reaped) != 1 {
		t.Fatalf("reaped %v", reaped)
	}
	s.Close()
	requireFutureField(t, rowRecord(t, dir, "SELECT record FROM deleted_characters WHERE division = ?", testDivision), `"deletePending":true`)
}

/*
================
TestGroundItemKeepsUnknownFields
================
*/
func TestGroundItemKeepsUnknownFields(t *testing.T) {
	t.Parallel()
	dir, clock := t.TempDir(), newTestClock()
	s := openTest(t, dir, clock)
	r := grounditem.NewRegistry()
	s.AttachGround(r)
	gid := r.Add(testDivision, grounditem.Item{RefObjID: 3117, StackCount: 1, Position: grounditem.Point{RegionID: 0x6a9a}}).Gid
	s.Mutate("ground seed", nil)
	s.Close()
	plantFutureField(t, dir, "SELECT record FROM ground_items WHERE division = ? AND gid = ?",
		"UPDATE ground_items SET record = ? WHERE division = ? AND gid = ?", testDivision, gid)

	s = openTest(t, dir, clock)
	r = grounditem.NewRegistry()
	r.Restore(s.GroundSnapshotForRestore())
	s.AttachGround(r)
	r.SetStackCount(testDivision, gid, 4)
	s.Mutate("unrelated stack", nil)
	s.Close()
	requireFutureField(t, rowRecord(t, dir, "SELECT record FROM ground_items WHERE division = ? AND gid = ?", testDivision, gid), `"stackCount":4`)
}

/*
================
TestLetterKeepsUnknownFields

A letter has no identity but its place, so its fields must follow it
when a new letter shifts it to the next row.
================
*/
func TestLetterKeepsUnknownFields(t *testing.T) {
	t.Parallel()
	dir, clock := t.TempDir(), newTestClock()
	s := openTest(t, dir, clock)
	c := memoTestCharacter("postbox")
	if err := s.CreateCharacter(testDivision, "account-a", c); err != nil {
		t.Fatal(err)
	}
	id := c.ID
	first := domain.LetterRecord{Sender: "Ash", SenderModelRefID: 1907, PackedReceiveTime: 0x00c8_739a, Body: "first body"}
	s.Letters().UpdateMailbox(testDivision, id, "letter-send", func(mailbox []domain.LetterRecord) ([]domain.LetterRecord, bool) {
		return append(mailbox, first), true
	})
	s.Close()
	plantFutureField(t, dir, "SELECT record FROM memos WHERE division = ? AND char_id = ? AND seq = 0",
		"UPDATE memos SET record = ? WHERE division = ? AND char_id = ? AND seq = 0", testDivision, id)

	s = openTest(t, dir, clock)
	fresh := domain.LetterRecord{Sender: "Cale", SenderModelRefID: 14875, PackedReceiveTime: 0x00c8_739b, Body: "fresh body"}
	s.Letters().UpdateMailbox(testDivision, id, "letter-read", func(mailbox []domain.LetterRecord) ([]domain.LetterRecord, bool) {
		mailbox[0].ReadFlag = 1
		return append([]domain.LetterRecord{fresh}, mailbox...), true
	})
	s.Close()
	requireFutureField(t, rowRecord(t, dir, "SELECT record FROM memos WHERE division = ? AND char_id = ? AND seq = 1", testDivision, id), `"readFlag":1`)
	if record := rowRecord(t, dir, "SELECT record FROM memos WHERE division = ? AND char_id = ? AND seq = 0", testDivision, id); strings.Contains(record, "futureField") {
		t.Fatalf("the new letter took the old letter's fields: %s", record)
	}
}

/*
================
TestGuildAndMemberKeepUnknownFields
================
*/
func TestGuildAndMemberKeepUnknownFields(t *testing.T) {
	t.Parallel()
	dir, clock := t.TempDir(), newTestClock()
	s := openTest(t, dir, clock)
	c := guildTestCharacter("Founder")
	if err := s.CreateCharacter(testDivision, "account-a", c); err != nil {
		t.Fatal(err)
	}
	guildID, err := s.Guilds().CreateGuild(testDivision, domain.GuildRecord{Name: "Silk", Level: 1},
		domain.GuildMemberRecord{CharID: c.ID, JID: uint32(c.ID), Name: c.Name}, c)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	plantFutureField(t, dir, "SELECT record FROM guilds WHERE division = ? AND guild_id = ?",
		"UPDATE guilds SET record = ? WHERE division = ? AND guild_id = ?", testDivision, guildID)
	plantFutureField(t, dir, "SELECT record FROM guild_members WHERE division = ? AND guild_id = ? AND char_id = ?",
		"UPDATE guild_members SET record = ? WHERE division = ? AND guild_id = ? AND char_id = ?", testDivision, guildID, c.ID)

	s = openTest(t, dir, clock)
	_, refusal := s.Guilds().UpdateGuildAs(testDivision, c.ID, "unrelated notice", domain.GuildAuthorization{},
		func(g domain.GuildRecord, m []domain.GuildMemberRecord) (domain.GuildRecord, []domain.GuildMemberRecord, bool) {
			g.NoticeSubject = "revised"
			m[0].Name = c.Name
			return g, m, true
		})
	if refusal.Refused() {
		t.Fatalf("update refused: %v", refusal)
	}
	s.Close()
	requireFutureField(t, rowRecord(t, dir, "SELECT record FROM guilds WHERE division = ? AND guild_id = ?", testDivision, guildID), `"revised"`)
	requireFutureField(t, rowRecord(t, dir, "SELECT record FROM guild_members WHERE division = ? AND guild_id = ? AND char_id = ?", testDivision, guildID, c.ID), `"Founder"`)
}

/*
================
TestTrainingCampAndMemberKeepUnknownFields
================
*/
func TestTrainingCampAndMemberKeepUnknownFields(t *testing.T) {
	t.Parallel()
	dir, clock := t.TempDir(), newTestClock()
	s := openTest(t, dir, clock)
	master := guildTestCharacter("campmaster")
	if err := s.CreateCharacter(testDivision, "account-a", master); err != nil {
		t.Fatal(err)
	}
	camp := openTestCamp(t, s, master)
	s.Close()
	plantFutureField(t, dir, "SELECT record FROM training_camps WHERE division = ? AND camp_id = ?",
		"UPDATE training_camps SET record = ? WHERE division = ? AND camp_id = ?", testDivision, camp.ID)
	plantFutureField(t, dir, "SELECT record FROM training_camp_members WHERE division = ? AND camp_id = ? AND char_id = ?",
		"UPDATE training_camp_members SET record = ? WHERE division = ? AND camp_id = ? AND char_id = ?", testDivision, camp.ID, master.ID)

	s = openTest(t, dir, clock)
	if _, _, ok := s.TrainingCamps().UpdateNotice(testDivision, master.ID, camp.ID, "Welcome", "Meet at the gate."); !ok {
		t.Fatal("notice refused")
	}
	s.Close()
	requireFutureField(t, rowRecord(t, dir, "SELECT record FROM training_camps WHERE division = ? AND camp_id = ?", testDivision, camp.ID), `"Welcome"`)
	requireFutureField(t, rowRecord(t, dir, "SELECT record FROM training_camp_members WHERE division = ? AND camp_id = ? AND char_id = ?", testDivision, camp.ID, master.ID),
		fmt.Sprintf(`%d`, master.ID))
}

/*
================
TestAllianceKeepsUnknownFields

The union writer takes a record the game built from its own state, so
the fields come back from the row it replaces.
================
*/
func TestAllianceKeepsUnknownFields(t *testing.T) {
	t.Parallel()
	dir, clock := t.TempDir(), newTestClock()
	s := openTest(t, dir, clock)
	union := domain.AllianceRecord{AllianceID: 7, Guilds: [domain.AllianceSlots]int64{7, 9}}
	if err := s.Alliances().SaveAlliance(testDivision, union, true); err != nil {
		t.Fatal(err)
	}
	s.Close()
	plantFutureField(t, dir, "SELECT record FROM alliances WHERE division = ? AND alliance_id = ?",
		"UPDATE alliances SET record = ? WHERE division = ? AND alliance_id = ?", testDivision, 7)

	s = openTest(t, dir, clock)
	if unions, err := s.Alliances().Alliances(testDivision); err != nil || len(unions) != 1 {
		t.Fatalf("unions %+v (%v)", unions, err)
	}
	union.Guilds[2] = 11
	if err := s.Alliances().SaveAlliance(testDivision, domain.AllianceRecord{AllianceID: 7, Guilds: union.Guilds}, true); err != nil {
		t.Fatal(err)
	}
	s.Close()
	requireFutureField(t, rowRecord(t, dir, "SELECT record FROM alliances WHERE division = ? AND alliance_id = ?", testDivision, 7), `11`)
}

/*
================
TestGuildWarKeepsUnknownFields
================
*/
func TestGuildWarKeepsUnknownFields(t *testing.T) {
	t.Parallel()
	dir, clock := t.TempDir(), newTestClock()
	s := openTest(t, dir, clock)
	var masters [2]*domain.Character
	var guilds [2]int64
	for side, name := range []string{"Red", "Blue"} {
		c := guildTestCharacter(name + "master")
		gold := int64(1000)
		c.Gold = &gold
		if err := s.CreateCharacter(testDivision, name, c); err != nil {
			t.Fatal(err)
		}
		id, err := s.Guilds().CreateGuild(testDivision, domain.GuildRecord{Name: name, Level: 1}, domain.GuildMemberRecord{CharID: c.ID, JID: uint32(c.ID), Name: c.Name}, c)
		if err != nil {
			t.Fatal(err)
		}
		masters[side], guilds[side] = c, id
	}
	wars, err := guildwar.New(testDivision, s.GuildWars())
	if err != nil {
		t.Fatal(err)
	}
	war, code, err := wars.Begin(domain.GuildWarStart{Masters: [2]int64{masters[0].ID, masters[1].ID},
		Record: domain.GuildWarRecord{Guilds: guilds, Stake: 1000, ScoreIndex: 1, EndMs: 2000}})
	if code != 0 || err != nil {
		t.Fatalf("begin %d %v", code, err)
	}
	s.Close()
	plantFutureField(t, dir, "SELECT record FROM guild_wars WHERE division = ? AND id = ?",
		"UPDATE guild_wars SET record = ? WHERE division = ? AND id = ?", testDivision, war.ID)

	s = openTest(t, dir, clock)
	if wars, err = guildwar.New(testDivision, s.GuildWars()); err != nil {
		t.Fatal(err)
	}
	if _, _, code, err := wars.Combat(domain.GuildWarCombat{WarID: war.ID, KillerID: masters[0].ID, VictimID: masters[1].ID, Score: 100}, 1999); code != 0 || err != nil {
		t.Fatalf("combat %d %v", code, err)
	}
	s.Close()
	requireFutureField(t, rowRecord(t, dir, "SELECT record FROM guild_wars WHERE division = ? AND id = ?", testDivision, war.ID), `[100,0]`)
}

/*
================
TestFortressAndStructureKeepUnknownFields

The fortress record is rebuilt by the game on every save, so its fields
come back from the row it replaces; the staff and tax doors share that
writer (upsertFortressTx).
================
*/
func TestFortressAndStructureKeepUnknownFields(t *testing.T) {
	t.Parallel()
	dir, clock := t.TempDir(), newTestClock()
	s := openTest(t, dir, clock)
	saved := domain.FortressRecord{FortressID: 1, GuildID: 7, TaxRate: 5}
	tower := domain.FortressStructureRecord{FortressID: 1, EventStructID: 85, RefObjID: 19536, OwnerGuildID: 7, HP: 120, State: 1}
	if err := s.Fortresses().SaveFortress(testDivision, saved); err != nil {
		t.Fatal(err)
	}
	if err := s.Fortresses().SaveFortressStructure(testDivision, tower, true); err != nil {
		t.Fatal(err)
	}
	s.Close()
	plantFutureField(t, dir, "SELECT record FROM fortresses WHERE division = ? AND fortress_id = ?",
		"UPDATE fortresses SET record = ? WHERE division = ? AND fortress_id = ?", testDivision, 1)
	plantFutureField(t, dir, "SELECT record FROM fortress_structures WHERE division = ? AND fortress_id = ? AND event_struct_id = ?",
		"UPDATE fortress_structures SET record = ? WHERE division = ? AND fortress_id = ? AND event_struct_id = ?", testDivision, 1, 85)

	s = openTest(t, dir, clock)
	if records, _, err := s.Fortresses().FortressState(testDivision); err != nil || len(records) != 1 {
		t.Fatalf("fortresses %+v (%v)", records, err)
	}
	if err := s.Fortresses().SaveFortress(testDivision, domain.FortressRecord{FortressID: 1, GuildID: 7, TaxRate: 15}); err != nil {
		t.Fatal(err)
	}
	tower.HP = 77
	if err := s.Fortresses().SaveFortressStructure(testDivision, tower, true); err != nil {
		t.Fatal(err)
	}
	s.Close()
	requireFutureField(t, rowRecord(t, dir, "SELECT record FROM fortresses WHERE division = ? AND fortress_id = ?", testDivision, 1), `"taxRate":15`)
	requireFutureField(t, rowRecord(t, dir, "SELECT record FROM fortress_structures WHERE division = ? AND fortress_id = ? AND event_struct_id = ?", testDivision, 1, 85), `77`)
}

/*
================
TestFortressItemForgeKeepsUnknownFields

saveFortressItemForgeTx is the forge row's only writer: the start and
collect doors both reach the row through it.
================
*/
func TestFortressItemForgeKeepsUnknownFields(t *testing.T) {
	t.Parallel()
	dir, clock := t.TempDir(), newTestClock()
	s := openTest(t, dir, clock)
	forge := domain.FortressItemForgeRecord{FortressID: 1, ItemRefID: 19227, Count: 10, StartedAtMs: 1000, EndsAtMs: 601000}
	saveForge := func(s *Store, forge domain.FortressItemForgeRecord) {
		t.Helper()
		tx, err := s.syncDBLocked().Begin()
		if err != nil {
			t.Fatal(err)
		}
		if err := saveFortressItemForgeTx(tx, testDivision, forge, true); err != nil {
			_ = tx.Rollback()
			t.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	saveForge(s, forge)
	s.Close()
	plantFutureField(t, dir, "SELECT record FROM fortress_item_forges WHERE division = ? AND fortress_id = ? AND item_ref_id = ?",
		"UPDATE fortress_item_forges SET record = ? WHERE division = ? AND fortress_id = ? AND item_ref_id = ?", testDivision, 1, 19227)

	s = openTest(t, dir, clock)
	forge.Count = 4
	saveForge(s, forge)
	s.Close()
	requireFutureField(t, rowRecord(t, dir, "SELECT record FROM fortress_item_forges WHERE division = ? AND fortress_id = ? AND item_ref_id = ?", testDivision, 1, 19227), `"count":4`)
}

/*
================
TestAccountStorageKeepsUnknownFields
================
*/
func TestAccountStorageKeepsUnknownFields(t *testing.T) {
	t.Parallel()
	dir, clock := t.TempDir(), newTestClock()
	s := openTest(t, dir, clock)
	c := seededCharacter()
	gold := int64(500)
	c.Gold = &gold
	if err := s.CreateCharacter(testDivision, "storage-account", c); err != nil {
		t.Fatal(err)
	}
	deposit := func(next *domain.Character, storage *domain.AccountStorage) error {
		remaining := *next.Gold - 100
		next.Gold = &remaining
		storage.Gold += 100
		return nil
	}
	if _, err := s.TransactStorage(c, deposit); err != nil {
		t.Fatal(err)
	}
	s.Close()
	plantFutureField(t, dir, "SELECT record FROM account_storage WHERE account_id = ?",
		"UPDATE account_storage SET record = ? WHERE account_id = ?", "storage-account")

	s = openTest(t, dir, clock)
	c = storedCharacter(t, s, c.Name)
	if stored, err := s.AccountStorage(c); err != nil || stored.Gold != 100 {
		t.Fatalf("storage %+v (%v)", stored, err)
	}
	if _, err := s.TransactStorage(c, deposit); err != nil {
		t.Fatal(err)
	}
	s.Close()
	requireFutureField(t, rowRecord(t, dir, "SELECT record FROM account_storage WHERE account_id = ?", "storage-account"), `"gold":200`)
}

/*
================
TestUniqueKillKeepsUnknownFields

A kill row is written once: it must load with the field, and later
kills must leave it in place.
================
*/
func TestUniqueKillKeepsUnknownFields(t *testing.T) {
	t.Parallel()
	dir, clock := t.TempDir(), newTestClock()
	s := openTest(t, dir, clock)
	first, err := s.RecordUniqueKill(testDivision, domain.UniqueKill{AtMs: 1000, RefObjID: 1954})
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	plantFutureField(t, dir, "SELECT record FROM unique_kills WHERE division = ? AND seq = ?",
		"UPDATE unique_kills SET record = ? WHERE division = ? AND seq = ?", testDivision, first.Seq)

	s = openTest(t, dir, clock)
	if kills, err := s.UniqueKillsAfter(testDivision, 0); err != nil || len(kills) != 1 || kills[0] != first {
		t.Fatalf("kills %+v (%v)", kills, err)
	}
	if _, err := s.RecordUniqueKill(testDivision, domain.UniqueKill{AtMs: 2000, RefObjID: 1954}); err != nil {
		t.Fatal(err)
	}
	s.Close()
	requireFutureField(t, rowRecord(t, dir, "SELECT record FROM unique_kills WHERE division = ? AND seq = ?", testDivision, first.Seq), `"atMs":1000`)
}

/*
================
TestMetaPlaneEntriesKeepUnknownFields

The four meta planes: the trade reward pools and job rankings (by
division) and the Halloween box and survival arena counts (by account).
================
*/
func TestMetaPlaneEntriesKeepUnknownFields(t *testing.T) {
	t.Parallel()
	dir, clock := t.TempDir(), newTestClock()
	s := openTest(t, dir, clock)
	c := seededCharacter()
	if err := s.CreateCharacter(testDivision, "account-a", c); err != nil {
		t.Fatal(err)
	}
	accountDoor := func(s *Store, c *domain.Character, boxes int64, entries uint8) {
		t.Helper()
		if !s.UpdateCharacter(c, "account planes", func() bool {
			s.HalloweenAccount("account-a").Count = boxes
			s.SookArenaAccount("account-a").Entries = entries
			return true
		}) {
			t.Fatal("account door refused")
		}
	}
	if !s.UpdateTrade([]*domain.Character{c}, "seed pool", func(pool *domain.TradeRewardPool) bool {
		*pool = domain.TradeRewardPool{Hunters: 3}
		return true
	}) {
		t.Fatal("seed pool refused")
	}
	if !s.CloseJobWeek(testDivision, 100, "seed week") {
		t.Fatal("seed week refused")
	}
	accountDoor(s, c, 2, 1)
	s.Close()
	plantPlaneFutureField(t, dir, metaKeyTradeRewards, testDivision)
	plantPlaneFutureField(t, dir, metaKeyJobRankings, testDivision)
	plantPlaneFutureField(t, dir, metaKeyHalloweenBoxes, "account-a")
	plantPlaneFutureField(t, dir, metaKeySookArenaAccounts, "account-a")

	s = openTest(t, dir, clock)
	c = storedCharacter(t, s, c.Name)
	if !s.UpdateTrade([]*domain.Character{c}, "pool change", func(pool *domain.TradeRewardPool) bool {
		return pool.Credit(domain.JobThief, 5)
	}) {
		t.Fatal("pool change refused")
	}
	if !s.CloseJobWeek(testDivision, 101, "next week") {
		t.Fatal("next week refused")
	}
	accountDoor(s, c, 3, 2)
	s.Close()
	requirePlaneFutureField(t, dir, metaKeyTradeRewards, testDivision, `"thieves":5`)
	requirePlaneFutureField(t, dir, metaKeyJobRankings, testDivision, `101`)
	requirePlaneFutureField(t, dir, metaKeyHalloweenBoxes, "account-a", `"count":3`)
	requirePlaneFutureField(t, dir, metaKeySookArenaAccounts, "account-a", `"entries":2`)
}
