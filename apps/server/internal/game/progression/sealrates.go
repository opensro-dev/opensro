/*
===========================================================================

sealrates.go - the closed beta's per-seal rare rates (port-only, not native)

Port-only, not native, beta balance (owner 2026-10-13). With
SRO_BETA_SEAL_RATES on, a rare (SoX) equipment drop of each degree and
seal is SealMultiplier times as likely as it is without the setting, on
top of whatever rare rate is already in force (the beta's RareRate). Off,
every rare drop rolls exactly as before.

A rare item's seal and degree come from its itemdata ItemClass (column 61),
as the client reads them: CSOItem_CalculateTierGroup is
(ItemClass - 1) % 3, which ItemTooltip_AppendRareGrade names
PARAM_RARE_FIRST/SECOND/THIRD, "Seal Of Star/Moon/Sun"; three classes make
one degree.

The roll keeps one owner (action/monsterloot.go): the rare window widens by
the table's largest multiplier, then a chosen rare stays rare with
probability multiplier / largest and otherwise drops as an ordinary item.
Each seal's chance is then exactly its multiplier times the old one.

===========================================================================
*/
package progression

import (
	"os"
	"strings"

	"opensro.online/server/internal/game/enterworld"
)

// EnvBetaSealRates turns the beta seal rates on ("on", "1", "true").
const EnvBetaSealRates = "SRO_BETA_SEAL_RATES"

// Seal tiers and the itemClass span of one degree: the shared owner's
// (enterworld/itemgrade.go, CSOItem_CalculateTierGroup's (ItemClass - 1) % 3).
const (
	SealStar             = enterworld.SealStar
	SealMoon             = enterworld.SealMoon
	SealSun              = enterworld.SealSun
	sealClassesPerDegree = enterworld.SealTiers
)

// maxSealDegree is the highest degree a v1.150 rare item has.
const maxSealDegree = 9

/*
================
SealRates

Multipliers by degree (1..9) and seal tier. The zero value is off: every
multiplier is 1.
================
*/
type SealRates struct {
	enabled bool
	table   [maxSealDegree][sealClassesPerDegree]int
}

/*
================
BetaSealRatesFromEnv

The owner's beta rates: degrees 1-8 triple every seal; degree 9 triples
Star, doubles Moon and leaves Sun as it is.
================
*/
func BetaSealRatesFromEnv() SealRates {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvBetaSealRates))) {
	case "on", "1", "true":
	default:
		return SealRates{}
	}
	rates := SealRates{enabled: true}
	for degree := 1; degree < maxSealDegree; degree++ {
		rates.table[degree-1] = [sealClassesPerDegree]int{SealStar: 3, SealMoon: 3, SealSun: 3}
	}
	rates.table[maxSealDegree-1] = [sealClassesPerDegree]int{SealStar: 3, SealMoon: 2, SealSun: 1}
	return rates
}

/*
================
SealRates.Enabled
================
*/
func (s SealRates) Enabled() bool { return s.enabled }

/*
================
SealRates.Multiplier

The multiplier of a rare item of this ItemClass; 1 when off or for a class
outside the nine degrees.
================
*/
func (s SealRates) Multiplier(itemClass int) int {
	if !s.enabled || itemClass < 1 || itemClass > maxSealDegree*sealClassesPerDegree {
		return 1
	}
	return s.table[(itemClass-1)/sealClassesPerDegree][(itemClass-1)%sealClassesPerDegree]
}

/*
================
SealRates.Table

The multipliers by degree (row 0 is degree 1) and seal (Star, Moon, Sun),
or nil when off. The website's catalogue publishes it.
================
*/
func (s SealRates) Table() [][]int {
	if !s.enabled {
		return nil
	}
	rows := make([][]int, 0, maxSealDegree)
	for _, degree := range s.table {
		rows = append(rows, append([]int(nil), degree[:]...))
	}
	return rows
}

/*
================
SealRates.Widening

The rare window's widening: the largest multiplier, so every seal's keep
chance (multiplier / widening) is at most 1. 1 when off.
================
*/
func (s SealRates) Widening() int {
	widest := 1
	if !s.enabled {
		return widest
	}
	for _, degree := range s.table {
		for _, m := range degree {
			widest = max(widest, m)
		}
	}
	return widest
}
