/*
===========================================================================

authority_upgrade_test.go - preserving upgrades, writer exclusion and recovery

===========================================================================
*/
package store

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"opensro.online/server/internal/domain"
)

/*
================
makePreMallAuthority
================
*/
func makePreMallAuthority(t *testing.T, dir string) {
	t.Helper()
	s := openTest(t, dir, newTestClock())
	if err := s.CreateCharacter(testDivision, "account'; --", seededCharacter()); err != nil {
		t.Fatal(err)
	}
	s.Close()
	db, err := connectDB(filepath.Join(dir, DBFileName))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec("DROP TABLE mall_accounts; DROP TABLE account_storage"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE meta SET value = ? WHERE key = ?", preMallLayoutVersion, metaKeyLayoutVersion); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE meta SET value = ? WHERE key = ?", UpgradeFromVersion, metaKeySchemaVersion); err != nil {
		t.Fatal(err)
	}
}

/*
================
TestMallUpgradePreservesAuthorityAndBackup
================
*/
func TestMallUpgradePreservesAuthorityAndBackup(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "quoted' authority")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	makePreMallAuthority(t, dir)
	before, err := os.ReadFile(filepath.Join(dir, DBFileName))
	if err != nil {
		t.Fatal(err)
	}
	if backup, err := UpgradeAuthority(dir, false); err != nil || backup != "" {
		t.Fatalf("dry run: %q %v", backup, err)
	}
	after, err := os.ReadFile(filepath.Join(dir, DBFileName))
	if err != nil || string(before) != string(after) {
		t.Fatal("dry run changed database bytes", err)
	}
	backup, err := UpgradeAuthority(dir, true)
	if err != nil || backup == "" {
		t.Fatalf("upgrade: %q %v", backup, err)
	}
	old, err := connectDB(backup)
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	oldGraph, err := loadDB(old, UpgradeFromVersion, preMallLayoutVersion)
	if err != nil || len(oldGraph.characters[testDivision]) != 1 {
		t.Fatal("backup does not restore the pre-upgrade graph", err)
	}
	s := openTest(t, dir, newTestClock())
	characters := s.Characters().CharactersForDivision(testDivision)
	if len(characters) != 1 || characters[0].AccountID != "account'; --" || len(characters[0].MissionInventory) == 0 {
		t.Fatalf("upgrade lost character or inventory: %+v", characters)
	}
	balance, err := s.MallBalance(characters[0])
	if err != nil || balance.Silk != 0 || balance.GiftSilk != 0 || balance.Points != 0 {
		t.Fatalf("unexpected initial currency: %+v %v", balance, err)
	}
	if _, err := UpgradeAuthority(dir, true); err == nil {
		t.Fatal("upgrade admitted a live authority")
	}
	s.Close()
	// A retried release finds the upgrade done and changes nothing.
	if backup, err := UpgradeAuthority(dir, true); !errors.Is(err, ErrAuthorityCurrent) || backup != "" {
		t.Fatalf("already upgraded authority: %q %v", backup, err)
	}
}

/*
================
TestMallUpgradeRefusesInvalidSource
================
*/
func TestMallUpgradeRefusesInvalidSource(t *testing.T) {
	dir := t.TempDir()
	makePreMallAuthority(t, dir)
	db, err := connectDB(filepath.Join(dir, DBFileName))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE characters SET name_lower = ?", "mismatched index"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if backup, err := UpgradeAuthority(dir, true); err == nil || backup != "" {
		t.Fatalf("invalid graph reached mutation: %q %v", backup, err)
	}
}

/*
================
TestMallUpgradeRollsBackSchemaOnCommitFailure
================
*/
func TestMallUpgradeRollsBackSchemaOnCommitFailure(t *testing.T) {
	dir := t.TempDir()
	makePreMallAuthority(t, dir)
	db, err := connectDB(filepath.Join(dir, DBFileName))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TRIGGER refuse_layout_upgrade BEFORE UPDATE ON meta
WHEN NEW.key = 'layoutVersion' BEGIN SELECT RAISE(ABORT, 'injected failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	backup, err := UpgradeAuthority(dir, true)
	if err == nil || backup == "" {
		t.Fatalf("did not preserve recovery after failure: %q %v", backup, err)
	}
	db, err = connectDB(filepath.Join(dir, DBFileName))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := loadDB(db, UpgradeFromVersion, preMallLayoutVersion); err != nil {
		t.Fatal("failed upgrade changed the original authority", err)
	}
	for _, table := range []string{"mall_accounts", "account_storage"} {
		var tables int
		if err := db.QueryRow("SELECT count(*) FROM sqlite_schema WHERE name = ?", table).Scan(&tables); err != nil || tables != 0 {
			t.Fatalf("failed upgrade retained a partial %s table: %d %v", table, tables, err)
		}
	}
}

/*
================
TestCompanionUpgradePreservesExistingWarehouseAndBackup

Schema 14 already owns the account tables. The upgrade must preserve their
contents, validate the existing graph, and only advance the record version.
================
*/
func TestCompanionUpgradePreservesExistingWarehouseAndBackup(t *testing.T) {
	dir := t.TempDir()
	s := openTest(t, dir, newTestClock())
	c := seededCharacter()
	if err := s.CreateCharacter(testDivision, "account'; --", c); err != nil {
		t.Fatal(err)
	}
	if _, err := s.TransactStorage(c, func(next *domain.Character, storage *domain.AccountStorage) error {
		row := next.MissionInventory[0]
		row.Slot = 0
		storage.Rows = []domain.InventoryRow{row}
		next.MissionInventory = next.MissionInventory[1:]
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	s.Close()
	db, err := connectDB(filepath.Join(dir, DBFileName))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE meta SET value = ? WHERE key = ?", preCompanionVersion, metaKeySchemaVersion); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(dir, DBFileName))
	if err != nil {
		t.Fatal(err)
	}
	if backup, err := UpgradeAuthority(dir, false); err != nil || backup != "" {
		t.Fatal("dry run", backup, err)
	}
	after, err := os.ReadFile(filepath.Join(dir, DBFileName))
	if err != nil || string(before) != string(after) {
		t.Fatal("dry run rewrote schema 14", err)
	}
	backup, err := UpgradeAuthority(dir, true)
	if err != nil || backup == "" {
		t.Fatal("upgrade", backup, err)
	}
	old, err := connectDB(backup)
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	if _, err := loadDB(old, preCompanionVersion, CurrentLayoutVersion); err != nil {
		t.Fatal("backup invalid", err)
	}
	reopened := openTest(t, dir, newTestClock())
	characters := reopened.Characters().CharactersForDivision(testDivision)
	if len(characters) != 1 {
		t.Fatal("character missing")
	}
	storage, err := reopened.AccountStorage(characters[0])
	if err != nil || len(storage.Rows) != 1 || storage.Rows[0].Slot != 0 {
		t.Fatal("warehouse lost", storage, err)
	}
}
