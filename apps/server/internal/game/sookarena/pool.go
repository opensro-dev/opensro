/*
===========================================================================

pool.go - arena encounters chosen from the shipped combat references

Props, structures, job actors and rewardless references never enter the pool.

===========================================================================
*/
package sookarena

import (
	"opensro.online/server/internal/game/world/monster"
	"strings"
)

/*
================
Pool
================
*/
type Pool struct{ Normal, Unique []monster.MonsterRef }

/*
================
BuildPool
================
*/
func BuildPool(refs []monster.MonsterRef) Pool {
	var p Pool
	for _, ref := range refs {
		// Port-only: ordinary tomb species only, never traps, egos or chain bosses.
		if strings.HasPrefix(ref.Codename, "MOB_TQ_") {
			family := strings.TrimSuffix(strings.TrimSuffix(strings.TrimPrefix(ref.Codename, "MOB_TQ_"), "_CLON2"), "_CLON")
			switch family {
			case "TOMBSOLDIER", "QINSHITOMBSTONE", "TOMBARCHER", "TOMBSTONEGHOST", "ROYALSOLDIER", "TOMBBUGGHOST", "SNAKESLAVE", "SNAKEWOMAN", "SNAKEDEMON", "SNAKEMAN":
			default:
				continue
			}
		}
		// Quest-only Rocky clones have inflated boss-like HP; use ordinary field variants.
		if strings.HasPrefix(ref.Codename, "MOB_QT_") {
			continue
		}
		// The CTF bracket family is listed in action/flagwar_match.go, not field encounters.
		switch ref.Codename {
		case "MOB_EV_WOLF", "MOB_EV_RAVEN", "MOB_EV_PENGUIN", "MOB_EV_KANGAROO", "MOB_EV_JINN":
			continue
		}
		// Port-only: event pets, reinforced variants and unique followers are not field encounters.
		if !strings.HasPrefix(ref.Codename, "MOB_") || strings.Contains(ref.Codename, "_STRONG_") || strings.Contains(ref.Codename, "_L2") || strings.Contains(ref.Codename, "_L3") || strings.Contains(ref.Codename, "_L4") {
			continue
		}
		if ref.TrainingDummy || ref.Structure || ref.MaxHP == 0 || ref.ModelPath == "" || ref.DefaultSkillIDs == ([10]uint32{}) || !ref.CombatPinned || !ref.RewardActionPinned || ref.ExpToGive == 0 || ref.RunSpeed <= 0 || monster.TradeNpcMonster(ref) {
			continue
		}
		if ref.MonsterType == 0 {
			p.Normal = append(p.Normal, ref)
		}
		if ref.MonsterType == 3 {
			p.Unique = append(p.Unique, ref)
		}
	}
	return p
}

/*
================
Normals - nearest installed level at or below the requested encounter level.
================
*/
func (p Pool) Normals(level int) []monster.MonsterRef {
	best := 0
	for _, ref := range p.Normal {
		if int(ref.Level) <= level && int(ref.Level) > best {
			best = int(ref.Level)
		}
	}
	var refs []monster.MonsterRef
	for _, ref := range p.Normal {
		if int(ref.Level) == best {
			refs = append(refs, ref)
		}
	}
	return refs
}

/*
================
Boss - nearest unique no more than five levels above the player.
================
*/
func (p Pool) Boss(level int) (monster.MonsterRef, bool) {
	var best monster.MonsterRef
	for _, ref := range p.Unique {
		if int(ref.Level) <= level && ref.Level > best.Level {
			best = ref
		}
	}
	return best, best.RefObjID != 0
}

/*
================
StageNormals - five closed level bands, always within player -8 .. +4.
================
*/
func (p Pool) StageNormals(level, stage int) []monster.MonsterRef {
	low, high := StageBounds(level, stage)
	var refs []monster.MonsterRef
	for _, ref := range p.Normal {
		if int(ref.Level) >= low && int(ref.Level) <= high {
			refs = append(refs, ref)
		}
	}
	return refs
}

/*
================
StageBounds
================
*/
func StageBounds(level, stage int) (int, int) {
	i := max(0, min(4, stage))
	return max(1, level+[]int{-8, -6, -3, -1, 1}[i]), max(1, level+[]int{-5, -3, 0, 2, 4}[i])
}

/*
================
StageBoss - never substitute an under-level unique for an unavailable band.
================
*/
func (p Pool) StageBoss(level, stage int) (monster.MonsterRef, bool) {
	low, high := StageBounds(level, stage)
	boss, ok := p.Boss(high)
	return boss, ok && int(boss.Level) >= low
}
