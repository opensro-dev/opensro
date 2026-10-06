/*
===========================================================================
tradereward_test.go - frozen native rewards and party settlement examples
===========================================================================
*/
package commerce

import (
	"encoding/json"
	"opensro.online/server/internal/domain"
	"os"
	"testing"
)

/*
================
TestTradeRewardsAgainstNativeInstructions
================
*/
func TestTradeRewardsAgainstNativeInstructions(t *testing.T) {
	raw, err := os.ReadFile("testdata/native-trade-rewards.json")
	if err != nil {
		t.Fatal(err)
	}
	var corpus struct {
		Weekly     []struct{ Current, Delta, Value int32 }
		Experience []struct {
			Job           uint8
			OneStar       bool
			Profit, Value int64
		}
	}
	if err := json.Unmarshal(raw, &corpus); err != nil {
		t.Fatal(err)
	}
	if len(corpus.Weekly) != 72 || len(corpus.Experience) != 110 {
		t.Fatal("incomplete native reward corpus")
	}
	for _, row := range corpus.Weekly {
		if got := AddWeeklyTradeReward(row.Current, row.Delta); got != row.Value {
			t.Fatalf("weekly %+v got %d", row, got)
		}
	}
	for _, row := range corpus.Experience {
		if got := TradeJobExperience(row.Job, row.Profit, row.OneStar); got != row.Value {
			t.Fatalf("experience %+v got %d", row, got)
		}
	}
}

/*
================
TestTradePartyGoldAndExperienceDistribution
================
*/
func TestTradePartyGoldAndExperienceDistribution(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   TradeRewardInput
		want TradeRewardDistribution
	}{
		{"origin", TradeRewardInput{Credit: 150, Job: domain.JobTrader, Party: true, Traders: 3, Hunters: 2}, TradeRewardDistribution{Actor: TradeReward{Gold: 150}}},
		{"solo", TradeRewardInput{Credit: 1100, Profit: 100, Job: domain.JobTrader}, TradeRewardDistribution{Actor: TradeReward{Gold: 1100, Experience: 50}}},
		{"hunter escort", TradeRewardInput{Credit: 1100, Profit: 100, Job: domain.JobTrader, Party: true, Traders: 1, Hunters: 1}, TradeRewardDistribution{Actor: TradeReward{Gold: 1080, Experience: 50}, Hunter: TradeReward{Gold: 20, Experience: 20}}},
		{"thieves", TradeRewardInput{Credit: 300, Profit: 400, Job: domain.JobThief, Party: true, Thieves: 2}, TradeRewardDistribution{Actor: TradeReward{Gold: 157, Experience: 83}, Thief: TradeReward{Gold: 157, Experience: 83}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := DistributeTradeReward(tc.in); got != tc.want {
				t.Fatalf("got %+v want %+v", got, tc.want)
			}
		})
	}
}
