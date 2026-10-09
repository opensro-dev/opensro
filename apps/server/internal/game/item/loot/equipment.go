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

widths are each table's class count (normal, rare): a group is an index
below its table's width.
================
*/
type equipmentCatalog struct {
	buckets map[equipmentKey]*equipmentBucket
	classes [2][][]classThreshold
	widths  [2]int
}

const (
	// catalogLevels is the 180 level rows every class table carries.
	catalogLevels = 180
	// vsroEquipmentWidth is a version 1 catalog's class count (vSRO's
	// _RefDropClassSel_Equip and _RareEquip both carry 36).
	vsroEquipmentWidth = 36
	// thresholdScale turns a cumulative float32 probability into the
	// million-roll threshold (7244F0).
	thresholdScale = 1_000_000
	// negligibleProbability is a class too small to be rolled.
	negligibleProbability = 0.000001
)

var equipment = mustEquipmentCatalog()

/*
================
equipmentSource

Version 1 is vSRO's fixed 36/36 layout; version 2 names each table's
width, which ISRO-R widens (its RareEquip table has 60 classes).
================
*/
type equipmentSource struct {
	Version      int
	Widths       map[string]int
	Items        []equipmentRef
	Normal, Rare [][]float32
}

/*
================
mustEquipmentCatalog
================
*/
func mustEquipmentCatalog() equipmentCatalog {
	var source equipmentSource
	if err := json.Unmarshal(equipmentJSON, &source); err != nil {
		panic(err)
	}
	c, err := compileEquipment(source)
	if err != nil {
		panic(err)
	}
	return c
}

/*
================
compileClassTable

One table's level rows as cumulative thresholds. Every row has exactly
width classes; each probability is finite and non-negative and a row sums
to at most 1. Classes below negligibleProbability are never rolled.
================
*/
func compileClassTable(rows [][]float32, width int) ([][]classThreshold, error) {
	if len(rows) != catalogLevels || width <= 0 {
		return nil, fmt.Errorf("class table needs %d level rows and a positive width", catalogLevels)
	}
	table := make([][]classThreshold, len(rows))
	for level, probabilities := range rows {
		if len(probabilities) != width {
			return nil, fmt.Errorf("level %d has %d classes, want %d", level+1, len(probabilities), width)
		}
		var sum, total float64
		var cumulative float32
		for group, p := range probabilities {
			if !probabilityValid(p) {
				return nil, fmt.Errorf("level %d class %d probability %v", level+1, group, p)
			}
			total += float64(p)
			if p <= negligibleProbability {
				continue
			}
			// float32 accumulation is the native one (7244F0).
			cumulative += p
			sum = float64(cumulative)
			table[level] = append(table[level], classThreshold{group, uint32(sum * thresholdScale)})
		}
		if total > 1+negligibleProbability {
			return nil, fmt.Errorf("level %d probabilities sum to %v", level+1, total)
		}
	}
	return table, nil
}

/*
================
compileEquipment
================
*/
func compileEquipment(source equipmentSource) (equipmentCatalog, error) {
	c := equipmentCatalog{buckets: map[equipmentKey]*equipmentBucket{}}
	switch source.Version {
	case 1:
		c.widths = [2]int{vsroEquipmentWidth, vsroEquipmentWidth}
	case 2:
		c.widths = [2]int{source.Widths["normal"], source.Widths["rare"]}
	default:
		return c, fmt.Errorf("equipment catalog version %d", source.Version)
	}
	for kind, rows := range [2][][]float32{source.Normal, source.Rare} {
		table, err := compileClassTable(rows, c.widths[kind])
		if err != nil {
			return c, fmt.Errorf("equipment table %d: %w", kind, err)
		}
		c.classes[kind] = table
	}
	seen := map[string]bool{}
	for _, r := range source.Items {
		width := c.widths[0]
		if r.Rare {
			width = c.widths[1]
		}
		if seen[r.Codename] || r.Codename == "" || r.Country > 1 || r.Group < 0 || r.Group >= width || r.Weight == 0 || r.Absolute > 100 {
			return c, fmt.Errorf("invalid equipment assignment: %+v", r)
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
	if source.Version >= 2 {
		// The v2 generator drops and logs a class with no v1.150 item; one
		// that reaches the runtime is a broken catalog.
		for kind := range c.classes {
			for level, row := range c.classes[kind] {
				for _, class := range row {
					if !c.anyCountry(class.group, kind == 1) {
						return c, fmt.Errorf("equipment table %d level %d class %d has no item", kind, level+1, class.group)
					}
				}
			}
		}
	}
	return c, nil
}

/*
================
anyCountry

Whether either country holds an item of the class.
================
*/
func (c equipmentCatalog) anyCountry(group int, rare bool) bool {
	return c.buckets[equipmentKey{0, group, rare}] != nil || c.buckets[equipmentKey{1, group, rare}] != nil
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
	width := c.widths[0]
	if rare {
		width = c.widths[1]
	}
	if country > 1 || group < 0 || group >= width || level == 0 || roll == nil {
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
