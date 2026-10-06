/*
===========================================================================

tradereward.go - shard-owned hunter and thief trade reward pools

AQ_AddTrijobReward (465220) credits hunters for trader sales and thieves
for thief sales. These funds belong to the shard, not the selling character.

===========================================================================
*/
package domain

import "math"

/*
================
TradeRewardPool
================
*/
type TradeRewardPool struct {
	Hunters int64 `json:"hunters"`
	Thieves int64 `json:"thieves"`
}

/*
================
Credit

Apply the native pool routing. Refuse overflow before changing the pool.
================
*/
func (p *TradeRewardPool) Credit(job uint8, gold int64) bool {
	var balance *int64
	switch job {
	case JobTrader:
		balance = &p.Hunters
	case JobThief:
		balance = &p.Thieves
	default:
		return gold == 0
	}
	if gold < 0 || *balance < 0 || gold > math.MaxInt64-*balance {
		return false
	}
	*balance += gold
	return true
}
