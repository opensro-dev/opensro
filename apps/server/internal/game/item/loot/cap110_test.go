/*
===========================================================================
cap110_test.go - optional drop compilation and startup refusal boundaries.
===========================================================================
*/
package loot

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

/*
================
TestCap110EquipmentUsesInstalledExpansionBucket
================
*/
func TestCap110EquipmentUsesInstalledExpansionBucket(t *testing.T) {
	requireTables(t)
	var source cap110Catalog
	source.Version = 1
	if err := json.Unmarshal(equipmentJSON, &source.Equipment); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(propertiesJSON, &source.Properties); err != nil {
		t.Fatal(err)
	}
	const code = "ITEM_CH_SWORD_11_A"
	source.Equipment.Items = append(source.Equipment.Items, equipmentRef{Codename: code, Group: 30, Type: "3:1:6:2", Weight: 100, Absolute: 100, Level: 101})
	source.Properties.Items[code] = propertyItem{Codename: code, Type: [4]uint8{3, 1, 6, 2}, Class: 31, MaxMagic: 9}
	source.Equipment.Normal[109] = make([]float32, source.Equipment.Widths["normal"])
	source.Equipment.Normal[109][30] = 1
	data, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	next, props, err := compileCap110(data)
	if err != nil {
		t.Fatal(err)
	}
	ref, ok := next.selectEquipment(0, 30, false, 110, func() (uint32, error) { return 0, nil })
	if !ok || ref.Codename != code || props.Items[code].Class != 31 {
		t.Fatal("expansion bucket or properties lost")
	}
	if err := InitCap110(t.TempDir(), false); err != nil {
		t.Fatalf("native off read optional file: %v", err)
	}
	if err := InitCap110(t.TempDir(), true); err == nil {
		t.Fatal("on admitted absent drop input")
	}
}

/*
================
TestCap110EquipmentRejectsMalformedData
================
*/
func TestCap110EquipmentRejectsMalformedData(t *testing.T) {
	for _, data := range []string{`{"Version":2}`, `{"Version":1,"Unknown":1}`, `{}`} {
		if _, _, err := compileCap110([]byte(data)); err == nil {
			t.Fatal("malformed optional loot admitted")
		}
	}
}

/*
================
rewardsSource

A minimal valid rewards file for one new monster: a fixed drop and a random
group, in the group-id range the importer uses.
================
*/
func rewardsSource() cap110Rewards {
	return cap110Rewards{Version: 1,
		Fixed:  []assignedDrop{{Monster: "MOB_SD_TEST_BOSS", Item: "ITEM_ETC_TEST_MATERIAL", Min: 1, Max: 1, Probability: 0.4}},
		Random: []assignedRandom{{Monster: "MOB_SD_TEST_BOSS", Group: 1000002, Min: 2, Max: 2, Probability: 0.2}},
		Groups: map[int][]groupDrop{1000002: {{Codename: "ITEM_ETC_TEST_TOKEN", Probability: 1}}}}
}

/*
================
TestCap110RewardsJoinNewMonstersWithoutTouchingNative
================
*/
func TestCap110RewardsJoinNewMonstersWithoutTouchingNative(t *testing.T) {
	data, err := json.Marshal(rewardsSource())
	if err != nil {
		t.Fatal(err)
	}
	nativeFixed, nativeGroups := len(nativeConsumables.fixed), len(nativeConsumables.groups)
	joined, err := compileCap110Rewards(nativeConsumables, data)
	if err != nil {
		t.Fatal(err)
	}
	if len(nativeConsumables.fixed) != nativeFixed || len(nativeConsumables.groups) != nativeGroups {
		t.Fatal("the native catalogue was modified")
	}
	if len(joined.fixed) != nativeFixed+1 || len(joined.groups) != nativeGroups+1 {
		t.Fatalf("rules not joined: fixed %d groups %d", len(joined.fixed), len(joined.groups))
	}
	// Every roll succeeds: the boss gives its fixed material and two tokens.
	always := func() (uint32, error) { return 0, nil }
	got := joined.assigned("MOB_SD_TEST_BOSS", 60, always)
	var codes []string
	for _, drop := range got {
		codes = append(codes, drop.Codename)
	}
	if len(got) != 3 || codes[0] != "ITEM_ETC_TEST_TOKEN" || codes[1] != "ITEM_ETC_TEST_TOKEN" || codes[2] != "ITEM_ETC_TEST_MATERIAL" {
		t.Fatalf("rewards did not drop as authored: %v", codes)
	}
	if len(joined.assigned("MOB_SD_OTHER", 60, always)) != 0 {
		t.Fatal("a monster without rules dropped a reward")
	}
}

/*
================
TestCap110RewardsRefuseWhatWouldChangeNativeOrDangle
================
*/
func TestCap110RewardsRefuseWhatWouldChangeNativeOrDangle(t *testing.T) {
	requireTables(t)
	var native string
	for monster := range nativeConsumables.fixed {
		native = monster
		break
	}
	nativeGroup := 0
	for group := range nativeConsumables.groups {
		nativeGroup = group
		break
	}
	cases := map[string]func(*cap110Rewards){
		"empty":          func(r *cap110Rewards) { r.Fixed, r.Random = nil, nil },
		"native monster": func(r *cap110Rewards) { r.Fixed[0].Monster = native },
		"native group": func(r *cap110Rewards) {
			r.Groups = map[int][]groupDrop{nativeGroup: r.Groups[1000002]}
			r.Random[0].Group = nativeGroup
		},
		"undefined group":      func(r *cap110Rewards) { r.Random[0].Group = 1999999 },
		"empty group":          func(r *cap110Rewards) { r.Groups[1000002] = nil },
		"zero chance":          func(r *cap110Rewards) { r.Fixed[0].Probability = 0 },
		"chance over one":      func(r *cap110Rewards) { r.Random[0].Probability = 1.5 },
		"min over max":         func(r *cap110Rewards) { r.Fixed[0].Min, r.Fixed[0].Max = 3, 2 },
		"not an item":          func(r *cap110Rewards) { r.Fixed[0].Item = "MOB_SD_X" },
		"not a monster":        func(r *cap110Rewards) { r.Fixed[0].Monster = "ITEM_X" },
		"duplicate fixed rule": func(r *cap110Rewards) { r.Fixed = append(r.Fixed, r.Fixed[0]) },
		"bad group member":     func(r *cap110Rewards) { r.Groups[1000002][0].Codename = "nothing" },
	}
	for name, change := range cases {
		source := rewardsSource()
		change(&source)
		data, err := json.Marshal(source)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := compileCap110Rewards(nativeConsumables, data); err == nil {
			t.Fatalf("%s admitted", name)
		}
	}
	for _, data := range []string{`{"Version":2}`, `{"Version":1,"Unknown":1}`, `{"Version":1,"Fixed":[]} {}`} {
		if _, err := compileCap110Rewards(nativeConsumables, []byte(data)); err == nil {
			t.Fatal("malformed rewards admitted")
		}
	}
}

/*
================
TestInitCap110RequiresRewardsAndIsRepeatable
================
*/
func TestInitCap110RequiresRewardsAndIsRepeatable(t *testing.T) {
	requireTables(t)
	var source cap110Catalog
	source.Version = 1
	if err := json.Unmarshal(equipmentJSON, &source.Equipment); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(propertiesJSON, &source.Properties); err != nil {
		t.Fatal(err)
	}
	equipmentData, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	rewardData, err := json.Marshal(rewardsSource())
	if err != nil {
		t.Fatal(err)
	}
	savedEquipment, savedProperties, savedConsumables := equipment, properties, consumables
	t.Cleanup(func() { equipment, properties, consumables = savedEquipment, savedProperties, savedConsumables })
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, cap110EquipmentFile), equipmentData, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := InitCap110(dir, true); err == nil {
		t.Fatal("expansion admitted without its rewards")
	}
	if err := os.WriteFile(filepath.Join(dir, cap110RewardsFile), rewardData, 0o600); err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		if err := InitCap110(dir, true); err != nil {
			t.Fatalf("attempt %d: %v", attempt, err)
		}
	}
	if len(consumables.fixed["MOB_SD_TEST_BOSS"]) != 1 {
		t.Fatal("rewards not installed, or stacked by a second call")
	}
}

/*
================
TestCap110ExpansionBucketsStayAwayFromNativeLevels

Degree-10 items of required level 90 sit in a bucket the native catalog leaves
empty. A level-90 monster must still step down as natively; a level-91 monster
may pick the item.
================
*/
func TestCap110ExpansionBucketsStayAwayFromNativeLevels(t *testing.T) {
	requireTables(t)
	var source cap110Catalog
	source.Version = 1
	if err := json.Unmarshal(equipmentJSON, &source.Equipment); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(propertiesJSON, &source.Properties); err != nil {
		t.Fatal(err)
	}
	const code = "ITEM_CH_SWORD_10_A"
	source.Equipment.Items = append(source.Equipment.Items, equipmentRef{Codename: code, Group: 30, Type: "3:1:6:2", Weight: 100, Absolute: 100, Level: 90})
	source.Properties.Items[code] = propertyItem{Codename: code, Type: [4]uint8{3, 1, 6, 2}, Class: 31, MaxMagic: 9}
	data, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	next, _, err := compileCap110(data)
	if err != nil {
		t.Fatal(err)
	}
	markExpansionBuckets(next, equipment)
	roll := func() (uint32, error) { return 0, nil }
	if ref, ok := next.selectEquipment(0, 30, false, 91, roll); !ok || ref.Codename != code {
		t.Fatalf("a level-91 monster lost the expansion item: %+v %v", ref, ok)
	}
	// Level 90 steps down to the native degree-9 class, as the native catalog does.
	if ref, ok := next.selectEquipment(0, 30, false, 90, roll); !ok || ref.Codename == code || ref.Group >= 27 {
		t.Fatalf("a level-90 monster did not step down to a native item: %+v %v", ref, ok)
	}
	for key, bucket := range next.buckets {
		if equipment.buckets[key] != nil && bucket.expansionOnly {
			t.Fatalf("native-filled bucket %+v was marked expansion-only", key)
		}
	}
}
