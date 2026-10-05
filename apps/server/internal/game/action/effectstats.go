/*
===========================================================================

effectstats.go - player combat stats with effects and abnormal writes

===========================================================================
*/

package action

import (
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

// statCatalogs are the reference sources every player keeper reads.
func (rt *Runtime) statCatalogs() combat.Catalogs {
	return combat.Catalogs{Items: rt.deps.ItemReferences(), Skills: rt.deps.SkillData(), MagicOptions: rt.deps.MagicOptionDefinitions()}
}

/*
==================
playerCombatStats

playerCombatStats reads installed contributions from their sole lifecycle
owners (effects and the abnormal block). Callers hold the character/division
operation door, as for equipment.
==================
*/
func (rt *Runtime) playerCombatStats(division string, c *enterworld.Character) (combat.Stats, combat.Loadout, error) {
	if c == nil {
		return combat.PlayerStats(c, rt.statCatalogs())
	}
	block := rt.playerAbnormal(division, c.Name)
	stats, loadout, err := combat.PlayerStatsWithModifiers(c, rt.statCatalogs(), rt.effects.ModifierWrites(division, c.Name), block)
	if err == nil && block != nil {
		stats.AbnormalMask = block.Mask
	}
	return stats, loadout, err
}

// engagedSkillModifierSource keys the engaged attack's own writes apart from
// every installed effect (whose sources count up from 0x80000000).
const engagedSkillModifierSource uint32 = 0x7fffff00

/*
==================
playerAttackStats

playerCombatStats while skill is engaged. SkillCombat_EngageSkill (593540)
installs the engaged skill's own modifier block through 594AC0, so an
attack that authors hr (the bow's Arrow Rain lines) rolls its hits with
that hit rate. A timed row's block belongs to its effect instead, and ru
is the reach rule's (skillActionReach), so only an attack's hr is added.
==================
*/
func (rt *Runtime) playerAttackStats(division string, c *enterworld.Character, skill enterworld.SkillRow) (combat.Stats, combat.Loadout, error) {
	if c == nil || !skill.BuffModifiers.Hr || skill.TimedEffect.Pinned {
		return rt.playerCombatStats(division, c)
	}
	block := rt.playerAbnormal(division, c.Name)
	engaged := enterworld.SkillBuffModifiers{Hr: true, HrFlat: skill.BuffModifiers.HrFlat, HrRate: skill.BuffModifiers.HrRate}
	writes := rt.effects.ModifierWrites(division, c.Name)
	for _, w := range buffModifierWrites(engaged, false) {
		w.Source = engagedSkillModifierSource
		writes = append(writes, w)
	}
	stats, loadout, err := combat.PlayerStatsWithModifiers(c, rt.statCatalogs(), writes, block)
	if err == nil && block != nil {
		stats.AbnormalMask = block.Mask
	}
	return stats, loadout, err
}

/*
==================
playerKeeperVitals

playerKeeperVitals is the living gauge ceiling (keeper params 3 and 4)
and the currents clamped to it. Nil current means full at that ceiling.
Death stays a stored zero, which clamps to zero here as well.
==================
*/
func (rt *Runtime) playerKeeperVitals(division string, c *enterworld.Character) (maxHP, maxMP, currentHP, currentMP int64) {
	if c == nil {
		return 0, 0, 0, 0
	}
	maxHP, maxMP = enterworld.DerivedMaxHP(c), enterworld.DerivedMaxMP(c)
	if stats, _, err := rt.playerCombatStats(division, c); err == nil {
		if hp, ok := stats.Param(3); ok && hp > 0 {
			maxHP = int64(hp)
		}
		if mp, ok := stats.Param(4); ok && mp > 0 {
			maxMP = int64(mp)
		}
	}
	currentHP = clampKeeperVital(c.CurrentHP, maxHP)
	currentMP = clampKeeperVital(c.CurrentMP, maxMP)
	return maxHP, maxMP, currentHP, currentMP
}

/*
==================
GameplayVitals

GameplayVitals is the party-roster read: keeper current and maximum,
same numbers combat and recovery use. The party package cannot import
action, so wiring passes this method in.
==================
*/
func (rt *Runtime) GameplayVitals(division string, c *enterworld.Character) (currentHP, maxHP, currentMP, maxMP int64) {
	if rt == nil {
		return 0, 0, 0, 0
	}
	maxHP, maxMP, currentHP, currentMP = rt.playerKeeperVitals(division, c)
	return
}

// publishedVitals is the single gameplay read of the living gauge.
func (rt *Runtime) publishedVitals(division string, c *enterworld.Character) simulation.Vitals {
	_, _, hp, mp := rt.playerKeeperVitals(division, c)
	return simulation.Vitals{CurrentHP: uint32(hp), CurrentMP: uint32(mp)}
}

/*
==================
clampStoredGaugeToKeeper

clampStoredGaugeToKeeper writes a stored current down when the keeper
maximum dropped (unequip, buff end, Panic, level down). Nil stays nil:
absent means full, and full follows the new maximum. 4E3294 params 3/4.
==================
*/
func (rt *Runtime) clampStoredGaugeToKeeper(division string, c *enterworld.Character) (hp bool, mp bool) {
	if c == nil {
		return false, false
	}
	maxHP, maxMP, _, _ := rt.playerKeeperVitals(division, c)
	if c.CurrentHP != nil && *c.CurrentHP > maxHP {
		clamped := maxHP
		c.CurrentHP = &clamped
		hp = true
	}
	if c.CurrentMP != nil && *c.CurrentMP > maxMP {
		clamped := maxMP
		c.CurrentMP = &clamped
		mp = true
	}
	return hp, mp
}

// gaugeDropFrames publishes a clamp: 0x33A6 for the currents and 0x343C
// when the maximum itself moved.
func (rt *Runtime) gaugeDropFrames(division string, c *enterworld.Character, hp, mp, stats bool) []wire.Frame {
	var frames []wire.Frame
	if c == nil {
		return frames
	}
	if hp || mp {
		frames = append(frames, wire.Frame{Opcode: simulation.OpVitalsUpdate, Payload: simulation.VitalsRefreshPayload(enterworld.ObjectIDForCharacter(c), rt.publishedVitals(division, c))})
	}
	if stats {
		if block, err := rt.PlayerBaseStats(division, c); err == nil {
			frames = append(frames, wire.Frame{Opcode: wire.OpBaseStats, Payload: block.Encode()})
		}
	}
	return frames
}

func clampKeeperVital(current *int64, maximum int64) int64 {
	if maximum < 0 {
		maximum = 0
	}
	if current == nil {
		return maximum
	}
	if *current < 0 {
		return 0
	}
	if *current > maximum {
		return maximum
	}
	return *current
}

// PlayerBaseStats is the wire projection of the same installed contributions.
// It does not advance timers or retire effects while preparing a packet.
func (rt *Runtime) PlayerBaseStats(division string, c *enterworld.Character) (wire.BaseStats, error) {
	if c == nil {
		return combat.PlayerBaseStats(c, rt.statCatalogs())
	}
	return combat.PlayerBaseStatsWithModifiers(c, rt.statCatalogs(), rt.effects.ModifierWrites(division, c.Name), rt.playerAbnormal(division, c.Name))
}
