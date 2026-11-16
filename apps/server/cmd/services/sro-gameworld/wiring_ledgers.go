/*
===========================================================================

wiring_ledgers.go - the shard's port-only ledgers and their lifecycle

The seal board (wiring_sealboard.go) and job honor (wiring_jobhonor.go,
#3530) are port-only, not native: each is off by default behind its own
flag, and when on it opens a small database beside the shard's authority
state, flushes on its own timer while the world runs, and is flushed and
closed once at drain or when a failed boot rolls back. That shared
lifecycle lives here, so the application calls one owner for all of them.

The AI caravans' memories (SRO_AI_CARAVANS, wiring_caravanai.go) share
it: they are written behind into the authority store, so they flush on
their own timer and once more at drain, before that store closes. A
failed boot never ran the tick, so their close then has nothing to write.

===========================================================================
*/
package main

import (
	"context"
	"errors"

	"golang.org/x/sync/errgroup"
	"opensro.online/server/internal/game/caravanai"
	"opensro.online/server/internal/platform/jobhonor"
	"opensro.online/server/internal/platform/sealboard"
)

/*
================
shardLedgers

The open ledgers; a nil one is switched off.
================
*/
type shardLedgers struct {
	// sealBoard is the weekly seal-drop counter; nil off.
	sealBoard *sealboard.Service
	// jobHonor is the job honor ledger; nil off.
	jobHonor *jobhonor.Service
	// caravans is the AI caravan runtime's memory flush; nil off.
	caravans *caravanai.Runtime
}

/*
================
installShardLedgers

Opens each ledger whose flag is on, seal board first.
================
*/
func installShardLedgers(application *gameWorldApplication, gameplay *gameplayPlane, shardID string) error {
	if err := installSealBoard(application, gameplay, shardID); err != nil {
		return err
	}
	if err := installJobHonor(application, gameplay, shardID); err != nil {
		return err
	}
	application.ledgers.caravans = gameplay.caravanAgents
	return nil
}

/*
================
shardLedgers.run

Starts each open ledger's timer in the run group.
================
*/
func (ledgers *shardLedgers) run(group *errgroup.Group, ctx context.Context) {
	if ledgers.sealBoard != nil {
		group.Go(func() error {
			ledgers.sealBoard.Run(ctx)
			return nil
		})
	}
	if ledgers.jobHonor != nil {
		group.Go(func() error {
			ledgers.jobHonor.Run(ctx)
			return nil
		})
	}
	if ledgers.caravans != nil {
		group.Go(func() error {
			ledgers.caravans.Run(ctx)
			return nil
		})
	}
}

/*
================
shardLedgers.close

Flushes and closes each open ledger, seal board first; every error is
kept.
================
*/
func (ledgers *shardLedgers) close() error {
	var err error
	if ledgers.sealBoard != nil {
		err = errors.Join(err, ledgers.sealBoard.Close())
	}
	if ledgers.jobHonor != nil {
		err = errors.Join(err, ledgers.jobHonor.Close())
	}
	if ledgers.caravans != nil {
		err = errors.Join(err, ledgers.caravans.Close())
	}
	return err
}
