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
worlds: native type 1 so no band opens at boot, fifty layers, and the
world row's own resident limit (four or eight) per layer - the source
world table's shape for 63-84 (#1800 step 3). Shape only:
registration itself is TestPortOnlyRegistration's subject.
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
		if row.NativeType != 1 || row.LayerLimit != 50 || (row.PlayerLimit != 4 && row.PlayerLimit != 8) {
			t.Fatalf("temple world %d shape = type %d, %d layers, %d players; want type 1, 50, 4 or 8",
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

/*
================
TestJupiterDefinitionsShape

The Jupiter worlds register (enterworld wires the call) as permanent
outdoor worlds: native type 0 so they open at boot, one layer, no
resident limit - the later client's own shape for 86-90 and 93-94
(#3267). The field switch takes the field world alone; the temple
switch takes the six temple worlds. Shape only: registration itself
is TestPortOnlyRegistration's subject.
================
*/
func TestJupiterDefinitionsShape(t *testing.T) {
	field := JupiterFieldDefinitions()
	if len(field) != 1 || field[0].ID != 90 || field[0].CodeName != "GROUP_JUPITER_FIELD" {
		t.Fatalf("jupiter field definitions = %+v", field)
	}
	temple := JupiterTempleDefinitions()
	if len(temple) != 6 {
		t.Fatalf("jupiter temple definitions = %d, want 6", len(temple))
	}
	wantTemple := []int{86, 87, 88, 89, 93, 94}
	for index, row := range append(field, temple...) {
		if index > 0 && int(row.ID) != wantTemple[index-1] {
			t.Fatalf("jupiter temple definition %d has id %d, want %d", index-1, row.ID, wantTemple[index-1])
		}
		if row.NativeType != 0 || row.LayerLimit != 1 || row.PlayerLimit != 0 {
			t.Fatalf("jupiter world %d shape = type %d, %d layers, %d players; want type 0, 1, 0",
				row.ID, row.NativeType, row.LayerLimit, row.PlayerLimit)
		}
		if row.CodeName == "" {
			t.Fatalf("jupiter world %d has no codename", row.ID)
		}
		for _, shipped := range Shipped() {
			if shipped.ID == row.ID || shipped.CodeName == row.CodeName {
				t.Fatalf("jupiter world %d collides with shipped %+v", row.ID, shipped)
			}
		}
	}
}
