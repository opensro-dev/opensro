/*
===========================================================================

grant_days_test.go - free grants per account per day (#3307)

The kit and the refill each count under their own grant; a door that
is no free grant does not count; the day stays after the character is
deleted; a failed commit counts once; the starter, beta and referral
silk count and an operator's grant does not.

===========================================================================
*/
package store

import (
	"errors"
	"testing"
	"time"

	"opensro.online/server/internal/domain"
)

/*
================
storedGrantDay
================
*/
type storedGrantDay struct {
	account, source, item string
	total, n              int64
}

/*
================
grantDays

Every stored grant day, in key order.
================
*/
func grantDays(t *testing.T, s *Store) []storedGrantDay {
	t.Helper()
	rows, err := s.syncDBLocked().Query(`SELECT account_id, source, item, total, n FROM grant_account_days
ORDER BY day_ms, account_id, source, item`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []storedGrantDay
	for rows.Next() {
		var row storedGrantDay
		if err := rows.Scan(&row.account, &row.source, &row.item, &row.total, &row.n); err != nil {
			t.Fatal(err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

/*
================
TestFreeItemGrantsCountPerAccountAndSurviveDeletion
================
*/
func TestFreeItemGrantsCountPerAccountAndSurviveDeletion(t *testing.T) {
	t.Parallel()

	clock := newTestClock()
	s := openTest(t, t.TempDir(), clock)
	c := newItemCharacter(t, s, "GrantKit")
	s.UpdateCharacter(c, domain.GrantStarterKit, func() bool {
		c.MissionInventory = append(c.MissionInventory, potionRow(14, 5))
		return true
	})
	s.UpdateCharacter(c, domain.GrantStarterRefill, func() bool {
		c.MissionInventory[1].StackCount += 3
		return true
	})
	s.UpdateCharacter(c, domain.GrantStarterRefill, func() bool {
		c.MissionInventory[1].StackCount += 2
		return true
	})
	s.UpdateCharacter(c, "quest-reward", func() bool {
		c.MissionInventory[1].StackCount += 9
		return true
	})
	account := "item-account-GrantKit"
	want := []storedGrantDay{
		{account, domain.GrantStarterKit, "ITEM_ETC_HP_POTION_01", 5, 1},
		{account, domain.GrantStarterRefill, "ITEM_ETC_HP_POTION_01", 5, 2},
	}
	assertGrantDays(t, grantDays(t, s), want)

	s.MutateCharacter(c, "delete-reserve GrantKit", func() {
		c.DeletePending = true
		c.DeleteReservedAt = clock.Now().Add(-8 * 24 * time.Hour).UTC().Format("2006-01-02T15:04:05.000Z")
	})
	if reaped := s.ReapMaturedDeletions(); len(reaped) != 1 {
		t.Fatalf("reaped %v, want the matured character", reaped)
	}
	assertGrantDays(t, grantDays(t, s), want)
}

/*
================
TestEventBotRewardCountsTowardTheDailyCap

The EventBot win grant (domain.GrantEventBotReward, #3533) is a free
grant like the kit: FreeGrantDayCount reads its grants per account
and UTC day, and no other door moves the count.
================
*/
func TestEventBotRewardCountsTowardTheDailyCap(t *testing.T) {
	t.Parallel()

	clock := newTestClock()
	s := openTest(t, t.TempDir(), clock)
	c := newItemCharacter(t, s, "EventWinner")
	for range 2 {
		s.UpdateCharacter(c, domain.GrantEventBotReward, func() bool {
			c.MissionInventory = append(c.MissionInventory, potionRow(14, 10))
			return true
		})
	}
	s.UpdateCharacter(c, "quest-reward", func() bool {
		c.MissionInventory = append(c.MissionInventory, potionRow(15, 10))
		return true
	})
	account := "item-account-EventWinner"
	nowMs := clock.Now().UnixMilli()
	if n, err := s.FreeGrantDayCount(account, domain.GrantEventBotReward, nowMs); err != nil || n != 2 {
		t.Fatalf("FreeGrantDayCount = %d, %v; want 2", n, err)
	}
	if n, err := s.FreeGrantDayCount(account, domain.GrantEventBotReward, nowMs+grantDayMs); err != nil || n != 0 {
		t.Fatalf("next day count = %d, %v; want 0", n, err)
	}
	if n, err := s.FreeGrantDayCount("nobody", domain.GrantEventBotReward, nowMs); err != nil || n != 0 {
		t.Fatalf("unknown account count = %d, %v; want 0", n, err)
	}
}

/*
================
TestFreeItemGrantWaitsThroughAFailedCommit
================
*/
func TestFreeItemGrantWaitsThroughAFailedCommit(t *testing.T) {
	t.Parallel()

	s := openTest(t, t.TempDir(), newTestClock())
	c := newItemCharacter(t, s, "GrantFail")
	s.FailCommits(errors.New("disk failure"))
	s.UpdateCharacter(c, domain.GrantStarterKit, func() bool {
		c.MissionInventory = append(c.MissionInventory, potionRow(14, 4))
		return true
	})
	if days := grantDays(t, s); len(days) != 0 {
		t.Fatalf("a failed commit wrote %+v", days)
	}
	s.FailCommits(nil)
	for range 2 {
		s.UpdateCharacter(c, "move", func() bool {
			c.Level = new(int64)
			return true
		})
	}
	assertGrantDays(t, grantDays(t, s), []storedGrantDay{
		{"item-account-GrantFail", domain.GrantStarterKit, "ITEM_ETC_HP_POTION_01", 4, 1}})
}

/*
================
TestFreeSilkGrantsCountAndOperatorGrantsDoNot
================
*/
func TestFreeSilkGrantsCountAndOperatorGrantsDoNot(t *testing.T) {
	t.Parallel()

	s := openTest(t, t.TempDir(), newTestClock())
	defer s.Close()
	if granted, err := s.GrantBetaSilkStarter("silk-acct", 100); !granted || err != nil {
		t.Fatalf("starter silk: %v, %v", granted, err)
	}
	if _, credited, err := s.CreditBetaSilk("silk-acct", 7, 1000, 0); !credited || err != nil {
		t.Fatalf("beta silk: %v, %v", credited, err)
	}
	if _, err := s.GrantSilk("silk-acct", 500); err != nil {
		t.Fatal(err)
	}
	code, _ := s.ReferralCode("inviter")
	if err := s.LinkReferral("friend", code, false); err != nil {
		t.Fatal(err)
	}
	playReferee(t, s, "friend", 30)
	if paid, err := s.QualifyReferral("friend", 30, testReferralRewards); !paid || err != nil {
		t.Fatalf("referral not paid: %v, %v", paid, err)
	}
	assertGrantDays(t, grantDays(t, s), []storedGrantDay{
		{"friend", domain.GrantReferralSilk, "gift-silk", 50, 1},
		{"inviter", domain.GrantReferralSilk, "gift-silk", 100, 1},
		{"silk-acct", domain.GrantBetaSilk, "silk", 7, 1},
		{"silk-acct", domain.GrantStarterSilk, "silk", 100, 1},
	})
}

// assertGrantDays fails unless got is exactly want.
func assertGrantDays(t *testing.T, got, want []storedGrantDay) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("grant days %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("grant days %+v, want %+v", got, want)
		}
	}
}

/*
================
TestGrantDaysTableIsAddedToAnOlderFile

A file an older binary wrote has no day table; the next open adds it
and the first free grant lands there.
================
*/
func TestGrantDaysTableIsAddedToAnOlderFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	first := openTest(t, dir, newTestClock())
	if _, err := first.syncDBLocked().Exec(`DROP TABLE grant_account_days`); err != nil {
		t.Fatal(err)
	}
	first.Close()
	s := openTest(t, dir, newTestClock())
	if granted, err := s.GrantBetaSilkStarter("late-acct", 30); !granted || err != nil {
		t.Fatalf("starter silk on the older file: %v, %v", granted, err)
	}
	assertGrantDays(t, grantDays(t, s), []storedGrantDay{{"late-acct", domain.GrantStarterSilk, "silk", 30, 1}})
}
