/*
===========================================================================

alchemy.go - C7 alchemy.json: the reinforcement odds

Every elixir and lucky powder with its authored success chance at each
plus (alchemy.ReinforceTables over the server's alchemy catalogue), and the
rules Reinforce applies around them, read from the same package's named
constants. A pressed attempt's chance is the elixir's, plus the powder's
while below the cap, plus the Lucky bonus and any skill or avatar bonus,
clamped to the minimum and maximum. The native game has no beta rate for
alchemy, so there is none here.

===========================================================================
*/
package catalogue

import (
	"sort"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/alchemy"
)

/*
================
ReinforceRow
================
*/
type ReinforceRow struct {
	ItemID  uint32   `json:"itemId"`
	Degree  int      `json:"degree,omitempty" doc:"A powder's equipment degree: it applies only to an item of exactly this degree (else the attempt is refused, 0x0A)."`
	Chances []int    `json:"chances" doc:"Success percent at the item's current plus +0..+11; the +11 value holds for every higher plus."`
	Kinds   []string `json:"kinds,omitempty" doc:"The equipment families an elixir accepts (weapon, shield, armor, accessory); any other item is refused (0x09)."`
}

/*
================
ReinforceRules
================
*/
type ReinforceRules struct {
	LuckBonus      int   `json:"luckBonus" doc:"Added when the running chance is below maxChance and the item has a Luck charge (MATTR_LUCK, a magic option on the item put on by a Luck stone); the attempt spends one charge."`
	MinChance      int   `json:"minChance" doc:"The attempt chance is c = elixir.chances[plus]; + powder.chances[plus] if a powder is staged and c < maxChance; + luckBonus if c < maxChance and the item has a Luck charge; + every bonusSources value active; then clamped to [minChance, maxChance]."`
	MaxChance      int   `json:"maxChance" doc:"The ceiling of the clamp; the powder and luck are skipped (not capped) when the running chance has already reached it."`
	RiskPlus       int   `json:"riskPlus" doc:"From this plus up, a failure destroys the item destroyPercent of the time (an Immortal charge, MATTR_ATHANASIA, saves it; some items are indestructible) and otherwise curses a random option (a Steady charge, MATTR_SOLID, prevents it)."`
	DestroyPercent int   `json:"destroyPercent" doc:"The share of failures at riskPlus and above that try to destroy the item."`
	AstralFloor    int   `json:"astralFloor" doc:"Every failure the item survives resets the plus to 0, or to astralFloor when the plus was at least astralFloor and an Astral charge (MATTR_ASTRAL) is spent. The elixir (and a staged powder) is always used."`
	ShardRates     []int `json:"shardRates,omitempty" doc:"Port-only, not native: when present this shard replaces elixir.chances[plus] with shardRates[plus] (the last entry holds above the table) and drops the minChance floor; the powder, luckBonus and bonusSources still add. Absent is the original game's rule."`
}

/*
================
BonusSource

One source of the flat reinforcement bonus: an active premium skill buff,
or an avatar magic option worn in avatar slots 0..3.
================
*/
type BonusSource struct {
	Kind  string `json:"kind" doc:"skill: added while the buff is active; avatar-option: added while worn."`
	ID    uint32 `json:"id"`
	Name  string `json:"name"`
	Bonus int    `json:"bonus,omitempty" doc:"The percent the source adds; an avatar option's value is rolled per item, so it is absent there."`
}

/*
================
AlchemyFile
================
*/
type AlchemyFile struct {
	Header
	Rules        ReinforceRules `json:"rules"`
	Elixirs      []ReinforceRow `json:"elixirs"`
	Powders      []ReinforceRow `json:"powders"`
	BonusSources []BonusSource  `json:"bonusSources"`
}

/*
================
AlchemySource
================
*/
type AlchemySource struct {
	Catalog *alchemy.Catalog
	ItemID  func(string) uint32
	// Skills are every skill row; the ones with a reinforcement bonus are
	// the premium buffs action.alchemyBonuses adds while active.
	Skills []enterworld.SkillRow
	// SkillName names a skill for the reader.
	SkillName func(enterworld.SkillRow) string
}

/*
================
BuildAlchemy
================
*/
func BuildAlchemy(header Header, source AlchemySource) AlchemyFile {
	catalog, itemID := source.Catalog, source.ItemID
	file := AlchemyFile{Header: header, Elixirs: []ReinforceRow{}, Powders: []ReinforceRow{}, BonusSources: bonusSources(source),
		Rules: ReinforceRules{LuckBonus: alchemy.ReinforceLuckBonus, MinChance: alchemy.ReinforceMinChance,
			MaxChance: alchemy.ReinforceMaxChance, RiskPlus: alchemy.ReinforceRiskPlus,
			DestroyPercent: alchemy.ReinforceDestroyPercent, AstralFloor: alchemy.ReinforceAstralFloor}}
	if catalog.Rates != nil {
		file.Rules.ShardRates = append([]int(nil), catalog.Rates...)
	}
	elixirs, powders := catalog.ReinforceTables()
	rows := func(tables []alchemy.ReinforceTable) []ReinforceRow {
		out := []ReinforceRow{}
		for _, table := range tables {
			if id := itemID(table.Codename); id != 0 {
				out = append(out, ReinforceRow{ItemID: id, Degree: table.Degree, Chances: append([]int(nil), table.Chances[:]...),
					Kinds: table.Kinds})
			}
		}
		return out
	}
	file.Elixirs, file.Powders = rows(elixirs), rows(powders)
	return file
}

/*
================
bonusSources

The flat reinforcement bonuses (action.alchemyBonuses): every skill with a
reinforcement bonus, and every avatar option tagged as an alchemy bonus.
================
*/
func bonusSources(source AlchemySource) []BonusSource {
	out := []BonusSource{}
	for _, row := range source.Skills {
		if row.AlchemyReinforceBonus > 0 {
			name := row.Codename
			if source.SkillName != nil {
				name = source.SkillName(row)
			}
			out = append(out, BonusSource{Kind: "skill", ID: row.ID, Name: name, Bonus: int(row.AlchemyReinforceBonus)})
		}
	}
	if source.Catalog != nil {
		for id, option := range source.Catalog.Magic {
			if option.Tag == alchemy.MagicTagAlchemyBonus {
				out = append(out, BonusSource{Kind: "avatar-option", ID: uint32(id), Name: option.Name})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind > out[j].Kind
		}
		return out[i].ID < out[j].ID
	})
	return out
}
