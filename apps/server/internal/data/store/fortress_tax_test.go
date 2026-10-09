/*
===========================================================================

fortress_tax_test.go - durable tax withdrawals through the real authority

Seeds the stored treasury before Restore. Refusals, successful withdrawals
and transaction failures must agree across authority, character memory and
SQL; concurrent requests must not mint or lose gold.

===========================================================================
*/
package store

import (
	"fmt"
	"math"
	"strings"
	"sync"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/world/fortress"
)

/*
================
fortressTaxFixture
================
*/
type fortressTaxFixture struct {
	s                     *Store
	a                     *fortress.Authority
	dir                   string
	master, member, rival *domain.Character
	guildID, rivalGuildID int64
}

/*
================
newFortressTaxFixture

The rival really belongs to another guild, so code 6 is not merely a test
of a missing character or missing guild membership.
================
*/
func newFortressTaxFixture(t *testing.T, treasury, gold int64) *fortressTaxFixture {
	t.Helper()
	f := &fortressTaxFixture{dir: t.TempDir()}
	f.s = openTest(t, f.dir, newTestClock())
	f.master = guildTestCharacter("taxmaster")
	f.member = guildTestCharacter("taxmember")
	f.rival = guildTestCharacter("taxrival")
	for _, c := range []*domain.Character{f.master, f.member, f.rival} {
		if err := f.s.CreateCharacter(testDivision, "test-account", c); err != nil {
			t.Fatal(err)
		}
	}
	f.guildID, _, _ = seedTestGuild(t, f.s, f.master, f.member)
	var err error
	f.rivalGuildID, err = f.s.Guilds().CreateGuild(testDivision,
		domain.GuildRecord{Name: "TaxRivals", Level: 2},
		domain.GuildMemberRecord{CharID: f.rival.ID, Name: f.rival.Name, Grade: 0}, f.rival)
	if err != nil {
		t.Fatal(err)
	}
	if !f.s.UpdateCharacters([]*domain.Character{f.master, f.member, f.rival}, "tax-fixture", func() bool {
		f.master.Gold = int64Ptr(gold)
		f.member.Gold = int64Ptr(0)
		f.rival.Gold = int64Ptr(0)
		return true
	}) {
		t.Fatal("fixture gold refused")
	}
	if err := f.s.Fortresses().SaveFortress(testDivision, domain.FortressRecord{
		FortressID: 1, GuildID: f.guildID, TaxGold: treasury, TaxRate: 7, StaffFlags: 3,
	}); err != nil {
		t.Fatal(err)
	}
	f.restore(t)
	return f
}

/*
================
restore

Period flags are runtime state, so explicitly reopen the collection period
after every restart rather than relying on a constructor default.
================
*/
func (f *fortressTaxFixture) restore(t *testing.T) {
	t.Helper()
	f.a = fortress.New([]fortress.Catalog{{ID: 1}})
	if err := f.a.Restore(testDivision, f.s.Fortresses()); err != nil {
		t.Fatal(err)
	}
	f.a.SetPeriod(testDivision, fortress.PeriodTax, true)
}

/*
================
reopen
================
*/
func (f *fortressTaxFixture) reopen(t *testing.T) {
	t.Helper()
	f.s.Close()
	f.s = openTest(t, f.dir, newTestClock())
	f.restore(t)
}

/*
================
balances

Look up the current store-owned character after reopen; the original
fixture pointer then belongs to the closed store and is not a witness.
================
*/
func (f *fortressTaxFixture) balances(t *testing.T, actorID, gold, treasury int64) {
	t.Helper()
	f.s.mu.RLock()
	actor := f.s.characterByIDLocked(testDivision, actorID)
	var memoryGold int64
	valid := actor != nil && actor.Gold != nil
	if valid {
		memoryGold = *actor.Gold
	}
	f.s.mu.RUnlock()
	if !valid || memoryGold != gold {
		t.Fatalf("actor %d memory gold=%d present=%v, want %d", actorID, memoryGold, valid, gold)
	}
	row, ok := f.a.Get(testDivision, 1)
	if !ok || row.TaxGold != treasury || row.TaxRate != 7 || row.StaffFlags != 3 {
		t.Fatalf("authority treasury=%+v present=%v, want gold=%d rate=7 staff=3", row, ok, treasury)
	}
	var diskGold, diskTreasury int64
	if err := f.s.db.QueryRow("SELECT json_extract(record, '$.gold') FROM characters WHERE division = ? AND id = ?",
		testDivision, actorID).Scan(&diskGold); err != nil {
		t.Fatal(err)
	}
	if err := f.s.db.QueryRow("SELECT COALESCE(json_extract(record, '$.taxGold'), 0) FROM fortresses WHERE division = ? AND fortress_id = 1",
		testDivision).Scan(&diskTreasury); err != nil {
		t.Fatal(err)
	}
	if diskGold != gold || diskTreasury != treasury {
		t.Fatalf("SQL gold=%d treasury=%d, want %d/%d", diskGold, diskTreasury, gold, treasury)
	}
}

/*
================
TestFortressTaxCollectionRefusalOrder
================
*/
func TestFortressTaxCollectionRefusalOrder(t *testing.T) {
	f := newFortressTaxFixture(t, 100, 10)
	for _, tc := range []struct {
		name      string
		period    bool
		fortress  uint32
		actor     int64
		requested int64
		code      uint8
	}{
		{"period before unknown fortress", false, 999, f.rival.ID, 1, 8},
		{"period before invalid amount", false, 1, f.master.ID, math.MinInt64, 8},
		{"fortress before foreign guild", true, 999, f.rival.ID, 1, 3},
		{"foreign guild before amount", true, 1, f.rival.ID, -1, 6},
		{"nonmaster before amount", true, 1, f.member.ID, -1, 7},
		{"nonmaster positive", true, 1, f.member.ID, 1, 7},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f.a.SetPeriod(testDivision, fortress.PeriodTax, tc.period)
			collected, code, err := f.a.CollectTax(testDivision, tc.fortress, tc.actor, tc.requested)
			if err != nil || code != tc.code || collected != 0 {
				t.Fatalf("collection=%d code=%d err=%v, want 0/%d/nil", collected, code, err, tc.code)
			}
			f.balances(t, f.master.ID, 10, 100)
			f.balances(t, f.member.ID, 0, 100)
			f.balances(t, f.rival.ID, 0, 100)
		})
	}
}

/*
================
TestFortressTaxCollectionSignedAmounts
================
*/
func TestFortressTaxCollectionSignedAmounts(t *testing.T) {
	for _, tc := range []struct {
		name                            string
		treasury, gold, requested, take int64
		code                            uint8
	}{
		{"partial", 100, 10, 25, 25, 0},
		{"zero", 100, 10, 0, 0, 0},
		{"negative", 100, 10, -1, 0, 0},
		{"minimum signed", 100, 10, math.MinInt64, 0, 0},
		{"maximum signed", math.MaxInt64, 0, math.MaxInt64, math.MaxInt64, 0},
		{"over treasury refuses", 100, 10, math.MaxInt64, 0, 2},
		{"empty treasury", 0, 10, 1, 0, 2},
		{"exact gold limit", 100, math.MaxInt64 - 25, 25, 25, 0},
		{"gold overflow", 100, math.MaxInt64 - 24, 25, 0, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFortressTaxFixture(t, tc.treasury, tc.gold)
			collected, code, err := f.a.CollectTax(testDivision, 1, f.master.ID, tc.requested)
			if err != nil || code != tc.code || collected != tc.take {
				t.Fatalf("collection=%d code=%d err=%v, want %d/%d/nil", collected, code, err, tc.take, tc.code)
			}
			f.balances(t, f.master.ID, tc.gold+tc.take, tc.treasury-tc.take)
			f.reopen(t)
			f.balances(t, f.master.ID, tc.gold+tc.take, tc.treasury-tc.take)
		})
	}
}

/*
================
TestFortressTaxCollectionRollbackAfterCharacterWrite

The trigger checks the transaction's credited character before aborting
the treasury write. An earlier failure cannot satisfy this witness.
================
*/
func TestFortressTaxCollectionRollbackAfterCharacterWrite(t *testing.T) {
	f := newFortressTaxFixture(t, 100, 10)
	const witness = "tax treasury failed after character credit"
	for _, operation := range []string{"INSERT", "UPDATE"} {
		statement := fmt.Sprintf(`CREATE TRIGGER reject_tax_%s BEFORE %s ON fortresses BEGIN
SELECT CASE WHEN (SELECT json_extract(record, '$.gold') FROM characters WHERE id = %d) = 35
THEN RAISE(ABORT, '%s') ELSE RAISE(ABORT, 'treasury preceded character credit') END; END`,
			operation, operation, f.master.ID, witness)
		if _, err := f.s.db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	collected, code, err := f.a.CollectTax(testDivision, 1, f.master.ID, 25)
	if collected != 0 || code != 2 || err == nil || !strings.Contains(err.Error(), witness) {
		t.Fatalf("failed collection=%d code=%d err=%v, want 0/2 and post-credit trigger witness", collected, code, err)
	}
	f.balances(t, f.master.ID, 10, 100)
	for _, operation := range []string{"INSERT", "UPDATE"} {
		if _, err := f.s.db.Exec("DROP TRIGGER reject_tax_" + operation); err != nil {
			t.Fatal(err)
		}
	}
	f.reopen(t)
	f.balances(t, f.master.ID, 10, 100)
	collected, code, err = f.a.CollectTax(testDivision, 1, f.master.ID, 25)
	if collected != 25 || code != 0 || err != nil {
		t.Fatalf("retry collection=%d code=%d err=%v", collected, code, err)
	}
	f.balances(t, f.master.ID, 35, 75)
}

/*
================
TestFortressTaxCollectionConcurrentConservation
================
*/
func TestFortressTaxCollectionConcurrentConservation(t *testing.T) {
	const treasury, initialGold, workers, request = 333, 41, 32, 17
	f := newFortressTaxFixture(t, treasury, initialGold)
	type result struct {
		collected int64
		code      uint8
		err       error
	}
	results := make(chan result, workers)
	start := make(chan struct{})
	var group sync.WaitGroup
	for i := 0; i < workers; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			collected, code, err := f.a.CollectTax(testDivision, 1, f.master.ID, request)
			results <- result{collected, code, err}
		}()
	}
	close(start)
	group.Wait()
	close(results)
	var collected int64
	successes, refusals := 0, 0
	for result := range results {
		if result.err != nil {
			t.Fatalf("concurrent collection=%+v", result)
		}
		switch {
		case result.code == 0 && result.collected == request:
			successes++
		case result.code == 2 && result.collected == 0:
			refusals++
		default:
			t.Fatalf("concurrent collection=%+v, want full withdrawal or code 2 with no collection", result)
		}
		collected += result.collected
	}
	const expectedSuccesses = treasury / request
	const remaining = treasury % request
	if successes != expectedSuccesses || refusals != workers-expectedSuccesses || collected != treasury-remaining {
		t.Fatalf("successes=%d refusals=%d collected=%d, want %d/%d/%d",
			successes, refusals, collected, expectedSuccesses, workers-expectedSuccesses, treasury-remaining)
	}
	f.balances(t, f.master.ID, initialGold+collected, treasury-collected)
	f.reopen(t)
	f.balances(t, f.master.ID, initialGold+collected, treasury-collected)
	// An oversized request leaves the remainder intact for an exact withdrawal.
	drained, code, err := f.a.CollectTax(testDivision, 1, f.master.ID, remaining)
	if drained != remaining || code != 0 || err != nil {
		t.Fatalf("remainder collection=%d code=%d err=%v, want %d/0/nil", drained, code, err, remaining)
	}
	collected += drained
	if collected != treasury {
		t.Fatalf("total collected=%d, want %d", collected, treasury)
	}
	f.balances(t, f.master.ID, initialGold+collected, 0)
	f.reopen(t)
	f.balances(t, f.master.ID, initialGold+collected, 0)
}

/*
================
TestFortressTaxCollectionTemporaryHolder

61D280 selects TempGuildID when nonzero, otherwise GuildID. With both tax
and war periods active, only the temporary holder's master may collect.
================
*/
func TestFortressTaxCollectionTemporaryHolder(t *testing.T) {
	f := newFortressTaxFixture(t, 100, 10)
	if err := f.s.Fortresses().SaveFortress(testDivision, domain.FortressRecord{
		FortressID: 1, GuildID: f.guildID, TempGuildID: f.rivalGuildID, TaxGold: 100, TaxRate: 7, StaffFlags: 3,
	}); err != nil {
		t.Fatal(err)
	}
	f.restore(t)
	f.a.SetPeriod(testDivision, fortress.PeriodWar, true)
	collected, code, err := f.a.CollectTax(testDivision, 1, f.master.ID, 25)
	if collected != 0 || code != 6 || err != nil {
		t.Fatalf("permanent holder collection=%d code=%d err=%v, want 0/6/nil", collected, code, err)
	}
	collected, code, err = f.a.CollectTax(testDivision, 1, f.rival.ID, 25)
	if collected != 25 || code != 0 || err != nil {
		t.Fatalf("temporary holder collection=%d code=%d err=%v, want 25/0/nil", collected, code, err)
	}
	f.balances(t, f.master.ID, 10, 75)
	f.balances(t, f.rival.ID, 25, 75)
	f.reopen(t)
	f.balances(t, f.master.ID, 10, 75)
	f.balances(t, f.rival.ID, 25, 75)
}
