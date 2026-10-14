/*
===========================================================================

economy_row_reason_test.go - a settlement door's subject and members

A trade-goods sale commits the seller's credit and the party's shares in
one door whose label names the seller; every other door keeps one reason
per direction.

===========================================================================
*/
package domain

import "testing"

/*
================
TestEconomyRowReasonSplitsASettlement
================
*/
func TestEconomyRowReasonSplitsASettlement(t *testing.T) {
	cases := []struct {
		label  string
		charID int64
		gain   bool
		want   string
	}{
		{"trade-sale 42", 42, true, "trade_sale_credit"},
		{"trade-sale 42", 7, true, "trade_party_share"},
		{"thief-sale 42", 42, true, "thief_den_sale"},
		{"thief-sale 42", 7, true, "trade_party_share"},
		// A settlement without a readable subject pays nobody as the seller.
		{"trade-sale", 42, true, "trade_party_share"},
		{"trade-sale x", 42, true, "trade_party_share"},
		// A settlement only ever pays; a loss through it is unattributed.
		{"trade-sale 42", 42, false, EconomyReasonUnattributed},
		// Other doors keep their one reason per direction, id or not.
		{"shop-transaction", 42, true, "npc_sale"},
		{"guild-level-up 1/7", 42, false, "guild_fee"},
	}
	for _, tc := range cases {
		if got := EconomyRowReason(tc.label, tc.charID, tc.gain); got != tc.want {
			t.Fatalf("EconomyRowReason(%q, %d, %v) = %q, want %q", tc.label, tc.charID, tc.gain, got, tc.want)
		}
	}
}
