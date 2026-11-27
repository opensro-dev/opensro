/*
===========================================================================

arenascroll_test.go - return scrolls refused inside a Battle Arena

The reference server refuses both return scrolls with 0x187A when the
reader stands in a Battle Arena world, so a cast cannot take a player out
of a running match. The reverse scroll checks its current world's type
first, so the arena refusal only answers there for a type-0 arena world.

===========================================================================
*/
package action

import (
	"bytes"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/styria"
)

/*
================
standInArena

Registers the arena world (it joins the catalogue only while its switch
is on) and places c inside layer 1 of it.
================
*/
func standInArena(t *testing.T, c *enterworld.Character) {
	t.Helper()
	if err := instance.RegisterPortOnly(instance.StyriaDefinitions()); err != nil {
		t.Fatal(err)
	}
	if c.World == nil {
		c.World = &domain.CharacterWorld{}
	}
	packed := uint32(instance.Pack(instance.DefinitionID(styria.WorldID), 1))
	c.World.PackedInstance = &packed
}

/*
================
TestReturnScrollInBattleArenaWorldIsRefused

The ordinary return scroll refuses 0x187A in the arena and neither
consumes the scroll nor starts the cast.
================
*/
func TestReturnScrollInBattleArenaWorldIsRefused(t *testing.T) {
	rt, c, _, _ := returnFixture(t, 5000)
	standInArena(t, c)
	out := useReturn(rt, c)
	if len(out.Frames) != 1 || !bytes.Equal(out.Frames[0].Payload, wire.EncodeItemUseError(errCodeScrollArena)) {
		t.Fatalf("arena return answered %+v, want the 0x7a refusal", out.Frames)
	}
	if c.MissionInventory[len(c.MissionInventory)-1].StackCount != 4 || c.NativeTeleportMode != 0 {
		t.Fatal("the refused return consumed the scroll or started the cast")
	}
}

/*
================
TestReturnScrollInBattleArenaWorldAnswersBeforeTransport

The reference server tests the arena ahead of the destination string,
so a reader with a summoned transport in the arena hears 0x187A, not
the RESURRECT branch's transport refusal 0x185E.
================
*/
func TestReturnScrollInBattleArenaWorldAnswersBeforeTransport(t *testing.T) {
	rt, c, _, _ := returnFixture(t, 5000)
	summonTestTransport(rt, c)
	standInArena(t, c)
	out := useReturn(rt, c)
	if len(out.Frames) != 1 || !bytes.Equal(out.Frames[0].Payload, wire.EncodeItemUseError(errCodeScrollArena)) {
		t.Fatalf("arena return with a transport answered %+v, want the 0x7a refusal", out.Frames)
	}
}

/*
================
TestThiefDenScrollInBattleArenaWorldAnswersArenaFirst

The den scroll's arena refusal precedes its own thief-only refusal: an
undressed reader in the arena hears 0x187A, not 0x186A.
================
*/
func TestThiefDenScrollInBattleArenaWorldAnswersArenaFirst(t *testing.T) {
	c := testCharacter()
	scroll := &enterworld.ItemRef{RefObjID: 3790, Codename: "ITEM_ETC_SCROLL_RETURN_THIEFDEN_01", Country: 3,
		TypeIDs: [4]int64{3, 3, 3, 1}, ReqQuadTypes: [4]int64{-1, -1, -1, -1}, ReturnDestination: "THIEFDEN",
		ReturnTeleport: "STORE_TD_GATE", NativeFields: enterworld.NewNativeFields(map[string]float64{
			"canUse": 1, "maxStack": 10, "itemParam1_29c": 300000, "itemParam2_2a0": 0})}
	items := testItems()
	items[scroll.Codename] = scroll
	c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: 26, RefObjID: scroll.RefObjID,
		Codename: scroll.Codename, TypeFlags: scroll.TypeFlags(), StackCount: 1})
	rt, _ := newTestRuntime(c, items)
	standInArena(t, c)
	assertItemUseRefusedUnchanged(t, rt, c, wire.NewWriter(3).U8(26).U16(scroll.TypeFlags()).Payload(), errCodeScrollArena)
}

/*
================
TestReverseScrollInBattleArenaWorldOrder

The reverse scroll checks the current world's type before the arena:
the shipped arena registration carries type 1, so 0x18F5 answers there;
a type-0 arena row answers 0x187A.
================
*/
func TestReverseScrollInBattleArenaWorldOrder(t *testing.T) {
	if err := instance.RegisterPortOnly([]instance.Definition{
		{ID: 980, CodeName: "INS_ARENA_JOB_WAR", NativeType: 0, LayerLimit: 1},
	}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		current instance.DefinitionID
		code    uint8
	}{
		{"type-1 arena answers the instance refusal", instance.DefinitionID(styria.WorldID), errCodeReverseWorld},
		{"type-0 arena answers the arena refusal", 980, errCodeScrollArena},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rt, c, _ := reverseReturnFixture(t)
			scroll := len(c.MissionInventory) - 1
			packed := uint32(instance.Pack(tc.current, 1))
			c.World.PackedInstance = &packed
			point := &domain.WorldPoint{World: 1}
			c.World.LastRecallPoint, c.World.LastDeathPoint = point, point
			out := useReverseScroll(rt, c, reverseReturnLastRecall)
			if len(out.Frames) != 1 || !bytes.Equal(out.Frames[0].Payload, wire.EncodeItemUseError(tc.code)) {
				t.Fatalf("reverse in world %d answered %+v, want refusal %#x", tc.current, out.Frames, tc.code)
			}
			if c.MissionInventory[scroll].StackCount != 2 || c.NativeTeleportMode != 0 {
				t.Fatal("the refused reverse return consumed the scroll or started the cast")
			}
		})
	}
}

/*
================
TestLimitedUseReturnsInBattleArenaWorldAreRefused

A limited use runs the same return handlers without a bag row, so the
arena refuses both its return and its reverse return with 0x187A and
starts no cast.
================
*/
func TestLimitedUseReturnsInBattleArenaWorldAreRefused(t *testing.T) {
	rt, c, _, ref := returnFixture(t, 5000)
	standInArena(t, c)
	reverse := &enterworld.ItemRef{RefObjID: 3800, Codename: "ITEM_MALL_REVERSE_RETURN_SCROLL", Country: 3,
		TypeIDs: [4]int64{3, 3, 3, 3}, ReturnDestination: "RESURRECT",
		NativeFields: enterworld.NewNativeFields(map[string]float64{"canUse": 1, "itemParam1_29c": 5000, "itemParam2_2a0": 1})}
	for _, tc := range []struct {
		name string
		ref  *enterworld.ItemRef
	}{{"return", ref}, {"reverse return", reverse}} {
		var result OpResult
		var revived *revivedWhereDead
		if rt.runLimitedUse(testDivision, c, tc.ref, reverseReturnLastRecall, rt.Now().UnixMilli(), &result, &revived) {
			t.Fatalf("limited %s in the arena started", tc.name)
		}
		if len(result.Frames) != 1 || !bytes.Equal(result.Frames[0].Payload, wire.EncodeItemUseError(errCodeScrollArena)) {
			t.Fatalf("limited %s answered %+v, want the 0x7a refusal", tc.name, result.Frames)
		}
		if c.NativeTeleportMode != 0 {
			t.Fatalf("limited %s started the cast", tc.name)
		}
	}
}
