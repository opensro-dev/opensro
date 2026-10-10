/*
===========================================================================

stacksizes_test.go - SRO_STACK_SIZES parsing and the per-row raise rules

===========================================================================
*/
package enterworld

import (
	"testing"

	"opensro.online/server/internal/testsupport/gamedatatest"
	"opensro.online/server/internal/testsupport/licensed"
)

/*
================
TestParseStackSizes
================
*/
func TestParseStackSizes(t *testing.T) {
	native, err := ParseStackSizes("  ")
	if err != nil || len(native) != 0 {
		t.Fatalf("empty setting = %v, %v; want native", native, err)
	}
	sizes, err := ParseStackSizes(" Potion=2000 , elixir=50,petpotion=100,luckypowder=65535")
	if err != nil {
		t.Fatal(err)
	}
	if got := sizes.String(); got != "elixir=50,luckypowder=65535,petpotion=100,potion=2000" {
		t.Fatalf("parsed %q", got)
	}
	for _, bad := range []string{
		"stone=50",            // unknown group (stones wait for #583)
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
	sizes := StackSizes{"potion": 2000, "petpotion": 500, "elixir": 50, "luckypowder": 100}
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
		{"elixir", stackRef(10, 1, 1), true, 50, 1},
		{"lucky powder", stackRef(10, 2, 50), true, 100, 50},
		// A single item in a potion family keeps its cap.
		{"single potion", stackRef(1, 3, 1), false, 1, 1},
		// Above a native cap the override raises it, mall bags included.
		{"mall potion bag", stackRef(1, 1, 1000), true, 2000, 1000},
		// Stones are out of scope until their merge identity lands (#583).
		{"magic stone", stackRef(11, 1, 1), false, 1, 1},
		{"attribute stone", stackRef(11, 2, 1), false, 1, 1},
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
	licensed.RequireGameData(t)
	items := NewTextdataItems(gamedatatest.TextdataDir(t))
	raised, err := items.ApplyStackSizes(StackSizes{"potion": 2000, "elixir": 50})
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
