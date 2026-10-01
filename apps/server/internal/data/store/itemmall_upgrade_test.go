/*
===========================================================================

itemmall_upgrade_test.go - preserving upgrades, writer exclusion and recovery

===========================================================================
*/
package store

import (
	"os"
	"path/filepath"
	"testing"
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
	if _, err := db.Exec("DROP TABLE mall_accounts"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE meta SET value = ? WHERE key = ?", preMallLayoutVersion, metaKeyLayoutVersion); err != nil {
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
	if backup, err := UpgradeMallAuthority(dir, false); err != nil || backup != "" {
		t.Fatalf("dry run: %q %v", backup, err)
	}
	after, err := os.ReadFile(filepath.Join(dir, DBFileName))
	if err != nil || string(before) != string(after) {
		t.Fatal("dry run changed database bytes", err)
	}
	backup, err := UpgradeMallAuthority(dir, true)
	if err != nil || backup == "" {
		t.Fatalf("upgrade: %q %v", backup, err)
	}
	old, err := connectDB(backup)
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	oldGraph, err := loadDB(old, CurrentVersion, preMallLayoutVersion)
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
	if _, err := UpgradeMallAuthority(dir, true); err == nil {
		t.Fatal("upgrade admitted a live authority")
	}
	s.Close()
	if _, err := UpgradeMallAuthority(dir, true); err == nil {
		t.Fatal("upgrade admitted an already upgraded authority")
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
	if backup, err := UpgradeMallAuthority(dir, true); err == nil || backup != "" {
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
	backup, err := UpgradeMallAuthority(dir, true)
	if err == nil || backup == "" {
		t.Fatalf("did not preserve recovery after failure: %q %v", backup, err)
	}
	db, err = connectDB(filepath.Join(dir, DBFileName))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := loadDB(db, CurrentVersion, preMallLayoutVersion); err != nil {
		t.Fatal("failed upgrade changed the original authority", err)
	}
	var tables int
	if err := db.QueryRow("SELECT count(*) FROM sqlite_schema WHERE name = ?", "mall_accounts").Scan(&tables); err != nil || tables != 0 {
		t.Fatalf("failed upgrade retained a partial currency table: %d %v", tables, err)
	}
}
