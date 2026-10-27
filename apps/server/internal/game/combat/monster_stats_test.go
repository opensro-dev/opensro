/*
===========================================================================

monster_stats_test.go - monster stats under abnormal states and curses

===========================================================================
*/

package combat

import (
	"testing"

	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/monster"
)

// statsOwner installs records without side effects the stats test observes.
type statsOwner struct{}

func (statsOwner) Alive() bool                                      { return true }
func (statsOwner) IsPlayer() bool                                   { return false }
func (statsOwner) IsMonster() bool                                  { return true }
func (statsOwner) CurrentHP() uint32                                { return 100 }
func (statsOwner) MaxHP() uint32                                    { return 100 }
func (statsOwner) MaxMP() uint32                                    { return 0 }
func (statsOwner) Param(uint16) float32                             { return 100 }
func (statsOwner) SourceExists(uint32) bool                         { return true }
func (statsOwner) SourceDead(uint32) bool                           { return false }
func (statsOwner) Roll(uint32, int32) bool                          { return false }
func (statsOwner) Now() int64                                       { return 0 }
func (statsOwner) ParamsChanged(bool)                               {}
func (statsOwner) SetMotion(uint8, uint8, float32)                  {}
func (statsOwner) CancelActions(bool)                               {}
func (statsOwner) StopMove()                                        {}
func (statsOwner) AIEvent(uint8, uint8, uint32)                     {}
func (statsOwner) Hit(uint32, bool, uint32, uint8, abnormal.Status) {}
func (statsOwner) ConsumeResources(int32, int32, uint8)             {}
func (statsOwner) Detonate(abnormal.Slot)                           {}

func blockWith(records ...abnormal.Record) *abnormal.Block {
	var b abnormal.Block
	for _, r := range records {
		b.Apply(statsOwner{}, r, 0)
	}
	return &b
}

// Bleeding writes percent-sum -N to parameters 5 and 6; the native
// minimum clamps an over-100 % cut to zero and retirement restores the base.
func TestBleedingDefenseCutThroughBlock(t *testing.T) {
	m := monster.Instance{Ref: monster.MonsterRef{CombatPinned: true, PhysicalDefense: 100, MagicalDefense: 200},
		Abnormal: blockWith(abnormal.Record{Status: abnormal.Bleeding, Grade: 8, DurationMs: 30000, Param40: 20})}
	s, err := MonsterInstanceStats(m)
	if err != nil || s.PhysicalDefense != 80 || s.MagicalDefense != 160 {
		t.Fatal("both defense channels", s, err)
	}
	m.Abnormal = blockWith(abnormal.Record{Status: abnormal.Bleeding, Grade: 8, DurationMs: 30000, Param40: 150})
	s, err = MonsterInstanceStats(m)
	if err != nil || s.PhysicalDefense != 0 || s.MagicalDefense != 0 {
		t.Fatal("negative defense was not clamped")
	}
	m.Abnormal = nil
	s, err = MonsterInstanceStats(m)
	if err != nil || s.PhysicalDefense != 100 || s.MagicalDefense != 200 {
		t.Fatal("retirement did not restore defenses")
	}
}

// Impotent scales outgoing damage by -M %, Division scales
// incoming damage by +M % (percent-product channel, source 5).
func TestCurseFinalDamageFactors(t *testing.T) {
	m := monster.Instance{Ref: monster.MonsterRef{CombatPinned: true, PhysicalDefense: 100, MagicalDefense: 200},
		Abnormal: blockWith(abnormal.Record{Status: abnormal.Impotent, Grade: 8, DurationMs: 30000, Param38: 35},
			abnormal.Record{Status: abnormal.Division, Grade: 8, DurationMs: 30000, Param38: 35})}
	s, err := MonsterInstanceStats(m)
	if err != nil || s.PhysicalDefense != 100 || s.MagicalDefense != 200 || s.PhysicalOutgoing != float32(.65) || s.MagicalOutgoing != float32(.65) || s.PhysicalIncoming != float32(1.35) || s.MagicalIncoming != float32(1.35) {
		t.Fatalf("curse projection %+v %v", s, err)
	}
	for _, flags := range []uint32{4, 8} {
		a := Stats{Level: 1, PhysicalAttackMin: 1000, PhysicalAttackMax: 1000, MagicalAttackMin: 1000, MagicalAttackMax: 1000, PhysicalOutgoing: s.PhysicalOutgoing, MagicalOutgoing: s.MagicalOutgoing}
		d := Stats{Level: 1, PhysicalDefense: 100, MagicalDefense: 100, PhysicalIncoming: s.PhysicalIncoming, MagicalIncoming: s.MagicalIncoming}
		result, err := ResolveMonster(a, d, enterworld.SkillAttack{Present: true, Flags: flags, Percent: 100}, func() (uint32, error) { return 0, nil })
		if err != nil || result.Damage != 789 {
			t.Fatalf("lane %d result=%+v err=%v", flags, result, err)
		}
		d.PhysicalDefense, d.MagicalDefense = 100000, 100000
		result, err = ResolveMonster(a, d, enterworld.SkillAttack{Present: true, Flags: flags, Percent: 100}, func() (uint32, error) { return 32767, nil })
		if err != nil || result.Damage != 87 {
			t.Fatalf("fallback lane%d: %+v %v", flags, result, err)
		}
	}
	m.Abnormal = nil
	s, err = MonsterInstanceStats(m)
	if err != nil || s.PhysicalOutgoing != 0 || s.MagicalIncoming != 0 {
		t.Fatal("retirement", s, err)
	}
}

/*
==================
TestStatusParameterWrites

Electric shock scales parameter 9 by (100-N) %; dark cuts
hit rate (B) by percent-sum; decay cuts its authored amount,
capped at current - 0.3*current (so at most 70 % of the defense).
==================
*/
func TestStatusParameterWrites(t *testing.T) {
	base := monster.MonsterRef{CombatPinned: true, PhysicalDefense: 1000, MagicalDefense: 200, EvasionRate: 80, HitRate: 90}
	var b abnormal.Block
	owner := decayOwner{defense: 1000}
	b.Apply(owner, abnormal.Record{Status: abnormal.ElectricShock, Level: 3, DurationMs: 9000, Param28: 25}, 0)
	b.Apply(owner, abnormal.Record{Status: abnormal.Dark, Grade: 2, DurationMs: 9000, Param48: 10}, 0)
	b.Apply(owner, abnormal.Record{Status: abnormal.Decay, Grade: 2, DurationMs: 9000, Param34: 500}, 0)
	s, err := MonsterInstanceStats(monster.Instance{Ref: base, Abnormal: &b})
	if err != nil || s.EvasionRate != 60 || s.HitRate != 81 || s.PhysicalDefense != 500 {
		t.Fatalf("status writes %+v %v", s, err)
	}
}

type decayOwner struct {
	statsOwner
	defense float32
}

func (o decayOwner) Param(id uint16) float32 {
	if id == 5 {
		return o.defense
	}
	return 0
}
