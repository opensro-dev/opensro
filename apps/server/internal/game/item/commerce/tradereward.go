/*
===========================================================================

tradereward.go - native trade gold, job experience and weekly contributions

5BD180/5BD510 split rewards among nearby dressed party members. Float32
stores follow the disassembly; keep arithmetic between stores in float64.

===========================================================================
*/
package commerce

import "opensro.online/server/internal/domain"

const (
	tradePartyBonus     = 0.05000000074505806
	tradeHunterBonus    = 1.0499999523162842
	weeklyRewardWarning = 2000000000
)

/*
================
TradeRewardInput
================
*/
type TradeRewardInput struct {
	Credit, Profit            int64
	Job                       uint8
	OneStar, Party            bool
	Traders, Thieves, Hunters int
}

/*
================
TradeReward
================
*/
type TradeReward struct{ Gold, Experience int64 }

/*
================
TradeRewardDistribution
================
*/
type TradeRewardDistribution struct{ Actor, Hunter, Thief TradeReward }

/*
================
TradeJobExperience

410520 spills the profit-times-job multiplier to float32 before CRT_ftol.
================
*/
func TradeJobExperience(job uint8, profit int64, oneStar bool) int64 {
	multiplier := float32(1)
	switch job {
	case domain.JobTrader:
		multiplier = 0.5
		if oneStar {
			multiplier = 0.8
		}
	case domain.JobThief:
		multiplier = 0.4
	case domain.JobHunter:
	default:
		return 0
	}
	value := float32(float64(profit) * float64(multiplier))
	if !(value > 0) {
		value = 1
	}
	return int64(value)
}

/*
================
DistributeTradeReward

Callers count members using 5BCFF0's role, alive, world and range predicates.
Zero-profit origin sales take the ordinary gold path and award no job EXP.
================
*/
func DistributeTradeReward(in TradeRewardInput) TradeRewardDistribution {
	out := TradeRewardDistribution{Actor: TradeReward{Gold: in.Credit}}
	if in.Profit <= 0 {
		return out
	}
	out.Actor.Experience = TradeJobExperience(in.Job, in.Profit, in.OneStar)
	if !in.Party {
		return out
	}
	if in.Job == domain.JobThief && in.Thieves > 0 {
		bonus := float32(float64(in.Thieves-1)*tradePartyBonus + 1)
		out.Thief.Gold = int64(float64(in.Credit) * float64(bonus) / float64(in.Thieves))
		out.Thief.Experience = int64(float64(out.Actor.Experience) * float64(bonus) / float64(in.Thieves))
		out.Actor = out.Thief
	}
	if in.Job != domain.JobTrader || in.Traders == 0 {
		return out
	}
	bonus := float32(float64(in.Traders-1)*tradePartyBonus + 1)
	boosted := int64(float64(in.Profit) * float64(bonus))
	profit := float64(float32(boosted))
	out.Actor.Gold = in.Credit + int64(4/float64(in.Hunters+4)*profit) - in.Profit
	out.Actor.Experience = TradeJobExperience(in.Job, boosted, in.OneStar)
	if in.Hunters == 0 {
		return out
	}
	fraction := float32(float64(in.Hunters) / float64(in.Hunters+4))
	out.Hunter.Gold = int64(profit * float64(fraction) * tradeHunterBonus / float64(in.Hunters))
	expScale := float32((float64(in.Hunters-1)*tradePartyBonus + 1) / float64(in.Hunters+4))
	out.Hunter.Experience = int64(float64(TradeJobExperience(domain.JobHunter, boosted, false)) * float64(expScale))
	return out
}

/*
================
AddWeeklyTradeReward

60E0A0 sign-extends both words. Above two billion the native diagnostic arm
reuses the sum as its lower bound, then adds the delta again; preserve that
observable behavior rather than assuming the misleading old "Clamped" name.
================
*/
func AddWeeklyTradeReward(current, delta int32) int32 {
	sum := int64(current) + int64(delta)
	if sum <= 0 {
		return 0
	}
	value := int64(current)
	if sum > weeklyRewardWarning {
		value = sum
	}
	increased := value + int64(delta)
	if value < increased {
		value = increased
	} else if value >= weeklyRewardWarning {
		value = weeklyRewardWarning
	}
	return int32(value)
}
