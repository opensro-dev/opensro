/*
===========================================================================
summonreference_test.go - native level suffix selection and publication closure
===========================================================================
*/
package enterworld

import (
	"reflect"
	"strconv"
	"testing"

	"opensro.online/server/internal/testsupport/gamedatatest"
)

/*
================
summonReferenceFixture
================
*/
type summonReferenceFixture map[string]*CharacterRef

/*
================
CharacterRefByCodename
================
*/
func (s summonReferenceFixture) CharacterRefByCodename(code string) (*CharacterRef, bool) {
	ref, found := s[code]
	return ref, found
}

/*
================
SummonableCharacterRefs
================
*/
func (s summonReferenceFixture) SummonableCharacterRefs() []CharacterRef { return nil }

/*
================
TestSummonLevelThresholdParsing
================
*/
func TestSummonLevelThresholdParsing(t *testing.T) {
	for _, tc := range []struct {
		text string
		want []uint8
	}{
		{"xxx", nil},
		{"", []uint8{1, 255}},
		{"20,5,10,5", []uint8{1, 5, 10, 20, 255}},
		{"5,0,20", []uint8{1, 5, 255}},
		{"5,,10", []uint8{1, 5, 255}},
		{",5", []uint8{1, 255}},
		{"5,", []uint8{1, 5, 255}},
		{" 5tail,+10,-1,261", []uint8{1, 5, 10, 255}},
		{"2147483647,10", []uint8{1, 10, 255}},
		{"2147483648,10", []uint8{1, 10, 255}},
		{"999999999999999999999tail,20", []uint8{1, 20, 255}},
		{"5,-2147483648,20", []uint8{1, 5, 255}},
		{"5,-2147483649,20", []uint8{1, 5, 255}},
		{"5,-999999999999999999999,20", []uint8{1, 5, 255}},
	} {
		item := &ItemRef{TypeIDs: [4]int64{3, 3, 3, 2}}
		item.ParamDescriptions[1] = tc.text
		if got := summonLevelThresholds(item); !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("%q: %v, want %v", tc.text, got, tc.want)
		}
		item.TypeIDs = [4]int64{3, 2, 1, 1}
		if got := summonLevelThresholds(item); got != nil {
			t.Fatal("persistent pet acquired consumable thresholds", got)
		}
	}
}

/*
================
TestSummonReferenceUsesAuthoredThresholdsNotCharacterLevel
================
*/
func TestSummonReferenceUsesAuthoredThresholdsNotCharacterLevel(t *testing.T) {
	item := &ItemRef{AssociatedCharacterCodename: "COS", SummonLevelThresholds: []uint8{1, 5, 10, 20, 255}}
	refs := summonReferenceFixture{}
	for index, code := range []string{"COS", "COS_5", "COS_10", "COS_20"} {
		refs[code] = &CharacterRef{Codename: code, RefObjID: uint32(index + 1), Level: 90, TidWord: 0x9c6}
	}
	for _, tc := range []struct {
		level int64
		code  string
	}{{0, ""}, {1, ""}, {4, ""}, {5, "COS_5"}, {9, "COS_5"}, {10, "COS_10"}, {19, "COS_10"}, {20, "COS_20"}, {254, "COS_20"}, {255, ""}, {256, ""}} {
		ref, found := SummonCharacterReference(refs, item, tc.level)
		if found != (tc.code != "") || found && ref.Codename != tc.code {
			t.Fatalf("level %d selected %+v, found=%v", tc.level, ref, found)
		}
	}
	delete(refs, "COS_10")
	if _, found := SummonCharacterReference(refs, item, 15); found {
		t.Fatal("missing chosen variant fell back to a different threshold")
	}
	if flags, found := summonCharacterTypeFlags(refs, item); !found || flags != 0x9c6 {
		t.Fatal("static guidance did not resolve consistent variant family")
	}
	refs["COS_20"].TidWord = 0x11c6
	if _, found := summonCharacterTypeFlags(refs, item); found {
		t.Fatal("mixed variant families supplied arbitrary guide flags")
	}
	item.SummonLevelThresholds = nil
	if ref, found := SummonCharacterReference(refs, item, 1); !found || ref != refs["COS"] {
		t.Fatal("direct association changed without a threshold map")
	}
}

/*
================
TestShippedSummonLevelFamiliesPublishEveryReachableVariant
================
*/
func TestShippedSummonLevelFamiliesPublishEveryReachableVariant(t *testing.T) {
	items := NewTextdataItems(gamedatatest.TextdataDir(t))
	items.Len()
	published := map[string]bool{}
	for _, ref := range items.SummonableCharacterRefs() {
		published[ref.Codename] = true
	}
	families := 0
	for _, item := range items.byCodename {
		if len(item.SummonLevelThresholds) == 0 {
			continue
		}
		families++
		for _, level := range item.SummonLevelThresholds {
			if level == 1 || level == 255 {
				continue
			}
			code := item.AssociatedCharacterCodename + "_" + strconv.Itoa(int(level))
			ref, found := SummonCharacterReference(items, item, int64(level))
			if _, authored := items.CharacterRefByCodename(code); !authored {
				// The existing capacity validator filters transport variants
				// above 140 slots. Unavailable references must not fall back.
				if found || published[code] {
					t.Fatalf("filtered variant admitted: %s", code)
				}
				continue
			}
			if !found || ref.Codename != code || !published[code] {
				t.Fatalf("unpublished %s level %d: %+v", item.Codename, level, ref)
			}
		}
		collector := refItemCollector{deps: &Deps{Items: items}, rows: []RefItemRow{{Codename: item.Codename}}}
		rows := collector.finish()
		flags := rows[0].SummonedCharacterTypeFlags
		if flags == nil || (*flags>>11 != 1 && *flags>>11 != 2) {
			t.Fatalf("%s guide omitted variant family", item.Codename)
		}
	}
	if families != 8 {
		t.Fatalf("level families=%d, want all eight shipped families", families)
	}
}
