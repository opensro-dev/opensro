/*
===========================================================================

jobsuit_party_test.go - the party job-pair table

===========================================================================
*/
package enterworld

import "testing"

/*
================
TestJobsMayPartyFollowsTheShardTable

Every pair of classes against the shard manager's jump table: class 1 and
3 accept 1 or 3, class 2 accepts 2, class 4 accepts 4, anything else none.
================
*/
func TestJobsMayPartyFollowsTheShardTable(t *testing.T) {
	native := func(a, b uint8) bool {
		switch a {
		case 1, 3:
			return b == 3 || b == 1
		case 2:
			return b == 2
		case 4:
			return b == 4
		}
		return false
	}
	for a := 0; a < 256; a++ {
		for b := 0; b < 256; b++ {
			if got, want := JobsMayParty(uint8(a), uint8(b)), native(uint8(a), uint8(b)); got != want {
				t.Fatalf("JobsMayParty(%d, %d) = %v, want %v", a, b, got, want)
			}
		}
	}
	if !JobsMayParty(1, 3) || JobsMayParty(1, 2) || JobsMayParty(2, 3) || JobsMayParty(4, 1) {
		t.Fatal("trader/hunter, thief and plain pairs are wrong")
	}
	if PartyJobClass(&Character{}) != 4 {
		t.Fatal("a player without a suit is not class 4")
	}
}

/*
================
TestPartyMemberNameIsTheAliasWhileSuited

The server's snapshot: a suited character's roster row carries its job alias, an undressed
one its own name; a suited character without an alias keeps its name.
================
*/
func TestPartyMemberNameIsTheAliasWhileSuited(t *testing.T) {
	c := &Character{Name: "killmepls"}
	c.Job.Alias = "Merchant"
	if got := PartyMemberName(c); got != "killmepls" {
		t.Fatalf("undressed member named %q", got)
	}
	c.MissionInventory = append(c.MissionInventory, InventoryRow{Slot: JobSuitSlot, TypeFlags: 0x3ac | 1<<11})
	if got := PartyMemberName(c); got != "Merchant" {
		t.Fatalf("suited member named %q, want its alias", got)
	}
	c.Job.Alias = ""
	if got := PartyMemberName(c); got != "killmepls" {
		t.Fatalf("suited member with no alias named %q", got)
	}
	if PartyMemberName(nil) != "" {
		t.Fatal("nil character has a name")
	}
}
