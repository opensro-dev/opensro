/*
===========================================================================

vitals.go - character gauges derived from persisted records

Package charactervitals owns the version-pinned rules that derive character
gauges from persisted domain records.

===========================================================================
*/
package charactervitals

import (
	"math"

	"opensro.online/server/internal/domain"
)

// StatWordMax is the storage/display ceiling of the native STR/INT words.
const StatWordMax int64 = 0xffff

// This file makes the character's absolute MaxHP/MaxMP DERIVED values:
//
//	MaxHP = trunc( 1.02^(level-1) * STR * 10 )
//	MaxMP = trunc( 1.02^(level-1) * INT * 10 )
//
// ADOPTION EVIDENCE:
// two DISJOINT derivations agree on this closed form - the community
// derived it empirically from live retail of exactly our era (S13/S14:
// nivlam-era emulator scene, 2008/09), and LANE-3 read the same structure
// out of the cross-version GameServer binary (ParamKeeper seeded with
// level/STR/INT and a 1.02^(level-1) scale, lane3 notes section F) without
// ever seeing the closed form. Period observations reproduce it to the
// digit, and the vendor _AddNewChar SQL creates characters at STR/INT
// 20/20 with HP/MP 200/200 - the formula at level 1. Confidence for
// v1.150: HIGH.
//
// It is trunc, NOT round - the retail worked examples depend on it
// (1.02^42 * 173 * 10 = 3974.23... -> 3974). Retail computed this in
// server-side double arithmetic (the formula never shipped in a table and
// the v1.150 client provably never computes a maximum), so float64 pow +
// Trunc here is the faithful reproduction, not an approximation.
//
// AUTHORITY: the derivation is the ONLY source of the maxima this server
// emits - the 0x343C block, the vitals/char-data current-HP clamps, the
// enter-world snapshot and the agent API all call these helpers. The
// persisted maxHp/maxMp record fields are NON-authoritative history: they
// are deliberately neither consulted nor rewritten (rewriting stored data
// on a rule change would destroy the import record; deriving at read
// leaves exactly one source of truth, which was lane3's objection to any
// half-measure).
//
// ONE OWNER. The stored current is a plain number. The living maximum is
// stat-graph param 3 (HP) and param 4 (MP), which include item options and
// abnormal factors. This package cannot see that graph.
//
// CurrentHP/CurrentMP no longer clamp a stored value down to the closed
// form. A negative stored value becomes 0. Death remains a stored 0.
// An absent current still means "full", but only the gameplay helper
// (action.Runtime.playerKeeperVitals) knows full-of-which-maximum. Readers
// that cannot reach the stat graph — the character store's life check,
// the mentor notice, and the party register — treat an absent current as
// full at this closed form, and a stored number as that number. They must
// not be used to apply damage, cost, or a HUD gauge. When a maximum drops,
// the gameplay owner writes the clamped current back, so those readers
// then show the new value.
//
// LEVEL-UP STAT GROWTH - IMPLEMENTED (levelup wave, LANE-1): the adopted
// package's other half is "+1 STR and +1 INT automatically per level,
// plus +3 free stat points" (5 per level: 2 auto, 3 manual). It is proven
// arithmetically by the retail observations (a fresh character reaching
// level 2 with no points spent shows 214 HP = trunc(1.02 * 21 * 10) -
// only STR 21 produces that, so the auto +1 is real). The level-up path
// that was deliberately absent through the adopt wave now exists:
// progression.Runtime.GrantExperience (internal/game/progression/levelup.go) walks the
// leveldata exp curve (column 1, the same CGlobalData_GetLevelDataPayload row the client's
// own 0x30D2 walk reads) and, per level gained, performs exactly the
// 3-step contract this comment used to hold open:
//
//  1. raise Strength AND Intellect by 1 (through clampStatWord);
//  2. grant +3 StatPoints (progression.StatPointsPerLevel);
//  3. re-emit BuildLoginStatBlock (0x343C) - the maxima then move
//     automatically because they are derived, and current HP/MP stays
//     put per the LEAVE policy above.
//
// The authority core is trigger-agnostic. Live monster kills and quests feed
// it through progression.ExperienceUpdater; monster deaths may walk level down
// through the signed-delta half without revoking all-time earned stats.
const (
	// vitalCurveBase is the per-level compounding factor of the retail
	// growth curve.
	vitalCurveBase = 1.02
	// vitalStatMultiplier scales the stat into gauge points.
	vitalStatMultiplier = 10
)

/*
==================
derivationLevel

derivationLevel reads the persisted level with the same [1, 140] clamp
the 0x32B3 char-data writer uses (absent reads as 1; leveldata.txt
carries 140 rows and the wire level is one byte).
==================
*/
func derivationLevel(c *domain.Character) int64 {
	return coerceInt(charLevel(c), 1, 140, 1)
}

/*
==================
DerivedVitalMax

derivedVitalMax is the shared curve: trunc(1.02^(level-1) * stat * 10),
floored at 1 (a zero maximum empties the HUD gauge - the same floor the
old emission fallback held) and capped at the wire's s32 ceiling.
==================
*/
func DerivedVitalMax(level, stat int64) int64 {
	value := math.Trunc(math.Pow(vitalCurveBase, float64(level-1)) * float64(stat) * vitalStatMultiplier)
	if !(value >= 1) { // NaN-safe: below 1 or not-a-number floors to 1
		return 1
	}
	if value > 0x7fffffff {
		return 0x7fffffff
	}
	return int64(value)
}

/*
==================
DerivedMaxHP

DerivedMaxHP is the character's authoritative maximum HP. The stat
input is the SAME word-clamped STR the 0x343C block shows the client,
so the gauge and the displayed stat can never disagree about which
value produced the maximum.
==================
*/
func DerivedMaxHP(c *domain.Character) int64 {
	return DerivedVitalMax(derivationLevel(c), clampStatWord(domain.CharacterStrength(c)))
}

// DerivedMaxMP is the character's authoritative maximum MP (INT curve).
func DerivedMaxMP(c *domain.Character) int64 {
	return DerivedVitalMax(derivationLevel(c), clampStatWord(domain.CharacterIntellect(c)))
}

/*
==================
CurrentHP

CurrentHP returns the stored HP. Negatives become 0. A value above the
closed form is kept. Nil means full at DerivedMaxHP for readers that
cannot see the stat graph; gameplay resolves nil against param 3.
==================
*/
func CurrentHP(c *domain.Character) int64 {
	if c == nil {
		return 0
	}
	return currentVital(c.CurrentHP, DerivedMaxHP(c))
}

// CurrentMP is the MP twin of CurrentHP. Nil means full at DerivedMaxMP
// here; gameplay resolves nil against param 4.
func CurrentMP(c *domain.Character) int64 {
	if c == nil {
		return 0
	}
	return currentVital(c.CurrentMP, DerivedMaxMP(c))
}

// Alive answers the one authoritative life predicate. Nil is not a character;
// a nil CurrentHP field on a real character is full, not dead or unavailable.
func Alive(c *domain.Character) bool {
	return c != nil && CurrentHP(c) > 0
}

func currentVital(current *int64, maximum int64) int64 {
	if maximum < 0 {
		maximum = 0
	}
	if current == nil {
		return maximum
	}
	if *current < 0 {
		return 0
	}
	return *current
}

func charLevel(c *domain.Character) *int64 {
	if c == nil {
		return nil
	}
	return c.Level
}

func coerceInt(value *int64, min, max, fallback int64) int64 {
	if value == nil {
		return fallback
	}
	if *value < min {
		return min
	}
	if *value > max {
		return max
	}
	return *value
}

func clampStatWord(value int64) int64 {
	if value < 0 {
		return 0
	}
	if value > StatWordMax {
		return StatWordMax
	}
	return value
}
