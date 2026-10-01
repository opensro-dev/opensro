/*
===========================================================================

actionsession_test.go - private action state and movement cancellation

The command reply wraps public combat and private progression. Shared tests
verify that wrapper before isolating the domain burst they are exercising.

===========================================================================
*/
package action

import (
	"bytes"
	"encoding/binary"
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
assertQueuedAction

Queue admission publishes only the private count; it must not apply damage,
healing, cost or movement before the executing front has finished.
================
*/
func assertQueuedAction(t *testing.T, result OpResult) {
	t.Helper()
	if result.DiagnosticRefusal != "" || len(result.Broadcast) != 0 || len(result.Recipients) != 0 || len(result.Frames) != 1 || len(result.ActorPrivate) != 1 {
		t.Fatal("queued action escaped the private command lane", result)
	}
	for _, frame := range []wire.Frame{result.Frames[0], result.ActorPrivate[0]} {
		if frame.Opcode != wire.OpActionState || !bytes.Equal(frame.Payload, []byte{1, 2}) {
			t.Fatal("wrong queued action count", frame)
		}
	}
}

/*
================
TestCommittedSkillPublishesBasicHandoffForHeldMovement

An initially refused cancel must become retryable when the authored skill
hands its target to a basic command, even if the remaining count stays one.
================
*/
func TestCommittedSkillPublishesBasicHandoffForHeldMovement(t *testing.T) {
	rt, clock, c, target := newCombatTestRuntime(t, 100000)
	skill := shippedOffense(t, "SKILL_CH_SWORD_SMASH_A_01")
	rt.deps.SkillData().(staticSkillSource)[skill.ID] = skill
	c.Skills = append(c.Skills, skill.ID)
	c.CurrentMP = testInt64(10000)
	rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: skill.ID, HasTarget: true, TargetGid: target.Gid}.Encode())
	request := wire.TargetInteract{Cancel: true}.Encode()
	refusal := rt.HandleTargetInteract(testDivision, c, request)
	if len(refusal.Frames) != 1 || !bytes.Equal(refusal.Frames[0].Payload, []byte{3, 1, 4}) {
		t.Fatal("committed skill did not retain its native count", refusal)
	}
	transition := false
	for tick := 0; tick < 100 && !transition; tick++ {
		clock.Advance(100 * time.Millisecond)
		for _, burst := range rt.TickHook()(clock.NowMs()) {
			for _, frame := range burst.Frames {
				if frame.Opcode == wire.OpActionState && bytes.Equal(frame.Payload, []byte{2, 1}) {
					if burst.OnlyCharacterID != c.ID {
						t.Fatal("skill-to-basic handoff leaked to peers")
					}
					transition = true
				}
			}
		}
	}
	if !transition {
		t.Fatal("authored basic continuation never released the refused cancel")
	}
	rt.HandleTargetInteract(testDivision, c, request)
	tokens := rt.castTokenCounter
	for tick := 0; tick < 20; tick++ {
		clock.Advance(100 * time.Millisecond)
		rt.TickHook()(clock.NowMs())
	}
	if rt.castTokenCounter != tokens || rt.actionQueueCount(testDivision, c.Name) != 0 {
		t.Fatal("held movement cancellation failed to stop authored repetition")
	}
}

/*
================
finishTestCast

Loot-specific fixtures begin after the fatal action's normal close. Drive
the injected clock and real tick rather than erasing combat state by hand.
================
*/
func finishTestCast(t *testing.T, rt *Runtime, clock *fakeClock, c *enterworld.Character) {
	t.Helper()
	for tick := 0; tick < 100 && rt.hasOpenSkillCast(testDivision, c.Name); tick++ {
		clock.Advance(100 * time.Millisecond)
		rt.TickHook()(clock.NowMs())
	}
	if rt.hasOpenSkillCast(testDivision, c.Name) {
		t.Fatal("fixture cast failed to close")
	}
}

/*
================
TestQueuedPickupTransfersActionPublicationToApproach
================
*/
func TestQueuedPickupTransfersActionPublicationToApproach(t *testing.T) {
	rt, clock, c, target := newCombatTestRuntime(t, 100000)
	from := rt.liveSpawn(simulation.WorldKey(testDivision, c.Name), c, clock.NowMs())
	from.X += 100
	heap := rt.Ground.Add(testDivision, PlanGoldDrop(
		GoldHeapRef{RefObjID: 62, Codename: "ITEM_ETC_GOLD_02", Tid1: 3, Tid2: 3, Tid3: 5, Tid4: 2},
		777, from, c.Name, clock.Now()))
	rt.HandleTargetInteract(testDivision, c, wire.BasicAttackEngage{TargetGid: target.Gid}.Encode())
	assertQueuedAction(t, rt.HandleTargetInteract(testDivision, c, wire.TargetInteract{Gid: heap.Gid}.Encode()))
	for tick := 0; tick < 60; tick++ {
		clock.Advance(100 * time.Millisecond)
		rt.TickHook()(clock.NowMs())
		if _, pending := rt.Pending.Peek(simulation.WorldKey(testDivision, c.Name)); pending && rt.actionQueueCount(testDivision, c.Name) != 1 {
			t.Fatal("combat retirement hid pickup approach")
		}
	}
	if _, exists := rt.Ground.Get(testDivision, heap.Gid); exists || rt.actionQueueCount(testDivision, c.Name) != 0 {
		t.Fatal("queued pickup failed to grant and retire")
	}
}

/*
================
TestEffectCancellationPreservesCombatQueueCount
================
*/
func TestEffectCancellationPreservesCombatQueueCount(t *testing.T) {
	rt, _, c, target := newCombatTestRuntime(t, 100000)
	attack := wire.BasicAttackEngage{TargetGid: target.Gid}.Encode()
	for count := byte(1); count <= 2; count++ {
		rt.HandleTargetInteract(testDivision, c, attack)
		result := rt.HandleTargetInteract(testDivision, c, wire.CancelActiveEffectRequest{EffectID: 123456}.Encode())
		if len(result.Frames) != 1 || !bytes.Equal(result.Frames[0].Payload, []byte{2, count}) {
			t.Fatal("independent effect cancellation hid the combat queue", result)
		}
	}
}

/*
================
TestRejectedCommandPreservesQueueBehindSelfCast

Self casts own a bracket without a repeating combat intent. Their pending
replacement must survive rejection just as a combo's pending command does.
================
*/
func TestRejectedCommandPreservesQueueBehindSelfCast(t *testing.T) {
	rt, clock, c, target := newCombatTestRuntime(t, 100000)
	skill := shippedOffense(t, "SKILL_CH_WATER_SELFHEAL_A_01")
	rt.deps.SkillData().(staticSkillSource)[skill.ID] = skill
	c.Skills = append(c.Skills, skill.ID)
	c.CurrentMP = testInt64(10000)
	rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: skill.ID}.Encode())
	assertQueuedAction(t, rt.HandleTargetInteract(testDivision, c, wire.BasicAttackEngage{TargetGid: target.Gid}.Encode()))
	rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: ^uint32(0)}.Encode())
	for tick := 0; tick < 50; tick++ {
		clock.Advance(100 * time.Millisecond)
		rt.TickHook()(clock.NowMs())
	}
	if rt.castTokenCounter <= 1 {
		t.Fatal("rejected command discarded the self cast's queued attack")
	}
}

/*
================
TestQueuedNonAttackSkillsExecuteThroughTheirNormalOwners

Both a heal and a buff must wait without charging, then execute through their
existing validators. This catches per-skill busy refusals and queue leakage.
================
*/
func TestQueuedNonAttackSkillsExecuteThroughTheirNormalOwners(t *testing.T) {
	for _, code := range []string{"SKILL_CH_WATER_SELFHEAL_A_01", "SKILL_CH_COLD_GANGGI_A_01"} {
		t.Run(code, func(t *testing.T) {
			rt, clock, c, target := newCombatTestRuntime(t, 100000)
			skill := shippedOffense(t, code)
			rt.deps.SkillData().(staticSkillSource)[skill.ID] = skill
			c.Skills = append(c.Skills, skill.ID)
			c.CurrentMP = testInt64(10000)
			rt.HandleTargetInteract(testDivision, c, wire.BasicAttackEngage{TargetGid: target.Gid}.Encode())
			beforeMP, beforeHP := enterworld.CurrentMP(c), enterworld.CurrentHP(c)
			assertQueuedAction(t, rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: skill.ID}.Encode()))
			if enterworld.CurrentMP(c) != beforeMP || enterworld.CurrentHP(c) != beforeHP {
				t.Fatal("pending skill applied cost or healing")
			}
			opened := false
			for tick := 0; tick < 50; tick++ {
				clock.Advance(100 * time.Millisecond)
				for _, burst := range rt.TickHook()(clock.NowMs()) {
					for _, frame := range burst.Frames {
						if frame.Opcode == wire.OpSkillCastResult && len(frame.Payload) >= 6 && frame.Payload[0] == 1 && binary.LittleEndian.Uint32(frame.Payload[2:6]) == skill.ID {
							opened = true
						}
					}
				}
			}
			if !opened || enterworld.CurrentMP(c) >= beforeMP {
				t.Fatal("queued skill never executed or charged through its owner", opened, enterworld.CurrentMP(c))
			}
		})
	}
}

/*
================
TestQueuedFollowWaitsForCommittedSkillAndThenOwnsPursuit
================
*/
func TestQueuedFollowWaitsForCommittedSkillAndThenOwnsPursuit(t *testing.T) {
	rt, clock, c, target := followFixture(t)
	skill := shippedOffense(t, "SKILL_CH_WATER_SELFHEAL_A_01")
	rt.deps.SkillData().(staticSkillSource)[skill.ID] = skill
	c.Skills = append(c.Skills, skill.ID)
	c.CurrentMP = testInt64(10000)
	rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: skill.ID}.Encode())
	assertQueuedAction(t, rt.HandleTargetInteract(testDivision, c, wire.FollowTarget{TargetGid: enterworld.ObjectIDForCharacter(target)}.Encode()))
	for tick := 0; tick < 50; tick++ {
		clock.Advance(100 * time.Millisecond)
		rt.TickHook()(clock.NowMs())
	}
	intent, exists := rt.combatIntentFor(testDivision, c.Name)
	if !exists || !intent.FollowTarget || intent.TargetGid != enterworld.ObjectIDForCharacter(target) {
		t.Fatal("queued Trace failed to take ownership", intent)
	}
	rt.HandleTargetInteract(testDivision, c, wire.TargetInteract{Cancel: true}.Encode())
	if _, exists := rt.combatIntentFor(testDivision, c.Name); exists {
		t.Fatal("Trace continued after cancellation")
	}
}

/*
================
assertAndSeparateActionSession

Require exactly one private native arm at the end of the actor's burst, and
none in the peer broadcast. Return the underlying domain burst for existing
damage, loot, preparation and progression assertions.
================
*/
func assertAndSeparateActionSession(t *testing.T, result OpResult) OpResult {
	t.Helper()
	want := wire.ArmActionState().Encode()
	for _, frame := range result.Broadcast {
		if frame.Opcode == wire.OpActionState {
			t.Fatal("private action state leaked to peers")
		}
	}
	for _, frames := range [][]wire.Frame{result.Frames, result.ActorPrivate} {
		if len(frames) == 0 {
			t.Fatal("missing action-session arm")
		}
		last := frames[len(frames)-1]
		if last.Opcode != wire.OpActionState || !bytes.Equal(last.Payload, want) {
			t.Fatalf("action-session tail = %+v, want B2CD %v", last, want)
		}
		for _, frame := range frames[:len(frames)-1] {
			if frame.Opcode == wire.OpActionState {
				t.Fatal("duplicate action-session arm")
			}
		}
	}
	result.Frames = result.Frames[:len(result.Frames)-1]
	result.ActorPrivate = result.ActorPrivate[:len(result.ActorPrivate)-1]
	return result
}

/*
================
TestQueuedReplacementPreservesCommittedComboAndRejectedAdmission
================
*/
func TestQueuedReplacementPreservesCommittedComboAndRejectedAdmission(t *testing.T) {
	rt, clock, c, target := newCombatTestRuntime(t, 100000)
	root := installSwordCombo(t, rt, c)
	rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: root.ID, HasTarget: true, TargetGid: target.Gid}.Encode())
	front, _ := rt.combatIntentFor(testDivision, c.Name)
	queued := rt.HandleTargetInteract(testDivision, c, wire.BasicAttackEngage{TargetGid: target.Gid}.Encode())
	if !bytes.Equal(queued.Frames[len(queued.Frames)-1].Payload, []byte{1, 2}) {
		t.Fatal("missing queued command", queued)
	}
	active, _ := rt.combatIntentFor(testDivision, c.Name)
	if active != front {
		t.Fatal("replacement truncated the committed chain")
	}
	rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: ^uint32(0), HasTarget: true, TargetGid: target.Gid}.Encode())
	active, _ = rt.combatIntentFor(testDivision, c.Name)
	if active != front {
		t.Fatal("rejected replacement discarded the executing chain")
	}
	var stages []uint32
	for tick := 1; tick <= 30; tick++ {
		for _, burst := range rt.TickHook()(clock.At(time.Duration(tick) * 100 * time.Millisecond).UnixMilli()) {
			for _, frame := range burst.Frames {
				if frame.Opcode == wire.OpSkillCastResult && len(frame.Payload) >= 6 && frame.Payload[0] == 1 {
					stages = append(stages, binary.LittleEndian.Uint32(frame.Payload[2:6]))
				}
			}
		}
	}
	if len(stages) < 3 || stages[0] != root.ChainNext || stages[1] != 8 || stages[2] != 2 {
		t.Fatalf("queued replacement skipped committed stages: %v", stages)
	}
}

/*
================
TestForcedCancellationCannotPromoteQueuedAttackAfterRelease
================
*/
func TestForcedCancellationCannotPromoteQueuedAttackAfterRelease(t *testing.T) {
	rt, clock, c, target := newCombatTestRuntime(t, 100000)
	request := wire.BasicAttackEngage{TargetGid: target.Gid}.Encode()
	rt.HandleTargetInteract(testDivision, c, request)
	rt.HandleTargetInteract(testDivision, c, request)
	rt.ClearCombatIntent(testDivision, c.Name)
	for tick := 1; tick <= 30; tick++ {
		rt.TickHook()(clock.At(time.Duration(tick) * 100 * time.Millisecond).UnixMilli())
	}
	if rt.castTokenCounter != 1 {
		t.Fatal("forced cancellation resurrected queued attack", rt.castTokenCounter)
	}
	rt.clearSkillFinalizes(testDivision, c.Name)
	clock.Advance(3 * time.Second)
	assertAndSeparateActionSession(t, rt.HandleTargetInteract(testDivision, c, request))
}

/*
================
assertAndSeparateActionReleases

Public cast fixtures remain byte-for-byte native evidence. Validate the
separate private session release before comparing those public captures.
================
*/
func assertAndSeparateActionReleases(t *testing.T, routed []simulation.DivisionFrames) []simulation.DivisionFrames {
	t.Helper()
	var public []simulation.DivisionFrames
	for _, burst := range routed {
		if len(burst.Frames) == 1 && burst.Frames[0].Opcode == wire.OpActionState {
			if burst.OnlyCharacterID == 0 || !bytes.Equal(burst.Frames[0].Payload, wire.ReleaseActionState().Encode()) {
				t.Fatalf("invalid private action retirement: %+v", burst)
			}
			continue
		}
		public = append(public, burst)
	}
	return public
}

/*
================
TestMovementCancelStopsBasicRepetitionWithoutUndoingCommittedDamage
================
*/
func TestMovementCancelStopsBasicRepetitionWithoutUndoingCommittedDamage(t *testing.T) {
	rt, clock, character, target := newCombatTestRuntime(t, 100000)
	first := rt.HandleTargetInteract(testDivision, character, wire.BasicAttackEngage{TargetGid: target.Gid}.Encode())
	assertAndSeparateActionSession(t, first)
	struck, _ := rt.Monsters.Get(testDivision, target.Gid)
	if struck.CurrentHP >= target.CurrentHP {
		t.Fatal("fixture did not commit a strike")
	}
	cancel := rt.HandleTargetInteract(testDivision, character, wire.TargetInteract{Cancel: true}.Encode())
	if len(rt.combatIntentSnapshot()) != 0 {
		t.Fatal("cancel retained repeating attack")
	}
	last := cancel.Frames[len(cancel.Frames)-1]
	if last.Opcode != wire.OpActionState || !bytes.Equal(last.Payload, wire.ReleaseActionState().Encode()) {
		t.Fatal("cancel did not release action session", cancel)
	}
	for step := 1; step <= 10; step++ {
		rt.TickHook()(clock.At(testBasicAttackActionDuration*10).UnixMilli() + int64(step))
	}
	after, _ := rt.Monsters.Get(testDivision, target.Gid)
	if after.CurrentHP != struck.CurrentHP {
		t.Fatalf("cancel changed committed HP or repeated: %d -> %d", struck.CurrentHP, after.CurrentHP)
	}
}

/*
================
TestQueuedActionCancellationPreservesExecutingContinuation

The native pair has a pending back entry and an executing front entry.
Cancellation removes only the former; another cancel can stop a basic front.
================
*/
func TestQueuedActionCancellationPreservesExecutingContinuation(t *testing.T) {
	rt, _, c, target := newCombatTestRuntime(t, 100000)
	request := wire.BasicAttackEngage{TargetGid: target.Gid}.Encode()
	rt.HandleTargetInteract(testDivision, c, request)
	before, _ := rt.combatIntentFor(testDivision, c.Name)
	queued := rt.HandleTargetInteract(testDivision, c, request)
	assertOpcodes(t, queued.Frames, wire.OpActionState)
	if !bytes.Equal(queued.Frames[0].Payload, []byte{1, 2}) {
		t.Fatal("replacement did not publish a pair", queued)
	}
	cancel := rt.HandleTargetInteract(testDivision, c, wire.TargetInteract{Cancel: true}.Encode())
	assertOpcodes(t, cancel.Frames, wire.OpActionState)
	if !bytes.Equal(cancel.Frames[0].Payload, []byte{2, 1}) {
		t.Fatal("queued cancellation did not preserve the front", cancel)
	}
	after, exists := rt.combatIntentFor(testDivision, c.Name)
	if !exists || after != before || !rt.hasOpenSkillCast(testDivision, c.Name) {
		t.Fatal("queued cancellation changed the executing owner")
	}
	rt.HandleTargetInteract(testDivision, c, wire.TargetInteract{Cancel: true}.Encode())
	if _, exists := rt.combatIntentFor(testDivision, c.Name); exists {
		t.Fatal("second cancel retained the basic continuation")
	}
}

/*
================
TestQueuedActionPublishesSingleCountAfterExecutingCastCloses
================
*/
func TestQueuedActionPublishesSingleCountAfterExecutingCastCloses(t *testing.T) {
	rt, clock, c, target := newCombatTestRuntime(t, 100000)
	request := wire.BasicAttackEngage{TargetGid: target.Gid}.Encode()
	rt.HandleTargetInteract(testDivision, c, request)
	rt.HandleTargetInteract(testDivision, c, request)
	count := 0
	for _, burst := range rt.TickHook()(clock.At(testBasicAttackActionDuration).UnixMilli()) {
		for _, frame := range burst.Frames {
			if frame.Opcode != wire.OpActionState {
				continue
			}
			if burst.OnlyCharacterID != c.ID || !bytes.Equal(frame.Payload, []byte{2, 1}) {
				t.Fatal("wrong queued retirement", burst)
			}
			count++
		}
	}
	if count != 1 {
		t.Fatalf("single-count transitions = %d, want one", count)
	}
}
