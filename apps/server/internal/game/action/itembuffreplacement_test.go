/*
===========================================================================

itembuffreplacement_test.go - a buff item and a cast buff refuse each other

The 20% damage scroll (cbuf, packed states 4 and 5) and the Chinese fire
attack-up buff (state 4) cannot both be active. Natively both directions
refuse 0x300C: a cast through the skill admission, an item's skill through the
indirect skill start, which runs the same admission with mask 0xFFFF. #bug-reports: "can't use lightning
and fire damage buffs with the 20% damage scroll open".

===========================================================================
*/

package action

import (
	"bytes"
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
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
TestDamageScrollAndAttackUpRefuseEachOther
================
*/
func TestDamageScrollAndAttackUpRefuseEachOther(t *testing.T) {
	refusal := offensiveRefusal(refusedReplacement).Frames[0].Payload

	// The scroll first: the buff's cast is refused, as natively.
	rt, _, c, buff := buffScrollFixture(t)
	useBuffScroll(rt, c)
	if scrollsLeft(c) != 1 || len(rt.effects.Snapshot(testDivision, c.Name)) != 1 {
		t.Fatalf("the scroll did not install: %d left", scrollsLeft(c))
	}
	cast := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: buff.ID}.Encode())
	if len(cast.Frames) == 0 || !bytes.Equal(cast.Frames[0].Payload, refusal) || len(rt.effects.Snapshot(testDivision, c.Name)) != 1 {
		t.Fatalf("the buff was not refused under the scroll: %+v", cast.Frames)
	}

	// The buff first, cast through to its release: the scroll is refused
	// too, and not spent.
	rt, clock, c, buff := buffScrollFixture(t)
	if cast := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: buff.ID}.Encode()); len(cast.Frames) == 0 || cast.Frames[0].Payload[0] != 1 {
		t.Fatalf("the buff alone was refused: %+v", cast.Frames)
	}
	for step := 0; step < 40 && len(rt.effects.Snapshot(testDivision, c.Name)) == 0; step++ {
		clock.Advance(buffReleaseStep)
		rt.TickHook()(clock.NowMs())
	}
	if len(rt.effects.Snapshot(testDivision, c.Name)) != 1 {
		t.Fatal("the buff never released")
	}
	used := useBuffScroll(rt, c)
	if len(used.Frames) == 0 || !bytes.Equal(used.Frames[0].Payload, refusal) {
		t.Fatalf("the scroll was not refused under the buff: %+v", used.Frames)
	}
	if scrollsLeft(c) != 2 || len(rt.effects.Snapshot(testDivision, c.Name)) != 1 {
		t.Fatalf("a refused scroll was spent or installed: %d left, %d effects", scrollsLeft(c), len(rt.effects.Snapshot(testDivision, c.Name)))
	}
}
