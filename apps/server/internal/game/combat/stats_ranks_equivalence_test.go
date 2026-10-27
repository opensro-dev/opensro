/*
===========================================================================

stats_ranks_equivalence_test.go - the shared rank rows change nothing

The passive projections now walk one list of selected rank rows
(learnedRankRows) built with the light group/level accessor, instead of
each building every learned row itself (2026-10-13 prod profile). This
test keeps the shipped implementation verbatim (old* below, copied from
main before the change) and requires byte-identical results on thousands
of random characters drawn from the real skill table: duplicates,
unresolved ids, several ranks of one group, every weapon kind.

===========================================================================
*/

package combat

import (
	"fmt"
	"math/rand"
	"opensro.online/server/internal/testsupport/tier"
	"reflect"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/paramkeeper"
	"opensro.online/server/internal/testsupport/gamedatatest"
)

// The shipped implementation before learnedRankRows, kept verbatim as the
// reference.
func oldLearnedGroupRanks(c *domain.Character, skills enterworld.SkillDataSource) map[uint32]groupRank {
	ranks := make(map[uint32]groupRank, len(c.Skills))
	for _, id := range c.Skills {
		row, ok := skills.SkillByID(id)
		if !ok {
			continue
		}
		if previous, exists := ranks[row.Group]; !exists || row.Level > previous.level {
			ranks[row.Group] = groupRank{id: row.ID, level: row.Level}
		}
	}
	return ranks
}

func oldLearnedPassives(c *domain.Character, skills enterworld.SkillDataSource, items enterworld.ItemRefSource, weaponKind uint8) ([]paramkeeper.Write, enterworld.SkillParameterValues, error) {
	if len(c.Skills) == 0 {
		return nil, enterworld.SkillParameterValues{}, nil
	}
	if skills == nil {
		return nil, enterworld.SkillParameterValues{}, fmt.Errorf("combat: learned skills require skill references")
	}
	current := oldLearnedGroupRanks(c, skills)
	var writes []paramkeeper.Write
	var source uint32 = 2048
	var power enterworld.SkillParameterValues
	for _, id := range c.Skills {
		row, ok := skills.SkillByID(id)
		if !ok {
			continue
		}
		if rank, exists := current[row.Group]; !exists || rank.id != id {
			continue
		}
		delete(current, row.Group)
		selected := row
		source++ // distinct from parameter and equipment projection identities
		// The passive toggle: a passive whose reqi the equipment fails contributes
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
			// The buff installer's reat block raises flat status reduction 0x91+i.
			for _, w := range StatusReductionWrites(p.Reat) {
				w.Source = source
				writes = append(writes, w)
			}
			// The buff installer's br block raises the flat block rate of
			// each lane its mask selects. BlockChance reads those lanes with no
			// shield test of its own, so the reqi walk is the only gate.
			if p.Br.Mask != 0 {
				for _, w := range BlockRateWrites(p.Br.Mask, p.Br.Value) {
					w.Source = source
					writes = append(writes, w)
				}
			}
			// The buff installer: hr, er, hpi and mpi put word 1 on the percent channel
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
			// The installer's dru block: word 0 on 0x80/0x81, word 1 on 0x82/0x83.
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

func oldLearnedStatusResistance(c *domain.Character, skills enterworld.SkillDataSource, items enterworld.ItemRefSource) [17]abnormal.Resistance {
	var out [17]abnormal.Resistance
	var filed [17]bool
	if skills == nil {
		return out
	}
	current := oldLearnedGroupRanks(c, skills)
	for _, id := range c.Skills {
		row, ok := skills.SkillByID(id)
		if !ok {
			continue
		}
		if rank, exists := current[row.Group]; !exists || rank.id != id {
			continue
		}
		delete(current, row.Group)
		selected := row
		rs := selected.PassiveParameters.Real
		if !selected.PassiveParameters.Pinned || selected.ChainSub || rs.Mask == 0 ||
			selected.Reqi.Present && ReqiRefusal(c, items, selected.Reqi) != 0 {
			continue
		}
		FileStatusResistance(&out, &filed, rs)
	}
	return out
}

func oldLearnedDamageReturn(c *domain.Character, skills enterworld.SkillDataSource, items enterworld.ItemRefSource) (enterworld.SkillDamageReturn, uint32) {
	if skills == nil {
		return enterworld.SkillDamageReturn{}, 0
	}
	current := oldLearnedGroupRanks(c, skills)
	for _, id := range c.Skills {
		row, ok := skills.SkillByID(id)
		if !ok {
			continue
		}
		if rank, exists := current[row.Group]; !exists || rank.id != id {
			continue
		}
		p := row.PassiveParameters
		if !p.Pinned || !p.DamageReturn.Present || row.ChainSub ||
			row.Reqi.Present && ReqiRefusal(c, items, row.Reqi) != 0 {
			continue
		}
		return p.DamageReturn, row.ID
	}
	return enterworld.SkillDamageReturn{}, 0
}

const (
	equivalenceCharacters = 4000
	equivalenceMaxSkills  = 160
	equivalenceIDLimit    = 40000
	equivalenceWeaponKind = 17
)

/*
================
TestRankRowsMatchTheShippedProjection
================
*/
func TestRankRowsMatchTheShippedProjection(t *testing.T) {
	tier.Long(t, "rank rows equal the shipped projection over 4000 characters")
	skills := enterworld.NewTextdataSkills(gamedatatest.TextdataDir(t))
	var ids []uint32
	for id := uint32(1); id < equivalenceIDLimit; id++ {
		if _, ok := skills.SkillByID(id); ok {
			ids = append(ids, id)
		}
	}
	if len(ids) < 1000 {
		t.Fatalf("only %d skills resolved; the table did not load", len(ids))
	}
	random := rand.New(rand.NewSource(20261013))
	for n := 0; n < equivalenceCharacters; n++ {
		c := &domain.Character{}
		count := random.Intn(equivalenceMaxSkills)
		for i := 0; i < count; i++ {
			switch random.Intn(20) {
			case 0: // an id the table does not have
				c.Skills = append(c.Skills, equivalenceIDLimit+uint32(random.Intn(1000)))
			case 1: // a duplicate of one already learned
				if len(c.Skills) > 0 {
					c.Skills = append(c.Skills, c.Skills[random.Intn(len(c.Skills))])
					continue
				}
				fallthrough
			default:
				// Neighbouring ids are often ranks of one group.
				base := random.Intn(len(ids))
				c.Skills = append(c.Skills, ids[min(base+random.Intn(3), len(ids)-1)])
			}
		}
		weapon := uint8(random.Intn(equivalenceWeaponKind))
		wantWrites, wantPower, wantErr := oldLearnedPassives(c, skills, nil, weapon)
		gotWrites, gotPower, gotErr := learnedPassives(c, skills, nil, weapon)
		rows := learnedRankRows(c, skills, nil)
		fromWrites, fromPower, fromErr := learnedPassivesFrom(c, rows, nil, weapon)
		label := fmt.Sprintf("character %d (%d skills, weapon %d)", n, len(c.Skills), weapon)
		if !reflect.DeepEqual(wantWrites, gotWrites) || wantPower != gotPower || fmt.Sprint(wantErr) != fmt.Sprint(gotErr) {
			t.Fatalf("%s: learnedPassives changed", label)
		}
		if len(c.Skills) > 0 && (!reflect.DeepEqual(wantWrites, fromWrites) || wantPower != fromPower || fmt.Sprint(wantErr) != fmt.Sprint(fromErr)) {
			t.Fatalf("%s: learnedPassivesFrom changed", label)
		}
		if want, got := oldLearnedStatusResistance(c, skills, nil), learnedStatusResistanceFrom(c, rows, nil); want != got {
			t.Fatalf("%s: status resistance changed", label)
		}
		wantReturn, wantSkill := oldLearnedDamageReturn(c, skills, nil)
		gotReturn, gotSkill := learnedDamageReturnFrom(c, rows, nil)
		if !reflect.DeepEqual(wantReturn, gotReturn) || wantSkill != gotSkill {
			t.Fatalf("%s: damage return changed", label)
		}
		if want, got := oldLearnedGroupRanks(c, skills), learnedGroupRanks(c, skills); !reflect.DeepEqual(want, got) {
			t.Fatalf("%s: group ranks changed", label)
		}
	}
}

/*
================
TestRankRowsWithoutTheLightAccessor

A skill source without SkillGroupRank is read through SkillByID and selects
the same rows.
================
*/
func TestRankRowsWithoutTheLightAccessor(t *testing.T) {
	skills := enterworld.NewTextdataSkills(gamedatatest.TextdataDir(t))
	plain := rowOnlySkills{skills}
	random := rand.New(rand.NewSource(7))
	for n := 0; n < 300; n++ {
		c := &domain.Character{}
		for i := random.Intn(80); i > 0; i-- {
			c.Skills = append(c.Skills, uint32(1+random.Intn(12000)))
		}
		if a, b := learnedRankRows(c, skills, nil), learnedRankRows(c, plain, nil); !reflect.DeepEqual(a, b) {
			t.Fatalf("character %d: the fallback selected different rows", n)
		}
	}
}

// rowOnlySkills hides SkillGroupRank (enterworld.SkillGroupRanker).
type rowOnlySkills struct{ inner *enterworld.TextdataSkills }

func (s rowOnlySkills) SkillByID(id uint32) (enterworld.SkillRow, bool) { return s.inner.SkillByID(id) }

/*
================
BenchmarkPlayerPassiveProjection
================
*/
func BenchmarkPlayerPassiveProjection(b *testing.B) {
	skills := enterworld.NewTextdataSkills(gamedatatest.TextdataDir(b))
	c := &domain.Character{}
	for id := uint32(1); len(c.Skills) < 140 && id < equivalenceIDLimit; id += 7 {
		if _, ok := skills.SkillByID(id); ok {
			c.Skills = append(c.Skills, id)
		}
	}
	b.Run("shipped", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			oldLearnedPassives(c, skills, nil, 0)
			oldLearnedStatusResistance(c, skills, nil)
			oldLearnedDamageReturn(c, skills, nil)
		}
	})
	b.Run("rank-rows", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			rows := learnedRankRows(c, skills, nil)
			learnedPassivesFrom(c, rows, nil, 0)
			learnedStatusResistanceFrom(c, rows, nil)
			learnedDamageReturnFrom(c, rows, nil)
		}
	})
	// The production path: a character with an ID hits rankcache.go.
	cached := *c
	cached.ID = 1
	b.Run("rank-rows-cached", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			rows := learnedRankRowsCached(&cached, skills)
			learnedPassivesFrom(&cached, rows, nil, 0)
			learnedStatusResistanceFrom(&cached, rows, nil)
			learnedDamageReturnFrom(&cached, rows, nil)
		}
	})
}
