/*
===========================================================================

referral.go - the referral rules and the reward sweep (#3518)

Port-only, not native (SRO_REFERRALS, off by default): the original has no
referral system. Off, nothing here runs and the website's referral routes
are not served, which is native.

The constants are the server's rules; the website reads them from the
referral route and never writes its own numbers. A referral pays when one
of the invited account's characters first reaches QualifyingLevel: the
sweep finds pending referrals every SweepInterval and asks the store to pay
each one that has, in one transaction per referral. The sweep, not the
level-up code, does this because a level-up runs inside its character's
transaction, and a second store transaction there would nest.

The numbers are the owner's (2026-11-16, #3518): 500 Silk to the inviter
and 250 to the invited player once one invited character reaches level 30,
for the first ten invitations of each inviter. Level 30 is past the
beginner zones (Jangan's first levels) and takes real play, so a fresh alt
mints nothing; the cap of ten bounds what a farm of real-but-shared
accounts could earn.

===========================================================================
*/
package referral

import (
	"context"
	"os"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/domain"
)

// EnvReferrals switches the referral system on ("on", "1" or "true").
const EnvReferrals = "SRO_REFERRALS"

const (
	// QualifyingLevel is the level one invited character must reach.
	QualifyingLevel = 30
	// ReferrerSilk is the inviter's reward per qualified invitation.
	ReferrerSilk = 500
	// RefereeSilk is the invited account's gift when it qualifies.
	RefereeSilk = 250
	// RewardCap is how many invitations pay the inviter; later ones still
	// pay the invited player's gift.
	RewardCap = 10
	// SweepInterval is how often pending referrals are checked.
	SweepInterval = 5 * time.Minute
	// SweepBatch bounds one pass; a longer queue drains over passes.
	SweepBatch = 200
)

/*
================
FromEnv
================
*/
func FromEnv() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvReferrals))) {
	case "on", "1", "true":
		return true
	}
	return false
}

/*
================
Rewards
================
*/
func Rewards() domain.ReferralRewards {
	return domain.ReferralRewards{ReferrerSilk: ReferrerSilk, RefereeSilk: RefereeSilk, RewardCap: RewardCap}
}

/*
================
Store

The store doors the sweep needs (data/store/referrals.go).
================
*/
type Store interface {
	PendingReferrals(after string, limit int) ([]domain.Referral, error)
	QualifyReferral(referee string, level int64, rewards domain.ReferralRewards) (bool, error)
}

/*
================
Sweep

One pass over up to SweepBatch pending referrals after the cursor: pays
each whose invited account has reached the level. A referral the store
fails on stays pending. Returns how many were paid and the cursor for the
next pass ("" once the end was reached, so the next pass starts over).
================
*/
func Sweep(store Store, after string) (int, string) {
	pending, err := store.PendingReferrals(after, SweepBatch)
	if err != nil {
		log.WithError(err).Warn("referrals: pending read failed; retrying next pass")
		return 0, after
	}
	next := ""
	if len(pending) == SweepBatch {
		next = pending[len(pending)-1].Referee
	}
	paid := 0
	for _, row := range pending {
		ok, err := store.QualifyReferral(row.Referee, QualifyingLevel, Rewards())
		if err != nil {
			log.WithError(err).Warn("referrals: reward failed; retrying next pass")
			continue
		}
		if ok {
			paid++
		}
	}
	return paid, next
}

/*
================
Run

Sweeps every SweepInterval until ctx ends.
================
*/
func Run(ctx context.Context, store Store) {
	ticker := time.NewTicker(SweepInterval)
	defer ticker.Stop()
	cursor := ""
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			var paid int
			paid, cursor = Sweep(store, cursor)
			if paid > 0 {
				log.Infof("referrals: %d invitation(s) qualified and paid", paid)
			}
		}
	}
}
