/*
===========================================================================

characters_rename_test.go - queued renames at the character list (#1689)

The list applies a queued rename before answering: the row shows the
new name, the record owes the friends/guild notice, and a rename whose
target was taken meanwhile is cancelled with its reason on the row.

===========================================================================
*/

package agentapi

import (
	"testing"

	"opensro.online/server/internal/data/store"
)

/*
================
TestCharacterListAppliesQueuedRename
================
*/
func TestCharacterListAppliesQueuedRename(t *testing.T) {
	api, authority := newTestAPI(t)
	handler := authenticatedHandler(t, api, "alice")
	created := postJSON(t, handler, "/character/create", createBody("FirstHero"))
	if created["nativeResult"] != float64(1) {
		t.Fatalf("create = %v", created)
	}
	hero := authority.Characters().CharactersForDivision(testDivision)[0]
	if err := authority.QueueCharacterRename(testDivision, hero, "RenamedHero", testNowUnixMillis); err != nil {
		t.Fatalf("queue: %v", err)
	}

	var listed map[string]any
	getJSON(t, handler, "/character/list", &listed)
	rows := listed["characters"].([]any)
	if len(rows) != 1 {
		t.Fatalf("rows = %v", listed)
	}
	row := rows[0].(map[string]any)
	if row["name"] != "RenamedHero" {
		t.Fatalf("row name = %v, want the applied rename", row["name"])
	}
	if _, refused := row["renameCancelled"]; refused {
		t.Fatalf("an applied rename carries a cancellation: %v", row)
	}
	if hero.Name != "RenamedHero" || hero.PendingRenameName != "" || hero.RenameNoticeFrom != "FirstHero" {
		t.Fatalf("record after list: name %q pending %q notice %q", hero.Name, hero.PendingRenameName, hero.RenameNoticeFrom)
	}
}

/*
================
TestCharacterListCancelsTakenRename

The target was free when queued and taken before the login: the row
keeps the old name and says why, and the record owes no notice.
================
*/
func TestCharacterListCancelsTakenRename(t *testing.T) {
	api, authority := newTestAPI(t)
	alice := authenticatedHandler(t, api, "alice")
	created := postJSON(t, alice, "/character/create", createBody("SecondHero"))
	if created["nativeResult"] != float64(1) {
		t.Fatalf("create = %v", created)
	}
	hero := authority.Characters().CharactersForDivision(testDivision)[0]
	if err := authority.QueueCharacterRename(testDivision, hero, "TakenLater", testNowUnixMillis); err != nil {
		t.Fatalf("queue: %v", err)
	}
	bob := authenticatedHandler(t, api, "bob")
	if made := postJSON(t, bob, "/character/create", createBody("TakenLater")); made["nativeResult"] != float64(1) {
		t.Fatalf("bob create = %v", made)
	}

	var listed map[string]any
	getJSON(t, alice, "/character/list", &listed)
	rows := listed["characters"].([]any)
	if len(rows) != 1 {
		t.Fatalf("rows = %v", listed)
	}
	row := rows[0].(map[string]any)
	if row["name"] != "SecondHero" || row["renameCancelled"] != store.RenameRefusedTaken {
		t.Fatalf("row = %v, want the old name with a taken cancellation", row)
	}
	if hero.Name != "SecondHero" || hero.PendingRenameName != "" || hero.RenameNoticeFrom != "" {
		t.Fatalf("record after cancel: name %q pending %q notice %q", hero.Name, hero.PendingRenameName, hero.RenameNoticeFrom)
	}
}
