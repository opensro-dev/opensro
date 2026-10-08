/*
===========================================================================

summonreference.go - authored consumable mount level variants

The item's Desc2 selects suffix thresholds, independently of the summoned
character's own level. Admission and bootstrap share this reference closure.

===========================================================================
*/
package enterworld

import (
	"sort"
	"strconv"
	"strings"
)

const (
	summonLevelEnd      = uint8(255)
	summonLevelByteMask = 0xff
)

/*
================
summonLevelByte

6A56E4 stores atoi's low byte. Its CRT strtol (9E3ACA..9E3B24) saturates
signed 32-bit overflow before that truncation, preserving numeric prefixes.
================
*/
func summonLevelByte(value string) uint8 {
	value = strings.TrimLeft(value, " \t\n\r\v\f")
	start := 0
	if len(value) > 0 && (value[0] == '-' || value[0] == '+') {
		start = 1
	}
	end := start
	for end < len(value) && value[end] >= '0' && value[end] <= '9' {
		end++
	}
	if end == start {
		return 0
	}
	// A valid numeric prefix can only return ErrRange. ParseInt supplies
	// the same saturated signed result as native strtol in that case.
	result, _ := strconv.ParseInt(value[:end], 10, 32)
	return uint8(result & summonLevelByteMask)
}

/*
================
summonLevelThresholds

RefObjItem_ReadRow 6A55FA..6A5773 enables only 3/3/3/2 Desc2 != xxx.
Its byte set starts at 1, stops parsing at zero and always ends at 255.
================
*/
func summonLevelThresholds(item *ItemRef) []uint8 {
	if item.TypeIDs != [4]int64{3, 3, 3, 2} || item.ParamDescriptions[1] == "xxx" {
		return nil
	}
	seen := map[uint8]bool{1: true, summonLevelEnd: true}
	for _, token := range strings.Split(item.ParamDescriptions[1], ",") {
		level := summonLevelByte(token)
		if level == 0 {
			break
		}
		seen[level] = true
	}
	levels := make([]uint8, 0, len(seen))
	for level := range seen {
		levels = append(levels, level)
	}
	sort.Slice(levels, func(i, j int) bool { return levels[i] < levels[j] })
	return levels
}

/*
================
SummonCharacterReference

49BBC1..49BC98 uses upper_bound(player level), refuses begin/end and
decrements before appending the selected suffix. An enabled map never falls
back to the base row or infers thresholds from CharacterRef.Level.
================
*/
func SummonCharacterReference(source CharacterRefSource, item *ItemRef, level int64) (*CharacterRef, bool) {
	if source == nil || item == nil || item.AssociatedCharacterCodename == "" {
		return nil, false
	}
	code := item.AssociatedCharacterCodename
	if len(item.SummonLevelThresholds) > 0 {
		if level < 1 || level >= int64(summonLevelEnd) {
			return nil, false
		}
		index := sort.Search(len(item.SummonLevelThresholds), func(i int) bool { return int64(item.SummonLevelThresholds[i]) > level })
		if index == 0 || index == len(item.SummonLevelThresholds) {
			return nil, false
		}
		code += "_" + strconv.Itoa(int(item.SummonLevelThresholds[index-1]))
	}
	ref, found := source.CharacterRefByCodename(code)
	return ref, found && ref != nil && ref.RefObjID != 0 && ref.Codename == code
}

/*
================
summonCharacterCodenames

Enumerate reachable references without invoking the public lazy-load door
from inside TextdataItems.load. The end sentinel is never selected.
================
*/
func summonCharacterCodenames(item *ItemRef) []string {
	if len(item.SummonLevelThresholds) == 0 {
		return []string{item.AssociatedCharacterCodename}
	}
	var codes []string
	for _, level := range item.SummonLevelThresholds {
		if level != summonLevelEnd {
			codes = append(codes, item.AssociatedCharacterCodename+"_"+strconv.Itoa(int(level)))
		}
	}
	return codes
}

/*
================
summonCharacterTypeFlags

Static item guidance has no player level. Only a consistent authored family
may supply its type; unavailable thresholds remain admission failures.
================
*/
func summonCharacterTypeFlags(source CharacterRefSource, item *ItemRef) (uint16, bool) {
	var flags uint16
	found := false
	for _, code := range summonCharacterCodenames(item) {
		ref, exists := source.CharacterRefByCodename(code)
		if !exists || ref == nil || ref.RefObjID == 0 || ref.Codename != code {
			continue
		}
		if found && flags != ref.TidWord {
			return 0, false
		}
		flags, found = ref.TidWord, true
	}
	return flags, found
}
