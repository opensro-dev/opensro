/*
===========================================================================

mostwantedtest.go - a fast, cheap Most Wanted for testing

Port-only, not native, temporary (owner decision 2026-10-15, at the
balance advisor's request for testing). SRO_MOST_WANTED_TEST changes the
port-only Most Wanted rule (mostwanted.go) while it is on:

  - a wanted level takes 5 counted kills, not 10, so level 10 is reached
    at 50: each counted kill adds two to the streak, and the level stays
    streak / 10 everywhere it is read;
  - the anti-farm window is off: the same victim counts every time;
  - the bounty is 1,000,000 gold per level instead of 100,000,000, so
    farming the rule moves no meaningful gold.

It does nothing unless SRO_MOST_WANTED is on too. A streak saved while it
is on keeps its doubled count; death or taking the suit off clears it as
always. Off, the default here, is the ordinary rule; the GameWorld's Nomad
job turns it on for the test window.

===========================================================================
*/
package action

import (
	"os"
	"strings"

	"opensro.online/server/internal/game/tunables"
)

// EnvMostWantedTest turns the test pacing on ("on", "1", "true").
const EnvMostWantedTest = "SRO_MOST_WANTED_TEST"

// The test pacing's kills per level and bounty per level are tunable
// (game/tunables: SRO_MOST_WANTED_TEST_KILLS, 5 by default and a divisor
// of 10; SRO_MOST_WANTED_TEST_BOUNTY, 1,000,000 by default).

/*
================
MostWantedTestStreakStepFromEnv

What one counted kill adds under the test pacing: 10 / kills per level,
so the level stays streak / 10 everywhere it is read. Read once at boot
(restart class): a saved streak keeps the count it earned.
================
*/
func MostWantedTestStreakStepFromEnv() (int64, error) {
	v, err := tunables.FromEnv(tunables.MostWantedTestKills)
	if err != nil {
		return 0, err
	}
	return mostWantedKillsPerLevel / int64(v.Number), nil
}

/*
================
MostWantedTestFromEnv
================
*/
func MostWantedTestFromEnv() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvMostWantedTest))) {
	case "on", "1", "true":
		return true
	}
	return false
}

/*
================
mostWantedStreakStep

What one counted kill adds to a streak.
================
*/
func (rt *Runtime) mostWantedStreakStep() int64 {
	if !rt.MostWantedTest {
		return 1
	}
	if rt.MostWantedTestStreakStep > 0 {
		return rt.MostWantedTestStreakStep
	}
	// A Runtime built without the boot wiring (tools, tests) takes the
	// registry's kills per level.
	return mostWantedKillsPerLevel / tunables.Current().Int(tunables.MostWantedTestKills)
}

/*
================
mostWantedBountyGold

The bounty a victim of the given wanted level is worth.
================
*/
func (rt *Runtime) mostWantedBountyGold(level int64) uint64 {
	if rt.MostWantedTest {
		return uint64(level) * uint64(tunables.Current().Int(tunables.MostWantedTestBounty))
	}
	return uint64(level) * mostWantedBountyPerLevel
}
