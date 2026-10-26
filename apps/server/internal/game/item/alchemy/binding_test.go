/*
===========================================================================

binding_test.go - bound rewards survive reinforcement and refuse dissolution

===========================================================================
*/
package alchemy

import (
	"reflect"
	"testing"
)

/*
================
TestBoundWeaponReinforcementPreservesBinding
================
*/
func TestBoundWeaponReinforcementPreservesBinding(t *testing.T) {
	catalog, items := fixture()
	items[0].CharacterBound = true
	result, err := catalog.Reinforce(items, []uint8{13, 14}, 0, sequence(t, 0))
	if err != nil {
		t.Fatal(err)
	}
	if !result.Success || !result.Items[0].CharacterBound || result.Items[0].Plus != 1 {
		t.Fatalf("reinforcement lost binding: %+v", result)
	}
}

/*
================
TestBoundWeaponCannotDissolveIntoTradableMaterials
================
*/
func TestBoundWeaponCannotDissolveIntoTradableMaterials(t *testing.T) {
	catalog, items := dissolveProfileFixture(t)
	items[0].CharacterBound = true
	before := clone(items)
	result, err := catalog.Dissolve(items, ProcessRequest{Mode: 3, Quantity: 1, Slots: []uint8{13, 14}}, sequence(t))
	if err == nil || len(result.Items) != 0 || !reflect.DeepEqual(before, items) {
		t.Fatalf("bound dissolution admitted: %+v, %v", result, err)
	}
}
