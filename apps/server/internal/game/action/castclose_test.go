/*
===========================================================================

castclose_test.go - a released self effect still closes its cast bracket

The release B505 frees the caster but is not the bracket's close. Without
the closing B505 the client keeps the casting aura on the ground.

===========================================================================
*/

package action

import (
	"encoding/binary"
	"testing"

	"opensro.online/server/internal/game/item/wire"
)

/*
================
TestReleasedSelfEffectClosesItsBracket

A prepared Frenzy releases, leaves the caster free to act, and queues the
mode-2 close of the same token after its recovery phase.
================
*/
func TestReleasedSelfEffectClosesItsBracket(t *testing.T) {
	rt, clock, c, skill, _ := frenzyFixture(t, "HEALTH_A")
	if skill.ActionCastingTimeMs == 0 {
		t.Fatal("fixture skill has no preparation")
	}
	out := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: skill.ID}.Encode())
	out = assertAndSeparateActionSession(t, out)
	frame, ok := findFrame(out.Frames, wire.OpSkillCastResult)
	if !ok || frame.Payload[0] != 1 {
		t.Fatalf("Frenzy refused: %+v", out)
	}
	token := binary.LittleEndian.Uint32(frame.Payload[10:])
	released := clock.NowMs() + int64(skill.ActionCastingTimeMs) + 1
	rt.advanceProjectileCasts(released)
	if len(rt.effects.Snapshot(testDivision, c.Name)) != 1 {
		t.Fatal("Frenzy was not installed")
	}
	if rt.hasOpenSkillCast(testDivision, c.Name) {
		t.Fatal("the close held the released caster busy")
	}
	closed := false
	for _, batch := range rt.drainSkillFinalizes(released + int64(skill.ActionDurationMs)) {
		for _, f := range batch.Frames {
			closed = closed || f.Opcode == wire.OpSkillEffectControl && len(f.Payload) == 6 &&
				f.Payload[0] == 2 && binary.LittleEndian.Uint32(f.Payload[2:]) == token
		}
	}
	if !closed {
		t.Fatal("the released cast never closed its bracket")
	}
}

/*
================
TestDetachedCastCloseRetiresWithSession

A free caster still owns its delayed presentation. Disconnect must remove
that close while preserving another player's queued packet.
================
*/
func TestDetachedCastCloseRetiresWithSession(t *testing.T) {
	rt, _ := newTestRuntime(testCharacter(), testItems())
	rt.queueDetachedCastClose(testDivision, "leaving", 11, 101, 50)
	rt.queueDetachedCastClose(testDivision, "staying", 22, 202, 50)
	if rt.hasOpenSkillCast(testDivision, "leaving") || len(rt.openSkillCastOwnerSnapshot()) != 0 {
		t.Error("presentation-only close retained command ownership")
	}
	rt.clearSkillFinalizes(testDivision, "leaving")
	got := rt.drainSkillFinalizes(50)
	if len(got) != 1 || got[0].SourceGID != 22 {
		t.Fatalf("disconnected session retained its close: %+v", got)
	}
}
