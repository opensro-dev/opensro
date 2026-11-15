/*
===========================================================================

wiring_contributor.go - the Contributor title on this shard (#3358)

Port-only, not native (SRO_CONTRIBUTOR_TITLES, owner's ask 2026-11-12).
The registry (internal/game/social/contributor) opens beside the chat
mute book in the shard's authority directory, the chat handler takes its
/contributor command, and a tick hook pushes the shown roster to the
division's sessions. With the flag off nothing opens, no hook installs
and no frame is sent. The website's read and pick of the title (#135)
reach the same registry through contributorTitleOf and
moveContributorTitle, which read it at call time: the flag decides each
request, and off answers not found.

===========================================================================
*/
package main

import (
	"errors"
	"os"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/agent/publicstats"
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
	// The login prompt rides the same pass: once per entry, to the
	// contributor sessions that have not answered it (contributor.Prompts).
	for _, prompt := range game.contributors.Prompts(online) {
		ticker.Push.PushToSession(prompt.SessionID, []simulation.Frame{{Opcode: contributor.OpPrompt, Payload: []byte{prompt.Kind}}})
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

/*
================
contributorTitleOf

The website's read of an account's Contributor title (port-only, not
native; #135): the registry's answer, or ok false while the title is off.
gameplay.contributors is read at call time and nothing is cached here, so
the flag decides every request. Set once at boot, before the public API's
listener starts.
================
*/
func contributorTitleOf(game *gameplayPlane, accountID string) (accounts []string, holder string, hidden bool, ok bool) {
	registry := game.contributors
	if registry == nil {
		return nil, "", false, false
	}
	return registry.TitleOf(accountID)
}

/*
================
moveContributorTitle

The website's pick of the Contributor title (port-only, not native; #135).
The registry moves the title only inside signedIn's own contributor group.
An account outside that group, or a title that is off, answers
publicstats.ErrNotOwned: the same not-found an account gets for another
account's character, so the two cannot be told apart. A save the store
refuses returns its own error, which the route answers as 503.
================
*/
func moveContributorTitle(game *gameplayPlane, signedIn, target string) error {
	registry := game.contributors
	if registry == nil {
		return publicstats.ErrNotOwned
	}
	err := registry.MoveTitle(signedIn, target)
	if errors.Is(err, contributor.ErrNotInGroup) {
		return publicstats.ErrNotOwned
	}
	return err
}
