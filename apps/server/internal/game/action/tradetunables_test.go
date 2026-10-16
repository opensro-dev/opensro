/*
===========================================================================

tradetunables_test.go - the trade group's percents are live tunables

Each percent the trade-job bonus, the thief party bonus and the Jangan
routes pay is read from the tunables snapshot: the defaults pay what the
rules always paid (the other tests run at the defaults), a changed value
pays the new percent, and a staff change takes effect at the next action.

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
tradeSettings

A snapshot with the given environment values over the defaults.
================
*/
func tradeSettings(t *testing.T, env map[string]string) *tunables.Settings {
	t.Helper()
	s, err := tunables.Parse(func(name string) string { return env[name] })
	if err != nil {
		t.Fatal(err)
	}
	return s
}

/*
================
TestTradeJobBonusReadsItsPercents

Raised per-head percents and caps change both shares; zero turns a share's
bonus off without touching the native split.
================
*/
func TestTradeJobBonusReadsItsPercents(t *testing.T) {
	in := commerce.TradeRewardInput{Credit: 100000, Profit: 40000, Job: domain.JobTrader, Party: true, Traders: 2, Hunters: 3}
	native := commerce.DistributeTradeReward(in)
	share := native.Actor.Gold - (in.Credit - in.Profit)
	for _, tc := range []struct {
		name                         string
		env                          map[string]string
		traderPercent, hunterPercent int64
	}{
		{"defaults", nil, 20, 20},
		{"raised", map[string]string{
			"SRO_TRADE_JOB_TRADER_PERCENT_PER_HUNTER": "15", "SRO_TRADE_JOB_TRADER_PERCENT_CAP": "100",
			"SRO_TRADE_JOB_HUNTER_PERCENT_PER_TRADER": "25", "SRO_TRADE_JOB_HUNTER_PERCENT_CAP": "40",
		}, 45, 40},
		{"zero", map[string]string{
			"SRO_TRADE_JOB_TRADER_PERCENT_PER_HUNTER": "0", "SRO_TRADE_JOB_HUNTER_PERCENT_CAP": "0",
		}, 0, 0},
	} {
		got := native
		applyTradeJobBonus(&got, in, tradeSettings(t, tc.env))
		if want := native.Actor.Gold + share*tc.traderPercent/100; got.Actor.Gold != want {
			t.Fatalf("%s: trader gold %d, want %d", tc.name, got.Actor.Gold, want)
		}
		if want := native.Hunter.Gold + native.Hunter.Gold*tc.hunterPercent/100; got.Hunter.Gold != want {
			t.Fatalf("%s: hunter gold %d, want %d", tc.name, got.Hunter.Gold, want)
		}
	}
}

/*
================
TestRoutePercentReadsItsTunables

Each town's route reads its own tunable, both ways.
================
*/
func TestRoutePercentReadsItsTunables(t *testing.T) {
	origins := map[uint32]tradeTown{1: townJangan, 2: townDonwhang, 3: townHotan, 4: townSamarkand, 5: townConstantinople}
	s := tradeSettings(t, map[string]string{
		"SRO_TRADE_ROUTE_DONWHANG_PERCENT": "70", "SRO_TRADE_ROUTE_HOTAN_PERCENT": "0",
		"SRO_TRADE_ROUTE_SAMARKAND_PERCENT": "250", "SRO_TRADE_ROUTE_CONSTANTINOPLE_PERCENT": "500",
	})
	for _, tc := range []struct {
		npc  string
		item uint32
		want int64
	}{
		{"NPC_WC_SPECIAL", 1, 70}, {"NPC_CH_SPECIAL", 2, 70},
		{"NPC_KT_SPECIAL", 1, 0}, {"NPC_CH_SPECIAL", 3, 0},
		{"NPC_CA_SPECIAL", 1, 250}, {"NPC_CH_SPECIAL", 4, 250},
		{"NPC_EU_SPECIAL", 1, 500}, {"NPC_CH_SPECIAL", 5, 500},
		{"NPC_WC_SPECIAL", 5, 0},
	} {
		if got := routePercent(origins, tc.npc, tc.item, s); got != tc.want {
			t.Fatalf("%s item %d: %d%%, want %d%%", tc.npc, tc.item, got, tc.want)
		}
	}
}

/*
================
TestTradeRouteProfitFollowsALiveChange

The site's quote reads the running snapshot at each call: a staff change
between two quotes reaches the second, never the first.
================
*/
func TestTradeRouteProfitFollowsALiveChange(t *testing.T) {
	quote := func(npcCodename string, itemID uint32) int64 {
		origins := map[uint32]tradeTown{1: townJangan}
		return routePercent(origins, npcCodename, itemID, tunables.Current())
	}
	restore := tunables.Swap(tradeSettings(t, nil))
	defer restore()
	if got := quote("NPC_EU_SPECIAL", 1); got != 300 {
		t.Fatalf("before the change: %d%%, want the default 300%%", got)
	}
	if applied, err := tunables.Apply(tunables.TradeRouteConstantinoplePercent, "120"); err != nil || !applied {
		t.Fatalf("live change: applied=%v err=%v", applied, err)
	}
	if got := quote("NPC_EU_SPECIAL", 1); got != 120 {
		t.Fatalf("after the change: %d%%, want 120%%", got)
	}
}

/*
================
TestThiefPartyPercentReadsItsTunables

A party of three thieves earns the snapshot's per-thief percent up to its
cap: 15 each capped at 40 pays 40, where the defaults pay 30.
================
*/
func TestThiefPartyPercentReadsItsTunables(t *testing.T) {
	rt, _, actor, _ := newCombatTestRuntime(t, 100)
	source := fixtureCharacters(rt.deps.(*enterworld.Deps).Characters)
	gids := []uint32{enterworld.ObjectIDForCharacter(actor)}
	for i := 0; i < 3; i++ {
		member := *actor
		member.ID, member.Name, member.MissionInventory = int64(40+i), "thief"+string(rune('a'+i)), nil
		dressThief(&member)
		source[testDivision] = append(source[testDivision], &member)
		gids = append(gids, enterworld.ObjectIDForCharacter(&member))
	}
	rt.ThiefPartyBonus = true
	rt.RewardParties = func(string) []RewardParty { return []RewardParty{{Members: gids}} }
	party := rt.rewardPartyOf(testDivision, gids[0])
	if got := rt.thiefPartyPercent(testDivision, party, tradeSettings(t, nil)); got != 30 {
		t.Fatalf("defaults: %d%%, want 30%%", got)
	}
	tuned := tradeSettings(t, map[string]string{"SRO_THIEF_PARTY_PERCENT_EACH": "15", "SRO_THIEF_PARTY_PERCENT_CAP": "40"})
	if got := rt.thiefPartyPercent(testDivision, party, tuned); got != 40 {
		t.Fatalf("tuned: %d%%, want the 40%% cap", got)
	}
}
