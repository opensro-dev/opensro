/*
===========================================================================

forge_test.go - a GM's /FORGE (forge.go, forgekeys.go)

Port-only (issue #1980). The flag off refuses; on, one fixture item per
family forges with its named attributes and lands bound at the GM's
feet; every refusal the catalogue implies is refused whole; /forge
help lists exactly the keys the forge accepts; and the per-GM rate
limit meters forging, not help.

===========================================================================
*/
package action

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/alchemy"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	forgeSwordCodename   = "ITEM_CH_SWORD_07"
	forgeAvatarCodename  = "ITEM_M_AVATAR_FORGE_HAT"
	forgeCapsuleCodename = "ITEM_ETC_FORGE_CAPSULE"
	forgePotionCodename  = "ITEM_ETC_HP_POTION_01"
)

// forgeMagicOptions is the test MagicOptionSource: param id -> tag.
type forgeMagicOptions map[uint32]uint32

/*
================
MagicOptionByParamID
================
*/
func (s forgeMagicOptions) MagicOptionByParamID(id uint32) (*enterworld.MagicOptionRow, bool) {
	tag, ok := s[id]
	if !ok {
		return nil, false
	}
	return &enterworld.MagicOptionRow{ParamID: id, Tag: tag}, true
}

/*
================
forgeTestItems

The forge's fixture references: one item per family the key table
derives from (weapon, avatar, capsule, the stock potion, two COS
scrolls and the transform target).
================
*/
func forgeTestItems() cosTestItemSource {
	items := testItems()
	durabilityFloor := int64(100)
	items[forgeSwordCodename] = &enterworld.ItemRef{
		RefObjID: 2001, Codename: forgeSwordCodename, TypeIDs: [4]int64{3, 1, 6, 1},
		Country: 3, RequiredSex: 2, ReqQuadTypes: [4]int64{-1, -1, -1, -1},
		CanDrop: true, CanDropOnDeath: true,
		VarianceIntMin1c0: &durabilityFloor, MaxDurability: 130,
		Combat: &enterworld.ItemCombatRef{
			PhysicalAttack: enterworld.ItemAttackRange{
				Minimum: enterworld.ItemStatRange{Min: 15, Max: 16, PerPlus: 2.4},
				Maximum: enterworld.ItemStatRange{Min: 16, Max: 18, PerPlus: 2.4},
			},
			MagicalAttack: enterworld.ItemAttackRange{
				Minimum: enterworld.ItemStatRange{Min: 25, Max: 26, PerPlus: 4.1},
				Maximum: enterworld.ItemStatRange{Min: 28, Max: 31, PerPlus: 4.1},
			},
			HitRate:      enterworld.ItemStatRange{Min: 24, Max: 30},
			CriticalRate: enterworld.ItemStatRange{Min: 5, Max: 10},
		},
	}
	items[forgeAvatarCodename] = &enterworld.ItemRef{
		RefObjID: 2002, Codename: forgeAvatarCodename, TypeIDs: [4]int64{3, 1, 13, 1},
		Country: 3, RequiredSex: 2,
	}
	items[forgeCapsuleCodename] = &enterworld.ItemRef{
		RefObjID: 2003, Codename: forgeCapsuleCodename, TypeIDs: [4]int64{3, 2, 2, 1},
	}
	refs := testCosSource(items)
	for i, name := range []string{"ATTACK", "PICKUP"} {
		ref := &enterworld.ItemRef{Codename: "SUMMON_" + name, RefObjID: uint32(900 + i), TypeIDs: [4]int64{3, 2, 1, int64(i + 1)}, Country: 3, AssociatedCharacterCodename: name,
			NativeFields: enterworld.NewNativeFields(map[string]float64{"canUse": 1, "itemParam1_29c": 60})}
		refs.staticItemSource[ref.Codename] = ref
		refs.characters[name] = &enterworld.CharacterRef{Codename: name, RefObjID: uint32(950 + i), TidWord: 0x1c6 | uint16(i+3)<<11, Level: 1, MaxHP: 100, MaxMP: 40, RunSpeed: 60, WalkSpeed: 20, Scale: 100, InventoryCapacity: uint8(i * 28)}
	}
	refs.characters["MOB_FORGE_TEST"] = &enterworld.CharacterRef{Codename: "MOB_FORGE_TEST", RefObjID: 777, TidWord: 0x1c6, Level: 9, MaxHP: 500, MaxMP: 50, RunSpeed: 60, WalkSpeed: 20, Scale: 100}
	return refs
}

/*
================
forgeTestCatalog

The forge's fixture catalogue: a degree-7 sword (magic capacity 2)
with a discrete-value option, a counted luck option, a capacity-
raising 'dura' option, a crit option, an armor-only option the sword
must refuse, and one avatar option rolling a range.
================
*/
func forgeTestCatalog() *alchemy.Catalog {
	sword := &enterworld.ItemRef{TypeIDs: [4]int64{3, 1, 6, 1}}
	avatar := &enterworld.ItemRef{TypeIDs: [4]int64{3, 1, 13, 1}}
	return &alchemy.Catalog{
		Items: map[string]alchemy.Reference{
			forgeSwordCodename:  {ID: 2001, Name: forgeSwordCodename, Flags: sword.TypeFlags(), Class: 19, MaxMagic: 2},
			forgeAvatarCodename: {ID: 2002, Name: forgeAvatarCodename, Flags: avatar.TypeFlags(), Class: 1, MaxMagic: 1},
		},
		Magic: map[uint16]alchemy.Magic{
			101: {ID: 101, Name: "MATTR_STR", Degree: 7, Tag: 0x737472, Params: [3]uint32{0x000A0005, 0x0000000F, 0}, Categories: []string{"weapon"}},
			102: {ID: 102, Name: "MATTR_LUCK", Degree: 7, Tag: forgeTagLuck, Categories: []string{"weapon"}},
			103: {ID: 103, Name: "MATTR_DUR", Degree: 7, Tag: forgeTagDura, Params: [3]uint32{0x000A0000, 0, 0}, Categories: []string{"weapon"}},
			105: {ID: 105, Name: "MATTR_CRIT", Degree: 7, Tag: 0x637269, Params: [3]uint32{0x00030000, 0, 0}, Categories: []string{"weapon"}},
			106: {ID: 106, Name: "MATTR_INT", Degree: 7, Tag: 0x696e74, Params: [3]uint32{0x000A0000, 0, 0}, Categories: []string{"armor"}},
			201: {ID: 201, Name: "MATTR_AVA_HP", Degree: 1, Tag: 0x687000, Params: [3]uint32{0, 10, 100}},
		},
		AvatarOptions: map[uint8][]string{1: {"MATTR_AVA_HP"}},
	}
}

/*
================
forgeTestSource
================
*/
func forgeTestSource() forgeMagicOptions {
	return forgeMagicOptions{
		101: 0x737472, 102: forgeTagLuck, 103: forgeTagDura,
		105: 0x637269, 106: 0x696e74, 201: 0x687000,
	}
}

/*
================
forgeTestRuntime
================
*/
func forgeTestRuntime(c *enterworld.Character, items enterworld.ItemRefSource) (*Runtime, *fakeClock) {
	deps := &enterworld.Deps{
		Characters:   enterworld.StaticCharacterSource{testDivision: {c}},
		Items:        items,
		MagicOptions: forgeTestSource(),
		NpcSpawns:    enterworld.NpcSpawnConfig{Roster: simulation.DefaultNpcRoster()},
	}
	rt := NewRuntime(deps, nil)
	clock := &fakeClock{now: time.UnixMilli(1_000_000)}
	rt.Now = clock.Now
	rt.GMItemForge = true
	rt.Alchemy = forgeTestCatalog()
	return rt, clock
}

/*
================
forgeTestGM
================
*/
func forgeTestGM() *enterworld.Character {
	c := testCharacter()
	c.GMPrivilege = true
	c.MissionInventory = nil
	return c
}

/*
================
forgedDrop

The one ground item a successful forge leaves, or a failed test.
================
*/
func forgedDrop(t *testing.T, rt *Runtime) grounditem.Item {
	t.Helper()
	all := rt.Ground.All(testDivision)
	if len(all) != 1 {
		t.Fatalf("ground holds %d item(s), want the one forged drop", len(all))
	}
	return all[0]
}

/*
================
TestForgeRefusesWithTheFlagOff
================
*/
func TestForgeRefusesWithTheFlagOff(t *testing.T) {
	c := forgeTestGM()
	rt, _ := forgeTestRuntime(c, forgeTestItems())
	rt.GMItemForge = false
	if lines, ok := rt.ForgeGMItem(testDivision, c.Name, forgeSwordCodename+" plus=1"); ok || lines != nil {
		t.Fatalf("flag off forged: %v %v", lines, ok)
	}
	if lines, ok := rt.ForgeGMItem(testDivision, c.Name, "help "+forgeSwordCodename); ok || lines != nil {
		t.Fatalf("flag off answered help: %v %v", lines, ok)
	}
	if n := rt.Ground.Count(testDivision); n != 0 {
		t.Fatalf("flag off left %d ground item(s)", n)
	}
}

/*
================
TestForgeWeaponCarriesItsNamedAttributes
================
*/
func TestForgeWeaponCarriesItsNamedAttributes(t *testing.T) {
	c := forgeTestGM()
	rt, _ := forgeTestRuntime(c, forgeTestItems())
	lines, ok := rt.ForgeGMItem(testDivision, c.Name,
		forgeSwordCodename+" plus=12 white.phyatk=80 blue.MATTR_STR=max blue.MATTR_LUCK=3")
	if !ok || len(lines) != 1 || !strings.Contains(lines[0], "+12") || !strings.Contains(lines[0], "(bound)") {
		t.Fatalf("forge answer = %v, %v", lines, ok)
	}
	drop := forgedDrop(t, rt)
	if !drop.ForgeBound || drop.Plus != 12 || drop.RefObjID != 2001 {
		t.Fatalf("drop = %+v", drop)
	}
	if got := (drop.VarianceBits >> 20) & 0x1f; got != 25 {
		t.Fatalf("phyatk variance field = %d, want 25 (80%%)", got)
	}
	wantOptions := []uint64{15<<32 | 101, 3<<32 | 102}
	if fmt.Sprint(drop.MagicOptions) != fmt.Sprint(wantOptions) {
		t.Fatalf("options = %v, want %v", drop.MagicOptions, wantOptions)
	}
	// Full durability at the forged spec: variance floor 100, no
	// durability blue, field 0 unset.
	if drop.Durability != 100 {
		t.Fatalf("durability = %d, want 100", drop.Durability)
	}
	if drop.DroppedBy != c.Name {
		t.Fatalf("drop owner = %q, want the GM %q", drop.DroppedBy, c.Name)
	}
}

/*
================
TestForgeWhiteAllAndDurabilityFollowTheSpec
================
*/
func TestForgeWhiteAllAndDurabilityFollowTheSpec(t *testing.T) {
	c := forgeTestGM()
	rt, _ := forgeTestRuntime(c, forgeTestItems())
	if _, ok := rt.ForgeGMItem(testDivision, c.Name, forgeSwordCodename+" white=100 durability=120"); !ok {
		t.Fatal("white=100 with durability 120 refused (ceiling is 130 at field 31)")
	}
	drop := forgedDrop(t, rt)
	if drop.VarianceBits != 0x1f|(0x1f<<15)|(0x1f<<20)|(0x1f<<25)|(0x1f<<30) {
		t.Fatalf("variance = %#x, want every weapon field at 31", drop.VarianceBits)
	}
	if drop.Durability != 120 {
		t.Fatalf("durability = %d, want 120", drop.Durability)
	}
}

/*
================
TestForgeBlueCapacityCountsTheSetItself
================
*/
func TestForgeBlueCapacityCountsTheSetItself(t *testing.T) {
	c := forgeTestGM()
	rt, _ := forgeTestRuntime(c, forgeTestItems())
	// Capacity 2: STR + LUCK + CRIT is one too many...
	if _, ok := rt.ForgeGMItem(testDivision, c.Name,
		forgeSwordCodename+" blue.MATTR_STR=10 blue.MATTR_LUCK=2 blue.MATTR_CRIT=3"); ok {
		t.Fatal("three blues on a capacity-2 sword forged")
	}
	// ...but a 'dura' blue raises the capacity it lands in.
	if _, ok := rt.ForgeGMItem(testDivision, c.Name,
		forgeSwordCodename+" blue.MATTR_STR=10 blue.MATTR_LUCK=2 blue.MATTR_DUR=10"); !ok {
		t.Fatal("the dura-raised capacity refused its own set")
	}
	drop := forgedDrop(t, rt)
	if len(drop.MagicOptions) != 3 {
		t.Fatalf("options = %v, want three", drop.MagicOptions)
	}
}

/*
================
TestForgeBlueFullTakesEveryOptionAtMax
================
*/
func TestForgeBlueFullTakesEveryOptionAtMax(t *testing.T) {
	c := forgeTestGM()
	rt, _ := forgeTestRuntime(c, forgeTestItems())
	if _, ok := rt.ForgeGMItem(testDivision, c.Name, forgeSwordCodename+" blue=full"); !ok {
		t.Fatal("blue=full refused")
	}
	drop := forgedDrop(t, rt)
	// Param-id order, each at max; CRIT no longer fits the capacity
	// the first three leave (2 + 1 for the dura option).
	want := []uint64{15<<32 | 101, 6<<32 | 102, 10<<32 | 103}
	if fmt.Sprint(drop.MagicOptions) != fmt.Sprint(want) {
		t.Fatalf("options = %v, want %v", drop.MagicOptions, want)
	}
}

/*
================
TestForgeStackableAvatarCapsuleAndPet
================
*/
func TestForgeStackableAvatarCapsuleAndPet(t *testing.T) {
	t.Run("stackable count", func(t *testing.T) {
		c := forgeTestGM()
		rt, _ := forgeTestRuntime(c, forgeTestItems())
		if _, ok := rt.ForgeGMItem(testDivision, c.Name, forgePotionCodename+" count=25"); !ok {
			t.Fatal("potion forge refused")
		}
		drop := forgedDrop(t, rt)
		if drop.StackCount != 25 || !drop.ForgeBound {
			t.Fatalf("drop = %+v", drop)
		}
	})
	t.Run("avatar blue range", func(t *testing.T) {
		c := forgeTestGM()
		rt, _ := forgeTestRuntime(c, forgeTestItems())
		if _, ok := rt.ForgeGMItem(testDivision, c.Name, forgeAvatarCodename+" blue.MATTR_AVA_HP=50"); !ok {
			t.Fatal("avatar forge refused")
		}
		drop := forgedDrop(t, rt)
		if fmt.Sprint(drop.MagicOptions) != fmt.Sprint([]uint64{50<<32 | 201}) {
			t.Fatalf("options = %v", drop.MagicOptions)
		}
		if _, ok := rt.ForgeGMItem(testDivision, c.Name, forgeAvatarCodename+" blue.MATTR_AVA_HP=5"); ok {
			t.Fatal("an avatar blue below its roll range forged")
		}
		if _, ok := rt.ForgeGMItem(testDivision, c.Name, forgeAvatarCodename+" plus=1"); ok {
			t.Fatal("an avatar took a plus")
		}
	})
	t.Run("capsule transform", func(t *testing.T) {
		c := forgeTestGM()
		rt, _ := forgeTestRuntime(c, forgeTestItems())
		if _, ok := rt.ForgeGMItem(testDivision, c.Name, forgeCapsuleCodename+" transform=mob_forge_test"); !ok {
			t.Fatal("capsule forge refused")
		}
		if drop := forgedDrop(t, rt); drop.TransformRefObjID != 777 {
			t.Fatalf("transform = %d, want the MOB_FORGE_TEST ref 777", drop.TransformRefObjID)
		}
		if _, ok := rt.ForgeGMItem(testDivision, c.Name, forgeCapsuleCodename+" transform=MOB_NOPE"); ok {
			t.Fatal("an unknown transform target forged")
		}
	})
	t.Run("pet level and name", func(t *testing.T) {
		c := forgeTestGM()
		rt, _ := forgeTestRuntime(c, forgeTestItems())
		if _, ok := rt.ForgeGMItem(testDivision, c.Name, "SUMMON_PICKUP pet.level=20 pet.name=Fetch"); !ok {
			t.Fatal("pickup pet forge refused")
		}
		drop := forgedDrop(t, rt)
		if drop.Summon == nil {
			t.Fatal("the forged scroll carries no pet")
		}
		pet := drop.Summon
		if pet.Name != "Fetch" || pet.Level != 20 || pet.CurrentHP != 100 || pet.Satiety != initialCompanionSatiety {
			t.Fatalf("pet = %+v", pet)
		}
		if pet.CommandMode != initialPickupCompanionMode || pet.Container == nil || pet.Container.Capacity != initialPickupCompanionSlots {
			t.Fatalf("pickup pet shape = %+v", pet)
		}
		if want := int64(1_000_000/1000 + 60*60); pet.RentalExpiresAtUnix != want {
			t.Fatalf("rental expiry = %d, want %d", pet.RentalExpiresAtUnix, want)
		}
	})
	t.Run("plain summoner without pet keys stays unmade", func(t *testing.T) {
		c := forgeTestGM()
		rt, _ := forgeTestRuntime(c, forgeTestItems())
		if _, ok := rt.ForgeGMItem(testDivision, c.Name, "SUMMON_ATTACK"); !ok {
			t.Fatal("plain summoner forge refused")
		}
		if drop := forgedDrop(t, rt); drop.Summon != nil {
			t.Fatalf("an unmade scroll carries a pet: %+v", drop.Summon)
		}
	})
}

/*
================
TestForgeRefusalsForgeNothing
================
*/
func TestForgeRefusalsForgeNothing(t *testing.T) {
	refusals := map[string]string{
		"unknown codename":       "ITEM_NOPE plus=1",
		"unknown key":            forgeSwordCodename + " shiny=1",
		"key without value":      forgeSwordCodename + " plus",
		"empty value":            forgeSwordCodename + " plus=",
		"plus past the body":     forgeSwordCodename + " plus=256",
		"plus not a number":      forgeSwordCodename + " plus=abc",
		"duplicate key":          forgeSwordCodename + " plus=1 plus=2",
		"white past 100":         forgeSwordCodename + " white=101",
		"white on a potion":      forgePotionCodename + " white=50",
		"plus on a potion":       forgePotionCodename + " plus=1",
		"count past the stack":   forgePotionCodename + " count=51",
		"count zero":             forgePotionCodename + " count=0",
		"count on a weapon":      forgeSwordCodename + " count=2",
		"blue value not in set":  forgeSwordCodename + " blue.MATTR_STR=7",
		"blue on the wrong body": forgeSwordCodename + " blue.MATTR_INT=10",
		"blue degree unknown":    forgeSwordCodename + " blue.MATTR_NOPE=1",
		"blue twice":             forgeSwordCodename + " blue.MATTR_STR=10 blue.MATTR_STR=15",
		"full then one":          forgeSwordCodename + " blue=full blue.MATTR_STR=10",
		"one then full":          forgeSwordCodename + " blue.MATTR_STR=10 blue=full",
		"durability too high":    forgeSwordCodename + " durability=101",
		"durability zero":        forgeSwordCodename + " durability=0",
		"bound opt-out":          forgeSwordCodename + " bound=0",
		"pet key on a weapon":    forgeSwordCodename + " pet.level=3",
		"pet level too high":     "SUMMON_ATTACK pet.level=141",
		"pet name too long":      "SUMMON_ATTACK pet.name=ThisNameIsTooLong",
		"transform on weapon":    forgeSwordCodename + " transform=MOB_FORGE_TEST",
		"help unknown item":      "help ITEM_NOPE",
		"help with keys":         "help " + forgeSwordCodename + " plus=1",
		"bare forge":             "",
	}
	for name, text := range refusals {
		t.Run(name, func(t *testing.T) {
			c := forgeTestGM()
			rt, _ := forgeTestRuntime(c, forgeTestItems())
			if lines, ok := rt.ForgeGMItem(testDivision, c.Name, text); ok {
				t.Fatalf("forge %q answered %v", text, lines)
			}
			if n := rt.Ground.Count(testDivision); n != 0 {
				t.Fatalf("refused forge left %d ground item(s)", n)
			}
		})
	}
}

/*
================
TestForgeNeedsPrivilegeInsideTheDoor
================
*/
func TestForgeNeedsPrivilegeInsideTheDoor(t *testing.T) {
	c := forgeTestGM()
	c.GMPrivilege = false
	rt, _ := forgeTestRuntime(c, forgeTestItems())
	if _, ok := rt.ForgeGMItem(testDivision, c.Name, forgePotionCodename+" count=1"); ok {
		t.Fatal("a non-GM forged")
	}
	if n := rt.Ground.Count(testDivision); n != 0 {
		t.Fatalf("a non-GM's forge left %d ground item(s)", n)
	}
}

/*
================
TestForgeHelpListsExactlyTheAcceptedKeys

For one item per family: every help line names a key, and every key
in the derived table accepts its canonical value through the real
command - the two can never drift, because both come from
forgeKeysFor.
================
*/
func TestForgeHelpListsExactlyTheAcceptedKeys(t *testing.T) {
	canonical := map[forgeKeyKind]string{
		forgeKeyBound: "1", forgeKeyPlus: "1", forgeKeyWhiteAll: "10",
		forgeKeyWhiteField: "10", forgeKeyBlue: "max", forgeKeyBlueFull: "full",
		forgeKeyDurability: "1", forgeKeyCount: "1", forgeKeyTransform: "MOB_FORGE_TEST",
		forgeKeyPetLevel: "3", forgeKeyPetName: "Fetch",
	}
	for _, codename := range []string{forgeSwordCodename, forgeAvatarCodename, forgePotionCodename, forgeCapsuleCodename, "SUMMON_ATTACK"} {
		t.Run(codename, func(t *testing.T) {
			c := forgeTestGM()
			rt, _ := forgeTestRuntime(c, forgeTestItems())
			lines, ok := rt.ForgeGMItem(testDivision, c.Name, "help "+codename)
			if !ok || len(lines) < 2 {
				t.Fatalf("help = %v, %v", lines, ok)
			}
			ref, found := forgeItemRef(forgeTestItems(), codename)
			if !found {
				t.Fatal("fixture lost its reference")
			}
			builder := rt.newForgeBuilder(ref)
			if len(lines) != len(builder.keys)+1 {
				t.Fatalf("help has %d line(s) for %d key(s): %v", len(lines), len(builder.keys), lines)
			}
			for name, key := range builder.keys {
				value, known := canonical[key.kind]
				if !known {
					t.Fatalf("no canonical value for key %q kind %d", name, key.kind)
				}
				fresh := forgeTestGM()
				freshRT, _ := forgeTestRuntime(fresh, forgeTestItems())
				if _, ok := freshRT.ForgeGMItem(testDivision, fresh.Name, codename+" "+name+"="+value); !ok {
					t.Fatalf("help lists %q but the forge refuses it", name)
				}
			}
		})
	}
}

/*
================
TestForgeRateLimitMetersForgingNotHelp
================
*/
func TestForgeRateLimitMetersForgingNotHelp(t *testing.T) {
	c := forgeTestGM()
	rt, clock := forgeTestRuntime(c, forgeTestItems())
	for i := 0; i < forgeRateMax; i++ {
		if _, ok := rt.ForgeGMItem(testDivision, c.Name, "help "+forgeSwordCodename); !ok {
			t.Fatal("help was metered")
		}
		if _, ok := rt.ForgeGMItem(testDivision, c.Name, forgePotionCodename+" count=1"); !ok {
			t.Fatalf("forge %d refused inside the allowance", i+1)
		}
	}
	if _, ok := rt.ForgeGMItem(testDivision, c.Name, forgePotionCodename+" count=1"); ok {
		t.Fatal("the forge past the rate limit went through")
	}
	clock.Advance(forgeRateWindow + time.Second)
	if _, ok := rt.ForgeGMItem(testDivision, c.Name, forgePotionCodename+" count=1"); !ok {
		t.Fatal("the window did not reopen")
	}
}

/*
================
TestForgeRateLimitIgnoresRefusedSpecs

The allowance meters forged items, not attempts: a window full of
malformed commands leaves the whole allowance intact.
================
*/
func TestForgeRateLimitIgnoresRefusedSpecs(t *testing.T) {
	c := forgeTestGM()
	rt, _ := forgeTestRuntime(c, forgeTestItems())
	for i := 0; i < forgeRateMax; i++ {
		if _, ok := rt.ForgeGMItem(testDivision, c.Name, forgePotionCodename+" count=abc"); ok {
			t.Fatal("a malformed spec forged an item")
		}
	}
	for i := 0; i < forgeRateMax; i++ {
		if _, ok := rt.ForgeGMItem(testDivision, c.Name, forgePotionCodename+" count=1"); !ok {
			t.Fatalf("forge %d refused: refused specs consumed the allowance", i+1)
		}
	}
	if _, ok := rt.ForgeGMItem(testDivision, c.Name, forgePotionCodename+" count=1"); ok {
		t.Fatal("the forge past the rate limit went through")
	}
}

/*
================
TestForgeRoundTripKeepsTheBoundMark

Forge -> ground snapshot/restore (restart) -> pickup (ground to bag)
-> character rows -> JSON (logout) -> rows again: the bound mark and
the forged attributes survive every conversion the item crosses.
================
*/
func TestForgeRoundTripKeepsTheBoundMark(t *testing.T) {
	c := forgeTestGM()
	rt, _ := forgeTestRuntime(c, forgeTestItems())
	if _, ok := rt.ForgeGMItem(testDivision, c.Name, forgeSwordCodename+" plus=9 blue.MATTR_LUCK=4"); !ok {
		t.Fatal("forge refused")
	}
	drop := forgedDrop(t, rt)

	// Restart: the ground registry snapshot restores the same item.
	snapshot := rt.Ground.Snapshot()
	restored := grounditem.NewRegistry()
	restored.Restore(snapshot)
	back := restored.All(testDivision)
	if len(back) != 1 || !back[0].ForgeBound || back[0].Plus != 9 {
		t.Fatalf("restored drop = %+v", back)
	}

	// Pickup conversion (ground -> bag item), then bag -> rows -> JSON.
	item := groundInventoryItem(drop, 1)
	if !item.ForgeBound {
		t.Fatal("pickup dropped the bound mark")
	}
	rows := rowsFromInvItems([]inventory.Item{item})
	if len(rows) != 1 || !rows[0].ForgeBound || rows[0].Plus != 9 {
		t.Fatalf("rows = %+v", rows)
	}
	encoded, err := json.Marshal(rows[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"forgeBound":true`) {
		t.Fatalf("persisted row lacks the bound mark: %s", encoded)
	}
	var decoded domain.InventoryRow
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if !decoded.ForgeBound {
		t.Fatal("the bound mark did not survive the row JSON")
	}
	again := invItemsFromRowsWithin([]enterworld.InventoryRow{decoded}, 200)
	if len(again) != 1 || !again[0].ForgeBound {
		t.Fatalf("reloaded items = %+v", again)
	}
}
