/*
===========================================================================

passives.go - learned passive skills projected onto the keeper

Each learned passive group's highest rank contributes its setv parameter
values, keeper writes (reat, br, passive defense, passive cr) and status
resistance buckets. A program declaring reqi is gated by the 59F0E0
equipment walk; passive cr follows the equipped weapon kind instead.
Nothing here is cached: Character.Snapshot is the only input.

===========================================================================
*/

package combat

import (
	"fmt"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/paramkeeper"
)

/*
==================
learnedPassives

Invariant: each currently learned passive rank contributes at most once.
Equipment eligibility applies only when the program declares reqi.
Character.Snapshot is the single input owner (learning, equipment, teardown
and restoration). This pure projection stores no bonus, activation latch or
invalidation cache. Never sum executing-skill cr blocks or persist derived
passive values.
==================
*/
func learnedPassives(c *domain.Character, skills enterworld.SkillDataSource, items enterworld.ItemRefSource, weaponKind uint8) ([]paramkeeper.Write, enterworld.SkillParameterValues, error) {
	if len(c.Skills) == 0 {
		return nil, enterworld.SkillParameterValues{}, nil
	}
	if skills == nil {
		return nil, enterworld.SkillParameterValues{}, fmt.Errorf("combat: learned skills require skill references")
	}
	current := make(map[uint32]enterworld.SkillRow)
	for _, id := range c.Skills {
		row, ok := skills.SkillByID(id)
		if !ok {
			continue
		} // Same unresolved-ID policy as the learn owner.
		previous, exists := current[row.Group]
		if !exists || row.Level > previous.Level {
			current[row.Group] = row
		}
	}
	var writes []paramkeeper.Write
	var source uint32 = 2048
	var power enterworld.SkillParameterValues
	for _, id := range c.Skills {
		row, ok := skills.SkillByID(id)
		if !ok {
			continue
		}
		selected, exists := current[row.Group]
		if !exists || selected.ID != id {
			continue
		}
		delete(current, row.Group)
		source++ // distinct from parameter and equipment projection identities
		// 59F0E0: a passive whose reqi the equipment fails contributes
		// nothing - its setv entries, keeper writes and resistances alike.
		eligible := !selected.Reqi.Present || ReqiRefusal(c, items, selected.Reqi) == 0
		if p := selected.PassiveParameters; p.Pinned && !selected.ChainSub && eligible {
			// 5A02E0 assigns the setv entry; it does not sum values. Highest
			// learned group rank is selected above.
			for slot := enterworld.SkillParameter(0); slot < enterworld.SkillParameterCount; slot++ {
				if p.Mask.Has(slot) {
					power[slot] = p.Values[slot]
				}
			}
			// 595542..59568F: reat raises flat status reduction 0x91+i.
			for i := uint16(0); i < 6; i++ {
				if p.Reat.Mask&(1<<i) != 0 {
					writes = append(writes, paramkeeper.Write{Parameter: 0x91 + i, Channel: paramkeeper.Flat, Source: source, Value: float32(p.Reat.Value)})
				}
			}
			// 594AC0 at 0x595DFD..0x595EFA: br raises the flat block rate of
			// each lane its mask selects. BlockChance reads those lanes with no
			// shield test of its own, so the reqi walk is the only gate.
			if p.Br.Mask != 0 {
				for _, w := range BlockRateWrites(p.Br.Mask, p.Br.Value) {
					w.Source = source
					writes = append(writes, w)
				}
			}
		}
		if d := selected.PassiveDefense; d.Pinned && !selected.ChainSub && selected.ChainNext == 0 && eligible {
			defense, err := defenseModifierWrites(source, DefenseModifierInput{Physical: d.Physical, Magical: d.Magical})
			if err != nil {
				return nil, power, err
			}
			writes = append(writes, defense...)
		}
		passive := selected.PassiveCritical
		if passive.Pinned && !selected.ChainSub && weaponKind != 0 && passive.WeaponKind == weaponKind {
			writes = append(writes, paramkeeper.Write{Parameter: 12, Source: source, Value: float32(passive.Flat)})
		}
	}
	return writes, power, nil
}

/*
==================
learnedStatusResistance

The CSkillManager status-resistance buckets real fills (59DF20): each
masked status's bucket keys its flats by grade. StatusResistance_Read
(5999E0) reports the lowest grade and the first flat filed under it; no
v1.150 source fills the percent side. Eligibility is 59F0E0's, as for
every other passive contribution.
==================
*/
func learnedStatusResistance(c *domain.Character, skills enterworld.SkillDataSource, items enterworld.ItemRefSource) [17]abnormal.Resistance {
	var out [17]abnormal.Resistance
	var filed [17]bool
	if skills == nil {
		return out
	}
	current := make(map[uint32]enterworld.SkillRow)
	for _, id := range c.Skills {
		row, ok := skills.SkillByID(id)
		if !ok {
			continue
		}
		if previous, exists := current[row.Group]; !exists || row.Level > previous.Level {
			current[row.Group] = row
		}
	}
	for _, id := range c.Skills {
		row, ok := skills.SkillByID(id)
		if !ok {
			continue
		}
		selected, exists := current[row.Group]
		if !exists || selected.ID != id {
			continue
		}
		delete(current, row.Group)
		rs := selected.PassiveParameters.Real
		if !selected.PassiveParameters.Pinned || selected.ChainSub || rs.Mask == 0 ||
			selected.Reqi.Present && ReqiRefusal(c, items, selected.Reqi) != 0 {
			continue
		}
		for _, source := range abnormal.Sources {
			if source.Resist < 0 || rs.Mask&source.Status.Bit() == 0 {
				continue
			}
			bucket := &out[source.Resist]
			if !filed[source.Resist] || int32(rs.Grade) < bucket.Grade {
				bucket.Grade, bucket.Flat = int32(rs.Grade), int32(rs.Flat)
				filed[source.Resist] = true
			}
		}
	}
	return out
}
