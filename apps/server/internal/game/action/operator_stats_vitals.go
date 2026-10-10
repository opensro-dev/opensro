/*
===========================================================================

operator_stats_vitals.go - detached keeper projection for operator stat reset

Session teardown removes runtime effects but retains timed jobs. Project those
jobs in restore order before trimming gauges; never start their offline clocks.

===========================================================================
*/
package action

import (
	"fmt"

	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/statuseffect"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/paramkeeper"
)

// Matches statuseffect.bindModifiersLocked's detached application namespace.
const operatorEffectSourceBase uint32 = 0x80000000
const operatorJobSecondMs uint32 = 1000

/*
================
operatorResetVitals

The caller supplies a detached snapshot with its proposed base stats already
set, while holding the authority door. Only its HP/MP may change, and only
after the entire projection succeeds. No live registry, clock, or door is used.
================
*/
func (rt *Runtime) operatorResetVitals(c *enterworld.Character) error {
	stats, _, err := combat.PlayerStatsWithModifiers(c, rt.statCatalogs(), nil, nil)
	if err != nil {
		return fmt.Errorf("stat reset keeper: %w", err)
	}
	var writes []paramkeeper.Write
	seen := make(map[uint32]bool)
	groups := make(map[uint32]bool)
	nextSource := operatorEffectSourceBase
	for _, job := range c.TimedSkillJobs {
		if job.RemainingMs == 0 {
			continue
		}
		row, ok := rt.restorableSkillJob(job.SkillID)
		if !ok || row.ID != job.SkillID || row.Group == 0 || seen[job.SkillID] || groups[row.Group] {
			return fmt.Errorf("stat reset cannot project timed job %d", job.SkillID)
		}
		seen[job.SkillID] = true
		groups[row.Group] = true
		if len(seen) > statuseffect.MaxAttachedEffectsPerCharacter {
			return fmt.Errorf("stat reset timed job capacity exceeded")
		}
		if min(job.RemainingMs, row.EffectDurationMs) < operatorJobSecondMs ||
			row.BodyStatus.Present || transformWord(row) != 0 || row.Concealment.Pinned || row.Imbue.Pinned ||
			row.ReplacementRefusal != "" || row.Replacement.Lnks || row.Replacement.Lks2 || row.Replacement.Efr2 ||
			row.MovementModifier.Present && (!row.MovementModifier.Supported ||
				row.MovementModifier.Kind > statuseffect.MovementIndependent ||
				!row.MovementModifier.Persistent && !enterworld.CharacterAlive(c)) {
			return fmt.Errorf("stat reset cannot project timed job lifecycle %d", job.SkillID)
		}
		// Restore uses phase two and a fresh nonzero token; validate the same
		// pure wire admission before promising that these writes will return.
		if _, err := (wire.AttachedEffect{GID: enterworld.ObjectIDForCharacter(c), SkillID: row.ID,
			InstanceToken: 1, Phase: 2}).Encode(wire.AttachedEffectLayout{Status: row.SpawnStatus, Rider: row.EffectRider}); err != nil {
			return fmt.Errorf("stat reset timed job %d: %w", job.SkillID, err)
		}
		added, err := rt.operatorJobWrites(row, stats)
		if err != nil {
			return fmt.Errorf("stat reset timed job %d: %w", job.SkillID, err)
		}
		if _, err := statuseffect.NewModifiers(added); err != nil {
			return fmt.Errorf("stat reset timed job %d: %w", job.SkillID, err)
		}
		for i := range added {
			added[i].Source = nextSource
		}
		if len(added) != 0 {
			nextSource++
		}
		writes = append(writes, added...)
		stats, _, err = combat.PlayerStatsWithModifiers(c, rt.statCatalogs(), writes, nil)
		if err != nil {
			return fmt.Errorf("stat reset timed job %d keeper: %w", job.SkillID, err)
		}
	}
	hp, hpOK := stats.Param(itemParamMaxHP)
	mp, mpOK := stats.Param(itemParamMaxMP)
	if !hpOK || !mpOK || hp <= 0 || mp <= 0 {
		return fmt.Errorf("stat reset keeper has invalid gauge maxima")
	}
	if c.CurrentHP != nil && *c.CurrentHP > int64(hp) {
		maximum := int64(hp)
		c.CurrentHP = &maximum
	}
	if c.CurrentMP != nil && *c.CurrentMP > int64(mp) {
		maximum := int64(mp)
		c.CurrentMP = &maximum
	}
	return nil
}

/*
================
operatorJobWrites

Mirror the modifier assembly in commitCharacterEffectWithCheckpoint without
installing an effect. Capped boosts read the preceding projected jobs. Clear
their presence in the item descriptor so timedItemModifierWrites takes its
pure branch and never queries runtime modifiers or abnormal state.
================
*/
func (rt *Runtime) operatorJobWrites(row enterworld.SkillRow, before combat.Stats) ([]paramkeeper.Write, error) {
	effect := row.TimedEffect
	item := effect
	item.Strength.Present, item.Intellect.Present = false, false
	writes := buffModifierWrites(row.BuffModifiers, itemProgramWritesAccuracy(effect))
	itemWrites, err := rt.timedItemModifierWrites("", nil, item)
	if err != nil {
		return nil, err
	}
	writes = append(writes, itemWrites...)
	if effect.Pinned && effect.ItemProgram {
		strength, _ := before.Param(itemParamStrength)
		intellect, _ := before.Param(itemParamIntellect)
		boosts, err := combat.StatBoostWrites(
			combat.StatBoost{Present: effect.Strength.Present, Value: effect.Strength.Value, CapPercent: effect.Strength.CapPercent, Current: strength},
			combat.StatBoost{Present: effect.Intellect.Present, Value: effect.Intellect.Value, CapPercent: effect.Intellect.CapPercent, Current: intellect})
		if err != nil {
			return nil, err
		}
		writes = append(writes, boosts...)
	}
	if effect.Pinned {
		writes = append(writes, combat.AttributeEffectWrites(effect.Attributes)...)
		if effect.Block.Present {
			writes = append(writes, combat.BlockRateWrites(effect.Block.Mask, effect.Block.Value)...)
		}
		if effect.Reat.Mask != 0 {
			writes = append(writes, combat.StatusReductionWrites(effect.Reat)...)
		}
		if effect.Bgra.Mask != 0 {
			writes = append(writes, combat.ElementResistanceWrites(effect.Bgra)...)
		}
		if effect.Defense {
			defense, err := combat.DefenseEffectWrites(combat.DefenseModifierInput{
				Physical: effect.Physical, Magical: effect.Magical, CapPercent: effect.CapPercent,
				CurrentPhysical: float32(before.PhysicalDefense), CurrentMagical: float32(before.MagicalDefense)})
			if err != nil {
				return nil, err
			}
			writes = append(writes, defense...)
		}
	}
	return writes, nil
}
