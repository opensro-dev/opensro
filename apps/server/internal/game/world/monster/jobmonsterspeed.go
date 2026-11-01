/*
===========================================================================

jobmonsterspeed.go - faster thief and hunter monsters (port-only, not native)

The job system's monsters (the reference server's thief- and
hunter-monster predicates) walk and run at their characterdata speeds
natively. The owner
approved the balance advisor's 20% more as a server setting (2026-10-15).
SRO_JOB_MONSTER_SPEED "on" scales those references' walk and run speed by
jobMonsterSpeedFactor once, when the population template is built, so the
movers, the chase timing and the spawn rows' speeds (which the client
moves them by) all read the same value. Unset or "off" is native.

===========================================================================
*/
package monster

import (
	"os"
	"strings"
)

// EnvJobMonsterSpeed turns the faster job monsters on ("on", "1", "true").
const EnvJobMonsterSpeed = "SRO_JOB_MONSTER_SPEED"

// jobMonsterSpeedFactor is the advisor's 20%.
const jobMonsterSpeedFactor = 1.2

/*
================
JobMonsterSpeedFromEnv
================
*/
func JobMonsterSpeedFromEnv() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvJobMonsterSpeed))) {
	case "on", "1", "true":
		return true
	}
	return false
}

/*
================
Template.WithJobMonsterSpeed

A template whose thief and hunter monster references walk and run
jobMonsterSpeedFactor times as fast, and how many it scaled. The receiver's
reference map is left as it was.
================
*/
func (t Template) WithJobMonsterSpeed() (Template, int) {
	refs := make(map[uint32]MonsterRef, len(t.Refs))
	scaled := 0
	for id, ref := range t.Refs {
		if ref.TradeAppearance() {
			ref.WalkSpeed = float64(float32(ref.WalkSpeed * jobMonsterSpeedFactor))
			ref.RunSpeed = float64(float32(ref.RunSpeed * jobMonsterSpeedFactor))
			scaled++
		}
		refs[id] = ref
	}
	t.Refs = refs
	return t, scaled
}
