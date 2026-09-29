/*
===========================================================================

equipment.go - immutable equipment class and weighted assignment selection

===========================================================================
*/

package loot

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
)

// Generated catalog: scripts/build/generate_loot_catalog.py. The backup's
// numeric identities never enter the runtime: every key exists in v1.150 media.
//
//go:embed .generated/equipment.json
var equipmentJSON []byte

/*
================
equipmentRef
================
*/
type equipmentRef struct {
	Count    uint16
	Codename string
	Country  uint8
	Group    int
	Rare     bool
	Type     string
	Weight   uint32
	Absolute uint32
	Level    uint8
}

/*
================
equipmentKey
================
*/
type equipmentKey struct {
	country uint8
	group   int
	rare    bool
}

/*
================
equipmentBucket
================
*/
type equipmentBucket struct {
	refs         []equipmentRef
	weights      []uint32
	alternatives map[string][]uint32
}

/*
================
classThreshold
================
*/
type classThreshold struct {
	group     int
	threshold uint32
}

/*
================
equipmentCatalog
================
*/
type equipmentCatalog struct {
	buckets map[equipmentKey]*equipmentBucket
	classes [2][][]classThreshold
}

var equipment = loadEquipmentCatalog()

/*
================
loadEquipmentCatalog
================
*/
func loadEquipmentCatalog() equipmentCatalog {
	var source struct {
		Version      int
		Items        []equipmentRef
		Normal, Rare [][]float32
	}
	if err := json.Unmarshal(equipmentJSON, &source); err != nil {
		panic(err)
	}
	if source.Version != 1 || len(source.Normal) != 180 || len(source.Rare) != 180 {
		panic("invalid equipment catalog version/levels")
	}
	c := equipmentCatalog{buckets: map[equipmentKey]*equipmentBucket{}}
	seen := map[string]bool{}
	for _, r := range source.Items {
		if seen[r.Codename] || r.Codename == "" || r.Country > 1 || r.Group < 0 || r.Group >= 36 || r.Weight == 0 || r.Absolute > 100 {
			panic(fmt.Sprintf("invalid equipment assignment: %+v", r))
		}
		seen[r.Codename] = true
		key := equipmentKey{r.Country, r.Group, r.Rare}
		b := c.buckets[key]
		if b == nil {
			b = &equipmentBucket{alternatives: map[string][]uint32{}}
			c.buckets[key] = b
		}
		weight := r.Weight
		if len(b.weights) > 0 {
			weight += b.weights[len(b.weights)-1]
		}
		b.refs = append(b.refs, r)
		b.weights = append(b.weights, weight)
		b.alternatives[r.Type] = append(b.alternatives[r.Type], uint32(len(b.refs)-1))
	}
	for kind, rows := range [2][][]float32{source.Normal, source.Rare} {
		c.classes[kind] = make([][]classThreshold, len(rows))
		for level, probabilities := range rows {
			if len(probabilities) != 36 {
				panic("invalid equipment class width")
			}
			var sum float32
			for group, p := range probabilities {
				if p < 0 || p > 1 {
					panic("invalid equipment probability")
				}
				if p <= 0.000001 {
					continue
				}
				sum += p
				c.classes[kind][level] = append(c.classes[kind][level], classThreshold{group, uint32(float64(sum) * 1_000_000)})
			}
		}
	}
	return c
}

// EquipmentGroup preserves float32 accumulation and native lower_bound
// inclusivity (7244f0 / 72e000). No per-kill table allocation or sorting.
/*
================
EquipmentGroup
================
*/
func EquipmentGroup(level uint8, rare bool, roll uint32) (int, bool) {
	kind := 0
	if rare {
		kind = 1
	}
	if level == 0 || int(level) > len(equipment.classes[kind]) || roll >= 1_000_000 {
		return 0, false
	}
	row := equipment.classes[kind][level-1]
	i := sort.Search(len(row), func(i int) bool { return row[i].threshold >= roll })
	if i == len(row) {
		return 0, false
	}
	return row[i].group, true
}

/*
================
SpecialEquipmentGroup

725600 uses a deterministic level/class map rather than the ordinary chance.
Reconstruction: the v1.150-compatible ordinary class row supplies that class;
selectEquipment retains the native lower-class fallback for unavailable tiers.
================
*/
func SpecialEquipmentGroup(level uint8) (int, bool) {
	return EquipmentGroup(level, false, 0)
}

// SelectEquipment implements 724120: weighted assignment, required-level
// fallback preserving the native type, then inclusive absolute admission.
// Missing version-specific content is never replaced by an invented item ID.
/*
================
SelectEquipment
================
*/
func SelectEquipment(country uint8, group int, rare bool, level uint8, roll func() (uint32, error)) (equipmentRef, bool) {
	return equipment.selectEquipment(country, group, rare, level, roll)
}

/*
================
selectEquipment
================
*/
func (c equipmentCatalog) selectEquipment(country uint8, group int, rare bool, level uint8, roll func() (uint32, error)) (equipmentRef, bool) {
	if country > 1 || group < 0 || group >= 36 || level == 0 || roll == nil {
		return equipmentRef{}, false
	}
	typeKey := ""
	for ; group >= 0; group-- {
		b := c.buckets[equipmentKey{country, group, rare}]
		if b == nil {
			continue
		}
		var chosen equipmentRef
		if typeKey == "" {
			r, err := roll()
			if err != nil {
				return equipmentRef{}, false
			}
			wanted := r % b.weights[len(b.weights)-1]
			i := sort.Search(len(b.weights), func(i int) bool { return b.weights[i] >= wanted })
			chosen = b.refs[i]
		} else {
			alternatives := b.alternatives[typeKey]
			if len(alternatives) == 0 {
				return equipmentRef{}, false
			}
			r, err := roll()
			if err != nil {
				return equipmentRef{}, false
			}
			chosen = b.refs[alternatives[r%uint32(len(alternatives))]]
		}
		if chosen.Level > level {
			typeKey = chosen.Type
			continue
		}
		r, err := roll()
		return chosen, err == nil && r%100 <= chosen.Absolute
	}
	return equipmentRef{}, false
}
