/*
===========================================================================

styria_reward.go - Styria Clash match rewards: who is paid, and the paying

Slice 4 of epic #3648. When the match clock decides the outcome
(styria_match.go), every participant still standing in the match is
owed the later client's GROUP_ARENA_JOB_WAR rewards (the table
itself lives in world/styria): job EXP by outcome, the level-banded
skill exp whatever the outcome, and the outcome's stones - sealed
rune seals on a win only. The later client delivers the items with
its Styria letters (UIIT_STYRIA_SERVER_WIN/DRAW/LOSE_REWARD), but
the v1.150 mailbox carries text only, so the items ride the
operator grant lane like the flag war's trophy (flagwar_match.go)
and the exps are granted on the spot; the letter texts and the
result window are the client slice's to draw. The achievement line
(UIIT_STT_ACHIEVE_TYPE_19_WIN_STYRIA) has no system to hook in this
port and stays unimplemented, as the epic's slice survey recorded.

Paying runs off the match lock, from the match tick's drain, in the
flag war's shape: winners are collected under the match lock when
the outcome lands and paid here, with no lane held - the grant lane
takes the division lock itself. A failed grant is logged and not
retried (the flag war's rule). The reward items are not in the
tree's item data yet (the data import is the next slice); until a
codename resolves, that item is skipped with one log line per
match, and the exps pay regardless.

===========================================================================
*/

package action

import (
	log "github.com/sirupsen/logrus"

	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/styria"
)

// One participant's result, read off the match outcome and their
// side. The reward table is keyed by it.
const (
	styriaResultWin = iota
	styriaResultLose
	styriaResultDraw
)

/*
================
styriaResultFor

One side's result in a decided match: a draw draws for everyone,
else the outcome's side wins and the other loses. An undecided
match (styriaOutcomeOpen) reads as a draw - the caller never asks
before the clock, so it cannot pay early by accident.
================
*/
func styriaResultFor(outcome, side int) int {
	if outcome == styriaOutcomeDraw || outcome == styriaOutcomeOpen {
		return styriaResultDraw
	}
	if (outcome == styriaOutcomeHunters && side == styriaSideHunters) ||
		(outcome == styriaOutcomeThieves && side == styriaSideThieves) {
		return styriaResultWin
	}
	return styriaResultLose
}

/*
================
styriaRewardFor

One participant's reward: the job EXP for their result, the
level-banded skill exp (styria.RewardSkillExp - the same whatever
the result), and the result's item grants. The arena coin is
never among them: the config group pays none (world/styria).
================
*/
func styriaRewardFor(result int, level int) (jobExp, skillExp int64, items []inventory.ItemAmount) {
	skillExp = styria.RewardSkillExp(level)
	stones := int64(styria.RewardStonesDraw)
	switch result {
	case styriaResultWin:
		jobExp, stones = styria.RewardJobExpWin, styria.RewardStonesWin
		items = append(items,
			inventory.ItemAmount{Codename: styria.MagicRuneSealCodename, Count: styria.RewardRuneSealsWin},
			inventory.ItemAmount{Codename: styria.StrengthRuneSealCodename, Count: styria.RewardRuneSealsWin})
	case styriaResultLose:
		jobExp, stones = styria.RewardJobExpLose, styria.RewardStonesLose
	default:
		jobExp = styria.RewardJobExpDraw
	}
	return jobExp, skillExp, append(items,
		inventory.ItemAmount{Codename: styria.BlueStoneCodename, Count: uint32(stones)},
		inventory.ItemAmount{Codename: styria.BlackStoneCodename, Count: uint32(stones)})
}

/*
================
styriaDue

One entered participant owed a reward when the clock decided:
their name routes the grant, their side reads their result.
Collected under the match lock; paid by the drain.
================
*/
type styriaDue struct {
	name string
	side int
}

/*
================
styriaPayReward

Pay one due, with no lane held: the character's exps through the
progression updaters, their items through the operator grant
lane. A participant who left between the clock and the drain is
skipped; a failed grant is logged, never retried.
================
*/
func (rt *Runtime) styriaPayReward(match *styriaMatch, due styriaDue) {
	c := rt.findCharacter(match.division, due.name)
	if c == nil {
		log.WithField("character", due.name).Warn("styria: reward skipped, the participant is gone")
		return
	}
	jobExp, skillExp, items := styriaRewardFor(styriaResultFor(match.outcome, due.side), int(rewardLevel(c)))

	var frames []wire.Frame
	if rt.UpdateExperience != nil && skillExp > 0 {
		if granted, ok := rt.UpdateExperience(c, 0, skillExp, 0); ok {
			frames = append(frames, granted...)
		}
	}
	if granted, ok := rt.addJobExperience(match.division, c, jobExp); ok {
		frames = append(frames, granted...)
	}
	if len(frames) > 0 && rt.PushCharacterFrames != nil {
		rt.PushCharacterFrames(match.division, c.Name, frames)
	}

	grantable, missing := rt.styriaGrantableItems(items)
	if len(missing) > 0 {
		log.WithField("items", missing).Warn("styria: reward items are not in the item data yet; skipped")
	}
	if len(grantable) > 0 {
		if err := rt.OperatorGrantItems(match.division, due.name, grantable); err != nil {
			log.WithError(err).WithField("character", due.name).Warn("styria: reward item grant failed")
		}
	}
}

/*
================
styriaGrantableItems

The grants whose codename the tree's item data already knows.
The reward items arrive with their own data import; until then
they are reported missing, not granted into a refused batch that
would also sink the stones beside them once some rows land.
================
*/
func (rt *Runtime) styriaGrantableItems(items []inventory.ItemAmount) (grantable []inventory.ItemAmount, missing []string) {
	for _, item := range items {
		if refs := rt.deps.ItemReferences(); refs != nil {
			if _, ok := refs.ItemRefByCodename(item.Codename); ok {
				grantable = append(grantable, item)
				continue
			}
		}
		missing = append(missing, item.Codename)
	}
	return grantable, missing
}
