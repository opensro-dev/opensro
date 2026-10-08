package inventory

import (
	"reflect"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/item/wire"
)

// Bug C parity fixtures: real TypeID words for every body-armor family
// member. Sockets: chest pieces are TID4 3 -> socket 1, leg pieces TID4 4 ->
// socket 4.

func clothesChest(slot uint8) Item {
	// TID 3.1.1.3: CH clothes garment, chest.
	return Item{Slot: slot, RefObjID: 5001, Codename: "ITEM_CH_M_CLOTHES_01_BA_A_DEF",
		TypeFlags: wire.PackTypeFlags(3, 1, 1, 3), Quantity: 1}
}

func clothesLegs(slot uint8) Item {
	// TID 3.1.1.4: CH clothes garment, legs.
	return Item{Slot: slot, RefObjID: 5002, Codename: "ITEM_CH_M_CLOTHES_01_LA_A_DEF",
		TypeFlags: wire.PackTypeFlags(3, 1, 1, 4), Quantity: 1}
}

func euClothesChest(slot uint8) Item {
	// TID 3.1.9.3: the EU clothes class counts as clothes too.
	return Item{Slot: slot, RefObjID: 5003, Codename: "ITEM_EU_M_CLOTHES_01_BA_A_DEF",
		TypeFlags: wire.PackTypeFlags(3, 1, 9, 3), Quantity: 1}
}

func lightChest(slot uint8) Item {
	// TID 3.1.2.3: CH protector (light) - body armor, NOT clothes.
	return Item{Slot: slot, RefObjID: 5004, Codename: "ITEM_CH_M_LIGHT_01_BA_A_DEF",
		TypeFlags: wire.PackTypeFlags(3, 1, 2, 3), Quantity: 1}
}

func heavyLegs(slot uint8) Item {
	// TID 3.1.3.4: CH armor (heavy) legs - body armor, NOT clothes.
	return Item{Slot: slot, RefObjID: 5005, Codename: "ITEM_CH_M_HEAVY_01_LA_A_DEF",
		TypeFlags: wire.PackTypeFlags(3, 1, 3, 4), Quantity: 1}
}

func TestBodyArmorFamilyPredicates(t *testing.T) {
	cases := []struct {
		name    string
		word    uint16
		family  bool
		clothes bool
	}{
		{"CH clothes chest", wire.PackTypeFlags(3, 1, 1, 3), true, true},
		{"EU clothes chest", wire.PackTypeFlags(3, 1, 9, 3), true, true},
		{"CH light chest", wire.PackTypeFlags(3, 1, 2, 3), true, false},
		{"CH heavy legs", wire.PackTypeFlags(3, 1, 3, 4), true, false},
		{"EU light (tid3 10)", wire.PackTypeFlags(3, 1, 10, 4), true, false},
		{"EU heavy (tid3 11)", wire.PackTypeFlags(3, 1, 11, 4), true, false},
		{"weapon", wire.PackTypeFlags(3, 1, 6, 2), false, false},
		{"shield", wire.PackTypeFlags(3, 1, 4, 1), false, false},
		{"potion (ETC band)", wire.PackTypeFlags(3, 3, 1, 1), false, false},
		{"empty word", 0, false, false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := IsBodyArmorFamilyTypeFlags(testCase.word); got != testCase.family {
				t.Fatalf("IsBodyArmorFamilyTypeFlags(0x%04X) = %v, want %v", testCase.word, got, testCase.family)
			}
			if got := IsClothesGarmentTypeFlags(testCase.word); got != testCase.clothes {
				t.Fatalf("IsClothesGarmentTypeFlags(0x%04X) = %v, want %v", testCase.word, got, testCase.clothes)
			}
		})
	}
}

func TestClothesHardExclusivityConflictScan(t *testing.T) {
	t.Run("clothes may not join worn hard armor", func(t *testing.T) {
		rows := []Item{heavyLegs(4)}
		conflict, found := ClothesHardExclusivityConflict(rows, clothesChest(20).TypeFlags)
		if !found {
			t.Fatal("clothes joining worn hard armor was allowed")
		}
		if conflict.RefObjID != 5005 {
			t.Fatalf("conflict row = %d, want the worn hard legs 5005", conflict.RefObjID)
		}
	})

	t.Run("hard armor may not join worn clothes", func(t *testing.T) {
		if _, found := ClothesHardExclusivityConflict([]Item{clothesChest(1)}, heavyLegs(20).TypeFlags); !found {
			t.Fatal("hard armor joining worn clothes was allowed")
		}
	})

	t.Run("EU clothes count as clothes", func(t *testing.T) {
		if _, found := ClothesHardExclusivityConflict([]Item{euClothesChest(1)}, heavyLegs(20).TypeFlags); !found {
			t.Fatal("hard armor joining worn EU clothes was allowed")
		}
	})

	t.Run("light plus heavy is legal", func(t *testing.T) {
		if _, found := ClothesHardExclusivityConflict([]Item{lightChest(1)}, heavyLegs(20).TypeFlags); found {
			t.Fatal("a light+heavy mix was refused; the exclusivity is clothes vs non-clothes")
		}
	})

	t.Run("a non-armor incoming never scans", func(t *testing.T) {
		if _, found := ClothesHardExclusivityConflict([]Item{clothesChest(1), heavyLegs(4)}, sword(20).TypeFlags); found {
			t.Fatal("a weapon triggered the body-armor scan")
		}
	})

	t.Run("only the body sockets 0..5 scan", func(t *testing.T) {
		// The same hard-armor word parked in a BAG slot must not conflict.
		if _, found := ClothesHardExclusivityConflict([]Item{heavyLegs(20)}, clothesChest(21).TypeFlags); found {
			t.Fatal("a bagged row was counted as worn")
		}
	})
}

// The forward equip gate: bug C through the move path.
func TestTransferRefusesClothesOntoWornHardArmor(t *testing.T) {
	inv := New([]Item{heavyLegs(4), clothesChest(20)}, domain.DefaultInventorySize)
	before := inv.Items()

	_, fault := inv.Transfer(20, SocketBody, 1, 1)
	if fault == nil {
		t.Fatal("equipping clothes over worn hard armor was allowed")
	}
	if fault.Reason != "clothesHardArmorExclusivity" {
		t.Fatalf("reason = %q, want clothesHardArmorExclusivity", fault.Reason)
	}
	if fault.Code != wire.ErrCodeExclusiveArmorMix {
		t.Fatalf("code = 0x%02X, want 0x%02X (01:32 armor-mix notice)",
			fault.Code, wire.ErrCodeExclusiveArmorMix)
	}
	if !reflect.DeepEqual(inv.Items(), before) {
		t.Fatal("a refused equip still mutated the inventory")
	}
}

func TestTransferRefusesHardArmorOntoWornClothes(t *testing.T) {
	inv := New([]Item{clothesChest(1), heavyLegs(20)}, domain.DefaultInventorySize)

	if _, fault := inv.Transfer(20, SocketLeg, 1, 1); fault == nil || fault.Reason != "clothesHardArmorExclusivity" {
		t.Fatalf("hard legs over worn clothes = %v, want clothesHardArmorExclusivity", fault)
	}
}

func TestTransferAllowsLightPlusHeavy(t *testing.T) {
	inv := New([]Item{lightChest(1), heavyLegs(20)}, domain.DefaultInventorySize)

	if _, fault := inv.Transfer(20, SocketLeg, 1, 1); fault != nil {
		t.Fatalf("a legal light+heavy mix was refused: %v", fault)
	}
	if worn, ok := inv.At(SocketLeg); !ok || worn.RefObjID != 5005 {
		t.Fatal("the heavy legs did not seat in the leg socket")
	}
}

func TestTransferAllowsNonArmorWhileClothesWorn(t *testing.T) {
	inv := New([]Item{clothesChest(1), sword(20)}, domain.DefaultInventorySize)

	if _, fault := inv.Transfer(20, SocketWeapon, 1, 1); fault != nil {
		t.Fatalf("equipping a weapon while wearing clothes was refused: %v", fault)
	}
}

// The swap-back gate: moving OUT of a socket onto an occupied slot seats the
// occupant, so the occupant faces the same gates.
func TestTransferSwapBackFacesTheExclusivityGate(t *testing.T) {
	// Wearing hard legs; the bag slot holds clothes LEGS (same socket
	// class). The swap would seat the clothes legs while the vacating hard
	// piece still counts as worn - native walks all six sockets with no
	// departing-socket exclusion, so this refuses: switching families
	// requires unequipping first.
	inv := New([]Item{heavyLegs(4), clothesLegs(20)}, domain.DefaultInventorySize)
	before := inv.Items()

	_, fault := inv.Transfer(4, 20, 1, 1)
	if fault == nil {
		t.Fatal("the swap-back seated clothes into a hard-armor loadout")
	}
	if fault.Reason != "clothesHardArmorExclusivity" {
		t.Fatalf("reason = %q, want clothesHardArmorExclusivity", fault.Reason)
	}
	if !reflect.DeepEqual(inv.Items(), before) {
		t.Fatal("a refused swap-back still mutated the inventory")
	}
}

func TestTransferSwapBackValidatesTheSocketClass(t *testing.T) {
	// The measured back door: unequipping the weapon onto a bagged chest
	// piece must not put the chest piece in the weapon socket.
	inv := New([]Item{sword(SocketWeapon), clothesChest(20)}, domain.DefaultInventorySize)
	if _, fault := inv.Transfer(SocketWeapon, 20, 1, 1); fault == nil || fault.Reason != "wrongSocket" {
		t.Fatalf("unequip onto a chest piece = %v, want wrongSocket", fault)
	}

	// And a non-equipable occupant refuses outright.
	inv = New([]Item{sword(SocketWeapon), potion(21)}, domain.DefaultInventorySize)
	if _, fault := inv.Transfer(SocketWeapon, 21, 1, 1); fault == nil || fault.Reason != "notEquipable" {
		t.Fatalf("unequip onto a potion = %v, want notEquipable", fault)
	}
}

func TestTransferUnequipToEmptyBagSlotIsFree(t *testing.T) {
	// Moving out of a socket into an EMPTY slot seats nothing: no gates.
	inv := New([]Item{heavyLegs(4), clothesChest(1)}, domain.DefaultInventorySize)

	if _, fault := inv.Transfer(4, 20, 1, 1); fault != nil {
		t.Fatalf("unequipping into an empty slot was refused: %v", fault)
	}
	if _, ok := inv.At(4); ok {
		t.Fatal("the leg socket is still occupied")
	}
}

func TestTransferSwapBackAllowsALegalOccupant(t *testing.T) {
	// Weapon-for-weapon swap through the bag: both directions legal.
	spare := Item{Slot: 20, RefObjID: 11460, Codename: "ITEM_CH_SWORD_02_A_RARE",
		TypeFlags: wire.PackTypeFlags(3, 1, 6, 2), Quantity: 1}
	inv := New([]Item{sword(SocketWeapon), spare}, domain.DefaultInventorySize)

	applied, fault := inv.Transfer(SocketWeapon, 20, 1, 1)
	if fault != nil {
		t.Fatalf("a weapon-for-weapon swap was refused: %v", fault)
	}
	if applied.Leg != LegSwap {
		t.Fatalf("leg = %q, want swap", applied.Leg)
	}
	if worn, _ := inv.At(SocketWeapon); worn.RefObjID != 11460 {
		t.Fatalf("the weapon socket holds %d, want the spare 11460", worn.RefObjID)
	}
}

// The TypeFlags socket table: the routes the codename heuristic could not
// express.
func TestEquipSocketForTypeFlagsRoutes(t *testing.T) {
	cases := []struct {
		name   string
		word   uint16
		socket uint8
		ok     bool
	}{
		{"job suit -> special dress", wire.PackTypeFlags(3, 1, 7, 1), SocketSpecialDress, true},
		{"weapon", wire.PackTypeFlags(3, 1, 6, 2), SocketWeapon, true},
		{"CH shield", wire.PackTypeFlags(3, 1, 4, 1), SocketShield, true},
		{"EU shield", wire.PackTypeFlags(3, 1, 4, 2), SocketShield, true},
		{"arrow ammo -> the shield socket", wire.PackTypeFlags(3, 3, 4, 1), SocketShield, true},
		{"head", wire.PackTypeFlags(3, 1, 2, 1), SocketHead, true},
		{"shoulder", wire.PackTypeFlags(3, 1, 2, 2), SocketShoulder, true},
		{"chest", wire.PackTypeFlags(3, 1, 2, 3), SocketBody, true},
		{"legs", wire.PackTypeFlags(3, 1, 2, 4), SocketLeg, true},
		{"arms", wire.PackTypeFlags(3, 1, 2, 5), SocketArm, true},
		{"feet", wire.PackTypeFlags(3, 1, 2, 6), SocketFoot, true},
		{"earring", wire.PackTypeFlags(3, 1, 5, 1), SocketEarring, true},
		{"necklace", wire.PackTypeFlags(3, 1, 5, 2), SocketNecklace, true},
		{"ring", wire.PackTypeFlags(3, 1, 5, 3), SocketRing, true},
		{"EU accessory (tid3 12)", wire.PackTypeFlags(3, 1, 12, 2), SocketNecklace, true},
		{"potion has no socket", wire.PackTypeFlags(3, 3, 1, 1), 0, false},
		{"armor tid4 out of table", wire.PackTypeFlags(3, 1, 2, 7), 0, false},
		{"zero word", 0, 0, false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			socket, ok := EquipSocketForTypeFlags(testCase.word)
			if ok != testCase.ok || (ok && socket != testCase.socket) {
				t.Fatalf("EquipSocketForTypeFlags(0x%04X) = (%d, %v), want (%d, %v)",
					testCase.word, socket, ok, testCase.socket, testCase.ok)
			}
		})
	}
}
