/*
===========================================================================

shadowmute_test.go - the shadow-mute list's file contract

===========================================================================
*/
package shadowmute

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

/*
================
TestListFollowsItsFile
================
*/
func TestListFollowsItsFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	// A list saved with a byte-order mark keeps its first account.
	if err := os.WriteFile(path, []byte("\ufeffSpammer\n# staff list\n\n  other  \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	list := Open(dir)
	for _, id := range []string{"spammer", "SPAMMER", "other"} {
		if !list.Muted(id) {
			t.Fatalf("%q not muted after boot", id)
		}
	}
	if list.Muted("# staff list") || list.Muted("someone") || list.Muted("") {
		t.Fatal("a comment, a stranger or an empty id is muted")
	}

	// Removing a line lifts the mute without a restart.
	if err := os.WriteFile(path, []byte("other\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	list.refresh()
	if list.Muted("spammer") || !list.Muted("other") {
		t.Fatal("the edited list was not re-read")
	}
	// Deleting the file mutes no one.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	list.refresh()
	if list.Muted("other") {
		t.Fatal("deleted list still mutes")
	}
	var none *List
	if none.Muted("other") {
		t.Fatal("nil list muted")
	}
}

/*
================
TestMutedRefreshesInTheBackground

A stale snapshot is answered at once and refreshed behind the caller, so
the next answer follows the file.
================
*/
func TestMutedRefreshesInTheBackground(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 10, 28, 16, 0, 0, 0, time.UTC)
	list := Open(dir)
	list.now = func() time.Time { return now }
	list.checkedAt.Store(now.UnixNano())
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte("spammer\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Fresh snapshot: no refresh starts and the old answer holds.
	if list.Muted("spammer") {
		t.Fatal("read the file inside the recheck interval")
	}
	now = now.Add(recheckInterval)
	list.Muted("spammer")
	deadline := time.Now().Add(5 * time.Second)
	for !list.Muted("spammer") {
		if time.Now().After(deadline) {
			t.Fatal("the stale snapshot was never refreshed")
		}
		time.Sleep(time.Millisecond)
	}
}
