/*
===========================================================================

sqlitestore_test.go - settings.db keeps the Store contract

===========================================================================
*/
package settingsapi

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"opensro.online/server/internal/game/tunables"
)

/*
================
openTestStore
================
*/
func openTestStore(t *testing.T) (*SQLiteStore, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), FileName)
	s, err := OpenSQLiteStore(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, path
}

/*
================
TestSQLiteStoreKeepsTheContract

Propose refuses a second pending proposal for one key; Decide removes the
proposal, stores the override and appends the audit in one commit; a
refusing check changes nothing; the audit pages newest first.
================
*/
func TestSQLiteStoreKeepsTheContract(t *testing.T) {
	s, _ := openTestStore(t)
	p := Proposal{ID: "p1", Key: tunables.JobKillNoticesPerMinute, From: "6", To: "3", By: "discord:1", At: 10, Reason: "x"}
	if err := s.Propose(p, AuditEntry{At: 10, Who: "discord:1", Action: ActionPropose, Key: p.Key, Result: ResultPending}); err != nil {
		t.Fatal(err)
	}
	if err := s.Propose(Proposal{ID: "p2", Key: p.Key, To: "4", At: 11}, AuditEntry{At: 11}); !errors.Is(err, ErrConflict) {
		t.Fatalf("second pending proposal: %v", err)
	}
	refused := errors.New("no")
	if _, err := s.Decide("p1", func(Proposal) (AuditEntry, bool, error) { return AuditEntry{}, true, refused }); !errors.Is(err, refused) {
		t.Fatalf("refusing check: %v", err)
	}
	if pending, _ := s.Pending(); len(pending) != 1 {
		t.Fatal("a refusing check removed the proposal")
	}
	got, err := s.Decide("p1", func(p Proposal) (AuditEntry, bool, error) {
		return AuditEntry{At: 12, Who: "discord:2", Action: ActionApprove, Key: p.Key, To: p.To, Result: ResultApplied}, true, nil
	})
	if err != nil || got.ID != "p1" {
		t.Fatalf("decide %+v %v", got, err)
	}
	overrides, _ := s.Overrides()
	pending, _ := s.Pending()
	if overrides[p.Key] != "3" || len(pending) != 0 {
		t.Fatalf("overrides %v pending %v", overrides, pending)
	}
	if _, err := s.Decide("p1", nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a decided id: %v", err)
	}
	entries, _ := s.Audit(10, 0)
	if len(entries) != 2 || entries[0].Action != ActionApprove || entries[1].Action != ActionPropose {
		t.Fatalf("audit %+v", entries)
	}
	if older, _ := s.Audit(10, entries[0].Seq); len(older) != 1 || older[0].Action != ActionPropose {
		t.Fatalf("paging %+v", older)
	}
}

/*
================
TestSQLiteStoreAuditIsAppendOnly
================
*/
func TestSQLiteStoreAuditIsAppendOnly(t *testing.T) {
	s, _ := openTestStore(t)
	if err := s.Refuse(AuditEntry{At: 1, Who: "discord:1", Action: ActionPropose, Result: ResultRefused}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`UPDATE audit SET who = 'x'`); err == nil {
		t.Fatal("an audit row was updated")
	}
	if _, err := s.db.Exec(`DELETE FROM audit`); err == nil {
		t.Fatal("an audit row was deleted")
	}
}

/*
================
TestSQLiteStoreSurvivesAReopenAndRefusesANewerFile
================
*/
func TestSQLiteStoreSurvivesAReopenAndRefusesANewerFile(t *testing.T) {
	s, path := openTestStore(t)
	p := Proposal{ID: "p1", Key: tunables.JobKillRepeatMinutes, To: "20", By: "discord:1", At: 1, Reason: "r"}
	if err := s.Propose(p, AuditEntry{At: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Decide("p1", func(Proposal) (AuditEntry, bool, error) { return AuditEntry{At: 2}, true, nil }); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	again, err := OpenSQLiteStore(path)
	if err != nil {
		t.Fatal(err)
	}
	overrides, _ := again.Overrides()
	_ = again.Close()
	if overrides[tunables.JobKillRepeatMinutes] != "20" {
		t.Fatalf("override lost across a reopen: %v", overrides)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`PRAGMA user_version = 99`); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	if newer, err := OpenSQLiteStore(path); err == nil {
		_ = newer.Close()
		t.Fatal("a file from a newer schema was opened")
	}
}
