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

import "opensro.online/server/internal/game/item/alchemy"

/*
================
ReinforceRow
================
*/
type ReinforceRow struct {
	ItemID uint32 `json:"itemId"`
	// Degree is a powder's equipment degree; elixirs have none.
	Degree int `json:"degree,omitempty"`
	// Chances is the success percent at +0..+11; +11 holds above it.
	Chances []int `json:"chances"`
}

/*
================
ReinforceRules
================
*/
type ReinforceRules struct {
	LuckBonus      int `json:"luckBonus"`
	MinChance      int `json:"minChance"`
	MaxChance      int `json:"maxChance"`
	RiskPlus       int `json:"riskPlus"`
	DestroyPercent int `json:"destroyPercent"`
	AstralFloor    int `json:"astralFloor"`
}

/*
================
AlchemyFile
================
*/
type AlchemyFile struct {
	Header
	Rules   ReinforceRules `json:"rules"`
	Elixirs []ReinforceRow `json:"elixirs"`
	Powders []ReinforceRow `json:"powders"`
}

/*
================
BuildAlchemy
================
*/
func BuildAlchemy(header Header, catalog *alchemy.Catalog, itemID func(string) uint32) AlchemyFile {
	file := AlchemyFile{Header: header, Elixirs: []ReinforceRow{}, Powders: []ReinforceRow{},
		Rules: ReinforceRules{LuckBonus: alchemy.ReinforceLuckBonus, MinChance: alchemy.ReinforceMinChance,
			MaxChance: alchemy.ReinforceMaxChance, RiskPlus: alchemy.ReinforceRiskPlus,
			DestroyPercent: alchemy.ReinforceDestroyPercent, AstralFloor: alchemy.ReinforceAstralFloor}}
	elixirs, powders := catalog.ReinforceTables()
	rows := func(tables []alchemy.ReinforceTable) []ReinforceRow {
		out := []ReinforceRow{}
		for _, table := range tables {
			if id := itemID(table.Codename); id != 0 {
				out = append(out, ReinforceRow{ItemID: id, Degree: table.Degree, Chances: append([]int(nil), table.Chances[:]...)})
			}
		}
		return out
	}
	file.Elixirs, file.Powders = rows(elixirs), rows(powders)
	return file
}
