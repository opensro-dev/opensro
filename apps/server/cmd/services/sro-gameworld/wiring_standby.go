/*
===========================================================================

wiring_standby.go - the canary standby handover's save-free load

Package main (the GameWorld's construction). What a boot loads before it
holds the shard: the game data bundle, the authored areas, the roster and
the standby-health listener, and the standby boot's posture towards the
shard process guard. The constructor in wiring.go drives them; an unarmed
boot runs none of this file's armed path.

===========================================================================
*/
package main

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/cluster/shard"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/alexandria"
	"opensro.online/server/internal/game/world/worldarea"
	"opensro.online/server/internal/gamedata"
	"opensro.online/server/internal/platform/processguard"
	"opensro.online/server/internal/transport"
)

/*
================
standbyBoot

How construction treats the shard process guard (opensro-ops#281).
Unarmed (the zero value): the caller already holds the guard and the
boot is the boot there has always been. Armed: construction probes the
guard at guardPath - won, it is the shard's writer; held by another
process, it loads everything that does not need the save and waits as
that process's standby.
================
*/
type standbyBoot struct {
	armed     bool
	guardPath string
}

/*
================
probeStandbyGuard

The armed boot's first act (opensro-ops#281): the standby-health
listener up, then one non-blocking probe of the shard process guard.
Won: this process is the shard's writer and construction continues
holding the guard. Held: construction loads without the public port
and waits (loadStandbyInputs) as the live process's standby.
================
*/
func probeStandbyGuard(startupContext context.Context, ts *transport.Server, ownedShard shard.Definition, boot standbyBoot) (guard *processguard.Guard, waiting bool, err error) {
	if err := startStandbyHealth(startupContext, ts); err != nil {
		return nil, false, err
	}
	guard, held, err := processguard.TryAcquire(boot.guardPath)
	if err != nil {
		return nil, false, fmt.Errorf("shard %q process guard: %w", ownedShard.ID, err)
	}
	if !held {
		return nil, true, nil
	}
	return guard, false, nil
}

/*
================
loadStandbyInputs

The armed boot's save-free load (opensro-ops#281): the game data
bundle, authored areas, the roster and the textdata catalogues, so a
standby is fully loaded while it waits, and a standby whose load fails
fails its deployment before the live process is stopped. A waiting
boot then waits for the guard and returns it for the application to
own (the settings store lies beside the save, so it opens after this,
with the rest of the ordinary boot); a boot that already holds the
guard returns nil.
================
*/
func loadStandbyInputs(startupContext context.Context, ts *transport.Server, ownedShard shard.Definition, boot standbyBoot, waiting bool) (saveFreeInputs, authorityTextdata, *processguard.Guard, error) {
	inputs, err := loadSaveFreeInputs()
	if err != nil {
		return saveFreeInputs{}, authorityTextdata{}, nil, err
	}
	loaded, err := loadAuthorityTextdata(inputs.devPaths)
	if err != nil {
		return saveFreeInputs{}, authorityTextdata{}, nil, err
	}
	if !waiting {
		return inputs, loaded, nil, nil
	}
	ts.SetBootPhase(transport.BootPhaseStandby)
	log.WithField("shard", ownedShard.ID).Info("standby: loaded and waiting for the live process's guard")
	guard, err := processguard.Acquire(startupContext, boot.guardPath)
	if err != nil {
		return saveFreeInputs{}, authorityTextdata{}, nil, fmt.Errorf("standby: shard %q process guard: %w", ownedShard.ID, err)
	}
	log.WithField("shard", ownedShard.ID).Info("standby: the live process is gone; taking over")
	return inputs, loaded, guard, nil
}

/*
================
closeGuard

Release the standby handover's guard (opensro-ops#281), wherever the
boot ends: last in Run's shutdown tail, after the authority store, or
on a construction failure in rollback. No-op on a boot whose caller
holds the guard.
================
*/
func (application *gameWorldApplication) closeGuard(where string) {
	if application.guard == nil {
		return
	}
	if err := application.guard.Close(); err != nil {
		log.Warnf("%s: process guard: %v", where, err)
	}
	application.guard = nil
}

/*
================
saveFreeInputs

What construction loads from the game data bundle before any of it
needs the save: the bundle's paths, the dev paths over them, the
authored areas and the character roster. The standby handover loads
these while it waits (opensro-ops#281); a cold boot loads them in the
same order.
================
*/
type saveFreeInputs struct {
	dataPaths     gamedata.Paths
	devPaths      enterworld.DevPaths
	authoredAreas *worldarea.Catalog
	roster        *enterworld.Roster
}

/*
================
loadSaveFreeInputs
================
*/
func loadSaveFreeInputs() (saveFreeInputs, error) {
	dataPaths, err := gamedata.Resolve()
	if err != nil {
		return saveFreeInputs{}, fmt.Errorf("game data: %w", err)
	}
	log.Infof(
		"game data: bundle=%q manifest=%s",
		dataPaths.BundleRoot,
		dataPaths.ManifestDigest,
	)
	// SRO_ALEXANDRIA (port-only, not native) needs its overlay in the data.
	if err := alexandria.Check(dataPaths.TextdataDir); err != nil {
		return saveFreeInputs{}, fmt.Errorf("game data: %w", err)
	}
	authoredAreas, err := worldarea.LoadAuthority(dataPaths.WorldAuthorityDir)
	if err != nil {
		return saveFreeInputs{}, fmt.Errorf("authored world areas: %w", err)
	}
	devPaths := enterworld.DevPathsFromEnv(
		dataPaths.CharacterAuthorityDir,
		dataPaths.TextdataDir,
	)
	devPaths.AuthoredAreas = authoredAreas
	devPaths.StructureZones = filepath.Join(dataPaths.WorldAuthorityDir, "structure-zones.json")
	characterRoster, err := enterworld.LoadRoster(devPaths.RosterPath)
	if err != nil {
		return saveFreeInputs{}, fmt.Errorf("character roster: %w", err)
	}
	return saveFreeInputs{dataPaths, devPaths, authoredAreas, characterRoster}, nil
}

// standbyBindRetry bounds the standby-health bind's retry window, and
// standbyBindPoll its pace: a transient scanner copy of this
// allocation starts with the same environment and can win the bind for
// the seconds it lives (the process guard's own history); the tracked
// process outlasts it instead of failing its boot.
const (
	standbyBindRetry = 15 * time.Second
	standbyBindPoll  = 250 * time.Millisecond
)

/*
================
startStandbyHealth

The standby-health listener, retried over standbyBindRetry: at the
deployment-assigned health port the GameWorld answers healthz and
standbyz from process start, before any public port exists.
================
*/
func startStandbyHealth(startupContext context.Context, ts *transport.Server) error {
	addr := standbyHealthAddr()
	deadline := time.Now().Add(standbyBindRetry)
	for {
		err := ts.StartStandbyHealth(addr)
		if err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("standby health listener: %w", err)
		}
		select {
		case <-startupContext.Done():
			return fmt.Errorf("standby health listener: %w", startupContext.Err())
		case <-time.After(standbyBindPoll):
		}
	}
}
