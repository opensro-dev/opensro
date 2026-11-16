/*
===========================================================================

wiring_jobhonor.go - the job honor ledger, installed (#3530)

Port-only, not native (SRO_JOB_HONOR, off by default). On, the ledger
(platform/jobhonor) opens beside the shard's authority state, the action
runtime scores trade sales into it, the application runs its flush and
05:00 tier change and closes it at shutdown, and the public API serves
its board. Off, nothing opens, nothing scores and the route answers 404.

===========================================================================
*/
package main

import (
	"fmt"
	"path/filepath"
	"time"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/data/store"
	"opensro.online/server/internal/game/action"
	"opensro.online/server/internal/platform/jobhonor"
)

/*
================
installJobHonor

Opens the ledger and points the action runtime's job honor sink at it.
The application owns the service from here, as it owns the seal board.
================
*/
func installJobHonor(application *gameWorldApplication, gameplay *gameplayPlane, shardID string) error {
	if !action.JobHonorFromEnv() {
		return nil
	}
	dir := store.DirForShardFromEnv(shardID)
	service, err := jobhonor.Open(dir, time.Now)
	if err != nil {
		return fmt.Errorf("job honor: %w", err)
	}
	application.ledgers.jobHonor = service
	gameplay.jobHonor = service
	gameplay.items.JobHonor = service
	log.Infof("job honor: trade sales score honor points (%s, port-only, not native); ledger in %s",
		action.EnvJobHonor, filepath.Join(dir, jobhonor.FileName))
	return nil
}
