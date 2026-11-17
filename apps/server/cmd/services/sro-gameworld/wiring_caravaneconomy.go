/*
===========================================================================

wiring_caravaneconomy.go - port-only caravan gold settlements

The daily allowance and gold credit commit together on the player's own
record. A restart cannot reset the allowance or pay a completed run twice.

===========================================================================
*/
package main

import (
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

const caravanDailyRewardLimit = 3

/*
================
caravanHireFee
================
*/
func (game *gameplayPlane) caravanHireFee(division, player string, fee int64) bool {
	c := game.deps.CharacterByName(division, player)
	if c == nil || fee <= 0 {
		return false
	}
	paid := game.deps.Update(c, "caravan-hire", func() bool {
		if c.RuntimeOnly || enterworld.DressedJob(c) != domain.JobTrader || c.Gold == nil || *c.Gold < fee {
			return false
		}
		gold := *c.Gold - fee
		c.Gold = &gold
		return true
	})
	if paid {
		game.publishCaravanGold(division, c)
	}
	return paid
}

/*
================
caravanEscortReward
================
*/
func (game *gameplayPlane) caravanEscortReward(division, player string, reward int64, _ string) {
	c := game.deps.CharacterByName(division, player)
	if c == nil || reward <= 0 {
		return
	}
	day := time.Now().UTC().Unix() / 86400
	paid := game.deps.Update(c, "caravan-escort", func() bool {
		if c.RuntimeOnly || enterworld.DressedJob(c) != domain.JobHunter || !enterworld.CharacterAlive(c) {
			return false
		}
		count := c.CaravanRewardCount
		if c.CaravanRewardDay != day {
			count = 0
		}
		if count >= caravanDailyRewardLimit {
			return false
		}
		gold := int64(0)
		if c.Gold != nil {
			gold = *c.Gold
		}
		if gold < 0 || gold > int64(wire.MaxGold)-reward {
			return false
		}
		gold += reward
		c.Gold, c.CaravanRewardDay, c.CaravanRewardCount = &gold, day, count+1
		return true
	})
	if paid {
		game.publishCaravanGold(division, c)
	}
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
