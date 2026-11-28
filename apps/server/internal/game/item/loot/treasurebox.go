/*
===========================================================================

treasurebox.go - the equipment bucket a Ch'in Tomb treasure box pays from

The v1.188 server's box outcomes 5-8 (a top-degree item, normal or
sealed) pick uniformly from a bucket of tradeable equipment keyed by
degree and, for sealed items, by seal tier. Its bucket build starts at
degree 10 and steps down to the highest degree that has any item, so on
a v1.150 data set the box pays the top degree there is. This file serves
that bucket from the loot catalogue's equipment, the tradeable non-event
gear every drop already draws from.

INFERENCE: the server builds its buckets from every tradeable equipment
row; the loot catalogue holds the same set (it is generated from the drop
classes, which name every tradeable, non-event piece), so its members are
taken as the bucket.

===========================================================================
*/
package loot

import "sort"

// TreasureBoxTopDegree is where the box's bucket walk starts: degree 10,
// the v1.188 server's first bucket key (9 = degree - 1).
const TreasureBoxTopDegree = 10

// equipmentTierCount is the three classes one degree holds in both
// tables: the normal row's strength tiers, the rare row's Star, Moon and
// Sun (sealgroup.go).
const equipmentTierCount = 3

/*
================
TreasureBoxEquipment

The codenames of the highest degree at or below maxDegree with any
admitted piece, sorted. seal 0 is normal gear (every strength tier of the
degree); 1, 2 and 3 are Star, Moon and Sun. admit filters a candidate
(nil admits every one); a degree whose candidates are all refused is
stepped past like an empty one. Empty when no degree has any.
================
*/
func TreasureBoxEquipment(maxDegree, seal int, admit func(codename string) bool) []string {
	if seal < 0 || seal > equipmentTierCount {
		return nil
	}
	byDegree := map[int][]string{}
	for key, bucket := range equipment.buckets {
		if key.rare != (seal > 0) || seal > 0 && key.group%equipmentTierCount != seal-1 {
			continue
		}
		degree := key.group/equipmentTierCount + 1
		if degree > maxDegree {
			continue
		}
		for _, ref := range bucket.refs {
			if admit == nil || admit(ref.Codename) {
				byDegree[degree] = append(byDegree[degree], ref.Codename)
			}
		}
	}
	for degree := maxDegree; degree >= 1; degree-- {
		codes := byDegree[degree]
		if len(codes) == 0 {
			continue
		}
		sort.Strings(codes)
		out := codes[:0]
		for i, code := range codes {
			if i == 0 || code != codes[i-1] {
				out = append(out, code)
			}
		}
		return out
	}
	return nil
}
