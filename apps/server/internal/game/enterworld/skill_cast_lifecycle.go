package enterworld

import (
	"fmt"
	"math"
	"sort"
	"strconv"
)

// nativeSkillCastLinks reproduces v1.150 7E9170 -> 7E5CC0. The ordered
// skill-ID map is walked after loading. A root receives its own ID at
// extendedInfo+260; all its successors receive that ROOT, not ChainNext.
// 7754B0 filters zero/self when choosing the B245 continuation branch.
// Cycles are rejected instead of hanging the exporter on malformed custom data.
func nativeSkillCastLinks(next map[uint32]uint32) (map[uint32]uint32, error) {
	ids := make([]uint32, 0, len(next))
	for id := range next {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	roots := make(map[uint32]uint32)
	for _, id := range ids {
		if next[id] == 0 || roots[id] != 0 {
			continue
		}
		roots[id] = id
		seen := map[uint32]bool{id: true}
		for child := next[id]; child != 0; child = next[child] {
			if _, ok := next[child]; !ok {
				break
			} // Native lookup miss ends the walk.
			if seen[child] {
				return nil, fmt.Errorf("cyclic skill cast chain: root %d, skill %d", id, child)
			}
			seen[child] = true
			roots[child] = id // Preserve the native overwrite on converging chains.
		}
	}
	for id, root := range roots {
		if id == root {
			delete(roots, id)
		}
	}
	return roots, nil
}

// 84C653..84C66B: pmhp's four-word body is indexed at CSkillData+10C.
// 8DCF61..8DCF6F reads its THIRD word (+8), comparing it with 100.
// Descriptor pointers are last-write-wins and stop at ssou; numeric arguments
// are never scanned as tags. The production parameter-arity table is injected
// so this projection shares the existing loader's decoding boundary.
func nativeSkillDefersCancellation(fields []string, arity func(uint32) int) bool {
	const pmhp = uint32(0x706d6870)
	const ssou = uint32(0x73736f75)
	end := len(fields)
	if end > 118 {
		end = 118
	}
	matched := false
	for i := 69; i < end; {
		n, err := strconv.ParseInt(fields[i], 10, 64)
		// A tag cell is one DWORD, written signed or unsigned; anything
		// wider is malformed rather than a wrapped tag.
		if err != nil || n < math.MinInt32 || n > math.MaxUint32 {
			return false
		}
		tag := uint32(n)
		if tag == ssou {
			break
		}
		size := arity(tag)
		if size < 0 || i+1+size > end {
			return false
		}
		if tag == pmhp {
			if size != 4 {
				return false
			}
			kind, err := strconv.ParseInt(fields[i+3], 10, 64)
			matched = err == nil && kind == 100
		}
		i += 1 + size
	}
	return matched
}
