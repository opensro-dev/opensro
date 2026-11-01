/*
===========================================================================

stacksizes_test.go - SRO_STACK_SIZES parsing and the per-row raise rules

===========================================================================
*/
package enterworld

import (
	"opensro.online/server/internal/testsupport/tier"
	"testing"

	"opensro.online/server/internal/testsupport/clientdata"
	"opensro.online/server/internal/testsupport/gamedatatest"
)

/*
================
TestParseStackSizes
================
*/
func TestParseStackSizes(t *testing.T) {
	for _, text := range []string{"  ", "off", " OFF "} {
		native, err := ParseStackSizes(text)
		if err != nil || len(native) != 0 {
			t.Fatalf("setting %q = %v, %v; want native", text, native, err)
		}
	}
	sizes, err := ParseStackSizes(" Potion=2000 , elixir=50,petpotion=100,luckypowder=65535")
	if err != nil {
		t.Fatal(err)
	}
	if got := sizes.String(); got != "elixir=50,luckypowder=65535,petpotion=100,potion=2000" {
		t.Fatalf("parsed %q", got)
	}
	for _, bad := range []string{
		"stone=50",            // unknown group
		"potion=50,potion=60", // repeated
		"potion=0",
		"potion=65536",
		"potion=lots",
		"potion",
		"=50",
		"potion=50,",
	} {
		if _, err := ParseStackSizes(bad); err == nil {
			t.Errorf("%q parsed; want an error", bad)
		}
	}
}

/*
================
stackRef
================
*/
func stackRef(typeID3, typeID4 int64, cap float64) *ItemRef {
	return &ItemRef{
		TypeIDs:      [4]int64{3, 3, typeID3, typeID4},
		NativeFields: NewNativeFields(map[string]float64{"maxStack": cap}),
	}
}

/*
================
TestStackSizesRaiseOnlyTheirGroups
================
*/
func TestStackSizesRaiseOnlyTheirGroups(t *testing.T) {
	sizes := StackSizes{"potion": 2000, "petpotion": 500, "universalpill": 500, "elixir": 50, "luckypowder": 100, "magicstone": 40, "attrstone": 30}
	for _, c := range []struct {
		name   string
		ref    *ItemRef
		raised bool
		cap    float64
		native int64
	}{
		{"hp potion", stackRef(1, 1, 50), true, 2000, 50},
		{"mp potion", stackRef(1, 2, 50), true, 2000, 50},
		{"vigor potion", stackRef(1, 3, 50), true, 2000, 50},
		{"pet hp potion", stackRef(1, 4, 50), true, 500, 50},
		{"pet hunger potion", stackRef(1, 9, 50), true, 500, 50},
		{"universal pill", stackRef(2, 6, 50), true, 500, 50},
		// Other cures (purification pills, 3/3/2/1) are not universal pills.
		{"purification pill", stackRef(2, 1, 50), false, 50, 50},
		{"elixir", stackRef(10, 1, 1), true, 50, 1},
		{"lucky powder", stackRef(10, 2, 50), true, 100, 50},
		// A single item in a potion family keeps its cap.
		{"single potion", stackRef(1, 3, 1), false, 1, 1},
		// Above a native cap the override raises it, mall bags included.
		{"mall potion bag", stackRef(1, 1, 1000), true, 2000, 1000},
		{"magic stone", stackRef(11, 1, 1), true, 40, 1},
		{"athanasia stone", stackRef(11, 7, 1), true, 40, 1},
		{"attribute stone", stackRef(11, 2, 1), true, 30, 1},
		// Tablets, materials and elements already stack; no group names them.
		{"magic tablet", stackRef(11, 3, 20), false, 20, 20},
		{"pet scroll", stackRef(1, 6, 50), false, 50, 50},
		{"equipment", &ItemRef{TypeIDs: [4]int64{3, 1, 6, 2}}, false, 0, 0},
	} {
		if got := sizes.Raise(c.ref); got != c.raised {
			t.Errorf("%s: raised = %v, want %v", c.name, got, c.raised)
		}
		if got := c.ref.NativeFields.Get("maxStack"); got != c.cap {
			t.Errorf("%s: cap = %v, want %v", c.name, got, c.cap)
		}
		if got := c.ref.NativeStackCap(); got != c.native {
			t.Errorf("%s: native cap = %d, want %d", c.name, got, c.native)
		}
	}
	ref := stackRef(1, 1, 50)
	if (StackSizes{}).Raise(ref) || ref.NativeFields.Get("maxStack") != 50 || ref.NativeMaxStack != 0 {
		t.Fatal("the native setting changed a row")
	}
	// An override never lowers: a 1000-stack mall bag keeps its cap.
	bag := stackRef(1, 1, 1000)
	if (StackSizes{"potion": 500}).Raise(bag) || bag.NativeFields.Get("maxStack") != 1000 || bag.NativeMaxStack != 0 {
		t.Fatal("an override lowered a native cap")
	}
}

/*
================
TestShippedStackSizesReachEveryReader

On the shipped itemdata: the raise survives the bounded cache's JSON
round trip, reaches the published command references, and the beta
potion refill still tops up to the native 50.
================
*/
func TestShippedStackSizesReachEveryReader(t *testing.T) {
	tier.Long(t, "stack-size raises reach every reader of the shipped itemdata")
	clientdata.RequireGameData(t)
	items := NewTextdataItems(gamedatatest.TextdataDir(t))
	raised, err := items.ApplyStackSizes(StackSizes{"potion": 2000, "elixir": 50, "universalpill": 500})
	if err != nil {
		t.Fatal(err)
	}
	if raised < 40 {
		t.Fatalf("raised %d rows; want the HP/MP/vigor potions and the eight elixirs", raised)
	}
	if err := items.UseBoundedCache(16); err != nil {
		t.Fatal(err)
	}
	defer items.Close()
	if _, err := items.ApplyStackSizes(StackSizes{"potion": 100}); err == nil {
		t.Fatal("a raise after archiving was accepted")
	}
	for _, c := range []struct {
		codename    string
		cap, native int64
	}{
		{"ITEM_ETC_HP_POTION_01", 2000, 50},
		{"ITEM_ETC_MP_POTION_05", 2000, 50},
		{"ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_WEAPON_A", 50, 1},
		{"ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_ACCESSARY_B", 50, 1},
		{"ITEM_MALL_HP_SUPERSET_2_BAG", 2000, 1000},
		{"ITEM_ETC_GNGWC_ALL_POTION_100", 1, 1},
		{"ITEM_ETC_ARCHEMY_MAGICSTONE_STR_01", 1, 1},
		{"ITEM_ETC_COS_HP_POTION_01", 50, 50},
		{"ITEM_ETC_CURE_ALL_01", 500, 50},
		{"ITEM_ETC_CURE_ALL_04", 500, 50},
	} {
		ref, ok := items.ItemRefByCodename(c.codename)
		if !ok {
			t.Fatalf("%s missing from shipped itemdata", c.codename)
		}
		if got := int64(ref.NativeFields.Get("maxStack")); got != c.cap || ref.NativeStackCap() != c.native {
			t.Errorf("%s: cap %d native %d, want %d native %d", c.codename, got, ref.NativeStackCap(), c.cap, c.native)
		}
	}
	commands := map[string]uint16{}
	for _, row := range items.ItemCommandReferences() {
		commands[row.Codename] = row.MaxStack
	}
	if commands["ITEM_ETC_HP_POTION_01"] != 2000 || commands["ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_WEAPON_A"] != 50 {
		t.Fatalf("command references publish %d and %d", commands["ITEM_ETC_HP_POTION_01"], commands["ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_WEAPON_A"])
	}
	refills := ResolveStarterRefills(items)
	if len(refills) != 2 {
		t.Fatalf("refill families = %d", len(refills))
	}
	for _, refill := range refills {
		for _, stack := range refill.Stack {
			if stack != 50 {
				t.Fatalf("refill stack %d; the beta refill must keep the native 50", stack)
			}
		}
	}
}

/*
================
TestShippedLuckyPowderStacksTo500

The F7 deployment default (luckypowder=500, owner 2026-10-13) on the
shipped itemdata: every Lucky Powder row (3/3/10/2) that stacks rises to
500, degree 9 A and B included; a row already above 500 keeps its own cap,
so no stored stack or mall bag exceeds its row; nothing else changes.
================
*/
func TestShippedLuckyPowderStacksTo500(t *testing.T) {
	clientdata.RequireGameData(t)
	dir := gamedatatest.TextdataDir(t)
	native := NewTextdataItems(dir)
	items := NewTextdataItems(dir)
	if _, err := items.ApplyStackSizes(StackSizes{"luckypowder": 500}); err != nil {
		t.Fatal(err)
	}
	powders := 0
	for _, row := range items.ItemCommandReferences() {
		ref, _ := items.ItemRefByCodename(row.Codename)
		before, _ := native.ItemRefByCodename(row.Codename)
		was := before.NativeFields.Get("maxStack")
		want := was
		if ref.TypeIDs == [4]int64{3, 3, 10, 2} && was > 1 && was < 500 {
			want = 500
			powders++
		}
		if got := ref.NativeFields.Get("maxStack"); got != want {
			t.Errorf("%s: cap %v, want %v (native %v)", row.Codename, got, want, was)
		}
	}
	for _, codename := range []string{"ITEM_ETC_ARCHEMY_REINFORCE_PROB_UP_A_09", "ITEM_ETC_ARCHEMY_REINFORCE_PROB_UP_B_09"} {
		if ref, ok := items.ItemRefByCodename(codename); !ok || ref.NativeFields.Get("maxStack") != 500 {
			t.Fatalf("%s does not stack to 500", codename)
		}
	}
	if powders < 2 {
		t.Fatalf("raised %d Lucky Powder rows", powders)
	}
}

/*
================
TestShippedAmmunitionStacksTo2500

The deployment default (owner 2026-10-14) on the shipped itemdata: every
arrow (3/3/4/1) and bolt (3/3/4/2) row at the native 250, the starter _DEF
rows included, rises to 2500. The Item Mall quivers (ITEM_MALL_QUIVER,
ITEM_MALL_BOLT) already stack 10000 natively and keep it: the override
only raises. Every other row keeps its cap.
================
*/
func TestShippedAmmunitionStacksTo2500(t *testing.T) {
	clientdata.RequireGameData(t)
	dir := gamedatatest.TextdataDir(t)
	native := NewTextdataItems(dir)
	items := NewTextdataItems(dir)
	if _, err := items.ApplyStackSizes(StackSizes{"arrow": 2500, "bolt": 2500}); err != nil {
		t.Fatal(err)
	}
	raised := 0
	for _, row := range items.ItemCommandReferences() {
		ref, _ := items.ItemRefByCodename(row.Codename)
		before, _ := native.ItemRefByCodename(row.Codename)
		was := before.NativeFields.Get("maxStack")
		want := was
		if (ref.TypeIDs == [4]int64{3, 3, 4, 1} || ref.TypeIDs == [4]int64{3, 3, 4, 2}) && was < 2500 {
			if was != 250 {
				t.Errorf("%s: native ammunition cap %v, want 250", row.Codename, was)
			}
			want = 2500
			raised++
		}
		if got := ref.NativeFields.Get("maxStack"); got != want {
			t.Errorf("%s: cap %v, want %v (native %v)", row.Codename, got, want, was)
		}
	}
	for _, codename := range []string{"ITEM_ETC_AMMO_ARROW_01", "ITEM_ETC_AMMO_ARROW_01_DEF", "ITEM_ETC_AMMO_BOLT_01", "ITEM_ETC_AMMO_BOLT_01_DEF"} {
		if ref, ok := items.ItemRefByCodename(codename); !ok || ref.NativeFields.Get("maxStack") != 2500 {
			t.Fatalf("%s does not stack to 2500", codename)
		}
	}
	if raised != 4 {
		t.Fatalf("raised %d ammunition rows, want 4", raised)
	}
	for _, codename := range []string{"ITEM_MALL_QUIVER", "ITEM_MALL_BOLT"} {
		if ref, ok := items.ItemRefByCodename(codename); !ok || ref.NativeFields.Get("maxStack") != 10000 {
			t.Fatalf("%s lost its native 10000 cap", codename)
		}
	}
}
