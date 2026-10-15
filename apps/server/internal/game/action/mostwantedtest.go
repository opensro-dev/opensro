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
)

// EnvMostWantedTest turns the test pacing on ("on", "1", "true").
const EnvMostWantedTest = "SRO_MOST_WANTED_TEST"

const (
	// mostWantedTestStreakStep is what one counted kill adds under the test
	// pacing: two, so streak / 10 reaches a level every 5 kills.
	mostWantedTestStreakStep = 2
	// mostWantedTestBountyPerLevel is the test bounty a wanted level is worth.
	mostWantedTestBountyPerLevel = 1_000_000
)

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
	if rt.MostWantedTest {
		return mostWantedTestStreakStep
	}
	return 1
}

/*
================
mostWantedBountyGold

The bounty a victim of the given wanted level is worth.
================
*/
func (rt *Runtime) mostWantedBountyGold(level int64) uint64 {
	if rt.MostWantedTest {
		return uint64(level) * mostWantedTestBountyPerLevel
	}
	return uint64(level) * mostWantedBountyPerLevel
}
