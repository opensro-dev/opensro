/*
===========================================================================

monsterstate_abnormal_sources_test.go - status source read-boundary tests

===========================================================================
*/
package simulation

import (
	"testing"
	"time"

	"opensro.online/server/internal/game/abnormal"
)

/*
================
populationCheckedAbnormalContext
================
*/
type populationCheckedAbnormalContext struct {
	testAbnormalContext
	t     *testing.T
	state *MonsterState
	reads int
}

/*
================
SourceExists

A source may be another monster in the same population. Its lookup must run
outside that population's transaction, including periodic status updates.
================
*/
func (c *populationCheckedAbnormalContext) SourceExists(division string, gid uint32, _ string) bool {
	c.t.Helper()
	if !c.state.mu.TryLock() {
		c.t.Fatal("status source lookup re-entered the population lock")
	}
	c.state.mu.Unlock()
	c.reads++
	_, exists := c.state.Get(division, gid)
	return exists
}

/*
================
SourceDead
================
*/
func (c *populationCheckedAbnormalContext) SourceDead(division string, gid uint32, _ string) bool {
	c.t.Helper()
	if !c.state.mu.TryLock() {
		c.t.Fatal("source life lookup re-entered population lock")
	}
	c.state.mu.Unlock()
	source, exists := c.state.Get(division, gid)
	return exists && source.CurrentHP == 0
}

/*
================
TestAbnormalSourceLookupRunsOutsidePopulationCommit
================
*/
func TestAbnormalSourceLookupRunsOutsidePopulationCommit(t *testing.T) {
	const division = "abnormal-source"
	const now = 10000
	s := damageTestState()
	s.clock = func() time.Time { return time.UnixMilli(now) }
	source := firstDamageTestMonster(t, s, division)
	ctx := &populationCheckedAbnormalContext{t: t, state: s}
	s.SetAbnormalContext(ctx)
	records := []abnormal.Record{{Status: abnormal.Burn, Level: 10, DurationMs: 7500, Rate24: 3, Scale20: 1, SourceGID: source.Gid}}
	sources := s.PrepareAbnormalSources(division, records)
	if ctx.reads != 1 {
		t.Fatalf("source lookup count %d", ctx.reads)
	}
	plans := []MonsterDamagePlan{{GID: source.Gid, Damage: 1, Abnormal: records, AbnormalSources: sources}}
	applied := s.ApplyDamageSequence(division, source.Gid, source.CurrentHP, plans)
	if len(applied) != 1 || applied[0].Instance.AbnormalMask()&abnormal.Burn.Bit() == 0 || ctx.reads != 1 {
		t.Fatal("status commit queried authority or failed to install burn")
	}
	update, ok := s.PlanAbnormalUpdate(division, source.Gid, now+2001)
	// Source resolution still occurs, but 52A288 ignores a self-sourced hit.
	if !ok || ctx.reads != 2 || len(update.Effects.Hits) != 0 {
		t.Fatalf("periodic update failed source resolution: %+v", update.Effects)
	}
	if _, ok := s.CommitAbnormalUpdate(update, now+2001); !ok || ctx.reads != 2 {
		t.Fatal("periodic commit queried authority or refused its plan")
	}
}

/*
================
TestUnpreparedAbnormalSourceRefusesWholeDamageTransaction
================
*/
func TestUnpreparedAbnormalSourceRefusesWholeDamageTransaction(t *testing.T) {
	for _, mode := range []string{"single", "batch", "sequences"} {
		t.Run(mode, func(t *testing.T) {
			const division = "missing-source"
			s := damageTestState()
			victim := firstDamageTestMonster(t, s, division)
			plan := MonsterDamagePlan{GID: victim.Gid, ExpectedHP: victim.CurrentHP, Damage: 1,
				Abnormal: []abnormal.Record{{Status: abnormal.Stun, DurationMs: 3000, SourceGID: 9}}}
			switch mode {
			case "single":
				if len(s.ApplyDamageSequence(division, victim.Gid, victim.CurrentHP, []MonsterDamagePlan{plan})) != 0 {
					t.Fatal("unprepared source accepted")
				}
			case "batch":
				if _, ok := s.ApplyDamageBatch(division, []MonsterDamagePlan{plan}); ok {
					t.Fatal("unprepared source accepted")
				}
			case "sequences":
				if _, ok := s.ApplyDamageSequences(division, [][]MonsterDamagePlan{{plan}}); ok {
					t.Fatal("unprepared source accepted")
				}
			}
			after, _ := s.Get(division, victim.Gid)
			if after.CurrentHP != victim.CurrentHP || after.Abnormal != nil {
				t.Fatal("refused transaction partially changed the victim")
			}
		})
	}
}

var _ MonsterAbnormalContext = (*populationCheckedAbnormalContext)(nil)
