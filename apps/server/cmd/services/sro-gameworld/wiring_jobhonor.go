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
	"bytes"
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

// honorBuffSweepMs is how often the tier buff convergence re-checks a
// session whose day and dress have not changed, so a tier or camp rank
// written behind the session's back still lands.
const honorBuffSweepMs = 15_000

// honorBuffKey is what a session's tier honor buffs were last
// reconciled against: the honor day, the job it is dressed as and the
// sweep step. A dress change or the 05:00 change moves the key at
// once; the step re-checks on the sweep.
type honorBuffKey struct {
	day   int64
	job   uint8
	sweep int64
}

/*
================
honorBuffTick

Converges every entered session's Academy honor buffs onto the tier it
holds (#3530): the first sight of a session, a dress change and the
05:00 tier change reconcile at once, and the sweep re-checks the rest.
The reconciler (SyncTrainingCampHonorBuffs) folds the job tier into
the camp's native rank, strongest wins, so this pass passes the
camp's own rank (CampHonorRank) and can only ever raise a master's
want to its tier, never strip the camp's grant. Sessions the tick
stops seeing are forgotten.
================
*/
func (game *gameplayPlane) honorBuffTick(ticker *simulation.Ticker, nowMs int64) []simulation.DivisionFrames {
	now := time.UnixMilli(nowMs)
	base := honorBuffKey{day: jobhonor.DayStart(now).Unix(), sweep: nowMs / honorBuffSweepMs}
	if game.honorBuffSeen == nil {
		game.honorBuffSeen = map[string]honorBuffKey{}
	}
	seen := map[string]bool{}
	for _, view := range sessionViews(ticker.Source) {
		if view.DivisionID != game.divisionID {
			continue
		}
		seen[view.SessionID] = true
		key := base
		if c := honorCharacter(game, view.CharacterID); c != nil {
			key.job = enterworld.DressedJob(c)
		}
		if game.honorBuffSeen[view.SessionID] == key {
			continue
		}
		game.honorBuffSeen[view.SessionID] = key
		game.items.SyncTrainingCampHonorBuffs(game.divisionID, view.CharacterID,
			game.items.CampHonorRank(game.divisionID, view.CharacterID))
	}
	for sessionID := range game.honorBuffSeen {
		if !seen[sessionID] {
			delete(game.honorBuffSeen, sessionID)
		}
	}
	return nil
}

// jobHonorTierSweepMs is how often the tier roster is rebuilt; a
// dress change or the 05:00 change shows within one sweep.
const jobHonorTierSweepMs = 5_000

/*
================
jobHonorTierTick

The coloured titles' roster pass (#3530): every entered character of
the division holding a tier for the job it is dressed as, pushed to
the division when the bytes change. The tiers are the newest
snapshot's (the ledger's Previous) and the dress the live record's,
so the roster shows the same grant the honor buff folds
(SyncTrainingCampHonorBuffs): a title is worn in the suit whose tier
earned it.

A session that enters after the last change never saw the division
push, so the pass keeps the set of sessions the standing bytes have
reached - the contributor roster's reach rule (contributorRosterTick):
on a change the division push is the delivery and the set restarts
as the sessions it reached; between changes, each new arrival gets
the standing roster on its own at the next sweep.
================
*/
func (game *gameplayPlane) jobHonorTierTick(ticker *simulation.Ticker, nowMs int64) []simulation.DivisionFrames {
	if nowMs < game.jobHonorTierNext {
		return nil
	}
	game.jobHonorTierNext = nowMs + jobHonorTierSweepMs
	held, _, err := game.jobHonor.Previous()
	if err != nil {
		return nil
	}
	tiers := make(map[[2]int64]int, len(held))
	for _, row := range held {
		if row.Tier != jobhonor.TierNone {
			tiers[[2]int64{row.CharacterID, int64(row.Job)}] = row.Tier
		}
	}
	var entries []wire.JobHonorTierEntry
	online := map[string]bool{}
	for _, view := range sessionViews(ticker.Source) {
		if view.DivisionID != game.divisionID {
			continue
		}
		online[view.SessionID] = true
		c := game.deps.CharacterByID(game.divisionID, view.CharacterID)
		if c == nil {
			continue
		}
		if job := enterworld.DressedJob(c); job != 0 {
			if tier := tiers[[2]int64{c.ID, int64(job)}]; tier != jobhonor.TierNone {
				entries = append(entries, wire.JobHonorTierEntry{Name: c.Name, Tier: tier})
			}
		}
	}
	payload := wire.EncodeJobHonorTierRoster(entries)
	frames := []simulation.Frame{{Opcode: wire.OpJobHonorTierRoster, Payload: payload}}
	if !bytes.Equal(payload, game.jobHonorTierBytes) {
		game.jobHonorTierBytes = payload
		ticker.Push.PushToDivision(game.divisionID, frames, "")
		game.jobHonorTierReached = online
		return nil
	}
	if game.jobHonorTierReached == nil {
		game.jobHonorTierReached = map[string]bool{}
	}
	for sessionID := range online {
		if !game.jobHonorTierReached[sessionID] {
			ticker.Push.PushToSession(sessionID, frames)
			game.jobHonorTierReached[sessionID] = true
		}
	}
	for sessionID := range game.jobHonorTierReached {
		if !online[sessionID] {
			delete(game.jobHonorTierReached, sessionID)
		}
	}
	return nil
}
