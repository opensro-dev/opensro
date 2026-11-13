/*
===========================================================================

read_window_test.go - a store inside the read window boots and stays newer

Part 2 of #3082 (#3091). StoreReadHeadroom is 0 until part 4, so these
tests open with the package-only wider window (Options.readHeadroom) and
play both binaries on one file: the current code is the vN binary, and a
store stamped one version and one layout higher, carrying a top-level
record field and a table vN does not know, is what the v(N+1) binary
left behind.

===========================================================================
*/
package store

import (
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

/*
================
openWithHeadroom

openTest with a read window headroom versions wide.
================
*/
func openWithHeadroom(t *testing.T, dir string, clock *testClock, headroom int) (*Store, error) {
	t.Helper()
	s, err := Open(dir, Options{Now: clock.Now, DefaultSkills: testSkillSeeder, AsyncCommit: asyncCommitEnabled(), readHeadroom: headroom})
	if err != nil {
		return nil, err
	}
	t.Cleanup(s.Close)
	s.waitBootVerified()
	return s, nil
}

/*
================
execAuthority

One statement on the closed store's file.
================
*/
func execAuthority(t *testing.T, dir, statement string, args ...any) {
	t.Helper()
	db, err := openDB(filepath.Join(dir, DBFileName))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(statement, args...); err != nil {
		t.Fatalf("%s: %v", statement, err)
	}
	if _, err := db.Exec("PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		t.Fatal(err)
	}
}

/*
================
metaStamp
================
*/
func metaStamp(t *testing.T, dir, key string) string {
	t.Helper()
	return rowRecord(t, dir, "SELECT value FROM meta WHERE key = ?", key)
}

/*
================
newerStore

A store as the v(N+1) binary leaves it: stamped CurrentVersion+1 and
CurrentLayoutVersion+1, its character carrying futureField, and a table
of the newer layout holding one row. Returns the character's name.
================
*/
func newerStore(t *testing.T, dir string, clock *testClock) string {
	t.Helper()
	s := openTest(t, dir, clock)
	c := seededCharacter()
	if err := s.CreateCharacter(testDivision, "account-a", c); err != nil {
		t.Fatal(err)
	}
	id := c.ID
	s.Close()
	plantFutureField(t, dir, "SELECT record FROM characters WHERE division = ? AND id = ?",
		"UPDATE characters SET record = ? WHERE division = ? AND id = ?", testDivision, id)
	execAuthority(t, dir, "CREATE TABLE future_plane (key TEXT PRIMARY KEY, value TEXT NOT NULL)")
	execAuthority(t, dir, "INSERT INTO future_plane (key, value) VALUES ('kept', 'by the newer release')")
	rewriteDatabaseMeta(t, dir, metaKeySchemaVersion, CurrentVersion+1)
	rewriteDatabaseMeta(t, dir, metaKeyLayoutVersion, CurrentLayoutVersion+1)
	return c.Name
}

/*
================
TestNewerStoreInsideTheWindowBootsAndStaysNewer

The vN binary boots on the v(N+1) store, commits a change, and leaves
the store as newer as it found it: both stamps, the newer record field
and the newer table's row. The v(N+1) binary then opens it at its own
exact version with the change and its data intact.
================
*/
func TestNewerStoreInsideTheWindowBootsAndStaysNewer(t *testing.T) {
	t.Parallel()
	dir, clock := t.TempDir(), newTestClock()
	name := newerStore(t, dir, clock)

	s, err := openWithHeadroom(t, dir, clock, 1)
	if err != nil {
		t.Fatalf("the vN binary refused a store inside its window: %v", err)
	}
	c := storedCharacter(t, s, name)
	gold := int64(9876)
	s.MutateCharacter(c, "vN change", func() { c.Gold = &gold })
	id := c.ID
	s.Close()

	if got := metaStamp(t, dir, metaKeySchemaVersion); got != strconv.Itoa(CurrentVersion+1) {
		t.Fatalf("schema stamp = %s, want %d: the older binary lowered it", got, CurrentVersion+1)
	}
	if got := metaStamp(t, dir, metaKeyLayoutVersion); got != strconv.Itoa(CurrentLayoutVersion+1) {
		t.Fatalf("layout stamp = %s, want %d: the older binary lowered it", got, CurrentLayoutVersion+1)
	}
	if got := rowRecord(t, dir, "SELECT value FROM future_plane WHERE key = 'kept'"); got != "by the newer release" {
		t.Fatalf("the newer layout's table row = %q", got)
	}
	requireFutureField(t, rowRecord(t, dir, "SELECT record FROM characters WHERE division = ? AND id = ?", testDivision, id), `"gold":9876`)

	// The v(N+1) binary's own exact load.
	db, err := openDB(filepath.Join(dir, DBFileName))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	loaded, err := loadDB(db, CurrentVersion+1, CurrentLayoutVersion+1, 0)
	if err != nil {
		t.Fatalf("the v(N+1) binary cannot open what vN left: %v", err)
	}
	if loaded.version != CurrentVersion+1 || loaded.layout != CurrentLayoutVersion+1 {
		t.Fatalf("loaded stamps %d/%d", loaded.version, loaded.layout)
	}
}

/*
================
TestStoresOutsideTheWindowStillRefuse

Today's window (StoreReadHeadroom 0) refuses the newer store with the
"exactly" wording; a wider window still refuses a store past it, and no
window admits a store below CurrentVersion, which stays the offline
upgrader's.
================
*/
func TestStoresOutsideTheWindowStillRefuse(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name     string
		schema   int
		headroom int
		want     string
	}{
		{"newer store, today's window", CurrentVersion + 1, 0, "exactly"},
		{"store past a one-step window", CurrentVersion + 2, 1, "through"},
		{"older store", CurrentVersion - 1, 1, "sro-authority-upgrade"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			dir, clock := t.TempDir(), newTestClock()
			s := openTest(t, dir, clock)
			s.Close()
			rewriteDatabaseMeta(t, dir, metaKeySchemaVersion, test.schema)
			_, err := openWithHeadroom(t, dir, clock, test.headroom)
			if !isVersionMismatch(err) || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("open = %v, want an incompatible-schema refusal naming %q", err, test.want)
			}
			if got := metaStamp(t, dir, metaKeySchemaVersion); got != strconv.Itoa(test.schema) {
				t.Fatalf("a refused store was restamped to %s", got)
			}
		})
	}
	// The layout window refuses on its own.
	dir, clock := t.TempDir(), newTestClock()
	s := openTest(t, dir, clock)
	s.Close()
	rewriteDatabaseMeta(t, dir, metaKeyLayoutVersion, CurrentLayoutVersion+1)
	if _, err := openWithHeadroom(t, dir, clock, 0); !errors.Is(err, errIncompatibleSchema) {
		t.Fatalf("a newer layout outside the window opened: %v", err)
	}
}

/*
================
TestVersionStampsNeverGoDown

The offline writers (the upgrade and a fresh authority's seed) stamp
through stampVersionTx: a lower stamp is raised, a higher one stays.
================
*/
func TestVersionStampsNeverGoDown(t *testing.T) {
	t.Parallel()
	dir, clock := t.TempDir(), newTestClock()
	s := openTest(t, dir, clock)
	s.Close()
	db, err := openDB(filepath.Join(dir, DBFileName))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, test := range []struct {
		stored string
		stamp  int
		want   string
	}{
		{"", 43, "43"},
		{"40", 43, "43"},
		{"50", 43, "50"},
		{"43", 43, "43"},
	} {
		if _, err := db.Exec("DELETE FROM meta WHERE key = 'probeVersion'"); err != nil {
			t.Fatal(err)
		}
		if test.stored != "" {
			if _, err := db.Exec("INSERT INTO meta (key, value) VALUES ('probeVersion', ?)", test.stored); err != nil {
				t.Fatal(err)
			}
		}
		if err := stampVersionTx(db, "probeVersion", test.stamp); err != nil {
			t.Fatal(err)
		}
		var got string
		if err := db.QueryRow("SELECT value FROM meta WHERE key = 'probeVersion'").Scan(&got); err != nil || got != test.want {
			t.Fatalf("stored %q, stamp %d: got %q (%v), want %q", test.stored, test.stamp, got, err, test.want)
		}
	}
}
