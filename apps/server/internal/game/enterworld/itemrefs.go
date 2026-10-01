/*
===========================================================================

itemrefs.go - loading itemdata textdata into item references

===========================================================================
*/

package enterworld

import (
	"encoding/binary"
	"math"
	"opensro.online/server/internal/data/recordcache"
	"opensro.online/server/internal/data/texttable"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode/utf16"

	"opensro.online/server/internal/game/world/monster"

	log "github.com/sirupsen/logrus"
)

/*
==================
TextdataItems

TextdataItems is the ItemRefSource over the extracted tab-separated
Silkroad textdata (the server_dep form of the _REF* tables), the same
source the (retired) Node launcher-api read through referenceData.mjs. Lazy +
cached; degrades to empty when the textdata is not present, so the server
still boots (and the equip roster behaves like MISSION_EQUIP_ITEMS=0).
==================
*/
type TextdataItems struct {
	archive  *recordcache.Cache[*ItemRef]
	nameKeys map[string]uint32
	idKeys   map[uint32]uint32
	dir      string

	once                 sync.Once
	byCodename           map[string]*ItemRef
	byID                 map[uint32]*ItemRef
	charactersByCodename map[string]*CharacterRef
	summonableCharacters []CharacterRef
}

// NewTextdataItems returns a lazy loader over dir (itemdata*.txt +
// textdataname.txt).
/*
================
NewTextdataItems
================
*/
func NewTextdataItems(dir string) *TextdataItems {
	return &TextdataItems{dir: dir}
}

// ItemRefByCodename implements ItemRefSource.
/*
================
ItemRefByCodename
================
*/
func (t *TextdataItems) ItemRefByCodename(codename string) (*ItemRef, bool) {
	t.once.Do(t.load)
	if t.archive != nil {
		key, ok := t.nameKeys[codename]
		if !ok {
			return nil, false
		}
		return t.archive.Get(key)
	}
	row, ok := t.byCodename[codename]
	return row, ok
}

/*
================
ItemRefByID
================
*/
func (t *TextdataItems) ItemRefByID(id uint32) (*ItemRef, bool) {
	t.once.Do(t.load)
	if t.archive != nil {
		key, ok := t.idKeys[id]
		if !ok {
			return nil, false
		}
		return t.archive.Get(key)
	}
	row, ok := t.byID[id]
	return row, ok
}

// Public immutable item identity/type data used by the native GM composer.
/*
================
ItemCommandReference
================
*/
type ItemCommandReference struct {
	RefObjID  uint32 `json:"refObjId"`
	Codename  string `json:"codename"`
	TypeFlags uint16 `json:"typeFlags"`
	MaxStack  uint16 `json:"maxStack"`
}

/*
================
ItemCommandReferences
================
*/
func (t *TextdataItems) ItemCommandReferences() []ItemCommandReference {
	t.once.Do(t.load)
	rows := make([]ItemCommandReference, 0, t.Len())
	for r := range t.itemRows() {
		cap := r.NativeFields.Get("maxStack")
		if cap < 1 {
			cap = 1
		}
		if cap > 65535 {
			cap = 65535
		}
		rows = append(rows, ItemCommandReference{r.RefObjID, r.Codename, r.TypeFlags(), uint16(cap)})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].RefObjID < rows[j].RefObjID })
	return rows
}

// CharacterRefByCodename implements CharacterRefSource.
/*
================
CharacterRefByCodename
================
*/
func (t *TextdataItems) CharacterRefByCodename(codename string) (*CharacterRef, bool) {
	t.once.Do(t.load)
	row, ok := t.charactersByCodename[codename]
	return row, ok
}

// SummonableCharacterRefs implements CharacterRefSource. The returned slice
// is detached so callers cannot mutate the shared media cache.
/*
================
SummonableCharacterRefs
================
*/
func (t *TextdataItems) SummonableCharacterRefs() []CharacterRef {
	t.once.Do(t.load)
	return append([]CharacterRef(nil), t.summonableCharacters...)
}

// Len reports how many itemdata rows loaded (0 = textdata absent).
/*
================
Len
================
*/
func (t *TextdataItems) Len() int {
	t.once.Do(t.load)
	if t.archive != nil {
		return t.archive.Len()
	}
	return len(t.byCodename)
}

/*
================
load
================
*/
func (t *TextdataItems) load() {
	t.byCodename = map[string]*ItemRef{}
	t.byID = map[uint32]*ItemRef{}
	defer func() {
		for r := range t.itemRows() {
			t.byID[r.RefObjID] = r
		}
	}()
	t.charactersByCodename = map[string]*CharacterRef{}
	entries, err := os.ReadDir(t.dir)
	if err != nil {
		log.Warnf("bootstrap: textdata not found in verified projection at %s; item reference lookups will be empty", t.dir)
		return
	}

	// SN_* symbol -> English display name (textdataname.txt: fields[1] =
	// symbol, fields[8] = the English column of the 9-language row).
	names := map[string]string{}
	for _, fields := range readTextdataFile(filepath.Join(t.dir, "textdataname.txt")) {
		if len(fields) < 9 || !strings.HasPrefix(fields[1], "SN_") {
			continue
		}
		english := strings.TrimSpace(fields[8])
		names[fields[1]] = english
	}

	rows := 0
	for _, entry := range entries {
		lower := strings.ToLower(entry.Name())
		if entry.IsDir() || !strings.HasPrefix(lower, "itemdata") || !strings.HasSuffix(lower, ".txt") {
			continue
		}
		for _, fields := range readTextdataFile(filepath.Join(t.dir, entry.Name())) {
			ref := buildItemRef(fields, names)
			if ref != nil {
				t.byCodename[ref.Codename] = ref
				rows++
			}
		}
	}

	characterRows := 0
	for _, entry := range entries {
		lower := strings.ToLower(entry.Name())
		if entry.IsDir() || !strings.HasPrefix(lower, "characterdata") || !strings.HasSuffix(lower, ".txt") {
			continue
		}
		for _, fields := range readTextdataFile(filepath.Join(t.dir, entry.Name())) {
			ref := buildCharacterRef(fields, names)
			if ref == nil {
				continue
			}
			t.charactersByCodename[ref.Codename] = ref
			characterRows++
		}
	}

	summonable := make(map[string]struct{})
	for _, item := range t.byCodename {
		if item == nil || item.TypeIDs != [4]int64{3, 3, 3, 2} || item.AssociatedCharacterCodename == "" {
			continue
		}
		summonable[item.AssociatedCharacterCodename] = struct{}{}
	}
	keys := make([]string, 0, len(summonable))
	for codename := range summonable {
		keys = append(keys, codename)
	}
	sort.Strings(keys)
	for _, codename := range keys {
		if ref := t.charactersByCodename[codename]; ref != nil {
			t.summonableCharacters = append(t.summonableCharacters, *ref)
		} else {
			log.Warnf("bootstrap: summon item references missing characterdata row %q", codename)
		}
	}
	log.Infof("bootstrap: textdata loaded from %s (%d item row(s), %d character row(s), %d summonable COS row(s))", t.dir, rows, characterRows, len(t.summonableCharacters))
}

/*
================
characterTidWord
================
*/
func characterTidWord(fields []string) (uint16, bool) {
	if len(fields) <= 12 {
		return 0, false
	}
	charBit, ok0 := textdataInt(fields[8])
	tid1, ok1 := textdataInt(fields[9])
	tid2, ok2 := textdataInt(fields[10])
	tid3, ok3 := textdataInt(fields[11])
	tid4, ok4 := textdataInt(fields[12])
	if !ok0 || !ok1 || !ok2 || !ok3 || !ok4 || charBit < 0 || tid1 < 0 || tid2 < 0 || tid3 < 0 || tid4 < 0 || tid4 > 0x1f {
		return 0, false
	}
	// Bits 11..15 distinguish attack, transport and collection COS. The
	// shared class discriminator alone cannot select immunity or pet AI.
	word := uint16((tid1&7)<<2 | (tid2&3)<<5 | (tid3&0xf)<<7 | tid4<<11)
	if charBit != 0 {
		word |= 0x0002
	}
	return word, true
}

/*
================
buildCharacterRef
================
*/
func buildCharacterRef(fields []string, names map[string]string) *CharacterRef {
	const lastRequiredColumn = 88
	if len(fields) <= lastRequiredColumn || strings.TrimSpace(fields[0]) != "1" {
		return nil
	}
	refObjID, ok := textdataInt(fields[1])
	if !ok || refObjID <= 0 || refObjID > int64(^uint32(0)) {
		return nil
	}
	tidWord, ok := characterTidWord(fields)
	if !ok {
		return nil
	}
	walk, walkOK := textdataFloat(fields[46])
	run, runOK := textdataFloat(fields[47])
	scale, scaleOK := textdataFloat(fields[48])
	level, levelOK := textdataInt(fields[57])
	maxHP, maxHPOK := textdataInt(fields[59])
	maxMP, maxMPOK := textdataInt(fields[60])
	capability, capabilityOK := textdataInt(fields[88])
	if !walkOK || !runOK || !scaleOK || walk < 0 || run < 0 || scale <= 0 ||
		!levelOK || level < 0 || level > 0xff || !maxHPOK || maxHP <= 0 || maxHP > int64(^uint32(0)) ||
		!maxMPOK || maxMP < 0 || maxMP > int64(^uint32(0)) ||
		!capabilityOK || capability < 0 || capability > int64(^uint32(0)) {
		return nil
	}
	nameStrID := strings.TrimSpace(fields[5])
	return &CharacterRef{
		Parameters:                 monster.CharacterParameters(fields),
		RefObjID:                   uint32(refObjID),
		TidWord:                    tidWord,
		Codename:                   strings.TrimSpace(fields[2]),
		NameStrID:                  nameStrID,
		Name:                       names[nameStrID],
		WalkSpeed:                  float32(walk),
		RunSpeed:                   float32(run),
		Scale:                      float32(scale),
		Level:                      uint8(level),
		MaxHP:                      uint32(maxHP),
		MaxMP:                      uint32(maxMP),
		MountedAttackCapability210: uint32(capability),
	}
}

/*
==================
itemdataRecordColumn

itemdataRecordColumn is one v1.150 itemdata projection entry: media column
index -> named native RefItemData field. kind "int": native fild (signed
integer, media text float-formatted so parse via trunc); "float": native
fld float32; "permille": stored as parse/1000.0 by the native row parser.
==================
*/
type itemdataRecordColumn struct {
	Index int
	Name  string
	Kind  string
}

// itemdataRecordColumns is the current v1.150 projection proven by the
// tooltip status-C evidence. It includes the full native requirement quad
// (RefItemData +0xc8..+0xd4 types / +0xd8..+0xe4 values); the names
// extend the 32/33 precedent as reqLevelTypeN/requiredLevelN, keeping
// the historical unsuffixed "requiredLevel" for slot 1.
var itemdataRecordColumns = []itemdataRecordColumn{
	{14, "country", "int"},
	{15, "rarity", "int"},
	// 808AD0 reads token 17 into RefObjData+A5; 5B6D22 tests this byte
	// before merchant sale. Shop membership is not a sale permission.
	{17, "canSell", "int"},
	// Token 19 (RefObjData+0xA7): bit 0x80 admits the item to the warehouse
	// (CIFStorage_OnSlotTransfer; 0 refuses with notice 1:0x43).
	{19, "canBorrow", "int"},
	// Authored CanUse flags: bit 0 admits direct activation; other bits
	// describe additional behavior (pet skill rows carry 129, not just 1).
	{24, "canUse", "int"},
	{26, "price", "int"},
	// Native RefItemData+0xb4/+0xb8. These are consumed independently by
	// sub_789630: CostRepair prices missing durability points; CostRevive is
	// the extra charge when current durability is zero.
	{27, "repairCostB4", "int"},
	{28, "reviveCostB8", "int"},
	// Token 30: the per-unit gold a warehouse deposit charges.
	{30, "keepingFee", "int"},
	{31, "sellPrice", "int"},
	{32, "reqLevelType1", "int"},
	{33, "requiredLevel", "int"},
	{34, "reqLevelType2", "int"},
	{35, "requiredLevel2", "int"},
	{36, "reqLevelType3", "int"},
	{37, "requiredLevel3", "int"},
	{38, "reqLevelType4", "int"},
	{39, "requiredLevel4", "int"},
	{57, "maxStack", "int"},
	{58, "reqGender", "int"},
	{59, "reqStr", "int"},
	{60, "reqInt", "int"},
	{61, "itemClass", "int"},
	{62, "setId", "int"},
	{63, "varianceIntMin1c0", "int"},
	{64, "varianceIntMax1c4", "int"},
	{65, "varianceFloatMin1c8", "float"},
	{66, "varianceFloatMax1cc", "float"},
	{67, "varianceFloatPerPlus1d0", "float"},
	{68, "varianceIntMin1d4", "int"},
	{69, "varianceIntMax1d8", "int"},
	{70, "varianceFloatPerPlus1dc", "float"},
	{71, "varianceFloatMin1e0", "float"},
	{72, "varianceFloatMax1e4", "float"},
	{73, "varianceFloatPerPlus1e8", "float"},
	{74, "varianceIntMin1ec", "int"},
	{75, "varianceIntMax1f0", "int"},
	{76, "varianceFloatMin1f4", "float"},
	{77, "varianceFloatMax1f8", "float"},
	{78, "varianceFloatPerPlus1fc", "float"},
	{79, "varianceFloatMin200", "float"},
	{80, "varianceFloatMax204", "float"},
	{81, "varianceFloatPerPlus208", "float"},
	{82, "varianceFloatMin20c", "permille"},
	{83, "varianceFloatMax210", "permille"},
	{84, "varianceFloatMin214", "permille"},
	{85, "varianceFloatMax218", "permille"},
	{86, "quivered", "int"},
	{87, "ammo1Tid4", "int"},
	{88, "ammo2Tid4", "int"},
	{89, "ammo3Tid4", "int"},
	{90, "ammo4Tid4", "int"},
	{91, "ammo5Tid4", "int"},
	{92, "speedClass", "int"},
	{93, "twoHanded", "int"},
	{94, "actionRange23c", "int"},
	{95, "varianceIntMin240", "int"},
	{96, "varianceIntMax244", "int"},
	{97, "varianceIntMin248", "int"},
	{98, "varianceIntMax24c", "int"},
	{99, "varianceFloatPerPlus250", "float"},
	{100, "varianceIntMin254", "int"},
	{101, "varianceIntMax258", "int"},
	{102, "varianceIntMin25c", "int"},
	{103, "varianceIntMax260", "int"},
	{104, "varianceFloatPerPlus264", "float"},
	{105, "varianceFloatMin268", "permille"},
	{106, "varianceFloatMax26c", "permille"},
	{107, "varianceFloatMin270", "permille"},
	{108, "varianceFloatMax274", "permille"},
	{109, "varianceFloatMin278", "permille"},
	{110, "varianceFloatMax27c", "permille"},
	{111, "varianceFloatMin280", "permille"},
	{112, "varianceFloatMax284", "permille"},
	{113, "varianceIntMin288", "int"},
	{114, "varianceIntMax28c", "int"},
	{115, "varianceFloatPerPlus290", "float"},
	{116, "varianceIntMin294", "int"},
	{117, "varianceIntMax298", "int"},
	// Native RefItemData+0x29C/+0x2A4, itemdata's Param1 and Param3. The
	// record's param block is 20 contiguous ints followed by 20 contiguous
	// description strings (sub_64DF40 walks them as two separate runs), so the
	// params are NOT interleaved with their labels: Param1 is +0x29C, Param2
	// is +0x2A0 and Param3 is +0x2A4. Itemdata pairs them as (Param, Desc)
	// columns, which is why Param3 is column 122 rather than 120.
	// sub_6E6E00 kind 3 reads Param1 as SECONDS and Param2 as a millisecond
	// limit, but the column's unit is per item family - minutes for a COS
	// summoner (ITEM_COS_P_EXTENSION_1D carries 1440), milliseconds for a
	// recall scroll (30000 against the 30-second retail return), seconds for
	// the ITEM_MALL_PET_SKILL_* and GOLD_TIME_SERVICE families that match the
	// kind-3 contract. Publish both unscaled and let the consumer decide.
	{118, "itemParam1_29c", "int"},
	{122, "itemParam3_2a4", "int"},
	// 80BFAC/80BFBC store the two tokens after the 20 (value,label)
	// pairs. 8093C0 returns CItemData+8, hence record offsets 51C/51D.
	{158, "maxMagicOptions51c", "byte"},
	{159, "avatarAttachment51d", "byte"},
}

// itemdataMaxDurabilityColumn: the friendly maxDurability alias is the Dur_U
// column (varianceIntMax1c4).
const itemdataMaxDurabilityColumn = 64

/*
================
buildItemRef
================
*/
func buildItemRef(fields []string, names map[string]string) *ItemRef {
	if len(fields) < 13 {
		return nil
	}
	refObjID, ok := textdataInt(fields[1])
	codename := fields[2]
	if !ok || refObjID < 0 || refObjID > int64(^uint32(0)) || !strings.HasPrefix(codename, "ITEM_") {
		return nil
	}

	ref := &ItemRef{
		RefObjID:          uint32(refObjID),
		Codename:          codename,
		NameStrID:         fields[5],
		DescriptionSymbol: strings.TrimSpace(fields[6]),
		// Requirement pass-defaults for absent/malformed columns: country 3
		// (all), sex 2 (unisex), stat floors 0, quad types -1 (empty slot).
		Country:       3,
		RequiredSex:   2,
		ReqQuadTypes:  [4]int64{-1, -1, -1, -1},
		ReqQuadValues: [4]int64{},
	}
	for i := range ref.ParamDescriptions {
		column := 119 + i*2
		if column < len(fields) {
			ref.ParamDescriptions[i] = strings.TrimSpace(fields[column])
		}
	}
	if len(fields) > 119 {
		ref.AssociatedCharacterCodename = strings.TrimSpace(fields[119])
	}
	for i := 0; i < 4; i++ {
		if v, ok := textdataInt(fields[9+i]); ok {
			ref.TypeIDs[i] = v
		}
	}
	// Desc1 (RefItem+0x2A4) names the skill a scroll (TID3 13) or a
	// monster mask (CGItemMonsterCapsule, TID 3/2/2; 493C30) casts.
	capsule := ref.TypeIDs[0] == 3 && ref.TypeIDs[1] == 2 && ref.TypeIDs[2] == 2
	if (ref.TypeIDs[2] == 13 || capsule) && len(fields) > 119 {
		ref.AssociatedSkillCodename = strings.TrimSpace(fields[119])
	}
	// The typed equip-requirement columns (see ItemRef field docs). All
	// four typed quad pairs (32..39) also land in the Record JSON via
	// itemdataRecordColumns as reqLevelType1/requiredLevel +
	// reqLevelType2..4/requiredLevel2..4.
	if len(fields) > 14 {
		if v, ok := textdataInt(fields[14]); ok {
			ref.Country = v
		}
	}
	// The four typed requirement pairs: types at 32/34/36/38, values at
	// 33/35/37/39 (interleaved; verified against the shipped rows - CH
	// heavy [1,-1,-1,-1]/[35,0,0,0], EU light [513,515,518,-1]/zeros).
	for slot := 0; slot < 4; slot++ {
		typeColumn := 32 + slot*2
		if len(fields) > typeColumn {
			if v, ok := textdataInt(fields[typeColumn]); ok {
				ref.ReqQuadTypes[slot] = v
			}
		}
		if len(fields) > typeColumn+1 {
			if v, ok := textdataInt(fields[typeColumn+1]); ok {
				ref.ReqQuadValues[slot] = v
			}
		}
	}
	if len(fields) > 58 {
		if v, ok := textdataInt(fields[58]); ok {
			ref.RequiredSex = v
		}
	}
	if len(fields) > 60 {
		if v, ok := textdataInt(fields[59]); ok {
			ref.RequiredStr = v
		}
		if v, ok := textdataInt(fields[60]); ok {
			ref.RequiredInt = v
		}
	}
	// The first .ddj path field is the icon (the .bsr fields before it are
	// the world/drop models); the LAST .bsr is the AssocFileDrop ground
	// model (native RefItemData+0x138).
	// v1.150 itemdata column 54 is AssocFileIcon; do not infer from models.
	if len(fields) > 54 {
		ref.Icon = strings.TrimSpace(fields[54])
	}
	if name, ok := names[ref.NameStrID]; ok {
		ref.Name = name
	}
	ref.NativeFields = buildItemNativeFields(fields)
	if len(fields) > 123 && ref.TypeIDs == [4]int64{3, 3, 3, 1} {
		ref.ReturnDestination = strings.TrimSpace(fields[123])
	}
	ref.Combat = buildItemCombatRef(fields)
	if len(fields) > 63 {
		if v, ok := textdataFloat(fields[63]); ok {
			min1c0 := int64(v)
			ref.VarianceIntMin1c0 = &min1c0
		}
	}
	if len(fields) > itemdataMaxDurabilityColumn {
		if v, ok := textdataFloat(fields[itemdataMaxDurabilityColumn]); ok {
			ref.MaxDurability = int64(v)
		}
	}
	// Potion recovery parameters are authoritative SERVER gameplay inputs,
	// not part of the browser RefItemData field projection. The adjacent media label columns pin
	// these cells in the shipped v1.150 rows:
	//   118 HP amount, 120 HP percent, 122 MP amount, 124 MP percent.
	if len(fields) > 118 {
		ref.RecoveryHP, _ = textdataFloat(fields[118])
	}
	if len(fields) > 120 {
		ref.RecoveryHPPercent, _ = textdataFloat(fields[120])
	}
	if len(fields) > 122 {
		ref.RecoveryMP, _ = textdataFloat(fields[122])
	}
	if len(fields) > 124 {
		ref.RecoveryMPPercent, _ = textdataFloat(fields[124])
	}
	if len(fields) > 124 {
		if v, ok := textdataInt(fields[124]); ok {
			ref.FortressRoleMask = v
		}
	}
	// 49AC50 / 49B710 read the SERVER record's Param1..Param6 at +2A0 with a
	// 0x20 stride - the same slots as the potion amounts above, so ParamN is
	// column 118+2(N-1). The shipped labels pin the roles: the universal pill
	// (ITEM_ETC_CURE_RANDOM_*) carries the curable mask, curable level and
	// chance in Param1..3; ITEM_ETC_CURE_ALL_* carries freeze, frostbite, burn,
	// shock, poison and zombie cure points in Param1..6.
	for i := range ref.CureLevels {
		if column := 118 + i*2; len(fields) > column {
			if v, ok := textdataInt(fields[column]); ok {
				ref.CureLevels[i] = v
			}
		}
	}
	ref.CureMask, ref.CureGradeSub, ref.CureChance = ref.CureLevels[0], ref.CureLevels[1], ref.CureLevels[2]
	return ref
}

/*
==================
buildItemCombatRef

buildItemCombatRef pins the columns consumed by the native
CSOItem_ApplyOneVarianceStat branches. The record is deliberately
all-or-none: a malformed combat cell must make a live cast fail closed,
not turn one missing defense or attack contribution into zero.
==================
*/
func buildItemCombatRef(fields []string) *ItemCombatRef {
	const lastCombatColumn = 117
	if len(fields) <= lastCombatColumn {
		return nil
	}
	read := func(index int) (float64, bool) {
		value, ok := textdataFloat(fields[index])
		return value, ok && !math.IsNaN(value) && !math.IsInf(value, 0)
	}
	values := make(map[int]float64, 36)
	for _, index := range []int{
		65, 66, 67,
		68, 69, 70,
		71, 72, 73,
		74, 75,
		76, 77, 78,
		79, 80, 81,
		82, 83, 84, 85,
		94,
		95, 96, 97, 98, 99,
		100, 101, 102, 103, 104,
		105, 106, 107, 108, 109, 110, 111, 112,
		113, 114, 115,
		116, 117,
	} {
		value, ok := read(index)
		if !ok {
			return nil
		}
		values[index] = value
	}
	stat := func(min, max, perPlus int) ItemStatRange {
		out := ItemStatRange{Min: values[min], Max: values[max]}
		if perPlus >= 0 {
			out.PerPlus = values[perPlus]
		}
		return out
	}
	attack := func(min1, max1, min2, max2, perPlus int) ItemAttackRange {
		return ItemAttackRange{
			Minimum: ItemStatRange{
				Min: values[min1], Max: values[max1], PerPlus: values[perPlus],
			},
			Maximum: ItemStatRange{
				Min: values[min2], Max: values[max2], PerPlus: values[perPlus],
			},
		}
	}
	// The v1.150 table stores reinforcement in permille and has no
	// reinforcement-per-plus columns. Keep the native float32 row parse.
	reinforcement := func(first int) ItemStatRange {
		return ItemStatRange{
			Min: float64(float32(values[first] / 1000)),
			Max: float64(float32(values[first+1] / 1000)),
		}
	}
	return &ItemCombatRef{
		ActionRange:                  values[94],
		PhysicalReinforcement:        ItemAttackRange{Minimum: reinforcement(105), Maximum: reinforcement(107)},
		MagicalReinforcement:         ItemAttackRange{Minimum: reinforcement(109), Maximum: reinforcement(111)},
		PhysicalDefenseReinforcement: reinforcement(82),
		MagicalDefenseReinforcement:  reinforcement(84),
		PhysicalDefense:              stat(65, 66, 67),
		EvasionRate:                  stat(68, 69, 70),
		ParryRate:                    stat(71, 72, 73),
		BlockRate:                    stat(74, 75, -1),
		MagicalDefense:               stat(76, 77, 78),
		MagicalParry:                 stat(79, 80, 81),
		PhysicalAttack:               attack(95, 96, 97, 98, 99),
		MagicalAttack:                attack(100, 101, 102, 103, 104),
		HitRate:                      stat(113, 114, 115),
		CriticalRate:                 stat(116, 117, -1),
	}
}

// buildItemNativeFields renders the finite numeric RefItemData projection.
// Non-finite or absent source cells stay absent instead of fabricating PK2 data.
/*
================
buildItemNativeFields
================
*/
func buildItemNativeFields(fields []string) NativeFields {
	values := make(map[string]float64, len(itemdataRecordColumns)+1)
	for _, column := range itemdataRecordColumns {
		if column.Index >= len(fields) {
			continue
		}
		value, ok := textdataFloat(fields[column.Index])
		if !ok {
			continue
		}
		switch column.Kind {
		case "byte":
			values[column.Name] = float64(uint8(int64(value)))
		case "int":
			values[column.Name] = float64(int64(value))
		case "permille":
			values[column.Name] = value / 1000
		default:
			values[column.Name] = value
		}
	}
	for i := 0; i < 20; i++ {
		column := 118 + i*2
		if column < len(fields) {
			if value, ok := textdataInt(fields[column]); ok {
				values["itemParam"+strconv.Itoa(i+1)+"_"+strconv.FormatInt(int64(0x29c+i*4), 16)] = float64(value)
			}
		}
	}
	if itemdataMaxDurabilityColumn < len(fields) {
		if value, ok := textdataFloat(fields[itemdataMaxDurabilityColumn]); ok {
			values["maxDurability"] = float64(int64(value))
		}
	}
	return NewNativeFields(values)
}

// textdataInt ports referenceData's toInt: Number(value) must be an integer.
/*
================
textdataInt
================
*/
func textdataInt(value string) (int64, bool) {
	// Plain decimal cells dominate the shipped tables (27k skill rows x 118
	// cells). Parse them directly; within +-2^53 the result is exactly what
	// the float path below returns, and anything else takes that path.
	if v, ok := plainDecimal(value); ok {
		return v, true
	}
	f, ok := textdataFloat(value)
	if !ok || f != float64(int64(f)) {
		return 0, false
	}
	return int64(f), true
}

// plainDecimal accepts [+-]digits with no surrounding space and a magnitude
// of at most 2^53, the range where float64 represents every integer.
/*
================
plainDecimal
================
*/
func plainDecimal(value string) (int64, bool) {
	s := value
	negative := false
	if len(s) > 0 && (s[0] == '-' || s[0] == '+') {
		negative = s[0] == '-'
		s = s[1:]
	}
	if len(s) == 0 || len(s) > 16 {
		return 0, false
	}
	var v int64
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return 0, false
		}
		v = v*10 + int64(c-'0')
	}
	if v > 1<<53 {
		return 0, false
	}
	if negative {
		v = -v
	}
	return v, true
}

// textdataFloat ports the Number(value)/isFinite reads ("42.0" is a legal
// integer cell in the media text).
/*
================
textdataFloat
================
*/
func textdataFloat(value string) (float64, bool) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return 0, false
	}
	f, err := strconv.ParseFloat(trimmed, 64)
	if err != nil {
		return 0, false
	}
	return f, true
}

/*
==================
ReadTextdataFile

ReadTextdataFile is the exported face of readTextdataFile for sibling
lanes that load their own shipped tables (internal/game/quest's questdata /
questcontentsdata catalog) - one decoder, so a media re-encode can
never split the lanes' readings of the same file.
==================
*/
func ReadTextdataFile(path string) [][]string {
	return readTextdataFile(path)
}

/*
==================
readTextdataFile

readTextdataFile reads one tab-separated textdata file, tolerant of the
encodings Silkroad releases ship (UTF-16LE with BOM, a null-heavy UTF-16LE
body, or UTF-8), skipping blank and // comment lines.
==================
*/
func readTextdataFile(path string) [][]string {
	buffer, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	text := decodeTextdata(buffer)
	cells := make(texttable.Cells)
	var rows [][]string
	for _, rawLine := range strings.Split(text, "\n") {
		line := strings.TrimRight(rawLine, "\r\t ")
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}
		rows = append(rows, cells.Split(line))
	}
	return rows
}

/*
================
decodeTextdata
================
*/
func decodeTextdata(buffer []byte) string {
	if len(buffer) >= 2 && buffer[0] == 0xff && buffer[1] == 0xfe {
		return decodeUTF16LE(buffer[2:])
	}
	sampled := len(buffer)
	if sampled > 512 {
		sampled = 512
	}
	nulls := 0
	for i := 0; i < sampled; i++ {
		if buffer[i] == 0x00 {
			nulls++
		}
	}
	if sampled > 0 && float64(nulls)/float64(sampled) > 0.2 {
		return decodeUTF16LE(buffer)
	}
	return string(buffer)
}

/*
================
decodeUTF16LE
================
*/
func decodeUTF16LE(buffer []byte) string {
	units := make([]uint16, 0, len(buffer)/2)
	for i := 0; i+1 < len(buffer); i += 2 {
		units = append(units, binary.LittleEndian.Uint16(buffer[i:i+2]))
	}
	return string(utf16.Decode(units))
}
