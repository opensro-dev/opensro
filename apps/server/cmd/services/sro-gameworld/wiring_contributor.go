/*
===========================================================================

wiring_contributor.go - the Contributor title on this shard (#3358)

Port-only, not native (SRO_CONTRIBUTOR_TITLES, owner's ask 2026-11-12).
The registry (internal/game/social/contributor) opens beside the chat
mute book in the shard's authority directory, the chat handler takes its
/contributor command, and a tick hook pushes the shown roster to the
division's sessions. With the flag off nothing opens, no hook installs
and no frame is sent.

===========================================================================
*/
package main

import (
	"os"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/data/store"
	"opensro.online/server/internal/game/social/contributor"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
wireContributorTitles

Called from register once the chat runtime exists. A corrupt opt-out
file refuses the boot (contributor.Open).
================
*/
func wireContributorTitles(game *gameplayPlane) error {
	if !contributor.Enabled(os.Getenv) {
		return nil
	}
	dir := store.DirForShardFromEnv(game.divisionID)
	members, err := contributor.MembersPath(os.Getenv, dir)
	if err != nil {
		return err
	}
	registry, err := contributor.Open(dir, members, store.WriteFileAtomic)
	if err != nil {
		return err
	}
	game.contributors = registry
	game.chat.Contributor = registry.Command
	log.Infof("contributor titles: ON (%s), members from %s", contributor.EnvContributorTitles, members)
	return nil
}

/*
================
contributorRosterTick

The roster pass: the division's entered sessions with their account and
character name, then the registry's answer pushed to everyone or to the
new arrivals only. CharacterByID reads the published lock-free index,
as the beta silk pass does.
================
*/
func (game *gameplayPlane) contributorRosterTick(ticker *simulation.Ticker, nowMs int64) []simulation.DivisionFrames {
	views := sessionViews(ticker.Source)
	online := make([]contributor.Online, 0, len(views))
	for _, view := range views {
		if view.DivisionID != game.divisionID {
			continue
		}
		c := game.deps.CharacterByID(game.divisionID, view.CharacterID)
		if c == nil {
			continue
		}
		online = append(online, contributor.Online{SessionID: view.SessionID, AccountID: c.AccountID, Name: c.Name})
	}
	payload, targets, all := game.contributors.Push(online, nowMs)
	if payload == nil {
		return nil
	}
	frames := []simulation.Frame{{Opcode: contributor.OpRoster, Payload: payload}}
	if all {
		ticker.Push.PushToDivision(game.divisionID, frames, "")
		return nil
	}
	for _, sessionID := range targets {
		ticker.Push.PushToSession(sessionID, frames)
	}
	return nil
}
