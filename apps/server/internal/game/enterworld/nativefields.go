/*
===========================================================================

nativefields.go - immutable numeric item-reference metadata

A presence bitmap distinguishes absent metadata from authored zero values.
JSON keeps names stable across schema changes; packed bytes stay process-local.

===========================================================================
*/
package enterworld

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
)

// NativeFields is an immutable schema-indexed RefItemData projection. Presence
// bits distinguish absent fields from zero; all float64 value bits are retained.
// The loader constructs it, and updates produce detached immutable values.
/*
================
NativeFields
================
*/
type NativeFields string

var nativeFieldNames, nativeFieldIDs = nativeFieldSchema()

/*
================
nativeFieldSchema
================
*/
func nativeFieldSchema() ([]string, map[string]int) {
	// associatedSkillId is the resolved Desc1 skill (itemdata col 119) the
	// client reads for the SRO Macro buff-item row (v1.696 006930a0: item
	// data +0x4d0 -> skill id at +0x1c). useSkillId stays the fortress
	// repair kit's countdown skill.
	names := []string{"associatedSkillId", "maxDurability", "useCooldownGroup524", "useCooldownDuration528", "useSkillId", "useSkillDurationMs", "stoneFamilyMask", "stoneTooltipMask"}
	for _, c := range itemdataRecordColumns {
		names = append(names, c.Name)
	}
	for i := 0; i < 20; i++ {
		names = append(names, "itemParam"+strconv.Itoa(i+1)+"_"+strconv.FormatInt(int64(0x29c+i*4), 16))
	}
	sort.Strings(names)
	ids := make(map[string]int)
	out := names[:0]
	for _, name := range names {
		if _, ok := ids[name]; !ok {
			ids[name] = len(out)
			out = append(out, name)
		}
	}
	return out, ids
}

/*
================
nativeFieldHeader
================
*/
func nativeFieldHeader() int { return (len(nativeFieldNames) + 7) / 8 }

/*
================
NewNativeFields
================
*/
func NewNativeFields(values map[string]float64) NativeFields {
	if len(values) == 0 {
		return ""
	}
	data := make([]byte, nativeFieldHeader()+8*len(nativeFieldNames))
	for key, v := range values {
		i, ok := nativeFieldIDs[key]
		if !ok {
			panic("unknown native item field: " + key)
		}
		data[i/8] |= 1 << uint(i%8)
		binary.LittleEndian.PutUint64(data[nativeFieldHeader()+i*8:], math.Float64bits(v))
	}
	return NativeFields(data)
}

/*
================
Lookup
================
*/
func (f NativeFields) Lookup(key string) (float64, bool) {
	i, ok := nativeFieldIDs[key]
	if !ok || len(f) == 0 || f[i/8]&(1<<uint(i%8)) == 0 {
		return 0, false
	}
	offset := nativeFieldHeader() + i*8
	return math.Float64frombits(binary.LittleEndian.Uint64([]byte(f[offset : offset+8]))), true
}

/*
================
Get
================
*/
func (f NativeFields) Get(key string) float64 { v, _ := f.Lookup(key); return v }

/*
================
With
================
*/
func (f NativeFields) With(key string, v float64) NativeFields {
	i, ok := nativeFieldIDs[key]
	if !ok {
		panic("unknown native item field: " + key)
	}
	data := []byte(f)
	if len(data) == 0 {
		data = make([]byte, nativeFieldHeader()+8*len(nativeFieldNames))
	}
	data[i/8] |= 1 << uint(i%8)
	binary.LittleEndian.PutUint64(data[nativeFieldHeader()+i*8:], math.Float64bits(v))
	return NativeFields(data)
}

/*
================
Without
================
*/
func (f NativeFields) Without(key string) NativeFields {
	i, ok := nativeFieldIDs[key]
	if !ok || len(f) == 0 {
		return f
	}
	data := []byte(f)
	data[i/8] &^= 1 << uint(i%8)
	clear(data[nativeFieldHeader()+i*8 : nativeFieldHeader()+(i+1)*8])
	for _, b := range data[:nativeFieldHeader()] {
		if b != 0 {
			return NativeFields(data)
		}
	}
	return ""
}

/*
================
wireItemFieldNames

Step 2B of #4421: the fields a browser build reads, and the only ones
ItemWireFields passes to a wire row. The packed value keeps the full
projection: server lookups (price, canSell, twoHanded and the rest)
read it through Lookup/Get, and the record cache round-trips it
through this same JSON expecting every field back. A field no client
code reads is bytes on every item row of every entry answer and
commerce seed for nothing.

The census behind this list, at the head it was taken: every
`fields.<name>`, `fields["<name>"]` and `nativeFields?.<name>` read in
apps/client-next/src, plus the key literals item-tooltip-stats passes
its throwing read(). A field joins this list only with a named client
reader; a field leaves it only when that reader goes. Every read of an
absent field in the client is guarded (`??`, `=== undefined`) except
itemTooltipStats' read(), whose whole key set is in the first group -
so a tab built before this change reads a slimmed row exactly as it
read a full one (Old-tab: safe, no protocol bump).

Groups: the equipment stat families and the durability range
(item-tooltip-stats.ts, itemMaxDurability); the tooltip and price rows
(item-tooltip, stall, storage, cos-trade, repair); the use, cooldown
and macro readers (item-cooldowns, return-scroll, sro-macro-potion);
the stone masks (item-tooltip-stone, alchemy-selection). The census
covers every path a row's fields travel: the wire `nativeFields`, the
`item.tooltip.fields` copy itemTooltipReference makes of it (read
directly and through its aliases - `f`, `fields`, `targetFields`,
`stoneFields`), and the direct `nativeFields` reads that bypass the
tooltip (mall-use, cos-timer, the macro rows); the output is pasted
in PR #4447. Making the compiler enforce this list - the client-owned
typed contract (ITEM_WIRE_FIELDS) - is #4448; until then this list is
maintained by hand against the readers named in each group.
================
*/
var wireItemFieldNames = []string{
	// Equipment stat families (item-tooltip-stats.ts).
	"varianceIntMin1c0", "varianceIntMax1c4",
	"varianceFloatMin1c8", "varianceFloatMax1cc", "varianceFloatPerPlus1d0",
	"varianceIntMin1d4", "varianceIntMax1d8", "varianceFloatPerPlus1dc",
	"varianceFloatMin1e0", "varianceFloatMax1e4", "varianceFloatPerPlus1e8",
	"varianceIntMin1ec", "varianceIntMax1f0",
	"varianceFloatMin1f4", "varianceFloatMax1f8", "varianceFloatPerPlus1fc",
	"varianceFloatMin200", "varianceFloatMax204", "varianceFloatPerPlus208",
	"varianceFloatMin20c", "varianceFloatMax210",
	"varianceFloatMin214", "varianceFloatMax218",
	"actionRange23c",
	"varianceIntMin240", "varianceIntMax244", "varianceIntMin248", "varianceIntMax24c", "varianceFloatPerPlus250",
	"varianceIntMin254", "varianceIntMax258", "varianceIntMin25c", "varianceIntMax260", "varianceFloatPerPlus264",
	"varianceFloatMin268", "varianceFloatMax26c", "varianceFloatMin270", "varianceFloatMax274",
	"varianceFloatMin278", "varianceFloatMax27c", "varianceFloatMin280", "varianceFloatMax284",
	"varianceIntMin288", "varianceIntMax28c", "varianceFloatPerPlus290",
	"varianceIntMin294", "varianceIntMax298",
	// Tooltip rows, prices and gates.
	"cashItem", "rarity", "canRepair", "price", "repairCostB4", "reviveCostB8",
	"country", "reqStr", "reqInt",
	"reqLevelType1", "requiredLevel", "maxStack", "reqGender", "itemClass", "setId",
	"maxDurability", "itemParam2_2a0", "itemParam6_2b0",
	"maxMagicOptions51c", "avatarAttachment51d",
	// Use, cooldown, macro and stone readers.
	"useCooldownGroup524", "useCooldownDuration528",
	"associatedSkillId", "useSkillId", "useSkillDurationMs",
	"canUse", "itemParam1_29c", "itemParam3_2a4", "itemParam4_2a8",
	"stoneFamilyMask", "stoneTooltipMask",
}

var wireItemFieldMask = buildWireItemFieldMask()

/*
================
buildWireItemFieldMask

A name outside the schema is a census typo: panic at package init
rather than silently never publishing the field.
================
*/
func buildWireItemFieldMask() []byte {
	mask := make([]byte, nativeFieldHeader())
	for _, name := range wireItemFieldNames {
		i, ok := nativeFieldIDs[name]
		if !ok {
			panic("wire item field outside the schema: " + name)
		}
		mask[i/8] |= 1 << uint(i%8)
	}
	return mask
}

/*
================
ItemWireFields

The wire projection of a row: a copy holding only the fields in
wireItemFieldNames. Every construction that puts NativeFields on the
wire (entry-blob rows, starter and equip WireItems, the commerce
reference seed and its deltas) passes through here; the source value
is not mutated. A row with no read field left publishes as "" and its
nativeFields key is omitted.
================
*/
func ItemWireFields(f NativeFields) NativeFields {
	if len(f) == 0 {
		return f
	}
	data := make([]byte, len(f))
	copy(data, f)
	kept := false
	for i := range nativeFieldNames {
		bit := byte(1 << uint(i%8))
		if wireItemFieldMask[i/8]&bit == 0 {
			data[i/8] &^= bit
			clear(data[nativeFieldHeader()+i*8 : nativeFieldHeader()+(i+1)*8])
			continue
		}
		if data[i/8]&bit != 0 {
			kept = true
		}
	}
	if !kept {
		return ""
	}
	return NativeFields(data)
}

/*
================
MarshalJSON

Writes the present fields as one JSON object, byte for byte what
json.Marshal of a map[string]float64 holding them writes: the schema
names are sorted and plain ASCII, so the object is the names in schema
order with no escaping, and each value is formatted as encoding/json
formats a float64 (appendJSONFloat).

Every item row of an entry blob carries one of these. Building a map
and letting encoding/json sort and reflect over it cost most of a
return-scroll arrival's encode on the live tick (#2143).
================
*/
func (f NativeFields) MarshalJSON() ([]byte, error) {
	out := make([]byte, 0, nativeFieldsJSONGuess)
	out = append(out, '{')
	for i, key := range nativeFieldNames {
		if len(f) == 0 || f[i/8]&(1<<uint(i%8)) == 0 {
			continue
		}
		offset := nativeFieldHeader() + i*8
		v := math.Float64frombits(binary.LittleEndian.Uint64([]byte(f[offset : offset+8])))
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return nil, fmt.Errorf("native item field %s: unsupported value %v", key, v)
		}
		if len(out) > 1 {
			out = append(out, ',')
		}
		out = append(out, '"')
		out = append(out, key...)
		out = append(out, '"', ':')
		out = appendJSONFloat(out, v)
	}
	return append(out, '}'), nil
}

// nativeFieldsJSONGuess sizes the object buffer: an item row carries
// about a dozen present fields of about 20 bytes each.
const nativeFieldsJSONGuess = 256

/*
================
appendJSONFloat

encoding/json's float64 format: the shortest round-trip decimal, in
exponent form only below 1e-6 or from 1e21 up, with a two-digit
negative exponent shortened (e-07 becomes e-7) as encoding/json does.
================
*/
func appendJSONFloat(out []byte, v float64) []byte {
	abs := math.Abs(v)
	format := byte('f')
	if abs != 0 && (abs < 1e-6 || abs >= 1e21) {
		format = 'e'
	}
	out = strconv.AppendFloat(out, v, format, -1, 64)
	if format == 'e' {
		n := len(out)
		if n >= 4 && out[n-4] == 'e' && out[n-3] == '-' && out[n-2] == '0' {
			out[n-2] = out[n-1]
			out = out[:n-1]
		}
	}
	return out
}

/*
================
UnmarshalJSON
================
*/
func (f *NativeFields) UnmarshalJSON(data []byte) error {
	var values map[string]float64
	if err := json.Unmarshal(data, &values); err != nil {
		return err
	}
	for key := range values {
		if _, ok := nativeFieldIDs[key]; !ok {
			return fmt.Errorf("unknown native item field: %s", key)
		}
	}
	*f = NewNativeFields(values)
	return nil
}
