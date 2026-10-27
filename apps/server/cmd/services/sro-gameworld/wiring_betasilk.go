/*
===========================================================================

wiring_betasilk.go - compose the beta silk owner and publish earned silk

Port-only, not native: the optional beta clock shares the request adapter's
outer publication gate so catalog/purchase replies cannot overtake a credit.

===========================================================================
*/
package main

import (
	log "github.com/sirupsen/logrus"

	"opensro.online/server/internal/data/store"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/action"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
composeBetaSilk

SRO_BETA_SILK (port-only, not native): silk earned per in-world hour,
credited into the account's real wallet (action/betasilk.go). Returns nil
and the store itself as the mall authority when the rate is off.
SRO_BETA_SILK_ACTIVE_ONLY (port-only) narrows earning to active play; off
is the shipped rule.
================
*/
func composeBetaSilk(authorityStore *store.Store, deps *enterworld.Deps) (*action.BetaSilk, domain.MallAuthority, error) {
	rate, err := action.BetaSilkFromEnv()
	if err != nil {
		return nil, nil, err
	}
	betaSilk := action.NewBetaSilk(authorityStore, rate)
	if betaSilk == nil {
		return nil, authorityStore, nil
	}
	activeOnly, err := action.BetaSilkActiveOnlyFromEnv()
	if err != nil {
		return nil, nil, err
	}
	betaSilk.ActiveOnly = activeOnly
	if activeOnly {
		log.Infof("item mall: beta silk credits active play only (%s)", action.EnvBetaSilkActiveOnly)
	}
	deps.EntryBetaSilk = betaSilk.Starter
	log.Infof("item mall: beta silk ON (%s): %d silk per in-world hour", action.EnvBetaSilk, rate)
	return betaSilk, betaSilk, nil
}

/*
================
betaSilkTick

Capture recipients after publication admission and enqueue to their exact
scene-qualified sessions before releasing it. Tick releases its wallet locks
before returning; no store or division operation lock is held while pushing.
================
*/
func (game *gameplayPlane) betaSilkTick(ticker *simulation.Ticker, nowMs int64) []simulation.DivisionFrames {
	unlock := game.items.LockPublication(game.divisionID, "betaSilk")
	defer unlock()
	snapshot := sessionViews(ticker.Source)
	sessions := make([]simulation.SessionView, 0, len(snapshot))
	for _, session := range snapshot {
		if session.DivisionID == game.divisionID {
			sessions = append(sessions, session)
		}
	}
	// One logical clock step, including an empty population, retires absence
	// correctly. Multiple sessions of an account do not earn multiple hours.
	updates := game.betaSilk.Tick(nowMs, sessions, game.deps.CharacterByID)
	byCharacter := make(map[int64][]simulation.Frame, len(updates))
	for _, update := range updates {
		if update.DivisionID == game.divisionID && update.OnlyCharacterID != 0 {
			byCharacter[update.OnlyCharacterID] = update.Frames
		}
	}
	for _, session := range sessions {
		if frames := byCharacter[session.CharacterID]; len(frames) != 0 {
			ticker.Push.PushToSession(session.SessionID, frames)
		}
	}
	// Generic hook routing would resnapshot and enqueue outside the gate.
	return nil
}
