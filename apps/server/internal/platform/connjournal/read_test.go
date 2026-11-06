/*
===========================================================================

read_test.go - the journal's read side: rows back as the census wants them

===========================================================================
*/
package connjournal

import (
	"path/filepath"
	"testing"
	"time"

	"opensro.online/server/internal/testsupport/wait"
)

/*
================
TestRowsReadsBackOpenAndClosedSpans
================
*/
func TestRowsReadsBackOpenAndClosedSpans(t *testing.T) {
	journal := openTestJournal(t, filepath.Join(t.TempDir(), "connections.sqlite"))
	journal.RecordEntered(Entry{Session: "s1", Account: "acct-a", Character: "Hero", Shard: "global-official",
		Transport: "webtransport", Address: "203.0.113.7:5000", UserAgent: "TestBrowser/1.0"})
	journal.RecordEntered(Entry{Session: "s2", Account: "acct-b", Character: "Mage", Shard: "global-official",
		Transport: "websocket", Address: "198.51.100.9:6000", UserAgent: "TestBrowser/2.0"})
	journal.CloseSession("s1")
	rows(t, journal, 2)

	found, err := journal.Rows(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 2 {
		t.Fatalf("rows: got %d, want 2", len(found))
	}
	first, second := found[0], found[1]
	if first.Session != "s1" || first.Account != "acct-a" || first.Character != "Hero" || first.Ended == 0 {
		t.Fatalf("closed row read back wrong: %+v", first)
	}
	if second.Session != "s2" || second.Ended != 0 || second.UserAgent != "TestBrowser/2.0" {
		t.Fatalf("open row read back wrong: %+v", second)
	}
	if first.IPKey == "" || first.NetKey == "" || first.IPKey == second.IPKey {
		t.Fatalf("hashes read back wrong: %q %q %q", first.IPKey, first.NetKey, second.IPKey)
	}

	future, err := journal.Rows(time.Now().Add(time.Hour).UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	if len(future) != 0 {
		t.Fatalf("rows past the bound: got %d, want 0", len(future))
	}
}

/*
================
TestRowsOnNilJournal
================
*/
func TestRowsOnNilJournal(t *testing.T) {
	var journal *Journal
	found, err := journal.Rows(0)
	if err != nil || found != nil {
		t.Fatalf("nil journal: got %v, %v; want no rows and no error", found, err)
	}
}

/*
================
TestRowsWaitsForTheWriter

Rows reads the writer's handle, so a just-recorded entry appears once
the writer applies it - the read never invents a row of its own.
================
*/
func TestRowsWaitsForTheWriter(t *testing.T) {
	journal := openTestJournal(t, filepath.Join(t.TempDir(), "connections.sqlite"))
	journal.RecordEntered(Entry{Session: "s1", Account: "acct-a", Character: "Hero", Address: "203.0.113.7:5000"})
	wait.Eventually(t, 5*time.Second, "the entered row to be readable", func() bool {
		found, err := journal.Rows(0)
		return err == nil && len(found) == 1 && found[0].Session == "s1"
	})
}
