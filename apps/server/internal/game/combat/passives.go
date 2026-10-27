/*
===========================================================================

passives.go - learned passive skills projected onto the keeper

Each learned passive group's highest rank contributes its setv parameter
values, keeper writes (reat, br, hpi/mpi/er/hr, passive defense, passive
cr) and status
resistance buckets. A program declaring reqi is gated by the server's reqi
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
groupRank
==================
*/
type groupRank struct {
	id    uint32
	level int64
}

/*
==================
learnedGroupRanks

The highest learned rank of each skill group, by ID. Unresolved IDs are
skipped (the learn owner's policy). Ranks, not rows: SkillRow is a large
value, and a map of rows allocated its buckets on every stats projection
(about 150 MB a minute with one player in combat).
==================
*/
func learnedGroupRanks(c *domain.Character, skills enterworld.SkillDataSource) map[uint32]groupRank {
	ranks := make(map[uint32]groupRank, len(c.Skills))
	for _, id := range c.Skills {
		group, level, ok := skillGroupRank(skills, id)
		if !ok {
			continue
		}
		if previous, exists := ranks[group]; !exists || level > previous.level {
			ranks[group] = groupRank{id: id, level: level}
		}
	}
	return ranks
}

/*
==================
skillGroupRank

A skill's group and level, through the source's light accessor when it has
one (enterworld.SkillGroupRanker) so no whole row is built. The row's ID is
the id it is looked up by.
==================
*/
func skillGroupRank(skills enterworld.SkillDataSource, id uint32) (uint32, int64, bool) {
	if ranker, ok := skills.(enterworld.SkillGroupRanker); ok {
		return ranker.SkillGroupRank(id)
	}
	row, ok := skills.SkillByID(id)
	return row.Group, row.Level, ok
}

/*
==================
learnedRankRows

The row of each learned group's highest rank, in c.Skills order: the one
list every passive projection walks (passives, status resistance, damage
return). Built once per stats projection, so each selected row is built
once instead of every learned row being built four times (2026-10-13 prod
profile: about 11% of the GameWorld). The selection is exactly the one the
projections made each on their own: the first id in c.Skills that holds
its group's highest rank, that group then taken. Rows are appended to
into[:0]. Callers take them through learnedRankRowsCached (rankcache.go).
==================
*/
func learnedRankRows(c *domain.Character, skills enterworld.SkillDataSource, into []enterworld.SkillRow) []enterworld.SkillRow {
	current := learnedGroupRanks(c, skills)
	rows := into[:0]
	for _, id := range c.Skills {
		group, _, ok := skillGroupRank(skills, id)
		if !ok {
			continue
		}
		if rank, exists := current[group]; !exists || rank.id != id {
			continue
		}
		delete(current, group)
		row, ok := skills.SkillByID(id)
		if !ok {
			continue
		}
		rows = append(rows, row)
	}
	return rows
}

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
	return learnedPassivesFrom(c, learnedRankRowsCached(c, skills), items, weaponKind)
}

/*
==================
learnedPassivesFrom

learnedPassives over the selected rank rows (learnedRankRows).
==================
*/
func learnedPassivesFrom(c *domain.Character, rows []enterworld.SkillRow, items enterworld.ItemRefSource, weaponKind uint8) ([]paramkeeper.Write, enterworld.SkillParameterValues, error) {
	var writes []paramkeeper.Write
	var source uint32 = 2048
	var power enterworld.SkillParameterValues
	for _, selected := range rows {
		source++ // distinct from parameter and equipment projection identities
		// The reqi walk: a passive whose reqi the equipment fails contributes
		// nothing - its setv entries, keeper writes and resistances alike.
		eligible := !selected.Reqi.Present || ReqiRefusal(c, items, selected.Reqi) == 0
		if p := selected.PassiveParameters; p.Pinned && !selected.ChainSub && eligible {
			// The server assigns the setv entry; it does not sum values. Highest
			// learned group rank is selected above.
			for slot := enterworld.SkillParameter(0); slot < enterworld.SkillParameterCount; slot++ {
				if p.Mask.Has(slot) {
					power[slot] = p.Values[slot]
				}
			}
			// The effect installer's reat case raises flat status reduction 0x91+i.
			for _, w := range StatusReductionWrites(p.Reat) {
				w.Source = source
				writes = append(writes, w)
			}
			// The effect installer's br case raises the flat block rate of
			// each lane its mask selects. BlockChance reads those lanes with no
			// shield test of its own, so the reqi walk is the only gate.
			if p.Br.Mask != 0 {
				for _, w := range BlockRateWrites(p.Br.Mask, p.Br.Value) {
					w.Source = source
					writes = append(writes, w)
				}
			}
			// The effect installer: hr, er, hpi and mpi put word 1 on the percent channel
			// and word 0 on the flat channel of parameters 11, 9, 3 and 4.
			for _, fr := range [...]struct {
				rate  enterworld.SkillFlatRate
				param uint16
			}{{p.HitRate, 11}, {p.Evasion, 9}, {p.MaxHP, 3}, {p.MaxMP, 4}} {
				if fr.rate.Present {
					writes = append(writes,
						paramkeeper.Write{Parameter: fr.param, Channel: paramkeeper.PercentSum, Source: source, Value: float32(fr.rate.Percent)},
						paramkeeper.Write{Parameter: fr.param, Channel: paramkeeper.Flat, Source: source, Value: float32(fr.rate.Flat)})
				}
			}
			if p.CriticalEvasion != 0 {
				writes = append(writes, CriticalEvasionWrite(p.CriticalEvasion, source))
			}
			if m := selected.BuffModifiers; p.IncomingReduction && m.Odar {
				writes = append(writes, IncomingReductionWrites(m.OdarBits, m.OdarWord, source)...)
			}
			// The installer's dru case: word 0 on 0x80/0x81, word 1 on 0x82/0x83.
			if p.Dru != [2]uint32{} {
				for i, params := range [2][2]uint16{{0x80, 0x81}, {0x82, 0x83}} {
					for _, param := range params {
						writes = append(writes, paramkeeper.Write{Parameter: param, Channel: paramkeeper.Flat, Source: source, Value: float32(p.Dru[i])})
					}
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

The skill manager's status-resistance buckets real fills: each
masked status's bucket keys its flats by grade. The server's resistance read
reports the highest unsigned grade and its highest unsigned flat; no
v1.150 source fills the percent side. Eligibility is the reqi walk's, as for
every other passive contribution.
==================
*/
func learnedStatusResistance(c *domain.Character, skills enterworld.SkillDataSource, items enterworld.ItemRefSource) [17]abnormal.Resistance {
	if skills == nil {
		return [17]abnormal.Resistance{}
	}
	return learnedStatusResistanceFrom(c, learnedRankRowsCached(c, skills), items)
}

/*
==================
learnedStatusResistanceFrom

learnedStatusResistance over the selected rank rows (learnedRankRows).
==================
*/
func learnedStatusResistanceFrom(c *domain.Character, rows []enterworld.SkillRow, items enterworld.ItemRefSource) [17]abnormal.Resistance {
	var out [17]abnormal.Resistance
	var filed [17]bool
	for _, selected := range rows {
		rs := selected.PassiveParameters.Real
		if !selected.PassiveParameters.Pinned || selected.ChainSub || rs.Mask == 0 ||
			selected.Reqi.Present && ReqiRefusal(c, items, selected.Reqi) != 0 {
			continue
		}
		FileStatusResistance(&out, &filed, rs)
	}
	return out
}
