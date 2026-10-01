/*
===========================================================================

growth.go - the closed-beta growth rates (port-only, not native)

Native progression grants exactly what the reward formulae compute. For the
closed beta the operator can turn on faster growth with one environment
variable; unset, every grant is native. Nothing persists the rate: switching
back is unsetting the variable and restarting the game world.

EXP: every level costs about as many at-level kills as level 3 does. The
level table gives both the EXP a level needs (ExpRequired) and the EXP an
at-level monster yields (MonsterExpBasis, GUST_Mob_Exp), so their ratio is
the native kill count for that level. A gain at level L is multiplied by
kills(L) / kills(3), never below 1, so early levels stay native and later
levels compress to the level-3 pace. Every source scales the same way:
kills, party shares and quest rewards all pass through applyExperience.

SP: skill EXP gains are multiplied by a flat rate so testers can train and
try skills freely.

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

// betaReferenceLevel is the level whose kill pace every level is held to.
const betaReferenceLevel = 3

// betaSkillExpRateDefault makes skill training generous for testers.
const betaSkillExpRateDefault = 100

/*
================
GrowthRates

The multipliers applied to positive gains. The zero value is native.
================
*/
type GrowthRates struct {
	Enabled      bool
	SkillExpRate int64
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
	rates := GrowthRates{Enabled: true, SkillExpRate: betaSkillExpRateDefault}
	if text := strings.TrimSpace(os.Getenv(EnvBetaSkillExpRate)); text != "" {
		if n, err := strconv.ParseInt(text, 10, 64); err == nil && n >= 1 {
			rates.SkillExpRate = n
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
scale

Positive gains only: the death penalty and every refusal stay native. A
level without both table rows keeps the native amount.
================
*/
func (g GrowthRates) scale(levels enterworld.LevelDataSource, level, expDelta, skillExpDelta int64) (int64, int64) {
	if !g.Enabled {
		return expDelta, skillExpDelta
	}
	if expDelta > 0 && levels != nil {
		kills, ok := levelKills(levels, level)
		reference, refOK := levelKills(levels, betaReferenceLevel)
		if ok && refOK && kills > reference {
			// clampExpDelta bounds the result to the wire's signed dword.
			expDelta = int64(min(float64(expDelta)*kills/reference, float64(1<<62)))
		}
	}
	if skillExpDelta > 0 && g.SkillExpRate > 1 {
		skillExpDelta = min(skillExpDelta, (1<<62)/g.SkillExpRate) * g.SkillExpRate
	}
	return expDelta, skillExpDelta
}
