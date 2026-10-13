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

The gids named by token-0 attached-effect adds of skill in the public
frames of a cast: the immediate broadcast and the release batches every
observer gets.
================
*/
func effectAdds(t *testing.T, skill enterworld.SkillRow, r OpResult, batches []simulation.DivisionFrames) []uint32 {
	t.Helper()
	frames := slices.Clone(r.Broadcast)
	for _, batch := range batches {
		if batch.OnlyCharacterID != 0 {
			continue
		}
		for _, f := range batch.Frames {
			frames = append(frames, wire.Frame{Opcode: f.Opcode, Payload: f.Payload})
		}
	}
	var gids []uint32
	for _, f := range frames {
		if f.Opcode != wire.OpAttachedEffect || len(f.Payload) < attachedEffectTokenOffset+4 {
			continue
		}
		if binary.LittleEndian.Uint32(f.Payload[attachedEffectTokenOffset:]) != recoveryEffectToken {
			continue
		}
		if binary.LittleEndian.Uint32(f.Payload[4:]) != skill.ID {
			t.Fatalf("token-0 add names skill %d, want %d", binary.LittleEndian.Uint32(f.Payload[4:]), skill.ID)
		}
		gids = append(gids, binary.LittleEndian.Uint32(f.Payload))
	}
	return gids
}

/*
==================
TestForceHealAndCureShowTheirEffectOnTheTarget

The reported bug: Force Heal and Force Cure landed with no effect on the
target. 593F50 sends each target one token-0 add (59466F -> 59AF00), so
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
target, so 594668 names the caster.
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

A resurrection inverts 59494E's test: the dead member in range gets the
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

Mana Wind (efr shape 6) leaves 593F50 through 5946C5 before the add.
Healing Orbit runs the timed handler, never 593F50: it installs its own
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

	orbit := shippedOffense(t, "SKILL_EU_CLERIC_HEALA_CYCLE_B_01")
	affordable(&orbit)
	q := newSupportParty(t, orbit)
	r, batches = q.castReleased(t, orbit, wire.SkillAction{ActionId: orbit.ID})
	if got := effectAdds(t, orbit, r, batches); len(got) != 0 {
		t.Fatalf("healing orbit token-0 adds %v", got)
	}
}
