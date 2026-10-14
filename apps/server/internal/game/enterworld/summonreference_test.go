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

/*
================
TestSummonFamilyStartsSeparatesTheFirstFormFromGaps

The rows below a family's first authored form are expected (the threshold
map always begins at 1); a missing row above it, or a family with no
authored form, is a real gap that stays a warning.
================
*/
func TestSummonFamilyStartsSeparatesTheFirstFormFromGaps(t *testing.T) {
	summoner := func(family string, levels ...uint8) *ItemRef {
		return &ItemRef{TypeIDs: [4]int64{3, 3, 3, 2}, AssociatedCharacterCodename: family, SummonLevelThresholds: levels}
	}
	items := map[string]*ItemRef{
		"ITEM_HORSE":  summoner("COS_T_HORSE", 1, 20, 40, 255),
		"ITEM_PIG":    summoner("COS_C_PIG", 1, 5, 255),
		"ITEM_HOLE":   summoner("COS_T_HOLE", 1, 10, 20, 30, 255),
		"ITEM_GHOST":  summoner("COS_T_GHOST", 1, 10, 255),
		"ITEM_PLAIN":  {TypeIDs: [4]int64{3, 3, 3, 2}, AssociatedCharacterCodename: "COS_PLAIN"},
		"ITEM_LOST":   {TypeIDs: [4]int64{3, 3, 3, 2}, AssociatedCharacterCodename: "COS_LOST"},
		"ITEM_WHOLE":  summoner("COS_T_WHOLE", 1, 20, 255),
		"ITEM_POTION": {TypeIDs: [4]int64{3, 3, 1, 1}, AssociatedCharacterCodename: "COS_T_NOTSUMMON", SummonLevelThresholds: []uint8{1, 5, 255}},
	}
	authored := map[string]bool{
		"COS_T_HORSE_20": true, "COS_T_HORSE_40": true, "COS_C_PIG_5": true,
		"COS_T_HOLE_10": true, "COS_T_HOLE_30": true, "COS_T_WHOLE_1": true, "COS_T_WHOLE_20": true,
		"COS_PLAIN": true,
	}
	starts, gaps := summonFamilyStarts(items, func(code string) bool { return authored[code] })
	want := []summonFamilyStart{
		{family: "COS_C_PIG", level: 5, missing: []string{"COS_C_PIG_1"}},
		{family: "COS_T_HOLE", level: 10, missing: []string{"COS_T_HOLE_1"}},
		{family: "COS_T_HORSE", level: 20, missing: []string{"COS_T_HORSE_1"}},
	}
	if !reflect.DeepEqual(starts, want) {
		t.Fatalf("starts = %+v, want %+v", starts, want)
	}
	// A thresholdless item's missing row (COS_LOST) is a gap too: its summon fails.
	if wantGaps := []string{"COS_LOST", "COS_T_GHOST_1", "COS_T_GHOST_10", "COS_T_HOLE_20"}; !reflect.DeepEqual(gaps, wantGaps) {
		t.Fatalf("gaps = %v, want %v", gaps, wantGaps)
	}
}

/*
================
TestShippedSummonFamiliesHaveNoGaps

Every shipped leveled summon family reaches an authored row at each
threshold from its first form up; only the rows below the first form are
missing, and those are the boot log's "starts at level N" lines.
================
*/
func TestShippedSummonFamiliesHaveNoGaps(t *testing.T) {
	items := NewTextdataItems(gamedatatest.TextdataDir(t))
	items.Len()
	starts, gaps := summonFamilyStarts(items.byCodename, func(code string) bool {
		_, found := items.CharacterRefByCodename(code)
		return found
	})
	if len(gaps) != 0 {
		t.Fatalf("summon thresholds above a family's first form have no row: %v", gaps)
	}
	for _, start := range starts {
		if len(start.missing) != 1 || start.missing[0] != start.family+"_1" {
			t.Fatalf("%s is missing more than its level-1 threshold: %v", start.family, start.missing)
		}
	}
}
