/*
===========================================================================

wiring_economy.go - the staff economy view's aggregates (port-only)

Snapshots the authority every SRO_ECONOMY_SNAPSHOT_HOURS (6 by default, 0
turns it off) and serves /economy/v1/* on the local operator listener,
guarded by the shard's economy-token file (platform/economy).

===========================================================================
*/
package main

import (
	"path/filepath"
	"time"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/data/store"
	"opensro.online/server/internal/platform/economy"
)

/*
================
installEconomy
================
*/
func installEconomy(application *gameWorldApplication, authority *authorityPlane, shardID string) error {
	every, err := economy.SnapshotEveryFromEnv()
	if err != nil {
		return err
	}
	if every <= 0 {
		log.Infof("economy: aggregates OFF (%s=0)", economy.EnvSnapshotHours)
		return nil
	}
	dir := store.DirForShardFromEnv(shardID)
	service, err := economy.Open(dir, authority.store, every, time.Now)
	if err != nil {
		return err
	}
	handler, err := service.Handler(filepath.Join(dir, economy.TokenFile))
	if err != nil {
		_ = service.Close()
		return err
	}
	authority.agentAPI.InstallEconomy(economy.Path, handler)
	application.economy = service
	log.Infof("economy: snapshot every %v; aggregates on %s", every, economy.Path)
	return nil
}
