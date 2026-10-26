/*
===========================================================================

wiring_sealboard.go - the weekly seal board's counter, installed (#1641)

Port-only, not native: with SRO_SEAL_BOARD off (the default) nothing is
opened and nothing counts. On, the sealboard service opens its
seal-board.db beside the shard's authority state (created on first open;
no operator step), the action runtime counts into it, and the
application owns its flush loop and close.

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
	"opensro.online/server/internal/platform/sealboard"
)

/*
================
installSealBoard

Opens the counter and points the action runtime's seal sink at it. The
application owns the service from here: Run flushes it, shutdown closes
it (a rollback closes it too).
================
*/
func installSealBoard(application *gameWorldApplication, gameplay *gameplayPlane, shardID string) error {
	if !action.SealBoardFromEnv() {
		return nil
	}
	dir := store.DirForShardFromEnv(shardID)
	service, err := sealboard.Open(dir, time.Now)
	if err != nil {
		return fmt.Errorf("seal board: %w", err)
	}
	application.sealBoard = service
	gameplay.items.SealDrops = service
	log.Infof("seal board: weekly seal-drop counting ON (%s); counts in %s",
		action.EnvSealBoard, filepath.Join(dir, sealboard.FileName))
	return nil
}
