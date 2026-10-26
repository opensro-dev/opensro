/*
===========================================================================

binding_upgrade_test.go - preserve ordinary stock while adding durable binding

Schema 32 adds optional item-instance binding without rewriting old records.
The upgrade is offline; new bound rewards must survive subsequent restarts.

===========================================================================
*/
package store

import (
	"errors"
	"path/filepath"
	"testing"
)

/*
================
TestBoundRewardsUpgradePreservesOrdinaryStock
================
*/
func TestBoundRewardsUpgradePreservesOrdinaryStock(t *testing.T) {
	dir := t.TempDir()
	s := openTest(t, dir, newTestClock())
	c := seededCharacter()
	if err := s.CreateCharacter(testDivision, "account", c); err != nil {
		t.Fatal(err)
	}
	before := readCharacterRecordRaw(t, s, testDivision, c.Name)
	s.Close()
	rewriteDatabaseMeta(t, dir, metaKeySchemaVersion, preBoundRewardsVersion)

	if backup, err := UpgradeAuthority(dir, false); err != nil || backup != "" {
		t.Fatalf("validate %q: %v", backup, err)
	}
	db, err := connectDB(filepath.Join(dir, DBFileName))
	if err != nil {
		t.Fatal(err)
	}
	version, err := readMetaInt(db, metaKeySchemaVersion, 0)
	db.Close()
	if err != nil || version != preBoundRewardsVersion {
		t.Fatalf("validation changed version to %d: %v", version, err)
	}
	backup, err := UpgradeAuthority(dir, true)
	if err != nil || backup == "" {
		t.Fatalf("upgrade %q: %v", backup, err)
	}
	backupDB, err := connectDB(backup)
	if err != nil {
		t.Fatal(err)
	}
	backupVersion, err := readMetaInt(backupDB, metaKeySchemaVersion, 0)
	backupDB.Close()
	if err != nil || backupVersion != preBoundRewardsVersion {
		t.Fatalf("backup version = %d: %v", backupVersion, err)
	}

	reopened := openTest(t, dir, newTestClock())
	if after := readCharacterRecordRaw(t, reopened, testDivision, c.Name); after != before {
		t.Fatal("upgrade rewrote the character record")
	}
	characters := reopened.Characters().CharactersForDivision(testDivision)
	if len(characters) != 1 || len(characters[0].MissionInventory) != 1 {
		t.Fatal("upgrade lost the character or inventory")
	}
	character := characters[0]
	if character.MissionInventory[0].CharacterBound {
		t.Fatal("upgrade bound ordinary inventory")
	}
	reopened.MutateCharacter(character, "test-bind-reward", func() {
		character.MissionInventory[0].CharacterBound = true
	})
	reopened.Close()
	restarted := openTest(t, dir, newTestClock())
	if !restarted.Characters().CharactersForDivision(testDivision)[0].MissionInventory[0].CharacterBound {
		t.Fatal("new binding was lost across restart")
	}
	restarted.Close()
	if _, err := UpgradeAuthority(dir, true); !errors.Is(err, ErrAuthorityCurrent) {
		t.Fatalf("second upgrade = %v, want ErrAuthorityCurrent", err)
	}
}
