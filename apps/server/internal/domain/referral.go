/*
===========================================================================

referral.go - one account inviting another (#3518)

Port-only, not native: the original has no referral system. A referral is
created once, when the invited account signs up with a code, and is
rewarded once, when one of the invited account's characters first reaches
the qualifying level. The store owns the rows (data/store/referrals.go);
the GameWorld's sweep and the website's routes read them through it.

===========================================================================
*/
package domain

import "errors"

// The refusals a referral link or approval can answer; the store returns
// them and the website routes map them to statuses.
var (
	// ErrReferralUnknownCode: no account owns that code.
	ErrReferralUnknownCode = errors.New("referral: unknown code")
	// ErrReferralSelf: the code is the invited account's own.
	ErrReferralSelf = errors.New("referral: an account cannot invite itself")
	// ErrReferralLinked: the account already names a referrer.
	ErrReferralLinked = errors.New("referral: already linked")
	// ErrReferralTooLate: the account already has a character, so it is
	// past sign-up.
	ErrReferralTooLate = errors.New("referral: the account already plays")
	// ErrReferralNotFound: no referral for that invited account.
	ErrReferralNotFound = errors.New("referral: not found")
)

/*
================
Referral

One invited account. Held is true while staff must approve it (the website
saw both accounts on the same connection); QualifiedMs is non-zero once it
was rewarded, with the Silk each side received.
================
*/
type Referral struct {
	Referee      string
	Referrer     string
	LinkedMs     int64
	Held         bool
	QualifiedMs  int64
	ReferrerSilk uint32
	RefereeSilk  uint32
}

/*
================
ReferralRewards

What one qualified referral pays: Silk to the inviting account (only for
its first RewardCap paid invitations) and a gift to the invited one.
================
*/
type ReferralRewards struct {
	ReferrerSilk uint32
	RefereeSilk  uint32
	RewardCap    int
}
