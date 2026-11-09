/*
===========================================================================

chatitemlinks_test.go - the items a chat line may link

Port-only, not native (SRO_CHAT_ITEM_LINKS): the action runtime reads a
linked item from the sender's own bag, worn gear, open warehouse, pet bag
and costume slots, and from nowhere else.

===========================================================================
*/
package action

import (
	"bytes"
	"testing"
	"time"
	"unicode/utf16"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/social/chat"
)

/*
================
wantLinked

The body and label a readable row must link as.
================
*/
func wantLinked(t *testing.T, rt *Runtime, c *enterworld.Character, ref chat.LinkRef, row enterworld.InventoryRow, label string) {
	t.Helper()
	got, ok := rt.LinkedItem(testDivision, c, ref)
	if !ok {
		t.Fatalf("ref %+v unreadable", ref)
	}
	body := enterworld.BuildItemBody(enterworld.InventoryWireItems([]enterworld.InventoryRow{row})[0])
	if !bytes.Equal(got.Body, body) || got.Label != label {
		t.Fatalf("ref %+v = % X %q, want % X %q", ref, got.Body, got.Label, body, label)
	}
}

/*
================
TestLinkedItemInventoryAndWorn
================
*/
func TestLinkedItemInventoryAndWorn(t *testing.T) {
	rt, c, _ := storageFixture(t)
	worn := enterworld.InventoryRow{Slot: 6, RefObjID: 11459, Codename: "ITEM_CH_SWORD_01_A_RARE",
		TypeFlags: wire.PackTypeFlags(3, 1, 6, 2), Plus: 7, Durability: 40, StackCount: 1, VarianceBits: "0"}
	c.MissionInventory = append(c.MissionInventory, worn)
	wantLinked(t, rt, c, chat.LinkRef{Source: chat.LinkSourceInventory, Slot: 6}, worn, "<ITEM_CH_SWORD_01_A_RARE +7>")
	potion := potionRow(t, c)
	wantLinked(t, rt, c, chat.LinkRef{Source: chat.LinkSourceInventory, Slot: uint16(potion.Slot)}, potion, "<ITEM_ETC_HP_POTION_01>")
}

/*
================
TestLinkedItemEmptySlotAndClosedStorage
================
*/
func TestLinkedItemEmptySlotAndClosedStorage(t *testing.T) {
	rt, c, authority := storageFixture(t)
	if _, ok := rt.LinkedItem(testDivision, c, chat.LinkRef{Source: chat.LinkSourceInventory, Slot: 90}); ok {
		t.Fatal("an empty bag slot linked")
	}
	stored := enterworld.InventoryRow{Slot: 2, RefObjID: 3630, Codename: "ITEM_ETC_HP_POTION_01",
		TypeFlags: wire.PackTypeFlags(3, 3, 1, 1), StackCount: 5, VarianceBits: "0"}
	authority.room.Rows = append(authority.room.Rows, stored)
	ref := chat.LinkRef{Source: chat.LinkSourceStorage, Slot: 2}
	if _, ok := rt.LinkedItem(testDivision, c, ref); ok {
		t.Fatal("LinkedItem read the warehouse; only StorageLinks may, outside any door")
	}
	body := enterworld.BuildItemBody(enterworld.InventoryWireItems([]enterworld.InventoryRow{stored})[0])
	read := rt.StorageLinks(testDivision, c, []chat.LinkRef{ref})
	if got := read[ref]; !bytes.Equal(got.Body, body) || got.Label != "<ITEM_ETC_HP_POTION_01>" {
		t.Fatalf("warehouse link % X %q", got.Body, got.Label)
	}
	rt.endStorageInteraction(testDivision, c.Name)
	if read := rt.StorageLinks(testDivision, c, []chat.LinkRef{ref}); len(read) != 0 {
		t.Fatal("a closed warehouse linked")
	}
}

/*
================
TestLinkedItemPetAndAvatar
================
*/
func TestLinkedItemPetAndAvatar(t *testing.T) {
	rt, c, _ := storageFixture(t)
	ref := chat.LinkRef{Source: chat.LinkSourcePet, Slot: 0, Pet: 7001}
	if _, ok := rt.LinkedItem(testDivision, c, ref); ok {
		t.Fatal("a pet item linked without a pet")
	}
	// A grab pet is a persistent companion on its summon item's row,
	// summoned beside the transport (ActiveCOS, another GID): both hold
	// an item at slot 0, and a link reads the pet it names (#3022).
	grabBag := enterworld.InventoryRow{Slot: 0, RefObjID: 3630, Codename: "ITEM_ETC_HP_POTION_01",
		TypeFlags: wire.PackTypeFlags(3, 3, 1, 1), StackCount: 3, VarianceBits: "0"}
	grabPet := &domain.CharacterCOS{GID: 7001, Codename: "COS_P_WOLF_001", Summoned: true, CurrentHP: 10,
		Container: &domain.COSContainer{Capacity: 28, Rows: []domain.InventoryRow{grabBag}}}
	c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: 20, RefObjID: 4903,
		Codename: "ITEM_COS_P_WOLF_SCROLL", Summon: grabPet})
	transportBag := enterworld.InventoryRow{Slot: 0, RefObjID: 11459, Codename: "ITEM_CH_SWORD_01_A_RARE",
		TypeFlags: wire.PackTypeFlags(3, 1, 6, 2), StackCount: 1, VarianceBits: "0"}
	c.ActiveCOS = &domain.CharacterCOS{GID: 7002, Codename: "COS_T_HORSE", Summoned: true, CurrentHP: 10,
		Container: &domain.COSContainer{Capacity: 28, Rows: []domain.InventoryRow{transportBag}}}
	wantLinked(t, rt, c, ref, grabBag, "<ITEM_ETC_HP_POTION_01>")
	wantLinked(t, rt, c, chat.LinkRef{Source: chat.LinkSourcePet, Slot: 0, Pet: 7002}, transportBag, "<ITEM_CH_SWORD_01_A_RARE>")
	// A link naming no pet, an unknown pet, a dead pet or a pet put away
	// reads nothing.
	for _, bad := range []chat.LinkRef{
		{Source: chat.LinkSourcePet, Slot: 0},
		{Source: chat.LinkSourcePet, Slot: 0, Pet: 7999},
	} {
		if _, ok := rt.LinkedItem(testDivision, c, bad); ok {
			t.Fatalf("ref %+v linked", bad)
		}
	}
	grabPet.CurrentHP = 0
	if _, ok := rt.LinkedItem(testDivision, c, ref); ok {
		t.Fatal("a dead pet's item linked")
	}
	grabPet.CurrentHP, grabPet.Summoned = 10, false
	if _, ok := rt.LinkedItem(testDivision, c, ref); ok {
		t.Fatal("a pet put away linked")
	}
	costume := enterworld.InventoryRow{Slot: 1, RefObjID: 11460, Codename: "ITEM_CH_SWORD_02_A_RARE",
		TypeFlags: wire.PackTypeFlags(3, 1, 6, 2), StackCount: 1, VarianceBits: "0"}
	c.AvatarInventory = &domain.AvatarInventory{Rows: []domain.InventoryRow{costume}}
	wantLinked(t, rt, c, chat.LinkRef{Source: chat.LinkSourceAvatar, Slot: 1}, costume, "<ITEM_CH_SWORD_02_A_RARE>")
}

/*
================
TestLinkedItemUnknownSource
================
*/
func TestLinkedItemUnknownSource(t *testing.T) {
	rt, c, _ := storageFixture(t)
	potion := potionRow(t, c)
	if _, ok := rt.LinkedItem(testDivision, c, chat.LinkRef{Source: 4, Slot: uint16(potion.Slot)}); ok {
		t.Fatal("an unknown source linked")
	}
}

//============================================================================

/*
================
storeLinkFixture

The real authority store behind the runtime: a character holding three
Global Chatting items in bag slot 21, a potion in warehouse slot 2, the
warehouse open, and the item-link switch on. The store's own lock is the
one the item-use door holds, so a storage read inside that door hangs.
================
*/
func storeLinkFixture(t *testing.T) (*doorRuntime, *enterworld.ItemRef, enterworld.InventoryRow) {
	t.Helper()
	ref := &enterworld.ItemRef{
		RefObjID: 3851, Codename: "ITEM_MALL_GLOBAL_CHATTING", TypeIDs: [4]int64{3, 3, 3, 5},
		ReqQuadTypes: [4]int64{-1, -1, -1, -1},
		NativeFields: enterworld.NewNativeFields(map[string]float64{"maxStack": 50, "canUse": 1}),
	}
	seed := testCharacter()
	seed.MissionInventory = append(seed.MissionInventory, enterworld.InventoryRow{
		Slot: 21, RefObjID: ref.RefObjID, Codename: ref.Codename, TypeFlags: ref.TypeFlags(), StackCount: 3,
	})
	d := openDoorRuntime(t, t.TempDir(), seed)
	d.rt.deps.ItemReferences().(staticItemSource)[ref.Codename] = ref
	d.rt.ConfigureStorage(d.authority)
	stored := enterworld.InventoryRow{Slot: 2, RefObjID: 3630, Codename: "ITEM_ETC_HP_POTION_01",
		TypeFlags: wire.PackTypeFlags(3, 3, 1, 1), StackCount: 5, VarianceBits: "0"}
	if _, err := d.authority.TransactStorage(d.character, func(_ *domain.Character, storage *domain.AccountStorage) error {
		storage.Rows = append(storage.Rows, stored)
		return nil
	}); err != nil {
		t.Fatalf("seed the warehouse: %v", err)
	}
	d.rt.openStorageSession(testDivision, d.character.Name, 17)
	d.rt.ChatItemLinks = &chat.ItemLinks{Source: d.rt, Limit: 1}
	return d, ref, stored
}

/*
================
storageTail

The broadcast tail a line linking row carries.
================
*/
func storageTail(row enterworld.InventoryRow) []byte {
	body := enterworld.BuildItemBody(enterworld.InventoryWireItems([]enterworld.InventoryRow{row})[0])
	return chat.AppendLinkTail(nil, []chat.LinkedItem{{Body: body}})
}

/*
================
TestGlobalChattingStorageLinkThroughTheRealStore

The Global Chatting use runs inside the store's character door; a
warehouse link must be read before that door, or the read waits on the
lock the door holds and the shard's store hangs.
================
*/
func TestGlobalChattingStorageLinkThroughTheRealStore(t *testing.T) {
	d, ref, stored := storeLinkFixture(t)
	d.rt.PushShardPeerFrames = func(string, string, []wire.Frame) {}
	request := append(globalChatRequest(ref, "wts \uFFFC"),
		chat.EncodeLinkRefs([]chat.LinkRef{{Source: chat.LinkSourceStorage, Slot: 2}})...)
	done := make(chan OpResult, 1)
	go func() { done <- d.rt.HandleItemUse(testDivision, d.character, request) }()
	select {
	case result := <-done:
		for _, frame := range result.Frames {
			if frame.Opcode == chat.OpChatBroadcast {
				if !bytes.HasSuffix(frame.Payload, storageTail(stored)) {
					t.Fatalf("line % X carries no warehouse link", frame.Payload)
				}
				return
			}
		}
		t.Fatalf("no line in %+v", result.Frames)
	case <-time.After(10 * time.Second):
		t.Fatal("the Global Chatting use hung on the store's lock")
	}
}

/*
================
TestChatLaneLinksAStorageItemThroughTheRealStore

The chat lane works on a snapshot of its sender; the store knows only the
live character, so the warehouse is read with the live one.
================
*/
func TestChatLaneLinksAStorageItemThroughTheRealStore(t *testing.T) {
	d, _, stored := storeLinkFixture(t)
	units := utf16.Encode([]rune("wts \uFFFC"))
	w := wire.NewWriter(4 + len(units)*2).U8(chat.ChatTypeAll).U8(0xFF).U16(uint16(len(units)))
	for _, u := range units {
		w.U16(u)
	}
	frame := append(w.Payload(), chat.EncodeLinkRefs([]chat.LinkRef{{Source: chat.LinkSourceStorage, Slot: 2}})...)
	outcome := chat.HandleChat(d.rt.deps, chat.Views{Links: d.rt.ChatItemLinks}, testDivision, d.character, frame)
	if !bytes.HasSuffix(outcome.Broadcast, storageTail(stored)) {
		t.Fatalf("line % X carries no warehouse link (%s)", outcome.Broadcast, outcome.Refusal)
	}
}
