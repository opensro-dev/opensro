/*
===========================================================================

cosdebuff_test.go - Vital Spot on a companion: the COS recipient arm

The cast deals no damage and attributes no kill: the word installs on
the pet, judged through its owner, lowers the pet's hit rate at both
stat read sites, retires at expiry, and leaves with the pet's death.

===========================================================================
*/
package action

import (
	"bytes"
	"encoding/binary"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
)

/*
================
teachVitalSpot
================
*/
func teachVitalSpot(t *testing.T, rt *Runtime, c *enterworld.Character, code string) enterworld.SkillRow {
	t.Helper()
	skill := shippedOffense(t, code)
	muscle := shippedOffense(t, "SKILL_CH_WATER_CANCEL_A_01")
	c.Skills = []uint32{muscle.ID, skill.ID}
	c.Masteries = []enterworld.CharacterMastery{{ID: 276, Level: 50}}
	c.Intellect = testInt64(2000)
	c.CurrentMP = testInt64(10000)
	rt.deps.SkillData().(staticSkillSource)[muscle.ID] = muscle
	rt.deps.SkillData().(staticSkillSource)[skill.ID] = skill
	return skill
}

/*
================
TestCosDebuffVitalSpotInstallsAndLowersHitRate
================
*/
func TestCosDebuffVitalSpotInstallsAndLowersHitRate(t *testing.T) {
	rt, clock, attacker, owner := newPvpPair(t)
	ref := equipStrikeTestCOS(t, rt, owner)
	skill := teachVitalSpot(t, rt, attacker, "SKILL_CH_WATER_CANCEL_B_01")
	pet := owner.ActiveCOS
	now := clock.NowMs()
	words := func() []monster.SelfEffect { return rt.cosDebuffWords(testDivision, owner.Name, pet.GID, now) }
	hitRate := func() float64 {
		stats, err := cosCombatStats(ref, pet, nil, words())
		if err != nil {
			t.Fatal(err)
		}
		return stats.HitRate
	}
	before := hitRate()
	out := rt.HandleTargetInteract(testDivision, attacker, wire.SkillAction{ActionId: skill.ID, HasTarget: true, TargetGid: pet.GID}.Encode())
	if _, ok := findFrame(out.Frames, wire.OpSkillCastResult); !ok {
		t.Fatalf("Vital Spot on the COS refused: %+v (%s)", out, out.DiagnosticRefusal)
	}
	attached, ok := findFrame(out.Frames, wire.OpAttachedEffect)
	if !ok {
		t.Fatal("no recipient instance frame", out.Frames)
	}
	tag, value := skill.HostileDebuff.Word()
	held := words()
	if len(held) != 1 || held[0].SkillID != skill.ID || held[0].Tag != tag || held[0].First != value ||
		held[0].UntilMs != now+int64(skill.HostileDebuff.DurationMs) {
		t.Fatalf("held words %+v", held)
	}
	if binary.LittleEndian.Uint32(attached.Payload[4:]) != skill.ID || binary.LittleEndian.Uint32(attached.Payload[8:]) != held[0].Token {
		t.Fatalf("recipient frame %x", attached.Payload)
	}
	// No damage, no kill: the pet and its owner keep every hit point.
	if pet.CurrentHP != ref.MaxHP || *owner.CurrentHP != 100 {
		t.Fatalf("debuff dealt damage: pet %d/%d, owner %d", pet.CurrentHP, ref.MaxHP, *owner.CurrentHP)
	}
	if got := hitRate(); got != max(before-float64(value), 0) || (value > 0 && got >= before) {
		t.Fatalf("hit rate %v -> %v, want -%d floored", before, got, value)
	}
	// Expiry retires the word and restores the stat.
	rt.advanceCosAbnormals(held[0].UntilMs + 1)
	if held := words(); len(held) != 0 {
		t.Fatalf("word survived expiry: %+v", held)
	}
	if got := hitRate(); got != before {
		t.Fatalf("hit rate %v after expiry, want %v", got, before)
	}
}

/*
================
TestCosDebuffWordStoreReplacesByTagAndCaps
================
*/
func TestCosDebuffWordStoreReplacesByTagAndCaps(t *testing.T) {
	rt, _, _, _ := newCombatTestRuntime(t, 1000)
	word := func(skill, token, tag, first uint32) monster.SelfEffect {
		return monster.SelfEffect{SkillID: skill, Token: token, Tag: tag, First: first, StartedAtMs: 0, UntilMs: 1000}
	}
	if _, ok := rt.installCosDebuffWord(testDivision, "owner", 7, word(1, 10, cosDebuffHitRateTag, 5), 0); !ok {
		t.Fatal("first install refused")
	}
	replaced, ok := rt.installCosDebuffWord(testDivision, "owner", 7, word(1, 11, cosDebuffHitRateTag, 6), 0)
	if !ok || replaced != 10 {
		t.Fatalf("same-tag recast replaced %d ok %v", replaced, ok)
	}
	if _, ok := rt.installCosDebuffWord(testDivision, "owner", 7, word(2, 12, cosDebuffEvasionTag, 7), 0); !ok {
		t.Fatal("distinct tag refused")
	}
	if held := rt.cosDebuffWords(testDivision, "owner", 7, 0); len(held) != 2 {
		t.Fatalf("held %+v", held)
	}
	for token := uint32(20); token < 20+cosDebuffWordCap; token++ {
		rt.installCosDebuffWord(testDivision, "owner", 7, word(3, token, 1000+token, 1), 0)
	}
	if _, ok := rt.installCosDebuffWord(testDivision, "owner", 7, word(3, 99, 9999, 1), 0); ok {
		t.Fatal("ninth distinct word admitted past the cap")
	}
	if tokens := rt.expireCosDebuffWords(testDivision, "owner", 7, 1001); len(tokens) == 0 {
		t.Fatal("expiry retired nothing")
	}
	if held := rt.cosDebuffWords(testDivision, "owner", 7, 1001); len(held) != 0 {
		t.Fatalf("held after expiry %+v", held)
	}
}

/*
================
TestCosDebuffWordsLeaveWithThePetsDeath
================
*/
func TestCosDebuffWordsLeaveWithThePetsDeath(t *testing.T) {
	rt, clock, _, owner := newPvpPair(t)
	equipStrikeTestCOS(t, rt, owner)
	pet := owner.ActiveCOS
	word := monster.SelfEffect{SkillID: 1, Token: 42, Tag: cosDebuffEvasionTag, First: 5,
		StartedAtMs: clock.NowMs(), UntilMs: clock.NowMs() + 60000}
	if _, ok := rt.installCosDebuffWord(testDivision, owner.Name, pet.GID, word, clock.NowMs()); !ok {
		t.Fatal("install refused")
	}
	pet.CurrentHP = 0
	frames := rt.advanceCosAbnormals(clock.NowMs())
	if held := rt.cosDebuffWords(testDivision, owner.Name, pet.GID, clock.NowMs()); len(held) != 0 {
		t.Fatalf("word survived the pet's death: %+v", held)
	}
	retired := false
	for _, division := range frames {
		for _, frame := range division.Frames {
			if frame.Opcode != wire.OpEndedEffectInstances {
				continue
			}
			ended, err := wire.DecodeEndedEffectInstances(frame.Payload)
			if err == nil && len(ended.InstanceTokens) == 1 && ended.InstanceTokens[0] == 42 {
				retired = true
			}
		}
	}
	if !retired {
		t.Fatal("the dead pet's word token never retired on the wire")
	}
}

/*
================
TestCosDebuffAdmissionKeepsTheTauntRefusal

Through the real cast door: a taunt row aimed at the companion still
refuses 0x3006 and installs nothing, while the Vital Spot debuff the
same caster then casts is accepted and installs its word.
================
*/
func TestCosDebuffAdmissionKeepsTheTauntRefusal(t *testing.T) {
	rt, clock, attacker, owner := newPvpPair(t)
	equipStrikeTestCOS(t, rt, owner)
	skill := teachVitalSpot(t, rt, attacker, "SKILL_CH_WATER_CANCEL_B_01")
	pet := owner.ActiveCOS
	now := clock.NowMs()
	taunt := shippedOffense(t, "SKILL_CH_WATER_CANCEL_A_01")
	taunt.Threat.Only = true
	rt.deps.SkillData().(staticSkillSource)[taunt.ID] = taunt
	refused := rt.HandleTargetInteract(testDivision, attacker, wire.SkillAction{ActionId: taunt.ID, HasTarget: true, TargetGid: pet.GID}.Encode())
	wantRefusal := offensiveRefusal(0x3006).Frames[0]
	refusalSeen := false
	for _, frame := range refused.Frames {
		if frame.Opcode == wantRefusal.Opcode && bytes.Equal(frame.Payload, wantRefusal.Payload) {
			refusalSeen = true
		}
	}
	if !refusalSeen {
		t.Fatalf("taunt on the COS not refused 0x3006: %+v (%s)", refused, refused.DiagnosticRefusal)
	}
	if held := rt.cosDebuffWords(testDivision, owner.Name, pet.GID, now); len(held) != 0 {
		t.Fatalf("a refused taunt installed words: %+v", held)
	}
	out := rt.HandleTargetInteract(testDivision, attacker, wire.SkillAction{ActionId: skill.ID, HasTarget: true, TargetGid: pet.GID}.Encode())
	if _, ok := findFrame(out.Frames, wire.OpSkillCastResult); !ok {
		t.Fatalf("Vital Spot on the COS refused: %+v (%s)", out, out.DiagnosticRefusal)
	}
	if held := rt.cosDebuffWords(testDivision, owner.Name, pet.GID, now); len(held) != 1 {
		t.Fatalf("accepted debuff held words %+v", held)
	}
}
