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
	now := time.Date(2026, 10, 28, 16, 0, 0, 0, time.UTC)
	list := Open(dir)
	list.now = func() time.Time { return now }

	// No file: nobody is muted.
	if list.Muted("spammer") {
		t.Fatal("missing file muted an account")
	}
	path := filepath.Join(dir, FileName)
	if err := os.WriteFile(path, []byte("# staff list\n\n  Spammer  \nother\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Within the recheck interval the old answer holds.
	if list.Muted("spammer") {
		t.Fatal("re-read before the recheck interval")
	}
	now = now.Add(recheckInterval)
	for _, id := range []string{"spammer", "SPAMMER", "other"} {
		if !list.Muted(id) {
			t.Fatalf("%q not muted", id)
		}
	}
	if list.Muted("# staff list") || list.Muted("someone") || list.Muted("") {
		t.Fatal("a comment, a stranger or an empty id is muted")
	}

	// Removing a line lifts the mute without a restart.
	if err := os.WriteFile(path, []byte("other\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	now = now.Add(recheckInterval)
	if list.Muted("spammer") || !list.Muted("other") {
		t.Fatal("the edited list was not re-read")
	}
	// Deleting the file mutes no one.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	now = now.Add(recheckInterval)
	if list.Muted("other") {
		t.Fatal("deleted list still mutes")
	}
	var none *List
	if none.Muted("other") {
		t.Fatal("nil list muted")
	}
}
