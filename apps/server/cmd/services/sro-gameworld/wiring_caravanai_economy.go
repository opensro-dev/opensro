/*
===========================================================================

wiring_caravanai_economy.go - slice 5's money wiring for the caravan agents

Port-only, not native (SRO_AI_CARAVANS). The caravan runtime decides
what is owed; these hooks move it through the action runtime's own
doors: an escort reward credits the Hunter (door caravan-escort-
reward), a hire fee debits the trader before the hire binds (door
caravan-hire-fee), and a parley tribute drops the trader's own cargo -
real specialty goods of its origin town - where it stands. With the
flag off none of this is installed and nothing moves.

===========================================================================
*/
package main

import (
	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
caravanRewardHook

A kept, survived escort pays real gold to the Hunter. The runtime
already counted the payment against the Hunter's day before firing,
so a failure here is logged, never retried into a second payment.
================
*/
func (game *gameplayPlane) caravanRewardHook() func(division, hunterPlayerName string, rewardGold int64, traderAgentName string) {
	return func(division, hunterPlayerName string, rewardGold int64, traderAgentName string) {
		if game.items == nil {
			return
		}
		if _, err := game.items.CaravanEscortReward(division, hunterPlayerName, rewardGold); err != nil {
			log.WithError(err).WithFields(log.Fields{
				"hunter": hunterPlayerName, "trader": traderAgentName, "gold": rewardGold,
			}).Warn("caravan-ai: escort reward did not land")
		}
	}
}

/*
================
caravanHireFeeHook

Hiring an AI Hunter debits the trader's purse first; a purse that
cannot pay hires nobody.
================
*/
func (game *gameplayPlane) caravanHireFeeHook() func(division, traderPlayerName string, feeGold int64, hunterAgentName string) bool {
	return func(division, traderPlayerName string, feeGold int64, hunterAgentName string) bool {
		if game.items == nil {
			return false
		}
		paid, err := game.items.CaravanHireFee(division, traderPlayerName, feeGold)
		if err != nil {
			log.WithError(err).WithFields(log.Fields{
				"trader": traderPlayerName, "hunter": hunterAgentName, "gold": feeGold,
			}).Warn("caravan-ai: hire fee charge failed")
			return false
		}
		return paid
	}
}

/*
================
caravanParleyDropHook

A parley tribute is the trader's own cargo - real specialty goods of
its origin town, dropped where it stands; never goods conjured for
the occasion.
================
*/
func (game *gameplayPlane) caravanParleyDropHook() func(division, agentName string, regionID uint16, x, y, z float32, stacks int, stackGoldValue int64) {
	return func(division, agentName string, regionID uint16, x, y, z float32, stacks int, stackGoldValue int64) {
		if game.items == nil || game.caravanAgents == nil {
			return
		}
		codename, ok := game.caravanAgents.TradeGoodsFor(division, agentName)
		if !ok {
			log.WithField("agent", agentName).Warn("caravan-ai: parley cargo has no trade goods; nothing dropped")
			return
		}
		at := simulation.Spawn{RegionID: regionID, X: float64(x), Y: float64(y), Z: float64(z)}
		if dropped := game.items.DropCaravanCargo(division, codename, stacks, at, agentName); dropped != stacks {
			log.WithFields(log.Fields{
				"agent": agentName, "goods": codename, "wanted": stacks, "dropped": dropped,
			}).Warn("caravan-ai: parley cargo drop fell short")
		}
	}
}
