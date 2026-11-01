/*
===========================================================================

statrecall.go - the stat point recall: every spent stat point back to the pool

ITEM_MALL_STATS_POINT_RECALL (and its _50LV.._81LV level-limited copies,
type 3/3/13/13). Neither binary carries the rule: the v1.150 client keeps
only the "Stat Withdrawal" / "Withdraw all stat points?" strings and sends
the ordinary 0x75BD use, and the v1.188 server's special-consumable switch
(case 0xC) no longer handles the type. INFERRED from the item's own
description ("Recalls all the stat points you have earned so far") and this
server's growth rule (levelup.go: per new maximum level +1 STR and +1 INT
automatically and 3 free points): STR and INT return to their automatic
values at the highest level reached and every point above them goes back to
the free pool. Nothing is invented: a character that never spent a point
recalls nothing and keeps the scroll.

===========================================================================
*/

package progression

import (
	"math"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
StatRecallUpdater

The door-free recall for the item-use owner, which calls it inside the
character transaction that consumes the scroll. It reports false, changing
nothing, when no point was spent.
================
*/
func (rt *Runtime) StatRecallUpdater() func(*enterworld.Character) ([]wire.Frame, bool) {
	return rt.recallStatPoints
}

/*
================
StatReset

What a full stat reset leaves: STR and INT at their automatic value for the
highest level reached, every point above it in the free pool.
================
*/
type StatReset struct {
	Strength, Intellect, Points int64
}

/*
================
ResetStats

The one stat-reset rule, shared by the recall scroll and the console's
reset-stats (#1609): STR = INT = BaseStatAtLevel(highest level reached),
i.e. MaxLevel + 19, and the free pool gains every point spent above it, so
at a never-spent build the pool is 3 x (MaxLevel - 1). The automatic +1
STR/+1 INT come only with a new maximum level (levelup.go), so a character
below its highest level keeps them in STR and INT. A record the reset
cannot represent is refused rather than normalised: a missing or negative
field, a level outside the wire byte, a stat below its automatic value, or
a pool wider than the stat-point word. Refusing never invents or loses a
point.
================
*/
func ResetStats(character *enterworld.Character) (StatReset, bool) {
	if character == nil || character.Level == nil || *character.Level < 1 || *character.Level > math.MaxUint8 ||
		character.Strength == nil || character.Intellect == nil {
		return StatReset{}, false
	}
	level := *character.Level
	if character.MaxLevel != nil && *character.MaxLevel > level && *character.MaxLevel <= math.MaxUint8 {
		level = *character.MaxLevel
	}
	base := BaseStatAtLevel(level)
	strength, intellect, points := *character.Strength, *character.Intellect, int64(0)
	if character.StatPoints != nil {
		points = *character.StatPoints
	}
	if strength < base || intellect < base || strength > statPointWordMax || intellect > statPointWordMax ||
		points < 0 || points > statPointWordMax {
		return StatReset{}, false
	}
	free := (strength - base) + (intellect - base) + points
	if free > statPointWordMax {
		return StatReset{}, false
	}
	return StatReset{Strength: base, Intellect: base, Points: free}, true
}

/*
================
recallStatPoints
================
*/
func (rt *Runtime) recallStatPoints(character *enterworld.Character) ([]wire.Frame, bool) {
	if character == nil {
		return nil, false
	}
	next := character.Snapshot()
	reset, ok := ResetStats(next)
	// Nothing spent (or a record the reset refuses): the scroll is kept.
	if !ok || reset.Strength == *next.Strength && reset.Intellect == *next.Intellect {
		return nil, false
	}
	strength, intellect, points := reset.Strength, reset.Intellect, reset.Points
	next.Strength, next.Intellect, next.StatPoints = &strength, &intellect, &points
	// Lower maxima never heal; an absent current pins at the old maximum first.
	if next.CurrentHP == nil {
		full := keeperOrDerived(rt, character, true)
		next.CurrentHP = &full
	}
	if next.CurrentMP == nil {
		full := keeperOrDerived(rt, character, false)
		next.CurrentMP = &full
	}
	clampGaugeToProjection(rt, next)
	display, err := rt.playerBaseStats(next)
	if err != nil {
		return nil, false
	}
	character.Strength, character.Intellect, character.StatPoints = next.Strength, next.Intellect, next.StatPoints
	character.CurrentHP, character.CurrentMP = next.CurrentHP, next.CurrentMP
	return []wire.Frame{
		{Opcode: wire.OpBaseStats, Payload: enterworld.BuildLoginStatBlock(character, display)},
		{Opcode: wire.OpPointsUpdate, Payload: wire.EncodePointsStatUpdate(uint16(points))},
		{Opcode: 0x33A6, Payload: clampedGaugePayload(character, rt)},
	}, true
}
