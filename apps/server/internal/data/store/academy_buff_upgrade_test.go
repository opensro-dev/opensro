/*
===========================================================================

academy_buff_upgrade_test.go - the schema 37 step (Academy cumulated buff)

===========================================================================
*/
package store

import (
	"errors"
	"testing"
)

/*
================
TestAcademyBuffUpgradeRewritesNothing

A schema 36 authority upgrades to 37 with its camp records untouched
(the cumulated-buff state is optional and absent, so its members hold
no buff); a second upgrade has nothing to do.
================
*/
func TestAcademyBuffUpgradeRewritesNothing(t *testing.T) {
	dir := t.TempDir()
	s := openTest(t, dir, newTestClock())
	master := seededCharacter()
	if err := s.CreateCharacter(testDivision, "account", master); err != nil {
		t.Fatal(err)
	}
	camp := openTestCamp(t, s, master)
	var before string
	if err := s.db.QueryRow("SELECT record FROM training_camp_members WHERE division = ? AND camp_id = ? AND char_id = ?", testDivision, camp.ID, master.ID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	s.Close()
	rewriteDatabaseMeta(t, dir, metaKeySchemaVersion, preAcademyBuffVersion)
	if backup, err := UpgradeAuthority(dir, true); err != nil || backup == "" {
		t.Fatalf("upgrade %q: %v", backup, err)
	}
	reopened := openTest(t, dir, newTestClock())
	var after string
	if err := reopened.db.QueryRow("SELECT record FROM training_camp_members WHERE division = ? AND camp_id = ? AND char_id = ?", testDivision, camp.ID, master.ID).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatal("the upgrade rewrote the camp member")
	}
	_, members, ok := reopened.TrainingCamps().Camp(testDivision, camp.ID)
	if !ok || len(members) != 1 {
		t.Fatalf("camp after upgrade: ok %v members %v", ok, members)
	}
	if members[0].BuffLive || members[0].BuffValue != 0 || members[0].BuffWindowStartMs != 0 || members[0].ReservedBuffs != nil {
		t.Fatalf("member holds a buff after the upgrade: %+v", members[0])
	}
	reopened.Close()
	if _, err := UpgradeAuthority(dir, true); !errors.Is(err, ErrAuthorityCurrent) {
		t.Fatalf("second upgrade = %v, want ErrAuthorityCurrent", err)
	}
}
