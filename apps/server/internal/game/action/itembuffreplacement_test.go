/*
===========================================================================

itembuffreplacement_test.go - the 20% damage scroll and the attack-up buffs

The native buff admission refuses a buff whose casting states an active
effect holds. With the later retail states (enterworld/skillstates.go,
#4382) the 20% damage scroll holds state 94 alone, so it stacks with the
Chinese fire (4) and lightning (5) attack-up buffs in either order, while
the quest damage-up buffs (94, 5, 4) still refuse both. #bug-reports:
"can't use lightning and fire damage buffs with the 20% damage scroll open".

===========================================================================
*/

package action

import (
	"bytes"
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/statuseffect"
	"opensro.online/server/internal/game/item/wire"
)

const (
	// buffScrollSlot is the scroll's bag slot.
	buffScrollSlot = 21
	// refusedReplacement is the native admission's refusal.
	refusedReplacement = 0x300c
	// buffReleaseStep is how far each tick moves while the buff casts.
	buffReleaseStep = 100 * time.Millisecond
)

/*
================
buffScrollFixture

A level 90 fire user holding the shipped attack-up buff and two shipped
20% damage scrolls.
================
*/
func buffScrollFixture(t *testing.T) (*Runtime, *fakeClock, *enterworld.Character, enterworld.SkillRow) {
	t.Helper()
	rt, clock, c, _ := newCombatTestRuntime(t, 10_000_000)
	buff := shippedOffense(t, "SKILL_CH_FIRE_GONGUP_A_01")
	scroll := shippedOffense(t, "SKILL_MALL_DAMAGE_INC_20P_SCROLL_01")
	rt.deps.(*enterworld.Deps).Skills = namedItemSkills{staticSkillSource{buff.ID: buff, scroll.ID: scroll}}
	level := int64(90)
	c.Level, c.MaxLevel = &level, &level
	c.Masteries = []enterworld.CharacterMastery{{ID: 275, Level: 90}}
	c.Skills = append(c.Skills, buff.ID)
	c.CurrentMP = testInt64(5000)
	items := rt.deps.ItemReferences().(staticItemSource)
	ref := &enterworld.ItemRef{RefObjID: 900002, Codename: "ITEM_MALL_DAMAGE_INC_20P_SCROLL", TypeIDs: [4]int64{3, 3, 13, 1},
		Country: 3, AssociatedSkillCodename: scroll.Codename, NativeFields: enterworld.NewNativeFields(map[string]float64{"canUse": 1})}
	items[ref.Codename] = ref
	c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: buffScrollSlot, RefObjID: ref.RefObjID,
		Codename: ref.Codename, TypeFlags: ref.TypeFlags(), StackCount: 2})
	return rt, clock, c, buff
}

/*
================
useBuffScroll
================
*/
func useBuffScroll(rt *Runtime, c *enterworld.Character) OpResult {
	var word uint16
	for _, row := range c.MissionInventory {
		if row.Slot == buffScrollSlot {
			word = row.TypeFlags
		}
	}
	return rt.HandleItemUse(testDivision, c, []byte{buffScrollSlot, byte(word), byte(word >> 8)})
}

/*
================
scrollsLeft
================
*/
func scrollsLeft(c *enterworld.Character) int64 {
	for _, row := range c.MissionInventory {
		if row.Slot == buffScrollSlot {
			return row.StackCount
		}
	}
	return 0
}

/*
================
TestDamageScrollAndAttackUpStack

Both orders: the scroll under the buff and the buff under the scroll each
install, and the scroll is spent once.
================
*/
func TestDamageScrollAndAttackUpStack(t *testing.T) {
	// The scroll first, then the buff's cast.
	rt, clock, c, buff := buffScrollFixture(t)
	useBuffScroll(rt, c)
	if scrollsLeft(c) != 1 || len(rt.effects.Snapshot(testDivision, c.Name)) != 1 {
		t.Fatalf("the scroll did not install: %d left", scrollsLeft(c))
	}
	if cast := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: buff.ID}.Encode()); len(cast.Frames) == 0 || cast.Frames[0].Payload[0] != 1 {
		t.Fatalf("the buff was refused under the scroll: %+v", cast.Frames)
	}
	releaseBuff(t, rt, clock, c, 2)

	// The buff first, cast through to its release, then the scroll.
	rt, clock, c, buff = buffScrollFixture(t)
	if cast := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: buff.ID}.Encode()); len(cast.Frames) == 0 || cast.Frames[0].Payload[0] != 1 {
		t.Fatalf("the buff alone was refused: %+v", cast.Frames)
	}
	releaseBuff(t, rt, clock, c, 1)
	used := useBuffScroll(rt, c)
	if refusal := offensiveRefusal(refusedReplacement).Frames[0].Payload; len(used.Frames) != 0 && bytes.Equal(used.Frames[0].Payload, refusal) {
		t.Fatalf("the scroll was refused under the buff: %+v", used.Frames)
	}
	if scrollsLeft(c) != 1 || len(rt.effects.Snapshot(testDivision, c.Name)) != 2 {
		t.Fatalf("the scroll did not stack: %d left, %d effects", scrollsLeft(c), len(rt.effects.Snapshot(testDivision, c.Name)))
	}
}

/*
================
releaseBuff

Ticks until the caster holds want effects: the buff's cast has released.
================
*/
func releaseBuff(t *testing.T, rt *Runtime, clock *fakeClock, c *enterworld.Character, want int) {
	t.Helper()
	for step := 0; step < 40 && len(rt.effects.Snapshot(testDivision, c.Name)) < want; step++ {
		clock.Advance(buffReleaseStep)
		rt.TickHook()(clock.NowMs())
	}
	if got := len(rt.effects.Snapshot(testDivision, c.Name)); got != want {
		t.Fatalf("the buff never released: %d effects, want %d", got, want)
	}
}

/*
================
TestDamageFamilyKeepsTheLaterRetailConflicts

The shipped rows, through the native conflict check: the scroll is free of
the attack-up buffs, and a quest damage-up buff refuses the scroll and both
attack-up buffs.
================
*/
func TestDamageFamilyKeepsTheLaterRetailConflicts(t *testing.T) {
	states := func(code string) uint32 { return shippedOffense(t, code).Replacement.PackedStates }
	scroll, quest := states("SKILL_MALL_DAMAGE_INC_20P_SCROLL_01"), states("SKILL_ETC_GNGWC_DAMAGE_UP_20_01")
	for _, code := range []string{"SKILL_CH_FIRE_GONGUP_A_01", "SKILL_CH_LIGHTNING_GWANTONG_A_01"} {
		var held statuseffect.CastingConflictSnapshot
		held.UpdateActive(states(code), false)
		if held.Conflicts(scroll) {
			t.Fatalf("the scroll (%#x) conflicts with %s", scroll, code)
		}
		if !held.Conflicts(quest) {
			t.Fatalf("the quest damage-up buff (%#x) stacks with %s", quest, code)
		}
	}
	var held statuseffect.CastingConflictSnapshot
	held.UpdateActive(scroll, false)
	if !held.Conflicts(quest) {
		t.Fatalf("the quest damage-up buff (%#x) stacks with the scroll", quest)
	}
}
