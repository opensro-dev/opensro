/*
===========================================================================

monsterstate_abnormal_test.go - tests for monsterstate_abnormal.go

===========================================================================
*/

package simulation

import (
	"encoding/binary"
	"math"
	"testing"
	"time"

	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
)

// testAbnormalContext resolves every caster, reports dead only when asked
// and keeps rolls deterministic.
/*
================
testAbnormalContext
================
*/
type testAbnormalContext struct {
	dead bool
	roll bool
}

/*
================
SourceExists
================
*/
func (c testAbnormalContext) SourceExists(string, uint32, string) bool { return true }

/*
================
SourceDead
================
*/
func (c testAbnormalContext) SourceDead(string, uint32, string) bool { return c.dead }

/*
================
Roll
================
*/
func (c testAbnormalContext) Roll(string, uint32, uint32, int32) bool { return c.roll }

/*
================
Param
================
*/
func (c testAbnormalContext) Param(instance monster.Instance, id uint16) float32 {
	if id == 8 {
		return float32(instance.Ref.MagicalParry)
	}
	return 0
}

/*
================
applyRecords
================
*/
func applyRecords(t *testing.T, s *MonsterState, division string, gid uint32, credit uint32, damage uint32, records ...abnormal.Record) MonsterDamageResult {
	t.Helper()
	current, _ := s.Get(division, gid)
	r := s.ApplyDamageSequence(division, gid, current.CurrentHP, []MonsterDamagePlan{{GID: gid, CreditGID: credit, Damage: damage, Abnormal: records, AbnormalSources: s.PrepareAbnormalSources(division, records)}})
	if len(r) != 1 {
		t.Fatal("impact refused")
	}
	return r[0]
}

// The impact door installs statuses atomically with HP; a stale HP snapshot
// installs nothing, and a fatal impact never installs.
/*
================
TestAbnormalInstallsAtomicallyWithImpact
================
*/
func TestAbnormalInstallsAtomicallyWithImpact(t *testing.T) {
	s := damageTestState()
	s.clock = func() time.Time { return time.UnixMilli(10000) }
	s.SetAbnormalContext(testAbnormalContext{})
	m := firstDamageTestMonster(t, s, "abnormal")
	stun := abnormal.Record{Status: abnormal.Stun, Grade: 3, DurationMs: 3000, SourceGID: 9, SourceName: "caster"}
	if r := s.ApplyDamageSequence("abnormal", m.Gid, m.CurrentHP+1, []MonsterDamagePlan{{GID: m.Gid, Damage: 1, Abnormal: []abnormal.Record{stun}}}); len(r) != 0 {
		t.Fatal("stale HP admitted")
	}
	if current, _ := s.Get("abnormal", m.Gid); current.Abnormal != nil {
		t.Fatal("refused impact installed a status")
	}
	r := applyRecords(t, s, "abnormal", m.Gid, 9, 1, stun)
	if r.Instance.AbnormalMask() != 0x4000 || !r.Abnormal.MaskChanged || !r.Abnormal.CancelActions || r.Instance.Motion.StateAt(math.MaxInt64-1) != 9 {
		t.Fatalf("stun install %+v %+v", r.Instance.Motion, r.Abnormal)
	}
	if len(s.AbnormalCandidates()) != 1 {
		t.Fatal("active block not indexed")
	}
	fatal := applyRecords(t, s, "abnormal", m.Gid, 9, r.Instance.CurrentHP, abnormal.Record{Status: abnormal.Sleep, Grade: 4, DurationMs: 3000, SourceGID: 9})
	if !fatal.Fatal || fatal.Instance.Abnormal != nil || len(s.AbnormalCandidates()) != 0 {
		t.Fatal("death retained abnormal state")
	}
	if binary.LittleEndian.Uint32(MonsterAbnormalPayload(fatal.Instance)[7:]) != 0 {
		t.Fatal("corpse published a mask")
	}
}

// Burn ticks at most once per 2 s through plan/commit; a stale plan (the
// block changed in between) cannot commit, and a fatal tick commits once.
/*
================
TestAbnormalUpdatePlanCommitAndStaleness
================
*/
func TestAbnormalUpdatePlanCommitAndStaleness(t *testing.T) {
	s := damageTestState()
	s.clock = func() time.Time { return time.UnixMilli(10000) }
	s.SetAbnormalContext(testAbnormalContext{})
	m := firstDamageTestMonster(t, s, "abnormal")
	burn := abnormal.Record{Status: abnormal.Burn, Level: 31, DurationMs: 31 * 750, Rate24: 9, Scale20: 1, SourceGID: 9, SourceName: "caster"}
	applyRecords(t, s, "abnormal", m.Gid, 9, 0, burn)
	plan, ok := s.PlanAbnormalUpdate("abnormal", m.Gid, 12001)
	if !ok || len(plan.Effects.Hits) != 1 || plan.Effects.Hits[0].Damage != 9 || !plan.Effects.Hits[0].Credited {
		t.Fatalf("burn plan %+v", plan.Effects)
	}
	// A replacement between plan and commit invalidates the plan.
	applyRecords(t, s, "abnormal", m.Gid, 9, 0, abnormal.Record{Status: abnormal.Burn, Level: 40, DurationMs: 40 * 750, Rate24: 9, Scale20: 1, SourceGID: 9, SourceName: "caster"})
	if _, ok := s.CommitAbnormalUpdate(plan, 12001); ok {
		t.Fatal("stale plan committed")
	}
	plan, _ = s.PlanAbnormalUpdate("abnormal", m.Gid, 12001)
	hit, ok := s.CommitAbnormalUpdate(plan, 12001)
	if !ok || hit.Applied != 9 {
		t.Fatal("tick", hit)
	}
	plan, _ = s.PlanAbnormalUpdate("abnormal", m.Gid, 14001)
	if len(plan.Effects.Hits) != 0 {
		t.Fatal("tick at the 2000 ms equality")
	}
	s.ForgetAbnormalSource("abnormal", 0, "CASTER")
	current, _ := s.Get("abnormal", m.Gid)
	if current.Abnormal == nil || current.Abnormal.Slots[abnormal.Burn].SourceName != "" || current.Abnormal.Slots[abnormal.Burn].SourceGID != 0 {
		t.Fatal("disconnect cured the victim or kept the source")
	}
}

// The v1.150 vitals mask carries grade bytes in ascending bit order over
// 017FCFC0 (bleeding 11, curses 19/20; dark 13 carries none).
/*
================
TestAbnormalPayloadGradeOrder
================
*/
func TestAbnormalPayloadGradeOrder(t *testing.T) {
	s := damageTestState()
	s.clock = func() time.Time { return time.UnixMilli(10000) }
	s.SetAbnormalContext(testAbnormalContext{})
	m := firstDamageTestMonster(t, s, "abnormal")
	r := applyRecords(t, s, "abnormal", m.Gid, 9, 0,
		abnormal.Record{Status: abnormal.Impotent, Grade: 8, DurationMs: 30000, SourceGID: 9},
		abnormal.Record{Status: abnormal.Division, Grade: 7, DurationMs: 30000, SourceGID: 9},
		abnormal.Record{Status: abnormal.Bleeding, Grade: 6, DurationMs: 30000, PeriodMs: 2000, SourceGID: 9},
		abnormal.Record{Status: abnormal.Dark, Grade: 5, DurationMs: 30000, SourceGID: 9},
		abnormal.Record{Status: abnormal.Burn, Level: 3, DurationMs: 3000, SourceGID: 9})
	p := MonsterAbnormalPayload(r.Instance)
	if len(p) != 14 || binary.LittleEndian.Uint32(p[7:]) != 0x182808 || p[11] != 6 || p[12] != 8 || p[13] != 7 {
		t.Fatalf("mask/grade order %x", p)
	}
}

// Root halts an in-flight mover at its live pose, and the navigation
// door refuses new legs while freeze, sleep, root or stun hold (4B0EA0).
/*
================
TestRootHaltsAndBlocksNavigation
================
*/
func TestRootHaltsAndBlocksNavigation(t *testing.T) {
	s := damageTestState()
	s.clock = func() time.Time { return time.UnixMilli(10000) }
	s.SetAbnormalContext(testAbnormalContext{})
	m := firstDamageTestMonster(t, s, "abnormal")
	instance, _ := s.Get("abnormal", m.Gid)
	blocked := instance
	blocked.Abnormal = &abnormal.Block{Mask: abnormal.Root.Bit()}
	if !blocked.MovementBlocked() || instance.MovementBlocked() {
		t.Fatal("movement gate")
	}
	for _, status := range []abnormal.Status{abnormal.Freeze, abnormal.Sleep, abnormal.Stun} {
		blocked.Abnormal = &abnormal.Block{Mask: status.Bit()}
		if !blocked.MovementBlocked() {
			t.Fatal("gate ignores", status)
		}
	}
	blocked.Abnormal = &abnormal.Block{Mask: abnormal.Slow.Bit()}
	if blocked.MovementBlocked() {
		t.Fatal("slow must not block movement")
	}
	_ = wire.OpSingleObjectSpawn
}

// burnTick installs a burn through the impact door and commits one planned
// update at now; rate 5533 is the level-140 table value.
/*
================
burnTick
================
*/
func burnTick(t *testing.T, s *MonsterState, division string, gid, source uint32, rate uint32, now int64) (MonsterDamageResult, bool) {
	t.Helper()
	if s.abnormalContext == nil {
		s.SetAbnormalContext(testAbnormalContext{})
	}
	record := abnormal.Record{Status: abnormal.Burn, Level: 30, DurationMs: 30 * 750, Rate24: rate, Scale20: 1, SourceGID: source, SourceName: "source"}
	applyRecords(t, s, division, gid, source, 0, record)
	plan, ok := s.PlanAbnormalUpdate(division, gid, now)
	if !ok {
		return MonsterDamageResult{}, false
	}
	return s.CommitAbnormalUpdate(plan, now)
}
