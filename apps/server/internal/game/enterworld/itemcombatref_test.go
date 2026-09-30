/*
===========================================================================

itemcombatref_test.go - complete reinforcement input admission

Keep malformed table cells from silently disabling one equipment coefficient.
The production parser and every combat consumer share this typed reference.

===========================================================================
*/
package enterworld

import "testing"

/*
================
TestItemCombatReferenceRequiresEveryReinforcementColumn
================
*/
func TestItemCombatReferenceRequiresEveryReinforcementColumn(t *testing.T) {
	const columns = 118
	fields := make([]string, columns)
	for i := range fields {
		fields[i] = "0"
	}
	for _, column := range []int{82, 83, 84, 85, 105, 106, 107, 108, 109, 110, 111, 112} {
		fields[column] = "invalid"
		if buildItemCombatRef(fields) != nil {
			t.Fatalf("invalid reinforcement column %d was accepted", column)
		}
		fields[column] = "0"
	}
	fields[82], fields[83], fields[105], fields[106] = "125", "250", "816", "900"
	ref := buildItemCombatRef(fields)
	if ref == nil {
		t.Fatal("complete numeric reference refused")
	}
	if ref.PhysicalDefenseReinforcement.Min != .125 || ref.PhysicalDefenseReinforcement.Max != .25 || ref.PhysicalReinforcement.Minimum.Min != float64(float32(.816)) || ref.PhysicalReinforcement.Minimum.Max != float64(float32(.9)) {
		t.Fatalf("reinforcement units or float32 row parse changed: %+v", ref)
	}
}
