/*
===========================================================================

fatal_progression_routing_test.go - ordered public damage and private progression tests

Exercise the production action owner and its native packet lifecycle.

===========================================================================
*/
package action

import (
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
TestFatalSkillRewardCommitsAtHitAndPublishesBeforeFinalize
================
*/
func TestFatalSkillRewardCommitsAtHitAndPublishesBeforeFinalize(t *testing.T) {
	rt, clock, character, target := newCombatTestRuntime(t, 1)
	type rewardCall struct {
		exp, skillExp int64
		sourceGid     uint32
	}
	var calls []rewardCall
	var pushed [][]wire.Frame
	rt.UpdateExperience = func(
		_ *enterworld.Character,
		expDelta, skillExpDelta int64,
		sourceGid uint32,
	) ([]wire.Frame, bool) {
		calls = append(calls, rewardCall{exp: expDelta, skillExp: skillExpDelta, sourceGid: sourceGid})
		return []wire.Frame{{Opcode: 0x30D2, Payload: []byte{0xA5}}}, true
	}
	rt.PushCharacterFrames = func(_ string, _ string, frames []wire.Frame) {
		pushed = append(pushed, append([]wire.Frame(nil), frames...))
	}

	result := rt.HandleTargetInteract(testDivision, character, wire.SkillAction{
		ActionId: 2, HasTarget: true, TargetGid: target.Gid,
	}.Encode())
	result = assertAndSeparateActionSession(t, result)
	token, _, fatal := assertSkillDamageOpen(
		t, result.Frames, 2, enterworld.ObjectIDForCharacter(character), target.Gid,
	)
	if !fatal {
		t.Fatal("one-HP target did not produce a fatal result")
	}
	// Fatal combat commits and returns the reward immediately. Native's reward
	// distributor invokes the player progression virtual synchronously; B505
	// is not a reward-authority or delivery prerequisite.
	if len(calls) != 1 || calls[0].exp != 26 || calls[0].skillExp != 109 || calls[0].sourceGid != target.Gid {
		t.Fatalf("reward call = %+v, want immediate exp=26 skillExp=109 source=%d", calls, target.Gid)
	}
	if len(result.Frames) != 3 || result.Frames[0].Opcode != wire.OpSkillCastResult ||
		result.Frames[1].Opcode != wire.OpObjectStateRefresh || result.Frames[2].Opcode != wire.OpExpUpdate {
		t.Fatalf("actor fatal burst = %+v, want B245 -> LIFE-dead -> 30D2 in the request transaction", result.Frames)
	}
	if len(result.Broadcast) != 2 || result.Broadcast[0].Opcode != wire.OpSkillCastResult || result.Broadcast[1].Opcode != wire.OpObjectStateRefresh {
		t.Fatalf("peer fatal burst = %+v, want public B245 -> LIFE-dead for a non-level reward", result.Broadcast)
	}
	if len(result.ActorPrivate) != 1 || result.ActorPrivate[0].Opcode != wire.OpExpUpdate {
		t.Fatalf("tick-owned actor projection = %+v, want private 30D2", result.ActorPrivate)
	}
	if len(pushed) != 0 {
		t.Fatalf("request-owned reward escaped through an asynchronous callback: %+v", pushed)
	}
	if retained, ok := rt.Monsters.Get(testDivision, target.Gid); !ok || retained.CurrentHP != 0 {
		t.Fatalf("defeated source at synchronous reward return = %+v/%v, want retained for death event 0x64", retained, ok)
	}
	assertOnlySkillReleases(t, rt.TickHook()(clock.Now().UnixMilli()))
	assertSkillCastClose(
		t,
		rt.TickHook()(clock.At(testBasicAttackActionDuration).UnixMilli()),
		testDivision,
		token,
	)
	rt.TickHook()(clock.At(testBasicAttackActionDuration + time.Millisecond).UnixMilli())
	if len(calls) != 1 || len(pushed) != 0 {
		t.Fatalf("reward applied more than once: calls=%d pushes=%d", len(calls), len(pushed))
	}
	rt.TickHook()(clock.At(monsterDeathPresentationRetention).UnixMilli())
	if _, ok := rt.Monsters.Get(testDivision, target.Gid); ok {
		t.Fatal("defeated source survived beyond the death-presentation retention")
	}
}

/*
================
TestFatalSkillLevelUpBurstKeepsNativePublicThenPrivateOrder
================
*/
func TestFatalSkillLevelUpBurstKeepsNativePublicThenPrivateOrder(t *testing.T) {
	rt, _, character, target := newCombatTestRuntime(t, 1)
	const actorGid = uint32(100001)
	rt.UpdateExperience = func(
		_ *enterworld.Character,
		_, _ int64,
		sourceGid uint32,
	) ([]wire.Frame, bool) {
		if sourceGid != target.Gid {
			t.Fatalf("reward source gid = %d, want %d", sourceGid, target.Gid)
		}
		return []wire.Frame{
			{Opcode: wire.OpLevelUpEffect, Payload: wire.EncodeLevelUpEffect(actorGid)},
			{Opcode: wire.OpBaseStats, Payload: []byte{1}},
			{Opcode: wire.OpPointsUpdate, Payload: []byte{2}},
			{Opcode: wire.OpExpUpdate, Payload: []byte{3}},
		}, true
	}

	result := rt.HandleTargetInteract(testDivision, character, wire.SkillAction{
		ActionId: 2, HasTarget: true, TargetGid: target.Gid,
	}.Encode())
	result = assertAndSeparateActionSession(t, result)
	wantActor := []uint16{
		wire.OpSkillCastResult,
		wire.OpObjectStateRefresh,
		wire.OpLevelUpEffect,
		wire.OpBaseStats,
		wire.OpPointsUpdate,
		wire.OpExpUpdate,
	}
	if len(result.Frames) != len(wantActor) {
		t.Fatalf("actor level-up burst = %+v", result.Frames)
	}
	for index, opcode := range wantActor {
		if result.Frames[index].Opcode != opcode {
			t.Fatalf("actor level-up opcode[%d] = 0x%04X, want 0x%04X", index, result.Frames[index].Opcode, opcode)
		}
	}
	if len(result.Broadcast) != 3 || result.Broadcast[0].Opcode != wire.OpSkillCastResult ||
		result.Broadcast[1].Opcode != wire.OpObjectStateRefresh || result.Broadcast[2].Opcode != wire.OpLevelUpEffect {
		t.Fatalf("public level-up projection = %+v, want B245 -> LIFE-dead -> 36B0", result.Broadcast)
	}
	wantPrivate := []uint16{wire.OpBaseStats, wire.OpPointsUpdate, wire.OpExpUpdate}
	if len(result.ActorPrivate) != len(wantPrivate) {
		t.Fatalf("private level-up projection = %+v", result.ActorPrivate)
	}
	for index, opcode := range wantPrivate {
		if result.ActorPrivate[index].Opcode != opcode {
			t.Fatalf("private level-up opcode[%d] = 0x%04X, want 0x%04X", index, result.ActorPrivate[index].Opcode, opcode)
		}
	}
}
