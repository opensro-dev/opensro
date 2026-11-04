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
	names := []string{"maxDurability", "useCooldownGroup524", "useCooldownDuration528", "useSkillId", "useSkillDurationMs", "stoneFamilyMask", "stoneTooltipMask"}
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
