/*
===========================================================================

recoveryaura_test.go - source-linked replacement through real aura casts

Recovery Division must replace its previous source and recipient instances;
counting icons alone would miss duplicate healing jobs behind one icon.

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
TestRecoveryDivisionRecastReplacesSourceAndChildren
================
*/
func TestRecoveryDivisionRecastReplacesSourceAndChildren(t *testing.T) {
	for _, code := range []string{
		"SKILL_EU_CLERIC_RECOVERYA_GROUP_A_01",
		"SKILL_EU_CLERIC_RECOVERYA_GROUP_A_02",
		"SKILL_EU_CLERIC_RECOVERYA_GROUP_A_03",
		"SKILL_EU_CLERIC_RECOVERYA_GROUP_A_04",
		"SKILL_EU_CLERIC_RECOVERYA_GROUP_A_05",
		"SKILL_EU_CLERIC_RECOVERYA_GROUP_A_06",
		"SKILL_EU_CLERIC_RECOVERYA_GROUP_A_07",
		"SKILL_EU_CLERIC_RECOVERYA_GROUP_A_08",
		"SKILL_EU_CLERIC_RECOVERYA_GROUP_B_01",
	} {
		t.Run(code, func(t *testing.T) { checkRecoveryRecast(t, code) })
	}
}

/*
================
checkRecoveryRecast
================
*/
func checkRecoveryRecast(t *testing.T, code string) {
	t.Helper()
	skill := shippedOffense(t, code)
	affordable(&skill)
	p := newSupportPair(t, skill)
	if !skill.Replacement.Efr2 || skill.Replacement.MatchesExecutionSelector {
		t.Fatalf("fixture no longer takes linked-area replacement: %+v", skill.Replacement)
	}
	var previous uint32
	for cast := 0; cast < 3; cast++ {
		// Each pass starts with enough MP; resource exhaustion is covered by
		// aura lifetime tests, not this replacement sequence.
		p.c.CurrentMP = testInt64(50000)
		result := castSelf(p.rt, p.c, skill.ID)
		if result.DiagnosticRefusal != "" || len(result.Frames) == 0 || result.Frames[0].Payload[0] == 2 {
			t.Fatalf("cast %d refused: %+v", cast, result)
		}
		p.rt.TickHook()(p.clock.NowMs())
		if len(p.rt.partyAuras) != 1 {
			t.Fatalf("cast %d left %d healing sources, want one", cast, len(p.rt.partyAuras))
		}
		aura := p.rt.partyAuras[0]
		if aura.token == previous {
			t.Fatal("recast kept the previous source instance")
		}
		previous = aura.token
		for _, recipient := range []*enterworld.Character{p.c, p.m} {
			var count int
			for _, effect := range p.rt.effects.Snapshot(testDivision, recipient.Name) {
				if effect.SkillID != skill.ID || effect.StopRequested {
					continue
				}
				count++
				if recipient == p.m && effect.AuraParentToken != aura.token {
					t.Fatalf("recipient retained child of retired source %d", effect.AuraParentToken)
				}
			}
			if count != 1 {
				t.Fatalf("cast %d: %s has %d active recovery instances", cast, recipient.Name, count)
			}
		}
		bardTick(p.rt, p.clock, time.Duration(skill.CoolTimeMs+skill.ActionCastingTimeMs+1)*time.Millisecond)
	}
}

/*
================
TestRecoveryDivisionOtherCasterReplacesLinkedSource

59D9CE..59DAB0 follows a recipient's area link back to the original caster
and stops both instances, even when the new spell comes from somebody else.
================
*/
func TestRecoveryDivisionOtherCasterReplacesLinkedSource(t *testing.T) {
	skill := shippedOffense(t, "SKILL_EU_CLERIC_RECOVERYA_GROUP_A_01")
	affordable(&skill)
	p := newSupportPair(t, skill)
	for _, caster := range []*enterworld.Character{p.c, p.m} {
		result := castSelf(p.rt, caster, skill.ID)
		if result.DiagnosticRefusal != "" || len(result.Frames) == 0 || result.Frames[0].Payload[0] == 2 {
			t.Fatalf("%s cast refused: %+v", caster.Name, result)
		}
		p.rt.TickHook()(p.clock.NowMs())
		if len(p.rt.partyAuras) != 1 || p.rt.partyAuras[0].casterName != caster.Name {
			t.Fatalf("%s cast left aura sources %+v", caster.Name, p.rt.partyAuras)
		}
		for _, recipient := range []*enterworld.Character{p.c, p.m} {
			var count int
			for _, effect := range p.rt.effects.Snapshot(testDivision, recipient.Name) {
				if effect.SkillID != skill.ID || effect.StopRequested {
					continue
				}
				count++
				if effect.AreaSourceGID != enterworld.ObjectIDForCharacter(caster) || effect.AreaSourceName != caster.Name {
					t.Fatalf("%s has stale source link: %+v", recipient.Name, effect)
				}
			}
			if count != 1 {
				t.Fatalf("%s has %d live effects after %s cast", recipient.Name, count, caster.Name)
			}
		}
		bardTick(p.rt, p.clock, time.Duration(skill.CoolTimeMs+skill.ActionCastingTimeMs+1)*time.Millisecond)
	}
}

/*
================
TestRecoveryDivisionSimultaneousSourcesDoNotCancelEachOther

Both sources exist before either has joined the other caster. The first
join retires the other source; that source must not heal or join afterward.
================
*/
func TestRecoveryDivisionSimultaneousSourcesDoNotCancelEachOther(t *testing.T) {
	skill := shippedOffense(t, "SKILL_EU_CLERIC_RECOVERYA_GROUP_A_01")
	affordable(&skill)
	p := newSupportPair(t, skill)
	for _, caster := range []*enterworld.Character{p.c, p.m} {
		result := castSelf(p.rt, caster, skill.ID)
		if result.DiagnosticRefusal != "" || len(result.Frames) == 0 || result.Frames[0].Payload[0] == 2 {
			t.Fatalf("%s cast refused: %+v", caster.Name, result)
		}
	}
	before := *p.m.CurrentHP
	p.rt.TickHook()(p.clock.NowMs())
	if *p.m.CurrentHP != before {
		t.Fatalf("retired source healed after being replaced: %d -> %d", before, *p.m.CurrentHP)
	}
	if len(p.rt.partyAuras) != 1 || p.rt.partyAuras[0].casterName != p.c.Name {
		t.Fatalf("simultaneous casts left wrong sources: %+v", p.rt.partyAuras)
	}
	for _, recipient := range []*enterworld.Character{p.c, p.m} {
		rows := p.rt.effects.Snapshot(testDivision, recipient.Name)
		if len(rows) != 1 || rows[0].StopRequested || rows[0].AreaSourceName != p.c.Name {
			t.Fatalf("%s retained wrong effects: %+v", recipient.Name, rows)
		}
	}
	bardTick(p.rt, p.clock, time.Duration(skill.Abnormal.Pulse)*time.Millisecond)
	if len(p.rt.partyAuras) != 1 || !p.rt.auraInstanceLive(p.rt.partyAuras[0]) {
		t.Fatal("surviving source disappeared on its next pulse")
	}
}

/*
================
TestRecoveryDivisionCancelledChildRejoinsWithoutStoppingSource

A stopped child must be retired before the same scan tries to replace it.
Otherwise its old area link incorrectly stops the still-live source.
================
*/
func TestRecoveryDivisionCancelledChildRejoinsWithoutStoppingSource(t *testing.T) {
	skill := shippedOffense(t, "SKILL_EU_CLERIC_RECOVERYA_GROUP_A_01")
	affordable(&skill)
	p := newSupportPair(t, skill)
	other := partyMate(p.rt, p.c, 12, "other-recovery-member", 20)
	setParty(p.rt, p.c, p.m, other)
	result := castSelf(p.rt, p.c, skill.ID)
	if result.DiagnosticRefusal != "" || len(result.Frames) == 0 || result.Frames[0].Payload[0] == 2 {
		t.Fatalf("initial cast refused: %+v", result)
	}
	p.rt.TickHook()(p.clock.NowMs())
	if len(p.rt.partyAuras) != 1 {
		t.Fatal("initial source missing")
	}
	aura := p.rt.partyAuras[0]
	child, otherChild := aura.members[p.m.Name], aura.members[other.Name]
	if child == 0 || otherChild == 0 {
		t.Fatal("initial recipient instances missing")
	}
	recorded := recordMemberWire(p.rt, p.c, p.m, other)
	p.rt.HandleTargetInteract(testDivision, p.m, wire.CancelActiveEffectRequest{
		EffectID: skill.ID, InstanceToken: child,
	}.Encode())
	if p.rt.instanceLive(testDivision, p.m.Name, skill.ID, child) {
		t.Fatal("cancel did not stop the child's instance")
	}
	recorded.tick(p.clock, time.Duration(skill.Abnormal.Pulse)*time.Millisecond)
	if len(p.rt.partyAuras) != 1 || p.rt.partyAuras[0].token != aura.token || !p.rt.auraInstanceLive(aura) {
		t.Fatal("a recipient cancellation stopped the caster's source")
	}
	if !p.rt.instanceLive(testDivision, other.Name, skill.ID, otherChild) {
		t.Fatal("a recipient cancellation removed another member's instance")
	}
	rows := p.rt.effects.Snapshot(testDivision, p.m.Name)
	if len(rows) != 1 || rows[0].InstanceToken == child || rows[0].AuraParentToken != aura.token || rows[0].StopRequested {
		t.Fatalf("cancelled child did not rejoin its original source: %+v", rows)
	}
	recorded.check(t)
}

/*
================
TestRecoveryDivisionConflictStateEndsWithItsSource

Same-family higher ranks replace before conflict checks. A downgrade or
another family sharing the authored state is refused until the source ends.
================
*/
func TestRecoveryDivisionConflictStateEndsWithItsSource(t *testing.T) {
	for _, tc := range []struct {
		name, oldCode, newCode string
		replaces               bool
	}{
		{"upgrade", "SKILL_EU_CLERIC_RECOVERYA_GROUP_A_01", "SKILL_EU_CLERIC_RECOVERYA_GROUP_A_08", true},
		{"downgrade", "SKILL_EU_CLERIC_RECOVERYA_GROUP_A_08", "SKILL_EU_CLERIC_RECOVERYA_GROUP_A_01", false},
		{"other-tier", "SKILL_EU_CLERIC_RECOVERYA_GROUP_A_01", "SKILL_EU_CLERIC_RECOVERYA_GROUP_B_01", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			old, incoming := shippedOffense(t, tc.oldCode), shippedOffense(t, tc.newCode)
			affordable(&old)
			affordable(&incoming)
			p := newSupportPair(t, old, incoming)
			result := castSelf(p.rt, p.c, old.ID)
			if result.DiagnosticRefusal != "" || len(result.Frames) == 0 || result.Frames[0].Payload[0] == 2 {
				t.Fatalf("initial cast refused: %+v", result)
			}
			p.rt.TickHook()(p.clock.NowMs())
			if len(p.rt.partyAuras) != 1 {
				t.Fatal("initial source missing")
			}
			source := p.rt.partyAuras[0]
			bardTick(p.rt, p.clock, time.Duration(old.CoolTimeMs+old.ActionCastingTimeMs+old.ActionDurationMs+1)*time.Millisecond)
			p.c.CurrentMP = testInt64(50000)
			result = castSelf(p.rt, p.c, incoming.ID)
			if tc.replaces {
				if result.DiagnosticRefusal != "" || len(result.Frames) == 0 || result.Frames[0].Payload[0] == 2 {
					t.Fatalf("higher rank refused: %+v", result)
				}
				p.rt.TickHook()(p.clock.NowMs())
				if len(p.rt.partyAuras) != 1 || p.rt.partyAuras[0].skillID != incoming.ID {
					t.Fatal("higher rank did not replace the lower source")
				}
				rows := p.rt.effects.Snapshot(testDivision, p.m.Name)
				if len(rows) != 1 || rows[0].SkillID != incoming.ID || rows[0].StopRequested {
					t.Fatalf("higher rank did not replace the recipient: %+v", rows)
				}
				return
			}
			if len(result.Frames) != 1 || len(result.Frames[0].Payload) != 2 ||
				result.Frames[0].Payload[0] != 2 || result.Frames[0].Payload[1] != 0x0c {
				t.Fatalf("conflicting recovery cast was not refused with 0x300c: %+v", result)
			}
			if !p.rt.auraInstanceLive(source) || len(p.rt.partyAuras) != 1 {
				t.Fatal("refused cast changed the original source")
			}
			if !p.rt.instanceLive(testDivision, p.m.Name, old.ID, source.members[p.m.Name]) {
				t.Fatal("refused cast removed the original recipient instance")
			}
			p.rt.HandleTargetInteract(testDivision, p.c, wire.CancelActiveEffectRequest{
				EffectID: old.ID, InstanceToken: source.token,
			}.Encode())
			p.rt.TickHook()(p.clock.NowMs())
			result = castSelf(p.rt, p.c, incoming.ID)
			if result.DiagnosticRefusal != "" || len(result.Frames) == 0 || result.Frames[0].Payload[0] == 2 {
				t.Fatalf("ended aura left a stale conflict: %+v", result)
			}
		})
	}
}
