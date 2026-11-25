/*
===========================================================================

caravan_gold.go - the AI caravan economy's gold doors (slice 5 of #3704)

Port-only, not native (SRO_AI_CARAVANS). The caravan runtime decides
what is owed; the gold itself moves here, through the character's own
mutation door like every other gold change, so the store's economy
ledger records it (doors caravan-escort-reward and caravan-hire-fee).
Nothing mints outside these two: an escort reward is created gold by
design of the epic (the owner's economy call), and a hire fee leaves
the hiring trader's purse before the hire binds - a purse that cannot
pay hires nobody.

===========================================================================
*/
package action

import (
	"errors"
	"fmt"

	log "github.com/sirupsen/logrus"
)

/*
================
CaravanEscortReward

Pays a completed AI-trader escort reward to the Hunter. The character
may be live or offline (the runtime records the paid day either way,
so an offline Hunter is never paid twice for one run).
================
*/
func (rt *Runtime) CaravanEscortReward(division, hunterName string, amount int64) (GoldAdjustment, error) {
	if amount <= 0 {
		return GoldAdjustment{}, fmt.Errorf("caravan escort reward must be positive, got %d", amount)
	}
	done, err := rt.adjustGoldThroughDoor(division, hunterName, amount, "caravan-escort-reward")
	if err != nil {
		return GoldAdjustment{}, err
	}
	log.WithFields(log.Fields{"character": done.Character, "amount": done.Applied,
		"before": done.Before, "after": done.After}).Info("gold: AI caravan escort reward")
	return done, nil
}

/*
================
CaravanHireFee

Charges a trader the AI Hunter hire fee. paid is false (with no error)
when the purse cannot cover the fee: the hire simply does not happen.
An unknown or deletion-pending character is an error, not a refusal.
================
*/
func (rt *Runtime) CaravanHireFee(division, traderName string, feeGold int64) (paid bool, err error) {
	if feeGold <= 0 {
		return false, fmt.Errorf("caravan hire fee must be positive, got %d", feeGold)
	}
	done, err := rt.adjustGoldThroughDoor(division, traderName, -feeGold, "caravan-hire-fee")
	if errors.Is(err, errGoldBelowZero) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	log.WithFields(log.Fields{"character": done.Character, "amount": done.Applied,
		"before": done.Before, "after": done.After}).Info("gold: AI caravan hire fee")
	return true, nil
}
