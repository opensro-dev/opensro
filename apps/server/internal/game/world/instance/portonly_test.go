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
