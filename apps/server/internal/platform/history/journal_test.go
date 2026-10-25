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

/*
================
TestPlayersListsEachCharacterNewestFirst

The operator's player list: one row per account and character, newest
first, with session count, play time, online state and badly ended sessions.
================
*/
func TestPlayersListsEachCharacterNewestFirst(t *testing.T) {
	j := testJournal(t)
	for _, row := range []struct {
		id, character, kind, category string
		at                            int64
		attached                      bool
	}{
		{"w1", "Wizard", "world_entered", "", 1000, true}, {"w1", "Wizard", "ended", "expected", 61000, false},
		{"w2", "Wizard", "world_entered", "", 100000, true}, {"w2", "Wizard", "ended", "connection", 160000, false},
		{"a1", "Archer", "world_entered", "", 200000, true},
	} {
		put(t, j, Event{Session: row.id, Account: "player", Character: row.character, Kind: row.kind, Category: row.category,
			At: row.at, Attached: row.attached, InWorld: true, Lifecycle: true})
	}
	players, err := j.players()
	if err != nil {
		t.Fatal(err)
	}
	if len(players) != 2 || players[0].Character != "Archer" || players[1].Character != "Wizard" {
		t.Fatalf("players = %+v", players)
	}
	archer, wizard := players[0], players[1]
	if !archer.Online || archer.Sessions != 1 || archer.Problems != 0 {
		t.Fatalf("archer = %+v", archer)
	}
	if wizard.Online || wizard.Sessions != 2 || wizard.Problems != 1 || wizard.ConnectedSeconds != 120 {
		t.Fatalf("wizard = %+v", wizard)
	}
}

/*
================
TestSkippedInsertNamesItsProducer

#716: an insert the database skips although no event holds its id fails
with the event's kind and the probe's findings, so the operator log says
which producer lost a record instead of "sql: no rows in result set". A
trigger stands in for whatever skipped the row.
================
*/
func TestSkippedInsertNamesItsProducer(t *testing.T) {
	j := testJournal(t)
	if _, err := j.db.Exec(`CREATE TRIGGER skip_probe BEFORE INSERT ON events WHEN NEW.kind='probe' BEGIN SELECT RAISE(IGNORE); END`); err != nil {
		t.Fatal(err)
	}
	id, _ := NewID()
	err := j.write(Event{ID: id, Kind: "probe", Account: "a"})
	if err == nil || !strings.Contains(err.Error(), "probe event") || !strings.Contains(err.Error(), "loose matches 0") ||
		strings.Contains(err.Error(), "no rows in result set") {
		t.Fatalf("skipped insert error = %v", err)
	}
	put(t, j, Event{Kind: "log", Account: "a"})
}

/*
================
TestFirstLifecycleEventOfASessionIsKept

#716: a session's first lifecycle event that is not an in-world entry
(here a connection before character selection) kept updateSession's
tolerated "no session yet" lookup error, so write rolled the event and the
new session back and logged "sql: no rows in result set".
================
*/
func TestFirstLifecycleEventOfASessionIsKept(t *testing.T) {
	j := testJournal(t)
	id, _ := NewID()
	if err := j.write(Event{ID: id, Session: "fresh", Account: "player", Kind: "connected", At: 1000, Lifecycle: true}); err != nil {
		t.Fatalf("first lifecycle event refused: %v", err)
	}
	var events, sessions int
	if err := j.db.QueryRow(`SELECT count(*) FROM events WHERE id=?`, id).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if err := j.db.QueryRow(`SELECT count(*) FROM sessions WHERE id='fresh'`).Scan(&sessions); err != nil {
		t.Fatal(err)
	}
	if events != 1 || sessions != 1 {
		t.Fatalf("stored %d events and %d sessions, want 1 and 1", events, sessions)
	}
}

/*
================
TestSessionRowRecordsTransportAndFallback

A session row shows how its latest attachment connected (#1449): the
transport from the attach event, the fallback from the later report,
and a resume over the other transport replaces both.
================
*/
func TestSessionRowRecordsTransportAndFallback(t *testing.T) {
	j := testJournal(t)
	put(t, j, Event{Session: "one", Account: "player", Character: "Wizard", Kind: "connected", At: 1000, Attached: true, Lifecycle: true,
		Fields: map[string]string{"transport": "websocket"}})
	put(t, j, Event{Session: "one", Account: "player", Character: "Wizard", Kind: "transport_reported", At: 2000, Attached: true, Lifecycle: true,
		Fields: map[string]string{"transport": "websocket", "fallback": "timeout"}})
	rows, err := j.sessions(Filter{Account: "player"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Transport != "websocket" || rows[0].Fallback != "timeout" {
		t.Fatalf("session row after report: %+v", rows)
	}
	put(t, j, Event{Session: "one", Account: "player", Character: "Wizard", Kind: "resumed", At: 3000, Attached: true, Lifecycle: true,
		Fields: map[string]string{"transport": "webtransport"}})
	rows, err = j.sessions(Filter{Account: "player"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Transport != "webtransport" || rows[0].Fallback != "" {
		t.Fatalf("session row after resume: %+v", rows)
	}
}

/*
================
TestSessionTransportColumnsMigrate

A journal written before the transport/fallback columns (#1449) opens
cleanly: its session rows survive, read empty, and take the facts from
the next lifecycle event.
================
*/
func TestSessionTransportColumnsMigrate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	// The pre-#1449 schema, exactly as the old CREATE TABLE wrote it.
	if _, err := db.Exec(`CREATE TABLE sessions (id TEXT PRIMARY KEY, account TEXT NOT NULL, character TEXT NOT NULL,
 shard TEXT NOT NULL, started INTEGER NOT NULL, seen INTEGER NOT NULL, transitioned INTEGER NOT NULL, ended INTEGER NOT NULL DEFAULT 0,
 attached INTEGER NOT NULL, category TEXT NOT NULL DEFAULT '', code TEXT NOT NULL DEFAULT '', estimated INTEGER NOT NULL DEFAULT 0);
INSERT INTO sessions(id,account,character,shard,started,seen,transitioned,attached) VALUES('old','player','Wizard','shard',1000,2000,2000,1)`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	j := &Journal{db: mustOpenJournalDB(t, path)}
	if err := j.initialize(); err != nil {
		t.Fatal(err)
	}
	rows, err := j.sessions(Filter{Account: "player"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ID != "old" || rows[0].Transport != "" || rows[0].Fallback != "" {
		t.Fatalf("migrated session row: %+v", rows)
	}
	// The migrated table still records: a new session on it takes the
	// facts from its first lifecycle event.
	put(t, j, Event{Session: "new", Account: "player", Character: "Wizard", Kind: "connected", At: 3000, Attached: true, Lifecycle: true,
		Fields: map[string]string{"transport": "websocket", "fallback": "refused"}})
	rows, err = j.sessions(Filter{Account: "player"})
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]SessionRow{}
	for _, row := range rows {
		byID[row.ID] = row
	}
	if byID["new"].Transport != "websocket" || byID["new"].Fallback != "refused" {
		t.Fatalf("session row on migrated table: %+v", rows)
	}
}

/*
================
mustOpenJournalDB
================
*/
func mustOpenJournalDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	return db
}
