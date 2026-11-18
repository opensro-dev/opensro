/*
===========================================================================
betamastery_test.go - beta training and return to native race budgets
===========================================================================
*/
package progression

import (
	"testing"
	"time"

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

/*
================
TestEarlyBetaCharactersKeepTheBetaBudget

SRO_BETA_MASTERY_EARLY (owner, 2026-11-18, #3551): a character created
before the native totals went live, over its native total, trains again;
one created since is held to the native total; with the switch off both
are. Saved training never changes.
================
*/
func TestEarlyBetaCharactersKeepTheBetaBudget(t *testing.T) {
	stamp := func(offset time.Duration) string {
		return enterworld.EarlyBetaCutoff.Add(offset).UTC().Format("2006-01-02T15:04:05.000Z")
	}
	for _, row := range []struct {
		name    string
		created string
		early   bool
		trains  bool
	}{
		{"early character, switch on", stamp(-24 * time.Hour), true, true},
		{"new character, switch on", stamp(time.Minute), true, false},
		{"early character, switch off", stamp(-24 * time.Hour), false, false},
	} {
		for _, c := range []*enterworld.Character{testCharacter(), testEuCharacter(40)} {
			c.CreatedAt = row.created
			id := c.Masteries[0].ID
			loadMasterySum(t, c, id, 300)
			rt := newTestRuntime(c)
			rt.BetaMasteryEarly = row.early
			sp := *c.SkillPoints
			result := rt.HandleMasteryLevelUp(testDivision, c, masteryPayload(id, 1))
			if !row.trains {
				assertTotalLimitRefusal(t, result)
				if masteryLevelSum(c) != 300 || *c.SkillPoints != sp {
					t.Fatalf("%s: a refusal changed saved training", row.name)
				}
				continue
			}
			if result.Frames[0].Payload[0] != wire.ResultSuccess || masteryLevelSum(c) != 301 {
				t.Fatalf("%s: training refused: %+v", row.name, result)
			}
		}
	}
}
