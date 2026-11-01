/*
===========================================================================

odds.go - exact selection chances, read from the tables without rolling

The website's catalogue publishes how often each item drops. Sampling the
planner would miss the rare rows (a Seal item is a one-in-a-million event),
so these functions walk the same tables the selectors roll on and count
the integer sample space each outcome owns, modulo bias included: a 15-bit
draw is 32768 equally likely values, a million-roll is 2^30 combined values
reduced modulo 1,000,000. Each function mirrors one selector in this
package; a test samples both and holds them together.

===========================================================================
*/
package loot

import (
	"fmt"
	"sort"
)

/*
================
Chance

One item's chance from one selection, and the stack it drops as.
================
*/
type Chance struct {
	Codename string
	Count    uint16
	Chance   float64
}

/*
================
moduloRangeMass

Inputs of 0..samples-1 whose remainder lies in lo..hi.
================
*/
func moduloRangeMass(samples, denominator, lo, hi uint64) uint64 {
	if hi < lo {
		return 0
	}
	mass := moduloAcceptanceMass(samples, denominator, hi)
	if lo > 0 {
		mass -= moduloAcceptanceMass(samples, denominator, lo-1)
	}
	return mass
}

/*
================
millionChance

The chance rollMillion (and the planner's rollCombinedMillion) lands at or
below threshold.
================
*/
func millionChance(threshold uint32) float64 {
	return float64(moduloAcceptanceMass(combinedRange, probabilityScale, uint64(threshold))) / float64(combinedRange)
}

/*
================
classChances

The chance of each class of one level row, as EquipmentGroup picks it: the
first class whose cumulative threshold reaches the million-roll. Rolls past
the last threshold select nothing.
================
*/
func classChances(row []classThreshold) map[int]float64 {
	out := map[int]float64{}
	var below uint64
	for _, class := range row {
		at := moduloAcceptanceMass(combinedRange, probabilityScale, uint64(class.threshold))
		if at > below {
			out[class.group] += float64(at-below) / float64(combinedRange)
			below = at
		}
	}
	return out
}

/*
================
selectionChances

What selectEquipment yields from one class, per item: the weighted pick
(r % total against the cumulative weights), the walk down to lower classes
keeping the picked type while the item is above the monster's level, and
the absolute admission r % 100 <= Absolute. The chances sum to the
selection's success; the rest is a failed attempt.
================
*/
func (c equipmentCatalog) selectionChances(country uint8, group int, rare bool, level uint8, mass float64, add func(equipmentRef, float64)) {
	c.walkSelection(country, group, rare, level, "", mass, add)
}

/*
================
walkSelection
================
*/
func (c equipmentCatalog) walkSelection(country uint8, group int, rare bool, level uint8, typeKey string, mass float64, add func(equipmentRef, float64)) {
	step := equipmentClassStep(rare)
	for ; group >= 0; group -= step {
		b := c.buckets[equipmentKey{country, group, rare}]
		if b == nil {
			continue
		}
		if typeKey == "" {
			total := uint64(b.weights[len(b.weights)-1])
			lo := uint64(0)
			for i, ref := range b.refs {
				hi := min(uint64(b.weights[i]), total-1)
				picked := moduloRangeMass(randomRange, total, lo, hi)
				lo = uint64(b.weights[i]) + 1
				if picked > 0 {
					c.settleSelection(country, group, rare, level, ref, mass*float64(picked)/float64(randomRange), add)
				}
			}
			return
		}
		alternatives := b.alternatives[typeKey]
		n := uint64(len(alternatives))
		for k, at := range alternatives {
			picked := moduloRangeMass(randomRange, n, uint64(k), uint64(k))
			c.settleSelection(country, group, rare, level, b.refs[at], mass*float64(picked)/float64(randomRange), add)
		}
		return
	}
}

/*
================
settleSelection

A picked item above the monster's level sends the walk one class down with
its type; one at or below it is admitted by the absolute roll.
================
*/
func (c equipmentCatalog) settleSelection(country uint8, group int, rare bool, level uint8, ref equipmentRef, mass float64, add func(equipmentRef, float64)) {
	if ref.Level > level {
		c.walkSelection(country, group-equipmentClassStep(rare), rare, level, ref.Type, mass, add)
		return
	}
	admitted := moduloAcceptanceMass(randomRange, 100, uint64(ref.Absolute))
	add(ref, mass*float64(admitted)/float64(randomRange))
}

/*
================
equipmentCountries

The drop buckets a monster's country reaches: 3 picks China or Europe on
roll & 1, an even split of the 15-bit draw.
================
*/
func equipmentCountries(country uint8) map[uint8]float64 {
	if country == 3 {
		return map[uint8]float64{0: 0.5, 1: 0.5}
	}
	if !HasEquipmentCountry(country) {
		return nil
	}
	return map[uint8]float64{country: 1}
}

/*
================
EquipmentAttemptChances

One equipment attempt of a monster of this level and country: the class
roll (EquipmentGroup), the country, then SelectEquipment. The chances sum
to the attempt's success.
================
*/
func EquipmentAttemptChances(level, country uint8, rare bool) []Chance {
	kind := 0
	if rare {
		kind = 1
	}
	if level == 0 || int(level) > len(equipment.classes[kind]) {
		return nil
	}
	acc := chanceAccumulator{}
	for group, classMass := range classChances(equipment.classes[kind][level-1]) {
		for dropCountry, countryMass := range equipmentCountries(country) {
			equipment.selectionChances(dropCountry, group, rare, level, classMass*countryMass, acc.add)
		}
	}
	return acc.sorted()
}

/*
================
SpecialEquipmentChances

One pick of a unique's prepass in the reference server: the level's fixed
class, the country, then SelectEquipment.
================
*/
func SpecialEquipmentChances(level, country uint8) []Chance {
	group, ok := SpecialEquipmentGroup(level)
	if !ok {
		return nil
	}
	acc := chanceAccumulator{}
	for dropCountry, countryMass := range equipmentCountries(country) {
		equipment.selectionChances(dropCountry, group, false, level, countryMass, acc.add)
	}
	return acc.sorted()
}

/*
================
ConsumableAttemptChances

One attempt of a consumable family at this level, as SelectConsumable.
================
*/
func ConsumableAttemptChances(family int, level uint8) []Chance {
	e, ok := consumables.families[family]
	if !ok || level == 0 || int(level) > len(e.classes[0]) {
		return nil
	}
	acc := chanceAccumulator{}
	for group, classMass := range classChances(e.classes[0][level-1]) {
		e.selectionChances(0, group, false, level, classMass, acc.add)
	}
	return acc.sorted()
}

/*
================
ConsumableFamilies

The families the planner's seven categories draw from, with each family's
chance of being the category's draw: 4 or 5 and 8 or 9 split on choice & 1.
================
*/
func ConsumableFamilies() [][]FamilyShare {
	return [][]FamilyShare{{{2, 1}}, {{3, 1}}, {{6, 1}}, {{10, 1}}, {{4, 0.5}, {5, 0.5}}, {{8, 0.5}, {9, 0.5}}, {{7, 1}}}
}

/*
================
FamilyShare
================
*/
type FamilyShare struct {
	Family int
	Share  float64
}

/*
================
AssignedChance

One assigned row of a monster: the item, its chance per try and how many
tries a kill makes (Min..Max, picked by one 15-bit draw). PerKill is the
chance a kill drops it at least once, before the kill's capacity.
================
*/
type AssignedChance struct {
	Codename string
	Plus     uint8
	Chance   float64
	Min, Max uint8
	PerKill  float64
	// Group is set for a random-group member: Chance is then the group's
	// firing chance times the member's share.
	Group bool
}

/*
================
AssignedDropChances

The random groups and fixed rows AssignedDrops rolls for a monster. A
distinct group drawing more than one member is not reduced to a closed
form; no v1.150 row has one, and an error keeps a future one from being
published wrong.
================
*/
func AssignedDropChances(monster string) ([]AssignedChance, error) {
	var out []AssignedChance
	for _, r := range consumables.random[monster] {
		if r.Distinct && r.Max > 1 {
			return nil, fmt.Errorf("monster %s group %d draws %d distinct members", monster, r.Group, r.Max)
		}
		fire := millionChance(uint32(float64(r.Probability) * 1e6))
		pool := consumables.groups[r.Group]
		var total uint64
		for i, row := range pool {
			total += groupMemberMass(i, len(pool), row.Probability)
		}
		for i, row := range pool {
			// Each of the count draws picks the member again, once the group
			// has fired: per kill, fire * (1 - (1 - member)^count).
			member := float64(groupMemberMass(i, len(pool), row.Probability)) / float64(total)
			out = append(out, AssignedChance{Codename: row.Codename, Chance: fire * member, Min: r.Min, Max: r.Max,
				PerKill: fire * countedChance(r.Min, r.Max, member), Group: true})
		}
	}
	for _, r := range consumables.fixed[monster] {
		chance := millionChance(uint32(float64(r.Probability) * 1e6))
		out = append(out, AssignedChance{Codename: r.Item, Plus: r.Plus, Chance: chance, Min: r.Min, Max: r.Max,
			PerKill: countedChance(r.Min, r.Max, chance)})
	}
	return out, nil
}

/*
================
countedChance

At least one success in a count of tries, the count Min + n % (Max-Min+1)
over the 15-bit draw n.
================
*/
func countedChance(minimum, maximum uint8, chance float64) float64 {
	span := uint64(maximum) - uint64(minimum) + 1
	var sum float64
	for k := uint64(0); k < span; k++ {
		weight := float64(moduloRangeMass(randomRange, span, k, k)) / float64(randomRange)
		sum += weight * atLeastOnce(chance, int(uint64(minimum)+k))
	}
	return sum
}

/*
================
atLeastOnce
================
*/
func atLeastOnce(chance float64, tries int) float64 {
	miss := 1.0
	for i := 0; i < tries; i++ {
		miss *= 1 - chance
	}
	return 1 - miss
}

/*
================
chanceAccumulator
================
*/
type chanceAccumulator map[chanceKey]*Chance

/*
================
chanceKey

A consumable can sit in two classes with different stacks; they stay apart.
================
*/
type chanceKey struct {
	codename string
	count    uint16
}

/*
================
chanceAccumulator.add
================
*/
func (acc chanceAccumulator) add(ref equipmentRef, mass float64) {
	if mass <= 0 {
		return
	}
	key := chanceKey{ref.Codename, max(ref.Count, 1)}
	if c := acc[key]; c != nil {
		c.Chance += mass
		return
	}
	acc[key] = &Chance{Codename: ref.Codename, Count: key.count, Chance: mass}
}

/*
================
chanceAccumulator.sorted

Most likely first, then by codename, so the output is stable.
================
*/
func (acc chanceAccumulator) sorted() []Chance {
	out := make([]Chance, 0, len(acc))
	for _, c := range acc {
		out = append(out, *c)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Chance != out[j].Chance {
			return out[i].Chance > out[j].Chance
		}
		if out[i].Codename != out[j].Codename {
			return out[i].Codename < out[j].Codename
		}
		return out[i].Count < out[j].Count
	})
	return out
}
