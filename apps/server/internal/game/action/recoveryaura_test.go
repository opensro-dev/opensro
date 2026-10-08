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
)

/*
================
TestRecoveryDivisionRecastReplacesSourceAndChildren
================
*/
func TestRecoveryDivisionRecastReplacesSourceAndChildren(t *testing.T) {
	skill := shippedOffense(t, "SKILL_EU_CLERIC_RECOVERYA_GROUP_A_01")
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
