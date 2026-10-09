/*
===========================================================================

growth.go - the closed-beta growth rates (port-only, not native)

Native progression grants exactly what the reward formulae compute. For the
closed beta the operator can turn on faster growth with one environment
variable; unset, every grant is native. Nothing persists the rate: switching
back is unsetting the variable and restarting the game world.

EXP: every level costs about as many at-level kills as level 1 does. The
level table gives both the EXP a level needs (ExpRequired) and the EXP an
at-level monster yields (MonsterExpBasis, GUST_Mob_Exp), so their ratio is
the native kill count for that level. A gain at level L is multiplied by
kills(L) / kills(1), never below 1, so every level after the first
compresses to the level-1 pace (one level costs what 1 -> 2 costs). Every source scales the same way:
kills, party shares and quest rewards all pass through applyExperience.
A resurrection's refund of EXP lost at death is not a gain and stays
native (ExperienceRefundUpdater).

SP: skill EXP gains get the same level compression as EXP, then a flat
SkillExpRate on top. A flat rate alone fell behind: at level 42 EXP is worth
~738x native but SP only 100x, so testers had to hold a mastery gap to keep
their skills level with their character.

Drops: a kill makes every drop DropRate times over (the unique prepass, the
monster's assigned rewards and its ordinary passes of equipment and
consumables) and its capacity grows with them, so testers can find gear to
upgrade and alchemy materials often without inventing items. Gold keeps its
native heap count; every gold heap is GoldRate times its native amount,
because levelling at the compressed pace outruns native gold income for gear. An equipment drop is
rare (Seal of Star/Moon/Sun) RareRate times as often: the native single roll
admits the residues 1..RareRate of 1000 instead of 1 alone.

Drop cap: DropRate grows a kill's capacity with its passes (an ordinary
monster's 8 becomes 160 at rate 20), which floods the ground. DropCap bounds
the ordinary items one kill leaves: the kill plans as before, then a uniform
random subset of DropCap survives, so no item family is favoured. Gold heaps
(native count, value-scaled) and a unique's own prepass (one native round)
are never cut. 0 leaves every planned item.

===========================================================================
*/
package progression

import (
	"os"
	"strconv"
	"strings"

	"opensro.online/server/internal/game/enterworld"
)

// EnvBetaGrowth turns the closed-beta growth on ("on", "1", "true").
const EnvBetaGrowth = "SRO_BETA_GROWTH"

// EnvBetaSkillExpRate overrides the beta skill-EXP multiplier.
const EnvBetaSkillExpRate = "SRO_BETA_SKILL_EXP_RATE"

// EnvBetaDropRate overrides the beta drop-pass multiplier.
const EnvBetaDropRate = "SRO_BETA_DROP_RATE"

// EnvBetaGoldRate overrides the beta gold-heap multiplier.
const EnvBetaGoldRate = "SRO_BETA_GOLD_RATE"

// EnvBetaRareRate overrides the beta rare-equipment (SoX) multiplier.
const EnvBetaRareRate = "SRO_BETA_RARE_RATE"

// EnvBetaDropCap overrides the beta per-kill item cap (0 turns it off).
const EnvBetaDropCap = "SRO_BETA_DROP_CAP"

// BetaReferenceLevel is the level whose kill pace every level is held to.
const BetaReferenceLevel = 1

// betaSkillExpRateDefault makes skill training generous for testers.
const betaSkillExpRateDefault = 100

// betaDropRateDefault makes every drop of a kill twenty times over: a test
// server, so plus upgrades and alchemy can be tried without long farming.
const betaDropRateDefault = 20

// maxBetaDropRate bounds an operator override; capacity bounds the drops.
const maxBetaDropRate = 100

// betaDropCapDefault keeps a kill to sixteen ordinary items, twice an
// ordinary monster's native capacity, so the rate shows as better finds
// rather than a carpet of potions.
const betaDropCapDefault = 16

// maxBetaDropCap bounds an operator override.
const maxBetaDropCap = 1000

// betaGoldRateDefault makes each gold heap fifty times its native amount.
const betaGoldRateDefault = 50

// maxBetaGoldRate bounds an operator override; heaps also clamp to a dword.
const maxBetaGoldRate = 10_000

// betaRareRateDefault makes a rare (SoX) equipment drop five times as likely.
const betaRareRateDefault = 5

// maxBetaRareRate bounds an operator override to the native roll's domain.
const maxBetaRareRate = 1000

/*
================
GrowthRates

The multipliers applied to positive gains. The zero value is native.
================
*/
type GrowthRates struct {
	Enabled      bool
	SkillExpRate int64
	DropRate     int
	DropCap      int
	GoldRate     int
	RareRate     int
}

/*
================
BetaGrowthFromEnv
================
*/
func BetaGrowthFromEnv() GrowthRates {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvBetaGrowth))) {
	case "on", "1", "true":
	default:
		return GrowthRates{}
	}
	rates := GrowthRates{
		Enabled:      true,
		SkillExpRate: betaSkillExpRateDefault,
		DropRate:     betaDropRateDefault,
		DropCap:      betaDropCapDefault,
		GoldRate:     betaGoldRateDefault,
		RareRate:     betaRareRateDefault,
	}
	if text := strings.TrimSpace(os.Getenv(EnvBetaSkillExpRate)); text != "" {
		if n, err := strconv.ParseInt(text, 10, 64); err == nil && n >= 1 {
			rates.SkillExpRate = n
		}
	}
	if text := strings.TrimSpace(os.Getenv(EnvBetaDropRate)); text != "" {
		if n, err := strconv.Atoi(text); err == nil && n >= 1 && n <= maxBetaDropRate {
			rates.DropRate = n
		}
	}
	if text := strings.TrimSpace(os.Getenv(EnvBetaDropCap)); text != "" {
		if n, err := strconv.Atoi(text); err == nil && n >= 0 && n <= maxBetaDropCap {
			rates.DropCap = n
		}
	}
	if text := strings.TrimSpace(os.Getenv(EnvBetaGoldRate)); text != "" {
		if n, err := strconv.Atoi(text); err == nil && n >= 1 && n <= maxBetaGoldRate {
			rates.GoldRate = n
		}
	}
	if text := strings.TrimSpace(os.Getenv(EnvBetaRareRate)); text != "" {
		if n, err := strconv.Atoi(text); err == nil && n >= 1 && n <= maxBetaRareRate {
			rates.RareRate = n
		}
	}
	return rates
}

/*
================
levelKills

The native at-level kill count of one level, or false without table rows.
================
*/
func levelKills(levels enterworld.LevelDataSource, level int64) (float64, bool) {
	need, ok := levels.ExpRequired(level)
	if !ok || need <= 0 {
		return 0, false
	}
	basis, ok := levels.MonsterExpBasis(level)
	if !ok || basis <= 0 {
		return 0, false
	}
	return float64(need) / float64(basis), true
}

/*
================
levelPace

How many native gains one gain at this level is worth: the level's kill
count over the reference level's, never below 1. A level without both
table rows keeps the native pace.
================
*/
func levelPace(levels enterworld.LevelDataSource, level int64) float64 {
	if levels == nil {
		return 1
	}
	kills, ok := levelKills(levels, level)
	reference, refOK := levelKills(levels, BetaReferenceLevel)
	if !ok || !refOK || kills <= reference {
		return 1
	}
	return kills / reference
}

/*
================
scale

Positive gains only: the death penalty and every refusal stay native.
clampExpDelta and clampSkillExpDelta bound the results to the wire's
signed dword.
================
*/
func (g GrowthRates) scale(levels enterworld.LevelDataSource, level, expDelta, skillExpDelta int64) (int64, int64) {
	if !g.Enabled {
		return expDelta, skillExpDelta
	}
	pace := levelPace(levels, level)
	if expDelta > 0 {
		expDelta = int64(min(float64(expDelta)*pace, float64(1<<62)))
	}
	if skillExpDelta > 0 {
		rate := float64(max(g.SkillExpRate, 1))
		skillExpDelta = int64(min(float64(skillExpDelta)*pace*rate, float64(1<<62)))
	}
	return expDelta, skillExpDelta
}
