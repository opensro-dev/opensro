/*
===========================================================================

jobmonsterdamage_test.go - a job monster's doubled damage under the rule

Port-only, not native (#2474): with SRO_JOB_MONSTER_AGGRESSION on, a
thief or hunter monster's outgoing damage factors double - over a curse
factor too - so the formula's final damage doubles on both lanes. Off,
and for ordinary monsters either way, the projection is native.

===========================================================================
*/
package combat

import (
	"testing"

	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/world/monster"
)

/*
================
TestJobMonsterDamageDoubledOnlyUnderTheRule
================
*/
func TestJobMonsterDamageDoubledOnlyUnderTheRule(t *testing.T) {
	thief := monster.Instance{Ref: monster.MonsterRef{CombatPinned: true, TidWord: 0x00c6, TypeID4: 2,
		PhysicalDefense: 100, MagicalDefense: 200}}
	ordinary := monster.Instance{Ref: monster.MonsterRef{CombatPinned: true, TidWord: 0x00c6, TypeID4: 1,
		PhysicalDefense: 100, MagicalDefense: 200}}

	t.Setenv(monster.EnvJobMonsterAggression, "")
	s, err := MonsterInstanceStats(thief)
	if err != nil || s.PhysicalOutgoing != 0 || s.MagicalOutgoing != 0 {
		t.Fatalf("rule off: %+v %v", s, err)
	}

	t.Setenv(monster.EnvJobMonsterAggression, "on")
	s, err = MonsterInstanceStats(thief)
	if err != nil || s.PhysicalOutgoing != 2 || s.MagicalOutgoing != 2 {
		t.Fatalf("rule on: %+v %v, want both outgoing factors doubled", s, err)
	}
	s, err = MonsterInstanceStats(ordinary)
	if err != nil || s.PhysicalOutgoing != 0 || s.MagicalOutgoing != 0 {
		t.Fatalf("ordinary monster under the rule: %+v %v", s, err)
	}

	cursed := thief
	cursed.Abnormal = blockWith(abnormal.Record{Status: abnormal.Impotent, Grade: 8, DurationMs: 30000, Param38: 35})
	s, err = MonsterInstanceStats(cursed)
	if err != nil || s.PhysicalOutgoing != float32(1.3) || s.MagicalOutgoing != float32(1.3) {
		t.Fatalf("cursed job monster under the rule: %+v %v, want the curse factor doubled", s, err)
	}
}
