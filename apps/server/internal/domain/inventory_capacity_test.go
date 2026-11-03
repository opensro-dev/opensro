package domain

import "testing"

// A grant grows the capacity in force at once; the cap counts any slots
// still waiting from before grants applied live, and a refused grant
// leaves the record untouched.
func TestInventoryExpansionGrantAppliesAndCaps(t *testing.T) {
	c := &Character{}
	if c.InventoryCapacity() != 45 {
		t.Fatalf("fresh capacity = %d, want 45", c.InventoryCapacity())
	}
	if !c.GrantInventoryExpansion(10) || c.InventoryCapacity() != 55 || c.InventoryExpansion != 0 {
		t.Fatalf("grant = %d (+%d), want 55 (+0) at once", c.InventoryCapacity(), c.InventoryExpansion)
	}
	if !c.GrantInventoryExpansion(22) || c.InventoryCapacity() != 77 {
		t.Fatalf("second grant = %d, want 77", c.InventoryCapacity())
	}
	if c.GrantInventoryExpansion(1) || c.InventoryCapacity() != 77 {
		t.Fatalf("grant past 77 was taken: capacity %d", c.InventoryCapacity())
	}
	if c.GrantInventoryExpansion(0) {
		t.Fatal("an empty grant reported a change")
	}
}

// Presenting moves every waiting slot; adopting takes a presented size
// only past the size in force, so a grant paid live while the entry was
// being built is never shrunk away by the older snapshot.
func TestInventoryExpansionPresentAndAdopt(t *testing.T) {
	detached := &Character{InventorySize: 55, InventoryExpansion: 2}
	if !detached.PresentInventoryExpansion() || detached.InventorySize != 57 || detached.InventoryExpansion != 0 {
		t.Fatalf("present = %d (+%d), want 57 (+0)", detached.InventorySize, detached.InventoryExpansion)
	}
	if detached.PresentInventoryExpansion() {
		t.Fatal("presenting nothing reported a change")
	}

	live := &Character{InventorySize: 55, InventoryExpansion: 2}
	if !live.GrantInventoryExpansion(10) || live.InventorySize != 65 || live.InventoryExpansion != 2 {
		t.Fatalf("live grant = %d (+%d), want 65 (+2 still waiting)", live.InventorySize, live.InventoryExpansion)
	}
	for _, presented := range []uint8{0, 45, 57, 70} {
		live.AdoptInventorySize(presented)
		if live.InventorySize != 65 || live.InventoryExpansion != 2 {
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
