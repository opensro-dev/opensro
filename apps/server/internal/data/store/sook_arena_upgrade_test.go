/*
===========================================================================

sook_arena_upgrade_test.go - schema 41 retains every pre-arena character byte

===========================================================================
*/
package store

import "testing"

/*
================
TestSookArenaUpgradePreservesCharacter
================
*/
func TestSookArenaUpgradePreservesCharacter(t *testing.T) {
	dir := t.TempDir()
	s := openTest(t, dir, newTestClock())
	c := seededCharacter()
	if err := s.CreateCharacter(testDivision, "account", c); err != nil {
		t.Fatal(err)
	}
	var before string
	if err := s.db.QueryRow("SELECT record FROM characters WHERE division = ? AND id = ?", testDivision, c.ID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	s.Close()
	rewriteDatabaseMeta(t, dir, metaKeySchemaVersion, 40)
	if backup, err := UpgradeAuthority(dir, true); err != nil || backup == "" {
		t.Fatalf("upgrade: %s %v", backup, err)
	}
	s = openTest(t, dir, newTestClock())
	defer s.Close()
	var after string
	if err := s.db.QueryRow("SELECT record FROM characters WHERE division = ? AND id = ?", testDivision, c.ID).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatal("upgrade rewrote character, inventory or companion data")
	}
}
