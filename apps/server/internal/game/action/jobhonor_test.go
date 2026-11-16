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

func TestAThiefsStealsDecayPerVictimEachHonorDay(t *testing.T) {
	at := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	rt := honorRuntime(t, at)
	thief := honorCharacter(1, "thief-account")
	full := int64(honorRunPointCap) * honorThiefGoldPerPoint
	steal := func(victim string, when time.Time) int {
		rt.openStealRun(thief, victim, thief.AccountID, when)
		return rt.scoreTradeSale(thief, domain.JobThief, full, when)
	}
	// An alt trader on another account feeding the same player's thief:
	// full, half, then nothing from that victim for the rest of the day.
	for i, want := range []int{honorRunPointCap, honorRunPointCap / 2, 0, 0} {
		if got := steal("AltTrader", at); got != want {
			t.Fatalf("steal %d of one victim kept %d, want %d", i+1, got, want)
		}
	}
	if got := steal("OtherTrader", at); got != honorRunPointCap {
		t.Fatalf("another victim kept %d", got)
	}
	// Another of the same account's thieves shares the decay.
	second := honorCharacter(2, "thief-account")
	rt.openStealRun(second, "AltTrader", second.AccountID, at)
	if got := rt.scoreTradeSale(second, domain.JobThief, full, at); got != 0 {
		t.Fatalf("the account's second thief kept %d from a spent victim", got)
	}
	if got := steal("AltTrader", at.Add(24*time.Hour)); got != honorRunPointCap {
		t.Fatalf("the next honor day kept %d", got)
	}
}

func TestOneLoadPickedUpInStacksIsOneSteal(t *testing.T) {
	at := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	rt := honorRuntime(t, at)
	thief := honorCharacter(1, "thief-account")
	full := int64(honorRunPointCap) * honorThiefGoldPerPoint
	for range 3 {
		rt.openStealRun(thief, "Victim", thief.AccountID, at)
	}
	if got := rt.scoreTradeSale(thief, domain.JobThief, full, at); got != honorRunPointCap {
		t.Fatalf("one load in three stacks kept %d", got)
	}
	rt.openStealRun(thief, "Victim", thief.AccountID, at)
	if got := rt.scoreTradeSale(thief, domain.JobThief, full, at); got != honorRunPointCap/2 {
		t.Fatalf("the second load kept %d, want half", got)
	}
}

func TestAThiefsSaleWithNoStealScoresNothing(t *testing.T) {
	at := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	rt := honorRuntime(t, at)
	thief := honorCharacter(1, "thief-account")
	if got := rt.scoreTradeSale(thief, domain.JobThief, 1_000_000_000, at); got != 0 {
		t.Fatalf("a sale with no haul kept %d", got)
	}
}

func TestStealsDecayByTheVictimsAccountNotItsAlias(t *testing.T) {
	at := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	trader := func(id int64, account, alias string) *enterworld.Character {
		c := honorCharacter(id, account)
		c.Job.Alias = alias
		return c
	}
	rt := cosOwnerIndexRuntime(map[string][]*enterworld.Character{testDivision: {
		trader(11, "alt-account", "AltOne"), trader(12, "alt-account", "AltTwo"), trader(13, "real-account", "Honest"),
	}})
	ledger, err := jobhonor.Open(t.TempDir(), func() time.Time { return at })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ledger.Close() })
	rt.JobHonor = ledger
	thief := honorCharacter(1, "thief-account")
	full := int64(honorRunPointCap) * honorThiefGoldPerPoint
	steal := func(alias string) int {
		rt.openStealRun(thief, rt.goodsVictim(testDivision, alias), thief.AccountID, at)
		return rt.scoreTradeSale(thief, domain.JobThief, full, at)
	}
	// Two trader characters of one alt account are one victim.
	for i, step := range []struct {
		alias string
		want  int
	}{{"AltOne", honorRunPointCap}, {"AltTwo", honorRunPointCap / 2}, {"altone", 0}, {"Honest", honorRunPointCap}} {
		if got := steal(step.alias); got != step.want {
			t.Fatalf("steal %d (%s) kept %d, want %d", i+1, step.alias, got, step.want)
		}
	}
	// The thief's own account's trader is no victim at all.
	own := honorCharacter(2, "alt-account")
	rt.openStealRun(own, rt.goodsVictim(testDivision, "AltOne"), own.AccountID, at.Add(48*time.Hour))
	if got := rt.scoreTradeSale(own, domain.JobThief, full, at.Add(48*time.Hour)); got != 0 {
		t.Fatalf("a steal from the thief's own account kept %d", got)
	}
	// Goods whose owner alias no character carries still decay by alias.
	if steal("Gone") != honorRunPointCap || steal("Gone") != honorRunPointCap/2 {
		t.Fatal("an unknown owner alias did not decay")
	}
}

/*
================
TestAStealScoresOnlyOnTheThiefsOwnKill

The owner's real cost (2026-11-16): goods score only when the thief's
account killed for them; goods dropped any other way score nothing,
spend no decay, and spoil an unsold haul they join.
================
*/
func TestAStealScoresOnlyOnTheThiefsOwnKill(t *testing.T) {
	at := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	rt := honorRuntime(t, at)
	thief := honorCharacter(1, "thief-account")
	full := int64(honorRunPointCap) * honorThiefGoldPerPoint
	for _, lostTo := range []string{"", "other-thief-account"} {
		rt.openStealRun(thief, "Victim", lostTo, at)
		if got := rt.scoreTradeSale(thief, domain.JobThief, full, at); got != 0 {
			t.Fatalf("goods lost to %q kept %d", lostTo, got)
		}
	}
	// Those pickups spent none of the victim's decay.
	rt.openStealRun(thief, "Victim", thief.AccountID, at)
	if got := rt.scoreTradeSale(thief, domain.JobThief, full, at); got != honorRunPointCap {
		t.Fatalf("the first real loss kept %d, want the whole run", got)
	}
	// Free goods of the same victim into an unsold real haul spoil it.
	rt.openStealRun(thief, "Third", thief.AccountID, at)
	rt.openStealRun(thief, "Third", "", at)
	if got := rt.scoreTradeSale(thief, domain.JobThief, full, at); got != 0 {
		t.Fatalf("a haul mixed with free goods kept %d", got)
	}
}
