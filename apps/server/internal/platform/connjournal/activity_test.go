/*
===========================================================================

activity_test.go - a row's traffic summary, stored and read back

A summary written at the row's close reads back as written, at a
character switch and at the session's end; a row closed without one
(a crash) reads nil, never zero traffic; past maxStoredOpcodes the rest
are summed under "other"; and a file from before the columns gains them.

===========================================================================
*/
package connjournal

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
	"opensro.online/server/internal/testsupport/wait"
)

/*
================
TestActivityIsStoredOnTheRowItCloses
================
*/
func TestActivityIsStoredOnTheRowItCloses(t *testing.T) {
	t.Parallel()

	journal := openTestJournal(t, filepath.Join(t.TempDir(), "connections.sqlite"))
	first := &Activity{FramesIn: 40, FirstHandledMs: 900, Opcodes: []OpcodeActivity{
		{Opcode: 0x3012, Count: 1, FirstMs: 2000}, {Opcode: 0x7034, Count: 12, FirstMs: 900}}}
	second := &Activity{FramesIn: 5, FirstHandledMs: -1, Opcodes: []OpcodeActivity{}}
	journal.RecordEntered(Entry{Session: "s1", Account: "alice", Character: "Hero", Transport: "webtransport"})
	journal.RecordEntered(Entry{Session: "s1", Account: "alice", Character: "Mage", Transport: "webtransport", Closing: first})
	journal.CloseSession("s1", second)
	found := closedRows(t, journal, 2)

	got := found[0].Activity
	if got == nil || got.FramesIn != 40 || got.FirstHandledMs != 900 || len(got.Opcodes) != 2 ||
		got.Opcodes[0] != (OpcodeActivity{Opcode: 0x7034, Count: 12, FirstMs: 900}) {
		t.Fatalf("first span %+v", got)
	}
	if got := found[1].Activity; got == nil || got.FramesIn != 5 || got.FirstHandledMs != -1 || len(got.Opcodes) != 0 {
		t.Fatalf("second span %+v", got)
	}
}

/*
================
TestActivityAbsentReadsNil

A close without a summary, and a crash's estimated close, leave the
row's summary unrecorded: nil, not zero frames.
================
*/
func TestActivityAbsentReadsNil(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "connections.sqlite")
	journal := openTestJournal(t, path)
	journal.RecordEntered(Entry{Session: "s1", Account: "alice", Character: "Hero"})
	journal.CloseSession("s1", nil)
	journal.RecordEntered(Entry{Session: "s2", Account: "bob", Character: "Mage"})
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	reopened := openTestJournal(t, path) // closes s2 as estimated
	found := closedRows(t, reopened, 2)
	for _, row := range found {
		if row.Activity != nil {
			t.Fatalf("row %s read a summary %+v", row.Session, row.Activity)
		}
	}
}

/*
================
TestActivityKeepsTheMostFrequentOpcodes
================
*/
func TestActivityKeepsTheMostFrequentOpcodes(t *testing.T) {
	t.Parallel()

	activity := &Activity{FramesIn: 1000, FirstHandledMs: 0}
	for i := range maxStoredOpcodes + 8 {
		activity.Opcodes = append(activity.Opcodes, OpcodeActivity{Opcode: uint16(0x7000 + i), Count: uint32(100 - i), FirstMs: int64(i)})
	}
	decoded, err := decodeActivity(1000, 0, encodeOpcodes(activity))
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded.Opcodes) != maxStoredOpcodes || decoded.Opcodes[0].Opcode != 0x7000 {
		t.Fatalf("kept %d opcodes, first %#x", len(decoded.Opcodes), decoded.Opcodes[0].Opcode)
	}
	var dropped uint32
	for i := maxStoredOpcodes; i < maxStoredOpcodes+8; i++ {
		dropped += uint32(100 - i)
	}
	if decoded.Other != dropped {
		t.Fatalf("other = %d, want %d", decoded.Other, dropped)
	}
}

/*
================
TestActivityColumnsAreAddedToAnOlderFile
================
*/
func TestActivityColumnsAreAddedToAnOlderFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "connections.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE connections (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 session TEXT NOT NULL, account TEXT NOT NULL, character TEXT NOT NULL, shard TEXT NOT NULL,
 started INTEGER NOT NULL, seen INTEGER NOT NULL, ended INTEGER NOT NULL DEFAULT 0,
 estimated INTEGER NOT NULL DEFAULT 0, transport TEXT NOT NULL DEFAULT '',
 ip_key TEXT NOT NULL DEFAULT '', net_key TEXT NOT NULL DEFAULT '',
 build TEXT NOT NULL DEFAULT '', user_agent TEXT NOT NULL DEFAULT '', screen TEXT NOT NULL DEFAULT '');
INSERT INTO connections(session,account,character,shard,started,seen,ended) VALUES('old','a','Hero','global-official',1,2,3);`); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()

	journal := openTestJournal(t, path)
	found, err := journal.Rows(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 || found[0].Activity != nil {
		t.Fatalf("the older row read %+v", found)
	}
}

// closedRows waits until want rows have closed and returns every row.
func closedRows(t *testing.T, journal *Journal, want int) []Row {
	t.Helper()
	var found []Row
	wait.Eventually(t, 5*time.Second, "rows to close", func() bool {
		var err error
		found, err = journal.Rows(0)
		if err != nil {
			t.Fatal(err)
		}
		closed := 0
		for _, row := range found {
			if row.Ended != 0 {
				closed++
			}
		}
		return closed >= want
	})
	return found
}
