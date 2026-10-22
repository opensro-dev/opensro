/*
===========================================================================

loot_test.go - rotating loot membership follows joins, leaves and disbanding

===========================================================================
*/
package party

import "testing"

/*
================
TestLootRotationTracksMembershipWithoutResetting
================
*/
func TestLootRotationTracksMembershipWithoutResetting(t *testing.T) {
	r := NewRegistry()
	if _, refusal := r.Form(testDivision, testMember(1), testMember(2), PartyOptionItemShare); refusal != "" {
		t.Fatal(refusal)
	}
	if _, refusal := r.Join(testDivision, testMember(1).Name, testMember(3)); refusal != "" {
		t.Fatal(refusal)
	}
	next := func(want uint32) {
		t.Helper()
		if got := r.NextLootMember(testDivision, testMember(1).Name); got != want {
			t.Fatalf("recipient %d, want %d", got, want)
		}
	}
	next(testMember(1).MemberID)
	if _, refusal := r.Leave(testDivision, testMember(2).Name); refusal != "" {
		t.Fatal(refusal)
	}
	if _, refusal := r.Join(testDivision, testMember(1).Name, testMember(4)); refusal != "" {
		t.Fatal(refusal)
	}
	next(testMember(3).MemberID)
	next(testMember(1).MemberID)
	next(testMember(4).MemberID)
	if got := r.NextLootMember("other", testMember(1).Name); got != 0 {
		t.Fatal("rotation crossed divisions")
	}
	if _, refusal := r.Leave(testDivision, testMember(1).Name); refusal != "" {
		t.Fatal(refusal)
	}
	next(0)
}

/*
================
TestIndividualLootHasNoPartyRecipient
================
*/
func TestIndividualLootHasNoPartyRecipient(t *testing.T) {
	r := NewRegistry()
	if _, refusal := r.Form(testDivision, testMember(1), testMember(2), PartyOptionExpShare); refusal != "" {
		t.Fatal(refusal)
	}
	if got := r.NextLootMember(testDivision, testMember(1).Name); got != 0 {
		t.Fatal("individual loot entered shared rotation")
	}
}

/*
================
TestOwnedDropPickupNeedsAnItemShareParty

A member may take another member's reserved drop only while the party
shares items; a stranger's drop and a party without item share refuse.
================
*/
func TestOwnedDropPickupNeedsAnItemShareParty(t *testing.T) {
	r := NewRegistry()
	if _, refusal := r.Form(testDivision, testMember(1), testMember(2), PartyOptionItemShare); refusal != "" {
		t.Fatal(refusal)
	}
	if !r.CanPickupOwnedDrop(testDivision, testMember(1).Name, testMember(2).MemberID) {
		t.Fatal("an item-share member's drop refused")
	}
	if r.CanPickupOwnedDrop(testDivision, testMember(1).Name, testMember(3).MemberID) {
		t.Fatal("a non-member's drop granted")
	}
	if r.CanPickupOwnedDrop(testDivision, testMember(3).Name, testMember(1).MemberID) {
		t.Fatal("a partyless character granted a member's drop")
	}
	plain := NewRegistry()
	if _, refusal := plain.Form(testDivision, testMember(1), testMember(2), 0); refusal != "" {
		t.Fatal(refusal)
	}
	if plain.CanPickupOwnedDrop(testDivision, testMember(1).Name, testMember(2).MemberID) {
		t.Fatal("a party without item share granted a member's drop")
	}
}
