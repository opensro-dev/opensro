/*
===========================================================================

wiring_mastery.go - the total mastery budget's composition

Reads the two port-only budget switches once at boot (not native; the
native totals are progression's totalMasteryCap): SRO_BETA_MASTERY gives
every character the beta's 5,000 total, SRO_BETA_MASTERY_EARLY gives it
only to characters created before the native totals went live
(enterworld/betamastery_early.go owns that rule). The world-entry deps
and the progression runtime receive the same two values, so the skill
window and the raise check cannot disagree.

===========================================================================
*/
package main

import (
	"time"

	log "github.com/sirupsen/logrus"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/progression"
)

/*
================
readMasteryAllowance

Both switches into the world-entry deps; an invalid value refuses boot.
================
*/
func readMasteryAllowance(deps *enterworld.Deps) error {
	override, err := progression.BetaMasteryFromEnv()
	if err != nil {
		return err
	}
	deps.MasteryTotalOverride = override
	// Owner, 2026-11-18, #3551: early beta characters keep the budget.
	deps.BetaMasteryEarly, err = enterworld.BetaMasteryEarlyFromEnv()
	return err
}

/*
================
applyMasteryAllowance

The same two values into the progression runtime, with the boot log line
that names the budget in force.
================
*/
func applyMasteryAllowance(stats *progression.Runtime, deps *enterworld.Deps) {
	stats.MasteryTotalOverride = deps.MasteryTotalOverride
	stats.BetaMasteryEarly = deps.BetaMasteryEarly
	switch {
	case stats.MasteryTotalOverride != 0:
		log.Infof("progression: beta total mastery allowance %d for both races (%s)",
			stats.MasteryTotalOverride, progression.EnvBetaMastery)
	case stats.BetaMasteryEarly:
		log.Infof("progression: characters created before %s keep the beta mastery allowance %d (%s)",
			enterworld.EarlyBetaCutoff.Format(time.RFC3339), enterworld.EarlyBetaMasteryCap, enterworld.EnvBetaMasteryEarly)
	}
}
