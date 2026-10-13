/*
===========================================================================

betarates_test.go - the beta growth the drop tables publish

===========================================================================
*/
package catalogue

import (
	"testing"

	"opensro.online/server/internal/game/progression"
)

/*
================
TestBetaRatesPublishTheSkillExpRate

The site's guide reads the skill EXP multiplier from droptables.json, so
the export carries the value the GameWorld uses (BetaGrowthFromEnv): an
operator's rate, the default while it is unset, and no beta object at
all when beta growth is off (the original rates, x1).
================
*/
func TestBetaRatesPublishTheSkillExpRate(t *testing.T) {
	monsters := []Monster{{Level: 30, Country: 3, Table: equipmentTableKey(30, 3)}}
	itemID := func(codename string) uint32 { return uint32(len(codename)) }

	t.Setenv(progression.EnvBetaGrowth, "on")
	t.Setenv(progression.EnvBetaSkillExpRate, "37")
	if beta := BuildDropTables(monsters, itemID, Header{}).Beta; beta == nil || beta.SkillExpRate != 37 {
		t.Fatalf("operator rate: beta %+v, want skillExpRate 37", beta)
	}

	t.Setenv(progression.EnvBetaSkillExpRate, "")
	want := progression.BetaGrowthFromEnv().SkillExpRate
	if beta := BuildDropTables(monsters, itemID, Header{}).Beta; beta == nil || beta.SkillExpRate != want || want <= 1 {
		t.Fatalf("unset rate: beta %+v, want the growth default %d", beta, want)
	}

	t.Setenv(progression.EnvBetaGrowth, "off")
	if beta := BuildDropTables(monsters, itemID, Header{}).Beta; beta != nil {
		t.Fatalf("growth off published %+v; the original rates have no beta object", beta)
	}
}
