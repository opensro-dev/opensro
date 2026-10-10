/*
===========================================================================

history_silk_test.go - a silk grant's operator history event

===========================================================================
*/
package main

import (
	"testing"

	"opensro.online/server/internal/game/action"
)

/*
================
TestSilkGrantEventNamesGrantorRecipientAmountAndBalance
================
*/
func TestSilkGrantEventNamesGrantorRecipientAmountAndBalance(t *testing.T) {
	e := silkGrantEvent(action.SilkGrant{Division: "global-official", Grantor: "Gm", Source: "gm", Character: "Tester",
		Account: "account-7", Amount: 100000, Balance: 100300})
	if e.Kind != "silk_grant" || e.Category != "operator" || e.Code != "gm" || e.Shard != "global-official" ||
		e.Character != "Tester" || e.Account != "account-7" {
		t.Fatalf("event %+v", e)
	}
	if e.Fields["grantor"] != "Gm" || e.Fields["amount"] != "100000" || e.Fields["balance"] != "100300" {
		t.Fatalf("fields %+v", e.Fields)
	}
}
