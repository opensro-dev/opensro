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
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/honorfeed"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
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
	gameplay.honorFeed = honorfeed.New(honorfeed.Sources{
		Current:  service.Current,
		Previous: service.Previous,
		Points:   service.CharacterPoints,
		Used:     service.Used,
		Now:      time.Now,
		Alias: func(characterID int64) (string, bool) {
			c := honorCharacter(gameplay, characterID)
			if c == nil || c.PublicHidden || c.Job.Alias == "" {
				return "", false
			}
			return c.Job.Alias, true
		},
		Account: func(characterID int64) (string, bool) {
			c := honorCharacter(gameplay, characterID)
			if c == nil || c.AccountID == "" {
				return "", false
			}
			return c.AccountID, true
		},
	})
	log.Infof("job honor: trade sales score honor points (%s, port-only, not native); ledger in %s",
		action.EnvJobHonor, filepath.Join(dir, jobhonor.FileName))
	return nil
}

/*
================
honorCharacter

The division's live record of a character, for the feed's alias and
account joins (the public board joins its own roster the same way).
================
*/
func honorCharacter(game *gameplayPlane, characterID int64) *enterworld.Character {
	for _, c := range game.deps.CharactersForDivision(game.divisionID) {
		if c != nil && c.ID == characterID {
			return c
		}
	}
	return nil
}

/*
================
jobHonorTick

The ranking feed's tick hook: one paced pass over the division's
entered sessions; a snapshot goes to each session whose bytes changed.
================
*/
func (game *gameplayPlane) jobHonorTick(ticker *simulation.Ticker, nowMs int64) []simulation.DivisionFrames {
	views := sessionViews(ticker.Source)
	sessions := make([]honorfeed.Session, 0, len(views))
	for _, view := range views {
		if view.DivisionID != game.divisionID {
			continue
		}
		sessions = append(sessions, honorfeed.Session{ID: view.SessionID, CharacterID: view.CharacterID})
	}
	for sessionID, payload := range game.honorFeed.Tick(nowMs, sessions) {
		ticker.Push.PushToSession(sessionID, []simulation.Frame{{Opcode: wire.OpJobHonorSnapshot, Payload: payload}})
	}
	return nil
}

/*
================
hotRouteTick

The hot route's announcement (#3530): the first time the tick sees a
session in an hour - and the first after the hour turns - that session
gets the hour's route as one server notice (0x3667, the line the chat
log shows). The scorer multiplies by the same selector
(action.HotRouteNames / hotRouteAt), so the line never promises what
the scorer does not pay. Sessions the tick stops seeing are forgotten.
================
*/
func (game *gameplayPlane) hotRouteTick(ticker *simulation.Ticker, nowMs int64) []simulation.DivisionFrames {
	at := time.UnixMilli(nowMs).UTC()
	hour := at.Unix() / 3600
	if game.hotRouteSeen == nil {
		game.hotRouteSeen = map[string]int64{}
	}
	line := ""
	seen := map[string]bool{}
	for _, view := range sessionViews(ticker.Source) {
		if view.DivisionID != game.divisionID {
			continue
		}
		seen[view.SessionID] = true
		if game.hotRouteSeen[view.SessionID] == hour {
			continue
		}
		game.hotRouteSeen[view.SessionID] = hour
		if line == "" {
			from, to := action.HotRouteNames(at)
			line = "Hot route this hour: " + from + " to " + to + " - job honor on this route pays x1.5."
		}
		frame := wire.NotificationFrame(line)
		ticker.Push.PushToSession(view.SessionID, []simulation.Frame{{Opcode: frame.Opcode, Payload: frame.Payload}})
	}
	for sessionID := range game.hotRouteSeen {
		if !seen[sessionID] {
			delete(game.hotRouteSeen, sessionID)
		}
	}
	return nil
}
