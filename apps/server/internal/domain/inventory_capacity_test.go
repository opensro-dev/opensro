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
	if !c.GrantInventoryExpansion(54) || c.InventoryCapacity() != 109 {
		t.Fatalf("second grant = %d, want 109", c.InventoryCapacity())
	}
	if c.GrantInventoryExpansion(1) || c.InventoryCapacity() != 109 {
		t.Fatalf("grant past 109 was taken: capacity %d", c.InventoryCapacity())
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

// The pages floor lifts a record below it and leaves every record at or
// past it alone, so a quest-grown or flag-raised bag is never lowered.
func TestInventoryFloorRaiseNeverLowers(t *testing.T) {
	fresh := &Character{}
	if !fresh.RaiseInventoryFloor(InventoryPagesBase) || fresh.InventoryCapacity() != 93 {
		t.Fatalf("fresh raise = %d, want 93", fresh.InventoryCapacity())
	}
	if fresh.RaiseInventoryFloor(InventoryPagesBase) {
		t.Fatal("raising to the floor in force reported a change")
	}
	grown := &Character{InventorySize: 109}
	if grown.RaiseInventoryFloor(InventoryPagesBase) || grown.InventoryCapacity() != 109 {
		t.Fatalf("grown record = %d, want 109 untouched", grown.InventoryCapacity())
	}
	if fresh.RaiseInventoryFloor(0) {
		t.Fatal("a zero floor reported a change")
	}
}

func TestInventoryCapacityValid(t *testing.T) {
	for _, c := range []struct {
		size, waiting uint8
		valid         bool
	}{
		{0, 0, true}, {45, 0, true}, {0, 32, true}, {61, 16, true}, {77, 0, true},
		{93, 0, true}, {109, 0, true}, {0, 64, true},
		{44, 0, false}, {110, 0, false}, {100, 10, false}, {0, 65, false},
	} {
		record := &Character{InventorySize: c.size, InventoryExpansion: c.waiting}
		if got := record.InventoryCapacityValid(); got != c.valid {
			t.Fatalf("capacity %d (+%d) valid = %v, want %v", c.size, c.waiting, got, c.valid)
		}
	}
}
