/*
===========================================================================

hitmarch_test.go - the hr block of 594AC0 on skill buffs and item programs

Hit March and Clout March are Bard party auras whose only payload is hr: the
caster and every joined member must gain hit rate (parameter 11) for as long
as the aura lives, and each must see its new hit rate in its own 0x343C.
Timed item programs read the same tag and must still install it exactly once.

===========================================================================
*/

package action

import (
	"fmt"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/paramkeeper"
	"opensro.online/server/internal/testsupport/gamedatatest"
)

const (
	hitMarchFirstID   = 9777 // SKILL_EU_BARD_SPEEDUPA_HITRATE_A_01
	cloutMarchID      = 9782 // SKILL_EU_BARD_SPEEDUPA_HITRATE_B_01
	hitMarchRadius    = 700  // efr(2,1,700,8,0,5)
	hitScrollCodename = "SKILL_ETC_E051123_HIT_SCROLL_01"
	chBowCallCodename = "SKILL_CH_BOW_CALL_A_01"
	maxScannedSkillID = 60000
)

// hitMarchFlat is the authored hr word 0 of each shipped rank (word 1 is 0).
var hitMarchFlat = map[uint32]float64{9777: 53, 9778: 63, 9779: 69, 9780: 74, 9781: 79, cloutMarchID: 84}

/*
================
hitRate

The evaluated parameter 11 of a character, through the same stats path
combat reads.
================
*/
func hitRate(t *testing.T, rt *Runtime, c *enterworld.Character) float64 {
	t.Helper()
	stats, _, err := rt.playerCombatStats(testDivision, c)
	if err != nil {
		t.Fatal(err)
	}
	return stats.HitRate
}

/*
================
hitRateWrites

The installed parameter-11 writes of one character, by channel.
================
*/
func hitRateWrites(rt *Runtime, name string) map[paramkeeper.Channel][]float32 {
	out := map[paramkeeper.Channel][]float32{}
	for _, w := range rt.effects.ModifierWrites(testDivision, name) {
		if w.Parameter == itemParamAccuracy {
			out[w.Channel] = append(out[w.Channel], w.Value)
		}
	}
	return out
}

/*
================
TestHitMarchEveryShippedRankRaisesPartyHitRate

Every shipped Hit March rank and Clout March install their flat hr on the
caster at the cast and on a party member at the aura's join walk; a member
outside the efr radius gets nothing. Cancelling the caster's instance retires
the aura and both contributions.
================
*/
func TestHitMarchEveryShippedRankRaisesPartyHitRate(t *testing.T) {
	for id := uint32(hitMarchFirstID); id <= cloutMarchID; id++ {
		t.Run(fmt.Sprint(id), func(t *testing.T) {
			rt, clock, c, row := marchFixture(t, id)
			want := hitMarchFlat[id]
			if !row.Aura.Present || !row.BuffModifiers.Hr || float64(row.BuffModifiers.HrFlat) != want ||
				row.BuffModifiers.HrRate != 0 || row.Aura.Radius != hitMarchRadius {
				t.Fatalf("unexpected shipped row: aura %+v, modifiers %+v", row.Aura, row.BuffModifiers)
			}
			mate := nearbyCharacter(rt, c, 12, "hit-mate", hitMarchRadius-1)
			outside := nearbyCharacter(rt, c, 13, "hit-outside", hitMarchRadius+50)
			rt.RewardParties = func(string) []RewardParty {
				return []RewardParty{{Members: []uint32{enterworld.ObjectIDForCharacter(c), enterworld.ObjectIDForCharacter(mate), enterworld.ObjectIDForCharacter(outside)}}}
			}
			baseCaster, baseMate, baseOutside := hitRate(t, rt, c), hitRate(t, rt, mate), hitRate(t, rt, outside)

			if result := castSelf(rt, c, id); result.DiagnosticRefusal != "" || !hasSkillEffect(rt, c.Name, id) {
				t.Fatalf("aura cast refused: %+v", result)
			}
			rt.advancePartyAuras(clock.NowMs())
			if !hasSkillEffect(rt, mate.Name, id) || hasSkillEffect(rt, outside.Name, id) {
				t.Fatal("join walk selected the wrong members")
			}
			for _, who := range []struct {
				c    *enterworld.Character
				base float64
				gain float64
			}{{c, baseCaster, want}, {mate, baseMate, want}, {outside, baseOutside, 0}} {
				if got := hitRate(t, rt, who.c) - who.base; got != who.gain {
					t.Errorf("%s hit rate gained %v, want %v", who.c.Name, got, who.gain)
				}
			}
			writes := hitRateWrites(rt, mate.Name)
			if len(writes[paramkeeper.Flat]) != 1 || len(writes[paramkeeper.PercentSum]) != 1 {
				t.Fatalf("member hr writes %+v, want one flat and one percent sum", writes)
			}

			token := rt.effects.Snapshot(testDivision, c.Name)[0].InstanceToken
			rt.HandleTargetInteract(testDivision, c, wire.CancelActiveEffectRequest{EffectID: id, InstanceToken: token}.Encode())
			rt.drainStoppedCharacterEffects()
			rt.advancePartyAuras(clock.NowMs())
			rt.drainStoppedCharacterEffects()
			if hitRate(t, rt, c) != baseCaster || hitRate(t, rt, mate) != baseMate {
				t.Fatalf("retired aura left hit rate %v/%v, want %v/%v", hitRate(t, rt, c), hitRate(t, rt, mate), baseCaster, baseMate)
			}
		})
	}
}

/*
================
TestHitMarchPublishesStatsAtInstallAndJoin

The hr installed by the aura must reach the clients: the caster's cast burst
carries its raised 0x343C privately (never to observers), and the join walk
routes the raised block to the joining member alone.
================
*/
func TestHitMarchPublishesStatsAtInstallAndJoin(t *testing.T) {
	rt, clock, c, _ := marchFixture(t, hitMarchFirstID)
	mate := nearbyCharacter(rt, c, 12, "hit-stats-mate", hitMarchRadius-1)
	rt.RewardParties = func(string) []RewardParty {
		return []RewardParty{{Members: []uint32{enterworld.ObjectIDForCharacter(c), enterworld.ObjectIDForCharacter(mate)}}}
	}
	gain := uint16(hitMarchFlat[hitMarchFirstID])
	casterBefore, err := rt.PlayerBaseStats(testDivision, c)
	if err != nil {
		t.Fatal(err)
	}
	mateBefore, err := rt.PlayerBaseStats(testDivision, mate)
	if err != nil {
		t.Fatal(err)
	}

	result := castSelf(rt, c, hitMarchFirstID)
	if result.DiagnosticRefusal != "" || !hasSkillEffect(rt, c.Name, hitMarchFirstID) {
		t.Fatalf("aura cast refused: %+v", result)
	}
	casterAfter, err := rt.PlayerBaseStats(testDivision, c)
	if err != nil {
		t.Fatal(err)
	}
	if casterAfter.HitRate != casterBefore.HitRate+gain {
		t.Fatalf("caster hit rate %d, want %d", casterAfter.HitRate, casterBefore.HitRate+gain)
	}
	if !hasStatsFrame(result.Frames, casterAfter) || !hasStatsFrame(result.ActorPrivate, casterAfter) || hasOpcode(result.Broadcast, wire.OpBaseStats) {
		t.Fatalf("caster stats not published privately: frames %+v, private %+v", result.Frames, result.ActorPrivate)
	}

	out := rt.advancePartyAuras(clock.NowMs())
	mateAfter, err := rt.PlayerBaseStats(testDivision, mate)
	if err != nil {
		t.Fatal(err)
	}
	if mateAfter.HitRate != mateBefore.HitRate+gain {
		t.Fatalf("member hit rate %d, want %d", mateAfter.HitRate, mateBefore.HitRate+gain)
	}
	published := false
	for _, batch := range out {
		for _, frame := range batch.Frames {
			if frame.Opcode != wire.OpBaseStats {
				continue
			}
			if batch.OnlyCharacterID != mate.ID || string(frame.Payload) != string(mateAfter.Encode()) {
				t.Fatalf("stats routed to %d with %x, want member %d with %x", batch.OnlyCharacterID, frame.Payload, mate.ID, mateAfter.Encode())
			}
			published = true
		}
	}
	if !published {
		t.Fatalf("join walk published no member stats: %+v", out)
	}
}

// hasStatsFrame finds a 0x343C carrying exactly want.
func hasStatsFrame(frames []wire.Frame, want wire.BaseStats) bool {
	for _, frame := range frames {
		if frame.Opcode == wire.OpBaseStats && string(frame.Payload) == string(want.Encode()) {
			return true
		}
	}
	return false
}

/*
================
TestShippedHrRowsFileHitRateOnce

Every shipped row carrying hr - item programs (hit scrolls, Pepero, quest
rewards), CH bow rows and the Bard marches - files exactly one percent-sum
and one flat write on parameter 11 with its authored words, whichever of the
two parsers owns the block.
================
*/
func TestShippedHrRowsFileHitRateOnce(t *testing.T) {
	rt, _, c, _ := newCombatTestRuntime(t, 100000)
	skills := shippedSkills(t)
	covered := map[string]bool{}
	for id := uint32(1); id < maxScannedSkillID; id++ {
		row, ok := skills.SkillByID(id)
		if !ok || !row.BuffModifiers.Hr {
			continue
		}
		covered[row.Codename] = true
		writes := buffModifierWrites(row.BuffModifiers, itemProgramWritesAccuracy(row.TimedEffect))
		items, err := rt.timedItemModifierWrites(testDivision, c, row.TimedEffect)
		if err != nil {
			t.Fatal(err)
		}
		writes = append(writes, items...)
		var flat, percent []float32
		for _, w := range writes {
			if w.Parameter != itemParamAccuracy {
				continue
			}
			switch w.Channel {
			case paramkeeper.Flat:
				flat = append(flat, w.Value)
			case paramkeeper.PercentSum:
				percent = append(percent, w.Value)
			}
		}
		if len(flat) != 1 || len(percent) != 1 || flat[0] != float32(row.BuffModifiers.HrFlat) || percent[0] != float32(row.BuffModifiers.HrRate) {
			t.Errorf("%s: hr(%d,%d) filed flat %v, percent %v", row.Codename, row.BuffModifiers.HrFlat, row.BuffModifiers.HrRate, flat, percent)
		}
	}
	for _, name := range []string{hitScrollCodename, chBowCallCodename, "SKILL_EU_BARD_SPEEDUPA_HITRATE_A_01", "SKILL_EU_BARD_SPEEDUPA_HITRATE_B_01"} {
		if !covered[name] {
			t.Errorf("%s does not carry hr", name)
		}
	}
}

/*
================
TestHitScrollStillRaisesHitRateOnce

Regression through the real inventory command: the hit scroll's item program
owns its hr block, so the skill-side hr copy must not install a second one.
================
*/
func TestHitScrollStillRaisesHitRateOnce(t *testing.T) {
	dir := gamedatatest.TextdataDir(t)
	items := enterworld.NewTextdataItems(dir)
	skills := enterworld.NewTextdataSkills(dir)
	for _, identity := range items.ItemCommandReferences() {
		ref, ok := items.ItemRefByID(identity.RefObjID)
		if !ok || ref.AssociatedSkillCodename != hitScrollCodename {
			continue
		}
		c := testCharacter()
		c.MissionInventory = []enterworld.InventoryRow{{Slot: 21, RefObjID: ref.RefObjID, Codename: ref.Codename, TypeFlags: ref.TypeFlags(), StackCount: 2}}
		rt, _ := newTestRuntime(c, items)
		rt.deps.(*enterworld.Deps).Skills = skills
		flags := ref.TypeFlags()
		result := rt.HandleItemUse(testDivision, c, []byte{21, byte(flags), byte(flags >> 8)})
		if len(result.Frames) == 0 || result.Frames[0].Payload[0] != 1 {
			t.Fatalf("hit scroll refused: %+v", result)
		}
		writes := hitRateWrites(rt, c.Name)
		if len(writes[paramkeeper.Flat]) != 1 || len(writes[paramkeeper.PercentSum]) != 1 {
			t.Fatalf("hit scroll hr writes %+v, want one flat and one percent sum", writes)
		}
		return
	}
	t.Fatalf("no item uses %s", hitScrollCodename)
}
