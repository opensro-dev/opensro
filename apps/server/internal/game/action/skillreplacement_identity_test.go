/*
===========================================================================

skillreplacement_identity_test.go - incoming caster identity at replacement

Recipient DTTP ranks must use the other-caster branch. Self release alone
may ignore its own current command, independently of an old area source.

===========================================================================
*/
package action

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/statuseffect"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
TestRecipientReplacementUsesIncomingCasterIdentity

Exercise the heal-over-time caller through the shared registry decision.
The rank is DTTP's authored rank, not the ordinary skill-family rank.
================
*/
func TestRecipientReplacementUsesIncomingCasterIdentity(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		self                  bool
		oldRank               uint32
		oldMode               uint8
		wantAllow, wantRetire bool
	}{
		{"self-lower", true, 3, 2, true, false},
		{"self-equal", true, 2, 2, true, false},
		{"other-lower", false, 3, 2, false, false},
		{"other-equal", false, 2, 2, true, true},
		{"other-higher", false, 1, 2, true, true},
		{"other-source-mode", false, 3, 1, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rt, _, caster, _ := newCombatTestRuntime(t, 100000)
			recipient := caster
			if !tc.self {
				recipient = nearbyCharacter(rt, caster, 11, "replacement-recipient", 30)
			}
			incoming := enterworld.SkillRow{ID: 70001, Group: 700,
				ReplacementPinned: true, Replacement: statuseffect.ReplacementDescriptor{
					Category: 3, Group: 700, DttpPresent: true, DttpKind: 5, DttpRank: 2,
				}}
			old := incoming
			old.ID++
			old.Replacement.DttpRank = tc.oldRank
			table := rt.deps.SkillData().(staticSkillSource)
			table[incoming.ID], table[old.ID] = incoming, old
			const oldToken = 90001
			if !rt.effects.Apply(statuseffect.Effect{DivisionID: testDivision, CharacterName: recipient.Name,
				SkillID: old.ID, SkillGroup: old.Group, InstanceToken: oldToken,
				State: statuseffect.StateActive, Phase: tc.oldMode}) {
				t.Fatal("old effect seed failed")
			}
			if got := rt.healOverTimeReplacementAllowed(testDivision, caster, recipient, incoming); got != tc.wantAllow {
				t.Fatalf("replacement allowed %v, want %v", got, tc.wantAllow)
			}
			rows := rt.effects.Snapshot(testDivision, recipient.Name)
			if len(rows) != 1 || rows[0].InstanceToken != oldToken || rows[0].StopRequested != tc.wantRetire {
				t.Fatalf("old effect retirement = %+v, want stop %v", rows, tc.wantRetire)
			}
		})
	}
}

/*
================
TestReplacementReleaseExemptionBelongsToCaster

A recipient preparing the same skill is still a different command owner.
Only the self-release wrapper may skip that current command's conflict.
================
*/
func TestReplacementReleaseExemptionBelongsToCaster(t *testing.T) {
	rt, _, caster, _ := newCombatTestRuntime(t, 100000)
	recipient := nearbyCharacter(rt, caster, 11, "busy-recipient", 30)
	row := enterworld.SkillRow{ID: 70001, Group: 700, ReplacementPinned: true,
		Replacement: statuseffect.ReplacementDescriptor{Category: 3, Group: 700, PackedStates: 7}}
	rt.deps.SkillData().(staticSkillSource)[row.ID] = row
	rt.pendingSkillFinalizesMu.Lock()
	rt.installCurrentSkillCommandLocked(testDivision, caster, 90001, row)
	rt.installCurrentSkillCommandLocked(testDivision, recipient, 90002, row)
	rt.pendingSkillFinalizesMu.Unlock()
	if rt.requestSelfEffectReplacement(testDivision, caster, row) {
		t.Fatal("ordinary self admission ignored its current command")
	}
	if !rt.healOverTimeReplacementAllowed(testDivision, caster, caster, row) {
		t.Fatal("self release conflicted with the command it releases")
	}
	if rt.healOverTimeReplacementAllowed(testDivision, caster, recipient, row) {
		t.Fatal("other recipient inherited the caster's release exemption")
	}
	if rt.requestEffectReplacement(testDivision, recipient, row, effectReplacementContext{released: true}) {
		t.Fatal("release flag without self identity bypassed recipient conflict")
	}
}

/*
================
TestShippedRevealRecipientRecastRetiresOldToken

Use the actual DTTP row and production recipient installer. Repeated reveal
must retire the old recipient token before publishing its replacement.
================
*/
func TestShippedRevealRecipientRecastRetiresOldToken(t *testing.T) {
	rt, clock, caster := concealmentFixture(t, frenzyDetectID)
	recipient := nearbyCharacter(rt, caster, 11, "revealed-recipient", 30)
	row, ok := rt.deps.SkillData().SkillByID(frenzyDetectID)
	if !ok || !row.ReplacementPinned || !row.Replacement.DttpPresent || row.Replacement.DttpKind != 5 {
		t.Fatal("fixture is not the shipped DTTP reveal")
	}
	var published []wire.Frame
	rt.PushCharacterFrames = func(division, name string, frames []wire.Frame) {
		if division == testDivision && name == recipient.Name {
			published = append(published, frames...)
		}
	}
	rt.installRecipientEffects(testDivision, []*enterworld.Character{recipient}, row, 0, clock.NowMs())
	before := rt.effects.Snapshot(testDivision, recipient.Name)
	if len(before) != 1 || before[0].Phase != 2 {
		t.Fatalf("missing initial recipient instance: %+v", before)
	}
	oldToken := before[0].InstanceToken
	published = nil
	rt.installRecipientEffects(testDivision, []*enterworld.Character{recipient}, row, 0, clock.NowMs()+1)
	after := rt.effects.Snapshot(testDivision, recipient.Name)
	if len(after) != 1 || after[0].InstanceToken == oldToken || after[0].StopRequested {
		t.Fatalf("reveal stacked or retained the old recipient instance: %+v", after)
	}
	endedAt, attachedAt := -1, -1
	for i, frame := range published {
		if frame.Opcode == wire.OpEndedEffectInstances {
			ended, err := wire.DecodeEndedEffectInstances(frame.Payload)
			if err != nil || len(ended.InstanceTokens) != 1 || ended.InstanceTokens[0] != oldToken {
				t.Fatalf("wrong reveal retirement: %+v, %v", ended, err)
			}
			endedAt = i
		}
		if frame.Opcode == wire.OpAttachedEffect {
			attachedAt = i
		}
	}
	if endedAt < 0 || attachedAt <= endedAt {
		t.Fatalf("reveal teardown/install order %d/%d: %+v", endedAt, attachedAt, published)
	}
}
