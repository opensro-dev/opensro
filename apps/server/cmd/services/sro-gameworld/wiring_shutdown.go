/*
===========================================================================

wiring_shutdown.go - the GameWorld's orderly shutdown tail

The application closes admission, drains both listeners, releases
the shard lease and closes durable authority last; a startup failure
rolls the same seats back in reverse. This file owns only that
tail: the construction in wiring.go publishes handles, and Run
(wiring.go) decides when the tail runs.

===========================================================================
*/
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"

	log "github.com/sirupsen/logrus"
	"golang.org/x/sync/errgroup"
)

/*
================
drainNetwork
================
*/
func (application *gameWorldApplication) drainNetwork() error {
	if application.preserveArenaRewards != nil {
		application.preserveArenaRewards()
	}
	if application.shutdownEventBot != nil {
		application.shutdownEventBot()
	}
	application.readiness.Close()
	log.Info("shutdown: draining the game transport and control API")

	ctx, cancel := context.WithTimeout(
		context.Background(),
		networkDrainTimeout,
	)
	defer cancel()
	var drains errgroup.Group
	if application.transport != nil {
		drains.Go(func() error {
			if err := application.transport.Shutdown(ctx); err != nil {
				return fmt.Errorf("transport drain: %w", err)
			}
			return nil
		})
	}
	if application.controlAPI != nil {
		drains.Go(func() error {
			if err := application.controlAPI.Shutdown(ctx); err != nil {
				return fmt.Errorf("control API drain: %w", err)
			}
			return nil
		})
	}
	return drains.Wait()
}

/*
================
releaseLease
================
*/
func (application *gameWorldApplication) releaseLease() {
	if !application.leaseOwned || application.reporter == nil {
		return
	}
	ctx, cancel := context.WithTimeout(
		context.Background(),
		leaseReleaseTimeout,
	)
	defer cancel()
	if err := application.reporter.Release(ctx); err != nil {
		log.Warnf("shutdown: releasing shard lease: %v", err)
	}
}

/*
================
rollback
================
*/
func (application *gameWorldApplication) rollback() {
	application.readiness.Close()
	if err := application.drainNetwork(); err != nil &&
		!errors.Is(err, http.ErrServerClosed) {
		log.Warnf("startup rollback: %v", err)
	}
	if application.publicAPI != nil {
		if err := application.publicAPI.Close(); err != nil {
			log.Warnf("startup rollback public api: %v", err)
		}
	}
	for _, cache := range []io.Closer{application.skillCache, application.itemCache} {
		if cache != nil {
			if err := cache.Close(); err != nil {
				log.Warnf("startup rollback reference cache: %v", err)
			}
		}
	}
	if application.populationCache != nil {
		if err := application.populationCache.Close(); err != nil {
			log.Warnf("startup rollback population cache: %v", err)
		}
	}
	if application.authority != nil {
		application.authority.Close()
	}
	application.releaseLease()
	application.leaseOwned = false
	if application.economy != nil {
		_ = application.economy.Close()
	}
	_ = application.ledgers.close()
	if application.settings != nil {
		_ = application.settings.Close()
	}
	// The history journal closes last, as in Run: every rollback line
	// above is still recorded (#3186).
	if application.history != nil {
		_ = application.history.Close()
	}
	application.closeGuard("startup rollback")
}
