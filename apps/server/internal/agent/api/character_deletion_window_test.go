/*
===========================================================================

character_deletion_window_test.go - the character list names when a deletion matures

The select screen counts down to deleteMaturesAt, so the instant must come
from the store's own deletion window (native seven days, or the shard's
SRO_QUICK_CHARACTER_DELETE day), never from a client constant.

===========================================================================
*/

package agentapi

import (
	"testing"
	"time"

	"opensro.online/server/internal/domain"
)

/*
================
TestCharacterListNamesTheDeletionMaturity
================
*/
func TestCharacterListNamesTheDeletionMaturity(t *testing.T) {
	api, st := newTestAPI(t)
	reserved := time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)
	c := &domain.Character{Name: "doomed", DeletePending: true, DeleteReservedAt: reserved.Format(time.RFC3339)}
	got := api.createdCharacterJSON(c)["deleteMaturesAt"]
	want := reserved.Add(st.DeletionWindow()).Format(time.RFC3339)
	if got != want {
		t.Fatalf("deleteMaturesAt = %v, want %s (the store's window)", got, want)
	}
	if _, ok := api.createdCharacterJSON(&domain.Character{Name: "kept"})["deleteMaturesAt"]; ok {
		t.Fatal("a character with no reservation names no maturity")
	}
}
