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
	// SkillExpRate multiplies skill EXP on top of the beta growth pace
	// (progression.GrowthRates.SkillExpRate, SRO_BETA_SKILL_EXP_RATE).
	SkillExpRate int64 `json:"skillExpRate" doc:"Port-only beta balance: skill EXP is multiplied by this on top of the beta growth, which holds every level's EXP and skill EXP to the level-1 kill pace. Without the beta object the original rates apply (x1)."`
	// RareChance replaces the native rareChance.
	RareChance float64 `json:"rareChance"`
	// Seals multiply each rare seal's chance on top of rareChance
	// (progression.SealRates); absent, every seal runs at rareChance.
	// Rare replaces a table's native rare attempt where the beta seal band
	// changes it (levels 81-89 also roll degree-9 Star and levels 86-94 also
	// roll degree-9 Moon).
	Rare  map[string][]DropChance `json:"rare,omitempty" doc:"Port-only beta balance: monster table to its rare attempt where the beta seal band changes it (levels 81-89 roll degree-9 Star at level 80's share and levels 86-94 roll degree-9 Moon at level 85's share); a table not named keeps its rare list."`
	Seals [][]int                 `json:"seals,omitempty" doc:"Port-only beta balance: nine rows (degree 1 first) of three multipliers (Seal of Star, Moon, Sun). A rare item's chance is its rareChance share times its degree and seal's multiplier; the seal is (ItemClass - 1) % 3 and the degree (ItemClass + 2) / 3."`
	// Mix gives the named families their own rate in place of dropRate
	// (progression.DropMix); absent, every family runs at dropRate.
	Mix map[string]int `json:"mix,omitempty" doc:"Family name (equipment, assigned, potion, cure, scroll, elixir, ammo, stone, tablet) to that family's rate; a family not named runs at dropRate."`
	// ElixirDropsOff removes every elixir from every drop
	// (progression.ElixirDrops).
	ElixirDropsOff bool `json:"elixirDropsOff,omitempty" doc:"Port-only beta balance: no monster drops an elixir (type 3/3/10/1); the Jangan blacksmith sells them. Their chances in these tables then drop nothing."`
	// ShopOnlyDegree9 removes degree-9 alchemy goods from every drop
	// (progression.ShopOnlyDegree9).
	// MaterialDropsOff removes every alchemy material from every drop
	// (progression.MaterialDrops).
	MaterialDropsOff bool `json:"materialDropsOff,omitempty" doc:"Port-only beta balance: no monster drops an alchemy material (type 3/3/11/4, monster parts). Their chances in these tables then drop nothing."`
	ShopOnlyDegree9  bool `json:"shopOnlyDegree9,omitempty" doc:"Port-only beta balance: degree-9 Lucky Powder (type 3/3/10/2), stat magic stones (3/3/11/1), attribute stones (3/3/11/2) and Luck, Steady, Astral, Athanasia and Repair stones (3/3/11/7), degree being the item's ItemClass, no longer drop; every town's grocer sells the Lucky Powder and Jangan's smith and grocer sell the stones (SRO_SHOP_EXTRA_GOODS). Their chances in these tables then drop nothing."`
	// SunDropRate is the degree-9 Sun class rate (progression.SunRate);
	// while it runs, Rare's named tables carry the widened Sun class.
	SunDropRate int `json:"sunDropRate,omitempty" doc:"Port-only beta balance: the degree-9 Seal of Sun class of the rare table is 30 times this rate as wide on levels 76-90, so each Sun item's chance is that multiple of its native one; every other class is unchanged. Rare's named tables already carry the widened chances."`
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
	// SunEventOnly says degree-9 Seal of Sun gear neither drops nor is sold
	// (progression.SunEventOnly); its chances are already left out.
	SunEventOnly bool `json:"sunEventOnly,omitempty" doc:"Port-only: degree-9 Seal of Sun gear (rarity 2, ItemClass 27) neither drops nor is sold; it comes only as an event reward. Its chances are left out of these tables, and a Sun pick drops nothing, so every other chance is unchanged."`
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
		Special: map[string][]DropChance{}, Consumables: map[string][]DropChance{}, SunEventOnly: progression.SunEventOnlyFromEnv().Enabled()}
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
	sunRate := progression.SunRateFromEnv()
	if beta := progression.BetaGrowthFromEnv(); beta.Enabled || sunRate.Active() {
		file.Beta = &BetaRates{DropRate: beta.DropRate, DropCap: beta.DropCap, GoldRate: beta.GoldRate, SkillExpRate: beta.SkillExpRate,
			RareChance: action.RareEquipmentChance(beta.RareRate), Seals: progression.BetaSealRatesFromEnv().Table(),
			ElixirDropsOff: progression.ElixirDropsFromEnv().Off(), ShopOnlyDegree9: progression.BetaShopOnlyDegree9FromEnv().Enabled(),
			MaterialDropsOff: progression.MaterialDropsFromEnv().Off(), SunDropRate: sunRate.Rate()}
		sealBand := progression.BetaSealRatesFromEnv().Enabled()
		for _, m := range monsters {
			level := uint8(m.Level)
			if _, done := file.Beta.Rare[m.Table]; done {
				continue
			}
			// The published rare attempt is the one the roll composes:
			// the seal band's row while the band runs, the Sun-widened
			// class while the rate runs, both together on levels 86-90.
			if !(sealBand && loot.InSealBand(level)) && !(sunRate.Active() && loot.SunScaleCovers(level)) {
				continue
			}
			if file.Beta.Rare == nil {
				file.Beta.Rare = map[string][]DropChance{}
			}
			file.Beta.Rare[m.Table] = dropChances(loot.ComposedRareChances(level, m.Country, sealBand, sunRate.MassMultiplier()), itemID)
		}
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
