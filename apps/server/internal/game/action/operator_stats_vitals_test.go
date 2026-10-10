/*
===========================================================================

operator_stats_vitals_test.go - offline reset preserves restored gauge bonuses

Exercise the real logout/restore owners and compare their keeper to the pure
operator projection. A failed projection must not alter the staged character.

===========================================================================
*/
package action

import (
	"reflect"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/statuseffect"
)

/*
================
TestOperatorResetVitalsPersistentGaugesAcrossReconnect
================
*/
func TestOperatorResetVitalsPersistentGaugesAcrossReconnect(t *testing.T) {
	for _, spent := range []int64{0, 10} {
		t.Run(map[bool]string{true: "already-reset", false: "spent-points"}[spent == 0], func(t *testing.T) {
			rt, _, c, request := statItemFixture(t, enterworld.SkillTimedEffect{
				HP: enterworld.SkillFlatRate{Present: true, Flat: 500, Percent: 25},
				MP: enterworld.SkillFlatRate{Present: true, Flat: 200, Percent: 50},
			})
			strength, intellect := domain.BaseStat+spent, domain.BaseStat+spent
			c.Strength, c.Intellect = &strength, &intellect
			result := rt.HandleItemUse(testDivision, c, request)
			if len(result.Frames) == 0 || result.Frames[0].Payload[0] != 1 {
				t.Fatal("persistent stat item refused", result)
			}
			beforeHP, beforeMP, _, _ := rt.playerKeeperVitals(testDivision, c)
			c.CurrentHP, c.CurrentMP = &beforeHP, &beforeMP
			rt.ForgetCharacter(testDivision, c.Name)
			jobs := append([]domain.TimedSkillJob(nil), c.TimedSkillJobs...)
			if len(jobs) != 1 || len(rt.effects.Snapshot(testDivision, c.Name)) != 0 {
				t.Fatal("logout fixture did not retain only the durable job")
			}
			deps := rt.deps.(*enterworld.Deps)
			// The parent holds this door while projecting. Reentry must fail
			// immediately in the test rather than hanging on a real store lock.
			deps.UpdateCharacter = func(_ *enterworld.Character, _ string, update func() bool) bool {
				deps.UpdateCharacter = func(*enterworld.Character, string, func() bool) bool {
					t.Fatal("projection reentered authority")
					return false
				}
				defer func() { deps.UpdateCharacter = nil }()
				return update()
			}
			if err := rt.OperatorResetStats(testDivision, c.Name); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(jobs, c.TimedSkillJobs) || len(rt.effects.Snapshot(testDivision, c.Name)) != 0 {
				t.Fatal("offline projection changed jobs or installed live effects")
			}
			resetHP, resetMP := *c.CurrentHP, *c.CurrentMP
			if spent == 0 && (resetHP != beforeHP || resetMP != beforeMP) {
				t.Fatal("already-reset character lost buffed gauges")
			}
			rt.RestoreTimedSkillJobs(testDivision, c.Name)
			maxHP, maxMP, _, _ := rt.playerKeeperVitals(testDivision, c)
			if resetHP != maxHP || resetMP != maxMP || *c.CurrentHP != resetHP || *c.CurrentMP != resetMP {
				t.Fatalf("projected %d/%d, restored maxima %d/%d, currents %d/%d", resetHP, resetMP, maxHP, maxMP, *c.CurrentHP, *c.CurrentMP)
			}
		})
	}
}

/*
================
TestOperatorResetVitalsProjectsOrderedCappedBoosts
================
*/
func TestOperatorResetVitalsProjectsOrderedCappedBoosts(t *testing.T) {
	c := testCharacter()
	rt, _ := newTestRuntime(c, testItems())
	skills := staticSkillSource{}
	for _, id := range []uint32{100, 101} {
		skills[id] = enterworld.SkillRow{ID: id, Group: id, EffectDurationMs: 10000,
			TimedEffect: enterworld.SkillTimedEffect{Pinned: true, Persistent: true, ItemProgram: true,
				Strength:  enterworld.SkillStatBoost{Present: true, Value: 100, CapPercent: 50},
				Intellect: enterworld.SkillStatBoost{Present: true, Value: 100, CapPercent: 50}}}
		c.TimedSkillJobs = append(c.TimedSkillJobs, domain.TimedSkillJob{SkillID: id, Token: id, RemainingMs: 7000})
	}
	rt.deps.(*enterworld.Deps).Skills = skills
	highHP, highMP := int64(100000), int64(100000)
	c.CurrentHP, c.CurrentMP = &highHP, &highMP
	next := c.Snapshot()
	if err := rt.operatorResetVitals(next); err != nil {
		t.Fatal(err)
	}
	if len(rt.effects.Snapshot(testDivision, c.Name)) != 0 || *c.CurrentHP != highHP {
		t.Fatal("projection touched the live owner")
	}
	rt.RestoreTimedSkillJobs(testDivision, c.Name)
	maxHP, maxMP, _, _ := rt.playerKeeperVitals(testDivision, c)
	if *next.CurrentHP != maxHP || *next.CurrentMP != maxMP {
		t.Fatalf("projection %d/%d disagrees with sequential restoration %d/%d", *next.CurrentHP, *next.CurrentMP, maxHP, maxMP)
	}
}

/*
================
TestOperatorResetVitalsRefusalIsAtomic
================
*/
func TestOperatorResetVitalsRefusalIsAtomic(t *testing.T) {
	for _, invalid := range []string{"unknown", "unsupported", "duplicate", "group", "linked", "short-duration", "body", "keeper"} {
		t.Run(invalid, func(t *testing.T) {
			c := testCharacter()
			rt, _ := newTestRuntime(c, testItems())
			row := enterworld.SkillRow{ID: 100, Group: 100, EffectDurationMs: 10000,
				TimedEffect: enterworld.SkillTimedEffect{Pinned: true, Persistent: true, ItemProgram: true,
					HP: enterworld.SkillFlatRate{Present: true, Flat: 500}}}
			c.TimedSkillJobs = []domain.TimedSkillJob{{SkillID: 100, Token: 50, RemainingMs: 7000}}
			switch invalid {
			case "unknown":
				c.TimedSkillJobs = append(c.TimedSkillJobs, domain.TimedSkillJob{SkillID: 999, RemainingMs: 7000})
			case "unsupported":
				row.TimedEffect.Pinned = false
			case "duplicate":
				c.TimedSkillJobs = append(c.TimedSkillJobs, c.TimedSkillJobs[0])
			case "group":
				c.TimedSkillJobs = append(c.TimedSkillJobs, domain.TimedSkillJob{SkillID: 101, RemainingMs: 7000})
			case "linked":
				row.Replacement.Lnks = true
			case "short-duration":
				c.TimedSkillJobs[0].RemainingMs = 999
			case "body":
				row.BodyStatus.Present = true
			case "keeper":
				c.Strength = nil
			}
			other := row
			other.ID = 101
			rt.deps.(*enterworld.Deps).Skills = staticSkillSource{100: row, 101: other}
			high := int64(100000)
			c.CurrentHP, c.CurrentMP = &high, &high
			before := c.Snapshot()
			if err := rt.operatorResetVitals(c); err == nil {
				t.Fatal("unprojectable job admitted")
			}
			if !reflect.DeepEqual(before, c.Snapshot()) || len(rt.effects.Snapshot(testDivision, c.Name)) != 0 {
				t.Fatal("refusal mutated character or runtime")
			}
		})
	}
}

/*
================
TestOperatorResetVitalsAllowsPersistentMovementOnly
================
*/
func TestOperatorResetVitalsAllowsPersistentMovementOnly(t *testing.T) {
	c := testCharacter()
	rt, _ := newTestRuntime(c, testItems())
	rt.deps.(*enterworld.Deps).Skills = staticSkillSource{100: {
		ID: 100, Group: 100, EffectDurationMs: 10000,
		MovementModifier: enterworld.SkillMovementModifier{Present: true, Supported: true,
			Persistent: true, Percent: 50, Kind: statuseffect.MovementHaste},
	}}
	c.TimedSkillJobs = []domain.TimedSkillJob{{SkillID: 100, Token: 80, RemainingMs: 7000}}
	before := c.Snapshot()
	if err := rt.operatorResetVitals(c); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, c.Snapshot()) || len(rt.effects.Snapshot(testDivision, c.Name)) != 0 {
		t.Fatal("movement-only projection changed character or runtime")
	}
}

/*
================
TestOperatorResetVitalsPreservesNilAndDeadGauges
================
*/
func TestOperatorResetVitalsPreservesNilAndDeadGauges(t *testing.T) {
	c := testCharacter()
	rt, _ := newTestRuntime(c, testItems())
	zero := int64(0)
	c.CurrentHP, c.CurrentMP = &zero, nil
	c.TimedSkillJobs = []domain.TimedSkillJob{{SkillID: 999, RemainingMs: 0}}
	if err := rt.operatorResetVitals(c); err != nil {
		t.Fatal(err)
	}
	if c.CurrentHP == nil || *c.CurrentHP != 0 || c.CurrentMP != nil {
		t.Fatal("projection changed death or implicit full mana")
	}
}
