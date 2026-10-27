/*
===========================================================================

tradejobbonus.go - traders and hunters paid more for travelling together

Port-only, not native (owner decision 2026-10-14, for the balance advisor).
With SRO_TRADE_JOB_BONUS on, a dressed trader's profitable party sale pays:

	the trader   +10% of its profit share per eligible hunter, up to +20%
	each hunter  +10% of its gold share per eligible trader, up to +60%

The four percents are live tunables (game/tunables, the trade group); the
numbers above are their defaults.

"Eligible" is the native nearby job-member set (alive, same world, within 1000 units),
which the seller, a trader, is always in. Only gold moves; job and
character EXP stay native. The credit's cost part is never raised: the
trader's bonus applies to its share of the profit alone. Off, the default
here, is native; the GameWorld's Nomad job turns it on.

===========================================================================
*/
package action

import (
	"os"
	"strings"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/item/commerce"
	"opensro.online/server/internal/game/tunables"
)

// EnvTradeJobBonus turns the bonus on ("on", "1", "true").
const EnvTradeJobBonus = "SRO_TRADE_JOB_BONUS"

/*
================
TradeJobBonusFromEnv
================
*/
func TradeJobBonusFromEnv() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvTradeJobBonus))) {
	case "on", "1", "true":
		return true
	}
	return false
}

/*
================
applyTradeJobBonus

Raises a native distribution's trader profit share and hunter gold share.
in carries the eligible counts DistributeTradeReward used; s is the sale's
one snapshot of the percents.
================
*/
func applyTradeJobBonus(out *commerce.TradeRewardDistribution, in commerce.TradeRewardInput, s *tunables.Settings) {
	if !in.Party || in.Profit <= 0 || in.Job != domain.JobTrader || in.Traders == 0 || in.Hunters == 0 {
		return
	}
	traderPercent := min(in.Hunters*int(s.Int(tunables.TradeJobTraderPercentPerHunter)),
		int(s.Int(tunables.TradeJobTraderPercentCap)))
	if share := out.Actor.Gold - (in.Credit - in.Profit); share > 0 {
		out.Actor.Gold += share * int64(traderPercent) / 100
	}
	hunterPercent := min(in.Traders*int(s.Int(tunables.TradeJobHunterPercentPerTrader)),
		int(s.Int(tunables.TradeJobHunterPercentCap)))
	out.Hunter.Gold += out.Hunter.Gold * int64(hunterPercent) / 100
}
