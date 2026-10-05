/*
===========================================================================

skillencoded.go - readers for a skill row's encoded parameter tail

The tail (columns 69..) is a stream of tag words, each followed by its
arguments. These helpers walk it for one fact at a time.

===========================================================================
*/

package enterworld

// encodedMultiImpactCount resolves the native cm pair at tag boundaries.
func encodedMultiImpactCount(fields []string) (uint8, bool) {
	for index := skilldataColEncodedTail; index+2 < len(fields); {
		tag, tagOK := textdataInt(fields[index])
		if !tagOK || tag == 0x73736f75 {
			return 0, false
		}
		if tag != skillMultiImpactTag {
			index += 1 + spawnParamArity(uint32(tag))
			continue
		}
		kind, kindOK := textdataInt(fields[index+1])
		count, countOK := textdataInt(fields[index+2])
		if !kindOK || kind != 2 || !countOK || count < 1 || count > 0xff {
			return 0, false
		}
		return uint8(count), true
	}
	return 0, false
}

// 84B2F0 starts at info+194[1], column 69. Consume complete blocks;
// a numeric argument can equal a tag without defining that parameter.
func encodedTailContainsTag(fields []string, wanted int64) bool {
	for index := skilldataColEncodedTail; index < len(fields); {
		value, ok := textdataInt(fields[index])
		if !ok {
			return false
		}
		if value == wanted {
			return true
		}
		if value == 0x73736f75 {
			return false
		}
		index += 1 + spawnParamArity(uint32(value))
	}
	return false
}

// textdataU32 reads an id cell; malformed or out-of-range degrades to 0,
// the shipped "no requirement" sentinel.
func textdataU32(value string) uint32 {
	v, ok := textdataInt(value)
	if !ok || v < 0 || v > 0xffffffff {
		return 0
	}
	return uint32(v)
}

// textdataDword reads a skill parameter word. The shard writes each DWORD
// as signed text, so a word above 0x7FFFFFFF (the four-week premium
// buff's 2,419,200,000 ms duration) arrives negative and means its
// two's-complement value.
func textdataDword(value string) (uint32, bool) {
	v, ok := textdataInt(value)
	if !ok || v < -0x80000000 || v > 0xffffffff {
		return 0, false
	}
	return uint32(v), true
}

// textdataNonNegative reads a count/level cell; malformed or negative
// degrades to 0.
func textdataNonNegative(value string) int64 {
	v, ok := textdataInt(value)
	if !ok || v < 0 {
		return 0
	}
	return v
}

func textdataByte(value string) (uint8, bool) {
	v, ok := textdataInt(value)
	if !ok || v < 0 || v > 0xff {
		return 0, false
	}
	return uint8(v), true
}

/*
==================
encodedEffectRider

85fb20 consumes a token when info+0x61 (column 8) is nonzero.
84b2f0 binds record+0x274 to the zero-argument efta marker.
This projection is version-specific to the admitted shipped skill table.
84B2F0's getv branch indexes its argument, not a top-level tag.
==================
*/
func encodedEffectRider(fields []string) bool {
	for i := skilldataColEncodedTail; i < len(fields); {
		n, ok := textdataInt(fields[i])
		if !ok || n == 0x73736f75 {
			return false
		}
		if n == tagGetv && i+1 < len(fields) {
			kind, ok := textdataInt(fields[i+1])
			if ok && (kind == 0x52504255 || kind == 0x53544455 || kind == 0x44544452) {
				return true
			}
		}
		i += 1 + spawnParamArity(uint32(n))
	}
	return false
}

// B5ED consumes a trailing duration for RPBU/STDU, excluding DTDR.
func encodedStealthDuration(fields []string) bool {
	for i := skilldataColEncodedTail; i < len(fields); {
		n, ok := textdataInt(fields[i])
		if !ok || n == 0x73736f75 {
			return false
		}
		if n == tagGetv && i+1 < len(fields) {
			kind, ok := textdataInt(fields[i+1])
			if ok && (kind == 0x52504255 || kind == 0x53544455) {
				return true
			}
		}
		i += 1 + spawnParamArity(uint32(n))
	}
	return false
}

// 84B2F0: descriptor references stop at ssou; parameter words are never tags.
func encodedPrimaryParameterEquals(fields []string, tag uint32, parameter int, value uint32, anyOccurrence bool) bool {
	matched := false
	for i := skilldataColEncodedTail; i < len(fields); {
		n, ok := textdataInt(fields[i])
		if !ok || n == 0x73736f75 {
			break
		}
		arity := spawnParamArity(uint32(n))
		if uint32(n) == tag && parameter < arity && i+1+parameter < len(fields) {
			matched = textdataU32(fields[i+1+parameter]) == value
			// efr=1/2/3 have independent descriptor pointers; lnks replaces one pointer.
			if anyOccurrence && matched {
				return true
			}
		}
		i += 1 + arity
	}
	return matched
}

// 84B2F0 stores one pointer per tag, so a later block replaces an earlier one.
func encodedLastParameters(fields []string, tag uint32) ([]uint32, bool) {
	var values []uint32
	found := false
	for i := skilldataColEncodedTail; i < len(fields); {
		n, ok := textdataInt(fields[i])
		if !ok || n == 0x73736f75 {
			break
		}
		arity := spawnParamArity(uint32(n))
		if uint32(n) == tag && i+arity < len(fields) {
			values = make([]uint32, arity)
			for k := range values {
				values[k] = textdataU32(fields[i+1+k])
			}
			found = true
		}
		i += 1 + arity
	}
	return values, found
}

// SkillSpeedBuff projects the hste/hst2 pointers read by 6DE630 and 6E2580.
type SkillSpeedBuff struct {
	Present, Active bool
}

func encodedSpeedBuff(fields []string) SkillSpeedBuff {
	var out SkillSpeedBuff
	for _, tag := range []uint32{0x68737465, 0x68737432} {
		if values, ok := encodedLastParameters(fields, tag); ok {
			out.Present = true
			out.Active = out.Active || len(values) > 0 && values[0] != 0
		}
	}
	return out
}

// SkillStatusLevel is a [mask, level] parameter pointer (hide, dttp).
type SkillStatusLevel struct {
	Present     bool
	Mask, Level uint32
}

func encodedStatusLevel(fields []string, tag uint32) SkillStatusLevel {
	values, ok := encodedLastParameters(fields, tag)
	if !ok || len(values) < 2 {
		return SkillStatusLevel{}
	}
	return SkillStatusLevel{Present: true, Mask: values[0], Level: values[1]}
}

// Native CSkillData +54 points at the dura duration in milliseconds.
func encodedEffectDuration(fields []string) uint32 {
	for i := skilldataColEncodedTail; i < len(fields); {
		n, ok := textdataInt(fields[i])
		if !ok || n == 0x73736f75 {
			return 0
		}
		if n == 0x64757261 && i+1 < len(fields) {
			duration, _ := textdataDword(fields[i+1])
			return duration
		}
		i += 1 + spawnParamArity(uint32(n))
	}
	return 0
}

func encodedSpawnStatus(fields []string) bool {
	return encodedTailContainsTag(fields, 0x65667461)
}
