package simulation

import (
	"opensro.online/server/internal/game/world/monster"
	"testing"
)

func TestConditionalSelectionOrderRefusalEntropyAndArchiveIsolation(t *testing.T) {
	s := damageTestState()
	m := firstDamageTestMonster(t, s, "conditional")
	s.mu.Lock()
	pop := s.populationForObject("conditional", m.Gid)
	m.Ref.DefaultSkillIDs = [10]uint32{22, 11}
	m.Nest.ConditionalSkills[0] = monster.ConditionalSkill{SkillID: 11, Data: 100}
	m.Nest.ConditionalSkills[1] = monster.ConditionalSkill{SkillID: 22, Data: 100}
	pop.instances.set(m.Gid, m)
	// Any archive read panics immediately. Populate the realistic sleeping
	// count, but neither selection nor effect retirement may inspect it.
	pop.instances.cold = make(map[uint32]archivedMonster)
	for gid := uint32(1000000); gid < 1051672; gid++ {
		pop.instances.cold[gid] = archivedMonster{}
	}
	s.mu.Unlock()
	draws := 0
	ops := MonsterMoverOps{Monsters: s, Rand: func() float64 { draws++; return .4 }}
	calls := []uint32{}
	ops.AttackPlan = func(_ monster.Instance, id uint32, pick AttackPick) (MonsterAttackPlan, bool) {
		calls = append(calls, id)
		if id == 22 {
			if pick.Sample != 0 {
				t.Fatal("conditional choice entropy")
			}
			return MonsterAttackPlan{SkillID: 22, SelfEffect: true, CooldownMs: 2500, ActionLifecycleMs: 2000}, true
		}
		if id == 11 {
			return MonsterAttackPlan{}, false
		} // command/program refusal still consumes condition
		return MonsterAttackPlan{SkillID: 99, Reach: 10, CooldownMs: 1000, ActionLifecycleMs: 500}, true
	}
	// Existing active selection bypasses both conditions and choice entropy.
	ops.selectMonsterAttack("conditional", m, 99, monster.Pose{}, playerPose{})
	live, _ := s.Get("conditional", m.Gid)
	if live.ConditionalUsed != 0 || draws != 0 {
		t.Fatal("retained selection consumed a condition")
	}
	p, ok := ops.selectMonsterAttack("conditional", m, 0, monster.Pose{}, playerPose{})
	if !ok || p.SkillID != 22 || draws != 0 {
		t.Fatal("default-slot order", p, draws)
	}
	var mover monster.MoverState
	ops.adoptMonsterAttack(&mover, p)
	if draws != 1 {
		t.Fatal("adoption must draw once")
	}
	for range 3 {
		retained, _ := ops.selectMonsterAttack("conditional", m, 22, monster.Pose{}, playerPose{})
		ops.adoptMonsterAttack(&mover, retained)
	}
	if draws != 1 {
		t.Fatal("retained skill redrew jitter")
	}
	if _, ok := ops.selectMonsterAttack("conditional", m, 0, monster.Pose{}, playerPose{}); ok {
		t.Fatal("refused condition replaced with ordinary attack")
	}
	live, _ = s.Get("conditional", m.Gid)
	if live.ConditionalUsed != 3 {
		t.Fatal("condition consumption lost", live.ConditionalUsed)
	}
	p, ok = ops.selectMonsterAttack("conditional", m, 0, monster.Pose{}, playerPose{})
	if !ok || p.SkillID != 99 || draws != 2 {
		t.Fatal("ordinary fallback after consumed conditions", p, draws)
	}
	e := monster.SelfEffect{SkillID: 22, Token: 42, Tag: 0x6372, First: 10, StartedAtMs: 1000, UntilMs: 9000}
	if !s.InstallMonsterSelfEffect("conditional", m.Gid, e, 1000) {
		t.Fatal("install")
	}
	copy, _ := s.Get("conditional", m.Gid)
	copy.SelfEffects[1].First = 999
	live, _ = s.Get("conditional", m.Gid)
	if live.SelfEffects[1].First != 10 {
		t.Fatal("snapshot aliases owner")
	}
	if len(s.RetireMonsterSelfEffects(9000)) != 0 {
		t.Fatal("early expiry")
	}
	if rows := s.RetireMonsterSelfEffects(9001); len(rows) != 1 || rows[0].Tokens[0] != 42 {
		t.Fatal("expiry identity", rows)
	}
	// Death also retires an installed effect, without resetting once flags.
	e.SkillID, e.Token, e.UntilMs = 11, 43, 10000
	if !s.InstallMonsterSelfEffect("conditional", m.Gid, e, 9001) {
		t.Fatal("second install")
	}
	s.ApplyDamage("conditional", m.Gid, 1000)
	if rows := s.RetireMonsterSelfEffects(9002); len(rows) != 1 || rows[0].Tokens[0] != 43 {
		t.Fatal("death cleanup", rows)
	}
	if len(pop.instances.cold) != 51672 {
		t.Fatal("unrelated archive changed")
	}
	if len(pop.instances.selfEffects) != 0 {
		t.Fatal("sparse effect index leaked")
	}
	_ = calls
}

func TestSelfEffectAndConsumedConditionCannotBeArchived(t *testing.T) {
	owner := NewMonsterState(monster.Template{})
	if err := owner.EnableDormantStorage(); err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	storage := newMonsterStorage(nil)
	storage.archive = owner.archive
	for _, row := range []monster.Instance{{Gid: 1, ConditionalUsed: 1}, {Gid: 2, SelfEffects: monster.SelfEffects{{Token: 1}}}} {
		storage.set(row.Gid, row)
		storage.freeze(row.Gid)
		if _, ok := storage.hot[row.Gid]; !ok {
			t.Fatal("mutable lifetime lost to archive")
		}
		storage.remove(row.Gid)
	}
	if len(storage.selfEffects) != 0 {
		t.Fatal("removed actor effect retained")
	}
}
