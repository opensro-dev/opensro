/*
===========================================================================

skillpassivepress_test.go - a pressed passive is consumed with no frame

The v1.150 client never presses a passive, but a bot does (#4152: a
crossbow Rogue's bot cast the learned passives and the port answered
"This skill is not implemented yet."). The reference server's skill-cast
command starts only queued casts (activity 2); any other activity returns
with no frame.

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
pressLearnedRow

The archer fixture with row learned and in the catalogue, pressing it on
the fixture's monster.
================
*/
func pressLearnedRow(t *testing.T, row enterworld.SkillRow) OpResult {
	t.Helper()
	rt, c, target, _, _ := arrowFixture(t)
	rt.deps.SkillData().(staticSkillSource)[row.ID] = row
	c.Skills = append(c.Skills, row.ID)
	return rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: row.ID, HasTarget: true, TargetGid: target}.Encode())
}

/*
================
TestPressedPassiveIsConsumedSilently
================
*/
func TestPressedPassiveIsConsumedSilently(t *testing.T) {
	passive := shippedOffense(t, "SKILL_EU_ROG_BOWP_DAMAGE_A_01")
	if passive.QueuedActivity() || passive.Group == 0 {
		t.Fatalf("not a shipped passive: activity %d group %d", passive.ActionKind, passive.Group)
	}
	result := pressLearnedRow(t, passive)
	if result.DiagnosticRefusal != "offensive-not-queued" {
		t.Fatalf("refusal %q, want offensive-not-queued", result.DiagnosticRefusal)
	}
	if len(result.Frames) != 0 || len(result.ActorPrivate) != 0 {
		t.Fatalf("a pressed passive must send nothing, sent %d/%d frames", len(result.Frames), len(result.ActorPrivate))
	}
}

/*
================
TestUnportedQueuedCastStillNamesTheGap

A queued cast with no runnable shape is a real port gap: it keeps the
diagnostic notice, so the gap stays visible to the player and the logs.
================
*/
func TestUnportedQueuedCastStillNamesTheGap(t *testing.T) {
	row := shippedOffense(t, "SKILL_EU_ROG_BOWP_DAMAGE_A_01")
	row.ActionKind = 2
	if !row.QueuedActivity() {
		t.Fatal("activity 2 must be a queued cast")
	}
	result := pressLearnedRow(t, row)
	if result.DiagnosticRefusal != "offensive-shape-unsupported" || len(result.ActorPrivate) != 1 {
		t.Fatalf("refusal %q with %d frames, want the not-implemented notice", result.DiagnosticRefusal, len(result.ActorPrivate))
	}
}
