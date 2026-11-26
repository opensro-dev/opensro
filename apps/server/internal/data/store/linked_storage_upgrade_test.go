/*
===========================================================================

linked_storage_upgrade_test.go - the additive schema 61 step (Magic Cube storage)

===========================================================================
*/
package store

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/testsupport/tier"
)

/*
================
testCubeContainer

A 10-slot Magic Cube linked storage holding one weapon intensify elixir.
================
*/
func testCubeContainer() *domain.COSContainer {
	return &domain.COSContainer{
		Capacity: 10,
		Rows: []domain.InventoryRow{{
			Slot:       0,
			RefObjID:   2151,
			Codename:   "ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_WEAPON_A",
			TypeFlags:  0x0a0d,
			StackCount: 3,
		}},
	}
}

/*
================
testCubeRow

One Magic Cube inventory row carrying linked storage.
================
*/
func testCubeRow(slot int64) domain.InventoryRow {
	return domain.InventoryRow{
		Slot:          slot,
		RefObjID:      35568,
		Codename:      "ITEM_PRE_MALL_MAGIC_CUBE_WEAPON_10",
		TypeFlags:     0x030b,
		StackCount:    1,
		Data:          3,
		LinkedStorage: testCubeContainer(),
	}
}

/*
================
TestLinkedStorageUpgradeRewritesNothing

A schema 60 authority at layout 17 upgrades to 61 with its records
untouched (the top-level cube storage carriers are optional and absent,
so every saved item is not a cube with contents); a second upgrade has
nothing to do.
================
*/
func TestLinkedStorageUpgradeRewritesNothing(t *testing.T) {
	tier.Long(t, "a schema-60 layout-17 authority upgrades to 61 with its records untouched")
	dir := t.TempDir()
	s := openTest(t, dir, newTestClock())
	c := seededCharacter()
	if err := s.CreateCharacter(testDivision, "account", c); err != nil {
		t.Fatal(err)
	}
	var before string
	if err := s.syncDBLocked().QueryRow("SELECT record FROM characters WHERE division = ? AND id = ?", testDivision, c.ID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	s.Close()
	rewriteDatabaseMeta(t, dir, metaKeySchemaVersion, 60)
	if backup, err := UpgradeAuthority(dir, true); err != nil || backup == "" {
		t.Fatalf("upgrade %q: %v", backup, err)
	}
	reopened := openTest(t, dir, newTestClock())
	var after string
	if err := reopened.syncDBLocked().QueryRow("SELECT record FROM characters WHERE division = ? AND id = ?", testDivision, c.ID).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatal("the upgrade rewrote the character")
	}
	reopened.Close()
	if _, err := UpgradeAuthority(dir, true); !errors.Is(err, ErrAuthorityCurrent) {
		t.Fatalf("second upgrade = %v, want ErrAuthorityCurrent", err)
	}
}

/*
================
TestLinkedStoragePersistsAtTopLevelAndRoundTrips

Magic Cube linked storage persists only at the top level of character,
account_storage, guild and ground_items (never nested inside item
arrays), keeping schema 61 rollback-compatible with schema 60, and
restores intact across close and reopen.
================
*/
func TestLinkedStoragePersistsAtTopLevelAndRoundTrips(t *testing.T) {
	t.Parallel()
	dir, clock := t.TempDir(), newTestClock()
	s := openTest(t, dir, clock)

	c := seededCharacter()
	c.MissionInventory = []domain.InventoryRow{
		testCubeRow(13),
		{
			Slot:       14,
			RefObjID:   23323,
			Codename:   "ITEM_COS_P_WOLF",
			TypeFlags:  0x08cc,
			StackCount: 1,
			Summon: &domain.CharacterCOS{
				GID:      7001,
				RefObjID: 19000,
				Codename: "COS_P_WOLF",
				Container: &domain.COSContainer{
					Capacity: 28,
					Rows:     []domain.InventoryRow{testCubeRow(2)},
				},
			},
		},
	}
	c.ActiveCOS = &domain.CharacterCOS{
		GID:      7002,
		RefObjID: 19001,
		Codename: "COS_P_RABBIT",
		Summoned: true,
		Container: &domain.COSContainer{
			Capacity: 28,
			Rows:     []domain.InventoryRow{testCubeRow(4)},
		},
	}
	if err := s.CreateCharacter(testDivision, "cube-account", c); err != nil {
		t.Fatal(err)
	}
	if _, err := s.TransactStorage(c, func(_ *domain.Character, storage *domain.AccountStorage) error {
		storage.Rows = []domain.InventoryRow{testCubeRow(5)}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	guildID, err := s.Guilds().CreateGuild(testDivision, domain.GuildRecord{Name: "CubeGuild", Level: 2},
		domain.GuildMemberRecord{CharID: c.ID, JID: uint32(c.ID), Name: c.Name, Grade: domain.GuildLeaderGrade}, c)
	if err != nil {
		t.Fatal(err)
	}
	if _, refusal, err := s.Guilds().TransactGuildStorageAs(testDivision, c.ID, func(_ *domain.Character, storage *domain.AccountStorage) error {
		storage.Rows = []domain.InventoryRow{testCubeRow(6)}
		return nil
	}); refusal.Refused() || err != nil {
		t.Fatalf("guild storage transact: refusal=%v err=%v", refusal, err)
	}
	reg := grounditem.NewRegistry()
	s.AttachGround(reg)
	dropped := reg.Add(testDivision, grounditem.Item{
		RefObjID:      35568,
		Codename:      "ITEM_PRE_MALL_MAGIC_CUBE_WEAPON_10",
		TypeFlags:     0x030b,
		StackCount:    1,
		Data:          3,
		Position:      grounditem.Point{RegionID: 0x62a8},
		LinkedStorage: testCubeContainer(),
	})
	s.Mutate("ground-cube-seed", nil)
	s.Close()

	// Verify the raw JSON on disk uses top-level carriers only.
	charRaw := rowRecord(t, dir, "SELECT record FROM characters WHERE division = ? AND id = ?", testDivision, c.ID)
	assertTopLevelKeyOnly(t, "character", charRaw, "cubeStorages", "linkedStorage")
	storageRaw := rowRecord(t, dir, "SELECT record FROM account_storage WHERE account_id = ?", "cube-account")
	assertTopLevelKeyOnly(t, "account_storage", storageRaw, "cubeStorages", "linkedStorage")
	guildRaw := rowRecord(t, dir, "SELECT record FROM guilds WHERE division = ? AND guild_id = ?", testDivision, guildID)
	assertTopLevelKeyOnly(t, "guild", guildRaw, "storageCubeStorages", "linkedStorage")
	groundRaw := rowRecord(t, dir, "SELECT record FROM ground_items WHERE division = ? AND gid = ?", testDivision, dropped.Gid)
	if !strings.Contains(groundRaw, `"linkedStorage"`) {
		t.Fatalf("ground_items missing top-level linkedStorage: %s", groundRaw)
	}

	reopened := openTest(t, dir, clock)
	defer reopened.Close()

	gotChar := storedCharacter(t, reopened, c.Name)
	if len(gotChar.CubeStorages) != 0 {
		t.Fatalf("reopened character retained transport CubeStorages: %+v", gotChar.CubeStorages)
	}
	assertCubeStorage(t, "bag cube", gotChar.MissionInventory[0].LinkedStorage)
	assertCubeStorage(t, "dormant pet cube", gotChar.MissionInventory[1].Summon.Container.Rows[0].LinkedStorage)
	assertCubeStorage(t, "active pet cube", gotChar.ActiveCOS.Container.Rows[0].LinkedStorage)

	gotStorage, err := reopened.AccountStorage(gotChar)
	if err != nil {
		t.Fatal(err)
	}
	assertCubeStorage(t, "account storage cube", gotStorage.Rows[0].LinkedStorage)

	gotGuild, _, ok := reopened.Guilds().Guild(testDivision, guildID)
	if !ok || gotGuild.Storage == nil || len(gotGuild.Storage.Rows) != 1 {
		t.Fatalf("reopened guild storage = %+v (ok=%v)", gotGuild.Storage, ok)
	}
	assertCubeStorage(t, "guild storage cube", gotGuild.Storage.Rows[0].LinkedStorage)

	reopenedReg := grounditem.NewRegistry()
	reopenedReg.Restore(reopened.GroundSnapshotForRestore())
	gotGround, ok := reopenedReg.Get(testDivision, dropped.Gid)
	if !ok {
		t.Fatalf("reopened ground missing dropped cube gid %d", dropped.Gid)
	}
	assertCubeStorage(t, "ground cube", gotGround.LinkedStorage)
}

/*
================
assertTopLevelKeyOnly

Verifies that raw carries wantKey at its top JSON object and does not
carry forbiddenKey anywhere inside the document.
================
*/
func assertTopLevelKeyOnly(t *testing.T, label, raw, wantKey, forbiddenKey string) {
	t.Helper()
	var top map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &top); err != nil {
		t.Fatalf("%s unmarshal: %v", label, err)
	}
	if _, ok := top[wantKey]; !ok {
		t.Fatalf("%s missing top-level %q: %s", label, wantKey, raw)
	}
	if strings.Contains(raw, `"`+forbiddenKey+`"`) {
		t.Fatalf("%s carried forbidden nested key %q: %s", label, forbiddenKey, raw)
	}
}

/*
================
assertCubeStorage
================
*/
func assertCubeStorage(t *testing.T, label string, storage *domain.COSContainer) {
	t.Helper()
	if storage == nil || storage.Capacity != 10 || len(storage.Rows) != 1 {
		t.Fatalf("%s linked storage = %+v, want capacity 10 with 1 row", label, storage)
	}
	if storage.Rows[0].Codename != "ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_WEAPON_A" || storage.Rows[0].StackCount != 3 {
		t.Fatalf("%s elixir row = %+v", label, storage.Rows[0])
	}
}
