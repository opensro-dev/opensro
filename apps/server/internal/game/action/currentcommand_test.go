/*
===========================================================================

currentcommand_test.go - tests for currentcommand.go

===========================================================================
*/

package action

import (
	"encoding/binary"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

func TestCurrentCommandSurvivesReleaseDequeueButNotFlightOrFinalize(t *testing.T) {
	for _, recovery := range []bool{false, true} {
		for _, refuse := range []bool{false, true} {
			name := "projectile"
			if recovery {
				name = "recovery"
			}
			if refuse {
				name += "-refused"
			}
			t.Run(name, func(t *testing.T) {
				rt, c, target, skill, now := arrowFixture(t)
				cast := wire.SkillAction{ActionId: skill.ID, HasTarget: true, TargetGid: target}
				if recovery {
					skill = shippedOffense(t, "SKILL_CH_WATER_SELFHEAL_A_01")
					c.Skills = append(c.Skills, skill.ID)
					cast = wire.SkillAction{ActionId: skill.ID}
					c.CurrentHP = testInt64(1)
				}
				// Distinct words ensure the owner preserves metadata, not just a token.
				skill.Replacement.PackedStates = 0x030201
				skill.Replacement.Ovl2Present = true
				skill.Replacement.Ovl2 = 0x060504
				skill.ReplacementPinned = true
				rt.deps.SkillData().(staticSkillSource)[skill.ID] = skill
				var start OpResult
				if recovery {
					start = rt.acceptSupportSkill(testDivision, c, rt.characterSnapshot(testDivision, c), cast, skill)
				} else {
					var decision skillCastDecision
					start, decision = rt.acceptSkillCastAt(testDivision, c, rt.characterSnapshot(testDivision, c), cast, now, rt.handlerDoors())
					if decision != skillCastAccepted {
						t.Fatal("preparation refused", decision)
					}
				}
				if len(start.Frames) != 1 {
					t.Fatal("missing opening cast", start)
				}
				token := binary.LittleEndian.Uint32(start.Frames[0].Payload[10:])
				assertCurrent := func() {
					t.Helper()
					current, ok := rt.currentSkillCommandFor(testDivision, c)
					if !ok || current.token != token || current.skillID != skill.ID || !current.pinned || current.descriptor != skill.Replacement {
						t.Fatal("current command mismatch", current, ok)
					}
				}
				assertCurrent()
				other := *c
				other.ID++
				if _, ok := rt.currentSkillCommandFor(testDivision, &other); ok {
					t.Fatal("actor identity leaked")
				}
				seen := false
				checkRelease := func(update func() bool) bool {
					seen = true
					if len(rt.pendingProjectileCasts) != 0 {
						t.Fatal("release queue not consumed")
					}
					assertCurrent()
					if !rt.hasOpenSkillCast(testDivision, c.Name) {
						t.Fatal("release temporarily lost action ownership")
					}
					if refuse {
						return false
					}
					return update()
				}
				rt.deps.(*enterworld.Deps).UpdateCharacter = func(_ *enterworld.Character, _ string, update func() bool) bool { return checkRelease(update) }
				rt.deps.(*enterworld.Deps).UpdateCharacters = func(_ []*enterworld.Character, _ string, update func() bool) bool { return checkRelease(update) }
				rt.advanceProjectileCasts(now + int64(skill.ActionCastingTimeMs) + 1)
				if !seen {
					t.Fatal("release never reached authority transaction")
				}
				if _, ok := rt.currentSkillCommandFor(testDivision, c); ok {
					t.Fatal("current command survived release")
				}
				if !refuse && !rt.hasOpenSkillCast(testDivision, c.Name) {
					t.Fatal("test did not retain flight/finalize lifetime")
				}
			})
		}
	}
}

func TestCurrentCommandCancelAndDisconnectRetireOwnership(t *testing.T) {
	for _, disconnect := range []bool{false, true} {
		rt, c, target, skill, now := arrowFixture(t)
		_, decision := rt.acceptSkillCastAt(testDivision, c, rt.characterSnapshot(testDivision, c), wire.SkillAction{ActionId: skill.ID, HasTarget: true, TargetGid: target}, now, rt.handlerDoors())
		if decision != skillCastAccepted {
			t.Fatal("preparation refused")
		}
		if disconnect {
			rt.clearSkillFinalizes(testDivision, c.Name)
		} else {
			rt.cancelPreparingProjectile(testDivision, c.Name)
		}
		if _, ok := rt.currentSkillCommandFor(testDivision, c); ok {
			t.Fatal("retired command remains current")
		}
		if frames := rt.advanceProjectileCasts(now + 10000); len(frames) != 0 {
			t.Fatal("retired preparation released", frames)
		}
	}
}
