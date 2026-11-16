/*
===========================================================================

referrals_test.go - referral codes, links and the one-time reward

===========================================================================
*/
package store

import (
	"errors"
	"testing"

	"opensro.online/server/internal/domain"
)

var testReferralRewards = domain.ReferralRewards{ReferrerSilk: 100, RefereeSilk: 50, RewardCap: 2}

/*
================
playReferee

Gives the account a character at the given level.
================
*/
func playReferee(t *testing.T, s *Store, account string, level int64) *domain.Character {
	t.Helper()
	character := seededCharacter()
	character.Name = "Ref" + account
	if err := s.CreateCharacter("alpha", account, character); err != nil {
		t.Fatal(err)
	}
	s.MutateCharacter(character, "test-level", func() { character.Level = &level })
	return character
}

/*
================
silkOf
================
*/
func silkOf(t *testing.T, s *Store, account string) uint32 {
	t.Helper()
	balance, err := s.GrantSilk(account, 0)
	if err != nil {
		t.Fatal(err)
	}
	// A reward is gift silk only: purchased silk, which the market takes
	// and pays, never moves.
	if balance.Silk != 0 {
		t.Fatalf("%s holds %d purchased silk; referral rewards are gift silk", account, balance.Silk)
	}
	return balance.GiftSilk
}

/*
================
TestReferralCodeIsMadeOnceAndReadsBack
================
*/
func TestReferralCodeIsMadeOnceAndReadsBack(t *testing.T) {
	t.Parallel()
	s := openTest(t, t.TempDir(), newTestClock())
	defer s.Close()

	code, err := s.ReferralCode("inviter")
	if err != nil || NormalizeReferralCode(code) != code {
		t.Fatalf("code %q, %v", code, err)
	}
	again, err := s.ReferralCode("inviter")
	if err != nil || again != code {
		t.Fatalf("second read %q, want %q (%v)", again, code, err)
	}
	other, _ := s.ReferralCode("someone-else")
	if other == code {
		t.Fatal("two accounts share a code")
	}
	owner, err := s.ReferralCodeOwner(" " + code[:4] + "-" + code[4:] + " ")
	if err != nil || owner != "inviter" {
		t.Fatalf("owner of a typed code %q, %v", owner, err)
	}
	if _, err := s.ReferralCodeOwner("NOPE"); !errors.Is(err, ErrReferralUnknownCode) {
		t.Fatalf("unknown code: %v", err)
	}
}

/*
================
TestLinkReferralRefusesSelfTwiceAndLate
================
*/
func TestLinkReferralRefusesSelfTwiceAndLate(t *testing.T) {
	t.Parallel()
	s := openTest(t, t.TempDir(), newTestClock())
	defer s.Close()

	code, _ := s.ReferralCode("inviter")
	if err := s.LinkReferral("inviter", code, false); !errors.Is(err, ErrReferralSelf) {
		t.Fatalf("self: %v", err)
	}
	if err := s.LinkReferral("friend", "ZZZZZZZZ", false); !errors.Is(err, ErrReferralUnknownCode) {
		t.Fatalf("unknown code: %v", err)
	}
	if err := s.LinkReferral("friend", code, false); err != nil {
		t.Fatal(err)
	}
	if err := s.LinkReferral("friend", code, false); !errors.Is(err, ErrReferralLinked) {
		t.Fatalf("second link: %v", err)
	}
	playReferee(t, s, "veteran", 1)
	if err := s.LinkReferral("veteran", code, false); !errors.Is(err, ErrReferralTooLate) {
		t.Fatalf("account that already plays: %v", err)
	}
	got, err := s.ReferralsBy("inviter")
	if err != nil || len(got) != 1 || got[0].Referee != "friend" {
		t.Fatalf("referrals %+v, %v", got, err)
	}
}

/*
================
TestQualifyReferralPaysOnceFromRealLevel
================
*/
func TestQualifyReferralPaysOnceFromRealLevel(t *testing.T) {
	t.Parallel()
	s := openTest(t, t.TempDir(), newTestClock())
	defer s.Close()

	code, _ := s.ReferralCode("inviter")
	if err := s.LinkReferral("friend", code, false); err != nil {
		t.Fatal(err)
	}
	character := playReferee(t, s, "friend", 29)
	if paid, err := s.QualifyReferral("friend", 30, testReferralRewards); paid || err != nil {
		t.Fatalf("paid below the level: %v, %v", paid, err)
	}
	level := int64(30)
	s.MutateCharacter(character, "test-level", func() { character.Level = &level })
	if paid, err := s.QualifyReferral("friend", 30, testReferralRewards); !paid || err != nil {
		t.Fatalf("not paid at the level: %v, %v", paid, err)
	}
	if paid, err := s.QualifyReferral("friend", 30, testReferralRewards); paid || err != nil {
		t.Fatalf("paid twice: %v, %v", paid, err)
	}
	if got := silkOf(t, s, "inviter"); got != 100 {
		t.Fatalf("inviter silk %d, want 100", got)
	}
	if got := silkOf(t, s, "friend"); got != 50 {
		t.Fatalf("friend silk %d, want 50", got)
	}
	row, err := s.ReferralOf("friend")
	if err != nil || row.QualifiedMs == 0 || row.ReferrerSilk != 100 || row.RefereeSilk != 50 {
		t.Fatalf("row %+v, %v", row, err)
	}
	if pending, _ := s.PendingReferrals("", 10); len(pending) != 0 {
		t.Fatalf("still pending: %+v", pending)
	}
}

/*
================
TestHeldReferralPaysOnlyAfterApproval
================
*/
func TestHeldReferralPaysOnlyAfterApproval(t *testing.T) {
	t.Parallel()
	s := openTest(t, t.TempDir(), newTestClock())
	defer s.Close()

	code, _ := s.ReferralCode("inviter")
	if err := s.LinkReferral("alt", code, true); err != nil {
		t.Fatal(err)
	}
	playReferee(t, s, "alt", 90)
	if pending, _ := s.PendingReferrals("", 10); len(pending) != 0 {
		t.Fatalf("a held referral is pending: %+v", pending)
	}
	if paid, _ := s.QualifyReferral("alt", 30, testReferralRewards); paid {
		t.Fatal("a held referral paid")
	}
	if err := s.ApproveReferral("alt"); err != nil {
		t.Fatal(err)
	}
	if paid, err := s.QualifyReferral("alt", 30, testReferralRewards); !paid || err != nil {
		t.Fatalf("approved referral not paid: %v, %v", paid, err)
	}
	if err := s.ApproveReferral("nobody"); !errors.Is(err, ErrReferralNotFound) {
		t.Fatalf("approve unknown: %v", err)
	}
}

/*
================
TestReferrerPaidOnlyUpToTheCap
================
*/
func TestReferrerPaidOnlyUpToTheCap(t *testing.T) {
	t.Parallel()
	s := openTest(t, t.TempDir(), newTestClock())
	defer s.Close()

	code, _ := s.ReferralCode("inviter")
	for _, friend := range []string{"f1", "f2", "f3"} {
		if err := s.LinkReferral(friend, code, false); err != nil {
			t.Fatal(err)
		}
		playReferee(t, s, friend, 30)
		if paid, err := s.QualifyReferral(friend, 30, testReferralRewards); !paid || err != nil {
			t.Fatalf("%s: %v, %v", friend, paid, err)
		}
	}
	if got := silkOf(t, s, "inviter"); got != 200 {
		t.Fatalf("inviter silk %d, want 200 (cap 2)", got)
	}
	if got := silkOf(t, s, "f3"); got != 50 {
		t.Fatalf("the third friend's gift %d, want 50", got)
	}
}
