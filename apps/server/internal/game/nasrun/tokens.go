/*
===========================================================================

tokens.go - the Devil's Spirit tokens earned on a unique kill

Port-only, not native (#3456 step 3a). No source this build reads has a
Spirit economy: the later clients sell the Spirit for silk. The owner
picked an earned path instead (2026-11-16): unique tokens per eligible
contributor, a daily cap, bound to the character. The tokens are a point
balance on the character, the shape of the native honor points
(refpricepolicyofitem payment type 32): earned in the field, spent at a
shop row priced in them (step 3b).

This file holds only the rules, as plain functions of their inputs: which
uniques pay and how much, the daily cap and its day, and the level band.
The award itself runs in the kill settlement (action/nasrun_tokens.go).
Every number is the owner's picked default, named so a later change is
one value.

===========================================================================
*/
package nasrun

import (
	"strings"
	"time"
)

const (
	// TokensEasy and TokensHard are one eligible contributor's award for
	// an easy or a hard unique.
	TokensEasy = 1
	TokensHard = 2
	// TokenDailyCap is the most one character earns in one token day.
	TokenDailyCap = 10
	// TokensMax bounds a balance, far above any price (the Spirit costs
	// 60), so a stored value can never overflow a shop's arithmetic.
	TokensMax = 1_000_000
	// TokenLevelBand: a contributor more than this many levels above the
	// unique earns nothing. Inferred (the owner's rule is "within the
	// unique's level band"; no source states a band): ten levels, the
	// width of a v1.150 hunting band, so a capped character cannot farm
	// the starter uniques.
	TokenLevelBand = 10
	// tokenDayResetHour is the UTC hour a token day starts: the shard's
	// quiet hour, as the other daily caps (progression.HalloweenDayKey).
	tokenDayResetHour = 4
	secondsPerDay     = 24 * 60 * 60
)

// easyUniques and hardUniques are the codename families of the owner's
// table, matched with their _L2/_L3 rows. Captain Ivy is MOB_AM_IVY,
// Cerberus MOB_EU_KERBEROS, Lord Yarkan MOB_TK_BONELORD and Demon
// Shaitan MOB_RM_TAHOMET; "the Jangan cave uniques" are the Qin-Shi
// tomb's guardians and snakes.
var (
	easyUniques = []string{"MOB_CH_TIGERWOMAN", "MOB_OA_URUCHI", "MOB_EU_KERBEROS", "MOB_AM_IVY"}
	hardUniques = []string{
		"MOB_KK_ISYUTARU", "MOB_TK_BONELORD", "MOB_RM_TAHOMET",
		"MOB_TQ_NORTHGUARDIAN", "MOB_TQ_SOUTHGUARDIAN", "MOB_TQ_WESTGUARDIAN", "MOB_TQ_EASTGUARDIAN",
		"MOB_TQ_BLACKSNAKE", "MOB_TQ_WHITESNAKE",
	}
)

/*
================
UniqueTokens

The award one eligible contributor earns for killing the unique named
codename: TokensHard, TokensEasy, or 0 for a monster outside the table.
A family matches its base row and its _L2/_L3 rows only, so a longer
codename sharing a prefix (another monster) never matches.
================
*/
func UniqueTokens(codename string) int64 {
	if inFamily(codename, hardUniques) {
		return TokensHard
	}
	if inFamily(codename, easyUniques) {
		return TokensEasy
	}
	return 0
}

/*
================
inFamily
================
*/
func inFamily(codename string, families []string) bool {
	for _, base := range families {
		if codename == base {
			return true
		}
		if rest, ok := strings.CutPrefix(codename, base); ok && (rest == "_L2" || rest == "_L3") {
			return true
		}
	}
	return false
}

/*
================
TokenDay

The token day containing now: days since the epoch, shifted so a day
starts at tokenDayResetHour UTC.
================
*/
func TokenDay(now time.Time) int64 {
	return (now.Unix() - tokenDayResetHour*60*60) / secondsPerDay
}

/*
================
WithinTokenBand

Whether a contributor of level earns from a unique of uniqueLevel.
================
*/
func WithinTokenBand(level, uniqueLevel int64) bool {
	return level <= uniqueLevel+TokenLevelBand
}

/*
================
TokenGrant

What one award does to a character's ledger: the day's count restarts
on a new day, the award is cut to what the cap leaves, and the balance
stops at TokensMax. Granted is what was added; Reached is true when this
award took the day's count to the cap.
================
*/
type TokenGrant struct {
	Balance, Day, DayCount int64
	Granted                int64
	Reached                bool
}

/*
================
GrantTokens
================
*/
func GrantTokens(balance, day, dayCount, award, today int64) TokenGrant {
	if day != today {
		day, dayCount = today, 0
	}
	grant := TokenGrant{Balance: balance, Day: day, DayCount: dayCount}
	room := TokenDailyCap - dayCount
	award = min(award, room, TokensMax-balance)
	if award <= 0 {
		return grant
	}
	grant.Balance += award
	grant.DayCount += award
	grant.Granted = award
	grant.Reached = grant.DayCount >= TokenDailyCap
	return grant
}
