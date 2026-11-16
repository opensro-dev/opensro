/*
===========================================================================

tokens_test.go - tests for tokens.go

The table pays the owner's easy and hard families with their _L2/_L3
rows and nothing else; the cap restarts each token day and cuts an award
to what is left; the balance never passes its bound.

===========================================================================
*/
package nasrun

import (
	"testing"
	"time"
)

/*
================
TestUniqueTokensPaysTheTable
================
*/
func TestUniqueTokensPaysTheTable(t *testing.T) {
	t.Parallel()

	cases := map[string]int64{
		"MOB_CH_TIGERWOMAN":    TokensEasy,
		"MOB_CH_TIGERWOMAN_L3": TokensEasy,
		"MOB_AM_IVY":           TokensEasy,
		"MOB_TK_BONELORD_L2":   TokensHard,
		"MOB_RM_TAHOMET":       TokensHard,
		"MOB_TQ_WHITESNAKE":    TokensHard,
		"MOB_CH_TIGERWOMAN_X":  0,
		"MOB_CH_TIGERWOMAN_L4": 0,
		"MOB_CH_BANDIT":        0,
		"":                     0,
	}
	for codename, want := range cases {
		if got := UniqueTokens(codename); got != want {
			t.Errorf("UniqueTokens(%q) = %d, want %d", codename, got, want)
		}
	}
}

/*
================
TestGrantTokensCapsTheDay
================
*/
func TestGrantTokensCapsTheDay(t *testing.T) {
	t.Parallel()

	g := GrantTokens(5, 100, 9, TokensHard, 100)
	if g.Granted != 1 || g.Balance != 6 || g.DayCount != TokenDailyCap || !g.Reached {
		t.Fatalf("the last token of the day: %+v", g)
	}
	if g = GrantTokens(6, 100, TokenDailyCap, TokensEasy, 100); g.Granted != 0 || g.Balance != 6 || g.Reached {
		t.Fatalf("a capped day: %+v", g)
	}
	if g = GrantTokens(6, 100, TokenDailyCap, TokensHard, 101); g.Granted != TokensHard || g.Day != 101 || g.DayCount != TokensHard || g.Reached {
		t.Fatalf("a new day: %+v", g)
	}
	if g = GrantTokens(TokensMax-1, 100, 0, TokensHard, 100); g.Granted != 1 || g.Balance != TokensMax {
		t.Fatalf("the bound: %+v", g)
	}
}

/*
================
TestTokenDayStartsAtTheResetHour
================
*/
func TestTokenDayStartsAtTheResetHour(t *testing.T) {
	t.Parallel()

	before := time.Date(2026, 11, 16, 3, 59, 59, 0, time.UTC)
	after := time.Date(2026, 11, 16, 4, 0, 0, 0, time.UTC)
	if TokenDay(after) != TokenDay(before)+1 {
		t.Fatalf("day %d at 03:59:59, %d at 04:00", TokenDay(before), TokenDay(after))
	}
}

/*
================
TestWithinTokenBand
================
*/
func TestWithinTokenBand(t *testing.T) {
	t.Parallel()

	if !WithinTokenBand(30, 20) || WithinTokenBand(31, 20) || !WithinTokenBand(1, 90) {
		t.Fatal("the band is the unique's level plus TokenLevelBand, no floor")
	}
}
