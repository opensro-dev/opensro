/*
===========================================================================

jobhonor_test.go - job honor scoring from trade sales (#3530)

A sale scores its profit, not its value; a run scores at most the run cap
until goods are loaded again; the account's daily cap is shared across its
characters (the real ledger); off, nothing scores.

===========================================================================
*/
package action

import (
	"testing"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/platform/jobhonor"
)

/*
================
honorRuntime

A runtime whose job honor sink is a real ledger in a temporary folder.
================
*/
func honorRuntime(t *testing.T, at time.Time) *Runtime {
	t.Helper()
	ledger, err := jobhonor.Open(t.TempDir(), func() time.Time { return at })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ledger.Close() })
	return &Runtime{JobHonor: ledger}
}

func honorCharacter(id int64, account string) *enterworld.Character {
	return &enterworld.Character{ID: id, AccountID: account}
}

func TestASaleScoresItsProfitNotItsValue(t *testing.T) {
	if got := tradeHonorPoints(domain.JobTrader, 0); got != 0 {
		t.Fatalf("a sale with no profit scored %d", got)
	}
	if got := tradeHonorPoints(domain.JobTrader, 3*honorTraderGoldPerPoint+1); got != 3 {
		t.Fatalf("trader profit scored %d, want 3", got)
	}
	if got := tradeHonorPoints(domain.JobThief, 3*honorTraderGoldPerPoint); got <= 3 {
		t.Fatalf("a thief's sale of the same profit scored %d, want more than a trader's", got)
	}
	if got := tradeHonorPoints(domain.JobHunter, 1_000_000); got != 0 {
		t.Fatalf("a hunter's sale scored %d", got)
	}
}

func TestARunScoresAtMostTheRunCapUntilGoodsAreLoadedAgain(t *testing.T) {
	at := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	rt := honorRuntime(t, at)
	c := honorCharacter(1, "acct")
	big := int64(honorRunPointCap) * honorTraderGoldPerPoint
	if got := rt.scoreTradeSale(c, domain.JobTrader, big/2, at); got != honorRunPointCap/2 {
		t.Fatalf("first sale of the run kept %d", got)
	}
	if got := rt.scoreTradeSale(c, domain.JobTrader, big, at); got != honorRunPointCap-honorRunPointCap/2 {
		t.Fatalf("second sale kept %d, want what the run cap leaves", got)
	}
	if got := rt.scoreTradeSale(c, domain.JobTrader, big, at); got != 0 {
		t.Fatalf("a spent run kept %d", got)
	}
	rt.openHonorRun(c)
	if got := rt.scoreTradeSale(c, domain.JobTrader, big, at); got != honorRunPointCap {
		t.Fatalf("a new run kept %d", got)
	}
}

func TestTheAccountCapCrossesItsCharacters(t *testing.T) {
	at := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	rt := honorRuntime(t, at)
	big := int64(honorRunPointCap) * honorTraderGoldPerPoint
	total := 0
	// Five characters of one account, one full run each: the account's
	// day stops at the cap however many alts it has.
	for id := int64(1); id <= 5; id++ {
		total += rt.scoreTradeSale(honorCharacter(id, "alts"), domain.JobTrader, big, at)
	}
	if total != jobhonor.DailyCap {
		t.Fatalf("five characters of one account kept %d, want the daily cap %d", total, jobhonor.DailyCap)
	}
	if got := rt.scoreTradeSale(honorCharacter(9, "other"), domain.JobTrader, big, at); got != honorRunPointCap {
		t.Fatalf("another account kept %d", got)
	}
}

func TestNothingScoresWithTheFlagOff(t *testing.T) {
	rt := &Runtime{}
	c := honorCharacter(1, "acct")
	rt.openHonorRun(c)
	if got := rt.scoreTradeSale(c, domain.JobTrader, 1_000_000_000, time.Now()); got != 0 {
		t.Fatalf("a runtime with no sink scored %d", got)
	}
}
