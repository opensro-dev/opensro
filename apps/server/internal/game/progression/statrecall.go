/*
===========================================================================

statrecall.go - the stat point recall: every spent stat point back to the pool

ITEM_MALL_STATS_POINT_RECALL (and its _50LV.._81LV level-limited copies,
type 3/3/13/13). Neither binary carries the rule: the v1.150 client keeps
only the "Stat Withdrawal" / "Withdraw all stat points?" strings and sends
the ordinary 0x75BD use, and the v1.188 server's special-consumable switch
(49C2B0 case 0xC) no longer handles the type. INFERRED from the item's own
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
	"opensro.online/server/internal/domain"
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
automaticStat

STR or INT a character has without spending a point: the creation value
plus one per level above 1, up to the highest level reached.
================
*/
func automaticStat(character *enterworld.Character) int64 {
	level := characterLevel(character)
	if character.MaxLevel != nil && *character.MaxLevel > level {
		level = *character.MaxLevel
	}
	if level < 1 {
		level = 1
	}
	return domain.BaseStat + autoStatPerLevel*(level-1)
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
	automatic := automaticStat(next)
	spentStr := max(0, enterworld.CharacterStrength(next)-automatic)
	spentInt := max(0, enterworld.CharacterIntellect(next)-automatic)
	if spentStr+spentInt == 0 {
		return nil, false
	}
	points := min(coercePoints(next.StatPoints)+spentStr+spentInt, statPointWordMax)
	strength := enterworld.CharacterStrength(next) - spentStr
	intellect := enterworld.CharacterIntellect(next) - spentInt
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
