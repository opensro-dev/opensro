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
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
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

func honorDressed(id int64, account string, job uint8) *enterworld.Character {
	c := honorCharacter(id, account)
	c.MissionInventory = []domain.InventoryRow{{Slot: enterworld.JobSuitSlot,
		TypeFlags: wire.PackTypeFlags(3, 1, 7, job), StackCount: 1}}
	return c
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
	if got := rt.scoreTradeSale(c, domain.JobTrader, big/2, false, at); got != honorRunPointCap/2 {
		t.Fatalf("first sale of the run kept %d", got)
	}
	if got := rt.scoreTradeSale(c, domain.JobTrader, big, false, at); got != honorRunPointCap-honorRunPointCap/2 {
		t.Fatalf("second sale kept %d, want what the run cap leaves", got)
	}
	if got := rt.scoreTradeSale(c, domain.JobTrader, big, false, at); got != 0 {
		t.Fatalf("a spent run kept %d", got)
	}
	rt.openHonorRun(c)
	if got := rt.scoreTradeSale(c, domain.JobTrader, big, false, at); got != honorRunPointCap {
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
		total += rt.scoreTradeSale(honorCharacter(id, "alts"), domain.JobTrader, big, false, at)
	}
	if total != jobhonor.DailyCap {
		t.Fatalf("five characters of one account kept %d, want the daily cap %d", total, jobhonor.DailyCap)
	}
	if got := rt.scoreTradeSale(honorCharacter(9, "other"), domain.JobTrader, big, false, at); got != honorRunPointCap {
		t.Fatalf("another account kept %d", got)
	}
}

func TestNothingScoresWithTheFlagOff(t *testing.T) {
	rt := &Runtime{}
	c := honorCharacter(1, "acct")
	rt.openHonorRun(c)
	if got := rt.scoreTradeSale(c, domain.JobTrader, 1_000_000_000, false, time.Now()); got != 0 {
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
		return rt.scoreTradeSale(thief, domain.JobThief, full, false, when)
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
	if got := rt.scoreTradeSale(second, domain.JobThief, full, false, at); got != 0 {
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
	if got := rt.scoreTradeSale(thief, domain.JobThief, full, false, at); got != honorRunPointCap {
		t.Fatalf("one load in three stacks kept %d", got)
	}
	rt.openStealRun(thief, "Victim", thief.AccountID, at)
	if got := rt.scoreTradeSale(thief, domain.JobThief, full, false, at); got != honorRunPointCap/2 {
		t.Fatalf("the second load kept %d, want half", got)
	}
}

func TestAThiefsSaleWithNoStealScoresNothing(t *testing.T) {
	at := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	rt := honorRuntime(t, at)
	thief := honorCharacter(1, "thief-account")
	if got := rt.scoreTradeSale(thief, domain.JobThief, 1_000_000_000, false, at); got != 0 {
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
		return rt.scoreTradeSale(thief, domain.JobThief, full, false, at)
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
	if got := rt.scoreTradeSale(own, domain.JobThief, full, false, at.Add(48*time.Hour)); got != 0 {
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
		if got := rt.scoreTradeSale(thief, domain.JobThief, full, false, at); got != 0 {
			t.Fatalf("goods lost to %q kept %d", lostTo, got)
		}
	}
	// Those pickups spent none of the victim's decay.
	rt.openStealRun(thief, "Victim", thief.AccountID, at)
	if got := rt.scoreTradeSale(thief, domain.JobThief, full, false, at); got != honorRunPointCap {
		t.Fatalf("the first real loss kept %d, want the whole run", got)
	}
	// Free goods of the same victim into an unsold real haul spoil it.
	rt.openStealRun(thief, "Third", thief.AccountID, at)
	rt.openStealRun(thief, "Third", "", at)
	if got := rt.scoreTradeSale(thief, domain.JobThief, full, false, at); got != 0 {
		t.Fatalf("a haul mixed with free goods kept %d", got)
	}
}

/*
================
TestAHuntersEscortDamagePaysOnlyOnArrival

A hunter's damage on a thief who hit a caravan is held, not scored;
the caravan's arrival pays it, once.
================
*/
func TestAHuntersEscortDamagePaysOnlyOnArrival(t *testing.T) {
	at := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	rt := honorRuntime(t, at)
	ledger := rt.JobHonor.(*jobhonor.Service)
	trader := honorDressed(1, "trader-account", domain.JobTrader)
	thief := honorDressed(2, "thief-account", domain.JobThief)
	hunter := honorDressed(3, "hunter-account", domain.JobHunter)
	const now = int64(1_000_000)
	// Damage with no attacked caravan behind it scores nothing.
	rt.noteHonorEscortDamage(hunter, thief, 10_000, now)
	rt.payHonorEscorts(trader, false, at)
	if got := ledger.Used("hunter-account"); got != 0 {
		t.Fatalf("unmarked damage paid %d", got)
	}
	rt.noteHonorCaravanAttack(thief, trader, now)
	rt.noteHonorEscortDamage(hunter, thief, 3*honorHunterDamagePerPoint, now)
	rt.noteHonorEscortDamage(hunter, thief, 1, now)
	if got := ledger.Used("hunter-account"); got != 0 {
		t.Fatalf("escort damage scored %d before the caravan arrived", got)
	}
	rt.payHonorEscorts(trader, false, at)
	if got := ledger.Used("hunter-account"); got != 3 {
		t.Fatalf("arrival paid %d, want 3", got)
	}
	// The escrow is spent: a second arrival pays nothing more.
	rt.payHonorEscorts(trader, false, at)
	if got := ledger.Used("hunter-account"); got != 3 {
		t.Fatalf("a second arrival paid again: %d", got)
	}
}

/*
================
TestEscortDamageIsCappedAndExpires

One caravan pays a hunter at most a run's cap, and damage on a thief
whose rob window has closed scores nothing.
================
*/
func TestEscortDamageIsCappedAndExpires(t *testing.T) {
	at := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	rt := honorRuntime(t, at)
	ledger := rt.JobHonor.(*jobhonor.Service)
	trader := honorDressed(1, "trader-account", domain.JobTrader)
	thief := honorDressed(2, "thief-account", domain.JobThief)
	hunter := honorDressed(3, "hunter-account", domain.JobHunter)
	const now = int64(1_000_000)
	rt.noteHonorCaravanAttack(thief, trader, now)
	rt.noteHonorEscortDamage(hunter, thief, int64(honorRunPointCap+50)*honorHunterDamagePerPoint, now)
	rt.payHonorEscorts(trader, false, at)
	if got := ledger.Used("hunter-account"); got != honorRunPointCap {
		t.Fatalf("a fought-off robbery paid %d, want the run cap", got)
	}
	// A second attack, answered after the window closed.
	rt.noteHonorCaravanAttack(thief, trader, now)
	rt.noteHonorEscortDamage(hunter, thief, 10*honorHunterDamagePerPoint, now+caravanRobWindowMs+1)
	rt.payHonorEscorts(trader, false, at)
	if got := ledger.Used("hunter-account"); got != honorRunPointCap {
		t.Fatalf("late damage paid: %d", got)
	}
}

/*
================
TestAJobKillDecaysPerVictimAccount

A job kill with something at stake scores full, then half, then
nothing against one victim account in a day; without a stake, or
inside one account, it scores nothing at all.
================
*/
func TestAJobKillDecaysPerVictimAccount(t *testing.T) {
	at := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	rt := honorRuntime(t, at)
	killer := honorDressed(1, "killer-account", domain.JobThief)
	victim := honorDressed(2, "victim-account", domain.JobTrader)
	alt := honorDressed(3, "victim-account", domain.JobTrader)
	for i, want := range []int{honorKillPoints, honorKillPoints / 2, 0} {
		if got := rt.scoreHonorKill(killer, victim, true, at); got != want {
			t.Fatalf("kill %d scored %d, want %d", i+1, got, want)
		}
	}
	// The victim's alt shares the decay: the account is the victim.
	if got := rt.scoreHonorKill(killer, alt, true, at); got != 0 {
		t.Fatalf("a kill of the victim's alt scored %d", got)
	}
	if got := rt.scoreHonorKill(killer, victim, false, at); got != 0 {
		t.Fatalf("a kill with nothing at stake scored %d", got)
	}
	sameAccount := honorDressed(9, "killer-account", domain.JobTrader)
	if got := rt.scoreHonorKill(killer, sameAccount, true, at); got != 0 {
		t.Fatalf("an intra-account kill scored %d", got)
	}
	naked := honorCharacter(4, "naked-account")
	if got := rt.scoreHonorKill(naked, victim, true, at); got != 0 {
		t.Fatalf("an undressed killer scored %d", got)
	}
}

/*
================
TestAJobKillStakesGoodsAndPoints

Carried trade goods stake a kill; a victim with honor kept today is
staked even empty-handed; a victim with neither is not.
================
*/
func TestAJobKillStakesGoodsAndPoints(t *testing.T) {
	at := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	rt := honorRuntime(t, at)
	loaded := honorDressed(1, "loaded-account", domain.JobTrader)
	loaded.MissionInventory = append(loaded.MissionInventory,
		domain.InventoryRow{Slot: 13, TypeFlags: wire.PackTypeFlags(3, 3, 8, 0), StackCount: 1})
	if !inventory.IsTradeGoods(loaded.MissionInventory[1].TypeFlags) {
		t.Fatal("test fixture is not trade goods")
	}
	if !rt.honorKillStake(loaded, at) {
		t.Fatal("carried trade goods did not stake the kill")
	}
	empty := honorDressed(2, "empty-account", domain.JobTrader)
	if rt.honorKillStake(empty, at) {
		t.Fatal("an empty victim with no points was staked")
	}
	rt.JobHonor.AwardJobHonor("empty-account", empty.ID, int(domain.JobTrader), 5, at)
	if !rt.honorKillStake(empty, at) {
		t.Fatal("points kept today did not stake the kill")
	}
}
