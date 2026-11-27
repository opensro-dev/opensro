/*
===========================================================================

mallgender_test.go - a mall package's wearer sex, from its items' own field

Issue #4480: the renewal mall's gender filter reads the item's own
ReqGender, never a name. Hydrate stamps it on the published package.

===========================================================================
*/
package commerce

import (
	"encoding/json"
	"strings"
	"testing"

	"opensro.online/server/internal/game/enterworld"
)

/*
================
genderRefs

An ItemRefSource over authored references: the two dress sexes, an
ungendered hat (ReqGender 2) and a plain item (no ReqGender at all).
================
*/
type genderRefs struct {
	byCode map[string]*enterworld.ItemRef
}

func (s genderRefs) ItemRefByCodename(codename string) (*enterworld.ItemRef, bool) {
	ref, ok := s.byCode[codename]
	return ref, ok
}

func (s genderRefs) ItemRefByID(id uint32) (*enterworld.ItemRef, bool) {
	for _, ref := range s.byCode {
		if ref.RefObjID == id {
			return ref, true
		}
	}
	return nil, false
}

/*
================
genderRef

One authored equipment reference; gender nil leaves ReqGender absent.
================
*/
func genderRef(codename string, id uint32, gender *float64) *enterworld.ItemRef {
	ref := &enterworld.ItemRef{
		Codename: codename,
		RefObjID: id,
		TypeIDs:  [4]int64{3, 1, 6, 2},
	}
	if gender != nil {
		ref.NativeFields = enterworld.NewNativeFields(map[string]float64{"reqGender": *gender})
	}
	return ref
}

/*
================
genderScrap

One package content row naming item: plus, variance and magic zero,
a nonzero durability so no derivation input is owed.
================
*/
func genderScrap(item string) []string {
	row := []string{"1", "15", "PKG", item, "0", "0", "100", "0"}
	for len(row) < 24 {
		row = append(row, "0")
	}
	// The native no-rental sentinel (scrapRentTerm).
	row[21] = "xxx"
	return row
}

/*
================
TestMallPackageGenderFromItemField

A package takes the sex of its first gendered content, read from the
item's ReqGender: 1 male, 0 female. ReqGender 2 and an absent field
stay ungendered, and only a gendered package publishes the field.
================
*/
func TestMallPackageGenderFromItemField(t *testing.T) {
	male, female, any := 1.0, 0.0, 2.0
	refs := genderRefs{byCode: map[string]*enterworld.ItemRef{
		"ITEM_DRESS_M": genderRef("ITEM_DRESS_M", 101, &male),
		"ITEM_DRESS_W": genderRef("ITEM_DRESS_W", 102, &female),
		"ITEM_HAT_ANY": genderRef("ITEM_HAT_ANY", 103, &any),
		"ITEM_PLAIN":   genderRef("ITEM_PLAIN", 104, nil),
	}}
	catalog := &MallCatalog{Offers: []MallPackage{
		{Code: "PKG_M", Scraps: [][]string{genderScrap("ITEM_DRESS_M")}},
		{Code: "PKG_W", Scraps: [][]string{genderScrap("ITEM_DRESS_W")}},
		{Code: "PKG_SET", Scraps: [][]string{genderScrap("ITEM_HAT_ANY"), genderScrap("ITEM_DRESS_W")}},
		{Code: "PKG_ANY", Scraps: [][]string{genderScrap("ITEM_HAT_ANY")}},
		{Code: "PKG_PLAIN", Scraps: [][]string{genderScrap("ITEM_PLAIN")}},
	}}
	if err := catalog.Hydrate(refs); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"PKG_M": "male", "PKG_W": "female", "PKG_SET": "female", "PKG_ANY": "", "PKG_PLAIN": "",
	}
	for _, offer := range catalog.Offers {
		if offer.Gender != want[offer.Code] {
			t.Fatalf("%s gender = %q, want %q", offer.Code, offer.Gender, want[offer.Code])
		}
		wire, err := json.Marshal(offer)
		if err != nil {
			t.Fatal(err)
		}
		if offer.Gender == "" && strings.Contains(string(wire), "gender") {
			t.Fatalf("%s publishes a gender field while ungendered: %s", offer.Code, wire)
		}
		if offer.Gender != "" && !strings.Contains(string(wire), `"gender":"`+offer.Gender+`"`) {
			t.Fatalf("%s does not publish its gender: %s", offer.Code, wire)
		}
	}
}
