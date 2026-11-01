/*
===========================================================================

skillrecoveryeffect_test.go - tests for skillrecoveryeffect.go: the token-0
attached-effect add every instant heal, cure and resurrection sends

The shipped rows are cast through HandleTargetInteract and released as the
client drives them; the adds are read from the frames every observer gets.

===========================================================================
*/

package action

import (
	"encoding/binary"
	"slices"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
effectAdds

The gids named by token-0 attached-effect adds of skill, each read from the
route that publishes it: the immediate result's Around and the release's
object-scoped batches. Every add must go around the object it names (the
recipient), never around the caster's own broadcast.
================
*/
func effectAdds(t *testing.T, skill enterworld.SkillRow, r OpResult, batches []simulation.DivisionFrames) []uint32 {
	t.Helper()
	routes := slices.Clone(r.Around)
	for _, batch := range batches {
		if batch.OnlyCharacterID != 0 {
			continue
		}
		route := ObservedFrames{SourceGID: batch.SourceGID}
		for _, f := range batch.Frames {
			route.Frames = append(route.Frames, wire.Frame{Opcode: f.Opcode, Payload: f.Payload})
		}
		routes = append(routes, route)
	}
	for _, f := range r.Broadcast {
		if f.Opcode == wire.OpAttachedEffect && binary.LittleEndian.Uint32(f.Payload[attachedEffectTokenOffset:]) == recoveryEffectToken {
			t.Fatal("a token-0 add rides the caster's broadcast")
		}
	}
	var gids []uint32
	for _, route := range routes {
		for _, f := range route.Frames {
			if f.Opcode != wire.OpAttachedEffect || len(f.Payload) < attachedEffectTokenOffset+4 {
				continue
			}
			if binary.LittleEndian.Uint32(f.Payload[attachedEffectTokenOffset:]) != recoveryEffectToken {
				continue
			}
			gid := binary.LittleEndian.Uint32(f.Payload)
			if binary.LittleEndian.Uint32(f.Payload[4:]) != skill.ID {
				t.Fatalf("token-0 add names skill %d, want %d", binary.LittleEndian.Uint32(f.Payload[4:]), skill.ID)
			}
			if route.SourceGID != gid {
				t.Fatalf("the add for %d is published around %d", gid, route.SourceGID)
			}
			gids = append(gids, gid)
		}
	}
	return gids
}

/*
==================
TestForceHealAndCureShowTheirEffectOnTheTarget

The reported bug: Force Heal and Force Cure landed with no effect on the
target. The native skill effect step sends each target one token-0 add, so
the client plays water_hpheal_effect_a / water_cure_effect_a on it.
==================
*/
func TestForceHealAndCureShowTheirEffectOnTheTarget(t *testing.T) {
	for _, code := range []string{"SKILL_CH_WATER_HEAL_A_01", "SKILL_CH_WATER_CURE_A_01"} {
		skill := shippedOffense(t, code)
		affordable(&skill)
		t.Run(code, func(t *testing.T) {
			p := newSupportPair(t, skill)
			mateGID := enterworld.ObjectIDForCharacter(p.m)
			r, batches := p.castReleased(t, skill, wire.SkillAction{ActionId: skill.ID, HasTarget: true, TargetGid: mateGID})
			if got := effectAdds(t, skill, r, batches); !slices.Equal(got, []uint32{mateGID}) {
				t.Fatalf("effect adds %v, want the target %d once", got, mateGID)
			}
		})
	}
}

/*
==================
TestSelfHealShowsItsEffectOnTheCaster

An untargeted instant heal has an empty action vector and no column-22
target, so the native skill effect step names the caster.
==================
*/
func TestSelfHealShowsItsEffectOnTheCaster(t *testing.T) {
	skill := shippedOffense(t, "SKILL_CH_WATER_SELFHEAL_A_01")
	p := newSupportPair(t, skill)
	r, batches := p.castReleased(t, skill, wire.SkillAction{ActionId: skill.ID})
	casterGID := enterworld.ObjectIDForCharacter(p.c)
	if got := effectAdds(t, skill, r, batches); !slices.Equal(got, []uint32{casterGID}) {
		t.Fatalf("effect adds %v, want the caster %d once", got, casterGID)
	}
}

/*
==================
TestGroupHealingShowsItsEffectOnEveryLivingMember

Group Healing's vector is the caster and the living members within 250;
each gets one add, the member at 300 and the dead one none.
==================
*/
func TestGroupHealingShowsItsEffectOnEveryLivingMember(t *testing.T) {
	skill := shippedOffense(t, "SKILL_EU_CLERIC_HEALA_GROUP_A_01")
	p := newSupportParty(t, skill, 100, 240, 300, 50)
	*p.mates[4].CurrentHP = 0
	r, batches := p.castReleased(t, skill, wire.SkillAction{ActionId: skill.ID})

	got := effectAdds(t, skill, r, batches)
	want := []uint32{enterworld.ObjectIDForCharacter(p.c)}
	for _, m := range p.mates[:3] {
		want = append(want, enterworld.ObjectIDForCharacter(m))
	}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("effect adds %v, want %v", got, want)
	}
}

/*
==================
TestGroupReverseShowsItsEffectOnlyOnTheDead

A resurrection inverts the native alive test: the dead member in range gets the
add, the living one does not, and with resu set the caster is never named.
==================
*/
func TestGroupReverseShowsItsEffectOnlyOnTheDead(t *testing.T) {
	skill := shippedOffense(t, "SKILL_EU_CLERIC_REBIRTHA_GROUP_A_01")
	affordable(&skill)
	p := newSupportParty(t, skill, 100, 400)
	dead, alive, farDead := p.mates[0], p.mates[1], p.mates[2]
	*dead.CurrentHP, *alive.CurrentHP, *farDead.CurrentHP = 0, 100, 0
	r, batches := p.castReleased(t, skill, wire.SkillAction{ActionId: skill.ID})
	if got, want := effectAdds(t, skill, r, batches), []uint32{enterworld.ObjectIDForCharacter(dead)}; !slices.Equal(got, want) {
		t.Fatalf("effect adds %v, want %v", got, want)
	}
}

/*
==================
TestShapeSixHealAndHealOverTimeSendNoOneShot

Mana Wind and Healing Division (efr shape 6) leave the native skill effect step early
before the add whenever their party vector holds anyone.
Healing Orbit runs the timed handler, never the skill effect step: it installs its own
effect with a real token and sends no token-0 add.
==================
*/
func TestShapeSixHealAndHealOverTimeSendNoOneShot(t *testing.T) {
	wind := shippedOffense(t, "SKILL_EU_BARD_RECOVERA_MANATRANS_A_01")
	affordable(&wind)
	p := newSupportParty(t, wind, 50)
	equipHarp(p.supportPair)
	r, batches := p.castReleased(t, wind, wire.SkillAction{ActionId: wind.ID, HasTarget: true, TargetGid: enterworld.ObjectIDForCharacter(p.m)})
	if got := effectAdds(t, wind, r, batches); len(got) != 0 {
		t.Fatalf("mana wind effect adds %v", got)
	}

	// Healing Division is an untargeted shape-6 heal: its vector is the
	// party list, and with members in range the native step skips the tail, so the
	// caster gets no add either.
	division := shippedOffense(t, "SKILL_EU_CLERIC_HEALA_DIVIDE_A_01")
	if area := division.Abnormal.EffectArea; division.TargetRequired || !area.Present || area.Shape != healSecondaryShape {
		t.Fatalf("healing division area %+v targeted %v", area, division.TargetRequired)
	}
	affordable(&division)
	d := newSupportParty(t, division, 50, 80)
	r, batches = d.castReleased(t, division, wire.SkillAction{ActionId: division.ID})
	if got := effectAdds(t, division, r, batches); len(got) != 0 {
		t.Fatalf("healing division effect adds %v", got)
	}

	orbit := shippedOffense(t, "SKILL_EU_CLERIC_HEALA_CYCLE_B_01")
	affordable(&orbit)
	q := newSupportParty(t, orbit)
	r, batches = q.castReleased(t, orbit, wire.SkillAction{ActionId: orbit.ID})
	if got := effectAdds(t, orbit, r, batches); len(got) != 0 {
		t.Fatalf("healing orbit token-0 adds %v", got)
	}
}

/*
==================
TestAnInstantSelfHealAtFullHealthStillShowsItsEffect

Natively the add is sent whatever the heal changed: a zero-cast self heal at
full HP returns it in the immediate result, around the caster, while the
caster's own burst stays the cast result alone.
==================
*/
func TestAnInstantSelfHealAtFullHealthStillShowsItsEffect(t *testing.T) {
	skill := shippedOffense(t, "SKILL_CH_WATER_SELFHEAL_A_01")
	skill.ActionCastingTimeMs = 0
	p := newSupportPair(t, skill)
	p.c.CurrentHP = testInt64(enterworld.DerivedMaxHP(p.c))
	r := assertAndSeparateActionSession(t, p.rt.HandleTargetInteract(testDivision, p.c, wire.SkillAction{ActionId: skill.ID}.Encode()))
	assertOpcodes(t, r.Frames, wire.OpSkillCastResult)
	casterGID := enterworld.ObjectIDForCharacter(p.c)
	if got := effectAdds(t, skill, r, nil); !slices.Equal(got, []uint32{casterGID}) {
		t.Fatalf("effect adds %v, want the caster %d once", got, casterGID)
	}
}
