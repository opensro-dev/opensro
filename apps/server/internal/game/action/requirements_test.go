package action

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

// The character-vs-itemdata equip gates (client 789C60 full-mask bits
// mirrored server-side), in native evaluation order: razed 0x004 -> STR
// 0x008 -> INT 0x010 -> level 0x020 (the type-1 quad walk) -> mastery
// 0x100 (the type>0x0a quad walk) -> gender 0x040 -> exclusivity 0x080 ->
// country 0x200 -> fortress role 0x400, each read off the typed ItemRef
// requirement columns and answered with its own 0xB06D notice byte.

// requirementTestItems: a CH sword with a level floor, a woman-only CH
// garment, an EU-only garment, an unrestricted potion-free sword, and a
// stat-gated blade (custom data; the shipped itemdata carries 0 there).
// charLevelQuad is the CH-shaped requirement quad: type 1 (character
// level) in slot 1, the rest empty - the shipped shape of every CH equip
// row.
func charLevelQuad(level int64) ([4]int64, [4]int64) {
	return [4]int64{1, -1, -1, -1}, [4]int64{level}
}

func requirementTestItems() staticItemSource {
	swordTypes, swordValues := charLevelQuad(16)
	trivialTypes, trivialValues := charLevelQuad(1)
	strTypes, strValues := charLevelQuad(99)
	ringTypes, ringValues := charLevelQuad(16)
	items := staticItemSource{
		"ITEM_CH_SWORD_03_A": {
			RefObjID: 3417, Codename: "ITEM_CH_SWORD_03_A", TypeIDs: [4]int64{3, 1, 6, 2},
			Country: 0, ReqQuadTypes: swordTypes, ReqQuadValues: swordValues, RequiredSex: 2,
		},
		"ITEM_CH_W_CLOTHES_01_BA_A": {
			RefObjID: 5010, Codename: "ITEM_CH_W_CLOTHES_01_BA_A", TypeIDs: [4]int64{3, 1, 1, 3},
			Country: 0, ReqQuadTypes: trivialTypes, ReqQuadValues: trivialValues, RequiredSex: 0,
		},
		"ITEM_EU_M_CLOTHES_01_BA_A": {
			RefObjID: 12722, Codename: "ITEM_EU_M_CLOTHES_01_BA_A", TypeIDs: [4]int64{3, 1, 9, 3},
			Country: 1, ReqQuadTypes: trivialTypes, ReqQuadValues: trivialValues, RequiredSex: 1,
		},
		"ITEM_CH_SWORD_01_A": {
			RefObjID: 3632, Codename: "ITEM_CH_SWORD_01_A", TypeIDs: [4]int64{3, 1, 6, 2},
			Country: 0, ReqQuadTypes: trivialTypes, ReqQuadValues: trivialValues, RequiredSex: 2,
		},
		"ITEM_CH_M_HEAVY_01_BA_A": {
			RefObjID: 5005, Codename: "ITEM_CH_M_HEAVY_01_BA_A", TypeIDs: [4]int64{3, 1, 3, 3},
			Country: 0, ReqQuadTypes: trivialTypes, ReqQuadValues: trivialValues, RequiredSex: 2,
		},
		// Level 99 alongside RequiredStr: the refusal must still be the STR
		// byte, pinning the native bit order 0x008 before 0x020.
		"ITEM_CUSTOM_STR_BLADE": {
			RefObjID: 9999, Codename: "ITEM_CUSTOM_STR_BLADE", TypeIDs: [4]int64{3, 1, 6, 2},
			Country: 0, ReqQuadTypes: strTypes, ReqQuadValues: strValues, RequiredSex: 2, RequiredStr: 120,
		},
		"ITEM_CUSTOM_INT_ORB": {
			RefObjID: 9998, Codename: "ITEM_CUSTOM_INT_ORB", TypeIDs: [4]int64{3, 1, 6, 2},
			Country: 0, ReqQuadTypes: trivialTypes, ReqQuadValues: trivialValues, RequiredSex: 2, RequiredInt: 130,
		},
		// TID 3.1.5.3 accessory: a ring, level-gated at 16.
		"ITEM_CH_RING_03_A": {
			RefObjID: 3480, Codename: "ITEM_CH_RING_03_A", TypeIDs: [4]int64{3, 1, 5, 3},
			Country: 0, ReqQuadTypes: ringTypes, ReqQuadValues: ringValues, RequiredSex: 2,
		},
		// Country 3 = the bit-0x200 wildcard (every region equips it); sex 2
		// = unisex. The gates must pass this for any character.
		"ITEM_ANY_REGION_RING": {
			RefObjID: 9997, Codename: "ITEM_ANY_REGION_RING", TypeIDs: [4]int64{3, 1, 5, 3},
			Country: 3, ReqQuadTypes: trivialTypes, ReqQuadValues: trivialValues, RequiredSex: 2,
		},
		// A durability-bearing sword (itemdata Dur_U > 0, like every shipped
		// weapon/armor row): the razed gate (bit 0x004) fires on it when the
		// INSTANCE durability reads 0. RequiredStr pins the native bit order
		// (0x004 razed before 0x008 STR).
		"ITEM_CH_SWORD_05_A": {
			RefObjID: 9996, Codename: "ITEM_CH_SWORD_05_A", TypeIDs: [4]int64{3, 1, 6, 2},
			Country: 0, ReqQuadTypes: trivialTypes, ReqQuadValues: trivialValues, RequiredSex: 2, RequiredStr: 120,
			MaxDurability: 87,
		},
		// Shipped EU heavy armor tier 5 (ITEM_EU_M_HEAVY_05_BA_A): type
		// 0x201 (513, Warrior mastery) with value 35 - the native bit 0x100
		// mastery gate, NOT a character-level requirement (type != 1 means
		// the level leg skips it). EU (Country 1), male (RequiredSex 1).
		"ITEM_EU_M_HEAVY_05_BA_A": {
			RefObjID: 13100, Codename: "ITEM_EU_M_HEAVY_05_BA_A", TypeIDs: [4]int64{3, 1, 11, 3},
			Country: 1, ReqQuadTypes: [4]int64{513, -1, -1, -1}, ReqQuadValues: [4]int64{35}, RequiredSex: 1,
		},
		// Shipped EU caster clothes shape: FOUR mastery types (Wizard 514,
		// Warlock 516, Bard 517, Cleric 518) sharing one value - the native
		// ANY-of walk needs only ONE of them at 60.
		"ITEM_EU_M_CLOTHES_07_BA_A": {
			RefObjID: 13200, Codename: "ITEM_EU_M_CLOTHES_07_BA_A", TypeIDs: [4]int64{3, 1, 9, 3},
			Country: 1, ReqQuadTypes: [4]int64{514, 516, 517, 518}, ReqQuadValues: [4]int64{60, 60, 60, 60}, RequiredSex: 1,
		},
		// The fortress siege hammer shape (TID 3.1.6.16, the client's 5504C0
		// family): role mask -1 = any fortress-guild role, like all 14
		// shipped rows.
		"ITEM_FORT_FORTRESS_HAMMER_06": {
			RefObjID: 19227, Codename: "ITEM_FORT_FORTRESS_HAMMER_06", TypeIDs: [4]int64{3, 1, 6, 16},
			Country: 3, ReqQuadTypes: trivialTypes, ReqQuadValues: trivialValues, RequiredSex: 2,
			FortressRoleMask: -1,
		},
	}
	for _, ref := range items {
		ref.Combat = &enterworld.ItemCombatRef{}
	}
	return items
}

func requirementTestCharacter(level int64, rows []enterworld.InventoryRow) *enterworld.Character {
	gold := int64(5000)
	gender := enterworld.GenderMale
	base := int64(enterworld.BaseStat)
	return &enterworld.Character{
		ID:               7,
		Name:             "reqTester",
		ModelCodename:    "CHAR_CH_MAN_ADVENTURER",
		Gender:           &gender,
		Level:            &level,
		MaxLevel:         &level,
		Strength:         &base,
		Intellect:        &base,
		Gold:             &gold,
		MissionInventory: rows,
	}
}

func bagRow(slot int64, source staticItemSource, codename string) enterworld.InventoryRow {
	ref := source[codename]
	return enterworld.InventoryRow{
		Slot: slot, RefObjID: ref.RefObjID, Codename: codename,
		TypeFlags: ref.TypeFlags(), VarianceBits: "0", StackCount: 1,
	}
}

func moveRefusalCode(t *testing.T, rt *Runtime, character *enterworld.Character, source, dest uint8) (uint8, bool) {
	t.Helper()
	result := rt.HandleItemMove(testDivision, character, encodeMove(t, wire.ItemMoveRequest{
		MovementType: wire.MoveTypeInventory,
		SourceSlot:   source,
		DestSlot:     dest,
		Quantity:     1,
	}))
	payload := result.Frames[0].Payload
	if payload[0] != 0x02 {
		return 0, false
	}
	return payload[1], true
}

func TestEquipRefusesBelowLevelRequirement(t *testing.T) {
	items := requirementTestItems()
	character := requirementTestCharacter(5, []enterworld.InventoryRow{
		bagRow(20, items, "ITEM_CH_SWORD_03_A"),
	})
	rt, _ := newTestRuntime(character, items)

	code, refused := moveRefusalCode(t, rt, character, 20, 6)
	if !refused || code != wire.ErrCodeLevelRequired {
		t.Fatalf("level-5 equip of a level-16 sword = refused=%v code=0x%02X, want [02 10]", refused, code)
	}
}

func TestEquipAllowsAtLevelRequirement(t *testing.T) {
	items := requirementTestItems()
	character := requirementTestCharacter(16, []enterworld.InventoryRow{
		bagRow(20, items, "ITEM_CH_SWORD_03_A"),
	})
	rt, _ := newTestRuntime(character, items)

	if code, refused := moveRefusalCode(t, rt, character, 20, 6); refused {
		t.Fatalf("level-16 equip of a level-16 sword refused with 0x%02X", code)
	}
}

// euCharacter builds an EU male character (the CHAR_EU_ codename prefix
// IS the country source) carrying the given mastery rows and one bag row.
func euCharacter(level int64, masteries []enterworld.CharacterMastery, row enterworld.InventoryRow) *enterworld.Character {
	gender := enterworld.GenderMale
	gold := int64(5000)
	base := int64(enterworld.BaseStat)
	return &enterworld.Character{
		ID: 8, Name: "euTester", ModelCodename: "CHAR_EU_MAN_ADVENTURER",
		Gender: &gender, Level: &level, MaxLevel: &level, Gold: &gold,
		Strength: &base, Intellect: &base,
		Masteries:        masteries,
		MissionInventory: []enterworld.InventoryRow{row},
	}
}

// withMastery returns rows with one mastery set to level (add or update).
func withMastery(rows []enterworld.CharacterMastery, id uint32, level int64) []enterworld.CharacterMastery {
	out := append([]enterworld.CharacterMastery{}, rows...)
	for i := range out {
		if out[i].ID == id {
			out[i].Level = level
			return out
		}
	}
	return append(out, enterworld.CharacterMastery{ID: id, Level: level})
}

// The EU tier gate is the native bit 0x100 MASTERY walk (type 0x201 =
// Warrior masterydata id 513), NOT a character-level gate: the level leg
// only tests quad slots whose type dword == 1, which EU armor never
// carries. Until the mastery plane existed the server approximated this
// with an unconditional level gate; this is the full-parity replacement.
func TestEquipEnforcesEuMasteryTier(t *testing.T) {
	items := requirementTestItems()
	heavyRow := func() enterworld.InventoryRow { return bagRow(20, items, "ITEM_EU_M_HEAVY_05_BA_A") }
	base := enterworld.DefaultMasteries(enterworld.RaceKeyEurope) // the creation seed, all level 1

	t.Run("Warrior below tier refuses with the equip guide", func(t *testing.T) {
		character := euCharacter(60, withMastery(base, 513, 20), heavyRow())
		rt, _ := newTestRuntime(character, items)
		code, refused := moveRefusalCode(t, rt, character, 20, 1)
		if !refused || code != wire.ErrCodeCantEquip {
			t.Fatalf("Warrior-20 equip of a Warrior-35 tier = refused=%v code=0x%02X, want [02 0E]", refused, code)
		}
	})

	t.Run("Warrior at tier equips even at character level 1", func(t *testing.T) {
		// The strongest native-semantics witness: mastery is the ONLY gate
		// on EU armor - the level leg skips type-513 slots entirely.
		character := euCharacter(1, withMastery(base, 513, 35), heavyRow())
		rt, _ := newTestRuntime(character, items)
		if code, refused := moveRefusalCode(t, rt, character, 20, 1); refused {
			t.Fatalf("Warrior-35 level-1 equip of the Warrior-35 tier refused with 0x%02X", code)
		}
	})

	t.Run("missing mastery records are the native miss-continue", func(t *testing.T) {
		// A record-less character passes the bit vacuously (the asm only
		// writes the flag when the record EXISTS) - this is why creation
		// and the v4 migration seed the racial set: the seeded level-1
		// records make the gate real.
		character := euCharacter(60, nil, heavyRow())
		rt, _ := newTestRuntime(character, items)
		if code, refused := moveRefusalCode(t, rt, character, 20, 1); refused {
			t.Fatalf("mastery-less equip refused with 0x%02X, want the documented native miss-pass", code)
		}
	})
}

// The EU caster clothes shape: four mastery types sharing one value, and
// the native walk is ANY-of with an early exit - ONE sufficient mastery
// clears the whole bit.
func TestEquipEuCasterClothesAnyOfMasteries(t *testing.T) {
	items := requirementTestItems()
	clothesRow := func() enterworld.InventoryRow { return bagRow(20, items, "ITEM_EU_M_CLOTHES_07_BA_A") }
	base := enterworld.DefaultMasteries(enterworld.RaceKeyEurope)

	t.Run("one sufficient mastery passes", func(t *testing.T) {
		// Bard 60 among {Wizard, Warlock, Bard, Cleric} @ 60: ANY-of.
		character := euCharacter(60, withMastery(base, 517, 60), clothesRow())
		rt, _ := newTestRuntime(character, items)
		if code, refused := moveRefusalCode(t, rt, character, 20, 1); refused {
			t.Fatalf("Bard-60 equip of the four-way caster clothes refused with 0x%02X", code)
		}
	})

	t.Run("all four below the tier refuse", func(t *testing.T) {
		masteries := base
		for _, id := range []uint32{514, 516, 517, 518} {
			masteries = withMastery(masteries, id, 59)
		}
		character := euCharacter(60, masteries, clothesRow())
		rt, _ := newTestRuntime(character, items)
		code, refused := moveRefusalCode(t, rt, character, 20, 1)
		if !refused || code != wire.ErrCodeCantEquip {
			t.Fatalf("all-caster-59 equip of the 60 tier = refused=%v code=0x%02X, want [02 0E]", refused, code)
		}
	})
}

func TestEquipRefusesGenderMismatch(t *testing.T) {
	items := requirementTestItems()
	// Male character, woman-only garment (RequiredSex 0).
	character := requirementTestCharacter(20, []enterworld.InventoryRow{
		bagRow(20, items, "ITEM_CH_W_CLOTHES_01_BA_A"),
	})
	rt, _ := newTestRuntime(character, items)

	code, refused := moveRefusalCode(t, rt, character, 20, 1)
	if !refused || code != wire.ErrCodeGenderMismatch {
		t.Fatalf("male equip of a RequiredSex-0 garment = refused=%v code=0x%02X, want [02 16]", refused, code)
	}
}

// The razed/broken gate (client 789C60 bit 0x004 @0x00789cc7): an item
// whose CLASS carries a durability attribute (Dur_U > 0) refuses to equip
// while its instance durability reads 0; durability-less accessories at 0
// pass; and razed evaluates BEFORE the STR bit (0x008).
func TestEquipRefusesRazedItem(t *testing.T) {
	items := requirementTestItems()

	t.Run("broken durability-bearing item refused before STR", func(t *testing.T) {
		// Instance durability 0 + RequiredStr 120: the refusal must be the
		// razed byte, not the STR byte - native evaluates 0x004 first.
		character := requirementTestCharacter(20, []enterworld.InventoryRow{
			bagRow(20, items, "ITEM_CH_SWORD_05_A"),
		})
		rt, _ := newTestRuntime(character, items)

		code, refused := moveRefusalCode(t, rt, character, 20, 6)
		if !refused || code != wire.ErrCodeCantEquipRazed {
			t.Fatalf("broken sword equip = refused=%v code=0x%02X, want [02 37]", refused, code)
		}
	})

	t.Run("intact instance passes the razed bit", func(t *testing.T) {
		row := bagRow(20, items, "ITEM_CH_SWORD_05_A")
		row.Durability = 87
		character := requirementTestCharacter(20, []enterworld.InventoryRow{row})
		rt, _ := newTestRuntime(character, items)

		// Durability > 0 clears 0x004; the next native bit (STR 0x008)
		// answers instead - proving razed no longer masks the ladder.
		code, refused := moveRefusalCode(t, rt, character, 20, 6)
		if !refused || code != wire.ErrCodeStrengthRequired {
			t.Fatalf("intact sword equip = refused=%v code=0x%02X, want the STR notice [02 30]", refused, code)
		}
	})

	t.Run("durability-less accessory at 0 passes", func(t *testing.T) {
		// Rings carry Dur_U = 0 in the shipped itemdata: the native blob
		// (+0xc0)[5] guard means the razed bit never tests them.
		character := requirementTestCharacter(20, []enterworld.InventoryRow{
			bagRow(20, items, "ITEM_ANY_REGION_RING"),
		})
		rt, _ := newTestRuntime(character, items)

		if code, refused := moveRefusalCode(t, rt, character, 20, 12); refused {
			t.Fatalf("zero-durability ring refused with 0x%02X", code)
		}
	})
}

func TestEquipAllowsAnyRegionCountryWildcard(t *testing.T) {
	items := requirementTestItems()
	// CH character, Country-3 ring: the 0x200 wildcard never refuses.
	character := requirementTestCharacter(20, []enterworld.InventoryRow{
		bagRow(20, items, "ITEM_ANY_REGION_RING"),
	})
	rt, _ := newTestRuntime(character, items)

	if code, refused := moveRefusalCode(t, rt, character, 20, 12); refused {
		t.Fatalf("CH equip of a Country-3 ring refused with 0x%02X", code)
	}
}

func TestEquipRefusesCountryMismatch(t *testing.T) {
	items := requirementTestItems()
	// CH character, EU-only garment (Country 1); RequiredSex 1 matches so
	// only the country bit refuses.
	character := requirementTestCharacter(20, []enterworld.InventoryRow{
		bagRow(20, items, "ITEM_EU_M_CLOTHES_01_BA_A"),
	})
	rt, _ := newTestRuntime(character, items)

	code, refused := moveRefusalCode(t, rt, character, 20, 1)
	if !refused || code != wire.ErrCodeCountryMismatch {
		t.Fatalf("CH equip of an EU garment = refused=%v code=0x%02X, want [02 2F]", refused, code)
	}
}

func TestEquipRequirementOrderMatchesNativeMask(t *testing.T) {
	items := requirementTestItems()

	t.Run("level (0x020) beats exclusivity (0x080)", func(t *testing.T) {
		// Worn hard armor + an UNDER-LEVELED garment: native evaluates the
		// level bit before the exclusivity scan.
		underLeveled := *items["ITEM_CH_W_CLOTHES_01_BA_A"]
		underLeveled.Codename = "ITEM_CH_W_CLOTHES_09_BA_A"
		underLeveled.ReqQuadTypes, underLeveled.ReqQuadValues = charLevelQuad(40)
		underLeveled.RequiredSex = 2
		items[underLeveled.Codename] = &underLeveled

		character := requirementTestCharacter(5, []enterworld.InventoryRow{
			bagRow(4, items, "ITEM_CH_M_HEAVY_01_BA_A"),
			bagRow(20, items, underLeveled.Codename),
		})
		rt, _ := newTestRuntime(character, items)

		code, refused := moveRefusalCode(t, rt, character, 20, 1)
		if !refused || code != wire.ErrCodeLevelRequired {
			t.Fatalf("under-leveled garment onto worn hard armor = 0x%02X, want the level notice 0x10 first", code)
		}
	})

	t.Run("mastery (0x100) beats gender (0x040)", func(t *testing.T) {
		// A FEMALE EU character below the Warrior-35 tier holding the
		// male-only (RequiredSex 1) EU heavy armor: BOTH the mastery and
		// the gender bits fail, and native evaluates 0x100 before 0x040 -
		// the answer must be the mastery refusal (the generic equip guide
		// 0x0E), never the gender byte 0x16.
		base := enterworld.DefaultMasteries(enterworld.RaceKeyEurope)
		character := euCharacter(60, withMastery(base, 513, 20),
			bagRow(20, items, "ITEM_EU_M_HEAVY_05_BA_A"))
		female := enterworld.GenderFemale
		character.Gender = &female
		character.ModelCodename = "CHAR_EU_WOMAN_ADVENTURER"
		rt, _ := newTestRuntime(character, items)

		code, refused := moveRefusalCode(t, rt, character, 20, 1)
		if !refused || code != wire.ErrCodeCantEquip {
			t.Fatalf("unmet mastery + wrong gender = refused=%v code=0x%02X, want the mastery notice [02 0E] first", refused, code)
		}
	})

	t.Run("exclusivity (0x080) beats country (0x200)", func(t *testing.T) {
		// Worn hard armor + an EU garment on a CH character: the exclusivity
		// scan runs before the country bit.
		character := requirementTestCharacter(20, []enterworld.InventoryRow{
			bagRow(4, items, "ITEM_CH_M_HEAVY_01_BA_A"),
			bagRow(20, items, "ITEM_EU_M_CLOTHES_01_BA_A"),
		})
		rt, _ := newTestRuntime(character, items)

		code, refused := moveRefusalCode(t, rt, character, 20, 1)
		if !refused || code != wire.ErrCodeExclusiveArmorMix {
			t.Fatalf("EU garment onto worn hard armor (CH char) = 0x%02X, want the exclusivity notice 0x32 first", code)
		}
	})
}

func TestEquipSwapBackRunsRequirementGates(t *testing.T) {
	items := requirementTestItems()
	// Unequipping the worn sword onto a bag slot holding an under-leveled
	// sword seats that occupant in the socket - the swap-back leg runs the
	// same gates against it.
	character := requirementTestCharacter(5, []enterworld.InventoryRow{
		{Slot: 6, RefObjID: items["ITEM_CH_SWORD_01_A"].RefObjID, Codename: "ITEM_CH_SWORD_01_A",
			TypeFlags: items["ITEM_CH_SWORD_01_A"].TypeFlags(), VarianceBits: "0", StackCount: 1},
		bagRow(20, items, "ITEM_CH_SWORD_03_A"),
	})
	rt, _ := newTestRuntime(character, items)

	code, refused := moveRefusalCode(t, rt, character, 6, 20)
	if !refused || code != wire.ErrCodeLevelRequired {
		t.Fatalf("swap-back seating a level-16 sword at level 5 = refused=%v code=0x%02X, want [02 10]", refused, code)
	}
}

func TestEquipRefusesStrengthBelowRequirement(t *testing.T) {
	items := requirementTestItems()
	// Level 60 < the blade's type-1 quad level 99 (ReqQuadValues) AND STR
	// (creation base 20) < RequiredStr 120: the answer must be the STR
	// byte, not the level byte - the native mask evaluates bit 0x008
	// (STR) before 0x020 (level).
	character := requirementTestCharacter(60, []enterworld.InventoryRow{
		bagRow(20, items, "ITEM_CUSTOM_STR_BLADE"),
	})
	rt, _ := newTestRuntime(character, items)

	code, refused := moveRefusalCode(t, rt, character, 20, 6)
	if !refused || code != wire.ErrCodeStrengthRequired {
		t.Fatalf("stat-gated blade = refused=%v code=0x%02X, want [02 30]", refused, code)
	}
}

func TestEquipAllowsSufficientStrength(t *testing.T) {
	items := requirementTestItems()
	character := requirementTestCharacter(99, []enterworld.InventoryRow{
		bagRow(20, items, "ITEM_CUSTOM_STR_BLADE"),
	})
	strength := int64(150)
	character.Strength = &strength
	rt, _ := newTestRuntime(character, items)

	if code, refused := moveRefusalCode(t, rt, character, 20, 6); refused {
		t.Fatalf("STR-150 level-99 equip of the stat blade refused with 0x%02X", code)
	}
}

func TestEquipAllowsFemaleGenderMatch(t *testing.T) {
	items := requirementTestItems()
	// The flipped-enum mapping in the OTHER direction: persisted gender 1
	// (bootstrap female) must land on native selector 0 and pass a
	// RequiredSex-0 garment.
	character := requirementTestCharacter(20, []enterworld.InventoryRow{
		bagRow(20, items, "ITEM_CH_W_CLOTHES_01_BA_A"),
	})
	female := enterworld.GenderFemale
	character.Gender = &female
	character.ModelCodename = "CHAR_CH_WOMAN_ADVENTURER"
	rt, _ := newTestRuntime(character, items)

	if code, refused := moveRefusalCode(t, rt, character, 20, 1); refused {
		t.Fatalf("female equip of a RequiredSex-0 garment refused with 0x%02X", code)
	}
}

func TestEquipRefusesIntellectRequirement(t *testing.T) {
	items := requirementTestItems()
	// INT (creation base 20) < RequiredInt 130.
	character := requirementTestCharacter(60, []enterworld.InventoryRow{
		bagRow(20, items, "ITEM_CUSTOM_INT_ORB"),
	})
	rt, _ := newTestRuntime(character, items)

	code, refused := moveRefusalCode(t, rt, character, 20, 6)
	if !refused || code != wire.ErrCodeIntellectRequired {
		t.Fatalf("INT-gated orb = refused=%v code=0x%02X, want [02 31]", refused, code)
	}
}

func TestEquipAllowsSufficientIntellect(t *testing.T) {
	items := requirementTestItems()
	character := requirementTestCharacter(60, []enterworld.InventoryRow{
		bagRow(20, items, "ITEM_CUSTOM_INT_ORB"),
	})
	intellect := int64(150)
	character.Intellect = &intellect
	rt, _ := newTestRuntime(character, items)

	if code, refused := moveRefusalCode(t, rt, character, 20, 6); refused {
		t.Fatalf("INT-150 equip of the INT orb refused with 0x%02X", code)
	}
}

func TestEquipAllowsEuropeCountryMatch(t *testing.T) {
	items := requirementTestItems()
	// The country mapping in the OTHER direction: a CHAR_EU_ character must
	// land on native country byte 1 and pass a Country-1 garment.
	character := requirementTestCharacter(20, []enterworld.InventoryRow{
		bagRow(20, items, "ITEM_EU_M_CLOTHES_01_BA_A"),
	})
	character.ModelCodename = "CHAR_EU_MAN_ROGUE"
	rt, _ := newTestRuntime(character, items)

	if code, refused := moveRefusalCode(t, rt, character, 20, 1); refused {
		t.Fatalf("EU equip of an EU garment refused with 0x%02X", code)
	}
}

func TestEquipRingSecondHandRunsRequirementGates(t *testing.T) {
	items := requirementTestItems()

	t.Run("under-leveled ring refused into either hand", func(t *testing.T) {
		character := requirementTestCharacter(5, []enterworld.InventoryRow{
			bagRow(20, items, "ITEM_CH_RING_03_A"),
		})
		rt, _ := newTestRuntime(character, items)

		code, refused := moveRefusalCode(t, rt, character, 20, 12)
		if !refused || code != wire.ErrCodeLevelRequired {
			t.Fatalf("level-5 ring equip into the second hand = refused=%v code=0x%02X, want [02 10]", refused, code)
		}
	})

	t.Run("qualified ring allowed into the second hand", func(t *testing.T) {
		character := requirementTestCharacter(16, []enterworld.InventoryRow{
			bagRow(20, items, "ITEM_CH_RING_03_A"),
		})
		rt, _ := newTestRuntime(character, items)

		if code, refused := moveRefusalCode(t, rt, character, 20, 12); refused {
			t.Fatalf("level-16 ring into the second hand refused with 0x%02X", code)
		}
	})
}

// The fortress-guild gate (client 789C60 bit 0x400 @0x00789fd7) is
// SOFT: a character with NO fortress-guild member record passes
// (client @0x0078a02e) - the retail posture for everyone outside a fortress
// guild, and this server's permanent posture until a fortress membership
// model exists. The refusal fires only when a member record exists and
// its role byte shares no bit with the item's mask.
func TestEquipFortressWeaponSoftGate(t *testing.T) {
	items := requirementTestItems()
	hammerRow := func() enterworld.InventoryRow { return bagRow(20, items, "ITEM_FORT_FORTRESS_HAMMER_06") }

	t.Run("no member record passes (the native miss)", func(t *testing.T) {
		character := requirementTestCharacter(1, []enterworld.InventoryRow{hammerRow()})
		rt, _ := newTestRuntime(character, items)
		if code, refused := moveRefusalCode(t, rt, character, 20, 6); refused {
			t.Fatalf("member-less fortress hammer equip refused with 0x%02X, want the native record-less pass", code)
		}
	})

	t.Run("member with a matching role bit passes", func(t *testing.T) {
		character := requirementTestCharacter(1, []enterworld.InventoryRow{hammerRow()})
		rt, _ := newTestRuntime(character, items)
		rt.FortressGuildRole = func(*enterworld.Character) (uint8, bool) {
			return 0x20, true
		}
		if code, refused := moveRefusalCode(t, rt, character, 20, 6); refused {
			t.Fatalf("engineer equip of an any-role (-1) hammer refused with 0x%02X", code)
		}
	})

	t.Run("member with no matching role bit refuses", func(t *testing.T) {
		// A commander-only mask (bit 0x01) against an engineer (0x20).
		commanderOnly := *items["ITEM_FORT_FORTRESS_HAMMER_06"]
		commanderOnly.Codename = "ITEM_FORT_COMMANDER_HAMMER"
		commanderOnly.FortressRoleMask = 0x01
		items[commanderOnly.Codename] = &commanderOnly

		character := requirementTestCharacter(1, []enterworld.InventoryRow{
			bagRow(20, items, commanderOnly.Codename),
		})
		rt, _ := newTestRuntime(character, items)
		rt.FortressGuildRole = func(*enterworld.Character) (uint8, bool) {
			return 0x20, true
		}
		code, refused := moveRefusalCode(t, rt, character, 20, 6)
		if !refused || code != wire.ErrCodeCantEquip {
			t.Fatalf("engineer equip of a commander-only hammer = refused=%v code=0x%02X, want [02 0E]", refused, code)
		}
	})
}

func TestEquipRefusesWithoutCombatItemdataRow(t *testing.T) {
	items := requirementTestItems()
	character := requirementTestCharacter(1, []enterworld.InventoryRow{
		{Slot: 20, RefObjID: 424242, Codename: "ITEM_CH_UNSEEDED_SWORD",
			TypeFlags: wire.PackTypeFlags(3, 1, 6, 2), VarianceBits: "0", StackCount: 1},
	})
	rt, _ := newTestRuntime(character, items)

	// Requirement lookup historically degraded open on a missing row. Once
	// the move also owns the derived-stat refresh, admitting that item would
	// commit an equipment state the canonical ParamKeeper graph cannot
	// represent. The transaction now fails closed before inventory changes.
	if code, refused := moveRefusalCode(t, rt, character, 20, 6); !refused || code != wire.ErrCodeInvalidRequest {
		t.Fatalf("unseeded item = refused=%v code=0x%02X, want the generic fail-closed row", refused, code)
	}
	if character.MissionInventory[0].Slot != 20 {
		t.Fatalf("refused unseeded equip moved inventory: %+v", character.MissionInventory)
	}
}
