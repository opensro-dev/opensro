/*
===========================================================================

automaxgear_apply_test.go - the flag-on rule against a real inventory

The tests build the same gameplay plane and action runtime the server wires,
with a small item catalog that mirrors the shipped type ids and level
requirements. They drive the rule itself (not only its config) so the
seating, the equip gate and the destroyed piece are pinned.

===========================================================================
*/
package main

import (
	"testing"

	"opensro.online/server/internal/game/action"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

// autoMaxRefs is a test ItemRefSource over the codenames these tests use.
type autoMaxRefs map[string]*enterworld.ItemRef

func (s autoMaxRefs) ItemRefByCodename(codename string) (*enterworld.ItemRef, bool) {
	ref, ok := s[codename]
	return ref, ok
}

func (s autoMaxRefs) ItemRefByID(id uint32) (*enterworld.ItemRef, bool) {
	for _, ref := range s {
		if ref.RefObjID == id {
			return ref, true
		}
	}
	return nil, false
}

// autoMaxMagicOptions resolves any param id to an STR option, so the minted
// blues pass the stat projection.
type autoMaxMagicOptions struct{}

func (autoMaxMagicOptions) MagicOptionByParamID(paramID uint32) (*enterworld.MagicOptionRow, bool) {
	// 'str' in the packed big-endian tag the combat switch reads.
	return &enterworld.MagicOptionRow{ParamID: paramID, OptionName: "MATTR_STR", Tag: 0x737472}, true
}

// autoMaxRef builds an equipment reference with the shipped shape: the type
// ids select the socket, and the typed-quad level leg carries the catalogue
// level.
func autoMaxRef(id uint32, codename string, typeIDs [4]int64, level int64) *enterworld.ItemRef {
	return &enterworld.ItemRef{
		RefObjID:      id,
		Codename:      codename,
		TypeIDs:       typeIDs,
		Country:       0,
		RequiredSex:   2,
		ReqQuadTypes:  [4]int64{1, -1, -1, -1},
		ReqQuadValues: [4]int64{level, -1, -1, -1},
		Combat:        &enterworld.ItemCombatRef{},
	}
}

// autoMaxCatalog is the piece set the tests use: CH male heavy armour at
// D1 and D2 (the D2 slots carry their shipped per-slot levels), one D2
// sword and the CH accessories.
//
// The codename tokens name the piece part (AA arm, LA leg, CA head, BA body,
// SA shoulder, FA foot); the TYPE IDS select the socket, and the two must
// agree with inventory.EquipSocketForTypeFlags. The D2 per-slot levels are
// the shipped ones: legs 8, shoulders 9, feet 10, head 11, hands 12, chest 13.
func autoMaxCatalog() autoMaxRefs {
	return autoMaxRefs{
		"ITEM_CH_M_HEAVY_01_AA_C_RARE": autoMaxRef(9001, "ITEM_CH_M_HEAVY_01_AA_C_RARE", [4]int64{3, 1, 3, 5}, 1),
		"ITEM_CH_M_HEAVY_02_AA_C_RARE": autoMaxRef(9002, "ITEM_CH_M_HEAVY_02_AA_C_RARE", [4]int64{3, 1, 3, 5}, 8),
		"ITEM_CH_M_HEAVY_02_SA_C_RARE": autoMaxRef(9003, "ITEM_CH_M_HEAVY_02_SA_C_RARE", [4]int64{3, 1, 3, 2}, 9),
		"ITEM_CH_M_HEAVY_02_FA_C_RARE": autoMaxRef(9004, "ITEM_CH_M_HEAVY_02_FA_C_RARE", [4]int64{3, 1, 3, 6}, 10),
		"ITEM_CH_M_HEAVY_02_CA_C_RARE": autoMaxRef(9005, "ITEM_CH_M_HEAVY_02_CA_C_RARE", [4]int64{3, 1, 3, 1}, 11),
		"ITEM_CH_M_HEAVY_02_LA_C_RARE": autoMaxRef(9006, "ITEM_CH_M_HEAVY_02_LA_C_RARE", [4]int64{3, 1, 3, 4}, 12),
		"ITEM_CH_M_HEAVY_02_BA_C_RARE": autoMaxRef(9007, "ITEM_CH_M_HEAVY_02_BA_C_RARE", [4]int64{3, 1, 3, 3}, 13),
		"ITEM_CH_SWORD_02_A_RARE":      autoMaxRef(9009, "ITEM_CH_SWORD_02_A_RARE", [4]int64{3, 1, 6, 2}, 8),
		"ITEM_CH_SWORD_02_C_RARE":      autoMaxRef(9010, "ITEM_CH_SWORD_02_C_RARE", [4]int64{3, 1, 6, 2}, 13),
		"ITEM_CH_EARRING_01_C_RARE":    autoMaxRef(9011, "ITEM_CH_EARRING_01_C_RARE", [4]int64{3, 1, 5, 1}, 1),
		"ITEM_CH_NECKLACE_01_C_RARE":   autoMaxRef(9012, "ITEM_CH_NECKLACE_01_C_RARE", [4]int64{3, 1, 5, 2}, 1),
		"ITEM_CH_RING_01_C_RARE":       autoMaxRef(9013, "ITEM_CH_RING_01_C_RARE", [4]int64{3, 1, 5, 3}, 1),
	}
}

// autoMaxRow is an equipped row for a catalogue reference.
func autoMaxRow(slot int64, ref *enterworld.ItemRef) enterworld.InventoryRow {
	return enterworld.InventoryRow{
		Slot: slot, RefObjID: ref.RefObjID, Codename: ref.Codename,
		TypeFlags: ref.TypeFlags(), VarianceBits: "0", Durability: 100, StackCount: 1,
	}
}

// autoMaxPlane builds the plane the server wires, over the given catalogue.
func autoMaxPlane(t *testing.T, c *enterworld.Character, refs enterworld.ItemRefSource) *gameplayPlane {
	t.Helper()
	deps := &enterworld.Deps{
		Items:        refs,
		MagicOptions: autoMaxMagicOptions{},
	}
	return &gameplayPlane{divisionID: "test", deps: deps, items: action.NewRuntime(deps, nil)}
}

// autoMaxCharacter is a level-N CH male character in heavy armour.
func autoMaxCharacter(level int64, rows ...enterworld.InventoryRow) *enterworld.Character {
	base := int64(enterworld.BaseStat)
	max := level
	return &enterworld.Character{
		ID: 1, Name: "Tester", ModelCodename: "CHAR_CH_MAN_ADVENTURER",
		Level: &level, MaxLevel: &max, Strength: &base, Intellect: &base,
		MissionInventory: rows,
	}
}

// socketRow returns the row in a socket, or nil.
func socketRow(c *enterworld.Character, slot int64) *enterworld.InventoryRow {
	for i := range c.MissionInventory {
		if c.MissionInventory[i].Slot == slot {
			return &c.MissionInventory[i]
		}
	}
	return nil
}

/*
================
TestApplyAutoMaxGearSeatsBestPiece

Flag on: a worn level-1 armour piece becomes the best variant the level
allows, with the configured plus and the minted whites and blues.
================
*/
func TestApplyAutoMaxGearSeatsBestPiece(t *testing.T) {
	t.Setenv(EnvAutoMaxGear, "on")
	refs := autoMaxCatalog()
	c := autoMaxCharacter(12,
		autoMaxRow(1, refs["ITEM_CH_M_HEAVY_02_BA_C_RARE"]),
		autoMaxRow(3, refs["ITEM_CH_M_HEAVY_01_AA_C_RARE"]),
	)
	game := autoMaxPlane(t, c, refs)
	frames := game.applyAutoMaxGear(c)
	if len(frames) == 0 {
		t.Fatal("an upgrade must publish frames")
	}
	seated := socketRow(c, 3)
	if seated == nil || seated.Codename != "ITEM_CH_M_HEAVY_02_AA_C_RARE" {
		t.Fatalf("socket 3 = %+v, want the D2 arms piece", seated)
	}
	if seated.Plus != autoMaxGearDefaultPlus {
		t.Fatalf("plus = %d, want %d", seated.Plus, autoMaxGearDefaultPlus)
	}
	if seated.VarianceBits == "" || seated.VarianceBits == "0" {
		t.Fatalf("minted whites missing: %q", seated.VarianceBits)
	}
}

/*
================
TestApplyAutoMaxGearFillsEmptySockets

Empty armour and accessory sockets are filled with the best piece the level
allows; no weapon, shield or job-dress socket is ever created.
================
*/
func TestApplyAutoMaxGearFillsEmptySockets(t *testing.T) {
	t.Setenv(EnvAutoMaxGear, "on")
	refs := autoMaxCatalog()
	// One worn piece supplies the race, gender and armour class the empty
	// sockets are filled from.
	c := autoMaxCharacter(8, autoMaxRow(1, refs["ITEM_CH_M_HEAVY_02_BA_C_RARE"]))
	game := autoMaxPlane(t, c, refs)
	game.applyAutoMaxGear(c)
	for _, socket := range []int64{6, 7, 8} {
		if row := socketRow(c, socket); row != nil {
			t.Fatalf("socket %d was created: %+v", socket, row)
		}
	}
	// Level 8 admits only the D2 arms (socket 3) among the empty armour
	// sockets; every other D2 piece needs a later level.
	if row := socketRow(c, 3); row == nil || row.Codename != "ITEM_CH_M_HEAVY_02_AA_C_RARE" {
		t.Fatalf("arm socket = %+v, want the level-8 D2 piece", row)
	}
	for _, socket := range []int64{0, 2, 4, 5} {
		if row := socketRow(c, socket); row != nil {
			t.Fatalf("socket %d filled at level 8 though its piece needs more: %+v", socket, row)
		}
	}
	// Accessories are level 1 at every tier.
	if row := socketRow(c, 9); row == nil || row.Codename != "ITEM_CH_EARRING_01_C_RARE" {
		t.Fatalf("earring socket = %+v", row)
	}
	if row := socketRow(c, 11); row == nil || row.Codename != "ITEM_CH_RING_01_C_RARE" {
		t.Fatalf("ring socket = %+v", row)
	}
	if row := socketRow(c, 12); row == nil || row.Codename != "ITEM_CH_RING_01_C_RARE" {
		t.Fatalf("second ring socket = %+v, want the ring pool", row)
	}
}

/*
================
TestApplyAutoMaxGearPerSlotUnlock

Armour unlocks one slot at a time. At level 8 only the legs candidate
exists; the shoulders (9), feet (10), head (11), hands (12) and chest (13)
stay empty until their level.
================
*/
func TestApplyAutoMaxGearPerSlotUnlock(t *testing.T) {
	t.Setenv(EnvAutoMaxGear, "on")
	// The D2 armour slots unlock in order (catalogue levels): arms 8,
	// shoulders 9, feet 10, head 11, legs 12, chest 13. A fill is cumulative:
	// every slot whose level is reached holds a piece, the later ones wait.
	unlock := map[int64]int64{8: 3, 9: 2, 10: 5, 11: 0, 12: 4, 13: 1}
	refs := autoMaxCatalog()
	for level := int64(8); level <= 13; level++ {
		c := autoMaxCharacter(level, autoMaxRow(1, refs["ITEM_CH_M_HEAVY_02_BA_C_RARE"]))
		game := autoMaxPlane(t, c, refs)
		game.applyAutoMaxGear(c)
		for socket := int64(0); socket < 6; socket++ {
			row := socketRow(c, socket)
			reached := false
			for l, s := range unlock {
				if s == socket && l <= level {
					reached = true
				}
			}
			if reached && row == nil {
				t.Fatalf("level %d: socket %d stays empty though its piece is available", level, socket)
			}
			if !reached && row != nil && socket != 1 {
				// Socket 1 is the worn chest the identity read comes from.
				t.Fatalf("level %d: socket %d filled before its level: %+v", level, socket, row)
			}
		}
	}
}

/*
================
TestApplyAutoMaxGearSkipsGateRefusedCandidate

A candidate the equip gate refuses falls through to the next-lower one,
exactly as the operator socket grant does.
================
*/
func TestApplyAutoMaxGearSkipsGateRefusedCandidate(t *testing.T) {
	t.Setenv(EnvAutoMaxGear, "on")
	refs := autoMaxCatalog()
	refs["ITEM_CH_M_HEAVY_02_AA_C_RARE"].RequiredStr = 999
	c := autoMaxCharacter(12,
		autoMaxRow(1, refs["ITEM_CH_M_HEAVY_02_BA_C_RARE"]),
		autoMaxRow(3, refs["ITEM_CH_M_HEAVY_01_AA_C_RARE"]),
	)
	game := autoMaxPlane(t, c, refs)
	game.applyAutoMaxGear(c)
	row := socketRow(c, 3)
	if row == nil || row.Codename != "ITEM_CH_M_HEAVY_01_AA_C_RARE" {
		t.Fatalf("socket 3 = %+v, want the worn D1 piece to stay", row)
	}
}

/*
================
TestApplyAutoMaxGearSameDegreeNextLevelVariant

A degree spans several levels. A same-degree, higher-level variant replaces
the worn piece.
================
*/
func TestApplyAutoMaxGearSameDegreeNextLevelVariant(t *testing.T) {
	t.Setenv(EnvAutoMaxGear, "on")
	refs := autoMaxCatalog()
	c := autoMaxCharacter(13, autoMaxRow(6, refs["ITEM_CH_SWORD_02_A_RARE"]))
	game := autoMaxPlane(t, c, refs)
	game.applyAutoMaxGear(c)
	row := socketRow(c, 6)
	if row == nil || row.Codename != "ITEM_CH_SWORD_02_C_RARE" {
		t.Fatalf("weapon socket = %+v, want the level-13 same-degree variant", row)
	}
}

/*
================
TestApplyAutoMaxGearKeepsEnhancedWornPiece

The swap destroys the worn piece, so a piece the player enhanced or rerolled
above the configuration stays: a higher degree alone is not proof of a
better piece when the worn one already carries +12 and the best blues.
================
*/
func TestApplyAutoMaxGearKeepsEnhancedWornPiece(t *testing.T) {
	t.Setenv(EnvAutoMaxGear, "on")
	t.Setenv(EnvAutoMaxGearPlus, "7")
	refs := autoMaxCatalog()
	worn := autoMaxRow(3, refs["ITEM_CH_M_HEAVY_02_AA_C_RARE"])
	worn.Plus = 12
	worn.VarianceBits = "9223372036854775808" // only the top bit: not a full white set
	worn.MagicOptions = []uint64{uint64(31) << 32}
	c := autoMaxCharacter(12,
		autoMaxRow(1, refs["ITEM_CH_M_HEAVY_02_BA_C_RARE"]),
		worn,
	)
	game := autoMaxPlane(t, c, refs)
	game.applyAutoMaxGear(c)
	row := socketRow(c, 3)
	if row == nil || row.Codename != "ITEM_CH_M_HEAVY_02_AA_C_RARE" {
		t.Fatalf("worn piece replaced or lost: %+v", row)
	}
}

/*
================
TestApplyAutoMaxGearNeverCreatesWeaponOrShield

An empty weapon, shield or job-dress socket is never filled.
================
*/
func TestApplyAutoMaxGearNeverCreatesWeaponOrShield(t *testing.T) {
	t.Setenv(EnvAutoMaxGear, "on")
	refs := autoMaxCatalog()
	c := autoMaxCharacter(90, autoMaxRow(1, refs["ITEM_CH_M_HEAVY_02_BA_C_RARE"]))
	game := autoMaxPlane(t, c, refs)
	game.applyAutoMaxGear(c)
	for _, socket := range []int64{6, 7, 8} {
		if row := socketRow(c, socket); row != nil {
			t.Fatalf("socket %d was created: %+v", socket, row)
		}
	}
}

/*
================
TestUpdateExperienceLevelUpFiresGearRule

The real wrapper fires the rule on a level INCREASE and writes no frame when
the update reports no level change. The stat updater is stubbed; the rule and
the seating are the production ones.
================
*/
func TestUpdateExperienceLevelUpFiresGearRule(t *testing.T) {
	t.Setenv(EnvAutoMaxGear, "on")
	refs := autoMaxCatalog()
	newCharacter := func() *enterworld.Character {
		return autoMaxCharacter(1,
			autoMaxRow(1, refs["ITEM_CH_M_HEAVY_02_BA_C_RARE"]),
			autoMaxRow(3, refs["ITEM_CH_M_HEAVY_01_AA_C_RARE"]),
		)
	}

	c := newCharacter()
	game := autoMaxPlane(t, c, refs)
	levelUp := func(character *enterworld.Character, _, _ int64, _ uint32) ([]wire.Frame, bool) {
		raised := int64(8)
		character.Level = &raised
		character.MaxLevel = &raised
		return []wire.Frame{{Opcode: wire.OpLevelUpEffect}}, true
	}
	frames, ok := game.experienceWithAutoMaxGear(levelUp)(c, 1, 0, 0)
	if !ok {
		t.Fatal("the update was reported as refused")
	}
	if len(frames) < 2 {
		t.Fatalf("frames = %d, want the level-up frame and the gear frames", len(frames))
	}
	if row := socketRow(c, 3); row == nil || row.Codename != "ITEM_CH_M_HEAVY_02_AA_C_RARE" {
		t.Fatalf("socket 3 = %+v, want the rule to have upgraded it", row)
	}

	// No level change: the wrapper adds nothing.
	c2 := newCharacter()
	game2 := autoMaxPlane(t, c2, refs)
	flat := func(*enterworld.Character, int64, int64, uint32) ([]wire.Frame, bool) {
		return []wire.Frame{{Opcode: wire.OpLevelUpEffect}}, true
	}
	frames2, ok := game2.experienceWithAutoMaxGear(flat)(c2, 1, 0, 0)
	if !ok || len(frames2) != 1 {
		t.Fatalf("no level change added frames: %d", len(frames2))
	}
}
