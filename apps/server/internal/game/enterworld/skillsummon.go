package enterworld

import "opensro.online/server/internal/game/world/monster"

// The server's parameter index indexes ssou (73736F75) and terminates ordinary parameter walking.
// Its unique-summon handler consumes nine tuples even when a reference is zero. Do not scan
// tuple operands as tags or reinterpret the unused trailing words as entries.
func encodedUniqueSummon(fields []string) monster.SummonSkill {
	var empty monster.SummonSkill
	for i := skilldataColEncodedTail; i < len(fields); {
		tag, ok := textdataInt(fields[i])
		if !ok {
			return empty
		}
		if tag != 0x73736f75 {
			i += 1 + spawnParamArity(uint32(tag))
			continue
		}
		if len(fields) < i+37 {
			return empty
		}
		hp, ok := textdataInt(fields[66])
		if !ok || hp < 0 || hp > 100 {
			return empty
		}
		result := monster.SummonSkill{Present: true, HPPercent: uint16(hp)}
		for slot := range result.Entries {
			var values [4]uint32
			for j := range values {
				n, valid := textdataInt(fields[i+1+slot*4+j])
				if !valid || n < 0 || n > 0x7fffffff {
					return empty
				}
				values[j] = uint32(n)
			}
			if values[3] < values[2] {
				return empty
			}
			result.Entries[slot] = monster.SummonEntry{RefObjID: values[0], Grade: uint8(values[1] & 15), Minimum: values[2], Maximum: values[3]}
		}
		return result
	}
	return empty
}
