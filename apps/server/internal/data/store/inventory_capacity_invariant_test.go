package store

import (
	"testing"

	"opensro.online/server/internal/domain"
)

// A persisted capacity outside 45..77, or waiting slots that overflow it,
// is refused at load: the entry block would promise slots no tab can show.
func TestLoadRefusesAnOutOfRangeInventoryCapacity(t *testing.T) {
	valid := func(size, waiting uint8) *domain.Character {
		return &domain.Character{ID: 1, Name: "expander", AccountID: "account", InventorySize: size, InventoryExpansion: waiting}
	}
	for _, c := range []struct{ size, waiting uint8 }{{0, 0}, {61, 16}, {77, 0}} {
		if err := validateCharacterIdentity(valid(c.size, c.waiting)); err != nil {
			t.Fatalf("capacity %d (+%d) refused: %v", c.size, c.waiting, err)
		}
	}
	for _, c := range []struct{ size, waiting uint8 }{{30, 0}, {78, 0}, {70, 8}} {
		if err := validateCharacterIdentity(valid(c.size, c.waiting)); err == nil {
			t.Fatalf("capacity %d (+%d) was accepted", c.size, c.waiting)
		}
	}
}
