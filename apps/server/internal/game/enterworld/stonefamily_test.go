/*
===========================================================================

stonefamily_test.go - synthetic stone family mask tests

All fixtures are invented rows; no shipped textdata is read here.

===========================================================================
*/
package enterworld

import "testing"

// fakeStoneMagicOptions resolves invented options by codename and degree.
type fakeStoneMagicOptions struct {
	rows map[string]*MagicOptionRow
}

/*
================
MagicOptionByParamID
================
*/
func (f fakeStoneMagicOptions) MagicOptionByParamID(paramID uint32) (*MagicOptionRow, bool) {
	return nil, false
}

/*
================
MagicOptionByCodenameDegree
================
*/
func (f fakeStoneMagicOptions) MagicOptionByCodenameDegree(codename string, degree int64) (*MagicOptionRow, bool) {
	row, ok := f.rows[codename]
	if !ok || row.Degree != degree {
		return nil, false
	}
	return row, true
}

/*
================
stoneTestRef
================
*/
func stoneTestRef(kind int64, descriptions ...string) *ItemRef {
	ref := &ItemRef{TypeIDs: [4]int64{3, 3, 11, kind}}
	copy(ref.ParamDescriptions[:], descriptions)
	return ref
}

/*
================
TestStoneFamilyMaskFor
================
*/
func TestStoneFamilyMaskFor(t *testing.T) {
	if got := stoneFamilyMaskFor("weapon armor"); got != stoneFamilyWeapon|stoneFamilyArmor {
		t.Fatalf("weapon armor mask = %d", got)
	}
	// Substring admission: 'mail' matches inside 'avatar_mail', as the
	// native joined-text search does.
	if got := stoneFamilyMaskFor("avatar_mail helm"); got != stoneFamilyMail|stoneFamilyHelm {
		t.Fatalf("avatar_mail helm mask = %d", got)
	}
	if got := stoneFamilyMaskFor("armor,shield"); got != stoneFamilyArmor|stoneFamilyShield {
		t.Fatalf("armor,shield mask = %d", got)
	}
	if got := stoneFamilyMaskFor(""); got != 0 {
		t.Fatalf("empty mask = %d", got)
	}
	// The native compare is case-sensitive: an uppercase descriptor admits
	// nothing, where the old fold admitted every word.
	if got := stoneFamilyMaskFor("Weapon ARMOR"); got != 0 {
		t.Fatalf("uppercase mask = %d", got)
	}
	if got := stoneFamilyMaskFor("weapon ARMOR"); got != stoneFamilyWeapon {
		t.Fatalf("mixed-case mask = %d", got)
	}
}

/*
================
TestItemStoneNativeFields
================
*/
func TestItemStoneNativeFields(t *testing.T) {
	magic := fakeStoneMagicOptions{rows: map[string]*MagicOptionRow{
		"MATTR_TEST":   {OptionName: "MATTR_TEST", Degree: 2, Descriptors: []string{"weapon", "armor"}},
		"MATTR_REPAIR": {OptionName: "MATTR_REPAIR", Degree: 2, Descriptors: []string{"weapon"}},
		"MATTR_EMPTY":  {OptionName: "MATTR_EMPTY", Degree: 2},
	}}
	base := NewNativeFields(map[string]float64{"itemParam1_29c": 2})

	mask, ok := ItemStoneNativeFields(base, stoneTestRef(1, "MATTR_TEST"), magic).Lookup("stoneFamilyMask")
	if !ok || uint32(mask) != stoneFamilyWeapon|stoneFamilyArmor {
		t.Fatalf("magic stone mask = %v, %v", mask, ok)
	}

	mask, ok = ItemStoneNativeFields(base, stoneTestRef(7, "MATTR_REPAIR"), magic).Lookup("stoneFamilyMask")
	if !ok || uint32(mask) != stoneFamilyWeapon|stoneFamilyHonor {
		t.Fatalf("repair stone mask = %v, %v", mask, ok)
	}

	// A resolved option with no descriptors publishes a present zero: the
	// native kind gate folded into the mask refuses every target.
	mask, ok = ItemStoneNativeFields(base, stoneTestRef(1, "MATTR_EMPTY"), magic).Lookup("stoneFamilyMask")
	if !ok || mask != 0 {
		t.Fatalf("empty option mask = %v, %v", mask, ok)
	}

	// An unresolvable option publishes nothing, so the client binds and
	// the server fuse remains the only gate.
	if _, ok = ItemStoneNativeFields(base, stoneTestRef(1, "MATTR_MISSING"), magic).Lookup("stoneFamilyMask"); ok {
		t.Fatal("missing option published a mask")
	}

	attribute := stoneTestRef(2)
	attribute.ParamDescriptions[3] = "armor,shield"
	mask, ok = ItemStoneNativeFields(base, attribute, magic).Lookup("stoneFamilyMask")
	if !ok || uint32(mask) != stoneFamilyArmor|stoneFamilyShield {
		t.Fatalf("attribute stone mask = %v, %v", mask, ok)
	}

	plain := &ItemRef{TypeIDs: [4]int64{3, 1, 6, 1}}
	if _, ok = ItemStoneNativeFields(base, plain, magic).Lookup("stoneFamilyMask"); ok {
		t.Fatal("non-stone published a mask")
	}

	if _, ok = ItemStoneNativeFields("", stoneTestRef(2), magic).Lookup("stoneFamilyMask"); ok {
		t.Fatal("degreeless stone published a mask")
	}
}
