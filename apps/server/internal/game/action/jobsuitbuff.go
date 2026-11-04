/*
===========================================================================

jobsuitbuff.go - a worn job suit's bonus, tiered by job level

Port-only, not native (owner decision 2026-11-04, #2475; the mapping was
settled by the agent vote on #2476). With SRO_JOB_SUIT_TIERS on, a
character wearing a job suit of its own job carries a tier from its job
level (grade): tier 1 at grade 1, then 2, 3, 5 and 7. A tier adds flat
STR and INT (1, 5, 10, 15, 20) and 1% movement speed per tier. The suits
themselves keep their native wear gates (jobdress.go: character level
and the suit's own job grade), so the higher-degree suits stay an earned
visual ladder; the tier lives on the job, not on the item, which is why
China climbs the same five tiers as Europe on its fewer suit models.

The bonus is never stored. Like the wanted streak's (mostwanted.go), it
is parameter writes in the stat projection (statModifierWrites) plus a
movement factor (refreshMovementEffects), both read from the live
character, so taking the suit off strips it in the same admission that
moves the item, and a job level gained while worn re-tiers it at the
next projection (addJobExperience also re-sends the stat block and the
speeds when a grant crosses a tier boundary).

The 7 days are display only: the buff exists exactly while the suit is
worn, so the server holds no timer for it. The buff-board row (built on
#1849's wanted-state display) counts 7 days from the wear it renders,
renewed each time the suit goes back on.

Off, the default here, is native: no writes, no factor, no frames. The
GameWorld's Nomad job turns it on for the live shard.

===========================================================================
*/
package action

import (
	"os"
	"strings"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/paramkeeper"
)

// EnvJobSuitTiers turns the rule on ("on", "1", "true").
const EnvJobSuitTiers = "SRO_JOB_SUIT_TIERS"

const (
	// jobSuitBuffModifierSource keys the suit tier's writes apart from
	// the engaged skill's (0x7fffff00), the wanted streak's (0x7fffff01)
	// and every installed effect (0x80000000 and up).
	jobSuitBuffModifierSource uint32 = 0x7fffff02
	// The STR (1) and INT (2) keepers, as the wanted streak's writes.
	jobSuitStrengthParameter  uint16 = 1
	jobSuitIntellectParameter uint16 = 2
	// JobSuitBuffDays is the displayed life of one wear; display only,
	// the worn gate is the real bound.
	JobSuitBuffDays = 7
)

// jobSuitTierGrades[i] is the job level tier i+1 needs (vote #2476:
// 1/2/3/5/7, keeping the native suit-group grades 1/3/5/7 on tier
// boundaries, inside the grade cap of 7).
var jobSuitTierGrades = [5]int64{1, 2, 3, 5, 7}

// jobSuitTierStats[i] is the flat STR and INT tier i+1 adds (owner
// #2475: 1, 5, 10, 15, 20); the movement bonus is 1% per tier.
var jobSuitTierStats = [5]float32{1, 5, 10, 15, 20}

/*
================
JobSuitTiersFromEnv
================
*/
func JobSuitTiersFromEnv() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvJobSuitTiers))) {
	case "on", "1", "true":
		return true
	}
	return false
}

/*
================
jobSuitTierForGrade

The tier a job level earns: the highest tier whose grade it has reached,
or 0 below tier 1.
================
*/
func jobSuitTierForGrade(grade int64) int {
	tier := 0
	for i, need := range jobSuitTierGrades {
		if grade >= need {
			tier = i + 1
		}
	}
	return tier
}

/*
================
Runtime.jobSuitTier

The character's live suit tier, or 0 with the rule off, without a suit,
or in a suit of another job (wear refuses that natively; the assert
keeps a saved outfit from leaking a tier if that ever changes). The tier
reads the worn suit's own job level: trader reads trader, never "any
job level".
================
*/
func (rt *Runtime) jobSuitTier(c *enterworld.Character) int {
	if !rt.JobSuitTiers || c == nil {
		return 0
	}
	dressed := enterworld.DressedJob(c)
	if dressed == 0 || dressed != c.Job.Type {
		return 0
	}
	return jobSuitTierForGrade(int64(c.Job.Grade))
}

/*
================
Runtime.jobSuitWrites

The tier's STR and INT writes, nil without a tier.
================
*/
func (rt *Runtime) jobSuitWrites(c *enterworld.Character) []paramkeeper.Write {
	tier := rt.jobSuitTier(c)
	if tier == 0 {
		return nil
	}
	value := jobSuitTierStats[tier-1]
	return []paramkeeper.Write{
		{Parameter: jobSuitStrengthParameter, Channel: paramkeeper.Flat, Source: jobSuitBuffModifierSource, Value: value},
		{Parameter: jobSuitIntellectParameter, Channel: paramkeeper.Flat, Source: jobSuitBuffModifierSource, Value: value},
	}
}

/*
================
Runtime.jobSuitSpeedFactor

The tier's movement factor on foot: +1% per tier.
================
*/
func (rt *Runtime) jobSuitSpeedFactor(c *enterworld.Character) float32 {
	if tier := rt.jobSuitTier(c); tier != 0 {
		return 1 + float32(tier)/100
	}
	return 1
}

/*
================
Runtime.jobSuitBuffRefresh

The stat block and movement speeds after a suit tier changed: mirrored
on mostWantedRefresh, private goes to the wearer, public to its
observers.
================
*/
func (rt *Runtime) jobSuitBuffRefresh(division string, c *enterworld.Character, now int64) (private, public []wire.Frame) {
	if stats, err := rt.PlayerBaseStats(division, c); err == nil {
		private = append(private, wire.Frame{Opcode: wire.OpBaseStats, Payload: stats.Encode()})
	} else {
		log.WithError(err).WithField("character", c.Name).Error("job suit tiers: stat projection failed")
	}
	return private, rt.refreshMovementEffects(division, c, now)
}
