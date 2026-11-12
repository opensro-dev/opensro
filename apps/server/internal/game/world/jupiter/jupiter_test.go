/*
===========================================================================

jupiter_test.go - the Jupiter identity table's shape.

===========================================================================
*/
package jupiter

import "testing"

/*
================
TestWorldsAreTheFieldAndTempleSets

The later client's seven Jupiter worlds: the field (90) and the two
Mirror Dimension temple sets (86-89, 93-94), each with the codename
its population rows name.
================
*/
func TestWorldsAreTheFieldAndTempleSets(t *testing.T) {
	worlds := Worlds()
	if len(worlds) != 7 {
		t.Fatalf("jupiter worlds = %d, want 7", len(worlds))
	}
	want := map[int]string{
		86: "GROUP_JUPITER_A", 87: "GROUP_JUPITER_B",
		88: "GROUP_JUPITER_A2", 89: "GROUP_JUPITER_B2",
		90: "GROUP_JUPITER_FIELD",
		93: "GROUP_JUPITER_A3", 94: "GROUP_JUPITER_B3",
	}
	for _, world := range worlds {
		code, ok := want[world.ID]
		if !ok || code != world.CodeName {
			t.Fatalf("world %d codename = %q, want %q", world.ID, world.CodeName, code)
		}
		if got, ok := CodeName(world.ID); !ok || got != world.CodeName {
			t.Fatalf("CodeName(%d) = %q, %t", world.ID, got, ok)
		}
	}
	if _, ok := CodeName(85); ok {
		t.Fatal("world 85 is not a Jupiter world")
	}
	if _, ok := CodeName(91); ok {
		t.Fatal("world 91 is not a Jupiter world")
	}
	field := FieldWorlds()
	if len(field) != 1 || field[0].ID != FieldWorldID || field[0].CodeName != "GROUP_JUPITER_FIELD" {
		t.Fatalf("field worlds = %+v", field)
	}
	temple := TempleWorlds()
	if len(temple) != 6 {
		t.Fatalf("temple worlds = %d, want 6", len(temple))
	}
	for _, world := range temple {
		if world.ID == FieldWorldID {
			t.Fatal("the field world is among the temple worlds")
		}
	}
}
