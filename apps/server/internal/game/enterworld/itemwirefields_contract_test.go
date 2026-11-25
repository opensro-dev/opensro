/*
===========================================================================

itemwirefields_contract_test.go - the server's list matches the client's

wireItemFieldNames is checked against itemwirefields.json, the copy
of the client's ITEM_WIRE_FIELDS tuple that
apps/client-next/tools/generate-item-wire-fields.mjs writes (#4448).
The client owns the read set; this side fails when the two drift.
===========================================================================
*/
package enterworld

import (
	"encoding/json"
	"os"
	"sort"
	"testing"
)

/*
================
TestWireItemFieldNamesMatchContract
================
*/
func TestWireItemFieldNamesMatchContract(t *testing.T) {
	blob, err := os.ReadFile("itemwirefields.json")
	if err != nil {
		t.Fatalf("read itemwirefields.json: %v", err)
	}
	var contract []string
	if err := json.Unmarshal(blob, &contract); err != nil {
		t.Fatalf("itemwirefields.json does not parse: %v", err)
	}
	published := append([]string(nil), wireItemFieldNames...)
	sort.Strings(published)
	sort.Strings(contract)
	if len(published) != len(contract) {
		t.Fatalf("wire list has %d fields, contract has %d; regenerate itemwirefields.json", len(published), len(contract))
	}
	for i := range published {
		if published[i] != contract[i] {
			t.Fatalf("wire list and contract differ at %q vs %q; regenerate itemwirefields.json", published[i], contract[i])
		}
	}
}
