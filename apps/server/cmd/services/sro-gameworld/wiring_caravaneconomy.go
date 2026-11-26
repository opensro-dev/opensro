/*
===========================================================================

wiring_caravaneconomy.go - port-only caravan gold settlements

Port-only, not native (SRO_AI_CARAVANS). The caravan runtime decides what
is owed (caravanai: the hire price, the escort reward and its daily cap
in the escort-day ledger); the gold moves here, through the character's
own mutation door, so the store's economy ledger records each change
under its door (domain.economyDoorReasons: caravan-hire, caravan-escort,
caravan-hire-payout). Every door refuses a character pending deletion
and a balance it would carry past wire.MaxGold, and changes nothing then;
only the hire payout, the player's own gold, then fills the purse to the
cap.

===========================================================================
*/
package main

import (
	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
caravanGoldDoor

Moves amount (signed) through door when allowed holds under the door.
false when the character is unknown, runtime-only or pending deletion,
when allowed refuses, or when the balance would leave [0, MaxGold]. An
online character is pushed its new balance.
================
*/
func (game *gameplayPlane) caravanGoldDoor(division, name, door string, amount int64, allowed func(c *enterworld.Character) bool) bool {
	c := game.deps.CharacterByName(division, name)
	if c == nil || amount == 0 {
		return false
	}
	moved := game.deps.Update(c, door, func() bool {
		if c.RuntimeOnly || c.DeletePending || (allowed != nil && !allowed(c)) {
			return false
		}
		balance := int64(0)
		if c.Gold != nil {
			balance = *c.Gold
		}
		if balance < 0 || (amount > 0 && balance > int64(wire.MaxGold)-amount) || (amount < 0 && balance < -amount) {
			return false
		}
		balance += amount
		c.Gold = &balance
		return true
	})
	if moved {
		game.publishCaravanGold(division, c)
	}
	return moved
}

/*
================
caravanHireFee

The job rules are the AI runtime's (caravanai.CanHire); this only takes
gold, from a player wearing a job suit.
================
*/
func (game *gameplayPlane) caravanHireFee(division, player string, fee int64) bool {
	if fee <= 0 {
		return false
	}
	return game.caravanGoldDoor(division, player, "caravan-hire", -fee, func(c *enterworld.Character) bool {
		return enterworld.DressedJob(c) != domain.JobNone
	})
}

/*
================
caravanHirePayout

A hired Trader's sale or returned goods cost, or a refused hire's fee:
the player's own gold coming back, so no daily cap. When the purse cannot
hold all of it, it is filled to wire.MaxGold and only the excess is lost,
logged with its amount (the goods are already sold, so there is nothing
to refuse); the door re-checks the bound, so a balance that moved
meanwhile refuses the fill instead of passing the cap.
================
*/
func (game *gameplayPlane) caravanHirePayout(division, player string, gold int64) bool {
	if gold <= 0 {
		return false
	}
	if game.caravanGoldDoor(division, player, "caravan-hire-payout", gold, nil) {
		return true
	}
	room := int64(wire.MaxGold) - game.caravanGold(division, player)
	if room > 0 && room < gold && game.caravanGoldDoor(division, player, "caravan-hire-payout", room, nil) {
		log.WithFields(log.Fields{"player": player, "gold": gold, "lost": gold - room}).Warn("caravan hire payout filled the purse to its cap; the excess is lost")
		return true
	}
	log.WithFields(log.Fields{"player": player, "gold": gold}).Warn("caravan hire payout refused: no loaded character, or the purse is full")
	return false
}

/*
================
caravanGold

The character's balance, 0 when it is unknown.
================
*/
func (game *gameplayPlane) caravanGold(division, name string) int64 {
	c := game.deps.CharacterByName(division, name)
	if c == nil {
		return 0
	}
	var balance int64
	game.deps.Read(division, func() {
		if c.Gold != nil {
			balance = *c.Gold
		}
	})
	return balance
}

/*
================
caravanEscortReward

Pays a completed escort's reward to a living Hunter and reports whether
the gold moved. The runtime reserved the Hunter's daily slot first and
gives it back on false (caravanai payEscort), so a refused payment costs
no slot.
================
*/
func (game *gameplayPlane) caravanEscortReward(division, player string, reward int64, _ string) bool {
	if reward <= 0 {
		return false
	}
	return game.caravanGoldDoor(division, player, "caravan-escort", reward, func(c *enterworld.Character) bool {
		return enterworld.DressedJob(c) == domain.JobHunter && enterworld.CharacterAlive(c)
	})
}

/*
================
publishCaravanGold
================
*/
func (game *gameplayPlane) publishCaravanGold(division string, c *enterworld.Character) {
	if game.presence == nil {
		return
	}
	var balance uint64
	game.deps.Read(division, func() {
		if c.Gold != nil {
			balance = uint64(*c.Gold)
		}
	})
	if session, ok := game.presence.SessionByName(division, c.Name); ok && session.WorldReady() {
		_ = session.Send(wire.OpPointsUpdate, (wire.GoldRefresh{Balance: balance, Notify: true}).Encode())
	}
}
