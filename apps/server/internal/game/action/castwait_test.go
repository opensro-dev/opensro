/*
===========================================================================

castwait_test.go - prepared casts and the hawk pay no record copy while waiting

#1943 slice items (4) and (6): a tick that only waits on a prepared
monster cast - or on the hawk's strike interval - used to detach the
whole target or caster record to read the few facts the wait verdict
needs. The verdicts now read those facts as values under the read
door; the copy is paid only where a strike commits through it. The
projectile lane's item (5) is not here: dropped at review (see the
commit message).

===========================================================================
*/
package action

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
TestMonsterCastWaitTickDoesNotCopyTheTarget

A monster's 1000 ms cast waits 500 ms in: the tick must produce no
frames, keep the cast queued and detach nothing. At release the cast
still lands exactly as before.
================
*/
func TestMonsterCastWaitTickDoesNotCopyTheTarget(t *testing.T) {
	rt, clock, c, m := newCombatTestRuntime(t, 100)
	m.Ref.DefaultSkillIDs[0] = 2
	skills := rt.deps.SkillData().(staticSkillSource)
	skill := skills[2]
	skill.ActionCastingTimeMs = 1000
	skill.Attack.Min, skill.Attack.Max, skill.Attack.Percent = 100, 100, 100
	skills[2] = skill
	start := rt.MonsterBasicAttack(testDivision, m, enterworld.ObjectIDForCharacter(c), 2, clock.NowMs())
	if !start.Accepted || len(rt.pendingMonsterCasts) != 1 {
		t.Fatalf("cast did not prepare: %+v", start)
	}
	p := rt.pendingMonsterCasts[0]
	before := enterworld.CurrentHP(c)
	allocs := testing.AllocsPerRun(50, func() {
		if out := rt.advanceMonsterCastInDivision(p, clock.NowMs()+500); out != nil {
			t.Fatalf("wait tick produced frames: %+v", out)
		}
	})
	t.Logf("monster cast wait tick: %.0f allocs/run", allocs)
	if allocs > monsterWaitAllocBound {
		t.Fatalf("wait tick allocated %.0f per run, bound %d (the target copy is back)", allocs, monsterWaitAllocBound)
	}
	if len(rt.pendingMonsterCasts) != 1 {
		t.Fatal("wait tick retired the cast")
	}
	out := rt.advanceMonsterCastInDivision(p, clock.NowMs()+1001)
	if len(out) == 0 || enterworld.CurrentHP(c) >= before {
		t.Fatalf("release did not land after the wait ticks: %+v hp %d -> %d", out, before, enterworld.CurrentHP(c))
	}
	if len(rt.pendingMonsterCasts) != 0 {
		t.Fatal("released cast stayed queued")
	}
}

// monsterWaitAllocBound sits between the two measured wait ticks: 5
// allocs/run reading values, 25 detaching the target record each tick.
const monsterWaitAllocBound = 12

/*
================
aimedHawk

A summoned Black Hawk whose owner has just attacked the fixture
monster, so its record is aimed with a fresh owner-attack stamp.
Returns the runtime, caster, target GID, a copy of the hawk record,
the attack time and the strike interval.
================
*/
func aimedHawk(t *testing.T) (*Runtime, *enterworld.Character, uint32, summonedHawk, int64, int64) {
	t.Helper()
	rt, c, target, _, now := arrowFixture(t)
	skill := shippedOffense(t, "SKILL_CH_BOW_CALL_B_01")
	// The level-1 fixture's MP cannot pay the summon; cost is not tested here.
	skill.Consumption.MP, skill.Consumption.MPPercent = 0, 0
	rt.deps.SkillData().(staticSkillSource)[skill.ID] = skill
	c.Skills = append(c.Skills, skill.ID)
	c.CurrentMP = testInt64(10000)
	if result := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: skill.ID}.Encode()); result.DiagnosticRefusal != "" {
		t.Fatalf("summon refused: %s", result.DiagnosticRefusal)
	}
	// Release the summon cast so its effect instance exists, then aim
	// through the same stamp an attack start applies.
	rt.advanceProjectileCasts(now + 100000)
	rt.noteHawkOwnerAttack(testDivision, c, target, now)
	_, row, ok := rt.activeHawk(testDivision, c.Name)
	if !ok {
		t.Fatal("hawk effect not active")
	}
	record := rt.hawks[hawkKey(testDivision, c.Name)]
	if record == nil || record.target != target {
		t.Fatalf("hawk not aimed at the target: %+v", record)
	}
	return rt, c, target, *record, now, int64(row.TimedEffect.Hawk.IntervalMs)
}

/*
================
TestHawkWaitTickDoesNotCopyTheCaster

Inside the strike interval the tick used to detach the whole caster
record before the interval gate refused it. The gate now reads the
door life facts and the record's own stamps, and detaches nothing.
================
*/
func TestHawkWaitTickDoesNotCopyTheCaster(t *testing.T) {
	rt, c, target, h, now, _ := aimedHawk(t)
	h.strikeAt = now // struck just now: the interval gate must refuse
	allocs := testing.AllocsPerRun(50, func() {
		if out := rt.advanceHawk(h, now+100); out != nil {
			t.Fatalf("wait tick produced frames: %+v", out)
		}
	})
	t.Logf("hawk wait tick: %.0f allocs/run", allocs)
	if allocs > hawkWaitAllocBound {
		t.Fatalf("wait tick allocated %.0f per run, bound %d (the caster copy is back)", allocs, hawkWaitAllocBound)
	}
	record := rt.hawks[hawkKey(testDivision, c.Name)]
	if record.target != target || record.strikeAt != 0 {
		t.Fatalf("wait tick touched the record: %+v", record)
	}
}

// hawkWaitAllocBound sits between the two measured wait ticks: 6
// allocs/run reading values, 30 detaching the caster record each tick.
const hawkWaitAllocBound = 15

/*
================
TestHawkStrikeUnchangedAtTheInterval

At one full interval the strike lands exactly as before (frames, HP,
the strike stamp), and a target that died mid-interval is cleared by
the strike tick without a strike.
================
*/
func TestHawkStrikeUnchangedAtTheInterval(t *testing.T) {
	rt, c, target, h, now, interval := aimedHawk(t)
	key := hawkKey(testDivision, c.Name)
	before, _ := rt.Monsters.Get(testDivision, target)
	at := now + interval
	out := rt.advanceHawk(h, at)
	if len(out) == 0 {
		t.Fatal("the strike tick produced nothing")
	}
	after, _ := rt.Monsters.Get(testDivision, target)
	if after.CurrentHP >= before.CurrentHP {
		t.Fatalf("strike did not damage: %d -> %d", before.CurrentHP, after.CurrentHP)
	}
	record := rt.hawks[key]
	if record.strikeAt != at || record.target != target {
		t.Fatalf("strike bookkeeping moved: %+v", record)
	}
	// The target dies mid-interval; the owner attacked within the last
	// interval, so the next strike tick resolves, clears and holds.
	rt.Monsters.ApplyDamage(testDivision, target, after.CurrentHP)
	h = *rt.hawks[key]
	h.ownerAttackAt = at + interval
	if out := rt.advanceHawk(h, at+interval); len(out) != 0 {
		t.Fatalf("struck a dead target: %+v", out)
	}
	record = rt.hawks[key]
	if record.target != 0 || record.strikeAt != at {
		t.Fatalf("dead target not cleared at the strike tick: %+v", record)
	}
}
