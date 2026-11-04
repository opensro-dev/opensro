/*
===========================================================================

jobmonsteraggression.go - job monsters strike at once and hit harder

Port-only, not native (#2474, owner request 2026-11-04). The thief and
hunter monsters that ambush trade runs natively hold the ordinary 3 s
spawn hold before their first sight scan and hit for their authored
damage. With SRO_JOB_MONSTER_AGGRESSION on, a job monster's spawn hold
is skipped (simulation/caravanbandit.go arms its mover at the spawn
instant, so the first AI leg can acquire and attack) and its outgoing
damage factors double (combat's MonsterInstanceStats), so every hit it
lands - on a player or a transport - deals twice the native damage.
Unset or "off" is native on both points.

===========================================================================
*/
package monster

import (
	"os"
	"strings"
)

// EnvJobMonsterAggression turns the aggressive job monsters on ("on", "1", "true").
const EnvJobMonsterAggression = "SRO_JOB_MONSTER_AGGRESSION"

// jobMonsterDamageFactor is the owner's doubled damage (#2474).
const jobMonsterDamageFactor = 2

/*
================
JobMonsterAggressionFromEnv
================
*/
func JobMonsterAggressionFromEnv() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvJobMonsterAggression))) {
	case "on", "1", "true":
		return true
	}
	return false
}

/*
================
JobMonsterDamageFactor

The outgoing-damage multiplier the rule gives a job monster: doubled
under the rule, native (1) otherwise. Ordinary monsters always get 1.
================
*/
func (i Instance) JobMonsterDamageFactor() float32 {
	if JobMonsterAggressionFromEnv() && i.JobMonster() {
		return jobMonsterDamageFactor
	}
	return 1
}
