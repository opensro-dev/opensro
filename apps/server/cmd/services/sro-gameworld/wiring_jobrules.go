/*
===========================================================================

wiring_jobrules.go - the port-only job rules a GameWorld boot turns on

The trader, thief and hunter rules that are not native (Most Wanted and
its test pacing, the suit tiers, the town-spawn reload and the suit
looks) each read their SRO_* switch here once, at boot. Every switch's
unset value is native; the log names the ones a shard turned on.

===========================================================================
*/
package main

import (
	"opensro.online/server/internal/game/action"
	"opensro.online/server/internal/game/tunables"

	log "github.com/sirupsen/logrus"
)

/*
================
wireJobRules

Reads the job rules' switches into the action runtime and the world-entry
dependencies.
================
*/
func (game *gameplayPlane) wireJobRules() error {
	// Port-only, not native: the job-kill streak, its bonuses and bounty.
	game.items.MostWanted = action.MostWantedFromEnv()
	if game.items.MostWanted {
		log.Infof("job: most wanted streaks, bonuses and bounties ON (%s)", action.EnvMostWanted)
	}
	// Port-only, not native: a worn job suit's bonus tiered by job level.
	game.items.JobSuitTiers = action.JobSuitTiersFromEnv()
	if game.items.JobSuitTiers {
		log.Infof("job: suit tiers by job level ON (%s)", action.EnvJobSuitTiers)
	}
	// Port-only, not native: a finished dress reloads at the town spawn.
	game.items.JobDressTownSpawn = action.JobDressTownSpawnFromEnv()
	if game.items.JobDressTownSpawn {
		log.Infof("job: a finished dress reloads at the nearest town spawn ON (%s)", action.EnvJobDressTownSpawn)
	}
	// Port-only, not native, temporary: Most Wanted's test pacing.
	game.items.MostWantedTest = action.MostWantedTestFromEnv()
	step, err := action.MostWantedTestStreakStepFromEnv()
	if err != nil {
		return err
	}
	game.items.MostWantedTestStreakStep = step
	if game.items.MostWantedTest {
		log.Infof("job: most wanted test pacing ON: %d kills a level, no anti-farm window, %d gold bounty a level (%s)",
			10/step, tunables.Current().Int(tunables.MostWantedTestBounty), action.EnvMostWantedTest)
	}
	// Port-only, not native: the bounty comes out of the victim's gold.
	game.items.MostWantedBountyTransfer = action.MostWantedBountyTransferFromEnv()
	if game.items.MostWantedBountyTransfer {
		log.Infof("job: most wanted bounties paid from the victim's gold (%s)", action.EnvMostWantedBountyTransfer)
	}
	// Port-only, not native: the killer hears its progress per counted kill.
	game.items.MostWantedStreakNotice = action.MostWantedStreakNoticeFromEnv()
	if game.items.MostWantedStreakNotice {
		log.Infof("job: most wanted progress notice per counted kill ON (%s)", action.EnvMostWantedStreakNotice)
	}
	// Port-only, not native: a companion's job kill counts for its owner.
	game.items.MostWantedCOSKills = action.MostWantedCOSKillsFromEnv()
	if game.items.MostWantedCOSKills {
		log.Infof("job: most wanted counts companion-delivered job kills ON (%s)", action.EnvMostWantedCOSKills)
	}
	// Port-only, not native: trader and hunter suits hide worn armour
	// (presentation, published to the client).
	game.deps.JobSuitHidesArmor = action.JobSuitHidesArmorFromEnv()
	if game.deps.JobSuitHidesArmor {
		log.Infof("job: trader and hunter suits hide worn armour (%s)", action.EnvJobSuitHidesArmor)
	}
	// Port-only, not native: level-20 thief suits wear the Moonlit Ash look
	// (presentation, published to the client).
	game.deps.MoonlitThiefSuit = action.MoonlitThiefSuitFromEnv()
	if game.deps.MoonlitThiefSuit {
		log.Infof("job: level-20 thief suits wear Moonlit Ash (%s)", action.EnvMoonlitThiefSuit)
	}
	return nil
}
