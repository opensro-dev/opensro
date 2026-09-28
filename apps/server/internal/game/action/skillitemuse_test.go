/*
===========================================================================

skillitemuse_test.go - consumable stat effects through the inventory authority

Exercise application, private stat publication, duplicate refusal, timed-job
restoration and expiry with the same owners as normal gameplay.

===========================================================================
*/
package action

import (
	"path/filepath"
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/testsupport/licensed"
)

/*
================
namedItemSkills
================
*/
type namedItemSkills struct {
	staticSkillSource
}

/*
================
SkillByCodename
================
*/
func (s namedItemSkills) SkillByCodename(name string) (enterworld.SkillRow, bool) {
	for _, row := range s.staticSkillSource {
		if row.Codename == name {
			return row, true
		}
	}
	return enterworld.SkillRow{}, false
}

/*
================
statItemFixture
================
*/
func statItemFixture(t *testing.T, effect enterworld.SkillTimedEffect) (*Runtime, *fakeClock, *enterworld.Character, []byte) {
	t.Helper()
	c := testCharacter()
	items := testItems()
	ref := &enterworld.ItemRef{RefObjID: 900001, Codename: "TEST_STAT_ITEM", TypeIDs: [4]int64{3, 3, 13, 1},
		Country: 3, AssociatedSkillCodename: "TEST_STAT_SKILL", NativeFields: enterworld.NewNativeFields(map[string]float64{"canUse": 1})}
	items[ref.Codename] = ref
	c.MissionInventory = []enterworld.InventoryRow{{Slot: 21, RefObjID: ref.RefObjID, Codename: ref.Codename, TypeFlags: ref.TypeFlags(), StackCount: 2}}
	rt, clock := newTestRuntime(c, items)
	effect.Pinned, effect.Persistent, effect.ItemProgram = true, true, true
	rt.deps.(*enterworld.Deps).Skills = namedItemSkills{staticSkillSource{
		100: {ID: 100, Codename: ref.AssociatedSkillCodename, Group: 100, Level: 1, EffectDurationMs: 10000, TimedEffect: effect},
	}}
	flags := ref.TypeFlags()
	return rt, clock, c, []byte{21, byte(flags), byte(flags >> 8)}
}

/*
================
TestStatItemUsesSharedLifetimeAndPrivateProjection
================
*/
func TestStatItemUsesSharedLifetimeAndPrivateProjection(t *testing.T) {
	rt, clock, c, request := statItemFixture(t, enterworld.SkillTimedEffect{
		HP: enterworld.SkillFlatRate{Present: true, Flat: 500},
		MP: enterworld.SkillFlatRate{Present: true, Flat: 200},
	})
	before, err := rt.PlayerBaseStats(testDivision, c)
	if err != nil {
		t.Fatal(err)
	}
	result := rt.HandleItemUse(testDivision, c, request)
	if result.Frames[0].Payload[0] != 1 || c.MissionInventory[0].StackCount != 1 {
		t.Fatalf("use failed: %+v", result)
	}
	after, err := rt.PlayerBaseStats(testDivision, c)
	if err != nil || after.MaxHP != before.MaxHP+500 || after.MaxMP != before.MaxMP+200 {
		t.Fatalf("stats before=%+v after=%+v err=%v", before, after, err)
	}
	private := false
	for _, frame := range result.Frames {
		private = private || frame.Opcode == wire.OpBaseStats
	}
	for _, frame := range result.Broadcast {
		if frame.Opcode == wire.OpBaseStats {
			t.Fatal("private stats leaked to peers")
		}
	}
	if !private || len(c.TimedSkillJobs) != 1 {
		t.Fatal("missing stat refresh or durable job")
	}
	if repeated := rt.HandleItemUse(testDivision, c, request); repeated.Frames[0].Payload[0] == 1 || c.MissionInventory[0].StackCount != 1 {
		t.Fatal("duplicate consumed or refreshed effect")
	}
	clock.Advance(3 * time.Second)
	rt.ForgetCharacter(testDivision, c.Name)
	if c.TimedSkillJobs[0].RemainingMs != 7000 {
		t.Fatal("logout lost remaining lifetime", c.TimedSkillJobs)
	}
	clock.Advance(24 * time.Hour)
	rt.RestoreTimedSkillJobs(testDivision, c.Name)
	restored, err := rt.PlayerBaseStats(testDivision, c)
	if err != nil || restored.MaxHP != after.MaxHP || restored.MaxMP != after.MaxMP {
		t.Fatal("restored modifiers changed", restored, err)
	}
	hp := int64(restored.MaxHP)
	c.CurrentHP = &hp
	clock.Advance(7 * time.Second)
	rt.effects.Expire(clock.NowMs())
	rt.drainStoppedCharacterEffects()
	ended, err := rt.PlayerBaseStats(testDivision, c)
	if err != nil || ended.MaxHP != before.MaxHP || ended.MaxMP != before.MaxMP || len(c.TimedSkillJobs) != 0 || *c.CurrentHP != int64(before.MaxHP) {
		t.Fatal("expiry failed to restore ceilings and clamp stored HP", ended, err)
	}
}

/*
================
TestStatItemRefusesTrailingBytesWithoutConsumption
================
*/
func TestStatItemRefusesTrailingBytesWithoutConsumption(t *testing.T) {
	rt, _, c, request := statItemFixture(t, enterworld.SkillTimedEffect{HP: enterworld.SkillFlatRate{Present: true, Flat: 500}})
	result := rt.HandleItemUse(testDivision, c, append(request, 0))
	if result.Frames[0].Payload[0] == 1 || c.MissionInventory[0].StackCount != 2 || len(rt.effects.Snapshot(testDivision, c.Name)) != 0 {
		t.Fatal("malformed request changed inventory or effects")
	}
}

/*
================
TestShippedStatItemPrograms

Walk the catalog through the actual inventory command. Every admitted family
must install parameter contributions, survive reconnect and remove all writes
at expiry. A parser-only test cannot catch a missing runtime producer.
================
*/
func TestShippedStatItemPrograms(t *testing.T) {
	licensed.RequireGameData(t)
	dir := filepath.Join("..", "..", "..", "..", "..", "..", "extracted", "Media_extracted", "server_dep", "silkroad", "textdata")
	items := enterworld.NewTextdataItems(dir)
	skills := enterworld.NewTextdataSkills(dir)
	count := 0
	families := make(map[string]bool)
	for _, identity := range items.ItemCommandReferences() {
		ref, ok := items.ItemRefByID(identity.RefObjID)
		if !ok || ref.TypeIDs[0] != 3 || ref.TypeIDs[1] != 3 || ref.TypeIDs[2] != 13 || ref.TypeIDs[3] < 1 || ref.TypeIDs[3] > 3 {
			continue
		}
		skill, ok := skills.SkillByCodename(ref.AssociatedSkillCodename)
		if !ok || !skill.TimedEffect.ItemProgram {
			continue
		}
		count++
		effect := skill.TimedEffect
		for family, present := range map[string]bool{
			"hp": effect.HP.Present, "mp": effect.MP.Present,
			"accuracy": effect.Accuracy.Present, "evasion": effect.Evasion.Present,
			"str": effect.Strength.Present, "int": effect.Intellect.Present,
			"damage": skill.BuffModifiers.Dru, "absorption": skill.BuffModifiers.Odar,
		} {
			families[family] = families[family] || present
		}
		t.Run(ref.Codename, func(t *testing.T) {
			c := testCharacter()
			c.MissionInventory = []enterworld.InventoryRow{{Slot: 21, RefObjID: ref.RefObjID, Codename: ref.Codename, TypeFlags: ref.TypeFlags(), StackCount: 2}}
			rt, clock := newTestRuntime(c, items)
			rt.deps.(*enterworld.Deps).Skills = skills
			flags := ref.TypeFlags()
			request := []byte{21, byte(flags), byte(flags >> 8)}
			result := rt.HandleItemUse(testDivision, c, request)
			if itemUseRequirements(c, ref) == wire.ErrCodeItemUseLevelRequired {
				if len(result.Frames) != 1 || result.Frames[0].Payload[0] != 2 || result.Frames[0].Payload[1] != wire.ErrCodeItemUseLevelRequired || c.MissionInventory[0].StackCount != 2 {
					t.Fatal("level-gated item bypassed admission", result)
				}
				// The combat fixture owns only level 1. Exercise the producer
				// after independently proving that real inventory admission
				// still refuses the authored higher-level item unchanged.
				rt.deps.Update(c, "test-item-producer", func() bool {
					return rt.useSkillItem(c, skillItemUse{division: testDivision, ref: ref, row: 0,
						request: wire.ItemUseRequest{Slot: 21, TypeWord: flags}, nowMs: clock.NowMs()}, &result)
				})
			}
			if len(result.Frames) == 0 || result.Frames[0].Payload[0] != 1 || c.MissionInventory[0].StackCount != 1 {
				t.Fatalf("authored program rejected: %+v", result)
			}
			writes := rt.effects.ModifierWrites(testDivision, c.Name)
			if len(writes) == 0 || len(c.TimedSkillJobs) != 1 {
				t.Fatal("consumed without modifiers or lifetime")
			}
			before, _, err := rt.playerCombatStats(testDivision, c)
			if err != nil {
				t.Fatal(err)
			}
			rt.ForgetCharacter(testDivision, c.Name)
			clock.Advance(time.Hour)
			rt.RestoreTimedSkillJobs(testDivision, c.Name)
			after, _, err := rt.playerCombatStats(testDivision, c)
			if err != nil {
				t.Fatal(err)
			}
			for _, write := range writes {
				want, _ := before.Param(write.Parameter)
				got, _ := after.Param(write.Parameter)
				if got != want {
					t.Fatalf("restored parameter %d: %v != %v", write.Parameter, got, want)
				}
			}
			clock.Advance(time.Duration(skill.EffectDurationMs) * time.Millisecond)
			rt.effects.Expire(clock.NowMs())
			rt.drainStoppedCharacterEffects()
			if len(rt.effects.ModifierWrites(testDivision, c.Name)) != 0 || len(c.TimedSkillJobs) != 0 {
				t.Fatal("expired item retained modifiers or durable job")
			}
		})
	}
	for family, present := range families {
		if !present {
			t.Errorf("no shipped coverage for %s", family)
		}
	}
	if count == 0 {
		t.Fatal("catalog did not exercise any stat item")
	}
	t.Logf("exercised %d shipped stat consumables", count)
}
