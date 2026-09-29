/*
===========================================================================

leveldata.go - authored level progression and restoration prices

The level and drop-gold tables belong to the verified server projection.
Missing rows refuse the consuming operation; they never imply free training,
free restoration or an invented experience curve.

===========================================================================
*/
package enterworld

import (
	"path/filepath"
	"sync"

	log "github.com/sirupsen/logrus"
)

/*
================
LevelDataSource

Only progression requires this interface. Restoration separately asks for
WithdrawalGoldBasis, so an unrelated experience source need not price items.
================
*/
type LevelDataSource interface {
	// SkillPointCost answers the SP a mastery level costs to train. The
	// second result is false when the table has no row for that level,
	// which must refuse the training rather than default to zero.
	SkillPointCost(level int64) (int64, bool)
	// ExpRequired answers the experience needed to advance FROM level to
	// level+1: the exp curve the client's own level-up walk reads (the
	// 0x30D2 handler sub_779620 calls sub_7e0f20(level) and reads the
	// u64 at row +0x08/+0x0c, immediately before the SP-cost field at
	// +0x10 - it is column index 1 of leveldata.txt). The second result
	// is false when the table has no row, which must refuse the exp
	// grant rather than invent a threshold.
	ExpRequired(level int64) (int64, bool)
	// MonsterExpBasis answers the CRefLevel +0x1c field (GUST_Mob_Exp,
	// column 5). The v1.188 GameServer uses this one authored value at both
	// sides of the progression boundary: Formulae_ComputeMonsterSkillExpReward
	// divides the monster's EXP reward by it, while
	// CGObjPC_ApplyDeathPenalties multiplies it by 100 for the ordinary-death
	// loss ceiling. Missing rows must refuse either mutation atomically.
	MonsterExpBasis(level int64) (int64, bool)
}

// leveldataSkillPointColumn is the SP-cost cell (the sub_7e0f20 row's
// +0x10 field): the third tab-separated column.
const leveldataSkillPointColumn = 2

// leveldataExpColumn is the exp-requirement cell (the sub_7e0f20 row's
// +0x08 u64, read by the 0x30D2 exp walk @0x77977a..0x7797f3): the second
// tab-separated column. First rows read level 1 -> 118, 2 -> 470,
// 3 -> 1058; row 140 carries 34900085783 (> u32, hence int64).
const leveldataExpColumn = 1

// leveldataMonsterExpBasisColumn is CRefLevel::GUST_Mob_Exp (+0x1c) in
// v1.188 and the client CLevelData reward-feedback divisor (+0x28) in v1.150.
// It is column 5 in the shared textdata. The shipped rows carry level 1 ->
// 24, level 4 -> 94, level 11 -> 259 and level 90 -> 6949.
const leveldataMonsterExpBasisColumn = 5

/*
================
TextdataLevels

One lazy owner publishes both tables. Gold uses dg.txt column 1, parsed by
v1.150 CDropGoldData at 80CC60; levelgold.txt is a different native table.
================
*/
type TextdataLevels struct {
	dir string

	once             sync.Once
	spCostByLvl      map[int64]int64
	expByLvl         map[int64]int64
	mobExpBasisByLvl map[int64]int64
	goldBasisByLvl   map[int64]int64
}

/*
================
NewTextdataLevels
================
*/
func NewTextdataLevels(dir string) *TextdataLevels {
	return &TextdataLevels{dir: dir}
}

/*
================
SkillPointCost
================
*/
func (t *TextdataLevels) SkillPointCost(level int64) (int64, bool) {
	t.once.Do(t.load)
	cost, ok := t.spCostByLvl[level]
	return cost, ok
}

/*
================
ExpRequired
================
*/
func (t *TextdataLevels) ExpRequired(level int64) (int64, bool) {
	t.once.Do(t.load)
	exp, ok := t.expByLvl[level]
	return exp, ok
}

/*
================
MonsterExpBasis
================
*/
func (t *TextdataLevels) MonsterExpBasis(level int64) (int64, bool) {
	t.once.Do(t.load)
	basis, ok := t.mobExpBasisByLvl[level]
	return basis, ok
}

/*
================
WithdrawalGoldBasis
================
*/
func (t *TextdataLevels) WithdrawalGoldBasis(level int64) (int64, bool) {
	t.once.Do(t.load)
	basis, ok := t.goldBasisByLvl[level]
	return basis, ok
}

/*
================
Len
================
*/
func (t *TextdataLevels) Len() int {
	t.once.Do(t.load)
	return len(t.spCostByLvl)
}

/*
================
load
================
*/
func (t *TextdataLevels) load() {
	t.spCostByLvl = map[int64]int64{}
	t.expByLvl = map[int64]int64{}
	t.mobExpBasisByLvl = map[int64]int64{}
	t.goldBasisByLvl = map[int64]int64{}
	const goldColumnCount = 3
	for _, fields := range readTextdataFile(filepath.Join(t.dir, "dg.txt")) {
		if len(fields) != goldColumnCount {
			continue
		}
		level, validLevel := textdataInt(fields[0])
		basis, validBasis := textdataInt(fields[1])
		if validLevel && validBasis && level > 0 && basis > 0 {
			t.goldBasisByLvl[level] = basis
		}
	}
	rows := readTextdataFile(filepath.Join(t.dir, "leveldata.txt"))
	if len(rows) == 0 {
		log.Warnf("bootstrap: leveldata.txt not found under verified projection %s; mastery training and exp grants will refuse every request", t.dir)
		return
	}
	for _, fields := range rows {
		if len(fields) <= leveldataSkillPointColumn {
			continue
		}
		level, ok := textdataInt(fields[0])
		if !ok || level < 1 {
			continue
		}
		if cost, ok := textdataInt(fields[leveldataSkillPointColumn]); ok && cost >= 0 {
			t.spCostByLvl[level] = cost
		}
		if exp, ok := textdataInt(fields[leveldataExpColumn]); ok && exp > 0 {
			t.expByLvl[level] = exp
		}
		if len(fields) > leveldataMonsterExpBasisColumn {
			if basis, ok := textdataInt(fields[leveldataMonsterExpBasisColumn]); ok && basis > 0 {
				t.mobExpBasisByLvl[level] = basis
			}
		}
	}
	log.Infof("bootstrap: leveldata loaded from %s (%d level row(s))", t.dir, len(t.spCostByLvl))
}
