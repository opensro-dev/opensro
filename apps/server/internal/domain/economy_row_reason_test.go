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
		// The caravan's "-<slots>s<stars>" stamp (#3162) changes the
		// stored door, never the reasons.
		{"trade-sale-101s5 42", 42, true, "trade_sale_credit"},
		{"trade-sale-101s5 42", 7, true, "trade_party_share"},
		{"thief-sale-32s5 42", 42, true, "thief_den_sale"},
		// Loading a transport burns at the trade-load door, stamped or not.
		{"trade-load-32s5", 42, false, "trade_goods_cost"},
		{"trade-load", 42, false, "trade_goods_cost"},
		// A settlement without a readable subject pays nobody as the seller.
		{"trade-sale", 42, true, "trade_party_share"},
		{"trade-sale x", 42, true, "trade_party_share"},
		// A settlement only ever pays; a loss through it is unattributed.
		{"trade-sale 42", 42, false, EconomyReasonUnattributed},
		// Other doors keep their one reason per direction, id or not.
		{"shop-transaction", 42, true, "npc_sale"},
		{"guild-level-up 1/7", 42, false, "guild_fee"},
		{"quest-reward", 42, true, "quest_reward"},
		{"quest-reward", 42, false, "quest_fee"},
		// Doors that moved gold as "unattributed" on the live shard.
		{"sook-payout", 42, true, "arena_reward"},
		{"sook-recovered-payout", 42, true, "arena_reward"},
		{"sook-chest-take-item", 42, true, "arena_reward"},
		{"bag-trade-goods-repair", 42, true, "trade_goods_refund"},
		{"fortress-structure-upgrade", 42, false, "fortress_fee"},
	}
	for _, tc := range cases {
		if got := EconomyRowReason(tc.label, tc.charID, tc.gain); got != tc.want {
			t.Fatalf("EconomyRowReason(%q, %d, %v) = %q, want %q", tc.label, tc.charID, tc.gain, got, tc.want)
		}
	}
}

/*
================
TestArenaAndRefundReasonsMint

The arena's gold and the trade-goods refund add gold to the shard, so the
console counts them as mints, not as unknown.
================
*/
func TestArenaAndRefundReasonsMint(t *testing.T) {
	for _, reason := range []string{"arena_reward", "trade_goods_refund"} {
		if kind := EconomyReasonKind(reason); kind != EconomyKindMint {
			t.Fatalf("EconomyReasonKind(%q) = %q, want mint", reason, kind)
		}
	}
}
