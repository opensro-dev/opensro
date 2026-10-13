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

	log "github.com/sirupsen/logrus"

	"opensro.online/server/internal/game/item/wire"
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

/*
================
collectSummonable

The characterdata rows summon items reach (each item's level thresholds,
and the whole pet families retained pets advance through), published as
summonableCharacters. A reached row authored in service but refused by
buildCharacterRef (refused) is recorded for RefusedSummonRows; a threshold
with no authored row at all stays a warning. Runs inside load.
================
*/
func (t *TextdataItems) collectSummonable(refused map[string]bool) {
	summonable := make(map[string]struct{})
	for _, item := range t.byCodename {
		if item == nil || (item.TypeIDs != [4]int64{3, 3, 3, 2} && !wire.IsCosSummoner(item.TypeFlags())) || item.AssociatedCharacterCodename == "" {
			continue
		}
		for _, code := range summonCharacterCodenames(item) {
			summonable[code] = struct{}{}
		}
	}
	// Retained pets advance to later characterdata rows. Their item still
	// names the initial row, so publish the whole supported pet family.
	for code, ref := range t.charactersByCodename {
		if ref.TidWord&0x7fe == 0x1c6 && (ref.TidWord>>11 == 3 || ref.TidWord>>11 == 4 || ref.TidWord>>11 == 5) {
			summonable[code] = struct{}{}
		}
	}
	keys := make([]string, 0, len(summonable))
	for codename := range summonable {
		keys = append(keys, codename)
	}
	sort.Strings(keys)
	for _, codename := range keys {
		if ref := t.charactersByCodename[codename]; ref != nil {
			t.summonableCharacters = append(t.summonableCharacters, *ref)
		} else if refused[codename] {
			// The row is authored but unreadable: a player's summon at this
			// level fails. RefusedSummonRows lets the shipped-data test fail
			// the build on it.
			t.refusedSummonRows = append(t.refusedSummonRows, codename)
			log.Errorf("bootstrap: summon item reaches characterdata row %q, which failed validation", codename)
		} else {
			// A threshold with no authored row (the 1 every map starts at,
			// below a family's first form): 49BBC1 refuses that summon too.
			log.Warnf("bootstrap: summon item references missing characterdata row %q", codename)
		}
	}
}

/*
================
RefusedSummonRows

Characterdata rows a summon item reaches that are authored as in service
but failed buildCharacterRef. Empty on readable data; the shipped-data test
fails the build on any.
================
*/
func (t *TextdataItems) RefusedSummonRows() []string {
	t.once.Do(t.load)
	return append([]string(nil), t.refusedSummonRows...)
}
