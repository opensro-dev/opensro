package enterworld

import (
	"opensro.online/server/internal/testsupport/gamedatatest"
	"opensro.online/server/internal/testsupport/licensed"
	"strings"
	"testing"
)

func TestProjectedConsumableNames(t *testing.T) {
	licensed.RequireGameData(t)
	dir := gamedatatest.TextdataDir(t)
	items := NewTextdataItems(dir)
	for _, tc := range []struct {
		codename string
		id       uint32
		name     string
	}{
		{"ITEM_ETC_HP_POTION_05", 8, "HP recovery potion (X-large)"},
		{"ITEM_ETC_MP_POTION_05", 15, "MP recovery potion (X-large)"},
		{"ITEM_ETC_CURE_RANDOM_01", 10368, "Purification pill (small)"},
		{"ITEM_ETC_CURE_RANDOM_02", 10369, "Purification pill (medium)"},
		{"ITEM_ETC_CURE_RANDOM_03", 10370, "Purification pill (large)"},
		{"ITEM_ETC_CURE_RANDOM_04", 10371, "Purification pill (X-large)"},
	} {
		t.Run(tc.codename, func(t *testing.T) {
			ref, ok := items.ItemRefByCodename(tc.codename)
			if !ok {
				t.Fatal("projected item missing")
			}
			if ref.RefObjID != tc.id || ref.Name != tc.name {
				t.Fatalf("projected item title: got id %d name %q, want id %d name %q", ref.RefObjID, ref.Name, tc.id, tc.name)
			}
		})
	}
}

func TestEveryProjectedItemHasDisplayName(t *testing.T) {
	licensed.RequireGameData(t)
	dir := gamedatatest.TextdataDir(t)
	items := NewTextdataItems(dir)
	if items.Len() != 8439 {
		t.Fatalf("item corpus changed: %d", items.Len())
	}
	for _, ref := range items.byCodename {
		name := strings.TrimSpace(ref.Name)
		if name == "" || name == "-" || name == "0" || strings.EqualFold(name, "xxx") {
			t.Errorf("missing title: %d %s -> %s", ref.RefObjID, ref.Codename, ref.NameStrID)
		}
	}
}

func TestTooltipAvatarTrailingFields(t *testing.T) {
	fields := make([]string, 160)
	fields[118], fields[156] = "99", "77"
	fields[158], fields[159] = "4", "1"
	values := buildItemNativeFields(fields)
	if values.Get("maxMagicOptions51c") != 4 || values.Get("avatarAttachment51d") != 1 || values.Get("itemParam20_2e8") != 77 {
		t.Fatalf("trailing avatar fields displaced: %v", values)
	}
	fields[158], fields[159] = "260", "256"
	values = buildItemNativeFields(fields)
	if values.Get("maxMagicOptions51c") != 4 || values.Get("avatarAttachment51d") != 0 {
		t.Fatal("native byte-store semantics not preserved")
	}
	if _, present := buildItemNativeFields(fields[:158]).Lookup("maxMagicOptions51c"); present {
		t.Fatal("missing native metadata manufactured")
	}
}

// buildItemRef must lift the equip-requirement media columns
// (14, 32..39, 58/59/60, 64) into the typed ItemRef fields the equip
// gates read, and default absent columns to their pass values (country 3,
// sex 2, stat floors 0, quad types -1).
func TestBuildItemRefRequirementColumns(t *testing.T) {
	fields := make([]string, 160)
	for i := range fields {
		fields[i] = "0"
	}
	fields[1] = "3417"
	fields[2] = "ITEM_CH_SWORD_03_A"
	fields[5] = "SN_ITEM_CH_SWORD_03_A"
	fields[6] = "SN_ITEM_CH_SWORD_03_A_TT_DESC"
	fields[120], fields[124], fields[156] = "25", "10", "77"
	fields[128] = "2"
	fields[9], fields[10], fields[11], fields[12] = "3", "1", "6", "2"
	fields[14] = "0"   // country: China
	fields[17] = "1"   // independent of trade/buy permission and shop membership
	fields[24] = "129" // CanUse is a bitfield; preserve additional authored flags.
	// The typed requirement quad, interleaved types/values (32..39): the
	// CH shape - type 1 (character level) at slot 1, the rest empty.
	fields[32], fields[33] = "1", "16"
	fields[34], fields[35] = "-1", "0"
	fields[36], fields[37] = "-1", "0"
	fields[38], fields[39] = "-1", "0"
	fields[58] = "2"    // requiredSex: unisex
	fields[59] = "7"    // reqStr
	fields[60] = "9"    // reqInt
	fields[64] = "87.0" // Dur_U: durability-bearing class (razed gate marker)
	fields[53], fields[54], fields[55] = "item/drop.bsr", "item\\china\\sword_03.ddj", "item/other.bsr"

	ref := buildItemRef(fields, map[string]string{})
	if ref == nil || ref.NativeFields.Get("canSell") != 1 || ref.NativeFields.Get("canUse") != 129 {
		t.Fatal("native sale permission column missing")
	}
	if ref == nil {
		t.Fatal("buildItemRef returned nil for a well-formed row")
	}
	if ref.DescriptionSymbol != "SN_ITEM_CH_SWORD_03_A_TT_DESC" || ref.NativeFields.Get("itemParam2_2a0") != 25 || ref.NativeFields.Get("itemParam4_2a8") != 10 || ref.NativeFields.Get("itemParam20_2e8") != 77 || ref.NativeFields.Get("itemParam6_2b0") != 2 {
		t.Fatalf("tooltip projection incomplete: %+v", ref)
	}
	if ref.Icon != "item\\china\\sword_03.ddj" {
		t.Fatalf("item icon = %q, want exact AssocFileIcon column 54", ref.Icon)
	}
	if ref.Country != 0 || ref.RequiredSex != 2 || ref.RequiredStr != 7 || ref.RequiredInt != 9 {
		t.Fatalf("requirement columns = country %d, sex %d, str %d, int %d; want 0/2/7/9",
			ref.Country, ref.RequiredSex, ref.RequiredStr, ref.RequiredInt)
	}
	if ref.ReqQuadTypes != [4]int64{1, -1, -1, -1} || ref.ReqQuadValues != [4]int64{16, 0, 0, 0} {
		t.Fatalf("requirement quad = %v/%v, want [1 -1 -1 -1]/[16 0 0 0]", ref.ReqQuadTypes, ref.ReqQuadValues)
	}
	if ref.MaxDurability != 87 {
		t.Fatalf("MaxDurability = %d, want 87 (Dur_U column 64)", ref.MaxDurability)
	}

	// The full typed quad (columns 32..39) must ride the numeric native-field
	// projection under its offset-stable RefItemData names.
	wantRecord := map[string]float64{
		"reqLevelType1": 1, "requiredLevel": 16,
		"reqLevelType2": -1, "requiredLevel2": 0,
		"reqLevelType3": -1, "requiredLevel3": 0,
		"reqLevelType4": -1, "requiredLevel4": 0,
	}
	for key, want := range wantRecord {
		got, present := ref.NativeFields.Lookup(key)
		if !present || got != want {
			t.Errorf("Record[%q] = %v (present=%v), want %v", key, got, present, want)
		}
	}
}

// The EU armor shape: mastery type dwords in the quad (513 Warrior at
// slot 1 with the tier level; caster multi-type rows fill later slots).
func TestBuildItemRefMasteryQuad(t *testing.T) {
	fields := make([]string, 160)
	for i := range fields {
		fields[i] = "0"
	}
	fields[1] = "13100"
	fields[2] = "ITEM_EU_M_HEAVY_05_BA_A"
	fields[5] = "SN_ITEM_EU_M_HEAVY_05_BA_A"
	fields[9], fields[10], fields[11], fields[12] = "3", "1", "11", "3"
	fields[32], fields[33] = "513", "35"
	fields[34], fields[35] = "-1", "0"
	fields[36], fields[37] = "-1", "0"
	fields[38], fields[39] = "-1", "0"

	ref := buildItemRef(fields, map[string]string{})
	if ref == nil {
		t.Fatal("buildItemRef returned nil")
	}
	if ref.ReqQuadTypes != [4]int64{513, -1, -1, -1} || ref.ReqQuadValues != [4]int64{35, 0, 0, 0} {
		t.Fatalf("mastery quad = %v/%v, want [513 -1 -1 -1]/[35 0 0 0]", ref.ReqQuadTypes, ref.ReqQuadValues)
	}
}

// The fortress role mask rides media column 124 (native +0x2ac); the 14
// shipped siege rows all carry -1 (any role).
func TestBuildItemRefFortressRoleMask(t *testing.T) {
	fields := make([]string, 160)
	for i := range fields {
		fields[i] = "0"
	}
	fields[1] = "19227"
	fields[2] = "ITEM_FORT_FORTRESS_HAMMER_06"
	fields[5] = "SN_ITEM_FORT_FORTRESS_HAMMER_06"
	fields[9], fields[10], fields[11], fields[12] = "3", "1", "6", "16"
	fields[124] = "-1"

	ref := buildItemRef(fields, map[string]string{})
	if ref == nil {
		t.Fatal("buildItemRef returned nil")
	}
	if ref.FortressRoleMask != -1 {
		t.Fatalf("FortressRoleMask = %d, want -1 (column 124)", ref.FortressRoleMask)
	}
}

// Common potion recovery values ride the v1.150 media columns consumed by
// the native item-use path. Keep them as typed server-only fields so gameplay
// can apply the authoritative values from the typed numeric field projection.
func TestBuildItemRefPotionRecoveryColumns(t *testing.T) {
	fields := make([]string, 160)
	for i := range fields {
		fields[i] = "0"
	}
	fields[1] = "9999"
	fields[2] = "ITEM_ETC_HP_POTION_TEST"
	fields[5] = "SN_ITEM_ETC_HP_POTION_TEST"
	fields[9], fields[10], fields[11], fields[12] = "3", "3", "1", "1"
	fields[118] = "120"
	fields[120] = "12.5"
	fields[122] = "80"
	fields[124] = "7.5"

	ref := buildItemRef(fields, map[string]string{})
	if ref == nil {
		t.Fatal("buildItemRef returned nil")
	}
	if ref.RecoveryHP != 120 || ref.RecoveryHPPercent != 12.5 ||
		ref.RecoveryMP != 80 || ref.RecoveryMPPercent != 7.5 {
		t.Fatalf("recovery columns = HP %v/%v%% MP %v/%v%%; want 120/12.5/80/7.5",
			ref.RecoveryHP, ref.RecoveryHPPercent, ref.RecoveryMP, ref.RecoveryMPPercent)
	}

	for _, key := range []string{"recoveryHP", "recoveryHPPercent", "recoveryMP", "recoveryMPPercent"} {
		if _, present := ref.NativeFields.Lookup(key); present {
			t.Errorf("NativeFields[%q] unexpectedly present; recovery fields are typed/server-only", key)
		}
	}
}

// TestRealTextdataBladeRow pins the itemdata loader against the extracted
// v1.150 data: the measured copper blade (refObjId 107) and its native fields.
func TestRealTextdataBladeRow(t *testing.T) {
	t.Parallel()
	items := sharedShippedItems(t)
	row, ok := items.ItemRefByCodename("ITEM_CH_BLADE_01_A")
	if !ok {
		t.Fatal("itemdata lacks ITEM_CH_BLADE_01_A")
	}
	if row.RefObjID != 107 {
		t.Errorf("blade refObjId = %d, want 107", row.RefObjID)
	}
	if row.TypeFlags() != 0x1b2c {
		t.Errorf("blade typeFlags = %#x, want 0x1b2c", row.TypeFlags())
	}
	if row.Name != "Copper Blade" {
		t.Errorf("blade name = %q", row.Name)
	}
	if row.NativeFields.Get("varianceIntMin1c0") != float64(69) || row.NativeFields.Get("maxDurability") != float64(84) || row.NativeFields.Get("sellPrice") != float64(427) {
		t.Errorf("blade native fields diverge: dur_l=%v maxDur=%v sell=%v",
			row.NativeFields.Get("varianceIntMin1c0"), row.NativeFields.Get("maxDurability"), row.NativeFields.Get("sellPrice"))
	}
}

// A truncated row (the 13-field minimum) keeps the pass defaults.
func TestBuildItemRefRequirementDefaults(t *testing.T) {
	fields := make([]string, 13)
	for i := range fields {
		fields[i] = "0"
	}
	fields[1] = "42"
	fields[2] = "ITEM_CH_TEST_SHORT_ROW"

	ref := buildItemRef(fields, map[string]string{})
	if ref == nil {
		t.Fatal("buildItemRef returned nil for the minimal row")
	}
	if ref.Country != 3 || ref.RequiredSex != 2 || ref.RequiredStr != 0 || ref.RequiredInt != 0 {
		t.Fatalf("defaults = country %d, sex %d, str %d, int %d; want 3/2/0/0",
			ref.Country, ref.RequiredSex, ref.RequiredStr, ref.RequiredInt)
	}
	if ref.ReqQuadTypes != [4]int64{-1, -1, -1, -1} {
		t.Fatalf("quad type defaults = %v, want all -1 (empty slots)", ref.ReqQuadTypes)
	}
}

func TestBuildItemRefRejectsIDsOutsideWireWidth(t *testing.T) {
	for _, id := range []string{"-1", "4294967296"} {
		fields := make([]string, 13)
		fields[1] = id
		fields[2] = "ITEM_HOSTILE_ID"
		if ref := buildItemRef(fields, nil); ref != nil {
			t.Fatalf("buildItemRef(%s) = %+v, want refusal", id, ref)
		}
	}
}

func TestShippedSwordCombatColumnsStayPinned(t *testing.T) {
	t.Parallel()
	items := sharedShippedItems(t)

	ref, ok := items.ItemRefByCodename("ITEM_CH_SWORD_01_A")
	if !ok || ref == nil || ref.Combat == nil {
		t.Fatalf("shipped sword combat ref = %+v/%v, want a complete typed row", ref, ok)
	}
	if ref.RefObjID != 71 || ref.TypeIDs != [4]int64{3, 1, 6, 2} {
		t.Fatalf("shipped sword identity = id %d types %v, want 71/[3 1 6 2]",
			ref.RefObjID, ref.TypeIDs)
	}
	got := ref.Combat
	if got.ActionRange != 6 ||
		got.PhysicalAttack.Minimum != (ItemStatRange{Min: 15, Max: 16, PerPlus: 2.4000001}) ||
		got.PhysicalAttack.Maximum != (ItemStatRange{Min: 16, Max: 18, PerPlus: 2.4000001}) ||
		got.MagicalAttack.Minimum != (ItemStatRange{Min: 25, Max: 26, PerPlus: 4.0999999}) ||
		got.MagicalAttack.Maximum != (ItemStatRange{Min: 28, Max: 31, PerPlus: 4.0999999}) ||
		got.HitRate != (ItemStatRange{Min: 24, Max: 30}) ||
		got.CriticalRate != (ItemStatRange{Min: 3, Max: 15}) {
		t.Fatalf("shipped sword combat columns moved: %+v", *got)
	}
}

func TestCombatColumnsAreAllOrNone(t *testing.T) {
	fields := make([]string, 118)
	for index := range fields {
		fields[index] = "0"
	}
	fields[1] = "71"
	fields[2] = "ITEM_CH_SWORD_01_A"
	fields[9], fields[10], fields[11], fields[12] = "3", "1", "6", "2"
	fields[113] = "not-a-number"

	ref := buildItemRef(fields, nil)
	if ref == nil {
		t.Fatal("ordinary item identity unexpectedly rejected")
	}
	if ref.Combat != nil {
		t.Fatalf("partial combat row became authoritative: %+v", *ref.Combat)
	}
}

// These are explicit product translations in the generated projection. The raw
// v1.150 source has blank English cells; do not assert this against the oracle.
func TestProjectedSpeedScrollNames(t *testing.T) {
	items := sharedShippedItems(t)
	for _, row := range []struct {
		id         uint32
		code, name string
	}{
		{9263, "ITEM_MALL_MOVE_SPEED_UP_50", "Moving Speed Scroll (50%)"},
		{9264, "ITEM_MALL_MOVE_SPEED_UP_100", "Moving Speed Scroll (100%)"},
		{24198, "ITEM_ETC_SPEED_UP_BASIC", "Beginner's Moving Speed Scroll"},
	} {
		byID, ok := items.ItemRefByID(row.id)
		if !ok || byID.Name != row.name || byID.DescriptionSymbol != "SN_"+row.code+"_TT_DESC" {
			t.Fatalf("incomplete scroll reference %d: %+v", row.id, byID)
		}
		byCode, ok := items.ItemRefByCodename(row.code)
		if !ok || byCode != byID {
			t.Fatalf("GM and inventory reference owners disagree for %s", row.code)
		}
	}
	item, _ := items.ItemRefByID(9264)
	if item.Icon != "item\\etc\\mall_move_speed_up_100.ddj" && item.Icon != "item/etc/mall_move_speed_up_100.ddj" {
		t.Fatalf("retail scroll icon replaced: %q", item.Icon)
	}
}

func TestReturnScrollDestinationIsTextAuthority(t *testing.T) {
	fields := make([]string, 160)
	fields[1], fields[2] = "61", "ITEM_ETC_SCROLL_RETURN_01"
	fields[9], fields[10], fields[11], fields[12] = "3", "3", "3", "1"
	fields[118], fields[120], fields[122], fields[123] = "30000", "1", "-1", "RESURRECT"
	ref := buildItemRef(fields, nil)
	if ref.ReturnDestination != "RESURRECT" || ref.NativeFields.Get("itemParam1_29c") != 30000 || ref.NativeFields.Get("itemParam3_2a4") != -1 {
		t.Fatal("return destination inferred from numeric or name fields")
	}
}
