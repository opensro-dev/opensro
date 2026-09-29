/*
===========================================================================

skillobject_test.go - dynamic-object row layout from native write primitives

The fixed byte vector is derived from 48CE60/484FB0/487CE0 instruction widths.
It is not described as a captured production packet.

===========================================================================
*/
package wire

import (
	"encoding/hex"
	"testing"
)

/*
================
TestSkillObjectListAndSingleRows
================
*/
func TestSkillObjectListAndSingleRows(t *testing.T) {
	row := SkillObjectSpawn{SkillID: 7108, GID: 0x01000001, Region: 0x6454, X: 100, Y: 10, Z: 200, Heading: 90, Appear: 1}
	want := "ffffffff5400c41b00000100000154640000c84200002041000048435a00"
	if got := hex.EncodeToString(row.Encode(false)); got != want {
		t.Fatalf("group row=%s want=%s", got, want)
	}
	if got := hex.EncodeToString(row.Encode(true)); got != want+"01" {
		t.Fatalf("single row=%s want=%s01", got, want)
	}
}
