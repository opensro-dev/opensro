/*
===========================================================================

commerce_seed_test.go - the immutable item reference seed

The seed is the bootstrap's reference dictionary: shop contents and the
fortress forge items, compressed at rest and detached on every read.

===========================================================================
*/
package action

import (
	"bytes"
	"encoding/json"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/commerce"
	"opensro.online/server/internal/game/item/inventory"
	"testing"
)

/*
================
TestCompressedCommerceSeedPreservesWireAndDetachedReads
================
*/
func TestCompressedCommerceSeedPreservesWireAndDetachedReads(t *testing.T) {
	rt, _ := merchantFixture(t)
	offers := rt.Commerce.Tabs[1]
	ref := offers[0].Ref
	offers[0].Contents = []commerce.Content{{Ref: ref}}
	rt.prepareCommerceReferences()
	want := rt.commerceReferences([]inventory.Item{{RefObjID: ref.RefObjID, Codename: ref.Codename, TypeFlags: ref.TypeFlags()}}, nil)
	got := rt.CommerceReferenceSeed()
	if len(got) != 1 || got[0].Opcode != want.Opcode || !bytes.Equal(got[0].Payload, want.Payload) {
		t.Fatal("seed wire bytes changed")
	}
	got[0].Payload[0] ^= 255
	if !bytes.Equal(rt.CommerceReferenceSeed()[0].Payload, want.Payload) {
		t.Fatal("caller mutated retained seed")
	}
}

/*
================
TestCommerceSeedNamesTheForgeItems

The production window draws forge items no shop sells; the seed carries a
resolvable forge row's reference, and leaves out one the data lacks.
================
*/
func TestCommerceSeedNamesTheForgeItems(t *testing.T) {
	rt, _ := merchantFixture(t)
	forge := enterworld.DefaultSiegeItemForgeGroups()[0].Items[0].RefObjID
	deps := rt.deps.(*enterworld.Deps)
	deps.Items.(staticItemSource)["ITEM_TEST_FORGE"] = &enterworld.ItemRef{RefObjID: forge, Codename: "ITEM_TEST_FORGE", Name: "Forge Test"}
	rt.prepareCommerceReferences()
	var names []string
	for _, frame := range rt.CommerceReferenceSeed() {
		var body struct {
			Items []struct {
				ID   uint32 `json:"refObjId"`
				Name string `json:"name"`
			} `json:"items"`
		}
		if err := json.Unmarshal(frame.Payload, &body); err != nil {
			t.Fatal(err)
		}
		for _, item := range body.Items {
			if _, ok := fortressForgeRows()[item.ID]; ok {
				names = append(names, item.Name)
			}
		}
	}
	if len(names) != 1 || names[0] != "Forge Test" {
		t.Fatalf("seeded forge references = %v, want the one resolvable row", names)
	}
}

/*
================
TestCommerceReferencesResolvesByRefObjIDBeforeCodename

Storage and inventory rows deduplicate by RefObjID; if a persisted row's
codename is empty or stale while RefObjID is valid, commerceReferences still
resolves the canonical item reference (icon, name, maxStack, nativeFields)
by RefObjID.
================
*/
func TestCommerceReferencesResolvesByRefObjIDBeforeCodename(t *testing.T) {
	rt, _ := merchantFixture(t)
	deps := rt.deps.(*enterworld.Deps)
	potion := deps.Items.(staticItemSource)["ITEM_ETC_HP_POTION_01"]
	potion.Name = "HP Recovery Potion (Small)"
	potion.Icon = "item/etc/hp_potion_01.ddj"
	potion.DescriptionSymbol = "SN_ITEM_ETC_HP_POTION_01_TT_DESC"

	frame := rt.commerceReferences([]inventory.Item{
		{
			RefObjID:  potion.RefObjID,
			Codename:  "",
			TypeFlags: potion.TypeFlags(),
		},
	}, nil)
	var body struct {
		Items []struct {
			ID                uint32                  `json:"refObjId"`
			Name              string                  `json:"name"`
			MaxStack          uint16                  `json:"maxStack"`
			Icon              string                  `json:"icon"`
			DescriptionSymbol string                  `json:"descriptionSymbol"`
			NativeFields      enterworld.NativeFields `json:"nativeFields"`
		} `json:"items"`
	}
	if err := json.Unmarshal(frame.Payload, &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(body.Items))
	}
	got := body.Items[0]
	if got.ID != potion.RefObjID || got.Name != potion.Name || got.Icon != potion.Icon || got.DescriptionSymbol != potion.DescriptionSymbol || got.MaxStack != 50 {
		t.Fatalf("resolved reference = %+v, want id=%d name=%q icon=%q desc=%q maxStack=50", got, potion.RefObjID, potion.Name, potion.Icon, potion.DescriptionSymbol)
	}
}
