/*
===========================================================================

fortress_upgrades_test.go - the bootstrap's siege structure upgrade rows

===========================================================================
*/
package enterworld

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

/*
================
structureUpgradeFixture

A codename book for the loader: the mapped reference id, or absent.
================
*/
type structureUpgradeFixture map[string]uint32

/*
================
structureUpgradeFixture.CharacterRefByCodename
================
*/
func (f structureUpgradeFixture) CharacterRefByCodename(codename string) (*CharacterRef, bool) {
	id, ok := f[codename]
	if !ok {
		return nil, false
	}
	return &CharacterRef{RefObjID: id, Codename: codename}, true
}

/*
================
structureUpgradeFixture.SummonableCharacterRefs
================
*/
func (f structureUpgradeFixture) SummonableCharacterRefs() []CharacterRef { return nil }

/*
================
TestSiegeStructUpgradeRowsResolveTargets

The enabled rows resolve to reference ids in table order: the target
list stops at the first "xxx", an unresolvable target stays 0 in place,
and a row whose structure or targets do not resolve drops out.
================
*/
func TestSiegeStructUpgradeRowsResolveTargets(t *testing.T) {
	dir := t.TempDir()
	body := strings.Join([]string{
		"1\tSTRUCT_A\tSTRUCT_A\tSTRUCT_B\tSTRUCT_C\txxx\tSTRUCT_D",
		"1\tSTRUCT_B\tSTRUCT_B\tSTRUCT_C",
		"0\tSTRUCT_OFF\tSTRUCT_OFF\tSTRUCT_B",
		"1\tSTRUCT_GONE\tSTRUCT_GONE\tSTRUCT_B",
		"1\tSTRUCT_EMPTY\tSTRUCT_EMPTY\txxx",
		"1\tSTRUCT_MISSING_TARGET\tSTRUCT_MISSING_TARGET\tSTRUCT_NOPE\tSTRUCT_B",
	}, "\n")
	if err := os.WriteFile(filepath.Join(dir, structureUpgradeFile), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	source := structureUpgradeFixture{
		"STRUCT_A":              100,
		"STRUCT_B":              101,
		"STRUCT_C":              102,
		"STRUCT_D":              103,
		"STRUCT_EMPTY":          104,
		"STRUCT_MISSING_TARGET": 105,
	}
	rows := siegeStructureUpgradeRows(dir, source)
	want := []SiegeStructUpgradeRow{
		{Ref: 100, Targets: []uint32{101, 102}},
		{Ref: 101, Targets: []uint32{102}},
		{Ref: 105, Targets: []uint32{0, 101}},
	}
	if !reflect.DeepEqual(rows, want) {
		t.Fatalf("rows = %+v, want %+v", rows, want)
	}
}

/*
================
TestSiegeStructUpgradeRowsMissingTable

No table, no directory or no reference book leaves the bootstrap
without rows instead of failing entry.
================
*/
func TestSiegeStructUpgradeRowsMissingTable(t *testing.T) {
	if rows := siegeStructureUpgradeRows(t.TempDir(), structureUpgradeFixture{}); rows != nil {
		t.Fatalf("rows = %+v, want nil", rows)
	}
	if rows := siegeStructureUpgradeRows("", structureUpgradeFixture{}); rows != nil {
		t.Fatalf("rows = %+v, want nil", rows)
	}
	if rows := siegeStructureUpgradeRows(t.TempDir(), nil); rows != nil {
		t.Fatalf("rows = %+v, want nil", rows)
	}
}
