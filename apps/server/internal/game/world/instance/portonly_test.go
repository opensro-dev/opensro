/*
===========================================================================

portonly_test.go - port-only world registration behind its switch.

===========================================================================
*/
package instance

import "testing"

/*
================
TestPortOnlyRegistration

The shipped catalogue never grows (41 rows, pinned in catalog_test.go);
port-only worlds join Lookup and Definitions only once registered, an
identical re-registration is a no-op, and a colliding or conflicting
row is refused.
================
*/
func TestPortOnlyRegistration(t *testing.T) {
	if _, ok := Lookup(PharaohTomb1ID); ok {
		t.Fatal("tomb world visible before registration")
	}
	if len(Definitions()) != 41 {
		t.Fatalf("catalogue has %d rows before registration, want the shipped 41", len(Definitions()))
	}
	definitions := PharaohTombDefinitions()
	if err := RegisterPortOnly(definitions); err != nil {
		t.Fatal(err)
	}
	if err := RegisterPortOnly(definitions); err != nil {
		t.Fatalf("identical re-registration refused: %v", err)
	}
	for _, want := range definitions {
		row, ok := Lookup(want.ID)
		if !ok || row != want {
			t.Fatalf("world %d lookup = %+v, %v; want %+v", want.ID, row, ok, want)
		}
	}
	all := Definitions()
	if len(all) != 44 || all[41] != definitions[0] || all[43] != definitions[2] {
		t.Fatalf("catalogue after registration = %d rows, tombs not appended in order", len(all))
	}
	if len(Shipped()) != 41 {
		t.Fatal("shipped catalogue grew")
	}
	conflicting := definitions[0]
	conflicting.CodeName = "INS_SOMETHING_ELSE"
	if err := RegisterPortOnly([]Definition{conflicting}); err == nil {
		t.Fatal("conflicting re-registration admitted")
	}
	if err := RegisterPortOnly([]Definition{{ID: 1, CodeName: "INS_COLLIDE"}}); err == nil {
		t.Fatal("shipped collision admitted")
	}
	if err := RegisterPortOnly([]Definition{{ID: 0, CodeName: "INS_NO_ID"}}); err == nil {
		t.Fatal("id-less world admitted")
	}
}

/*
================
TestTempleDefinitionsShape

The temple bands register (slice 2 wires the call) as instanced
worlds: native type 1 so no band opens at boot, a hundred layers, at
most eight residents a layer - the shipped quest instances' shape
with the owner settlement's per-layer limit (#1800 step 3). Shape
only: registration itself is TestPortOnlyRegistration's subject.
================
*/
func TestTempleDefinitionsShape(t *testing.T) {
	definitions := TempleDefinitions()
	if len(definitions) != 22 {
		t.Fatalf("temple definitions = %d, want 22", len(definitions))
	}
	for index, row := range definitions {
		if int(row.ID) != 63+index {
			t.Fatalf("temple definition %d has id %d, want %d", index, row.ID, 63+index)
		}
		if row.NativeType != 1 || row.LayerLimit != 100 || row.PlayerLimit != 8 {
			t.Fatalf("temple world %d shape = type %d, %d layers, %d players; want type 1, 100, 8",
				row.ID, row.NativeType, row.LayerLimit, row.PlayerLimit)
		}
		if row.CodeName == "" {
			t.Fatalf("temple world %d has no codename", row.ID)
		}
		for _, shipped := range Shipped() {
			if shipped.ID == row.ID || shipped.CodeName == row.CodeName {
				t.Fatalf("temple world %d collides with shipped %+v", row.ID, shipped)
			}
		}
	}
}
