/*
===========================================================================

market_commitfail_test.go - every market door, failed at its commit

The market design's invariant: a door that fails to commit leaves every
item, wallet and listing exactly where it was. Each case runs its door
once with a forced commit failure (FailCommits fails the transaction
after all of its writes) and checks the whole market state is unchanged,
on disk and in the live records, then runs it again for real and checks
it did move something, so a door that silently does nothing cannot pass.

===========================================================================
*/
package store

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"opensro.online/server/internal/domain"
)

// marketStateTables are the tables a market door may write.
var marketStateTables = []string{"account_storage", "mall_accounts", "characters", "market_listings", "market_sales",
	"economy_ledger", "silk_history"}

/*
================
marketState

Every row of the tables a door may write, plus each live character's bag
and gold, as one comparable string.
================
*/
func marketState(t *testing.T, s *Store, characters ...*domain.Character) string {
	t.Helper()
	var out strings.Builder
	for _, table := range marketStateTables {
		rows, err := s.syncDBLocked().Query("SELECT * FROM " + table + " ORDER BY 1, 2")
		if err != nil {
			t.Fatalf("%s: %v", table, err)
		}
		columns, err := rows.Columns()
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			values := make([]any, len(columns))
			pointers := make([]any, len(columns))
			for i := range values {
				pointers[i] = &values[i]
			}
			if err := rows.Scan(pointers...); err != nil {
				t.Fatal(err)
			}
			fmt.Fprintf(&out, "%s %v\n", table, values)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		if err := rows.Close(); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range characters {
		gold := int64(0)
		if c.Gold != nil {
			gold = *c.Gold
		}
		fmt.Fprintf(&out, "live %s gold %d bag", c.Name, gold)
		for _, row := range c.MissionInventory {
			fmt.Fprintf(&out, " %d:%s", row.Slot, row.Codename)
		}
		out.WriteString("\n")
	}
	return out.String()
}

/*
================
marketDoorCase

One door, set up on a fresh store. The returned func runs it once and
returns its error; the characters are the live records it may touch.
================
*/
type marketDoorCase struct {
	name  string
	setup func(t *testing.T) (*Store, func() error, []*domain.Character)
}

/*
================
expiredHeldListing

A Silk listing that expired into a full warehouse and is held, with one
warehouse slot freed again, ready to take back.
================
*/
func expiredHeldListing(t *testing.T) (*Store, *domain.Character, *domain.Character, domain.MarketListing) {
	t.Helper()
	s, clock, seller, buyer := marketFixture(t)
	listing := listSword(t, s, domain.MarketSilk, 50)
	if _, err := s.TransactStorage(seller, func(_ *domain.Character, storage *domain.AccountStorage) error {
		for slot := int64(0); slot < storage.Capacity; slot++ {
			storage.Rows = append(storage.Rows, domain.InventoryRow{Slot: slot, RefObjID: 62, Codename: "ITEM_ETC_GOLD_01",
				VarianceBits: "0", StackCount: 1})
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	clock.Advance(25 * time.Hour)
	if n, err := s.ExpireMarketListings(10); err != nil || n != 1 {
		t.Fatalf("expiry = %d, %v", n, err)
	}
	if _, err := s.TransactStorage(seller, func(_ *domain.Character, storage *domain.AccountStorage) error {
		storage.Rows = storage.Rows[1:]
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return s, seller, buyer, listing
}

var marketDoorCases = []marketDoorCase{
	{"list from the warehouse", func(t *testing.T) (*Store, func() error, []*domain.Character) {
		s, _, seller, buyer := marketFixture(t)
		return s, func() error {
			_, err := s.MarketList(domain.MarketListRequest{Division: testDivision, Account: marketSeller, Slot: 0,
				Currency: domain.MarketSilk, Price: 50, Admit: admitAll, Rules: testMarketRules})
			return err
		}, []*domain.Character{seller, buyer}
	}},
	{"list from the bag", func(t *testing.T) (*Store, func() error, []*domain.Character) {
		s := openTest(t, t.TempDir(), newTestClock())
		seller := seededCharacter()
		if err := s.CreateCharacter(testDivision, marketSeller, seller); err != nil {
			t.Fatal(err)
		}
		return s, func() error {
			_, err := s.MarketList(domain.MarketListRequest{Division: testDivision, Account: marketSeller, Character: seller,
				Slot: 13, Currency: domain.MarketGold, Price: 1000, Admit: admitAll, Rules: testMarketRules})
			return err
		}, []*domain.Character{seller}
	}},
	{"buy with Silk", func(t *testing.T) (*Store, func() error, []*domain.Character) {
		s, _, seller, buyer := marketFixture(t)
		listing := listSword(t, s, domain.MarketSilk, 500)
		if _, err := s.GrantSilk(marketBuyer, 800); err != nil {
			t.Fatal(err)
		}
		return s, func() error {
			_, err := s.MarketBuy(domain.MarketBuyRequest{ListingID: listing.ID, Version: listing.Version,
				BuyerAccount: marketBuyer, Rules: testMarketRules})
			return err
		}, []*domain.Character{seller, buyer}
	}},
	{"buy with gold", func(t *testing.T) (*Store, func() error, []*domain.Character) {
		s, _, seller, buyer := marketFixture(t)
		listing := listSword(t, s, domain.MarketGold, 1000)
		return s, func() error {
			_, err := s.MarketBuy(domain.MarketBuyRequest{ListingID: listing.ID, Version: listing.Version,
				BuyerAccount: marketBuyer, Payer: buyer, Rules: testMarketRules})
			return err
		}, []*domain.Character{seller, buyer}
	}},
	{"cancel", func(t *testing.T) (*Store, func() error, []*domain.Character) {
		s, _, seller, buyer := marketFixture(t)
		listing := listSword(t, s, domain.MarketSilk, 50)
		return s, func() error {
			_, err := s.MarketCancel(marketSeller, listing.ID)
			return err
		}, []*domain.Character{seller, buyer}
	}},
	{"expire", func(t *testing.T) (*Store, func() error, []*domain.Character) {
		s, clock, seller, buyer := marketFixture(t)
		listSword(t, s, domain.MarketSilk, 50)
		clock.Advance(25 * time.Hour)
		return s, func() error {
			n, err := s.ExpireMarketListings(10)
			if err == nil && n != 1 {
				err = fmt.Errorf("%d listings expired, want 1", n)
			}
			return err
		}, []*domain.Character{seller, buyer}
	}},
	{"take back", func(t *testing.T) (*Store, func() error, []*domain.Character) {
		s, seller, buyer, listing := expiredHeldListing(t)
		return s, func() error {
			_, err := s.MarketTakeBack(marketSeller, listing.ID)
			return err
		}, []*domain.Character{seller, buyer}
	}},
}

/*
================
TestMarketDoorsLeaveEverythingInPlaceWhenTheCommitFails
================
*/
func TestMarketDoorsLeaveEverythingInPlaceWhenTheCommitFails(t *testing.T) {
	t.Parallel()
	for _, door := range marketDoorCases {
		t.Run(door.name, func(t *testing.T) {
			t.Parallel()
			s, run, characters := door.setup(t)
			before := marketState(t, s, characters...)
			s.FailCommits(errors.New("disk failure"))
			if err := run(); err == nil {
				t.Fatal("the door succeeded through a failed commit")
			}
			s.FailCommits(nil)
			if after := marketState(t, s, characters...); after != before {
				t.Fatalf("a failed commit changed the market:\nbefore:\n%s\nafter:\n%s", before, after)
			}
			if n := swordCount(t, s, characters...); n != 1 {
				t.Fatalf("after the failed commit the sword is in %d places", n)
			}
			if err := run(); err != nil {
				t.Fatalf("the door refused after the disk recovered: %v", err)
			}
			if after := marketState(t, s, characters...); after == before {
				t.Fatal("the door changed nothing, so the failure case proved nothing")
			}
			if n := swordCount(t, s, characters...); n != 1 {
				t.Fatalf("after the door the sword is in %d places", n)
			}
		})
	}
}
