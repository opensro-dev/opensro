/*
===========================================================================
journal_test.go - durable receipts, playtime accounting and operator access
===========================================================================
*/
package history

import (
	"context"
	"database/sql"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

/*
================
testJournal

Direct writes provide deterministic timestamps without a running checkpoint.
================
*/
func testJournal(t *testing.T) *Journal {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "history.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	j := &Journal{db: db}
	if err := j.initialize(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return j
}

/*
================
put
================
*/
func put(t *testing.T, j *Journal, e Event) {
	t.Helper()
	e.ID, _ = NewID()
	if err := j.write(e); err != nil {
		t.Fatal(err)
	}
}

/*
================
TestConnectedTimeExcludesGraceAndMergesConcurrentSessions
================
*/
func TestConnectedTimeExcludesGraceAndMergesConcurrentSessions(t *testing.T) {
	j := testJournal(t)
	for _, row := range []struct {
		id, kind string
		at       int64
		attached bool
	}{
		{"one", "world_entered", 1000, true}, {"one", "detached", 61000, false},
		{"one", "resumed", 91000, true}, {"one", "ended", 151000, false},
		{"two", "world_entered", 31000, true}, {"two", "ended", 121000, false},
	} {
		put(t, j, Event{Session: row.id, Account: "player", Character: "Wizard", Kind: row.kind, At: row.at, Attached: row.attached, InWorld: true, Lifecycle: true})
	}
	sessions, err := j.sessions(Filter{Account: "player"})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range sessions {
		if s.ID == "one" && s.ConnectedSeconds != 120 {
			t.Fatalf("grace counted: %+v", s)
		}
	}
	summary, err := j.summary(Filter{Account: "PLAYER"}, time.UnixMilli(200000))
	if err != nil {
		t.Fatal(err)
	}
	if summary["connectedSeconds"] != float64(150) {
		t.Fatalf("overlap not merged: %v", summary)
	}
}

/*
================
TestCrashRecoveryAndDetachedCheckpoint
================
*/
func TestCrashRecoveryAndDetachedCheckpoint(t *testing.T) {
	j := testJournal(t)
	put(t, j, Event{Session: "one", Account: "player", Character: "Wizard", Kind: "world_entered", At: 1000, Attached: true, InWorld: true, Lifecycle: true})
	if err := j.checkpoint(16000); err != nil {
		t.Fatal(err)
	}
	put(t, j, Event{Session: "one", Account: "player", Character: "Wizard", Kind: "detached", At: 21000, Lifecycle: true})
	if err := j.checkpoint(31000); err != nil {
		t.Fatal(err)
	}
	if err := j.initialize(); err != nil {
		t.Fatal(err)
	}
	sessions, err := j.sessions(Filter{Account: "player"})
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || !sessions[0].Estimated || sessions[0].Seen != 21000 || sessions[0].ConnectedSeconds != 20 {
		t.Fatalf("incorrect recovery: %+v", sessions)
	}
	data, err := j.Query(Filter{Session: "one", Kind: "ended"})
	if err != nil {
		t.Fatal(err)
	}
	if len(data["events"].([]EventRow)) != 1 {
		t.Fatal("missing recovered ending")
	}
}

/*
================
TestConfirmedIncidentIsDurableAndIdempotent
================
*/
func TestConfirmedIncidentIsDurableAndIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.sqlite")
	j, err := Open(path, "agent", "realm", "build-one")
	if err != nil {
		t.Fatal(err)
	}
	id, _ := NewID()
	e := Event{ID: id, Account: "player", Kind: "client_incident", Category: "software", Code: "packet_application_failed"}
	for range 2 {
		if err := j.RecordConfirmed(context.Background(), e); err != nil {
			t.Fatal(err)
		}
	}
	e.Account = "another"
	if err := j.RecordConfirmed(context.Background(), e); err == nil {
		t.Fatal("another account reused receipt")
	}
	if err := j.Close(); err != nil {
		t.Fatal(err)
	}
	j, err = Open(path, "agent", "realm", "build-two")
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	data, err := j.Query(Filter{Incident: id})
	if err != nil {
		t.Fatal(err)
	}
	events := data["events"].([]EventRow)
	if len(events) != 1 || events[0].Account != "player" || events[0].Build != "build-one" {
		t.Fatalf("receipt not durable: %+v", events)
	}
}

/*
================
TestOperatorHistoryRequiresLocalAuthenticatedRequest
================
*/
func TestOperatorHistoryRequiresLocalAuthenticatedRequest(t *testing.T) {
	j := testJournal(t)
	path := filepath.Join(t.TempDir(), "operator-token")
	token := strings.Repeat("x", 32)
	if err := os.WriteFile(path, []byte(token), 0600); err != nil {
		t.Fatal(err)
	}
	handler, err := OperatorHandler(j, path)
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"", "Authorization", "Origin", "X-Forwarded-For", "Host"} {
		r := httptest.NewRequest("GET", "http://127.0.0.1/internal/operations/history", nil)
		r.RemoteAddr = "127.0.0.1:1234"
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("X-SRO-Local-Diagnostics", "1")
		if bad == "Host" {
			r.Host = "attacker.test"
		} else if bad != "" {
			r.Header.Set(bad, "untrusted")
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		want := 403
		if bad == "" {
			want = 200
		}
		if w.Code != want {
			t.Fatalf("%s: got %d: %s", bad, w.Code, w.Body.String())
		}
	}
}

/*
================
TestCharacterLookupDoesNotBorrowAnotherAccountsLogin
================
*/
func TestCharacterLookupDoesNotBorrowAnotherAccountsLogin(t *testing.T) {
	j := testJournal(t)
	put(t, j, Event{Account: "other", Kind: "login_succeeded", At: 2000})
	data, err := j.summary(Filter{Character: "Wizard"}, time.UnixMilli(3000))
	if err != nil {
		t.Fatal(err)
	}
	if data["lastAuthentication"] != int64(0) {
		t.Fatalf("unrelated login: %v", data)
	}
}

/*
================
TestCheckpointDoesNotSuppressQueuedDetach
================
*/
func TestCheckpointDoesNotSuppressQueuedDetach(t *testing.T) {
	j := testJournal(t)
	put(t, j, Event{Session: "one", Account: "player", Character: "Wizard", Kind: "world_entered", At: 1000, Attached: true, InWorld: true, Lifecycle: true})
	if err := j.checkpoint(31000); err != nil {
		t.Fatal(err)
	}
	put(t, j, Event{Session: "one", Account: "player", Character: "Wizard", Kind: "detached", At: 21000, Lifecycle: true})
	put(t, j, Event{Session: "one", Account: "player", Character: "Wizard", Kind: "ended", At: 51000, Lifecycle: true})
	rows, err := j.sessions(Filter{Account: "player"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ConnectedSeconds != 20 || rows[0].Seen != 21000 || rows[0].Ended != 51000 {
		t.Fatalf("checkpoint replaced lifecycle evidence: %+v", rows)
	}
}

/*
================
TestFailedCheckpointRetainsLossCounters
================
*/
func TestFailedCheckpointRetainsLossCounters(t *testing.T) {
	j := testJournal(t)
	j.dropped.Add(3)
	j.failed.Add(2)
	if err := j.db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := j.checkpoint(31000); err == nil {
		t.Fatal("closed DB checkpoint succeeded")
	}
	if j.dropped.Load() != 3 || j.failed.Load() != 2 {
		t.Fatal("failed flush erased loss counters")
	}
}
