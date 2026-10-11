/*
===========================================================================

operator_silk_test.go - the one silk grant both GM and operator use

===========================================================================
*/
package action

import (
	"encoding/json"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
silkWalletStub
================
*/
type silkWalletStub struct {
	balances map[string]uint32
}

func (w *silkWalletStub) GrantSilk(account string, amount uint32) (domain.MallBalance, error) {
	w.balances[account] += amount
	return domain.MallBalance{Silk: w.balances[account]}, nil
}

/*
================
TestGrantSilkCreditsRecordsAndPushes
================
*/
func TestGrantSilkCreditsRecordsAndPushes(t *testing.T) {
	c := testCharacter()
	c.AccountID = "account-7"
	rt, _ := newTestRuntime(c, testItems())
	wallet := &silkWalletStub{balances: map[string]uint32{"account-7": 300}}
	rt.SilkWallet = wallet
	var records []SilkGrant
	rt.RecordSilkGrant = func(g SilkGrant) { records = append(records, g) }
	var pushed []wire.Frame
	rt.PushCharacterFrames = func(_, name string, frames []wire.Frame) {
		if name == c.Name {
			pushed = append(pushed, frames...)
		}
	}

	balance, err := rt.GrantSilk(testDivision, "Operator", "operator", c.Name, 100000)
	if err != nil || balance.Silk != 100300 || wallet.balances["account-7"] != 100300 {
		t.Fatalf("grant %+v, %v, wallet %+v", balance, err, wallet.balances)
	}
	if len(records) != 1 || records[0] != (SilkGrant{Division: testDivision, Grantor: "Operator", Source: "operator",
		Character: c.Name, Account: "account-7", Amount: 100000, Balance: 100300}) {
		t.Fatalf("history %+v", records)
	}
	var shown domain.MallBalance
	if len(pushed) != 1 || pushed[0].Opcode != opMallBalance || json.Unmarshal(pushed[0].Payload, &shown) != nil || shown.Silk != 100300 {
		t.Fatalf("balance push %+v", pushed)
	}
}

/*
================
TestGrantSilkBoundsAndUnknownRecipients
================
*/
func TestGrantSilkBoundsAndUnknownRecipients(t *testing.T) {
	c := testCharacter()
	c.AccountID = "account-7"
	rt, _ := newTestRuntime(c, testItems())
	wallet := &silkWalletStub{balances: map[string]uint32{}}
	rt.SilkWallet = wallet
	rt.RecordSilkGrant = func(SilkGrant) { t.Fatal("a refused grant was recorded") }
	for _, amount := range []uint32{0, MaxSilkGrant + 1} {
		if _, err := rt.GrantSilk(testDivision, "Gm", "gm", c.Name, amount); err == nil {
			t.Fatalf("amount %d was granted", amount)
		}
	}
	if _, err := rt.GrantSilk(testDivision, "Gm", "gm", "NoSuchCharacter", 5); err == nil {
		t.Fatal("an unknown recipient was granted")
	}
	if len(wallet.balances) != 0 {
		t.Fatalf("refused grants touched the wallet: %+v", wallet.balances)
	}
}

/*
================
TestGrantSilkReachesAnOfflineRecipient

No session and no push hook: the wallet is the account's stored row.
================
*/
func TestGrantSilkReachesAnOfflineRecipient(t *testing.T) {
	c := testCharacter()
	c.AccountID = "account-9"
	rt, _ := newTestRuntime(c, testItems())
	wallet := &silkWalletStub{balances: map[string]uint32{}}
	rt.SilkWallet = wallet
	rt.PushCharacterFrames = nil
	if balance, err := rt.GrantSilk(testDivision, "Gm", "gm", c.Name, MaxSilkGrant); err != nil || balance.Silk != MaxSilkGrant {
		t.Fatalf("offline grant %+v, %v", balance, err)
	}
}
