/*
===========================================================================

wiring_referrals.go - the referral reward sweep (#3518)

Port-only, not native (SRO_REFERRALS, off by default): with the flag on,
the GameWorld pays qualified referrals every referral.SweepInterval
(game/referral). Off, nothing runs and the website's referral routes are
not served (public_api.go).

===========================================================================
*/
package main

import (
	"context"

	"golang.org/x/sync/errgroup"
	"opensro.online/server/internal/game/referral"
)

/*
================
goReferralSweep
================
*/
func (application *gameWorldApplication) goReferralSweep(ctx context.Context, group *errgroup.Group) {
	if application.authority == nil || !referral.FromEnv() {
		return
	}
	group.Go(func() error {
		referral.Run(ctx, application.authority)
		return nil
	})
}
