/*
===========================================================================

consumables.go - immutable consumable and assigned reward selection

===========================================================================
*/

package loot

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"math"
	"sort"
)

//go:embed .generated/consumables.json
var consumablesJSON []byte

/*
================
DropItem
================
*/
type DropItem struct {
	Codename  string
	Count     uint16
	Plus      uint8
	Special   bool
	NonRepair bool
	Assigned  bool
}

/*
================
consumableRef
================
*/
type consumableRef struct {
	equipmentRef
	Family int
}

/*
================
assignedDrop
================
*/
type assignedDrop struct {
	Monster, Item string
	Plus          uint8
	Min, Max      uint8
	Probability   float32
}

/*
================
assignedRandom
================
*/
type assignedRandom struct {
	Monster     string
	Group       int
	Distinct    bool
	Min, Max    uint8
	Probability float32
}

/*
================
groupDrop
================
*/
type groupDrop struct {
	Codename    string
	Probability float32
}

/*
================
consumableSource
================
*/
type consumableSource struct {
	Version int
	Widths  map[int]int
	Items   []consumableRef
	Classes map[int][][]float32
	Fixed   []assignedDrop
	Random  []assignedRandom
	Groups  map[int][]groupDrop
}

/*
================
consumableCatalog
================
*/
type consumableCatalog struct {
	families map[int]equipmentCatalog
	fixed    map[string][]assignedDrop
	random   map[string][]assignedRandom
	groups   map[int][]groupDrop
}

var consumables = mustConsumables()

/*
================
mustConsumables
================
*/
func mustConsumables() consumableCatalog {
	var source consumableSource
	if err := json.Unmarshal(consumablesJSON, &source); err != nil {
		panic(err)
	}
	c, err := compileConsumables(source)
	if err != nil {
		panic(err)
	}
	return c
}

/*
================
vsroConsumableWidths

A version 1 catalog's class counts per family: vSRO's _RefDropClassSel_*
tables (2 HP/MP, 3 cure, 4 return, 5 arrow and bolt, 6 sky scroll,
7 alchemy, 8 magic stone, 9 attribute stone, 10 reinforce).
================
*/
func vsroConsumableWidths() map[int]int {
	return map[int]int{2: 7, 3: 14, 4: 6, 5: 6, 6: 3, 7: 12, 8: 12, 9: 12, 10: 2}
}

/*
================
probabilityValid
================
*/
func probabilityValid(p float32) bool {
	return !math.IsNaN(float64(p)) && !math.IsInf(float64(p), 0) && p >= 0 && p <= 1
}

/*
================
compileConsumables
================
*/
func compileConsumables(s consumableSource) (consumableCatalog, error) {
	c := consumableCatalog{families: map[int]equipmentCatalog{}, fixed: map[string][]assignedDrop{}, random: map[string][]assignedRandom{}, groups: s.Groups}
	widths := s.Widths
	switch s.Version {
	case 1:
		widths = vsroConsumableWidths()
	case 2:
	default:
		return c, fmt.Errorf("invalid consumable catalog version")
	}
	for family, rows := range s.Classes {
		switch family {
		case 2, 3, 4, 5, 6, 7, 8, 9, 10:
		default:
			return c, fmt.Errorf("unknown consumable family %d", family)
		}
		table, err := compileClassTable(rows, widths[family])
		if err != nil {
			return c, fmt.Errorf("consumable family %d: %w", family, err)
		}
		e := equipmentCatalog{buckets: map[equipmentKey]*equipmentBucket{}, widths: [2]int{widths[family], 0}}
		e.classes[0] = table
		c.families[family] = e
	}
	seen := map[string]bool{}
	for _, r := range s.Items {
		e, ok := c.families[r.Family]
		key := fmt.Sprintf("%d/%d/%s", r.Family, r.Group, r.Codename)
		if !ok || seen[key] || r.Codename == "" || r.Group < 0 || r.Group >= e.widths[0] || r.Count == 0 || r.Weight == 0 || r.Absolute > 100 {
			return c, fmt.Errorf("invalid consumable assignment %s", key)
		}
		seen[key] = true
		k := equipmentKey{0, r.Group, false}
		b := e.buckets[k]
		if b == nil {
			b = &equipmentBucket{alternatives: map[string][]uint32{}}
			e.buckets[k] = b
		}
		weight := r.Weight
		if len(b.weights) > 0 {
			last := b.weights[len(b.weights)-1]
			if math.MaxUint32-last < weight {
				return c, fmt.Errorf("weight overflow")
			}
			weight += last
		}
		b.refs = append(b.refs, r.equipmentRef)
		b.weights = append(b.weights, weight)
		b.alternatives[r.Type] = append(b.alternatives[r.Type], uint32(len(b.refs)-1))
	}
	if s.Version >= 2 {
		// The generator drops and logs a class with no v1.150 item.
		for family, e := range c.families {
			for level, row := range e.classes[0] {
				for _, class := range row {
					if e.buckets[equipmentKey{0, class.group, false}] == nil {
						return c, fmt.Errorf("consumable family %d level %d class %d has no item", family, level+1, class.group)
					}
				}
			}
		}
	}
	for _, r := range s.Fixed {
		if r.Monster == "" || r.Item == "" || r.Min > r.Max || r.Max == 0 || !probabilityValid(r.Probability) {
			return c, fmt.Errorf("invalid assigned drop")
		}
		c.fixed[r.Monster] = append(c.fixed[r.Monster], r)
	}
	for id, rows := range s.Groups {
		if id <= 0 || len(rows) == 0 || len(rows) > 256 {
			return c, fmt.Errorf("invalid assigned group")
		}
		for _, r := range rows {
			if r.Codename == "" || !probabilityValid(r.Probability) {
				return c, fmt.Errorf("invalid group item")
			}
		}
	}
	for _, r := range s.Random {
		if r.Monster == "" || r.Min > r.Max || r.Max == 0 || !probabilityValid(r.Probability) || len(s.Groups[r.Group]) == 0 || (r.Distinct && int(r.Max) > len(s.Groups[r.Group])) {
			return c, fmt.Errorf("invalid monster random group")
		}
		c.random[r.Monster] = append(c.random[r.Monster], r)
	}
	return c, nil
}

// SelectConsumable shares the native class/weighted/absolute selection rules
// with equipment, but keeps families and quantities in separate immutable buckets.
/*
================
SelectConsumable
================
*/
func SelectConsumable(family int, level uint8, classRoll uint32, roll func() (uint32, error)) (DropItem, bool) {
	return consumables.selectConsumable(family, level, classRoll, roll)
}

/*
================
selectConsumable
================
*/
func (c consumableCatalog) selectConsumable(family int, level uint8, classRoll uint32, roll func() (uint32, error)) (DropItem, bool) {
	e, ok := c.families[family]
	if !ok || level == 0 || int(level) > len(e.classes[0]) || classRoll >= 1e6 {
		return DropItem{}, false
	}
	classes := e.classes[0][level-1]
	i := sort.Search(len(classes), func(i int) bool { return classes[i].threshold >= classRoll })
	if i == len(classes) {
		return DropItem{}, false
	}
	r, ok := e.selectEquipment(0, classes[i].group, false, level, roll)
	return DropItem{Codename: r.Codename, Count: r.Count}, ok
}

/*
================
rollMillion
================
*/
func rollMillion(roll func() (uint32, error)) (uint32, bool) {
	a, e := roll()
	if e != nil || a > 32767 {
		return 0, false
	}
	b, e := roll()
	if e != nil || b > 32767 {
		return 0, false
	}
	return ((b << 15) | a) % 1000000, true
}

// AssignedDrops handles random groups first, then fixed rows (724e30/724a00).
// Both are keyed by monster codename. Results are plans, never inventory grants.
/*
================
AssignedDrops
================
*/
func AssignedDrops(monster string, limit int, roll func() (uint32, error)) []DropItem {
	return consumables.assigned(monster, limit, roll)
}

/*
================
assigned
================
*/
func (c consumableCatalog) assigned(monster string, limit int, roll func() (uint32, error)) []DropItem {
	if roll == nil || limit <= 0 {
		return nil
	}
	var out []DropItem
	for _, r := range c.random[monster] {
		v, ok := rollMillion(roll)
		if !ok {
			return out
		}
		if v > uint32(float64(r.Probability)*1e6) {
			continue
		}
		n, e := roll()
		if e != nil {
			return out
		}
		count := int(r.Min) + int(n%(uint32(r.Max)-uint32(r.Min)+1))

		pool := c.groups[r.Group]
		selected := make([]bool, len(pool))
		for count > 0 && len(out) < limit {
			at, err := selectGroupMember(pool, selected, roll)
			if err != nil {
				return nil
			}
			out = append(out, DropItem{Codename: pool[at].Codename, Count: 1, Assigned: true})
			count--
			if r.Distinct {
				selected[at] = true
			}
		}
	}
	for _, r := range c.fixed[monster] {
		if len(out) >= limit {
			break
		}
		n, e := roll()
		if e != nil {
			return out
		}
		count := int(r.Min) + int(n%(uint32(r.Max)-uint32(r.Min)+1))
		for i := 0; i < count && len(out) < limit; i++ {
			chance, ok := rollMillion(roll)
			if !ok {
				return out
			}
			if chance <= uint32(float64(r.Probability)*1e6) {
				out = append(out, DropItem{Codename: r.Item, Count: 1, Plus: r.Plus, Assigned: true})
			}
		}
	}
	return out
}

// Native 7245c0 sets inventory capacity and category passes; 726a70 sets
// repeated class-selection attempts within a category, stopping at success.
/*
================
MonsterDropBudget
================
*/
func MonsterDropBudget(rarity uint8, code string) (capacity, passes, attempts int) {
	capacity, passes, attempts = 8, 1, 1
	switch rarity & 15 {
	case 1:
		capacity, passes = 9, 2
	case 3:
		capacity, passes, attempts = 60, 10, 30
		if code == "MOB_RM_ROC" {
			capacity = 250
		}
		if code == "MOB_TQ_WHITESNAKE" {
			attempts = 60
		}
	case 4:
		capacity, passes, attempts = 30, 5, 4
	case 5:
		capacity, passes = 60, 60
	case 6:
		capacity, passes = 20, 4
	case 7:
		capacity, passes = 30, 8
	case 8:
		capacity, passes, attempts = 60, 10, 30
	}
	if rarity>>4 == 1 {
		attempts *= 9
	}
	return
}
