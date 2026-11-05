/*
===========================================================================

quests_test.go - the quest rewards the catalogue publishes

===========================================================================
*/
package catalogue

import (
	"fmt"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/progression"
	"opensro.online/server/internal/game/quest"
	"opensro.online/server/internal/testsupport/gamedatatest"
	"opensro.online/server/internal/testsupport/tier"
)

/*
================
TestQuestRewardsPublishWhatTheBetaPays

Issue #2539: the website showed the native EXP/SP while the server pays
them times the beta growth band. The export now prices each quest at
its own level with the grant's own GrowthRates (PaidRewards): a quest
in a fixed band publishes the banded amounts beside the native ones, a
quest outside every band publishes no paidExp (its EXP is native,
though its skill EXP still takes the flat rate), and with the beta off
nothing publishes a paid amount at all.
================
*/
func TestQuestRewardsPublishWhatTheBetaPays(t *testing.T) {
	tier.Long(t, "quest rewards publish the beta-paid amounts")
	dir := gamedatatest.TextdataDir(t)
	catalog := quest.NewCatalog(dir)
	defs, err := quest.LoadDefinitions(catalog, enterworld.NewTextdataItems(dir))
	if err != nil {
		t.Fatal(err)
	}
	listed := []string{}
	targetLevel := 0
	for _, def := range defs.All() {
		listed = append(listed, def.Codename)
		if targetLevel == 0 && def.RewardExp > 0 && def.Level >= 1 {
			targetLevel = int(def.Level)
		}
	}
	if targetLevel == 0 {
		t.Fatal("no quest with an EXP reward in the shipped definitions")
	}
	source := QuestSource{Catalog: catalog, Definitions: defs, Text: func(string) string { return "" },
		ItemID: func(string) uint32 { return 1 }, Listed: listed, Levels: enterworld.NewTextdataLevels(dir)}

	t.Setenv(progression.EnvBetaGrowth, "on")
	t.Setenv(progression.EnvBetaGrowthBands, fmt.Sprintf("%d-%d:1639", targetLevel, targetLevel))
	t.Setenv(progression.EnvBetaSkillExpRate, "100")
	banded := 0
	for _, q := range BuildQuests(source) {
		if q.Level == targetLevel && q.Rewards.Exp > 0 {
			banded++
			if want := q.Rewards.Exp * 1639; q.Rewards.PaidExp != want {
				t.Errorf("%s (level %d) paidExp %d, want %d (native %d x 1639)", q.Codename, q.Level, q.Rewards.PaidExp, want, q.Rewards.Exp)
			}
			if q.Rewards.SkillExp > 0 {
				if want := q.Rewards.SkillExp * 1639 * 100; q.Rewards.PaidSkillExp != want {
					t.Errorf("%s paidSkillExp %d, want %d (native %d x 1639 x 100)", q.Codename, q.Rewards.PaidSkillExp, want, q.Rewards.SkillExp)
				}
			}
			continue
		}
		if q.Rewards.PaidExp != 0 {
			t.Errorf("%s (level %d) published paidExp %d outside the level-%d band", q.Codename, q.Level, q.Rewards.PaidExp, targetLevel)
		}
		if q.Rewards.SkillExp > 0 {
			if want := q.Rewards.SkillExp * 100; q.Rewards.PaidSkillExp != want {
				t.Errorf("%s (level %d) paidSkillExp %d, want %d (the flat rate alone outside a band)", q.Codename, q.Level, q.Rewards.PaidSkillExp, want)
			}
		}
	}
	if banded == 0 {
		t.Fatalf("no level-%d quest with an EXP reward was exported", targetLevel)
	}

	t.Setenv(progression.EnvBetaGrowth, "off")
	for _, q := range BuildQuests(source) {
		if q.Rewards.PaidExp != 0 || q.Rewards.PaidSkillExp != 0 {
			t.Fatalf("beta off published paid rewards on %s: %+v", q.Codename, q.Rewards)
		}
	}
}
