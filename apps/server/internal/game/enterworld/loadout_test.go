/*
===========================================================================

loadout_test.go - creation rules, starter items and the item loadout

The loadout mirrors the native character-list record: the model, the worn
items in socket order and the avatar items. Creation grants the _DEF starter
items of the native weapon and protector tables (Europe choice 6 is the
darkstaff, robe only).

===========================================================================
*/

package enterworld

import (
	"encoding/json"
	"reflect"
	"testing"
)

/*
================
i64
================
*/
func i64(v int64) *int64 { return &v }

/*
================
f64
================
*/
func f64(v float64) *float64 { return &v }

/*
================
testRoster

The slice of roster.json the loadout reads: model rows.
================
*/
func testRoster() *Roster {
	return &Roster{
		Format:  characterAuthorityFormat,
		Version: characterAuthorityVersion,
		Models: []RosterModel{
			{Codename: "CHAR_CH_MAN_ADVENTURER", RefObjID: 1907, BodyRadius: 4},
			{Codename: "CHAR_CH_WOMAN_ADVENTURER", RefObjID: 1920, BodyRadius: 4},
			{Codename: "CHAR_EU_MAN_ADVENTURER", RefObjID: 14726, BodyRadius: 4},
		},
	}
}

/*
================
chinaSpearman

A China male created with the spear (chinaWeaponKinds[3]) and no armor.
================
*/
func chinaSpearman() *Character {
	return &Character{
		Name:           "asd2",
		RaceIndex:      i64(RaceChina),
		Gender:         i64(GenderMale),
		ModelCodename:  "CHAR_CH_MAN_ADVENTURER",
		ModelRef:       i64(1907),
		WeaponSelected: true,
		WeaponIndex:    i64(3),
	}
}

/*
================
europeWarlock

A Europe female created with the darkstaff (choice 6) and the robe.
================
*/
func europeWarlock() *Character {
	return &Character{
		Name:           "warlock",
		RaceIndex:      i64(RaceEurope),
		Gender:         i64(GenderFemale),
		ModelCodename:  "CHAR_EU_WOMAN_ADVENTURER",
		WeaponSelected: true,
		WeaponIndex:    i64(6),
		ArmorSelected:  true,
		ProtectorIndex: i64(1),
	}
}

/*
================
TestCharacterCreationValidUsesTheAuthoredSelectionTables
================
*/
func TestCharacterCreationValidUsesTheAuthoredSelectionTables(t *testing.T) {
	base := Character{
		ModelCodename:  "CHAR_CH_MAN_ADVENTURER",
		HeightIndex:    i64(0),
		VolumeIndex:    i64(0),
		WeaponIndex:    i64(3),
		ProtectorIndex: i64(0),
		WeaponSelected: true,
	}
	roster := testRoster()
	europe := func(weapon, protector int64) func(*Character) {
		return func(c *Character) {
			c.ModelCodename = "CHAR_EU_MAN_ADVENTURER"
			c.WeaponIndex = i64(weapon)
			c.ArmorSelected = protector > 0
			c.ProtectorIndex = i64(protector)
		}
	}

	cases := []struct {
		name      string
		mutate    func(*Character)
		wantValid bool
	}{
		{name: "china without protector", wantValid: true},
		{name: "unknown model", mutate: func(c *Character) { c.ModelCodename = "CHAR_UNKNOWN" }},
		{name: "height outside slider", mutate: func(c *Character) { c.HeightIndex = i64(5) }},
		{name: "weapon zero", mutate: func(c *Character) { c.WeaponIndex = i64(0) }},
		{name: "selection flag mismatch", mutate: func(c *Character) { c.ArmorSelected = true }},
		{name: "china protector three", mutate: func(c *Character) {
			c.ArmorSelected = true
			c.ProtectorIndex = i64(3)
		}, wantValid: true},
		{name: "china protector four", mutate: func(c *Character) {
			c.ArmorSelected = true
			c.ProtectorIndex = i64(4)
		}},
		{name: "europe dagger light", mutate: europe(1, 1), wantValid: true},
		{name: "europe dagger has no second protector", mutate: europe(1, 2)},
		{name: "europe requires protector", mutate: europe(2, 0)},
		{name: "europe darkstaff robe", mutate: europe(6, 1), wantValid: true},
		{name: "europe darkstaff has only the robe", mutate: europe(6, 2)},
		{name: "europe staff light or robe", mutate: europe(9, 2), wantValid: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			character := base
			if tc.mutate != nil {
				tc.mutate(&character)
			}
			if got := CharacterCreationValid(&character, roster); got != tc.wantValid {
				t.Fatalf("CharacterCreationValid() = %v, want %v for %+v", got, tc.wantValid, character)
			}
		})
	}
}

/*
================
TestResolveWeaponKindTables
================
*/
func TestResolveWeaponKindTables(t *testing.T) {
	cases := []struct {
		name      string
		character *Character
		want      string
	}{
		{"notSelected", &Character{RaceIndex: i64(RaceChina), WeaponIndex: i64(2)}, ""},
		{"chinaIndex0", &Character{WeaponSelected: true, RaceIndex: i64(RaceChina), WeaponIndex: i64(0)}, ""},
		{"chinaBlade", &Character{WeaponSelected: true, RaceIndex: i64(RaceChina), WeaponIndex: i64(2)}, "BLADE"},
		{"chinaBow", &Character{WeaponSelected: true, RaceIndex: i64(RaceChina), WeaponIndex: i64(5)}, "BOW"},
		{"chinaClampHigh", &Character{WeaponSelected: true, RaceIndex: i64(RaceChina), WeaponIndex: i64(99)}, "BOW"},
		{"europeDagger", &Character{WeaponSelected: true, RaceIndex: i64(RaceEurope), WeaponIndex: i64(1)}, "DAGGER"},
		{"europeDarkstaff", &Character{WeaponSelected: true, RaceIndex: i64(RaceEurope), WeaponIndex: i64(6)}, "DARKSTAFF"},
		{"europeHarp", &Character{WeaponSelected: true, RaceIndex: i64(RaceEurope), WeaponIndex: i64(8)}, "HARP"},
		{"europeIndex9Staff", &Character{WeaponSelected: true, RaceIndex: i64(RaceEurope), WeaponIndex: i64(9)}, "STAFF"},
		{"europeAbsentIndex", &Character{WeaponSelected: true, RaceIndex: i64(RaceEurope)}, ""},
	}
	for _, tc := range cases {
		if got := ResolveWeaponKind(tc.character); got != tc.want {
			t.Errorf("%s: kind = %q, want %q", tc.name, got, tc.want)
		}
	}
}

/*
================
TestResolveProtectorKindTables

Europe follows the per-weapon native list; a protector outside it is none.
================
*/
func TestResolveProtectorKindTables(t *testing.T) {
	cases := []struct {
		name      string
		character *Character
		want      string
	}{
		{"noArmor", &Character{RaceIndex: i64(RaceChina), ProtectorIndex: i64(1)}, ""},
		{"chinaHeavy", &Character{ArmorSelected: true, RaceIndex: i64(RaceChina), ProtectorIndex: i64(1)}, "HEAVY"},
		{"chinaLight", &Character{ArmorSelected: true, RaceIndex: i64(RaceChina), ProtectorIndex: i64(2)}, "LIGHT"},
		{"chinaClothes", &Character{ArmorSelected: true, RaceIndex: i64(RaceChina), ProtectorIndex: i64(3)}, "CLOTHES"},
		{"chinaIndex4", &Character{ArmorSelected: true, RaceIndex: i64(RaceChina), ProtectorIndex: i64(4)}, ""},
		{"chinaIndex0", &Character{ArmorSelected: true, RaceIndex: i64(RaceChina), ProtectorIndex: i64(0)}, ""},
		{"europeSwordHeavy", &Character{ArmorSelected: true, RaceIndex: i64(RaceEurope), WeaponIndex: i64(2), ProtectorIndex: i64(1)}, "HEAVY"},
		{"europeSwordLight", &Character{ArmorSelected: true, RaceIndex: i64(RaceEurope), WeaponIndex: i64(2), ProtectorIndex: i64(2)}, "LIGHT"},
		{"europeSwordOutOfRange", &Character{ArmorSelected: true, RaceIndex: i64(RaceEurope), WeaponIndex: i64(2), ProtectorIndex: i64(3)}, ""},
		{"europeDarkstaffRobe", &Character{ArmorSelected: true, RaceIndex: i64(RaceEurope), WeaponIndex: i64(6), ProtectorIndex: i64(1)}, "CLOTHES"},
		{"europeTstaffClothes", &Character{ArmorSelected: true, RaceIndex: i64(RaceEurope), WeaponIndex: i64(7), ProtectorIndex: i64(1)}, "CLOTHES"},
	}
	for _, tc := range cases {
		if got := ResolveProtectorKind(tc.character); got != tc.want {
			t.Errorf("%s: protector = %q, want %q", tc.name, got, tc.want)
		}
	}
}

/*
================
TestCreationStarterItems
================
*/
func TestCreationStarterItems(t *testing.T) {
	cases := []struct {
		name      string
		character *Character
		want      []StarterItem
	}{
		{"chinaSpearNoArmor", chinaSpearman(), []StarterItem{
			{Codename: "ITEM_CH_SPEAR_01_A_DEF", Slot: slotWeapon},
		}},
		{"europeDarkstaffRobe", europeWarlock(), []StarterItem{
			{Codename: "ITEM_EU_W_CLOTHES_01_BA_A_DEF", Slot: slotChest},
			{Codename: "ITEM_EU_W_CLOTHES_01_LA_A_DEF", Slot: slotLegs},
			{Codename: "ITEM_EU_W_CLOTHES_01_FA_A_DEF", Slot: slotFeet},
			{Codename: "ITEM_EU_DARKSTAFF_01_A_DEF", Slot: slotWeapon},
		}},
		{"europeStaffShield", &Character{
			RaceIndex: i64(RaceEurope), Gender: i64(GenderMale), ModelCodename: "CHAR_EU_MAN_ADVENTURER",
			WeaponSelected: true, WeaponIndex: i64(9), ArmorSelected: true, ProtectorIndex: i64(1),
		}, []StarterItem{
			{Codename: "ITEM_EU_M_LIGHT_01_BA_A_DEF", Slot: slotChest},
			{Codename: "ITEM_EU_M_LIGHT_01_LA_A_DEF", Slot: slotLegs},
			{Codename: "ITEM_EU_M_LIGHT_01_FA_A_DEF", Slot: slotFeet},
			{Codename: "ITEM_EU_STAFF_01_A_DEF", Slot: slotWeapon},
			{Codename: "ITEM_EU_SHIELD_01_A_DEF", Slot: slotShield},
		}},
	}
	for _, tc := range cases {
		if got := CreationStarterItems(tc.character, tc.character.ModelCodename); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: starter items = %+v, want %+v", tc.name, got, tc.want)
		}
	}
}

/*
================
TestNativeWeaponAnimationSetNameForTypeFlags

Class 0x0a (darkstaff) shares the one-hand staff set.
================
*/
func TestNativeWeaponAnimationSetNameForTypeFlags(t *testing.T) {
	cases := map[uint16]string{
		2: "sword", 3: "sword", 4: "spear", 5: "spear", 6: "bow",
		7: "onehand_sword", 8: "twohand_sword", 9: "dual_axe",
		10: "onehand_staff", 11: "twohand_staff", 12: "bow", 13: "dagger", 14: "harf",
		15: "onehand_staff",
	}
	for tid4, want := range cases {
		if got := NativeWeaponAnimationSetNameForTypeFlags(tid4 << 11); got != want {
			t.Errorf("tid4 %d animation set = %q, want %q", tid4, got, want)
		}
	}
	if got := NativeWeaponAnimationSetNameForTypeFlags(0); got != "" {
		t.Fatalf("non-weapon animation set = %q, want unresolved", got)
	}
}

/*
================
TestVisualLoadoutFromInventory

Worn sockets 0..8 in socket order, avatar rows, and the worn weapon's type
word as the animation set; inventory rows past socket 8 never show.
================
*/
func TestVisualLoadoutFromInventory(t *testing.T) {
	character := chinaSpearman()
	character.MissionInventory = []InventoryRow{
		{Slot: 13, RefObjID: 9001},
		{Slot: 6, RefObjID: 107, TypeFlags: 3 << 11, Plus: 4},
		{Slot: 1, RefObjID: 11},
		{Slot: 4, RefObjID: 12},
	}
	character.AvatarInventory = &AvatarInventory{Rows: []InventoryRow{{Slot: 0, RefObjID: 23401}}}
	loadout := ResolveVisualLoadout(character, testRoster(), 1907)
	want := VisualLoadout{
		ModelCodename:    "CHAR_CH_MAN_ADVENTURER",
		Items:            []VisualItem{{RefObjID: 11}, {RefObjID: 12}, {RefObjID: 107, Plus: 4}},
		Avatars:          []VisualItem{{RefObjID: 23401}},
		AnimationSetName: "sword",
		HeightScale:      loadout.HeightScale,
		VolumeScale:      loadout.VolumeScale,
	}
	if !reflect.DeepEqual(loadout, want) {
		t.Fatalf("loadout = %+v, want %+v", loadout, want)
	}
}

/*
================
TestFirstBootstrapLoadoutUsesTheCreationWeaponSet

Before the inventory is seeded the loadout has no items and animates with
the creation weapon.
================
*/
func TestFirstBootstrapLoadoutUsesTheCreationWeaponSet(t *testing.T) {
	loadout := ResolveVisualLoadout(europeWarlock(), testRoster(), 0)
	if len(loadout.Items) != 0 || loadout.AnimationSetName != "onehand_staff" {
		t.Fatalf("loadout = %+v, want no items and the darkstaff (one-hand staff) set", loadout)
	}
}

/*
================
TestVisualLoadoutJSONContract

The exact field names the browser reads, with item lists as arrays.
================
*/
func TestVisualLoadoutJSONContract(t *testing.T) {
	character := chinaSpearman()
	character.MissionInventory = []InventoryRow{}
	data, err := json.Marshal(ResolveVisualLoadout(character, testRoster(), 1907))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	want := []string{"modelCodename", "items", "avatars", "animationSetName", "heightScale", "volumeScale"}
	if len(fields) != len(want) {
		t.Fatalf("payload fields = %s, want exactly %v", data, want)
	}
	for _, key := range want {
		if _, ok := fields[key]; !ok {
			t.Errorf("payload lacks %q (have %s)", key, data)
		}
	}
	if string(fields["items"]) != "[]" || string(fields["avatars"]) != "[]" {
		t.Errorf("item lists encode as %s / %s, want [] (never null)", fields["items"], fields["avatars"])
	}
}
