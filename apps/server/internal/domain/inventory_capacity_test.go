package domain

import "testing"

// A grant waits; the cap counts the waiting slots too, and a refused grant
// leaves the record untouched.
func TestInventoryExpansionGrantWaitsAndCaps(t *testing.T) {
	c := &Character{}
	if c.InventoryCapacity() != 45 {
		t.Fatalf("fresh capacity = %d, want 45", c.InventoryCapacity())
	}
	if !c.GrantInventoryExpansion(10) || c.InventoryCapacity() != 45 || c.InventoryExpansion != 10 {
		t.Fatalf("grant = %d (+%d), want 45 (+10)", c.InventoryCapacity(), c.InventoryExpansion)
	}
	if !c.GrantInventoryExpansion(22) || c.InventoryExpansion != 32 {
		t.Fatalf("second grant waiting = %d, want 32", c.InventoryExpansion)
	}
	if c.GrantInventoryExpansion(1) || c.InventoryExpansion != 32 {
		t.Fatalf("grant past 77 was taken: waiting %d", c.InventoryExpansion)
	}
	if c.GrantInventoryExpansion(0) {
		t.Fatal("an empty grant reported a change")
	}
}

// Presenting moves every waiting slot; adopting moves only what an entry
// presented, so a grant paid meanwhile keeps waiting.
func TestInventoryExpansionPresentAndAdopt(t *testing.T) {
	detached := &Character{InventorySize: 55, InventoryExpansion: 2}
	if !detached.PresentInventoryExpansion() || detached.InventorySize != 57 || detached.InventoryExpansion != 0 {
		t.Fatalf("present = %d (+%d), want 57 (+0)", detached.InventorySize, detached.InventoryExpansion)
	}
	if detached.PresentInventoryExpansion() {
		t.Fatal("presenting nothing reported a change")
	}

	live := &Character{InventorySize: 55, InventoryExpansion: 2}
	live.GrantInventoryExpansion(10)
	live.AdoptInventorySize(57)
	if live.InventorySize != 57 || live.InventoryExpansion != 10 {
		t.Fatalf("adopt = %d (+%d), want 57 (+10)", live.InventorySize, live.InventoryExpansion)
	}
	for _, presented := range []uint8{0, 45, 57, 70} {
		live.AdoptInventorySize(presented)
		if live.InventorySize != 57 || live.InventoryExpansion != 10 {
			t.Fatalf("adopting %d changed the record: %d (+%d)", presented, live.InventorySize, live.InventoryExpansion)
		}
	}
}

func TestInventoryCapacityValid(t *testing.T) {
	for _, c := range []struct {
		size, waiting uint8
		valid         bool
	}{
		{0, 0, true}, {45, 0, true}, {0, 32, true}, {61, 16, true}, {77, 0, true},
		{44, 0, false}, {78, 0, false}, {70, 8, false}, {0, 33, false},
	} {
		record := &Character{InventorySize: c.size, InventoryExpansion: c.waiting}
		if got := record.InventoryCapacityValid(); got != c.valid {
			t.Fatalf("capacity %d (+%d) valid = %v, want %v", c.size, c.waiting, got, c.valid)
		}
	}
}
