/*
===========================================================================
betamastery_test.go - beta training and return to native race budgets
===========================================================================
*/
package progression

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
TestBetaMasteryConfiguration
================
*/
func TestBetaMasteryConfiguration(t *testing.T) {
	for _, text := range []string{"", "off", "false", "0", "on", "true", "1", " ON ", "typo"} {
		t.Setenv(EnvBetaMastery, text)
		got, err := BetaMasteryFromEnv()
		if text == "typo" {
			if err == nil {
				t.Fatal("invalid switch admitted")
			}
			continue
		}
		want := int64(0)
		if text == "on" || text == "true" || text == "1" || text == " ON " {
			want = 5000
		}
		if err != nil || got != want {
			t.Fatalf("%q: %d, %v", text, got, err)
		}
	}
}

/*
================
TestBetaMasteryBothRacesAndNativeRestoration

Train beyond each native allowance, then disable the beta policy. Existing
levels and SP survive; further training is refused by the original budget.
================
*/
func TestBetaMasteryBothRacesAndNativeRestoration(t *testing.T) {
	for _, c := range []*enterworld.Character{testCharacter(), testEuCharacter(40)} {
		id := c.Masteries[0].ID
		loadMasterySum(t, c, id, 300)
		rt := newTestRuntime(c)
		rt.MasteryTotalOverride = BetaTotalMasteryCap
		if rt.masteryAllowance(c) != 5000 {
			t.Fatal("wrong beta allowance")
		}
		result := rt.HandleMasteryLevelUp(testDivision, c, masteryPayload(id, 1))
		if result.Frames[0].Payload[0] != wire.ResultSuccess || masteryLevelSum(c) != 301 {
			t.Fatalf("beta training refused: %+v", result)
		}
		sp := *c.SkillPoints
		rt.MasteryTotalOverride = 0
		assertTotalLimitRefusal(t, rt.HandleMasteryLevelUp(testDivision, c, masteryPayload(id, 1)))
		if masteryLevelSum(c) != 301 || *c.SkillPoints != sp {
			t.Fatal("native restoration changed saved training")
		}
	}
}

/*
================
TestBetaMasteryKeepsLevelAndSPGates
================
*/
func TestBetaMasteryKeepsLevelAndSPGates(t *testing.T) {
	for _, c := range []*enterworld.Character{testCharacter(), testEuCharacter(40)} {
		id := c.Masteries[0].ID
		rt := newTestRuntime(c)
		rt.MasteryTotalOverride = BetaTotalMasteryCap
		c.Level = int64Ptr(1)
		c.Masteries[0].Level = 1
		result := rt.HandleMasteryLevelUp(testDivision, c, masteryPayload(id, 1))
		if result.Frames[0].Payload[1] != wire.ErrCodeMasteryLevelLimit {
			t.Fatal("beta bypassed character level", result)
		}
		c.Level = int64Ptr(40)
		c.SkillPoints = int64Ptr(0)
		result = rt.HandleMasteryLevelUp(testDivision, c, masteryPayload(id, 1))
		if result.Frames[0].Payload[0] != wire.ResultError || c.Masteries[0].Level != 1 {
			t.Fatal("beta bypassed SP", result)
		}
	}
}
