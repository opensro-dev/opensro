/*
===========================================================================

droptables.go - C4 droptables.json: the level tables monsters share

A kill's ordinary drops come from tables keyed by the monster's level (and
for equipment its country), so they live here once instead of on every
monster row. Each entry is one attempt's chance per item; the chances of
a table sum to the attempt's success.

Per kill (planMonsterKillLoot), for a variant with passes P and attempts A:
each pass rolls rare once (rareChance), then up to A equipment attempts on
the normal or rare table, then one draw per consumable category, each up
to A attempts. An item with attempt chance c in a table whose attempt
success is s drops in one pass with chance c * (1 - (1 - s)^A) / s, and in
a kill with 1 - (1 - that)^P. All of it is before the kill's capacity and
the killer's level-gap admission.

===========================================================================
*/
package catalogue

import (
	"fmt"
	"sort"

	"opensro.online/server/internal/game/action"
	"opensro.online/server/internal/game/item/loot"
	"opensro.online/server/internal/game/progression"
)

/*
================
DropChance
================
*/
type DropChance struct {
	ItemID uint32 `json:"itemId"`
	// Count is the stack the item drops as, when it is more than one.
	Count  uint16  `json:"count,omitempty"`
	Chance float64 `json:"chance"`
}

/*
================
EquipmentTable
================
*/
type EquipmentTable struct {
	Normal []DropChance `json:"normal"`
	Rare   []DropChance `json:"rare"`
}

/*
================
FamilyShare
================
*/
type FamilyShare struct {
	Family int     `json:"family"`
	Share  float64 `json:"share"`
}

/*
================
BetaRates

The closed beta's port-only multipliers, when the build ran with them on
(progression.BetaGrowthFromEnv). They are not native: the native chances
above stay as they are, and the website shows these beside them.
================
*/
type BetaRates struct {
	// DropRate multiplies the passes and the capacity.
	DropRate int `json:"dropRate"`
	// DropCap keeps at most this many ordinary items of a kill (0: no cap).
	DropCap int `json:"dropCap"`
	// GoldRate multiplies each gold heap.
	GoldRate int `json:"goldRate"`
	// RareChance replaces the native rareChance.
	RareChance float64 `json:"rareChance"`
	// Mix gives the named families their own rate in place of dropRate
	// (progression.DropMix); absent, every family runs at dropRate.
	Mix map[string]int `json:"mix,omitempty" doc:"Family name (equipment, assigned, potion, cure, scroll, elixir, ammo, stone, tablet) to that family's rate; a family not named runs at dropRate."`
}

/*
================
DropTablesFile
================
*/
type DropTablesFile struct {
	Header
	// RareChance is the native chance a pass rolls on the rare table.
	RareChance float64 `json:"rareChance"`
	// Categories are the consumable draws of a pass, in order: each a
	// family, or two families split by a coin.
	Categories [][]FamilyShare `json:"categories"`
	// Equipment is keyed "level/country" (Monster.Table).
	Equipment map[string]EquipmentTable `json:"equipment"`
	// Special is a unique's prepass pick, keyed as Equipment.
	Special map[string][]DropChance `json:"special"`
	// Consumables is keyed "family/level".
	Consumables map[string][]DropChance `json:"consumables"`
	Beta        *BetaRates              `json:"beta,omitempty"`
}

/*
================
BuildDropTables

The tables the monsters reference, and the consumable tables of every
level a monster has.
================
*/
func BuildDropTables(monsters []Monster, itemID func(string) uint32, header Header) DropTablesFile {
	file := DropTablesFile{Header: header, RareChance: action.RareEquipmentChance(0), Equipment: map[string]EquipmentTable{},
		Special: map[string][]DropChance{}, Consumables: map[string][]DropChance{}}
	for _, category := range loot.ConsumableFamilies() {
		var shares []FamilyShare
		for _, share := range category {
			shares = append(shares, FamilyShare{Family: share.Family, Share: share.Share})
		}
		file.Categories = append(file.Categories, shares)
	}
	levels := map[uint8]bool{}
	for _, m := range monsters {
		level, country := uint8(m.Level), m.Country
		levels[level] = true
		if _, done := file.Equipment[m.Table]; !done {
			file.Equipment[m.Table] = EquipmentTable{Normal: dropChances(loot.EquipmentAttemptChances(level, country, false), itemID),
				Rare: dropChances(loot.EquipmentAttemptChances(level, country, true), itemID)}
		}
		if m.Unique != nil {
			file.Special[m.Unique.Table] = dropChances(loot.SpecialEquipmentChances(level, country), itemID)
		}
	}
	for level := range levels {
		for _, category := range loot.ConsumableFamilies() {
			for _, share := range category {
				file.Consumables[fmt.Sprintf("%d/%d", share.Family, level)] = dropChances(loot.ConsumableAttemptChances(share.Family, level), itemID)
			}
		}
	}
	if beta := progression.BetaGrowthFromEnv(); beta.Enabled {
		file.Beta = &BetaRates{DropRate: beta.DropRate, DropCap: beta.DropCap, GoldRate: beta.GoldRate,
			RareChance: action.RareEquipmentChance(beta.RareRate)}
		// A malformed mix stops the GameWorld at boot, and sro-catalogue
		// refuses it before building, so an error here is never published.
		if mix, err := progression.BetaDropMixFromEnv(); err == nil {
			file.Beta.Mix = mix
		}
	}
	return file
}

/*
================
dropChances

Chances keyed by item id; a codename the item references lack is left
out, as the planner's prepareSelectedDrop refuses it.
================
*/
func dropChances(chances []loot.Chance, itemID func(string) uint32) []DropChance {
	out := []DropChance{}
	for _, c := range chances {
		id := itemID(c.Codename)
		if id == 0 {
			continue
		}
		row := DropChance{ItemID: id, Chance: c.Chance}
		if c.Count > 1 {
			row.Count = c.Count
		}
		out = append(out, row)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Chance > out[j].Chance })
	return out
}
