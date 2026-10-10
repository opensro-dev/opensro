/*
===========================================================================

monster_self_effect.go - monster combat stats with self effects and abnormal writes

===========================================================================
*/

package combat

import (
	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/paramkeeper"
	"opensro.online/server/internal/game/world/monster"
)

/*
==================
MonsterInstanceStats

MonsterInstanceStats projects the live actor's combat parameters: the
RefObjChar base (4CEFE0, flat source zero), monster self effects (594AC0:
flat 5/6 for defp, flat/percent 12 for cr, flat 80..83 for dru, each an
independent source) and the abnormal block's writes (source 5, or source 0
for frostbite's 8C). Curse factors B2..B5 come from the block alone.
==================
*/
func MonsterInstanceStats(instance monster.Instance) (Stats, error) {
	s, err := MonsterStats(instance.Ref)
	if err != nil {
		return s, err
	}
	// +0xD34 is the published abnormal mask (4A8575). atca at 58F52F tests it
	// on the target (esi), which this projection is when the instance is the defender.
	s.AbnormalMask = instance.AbnormalMask()
	if err = applyMonsterSelfEffects(&s, instance.SelfEffects, instance.TargetEffects, instance.Abnormal); err != nil {
		return Stats{}, err
	}
	if instance.Abnormal == nil {
		return s, nil
	}
	for _, entry := range []struct {
		id    uint16
		value *float32
	}{{0xb2, &s.PhysicalOutgoing}, {0xb3, &s.MagicalOutgoing}, {0xb4, &s.PhysicalIncoming}, {0xb5, &s.MagicalIncoming}} {
		if !instance.Abnormal.Touches(entry.id) {
			continue
		}
		definition, _ := paramkeeper.NativeDefinition(entry.id)
		p, err := paramkeeper.New(definition)
		if err != nil {
			return Stats{}, err
		}
		if err = instance.Abnormal.ApplyTo(entry.id, p); err != nil {
			return Stats{}, err
		}
		value, err := p.Value()
		if err != nil {
			return Stats{}, err
		}
		*entry.value = value
	}
	return s, nil
}

// The terd and thrd program tags a target effect carries.
const (
	monsterEvasionDecrease = 0x74657264
	monsterHitRateDecrease = 0x74687264
)

func applyMonsterSelfEffects(s *Stats, effects monster.SelfEffects, targets monster.TargetEffects, block *abnormal.Block) error {
	for _, entry := range []struct {
		id      uint16
		value   *float64
		maximum float32
	}{
		{5, &s.PhysicalDefense, 9999999}, {6, &s.MagicalDefense, 9999999}, {7, &s.ParryRate, 10000}, {8, &s.MagicalParry, 10000},
		{9, &s.EvasionRate, 65535}, {10, &s.BlockRate, 100}, {11, &s.HitRate, 65535}, {12, &s.CriticalRate, 100},
		{128, &s.PhysicalBasicRate, 1000}, {129, &s.PhysicalSkillRate, 1000}, {130, &s.MagicalBasicRate, 1000}, {131, &s.MagicalSkillRate, 1000},
	} {
		touched := block != nil && block.Touches(entry.id)
		if effects == (monster.SelfEffects{}) && targets == (monster.TargetEffects{}) && !touched {
			continue
		}
		p, err := paramkeeper.New(paramkeeper.Definition{Base: float32(*entry.value), Maximum: entry.maximum})
		if err != nil {
			return err
		}
		// Definition.Base is the empty-keeper fallback, not a contribution.
		// Native actor initialization inserts the base stat as source zero.
		if _, err = p.Apply(paramkeeper.Flat, 0, float32(*entry.value)); err != nil {
			return err
		}
		for _, e := range effects {
			if e.Token == 0 {
				continue
			}
			var value uint32
			applies := false
			switch e.Tag {
			case 0x64656670:
				if entry.id == 5 {
					value, applies = e.First, true
				}
				if entry.id == 6 {
					value, applies = e.Second, true
				}
			case 0x6372:
				if entry.id == 12 {
					value, applies = e.First, true
					if _, err = p.Apply(paramkeeper.PercentSum, e.Token, float32(e.Second)); err != nil {
						return err
					}
				}
			case 0x647275:
				if entry.id == 128 || entry.id == 129 {
					value, applies = e.First, true
				}
				if entry.id == 130 || entry.id == 131 {
					value, applies = e.Second, true
				}
			}
			if applies {
				if _, err = p.Apply(paramkeeper.Flat, e.Token, float32(value)); err != nil {
					return err
				}
			}
		}
		// 594AC0 writes another actor's terd / thrd negated on the flat
		// channel: 9 evasion (59591A), 0xB hit rate (595954).
		for _, e := range targets {
			if e.Token == 0 {
				continue
			}
			if e.Tag == monsterEvasionDecrease && entry.id == 9 || e.Tag == monsterHitRateDecrease && entry.id == 11 {
				if _, err = p.Apply(paramkeeper.Flat, e.Token, -float32(e.First)); err != nil {
					return err
				}
			}
		}
		if touched {
			if err = block.ApplyTo(entry.id, p); err != nil {
				return err
			}
		}
		value, err := p.Value()
		if err != nil {
			return err
		}
		*entry.value = float64(value)
	}
	return nil
}
