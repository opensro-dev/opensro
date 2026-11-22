/*
===========================================================================

wiring_uniquestatus.go - the Unique Status feed on this shard (#3531)

Port-only, not native (SRO_UNIQUE_STATUS). The feed
(internal/game/uniquestatus) composes from the same two owners as the
public API's uniques page - the monster state's UniqueStates and the
store's unique kill record - and a tick hook pushes its snapshots and
updates. With the flag off nothing is built, no hook installs and no
frame is sent.

===========================================================================
*/
package main

import (
	"os"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/data/store"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/uniquestatus"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
wireUniqueStatus

Builds the feed beside the plane, from the shard's own authorities (the
public API's sources in public_api.go read the same two owners).
================
*/
func wireUniqueStatus(game *gameplayPlane, authority *store.Store) {
	if !uniquestatus.Enabled(os.Getenv) {
		return
	}
	game.uniqueStatus = uniquestatus.New(uniquestatus.Sources{
		States: func() []simulation.UniqueState {
			if game.deps.MonsterState == nil {
				return nil
			}
			return game.deps.MonsterState.UniqueStates(game.divisionID)
		},
		KillsAfter: func(afterSeq int64) ([]domain.UniqueKill, error) {
			return authority.UniqueKillsAfter(game.divisionID, afterSeq)
		},
		KillPage:   store.UniqueKillPage,
		UniqueName: uniqueNameSource(game.deps.MonsterState),
	})
	log.Infof("unique status: ON (%s)", uniquestatus.EnvUniqueStatus)
}

/*
================
uniqueStatusTick

The feed's tick hook: one paced pass over the division's entered
sessions; snapshots go to the sessions that are new, updates to the
whole division.
================
*/
func (game *gameplayPlane) uniqueStatusTick(ticker *simulation.Ticker, nowMs int64) []simulation.DivisionFrames {
	views := sessionViews(ticker.Source)
	ids := make([]string, 0, len(views))
	for _, view := range views {
		if view.DivisionID != game.divisionID {
			continue
		}
		ids = append(ids, view.SessionID)
	}
	snapshots, updates := game.uniqueStatus.Tick(nowMs, ids)
	for sessionID, payload := range snapshots {
		ticker.Push.PushToSession(sessionID, []simulation.Frame{{Opcode: wire.OpUniqueStatusSnapshot, Payload: payload}})
	}
	if len(updates) > 0 {
		frames := make([]simulation.Frame, 0, len(updates))
		for _, payload := range updates {
			frames = append(frames, simulation.Frame{Opcode: wire.OpUniqueStatusUpdate, Payload: payload})
		}
		ticker.Push.PushToDivision(game.divisionID, frames, "")
	}
	return nil
}
