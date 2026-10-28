package action

import (
	"bytes"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/gmcommand"
	"opensro.online/server/internal/game/item/statuseffect"
	"opensro.online/server/internal/game/item/wire"
)

func TestGMBodyStatusProductionDispatchPublicationAndDamage(t *testing.T) {
	rt, clock, c, instance := newCombatTestRuntime(t, 100)
	deps := rt.deps.(*enterworld.Deps)
	instance.Ref.DefaultSkillIDs[0] = 2
	skills := deps.SkillData().(staticSkillSource)
	skill := skills[2]
	skill.Attack.Min, skill.Attack.Max, skill.Attack.Percent = 1, 1, 100
	skills[2] = skill
	c.ActiveCOS = &domain.CharacterCOS{GID: 300, Summoned: true}
	var actor, peers []wire.Frame
	rt.PushCharacterFrames = func(_, _ string, frames []wire.Frame) { actor = append(actor, frames...) }
	rt.PushDivisionPeerFrames = func(_, _ string, frames []wire.Frame) { peers = append(peers, frames...) }
	request := func(command byte) gmcommand.Outcome {
		return gmcommand.HandleGmCommand(deps, nil, testDivision, c, []byte{command}, rt)
	}
	if out := request(gmcommand.SubInvisible); out.Ack != nil || c.NativeBodyStatus != 0 || len(actor) != 0 {
		t.Fatalf("unprivileged mutation: %+v", out)
	}
	c.GMPrivilege = true
	for _, step := range []struct{ command, want uint8 }{{14, 4}, {15, 3}, {15, 0}, {14, 4}, {14, 0}} {
		actor, peers = nil, nil
		out := request(step.command)
		if !bytes.Equal(out.Ack, []byte{1, step.command}) || c.NativeBodyStatus != step.want || c.ActiveCOS.NativeBodyStatus != step.want {
			t.Fatalf("toggle %+v: %+v, status %d", step, out, c.NativeBodyStatus)
		}
		if len(actor) != 2 || len(peers) != 2 {
			t.Fatalf("missing owner/companion publication: %v / %v", actor, peers)
		}
		for i, frame := range actor {
			state, err := wire.DecodeObjectStateRefresh(frame.Payload)
			if err != nil || frame.Opcode != wire.OpObjectStateRefresh || state.StateType != 4 || state.Value != step.want || !bytes.Equal(frame.Payload, peers[i].Payload) {
				t.Fatalf("bad state frame %+v", frame)
			}
		}
		if step.want != 0 {
			hp := enterworld.CurrentHP(c)
			result := rt.MonsterBasicAttack(testDivision, instance, enterworld.ObjectIDForCharacter(c), 2, clock.NowMs())
			if result.Accepted || !result.TargetAlive || len(result.Frames) != 0 || enterworld.CurrentHP(c) != hp {
				t.Fatalf("GM immunity failed: %+v", result)
			}
		}
	}
	// Revoking privilege after dispatch's detached read still prevents mutation.
	deps.UpdateCharacter = func(c *enterworld.Character, _ string, update func() bool) bool {
		c.GMPrivilege = false
		return update()
	}
	if out := request(14); out.Ack[0] != 2 || c.NativeBodyStatus != 0 {
		t.Fatal("stale privilege admitted")
	}
}

func bodyEffectSkills() staticSkillSource {
	return staticSkillSource{
		100: {ID: 100, Group: 9, EffectDurationMs: 100, BodyStatus: enterworld.SkillBodyStatus{Present: true, Supported: true, Value: 6}},
		101: {ID: 101, Group: 10, EffectDurationMs: 200, BodyStatus: enterworld.SkillBodyStatus{Present: true, Supported: true, Value: 7}},
		102: {ID: 102, Group: 11, BodyStatus: enterworld.SkillBodyStatus{Present: true}},
	}
}

func TestBodyEffectFatalDamageRetiresBeforeLifeAndCannotExpireAfterRebirth(t *testing.T) {
	rt, clock, c, instance := newCombatTestRuntime(t, 100)
	instance.Ref.DefaultSkillIDs[0] = 2
	instance.Nest.NativeTacticsFlags = 0x200
	c.CurrentHP = testInt64(1)
	skills := rt.deps.SkillData().(staticSkillSource)
	attack := skills[2]
	attack.Attack.Min, attack.Attack.Max, attack.Attack.Percent = 100, 100, 100
	skills[2] = attack
	for id, row := range bodyEffectSkills() {
		skills[id] = row
	}
	if !rt.ApplyCharacterEffectPresentation(testDivision, c.Name, 100, 77, statuseffect.StateActive, false, EffectPresentation{Phase: 2}, clock.NowMs()) {
		t.Fatal("effect refused")
	}
	c.BattleUntilMs = 0 // battle exit on death is pinned by the battle-state tests
	old := c.Snapshot()
	var actorRetirement, peerRetirement []wire.Frame
	rt.PushCharacterFrames = func(_, _ string, frames []wire.Frame) { actorRetirement = append(actorRetirement, frames...) }
	rt.PushDivisionPeerFrames = func(_, _ string, frames []wire.Frame) { peerRetirement = append(peerRetirement, frames...) }
	result := rt.MonsterBasicAttack(testDivision, instance, enterworld.ObjectIDForCharacter(c), 2, clock.NowMs())
	if !result.Accepted || result.TargetAlive || c.NativeBodyStatus != 0 || c.BodyStatusOwner != 0 || len(rt.effects.Snapshot(testDivision, c.Name)) != 0 {
		t.Fatal("death retained body effect", result, c.NativeBodyStatus)
	}
	if len(actorRetirement) != 2 || len(peerRetirement) != 2 || actorRetirement[0].Opcode != wire.OpObjectStateRefresh || actorRetirement[0].Payload[4] != wire.StateChannelBody || actorRetirement[0].Payload[5] != 0 || actorRetirement[1].Opcode != wire.OpEndedEffectInstances {
		t.Fatal("death retirement was not enqueued before returning life frames", actorRetirement, peerRetirement)
	}
	if len(result.Frames) != 3 || result.Frames[2].Payload[4] != wire.StateChannelLife {
		t.Fatal("retirement duplicated into deferred combat delivery", result.Frames)
	}
	if old.NativeBodyStatus != 6 {
		t.Fatal("death mutated detached snapshot")
	}
	rt.HandleLocalRebirth(testDivision, c, []byte{1})
	if !enterworld.CharacterAlive(c) {
		t.Fatal("rebirth refused")
	}
	if !rt.ApplyCharacterEffectPresentation(testDivision, c.Name, 100, 77, statuseffect.StateActive, false, EffectPresentation{Phase: 2}, clock.NowMs()+50) {
		t.Fatal("replacement refused")
	}
	if len(actorRetirement) != 4 || actorRetirement[2].Payload[5] != 6 || actorRetirement[3].Opcode != wire.OpAttachedEffect {
		t.Fatal("wire token reused before old retirement publication", actorRetirement)
	}
	rt.TickHook()(clock.NowMs() + 100)
	if c.NativeBodyStatus != 6 {
		t.Fatal("pre-death expiry cleared new application")
	}
	rt.TickHook()(clock.NowMs() + 150)
	if c.NativeBodyStatus != 6 {
		t.Fatal("new effect expired at equality")
	}
	rt.TickHook()(clock.NowMs() + 151)
	if c.NativeBodyStatus != 0 {
		t.Fatal("new effect did not expire")
	}
}

func TestPlainHideWithOwnedCOSDoesNotInventCompanionPropagation(t *testing.T) {
	rt, c := newActiveEffectTestRuntime(t, bodyEffectSkills())
	c.ActiveCOS = &domain.CharacterCOS{GID: 300, Summoned: true, CurrentHP: 100}
	if !rt.ApplyCharacterEffectPresentation(testDivision, c.Name, 100, 77, statuseffect.StateActive, false, EffectPresentation{Phase: 2}, 1000) {
		t.Fatal("owned COS incorrectly refused plain hide")
	}
	if c.NativeBodyStatus != 6 || c.ActiveCOS.NativeBodyStatus != 0 || !c.ActiveCOS.Summoned {
		t.Fatal("hide was confused with GM propagation or COS dismissal")
	}
	rt.TickHook()(1101)
	if c.NativeBodyStatus != 0 || !c.ActiveCOS.Summoned {
		t.Fatal("hide expiry changed companion lifetime")
	}
}

func TestBodyEffectReplacementCancellationExpiryAndGMOwnership(t *testing.T) {
	rt, c := newActiveEffectTestRuntime(t, bodyEffectSkills())
	apply := func(id, token uint32, now int64) {
		t.Helper()
		if !rt.ApplyCharacterEffectPresentation(testDivision, c.Name, id, token, statuseffect.StateActive, false, EffectPresentation{Phase: 2}, now) {
			t.Fatal("application refused")
		}
	}
	apply(100, 50, 1000)
	old := c.Snapshot()
	if old.NativeBodyStatus != 6 || old.BodyStatusOwner == 0 {
		t.Fatal("effect did not write status")
	}
	apply(100, 50, 1050)
	if c.BodyStatusOwner == old.BodyStatusOwner {
		t.Fatal("replacement reused ownership")
	}
	rt.TickHook()(1100)
	if c.NativeBodyStatus != 6 {
		t.Fatal("old expiry cleared replacement")
	}
	apply(101, 51, 1101)
	rt.TickHook()(1151)
	if c.NativeBodyStatus != 7 || old.NativeBodyStatus != 6 {
		t.Fatal("retired effect cleared newer status or mutated snapshot")
	}
	result := rt.HandleTargetInteract(testDivision, c, (wire.CancelActiveEffectRequest{EffectID: 101}).Encode())
	if len(result.Frames) == 0 || c.NativeBodyStatus != 7 {
		t.Fatal("cancel skipped retirement phase")
	}
	rt.TickHook()(1152)
	if c.NativeBodyStatus != 0 || c.BodyStatusOwner != 0 {
		t.Fatal("cancel did not retire owned status")
	}
	apply(101, 52, 1200)
	c.GMPrivilege = true
	if !rt.ToggleGMBodyStatus(testDivision, c.Name, 4) {
		t.Fatal("GM replacement refused")
	}
	rt.TickHook()(1401)
	if c.NativeBodyStatus != 4 {
		t.Fatal("stale effect expiry cleared GM status")
	}
	rt.ForgetCharacter(testDivision, c.Name)
	if c.NativeBodyStatus != 0 || len(rt.effects.Snapshot(testDivision, c.Name)) != 0 {
		t.Fatal("disconnect retained runtime status")
	}
	apply(100, 50, 2000)
	rt.TickHook()(2001)
	if c.NativeBodyStatus != 6 {
		t.Fatal("reused session/token cleared by old lifetime")
	}
	rt.TickHook()(2100)
	if c.NativeBodyStatus != 6 {
		t.Fatal("expiry cleared body status at equality")
	}
	rt.TickHook()(2101)
	if c.NativeBodyStatus != 0 {
		t.Fatal("expiry failed")
	}
	if rt.ApplyCharacterEffect(testDivision, c.Name, 102, 53, statuseffect.StateActive, false) {
		t.Fatal("unsupported producer enabled")
	}
}

func TestGMStatusPreservedByRebirthAndClearedAtDisconnect(t *testing.T) {
	c := rebirthTestCharacter(1, 0)
	c.GMPrivilege = true
	rt, _ := newTestRuntime(c, testItems())
	if !rt.ToggleGMBodyStatus(testDivision, c.Name, 4) {
		t.Fatal("toggle refused")
	}
	result := rt.HandleLocalRebirth(testDivision, c, []byte{1})
	if c.NativeBodyStatus != 4 || !enterworld.CharacterAlive(c) {
		t.Fatal("rebirth cleared GM status")
	}
	found := false
	for _, frame := range result.Frames {
		if frame.Opcode == wire.OpObjectStateRefresh && len(frame.Payload) == 6 && frame.Payload[4] == 4 && frame.Payload[5] == 4 {
			found = true
		}
	}
	if !found {
		t.Fatal("rebuilt client actor lost status")
	}
	rt.ForgetCharacter(testDivision, c.Name)
	if c.NativeBodyStatus != 0 {
		t.Fatal("new actor lifetime retained GM status")
	}
}

func TestGMStatusNativeRefusalMatrix(t *testing.T) {
	for current := 0; current < 256; current++ {
		for _, requested := range []uint8{3, 4} {
			next, ok := domain.GMToggleBodyStatus(uint8(current), requested)
			refused := current == 1 || current == 2 || current == 5 || current == 6
			want := requested
			if uint8(current) == requested {
				want = 0
			}
			if ok == refused || ok && next != want {
				t.Fatalf("%d -> %d: %d %v", current, requested, next, ok)
			}
		}
	}
}

func TestEffectRetirementEnqueuesBeforeWireTokenReuse(t *testing.T) {
	rt, c := newActiveEffectTestRuntime(t, bodyEffectSkills())
	var frames []wire.Frame
	rt.PushCharacterFrames = func(_, _ string, f []wire.Frame) { frames = append(frames, f...) }
	rt.PushDivisionPeerFrames = func(_, _ string, _ []wire.Frame) {}
	apply := func(id uint32, now int64) {
		t.Helper()
		if !rt.ApplyCharacterEffectPresentation(testDivision, c.Name, id, 70, statuseffect.StateActive, false, EffectPresentation{Phase: 2}, now) {
			t.Fatal("application refused")
		}
	}
	apply(101, 1000)
	c.GMPrivilege = true
	if !rt.ToggleGMBodyStatus(testDivision, c.Name, 4) {
		t.Fatal("GM override refused")
	}
	frames = nil
	if delayed := rt.TickHook()(1201); len(delayed) != 0 {
		t.Fatalf("retirement escaped ordered publication: %+v", delayed)
	}
	apply(100, 1202)
	if len(frames) != 3 || frames[0].Opcode != wire.OpEndedEffectInstances || frames[1].Opcode != wire.OpObjectStateRefresh || frames[2].Opcode != wire.OpAttachedEffect {
		t.Fatalf("reused token overtaken: %+v", frames)
	}
}

func TestReplacingBodyEffectWithOrdinaryEffectReleasesOwnedStatus(t *testing.T) {
	skills := bodyEffectSkills()
	skills[103] = enterworld.SkillRow{ID: 103, Group: 9}
	rt, c := newActiveEffectTestRuntime(t, skills)
	if !rt.ApplyCharacterEffect(testDivision, c.Name, 100, 80, statuseffect.StateActive, false) {
		t.Fatal("body application refused")
	}
	if !rt.ApplyCharacterEffect(testDivision, c.Name, 103, 80, statuseffect.StateActive, false) || c.NativeBodyStatus != 0 || c.BodyStatusOwner != 0 {
		t.Fatal("replacement retained retired ownership")
	}
}

/*
================
TestBodyStatusReadWithoutACopy

The re-entry body-status replay (rebirth and relocation) reads the
one retained field under the read door instead of detaching the
record (#1943): the same value the copy carries, at a fraction of
its cost.
================
*/
func TestBodyStatusReadWithoutACopy(t *testing.T) {
	rt, _, c, _ := newCombatTestRuntime(t, 100)
	for _, want := range []uint8{3, 0, 3} {
		c.NativeBodyStatus = want
		if _, _, status := rt.characterLifeState(testDivision, c); status != want {
			t.Fatalf("body status %d, want %d", status, want)
		}
	}
	copyAllocs := testing.AllocsPerRun(20, func() { rt.characterSnapshot(testDivision, c) })
	readAllocs := testing.AllocsPerRun(20, func() { rt.characterLifeState(testDivision, c) })
	t.Logf("allocs: one character copy %.0f, body-status read %.0f", copyAllocs, readAllocs)
	if readAllocs >= copyAllocs {
		t.Fatalf("the body-status read still copies the character: %.0f allocs; one copy is %.0f",
			readAllocs, copyAllocs)
	}
}
