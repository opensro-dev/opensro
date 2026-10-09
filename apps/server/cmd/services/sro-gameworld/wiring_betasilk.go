/*
===========================================================================

wiring_betasilk.go - publish earned silk inside the mall publication gate

Port-only, not native: the optional beta clock shares the request adapter's
outer publication gate so catalog/purchase replies cannot overtake a credit.

===========================================================================
*/
package main

import "opensro.online/server/internal/game/world/simulation"

/*
================
betaSilkTick

Capture recipients after publication admission and enqueue to their exact
scene-qualified sessions before releasing it. Tick releases its wallet locks
before returning; no store or division operation lock is held while pushing.
================
*/
func (game *gameplayPlane) betaSilkTick(ticker *simulation.Ticker, nowMs int64) []simulation.DivisionFrames {
	unlock := game.items.LockPublication(game.divisionID)
	defer unlock()
	snapshot := ticker.Source.SnapshotSessions()
	sessions := make([]simulation.SessionSnapshot, 0, len(snapshot))
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
