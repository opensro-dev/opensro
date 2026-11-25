/*
===========================================================================

operator_gold.go - the operator's audited gold correction (operator tooling)

Port-only, not native: the original has no economy correction tool, and
editing the live store by hand would be overwritten or contradicted by
the running GameWorld. One signed adjustment runs through the character's
own mutation door like every other gold change, so the store's economy
ledger records it (door operator-adjust-gold, reason operator_adjust)
on a live or offline character alike, and an online character is pushed
its new balance. Only the operator credential reaches it (the Agent's
adjust-gold player operation); like the silk grant it is staff tooling,
not a gameplay rule, so it carries no SRO_ flag.

A debit that would take the balance below zero is refused whole - never
clamped - and the refusal reports the amount that could have applied.

===========================================================================
*/
package action

import (
	"errors"
	"fmt"

	log "github.com/sirupsen/logrus"

	"opensro.online/server/internal/game/item/wire"
)

// errGoldBelowZero marks the one refusal a caller may legitimately
// treat as an answer rather than a fault: the debit exceeds the
// balance. It wraps into the refusal's message, which keeps its
// operator wording (operator_gold_test.go pins it).
var errGoldBelowZero = errors.New("gold debit exceeds the balance")

/*
================
GoldAdjustment

One committed correction, for the operator audit: what was asked, what
moved, and the balances it moved between.
================
*/
type GoldAdjustment struct {
	Character string
	Requested int64
	Applied   int64
	Before    int64
	After     int64
}

/*
================
OperatorAdjustGold

Apply amount (signed) to the named character's gold through its mutation
door. The character may be live or offline; a connected session receives
the new balance as a gold refresh. A zero amount, an unknown or
deletion-pending character, an overflowing credit and a debit past zero
all refuse and change nothing; the below-zero refusal names the debit
that could have applied (the whole balance, down to exactly zero).
================
*/
func (rt *Runtime) OperatorAdjustGold(division, name string, amount int64) (GoldAdjustment, error) {
	done, err := rt.adjustGoldThroughDoor(division, name, amount, "operator-adjust-gold")
	if err != nil {
		return GoldAdjustment{}, err
	}
	log.WithFields(log.Fields{"character": done.Character, "amount": done.Applied,
		"before": done.Before, "after": done.After}).Info("gold: operator adjustment")
	return done, nil
}

/*
================
adjustGoldThroughDoor

The one signed gold movement every audited correction rides: the
operator's adjustment (operator-adjust-gold) and the AI caravan
economy (caravan-escort-reward, caravan-hire-fee; caravan_gold.go).
The door name is the store ledger's record of who moved the gold. A
zero amount, an unknown or deletion-pending character, an overflowing
credit and a debit past zero all refuse and change nothing; the
below-zero refusal wraps errGoldBelowZero and names the debit that
could have applied (the whole balance, down to exactly zero). An
online character is pushed its new balance.
================
*/
func (rt *Runtime) adjustGoldThroughDoor(division, name string, amount int64, door string) (GoldAdjustment, error) {
	if amount == 0 {
		return GoldAdjustment{}, fmt.Errorf("gold adjustment must not be zero")
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	c := rt.findCharacter(division, name)
	if c == nil {
		return GoldAdjustment{}, fmt.Errorf("character not found")
	}
	var done GoldAdjustment
	var refused error
	if !rt.deps.Update(c, door, func() bool {
		if c.DeletePending {
			refused = fmt.Errorf("character is pending deletion")
			return false
		}
		before := int64(0)
		if c.Gold != nil {
			before = *c.Gold
		}
		after := before + amount
		if amount > 0 && after < before {
			refused = fmt.Errorf("gold adjustment of %d would overflow %s's balance of %d", amount, c.Name, before)
			return false
		}
		if after < 0 {
			refused = fmt.Errorf("%w: gold adjustment of %d would take %s below zero: balance is %d, could apply %d",
				errGoldBelowZero, amount, c.Name, before, -before)
			return false
		}
		c.Gold = &after
		done = GoldAdjustment{Character: c.Name, Requested: amount, Applied: amount, Before: before, After: after}
		return true
	}) {
		if refused != nil {
			return GoldAdjustment{}, refused
		}
		return GoldAdjustment{}, fmt.Errorf("character gold adjustment refused")
	}
	// An online character sees the new balance at once, as any other gold
	// change shows it; the push hook reaches no session for an offline one.
	if rt.PushCharacterFrames != nil {
		rt.PushCharacterFrames(division, done.Character, []wire.Frame{goldFrame(rt.characterSnapshot(division, c))})
	}
	return done, nil
}
