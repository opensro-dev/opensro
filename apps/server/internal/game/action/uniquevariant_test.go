/*
===========================================================================

uniquevariant_test.go - who a build-locked unique copy admits

Pins SRO_STR_INT_UNIQUES at the one decision every direct-damage plan runs
through: the permanent allocation, the tie rule, and the untouched native
default.

===========================================================================
*/
package action

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/monster"
)

/*
================
TestUniqueVariantRefused

A strictly physical total is the STR build; everything else, balance
included, is the INT build. The shipped unique admits everyone.
================
*/
func TestUniqueVariantRefused(t *testing.T) {
	physical := &enterworld.Character{Strength: int64Pointer(300), Intellect: int64Pointer(100)}
	magical := &enterworld.Character{Strength: int64Pointer(100), Intellect: int64Pointer(300)}
	balanced := &enterworld.Character{Strength: int64Pointer(200), Intellect: int64Pointer(200)}
	strengthCopy := monster.Instance{Ref: monster.MonsterRef{UniqueBuild: monster.UniqueBuildSTR}}
	magicCopy := monster.Instance{Ref: monster.MonsterRef{UniqueBuild: monster.UniqueBuildINT}}
	shipped := monster.Instance{Ref: monster.MonsterRef{}}
	for _, tc := range []struct {
		name   string
		c      *enterworld.Character
		target monster.Instance
		want   bool
	}{
		{"physical attacks the STR copy", physical, strengthCopy, false},
		{"physical attacks the INT copy", physical, magicCopy, true},
		{"magical attacks the INT copy", magical, magicCopy, false},
		{"magical attacks the STR copy", magical, strengthCopy, true},
		{"balanced attacks the STR copy", balanced, strengthCopy, true},
		{"balanced attacks the INT copy", balanced, magicCopy, false},
		{"physical attacks the shipped unique", physical, shipped, false},
		{"magical attacks the shipped unique", magical, shipped, false},
		{"no character", nil, strengthCopy, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := uniqueVariantRefused(tc.c, tc.target); got != tc.want {
				t.Fatalf("uniqueVariantRefused = %v, want %v", got, tc.want)
			}
		})
	}
}

/*
================
TestUniqueVariantRefusedUsesPermanentAllocation

Gear and buffs never enter the decision: only the stored pools do, so a
physical character carrying magical gear is still refused by the INT copy.
================
*/
func TestUniqueVariantRefusedUsesPermanentAllocation(t *testing.T) {
	physical := &enterworld.Character{Strength: int64Pointer(400), Intellect: int64Pointer(120)}
	magicCopy := monster.Instance{Ref: monster.MonsterRef{UniqueBuild: monster.UniqueBuildINT}}
	if !uniqueVariantRefused(physical, magicCopy) {
		t.Fatal("the INT copy admitted a physical character")
	}
	strengthCopy := monster.Instance{Ref: monster.MonsterRef{UniqueBuild: monster.UniqueBuildSTR}}
	if uniqueVariantRefused(physical, strengthCopy) {
		t.Fatal("the STR copy refused its own build")
	}
}

/*
================
int64Pointer
================
*/
func int64Pointer(value int64) *int64 {
	return &value
}
