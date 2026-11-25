/*
===========================================================================

nativefields_wire_test.go - the wire projection keeps the read set only

Step 2B of #4421: ItemWireFields passes a wire row only the fields a
browser build reads (wireItemFieldNames). These tests pin the two
halves of that contract: a read field is never slimmed away, an unread
field never reaches a wire row - while the packed source value, the
server's own copy, keeps everything.

===========================================================================
*/
package enterworld

import (
	"encoding/json"
	"testing"
)

/*
================
TestItemWireFieldsKeepsTheReadSet
================
*/
func TestItemWireFieldsKeepsTheReadSet(t *testing.T) {
	fields := NewNativeFields(map[string]float64{
		// Read by the client: must pass to the wire.
		"varianceIntMin1c0":      69,
		"price":                  427,
		"itemParam1_29c":         30000,
		"useCooldownDuration528": 5000,
		"associatedSkillId":      1234,
		"stoneTooltipMask":       3,
		"country":                1,
		"reqStr":                 40,
		"reqInt":                 35,
		"itemParam3_2a4":         120,
		"canUse":                 0x2001,
		// Never read by the client: must not pass.
		"canSell":        1,
		"twoHanded":      1,
		"sellPrice":      427,
		"canTrade":       1,
		"itemParam9_2bc": 7,
	})
	wire := ItemWireFields(fields)
	for _, name := range []string{"varianceIntMin1c0", "price", "itemParam1_29c", "useCooldownDuration528", "associatedSkillId", "stoneTooltipMask", "country", "reqStr", "reqInt", "itemParam3_2a4", "canUse"} {
		if _, ok := wire.Lookup(name); !ok {
			t.Errorf("read field %q missing from the wire value", name)
		}
	}
	for _, name := range []string{"canSell", "twoHanded", "sellPrice", "canTrade", "itemParam9_2bc"} {
		if _, ok := wire.Lookup(name); ok {
			t.Errorf("unread field %q passed to the wire value", name)
		}
	}
	// The filter copies: the server's own value keeps everything.
	if v, ok := fields.Lookup("canSell"); !ok || v != 1 {
		t.Errorf("source Lookup(canSell) = %v, %v; want 1, true", v, ok)
	}
	// And the marshalled wire object carries the same slimmed set.
	blob, err := wire.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	var published map[string]float64
	if err := json.Unmarshal(blob, &published); err != nil {
		t.Fatalf("wire object does not parse: %v", err)
	}
	if len(published) != 11 {
		t.Errorf("wire object %s has %d fields, want the 11 read ones", blob, len(published))
	}
}

/*
================
TestItemWireFieldsEmpty

A row whose fields are all unread publishes as "", so its
nativeFields key is omitted from the row entirely.
================
*/
func TestItemWireFieldsEmpty(t *testing.T) {
	if got := ItemWireFields(""); got != "" {
		t.Errorf("ItemWireFields(\"\") = %q, want \"\"", got)
	}
	unreadOnly := NewNativeFields(map[string]float64{"canSell": 1, "twoHanded": 1})
	if got := ItemWireFields(unreadOnly); got != "" {
		t.Errorf("ItemWireFields(unread only) = %q, want \"\"", got)
	}
}

/*
================
TestWireItemFieldNamesResolve

Every published name must be a schema name (the mask builder panics at
init on a typo; this pins it in the test report too), with no
duplicates.
================
*/
func TestWireItemFieldNamesResolve(t *testing.T) {
	seen := map[string]bool{}
	for _, name := range wireItemFieldNames {
		if _, ok := nativeFieldIDs[name]; !ok {
			t.Errorf("wire item field %q is not in the schema", name)
		}
		if seen[name] {
			t.Errorf("wire item field %q listed twice", name)
		}
		seen[name] = true
	}
}
