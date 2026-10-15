/*
===========================================================================

tradejobbonus_test.go - traders and hunters paid more for travelling together

Port-only, not native: off, the native distribution is paid unchanged; on,
the trader's profit share and the hunters' gold rise by the capped percents.

===========================================================================
*/
package action

import (
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/commerce"
	"opensro.online/server/internal/game/tunables"
)

/*
================
TestTradeJobBonusPercentsAndCaps
================
*/
func TestTradeJobBonusPercentsAndCaps(t *testing.T) {
	for _, tc := range []struct {
		name                         string
		in                           commerce.TradeRewardInput
		traderPercent, hunterPercent int64
	}{
		{"one of each", commerce.TradeRewardInput{Credit: 100000, Profit: 40000, Job: domain.JobTrader, Party: true, Traders: 1, Hunters: 1}, 10, 10},
		{"trader capped", commerce.TradeRewardInput{Credit: 100000, Profit: 40000, Job: domain.JobTrader, Party: true, Traders: 2, Hunters: 4}, 20, 20},
		{"hunter capped", commerce.TradeRewardInput{Credit: 100000, Profit: 40000, Job: domain.JobTrader, Party: true, Traders: 8, Hunters: 1}, 10, 60},
		{"no hunter", commerce.TradeRewardInput{Credit: 100000, Profit: 40000, Job: domain.JobTrader, Party: true, Traders: 3}, 0, 0},
		{"no profit", commerce.TradeRewardInput{Credit: 100000, Job: domain.JobTrader, Party: true, Traders: 1, Hunters: 1}, 0, 0},
		{"thief sale", commerce.TradeRewardInput{Credit: 100000, Profit: 40000, Job: domain.JobThief, Party: true, Thieves: 2, Hunters: 1}, 0, 0},
	} {
		native := commerce.DistributeTradeReward(tc.in)
		got := native
		applyTradeJobBonus(&got, tc.in, tunables.Current())
		share := native.Actor.Gold - (tc.in.Credit - tc.in.Profit)
		if tc.traderPercent == 0 {
			share = 0
		}
		if want := native.Actor.Gold + share*tc.traderPercent/100; got.Actor.Gold != want {
			t.Fatalf("%s: trader gold %d, want %d (native %d)", tc.name, got.Actor.Gold, want, native.Actor.Gold)
		}
		if want := native.Hunter.Gold + native.Hunter.Gold*tc.hunterPercent/100; got.Hunter.Gold != want {
			t.Fatalf("%s: hunter gold %d, want %d (native %d)", tc.name, got.Hunter.Gold, want, native.Hunter.Gold)
		}
		if got.Actor.Experience != native.Actor.Experience || got.Hunter.Experience != native.Hunter.Experience || got.Thief != native.Thief {
			t.Fatalf("%s: the bonus moved EXP or a thief share: %+v vs %+v", tc.name, got, native)
		}
	}
}

/*
================
TestTraderSaleBonusFollowsTheFlag

The real sale path, a trader and a hunter in one party, off and on.
================
*/
func TestTraderSaleBonusFollowsTheFlag(t *testing.T) {
	rt, _, trader, hunter := newPvpPair(t)
	dressForJob(rt, trader, 9303, "ITEM_CH_M_TRADE_TRADER_04", 1)
	dressForJob(rt, hunter, 9304, "ITEM_CH_M_TRADE_HUNTER_04", 3)
	roster := tradeRoster{members: []*enterworld.Character{trader, hunter}, party: true}
	sale := commerce.TradeRewardInput{Credit: 100000, Profit: 40000}
	paid := func() map[string]int64 {
		out := map[string]int64{}
		for _, payout := range rt.tradeRewards(testDivision, trader, roster, sale, tunables.Current()) {
			out[payout.character.Name] = payout.Gold
		}
		return out
	}
	rt.TradeJobBonus = false
	native := paid()
	expected := commerce.DistributeTradeReward(commerce.TradeRewardInput{Credit: 100000, Profit: 40000, Job: domain.JobTrader, Party: true, Traders: 1, Hunters: 1})
	if native[trader.Name] != expected.Actor.Gold || native[hunter.Name] != expected.Hunter.Gold {
		t.Fatalf("off: paid %v, native %+v", native, expected)
	}
	rt.TradeJobBonus = true
	bonus := paid()
	share := native[trader.Name] - (100000 - 40000)
	if bonus[trader.Name] != native[trader.Name]+share/10 || bonus[hunter.Name] != native[hunter.Name]+native[hunter.Name]/10 {
		t.Fatalf("on: paid %v, native %v", bonus, native)
	}
}

/*
================
TestTradeJobBonusFromEnv
================
*/
func TestTradeJobBonusFromEnv(t *testing.T) {
	for value, want := range map[string]bool{"": false, "off": false, "on": true, "1": true, "TRUE": true} {
		t.Setenv(EnvTradeJobBonus, value)
		if got := TradeJobBonusFromEnv(); got != want {
			t.Fatalf("%q: %v, want %v", value, got, want)
		}
	}
}
