/*
===========================================================================

journal_test.go - the census journal: hashes, pairing, recovery, pruning

The hashes are keyed (keyed and stable within a key is the whole
privacy claim), entered rows pair with their close, a crash closes at
the last checkpoint as estimated, and closed rows are pruned at 30
days while open ones are kept.
===========================================================================
*/
package connjournal

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"opensro.online/server/internal/testsupport/wait"
)

// testKey is the one key most tests journal under.
var testKey = []byte("census-test-key-32-bytes-exactly!!")

/*
================
storedConnection

One connections row as the tests read it back.
================
*/
type storedConnection struct {
	session, account, character, transport string
	started, seen, ended                   int64
	estimated                              int64
	ipKey, netKey                          string
	userAgent                              string
}

/*
================
openTestJournal
================
*/
func openTestJournal(t *testing.T, path string) *Journal {
	t.Helper()
	journal, err := Open(path, "global-official", testKey)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = journal.Close() })
	return journal
}

/*
================
rows

Every stored row, oldest first. Waits until the writer has applied at
least want rows: RecordEntered only enqueues.
================
*/
func rows(t *testing.T, journal *Journal, want int) []storedConnection {
	t.Helper()
	var found []storedConnection
	wait.Eventually(t, 5*time.Second, "writer to apply rows", func() bool {
		var err error
		found, err = readRows(journal)
		if err != nil {
			t.Fatal(err)
		}
		return len(found) >= want
	})
	return found
}

/*
================
readRows
================
*/
func readRows(journal *Journal) ([]storedConnection, error) {
	result, err := journal.db.Query(`SELECT session,account,character,transport,started,seen,ended,estimated,ip_key,net_key,user_agent
FROM connections ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer result.Close()
	var found []storedConnection
	for result.Next() {
		var row storedConnection
		if err := result.Scan(&row.session, &row.account, &row.character, &row.transport,
			&row.started, &row.seen, &row.ended, &row.estimated, &row.ipKey, &row.netKey, &row.userAgent); err != nil {
			return nil, err
		}
		found = append(found, row)
	}
	return found, result.Err()
}

/*
================
waitEnded

Waits until the session's row is closed and returns it.
================
*/
func waitEnded(t *testing.T, journal *Journal, session string) storedConnection {
	t.Helper()
	var closed storedConnection
	wait.Eventually(t, 5*time.Second, "session row to close", func() bool {
		found, err := readRows(journal)
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range found {
			if row.session == session && row.ended != 0 {
				closed = row
				return true
			}
		}
		return false
	})
	return closed
}

/*
================
TestKeyHashesAreKeyedStableAndGrouped
================
*/
func TestKeyHashesAreKeyedStableAndGrouped(t *testing.T) {
	ipKey, netKey := keyHashes(testKey, "203.0.113.9")
	if ipKey == "" || netKey == "" || ipKey == netKey {
		t.Fatalf("hashes = %q, %q", ipKey, netKey)
	}
	// Stable within the key, also through a host:port peer form.
	if again, _ := keyHashes(testKey, "203.0.113.9:5000"); again != ipKey {
		t.Fatalf("same address hashed differently: %q != %q", again, ipKey)
	}
	// Another key names another client entirely.
	if other, _ := keyHashes([]byte("another-key"), "203.0.113.9"); other == ipKey {
		t.Fatal("hash does not depend on the key")
	}
	// Same /24: different addresses, one network.
	_, neighbourNet := keyHashes(testKey, "203.0.113.200")
	if neighbourNet != netKey {
		t.Fatal("two addresses in one /24 hashed to different networks")
	}
	// Another /24: another network.
	if _, otherNet := keyHashes(testKey, "203.0.114.9"); otherNet == netKey {
		t.Fatal("two /24 networks hashed alike")
	}
	// IPv6 groups at /48.
	_, v6NetA := keyHashes(testKey, "2001:db8:1:2::1")
	_, v6NetB := keyHashes(testKey, "2001:db8:1:9::5")
	_, v6NetC := keyHashes(testKey, "2001:db8:2::1")
	if v6NetA != v6NetB || v6NetA == v6NetC {
		t.Fatalf("ipv6 networks: %q %q %q", v6NetA, v6NetB, v6NetC)
	}
	// A stand-in peer hashes to nothing, never to a guess.
	if ip, net := keyHashes(testKey, "fake:0"); ip != "" || net != "" {
		t.Fatalf("unparseable address hashed: %q %q", ip, net)
	}
}

/*
================
TestEnteredRowClosesAtSessionEnd
================
*/
func TestEnteredRowClosesAtSessionEnd(t *testing.T) {
	journal := openTestJournal(t, filepath.Join(t.TempDir(), "connections.sqlite"))
	journal.RecordEntered(Entry{Session: "boot:1", Account: "alice", Character: "Hero",
		Shard: "global-official", Transport: "websocket", Address: "203.0.113.9", UserAgent: "TestBrowser/1.0"})
	open := rows(t, journal, 1)[0]
	if open.ended != 0 || open.account != "alice" || open.character != "Hero" || open.transport != "websocket" {
		t.Fatalf("open row = %+v", open)
	}
	if want, _ := keyHashes(testKey, "203.0.113.9"); open.ipKey != want {
		t.Fatalf("ip key = %q, want %q", open.ipKey, want)
	}
	journal.CloseSession("boot:1")
	closed := waitEnded(t, journal, "boot:1")
	if closed.ended < closed.started {
		t.Fatalf("closed row = %+v", closed)
	}
	// The raw address is in no stored column, only its hashes are.
	for _, field := range []string{closed.session, closed.account, closed.character, closed.transport, closed.ipKey, closed.netKey, closed.userAgent} {
		if field == "203.0.113.9" {
			t.Fatalf("raw address stored in %+v", closed)
		}
	}
}

/*
================
TestSecondEntryClosesTheFirstSpan
================
*/
func TestSecondEntryClosesTheFirstSpan(t *testing.T) {
	journal := openTestJournal(t, filepath.Join(t.TempDir(), "connections.sqlite"))
	journal.RecordEntered(Entry{Session: "boot:2", Account: "alice", Character: "Hero", Transport: "websocket", Address: "203.0.113.9"})
	rows(t, journal, 1)
	journal.RecordEntered(Entry{Session: "boot:2", Account: "alice", Character: "Mage", Transport: "websocket", Address: "203.0.113.9"})
	found := rows(t, journal, 2)
	if found[0].character != "Hero" || found[0].ended == 0 || found[0].ended != found[1].started {
		t.Fatalf("spans = %+v", found)
	}
	if found[1].character != "Mage" || found[1].ended != 0 {
		t.Fatalf("spans = %+v", found)
	}
}

/*
================
TestCrashRecoveryClosesAtTheLastCheckpoint
================
*/
func TestCrashRecoveryClosesAtTheLastCheckpoint(t *testing.T) {
	path := filepath.Join(t.TempDir(), "connections.sqlite")
	first := openTestJournal(t, path)
	first.RecordEntered(Entry{Session: "boot:3", Account: "alice", Character: "Hero", Transport: "webtransport", Address: "203.0.113.9"})
	open := rows(t, first, 1)[0]
	// Advance the checkpoint by hand, then stop the process the way a
	// crash does: the journal closes (its file must close) with the
	// session never ended.
	if err := first.checkpoint(); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second := openTestJournal(t, path)
	recovered := rows(t, second, 1)[0]
	if recovered.ended == 0 || recovered.estimated != 1 {
		t.Fatalf("recovered row = %+v", recovered)
	}
	if recovered.ended < open.started || recovered.ended < open.seen {
		t.Fatalf("recovered row = %+v (open was %+v)", recovered, open)
	}
}

/*
================
TestCheckpointPrunesClosedRowsAtRetention
================
*/
func TestCheckpointPrunesClosedRowsAtRetention(t *testing.T) {
	journal := openTestJournal(t, filepath.Join(t.TempDir(), "connections.sqlite"))
	base := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	journal.now = func() time.Time { return base }
	journal.RecordEntered(Entry{Session: "boot:4", Account: "alice", Character: "Hero", Transport: "websocket", Address: "203.0.113.9"})
	journal.RecordEntered(Entry{Session: "boot:5", Account: "bob", Character: "Mage", Transport: "websocket", Address: "203.0.113.10"})
	rows(t, journal, 2)
	journal.CloseSession("boot:4")
	waitEnded(t, journal, "boot:4")
	// Thirty-one days on, the closed row is pruned and the open one kept.
	journal.now = func() time.Time { return base.Add(31 * 24 * time.Hour) }
	if err := journal.checkpoint(); err != nil {
		t.Fatal(err)
	}
	found, err := readRows(journal)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 || found[0].session != "boot:5" {
		t.Fatalf("rows after prune = %+v", found)
	}
}

/*
================
TestLoadOrCreateKeyIsStableAndRefusesGarbage
================
*/
func TestLoadOrCreateKeyIsStableAndRefusesGarbage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "connections.key")
	key, err := LoadOrCreateKey(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(key) != keyBytes {
		t.Fatalf("key = %d bytes", len(key))
	}
	again, err := LoadOrCreateKey(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(again) != string(key) {
		t.Fatal("the key changed across loads")
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0600 {
			t.Fatalf("key file mode = %v", info.Mode().Perm())
		}
	}
	bad := filepath.Join(t.TempDir(), "bad.key")
	if err := os.WriteFile(bad, []byte("not a key"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOrCreateKey(bad); err == nil {
		t.Fatal("a garbage key file was accepted")
	}
}

/*
================
TestEnabledReadsTheFlag
================
*/
func TestEnabledReadsTheFlag(t *testing.T) {
	for value, want := range map[string]bool{"": false, "off": false, "0": false, "on": true, "1": true, "true": true, " ON ": true} {
		t.Setenv(EnvConnectionJournal, value)
		if got := Enabled(); got != want {
			t.Fatalf("Enabled() with %q = %v, want %v", value, got, want)
		}
	}
}
