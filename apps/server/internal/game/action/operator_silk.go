/*
===========================================================================

operator_silk.go - GMs and operators grant silk (operator tooling)

One function serves both the in-game /SILK command (gmcommand) and the
operator dashboard (the Agent's grant-silk player operation). Not a
gameplay rule and not native, so it carries no SRO_ flag: only a GM
(GMPrivilege) or the operator credential reaches it. The amount comes from
the GM or operator; the balance is read from the wallet, never from a
client. The recipient may be offline: the wallet is the account's row in
the store, so nothing waits on a session.

===========================================================================
*/
package action

import (
	"encoding/json"
	"fmt"

	log "github.com/sirupsen/logrus"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/item/wire"
)

// MaxSilkGrant bounds one grant; the lower bound is 1.
const MaxSilkGrant = 1_000_000

/*
================
SilkWallet

The account wallet a grant credits (store.GrantSilk).
================
*/
type SilkWallet interface {
	GrantSilk(accountID string, amount uint32) (domain.MallBalance, error)
}

/*
================
SilkGrant

One committed grant, for the history journal.
================
*/
type SilkGrant struct {
	Division, Grantor, Source string
	Character, Account        string
	Amount, Balance           uint32
}

/*
================
GrantSilk

Credit amount silk to the account of the character named name, record it,
and push the new balance to the character when it is online. source names
the path ("gm" or "operator").
================
*/
func (rt *Runtime) GrantSilk(division, grantor, source, name string, amount uint32) (domain.MallBalance, error) {
	if amount == 0 || amount > MaxSilkGrant {
		return domain.MallBalance{}, fmt.Errorf("silk grant must be 1..%d", MaxSilkGrant)
	}
	if rt.SilkWallet == nil {
		return domain.MallBalance{}, fmt.Errorf("silk grant unavailable")
	}
	var c *domain.Character
	rt.deps.Read(division, func() {
		if found := rt.findCharacter(division, name); found != nil {
			c = found.Snapshot()
		}
	})
	if c == nil || c.DeletePending {
		return domain.MallBalance{}, fmt.Errorf("character not found")
	}
	if c.AccountID == "" {
		return domain.MallBalance{}, fmt.Errorf("character has no account")
	}
	balance, err := rt.SilkWallet.GrantSilk(c.AccountID, amount)
	if err != nil {
		return domain.MallBalance{}, err
	}
	log.WithFields(log.Fields{"grantor": grantor, "source": source, "character": c.Name, "amount": amount,
		"balance": balance.Silk}).Info("silk: granted")
	if rt.RecordSilkGrant != nil {
		rt.RecordSilkGrant(SilkGrant{Division: division, Grantor: grantor, Source: source, Character: c.Name,
			Account: c.AccountID, Amount: amount, Balance: balance.Silk})
	}
	// An open mall shows the new balance at once, as a purchase or an
	// earned credit does (MALL_BALANCE_CONTROL).
	if payload, err := json.Marshal(balance); err == nil && rt.PushCharacterFrames != nil {
		rt.PushCharacterFrames(division, c.Name, []wire.Frame{{Opcode: opMallBalance, Payload: payload}})
	}
	return balance, nil
}

/*
================
GrantGMSilk

The gmcommand port: the GM's /SILK. Reports the new balance.
================
*/
func (rt *Runtime) GrantGMSilk(division, sender, name string, amount uint32) (uint32, bool) {
	balance, err := rt.GrantSilk(division, sender, "gm", name, amount)
	if err != nil {
		log.WithError(err).WithFields(log.Fields{"gm": sender, "character": name}).Info("silk: GM grant refused")
		return 0, false
	}
	return balance.Silk, true
}
