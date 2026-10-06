/*
===========================================================================

skillreplacement_test.go - replacement admission and effect retirement

Checks current-command conflicts, rank rejection and the old token's
retirement before a replacement publishes its new state.

===========================================================================
*/
package action

import (
	"testing"

	"opensro.online/server/internal/game/item/statuseffect"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
TestSelfEffectAdmissionUsesCurrentCommandConflicts
================
*/
func TestSelfEffectAdmissionUsesCurrentCommandConflicts(t *testing.T) {
	rt, c, target, arrow, now := arrowFixture(t)
	row := shippedOffense(t, "SKILL_CH_LIGHTNING_GYEONGGONG_A_01")
	// A distinct real state index discriminates current ownership from the
	// finalization queue. Keep the complete admitted programs unchanged.
	row.Replacement.PackedStates = 7
	arrow.Replacement.PackedStates = 7
	rt.deps.SkillData().(staticSkillSource)[arrow.ID] = arrow
	rt.deps.SkillData().(staticSkillSource)[row.ID] = row
	c.Skills = append(c.Skills, row.ID)
	c.CurrentMP = testInt64(1000)
	_, decision := rt.acceptSkillCastAt(testDivision, c, rt.characterSnapshot(testDivision, c), wire.SkillAction{ActionId: arrow.ID, HasTarget: true, TargetGid: target}, now)
	if decision != skillCastAccepted {
		t.Fatal("attack not preparing")
	}
	mp := *c.CurrentMP
	denied := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: row.ID}.Encode())
	if len(rt.effects.Snapshot(testDivision, c.Name)) != 0 || *c.CurrentMP != mp || len(denied.Frames) != 1 || len(denied.Frames[0].Payload) != 2 || denied.Frames[0].Payload[0] != 2 || denied.Frames[0].Payload[1] != 0x0c {
		t.Fatal("current cast conflict ignored", denied)
	}
	rt.advanceProjectileCasts(now + int64(arrow.ActionCastingTimeMs) + 1)
	if !rt.hasOpenSkillCast(testDivision, c.Name) {
		t.Fatal("missing retained flight")
	}
	accepted := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: row.ID}.Encode())
	if len(rt.effects.Snapshot(testDivision, c.Name)) != 1 || len(accepted.Frames) == 0 || accepted.Frames[0].Payload[0] != 1 {
		t.Fatal("retired current pointer still blocked buff", accepted)
	}
}

/*
================
TestSelfEffectRankReplacementRetiresOldToken
================
*/
func TestSelfEffectRankReplacementRetiresOldToken(t *testing.T) {
	for _, lower := range []bool{false, true} {
		rt, _, c, _ := newCombatTestRuntime(t, 100000)
		row := shippedOffense(t, "SKILL_CH_LIGHTNING_GYEONGGONG_A_01")
		old := row
		old.ID += 100000
		old.Replacement.Rank = row.Replacement.Rank
		if lower {
			old.Replacement.Rank++
		}
		rt.deps.SkillData().(staticSkillSource)[old.ID] = old
		rt.deps.SkillData().(staticSkillSource)[row.ID] = row
		c.Skills = append(c.Skills, row.ID)
		c.CurrentMP = testInt64(1000)
		if !rt.effects.Apply(statuseffect.Effect{DivisionID: testDivision, CharacterName: c.Name, SkillID: old.ID, SkillGroup: old.Group, InstanceToken: 100000, State: statuseffect.StateActive, Phase: 2}) {
			t.Fatal("seed")
		}
		out := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: row.ID}.Encode())
		rows := rt.effects.Snapshot(testDivision, c.Name)
		if lower {
			if len(rows) != 1 || rows[0].StopRequested || *c.CurrentMP != 1000 {
				t.Fatal("weaker buff replaced old rank", rows)
			}
		} else {
			if len(rows) != 1 || rows[0].StopRequested || rows[0].InstanceToken == 100000 || *c.CurrentMP == 1000 {
				t.Fatal("equal rank did not replace", rows)
			}
			frame, ok := findFrame(out.Frames, wire.OpEndedEffectInstances)
			if !ok {
				t.Fatal("replacement omitted old token teardown")
			}
			ended, err := wire.DecodeEndedEffectInstances(frame.Payload)
			if err != nil || len(ended.InstanceTokens) != 1 || ended.InstanceTokens[0] != 100000 {
				t.Fatal("wrong retired token", ended, err)
			}
			if batches := rt.drainStoppedCharacterEffects(); len(batches) != 0 {
				t.Fatal("replacement retired twice", batches)
			}
			remaining := rt.effects.Snapshot(testDivision, c.Name)
			if len(remaining) != 1 || remaining[0].SkillID != row.ID {
				t.Fatal("new buff was retired with old", remaining)
			}
		}
	}
}
