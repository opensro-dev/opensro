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
TestMonsterImpactRefused

The plan-time admission: a player is asked directly, a companion through
the owner the pet lane already holds, and a build-locked copy refuses an
unattributed hit rather than letting it through.
================
*/
func TestMonsterImpactRefused(t *testing.T) {
	rt := &Runtime{}
	physical := &enterworld.Character{Strength: int64Pointer(300), Intellect: int64Pointer(100)}
	magical := &enterworld.Character{Strength: int64Pointer(100), Intellect: int64Pointer(300)}
	strengthCopy := monster.Instance{Ref: monster.MonsterRef{UniqueBuild: monster.UniqueBuildSTR}}
	magicCopy := monster.Instance{Ref: monster.MonsterRef{UniqueBuild: monster.UniqueBuildINT}}
	shipped := monster.Instance{Ref: monster.MonsterRef{}}
	for _, tc := range []struct {
		name   string
		caster monsterImpactCaster
		target monster.Instance
		want   bool
	}{
		{"player refused by the other build", monsterImpactCaster{character: magical}, strengthCopy, true},
		{"player admitted by its own build", monsterImpactCaster{character: magical}, magicCopy, false},
		{"pet asked through its owner", monsterImpactCaster{owner: magical, credit: 100001}, strengthCopy, true},
		{"pet admitted through its owner", monsterImpactCaster{owner: physical, credit: 100001}, magicCopy, true},
		{"unattributed hit on a copy", monsterImpactCaster{}, strengthCopy, true},
		{"shipments admit anyone", monsterImpactCaster{character: magical}, shipped, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := rt.monsterImpactRefused(tc.caster, tc.target); got != tc.want {
				t.Fatalf("monsterImpactRefused = %v, want %v", got, tc.want)
			}
		})
	}
}

/*
================
TestStrIntCopiesPayTheirBaseCounts

The unique prepass reads the base's codename, so a copy keeps the
codename-keyed overrides (here Lord Yarkan's repairable-drop count) that its
own codename would lose.
================
*/
func TestStrIntCopiesPayTheirBaseCounts(t *testing.T) {
	copyRef := monster.MonsterRef{Codename: "MOB_TK_BONELORD_STR", VariantOf: "MOB_TK_BONELORD", MonsterType: 3}
	base := UniqueDropCounts(3, "MOB_TK_BONELORD")
	if got := UniqueDropCounts(3, copyRef.BaseCodename()); got != base {
		t.Fatalf("copy counts = %+v, want the base's %+v", got, base)
	}
	if own := UniqueDropCounts(3, copyRef.Codename); own == base {
		t.Fatal("this species has no codename-keyed override, so the test proves nothing")
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
