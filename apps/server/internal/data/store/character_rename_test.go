package store

import (
	"errors"
	"reflect"
	"testing"

	"opensro.online/server/internal/domain"
)

func TestOperatorRenamePreservesIdentityAndReopensGMName(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	s := openTest(t, dir, newTestClock())
	c := &domain.Character{Name: "Test2", MissionInventory: []domain.InventoryRow{{Slot: 13, RefObjID: 24198, StackCount: 2}}}
	peer := &domain.Character{Name: "Peer"}
	for _, value := range []*domain.Character{c, peer} {
		if err := s.CreateCharacter(testDivision, "test-account", value); err != nil {
			t.Fatal(err)
		}
	}
	id := c.ID
	s.Mutate("rename-fixture", func() {
		c.Friends = []domain.FriendRecord{{ID: peer.ID, Name: peer.Name}}
		peer.Friends = []domain.FriendRecord{{ID: id, Name: c.Name}}
		peer.BlockedWhisperers = []string{"TEST2"}
	})
	s.commitFail = errors.New("disk failure")
	if err := s.RenameCharacterOffline(testDivision, "Test2", "[GM]Test2"); err == nil {
		t.Fatal("failure accepted")
	}
	if c.Name != "Test2" || peer.Friends[0].Name != "Test2" {
		t.Fatal("failure changed memory")
	}
	s.commitFail = nil
	if err := s.RenameCharacterOffline(testDivision, "Test2", "[GM]Test2"); err != nil {
		t.Fatal(err)
	}
	if c.ID != id || c.AccountID != "test-account" || c.GMPrivilege {
		t.Fatal("rename changed identity or granted privilege")
	}
	if peer.Friends[0].Name != c.Name || peer.BlockedWhisperers[0] != c.Name {
		t.Fatal("stale social references")
	}
	if err := s.RenameCharacterOffline(testDivision, "Peer", "[GM]test2"); !errors.Is(err, ErrCharacterNameConflict) {
		t.Fatalf("collision: %v", err)
	}
	if err := s.CreateCharacter(testDivision, "test-account", &domain.Character{Name: "[GM]Other"}); err == nil {
		t.Fatal("player creation accepted reserved prefix")
	}
	s.Close()
	reopened := openTest(t, dir, newTestClock())
	for _, loaded := range reopened.Characters().CharactersForDivision(testDivision) {
		if loaded.ID == id {
			if loaded.Name != "[GM]Test2" || !reflect.DeepEqual(loaded.MissionInventory, c.MissionInventory) {
				t.Fatal("rename/inventory did not survive reopen")
			}
			return
		}
	}
	t.Fatal("renamed character missing")
}
