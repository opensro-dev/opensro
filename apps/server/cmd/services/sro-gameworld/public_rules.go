/*
===========================================================================

public_rules.go - the "Original vs OpenSRO" list the public API serves (P9)

Every player-facing port-only switch, read through the same function the
game reads it with, so the site shows the shard's live setting, plus the
deliberate deviations that have no switch. Operator-only switches (movement
clip telemetry, data paths) are not player-facing and are left out.

===========================================================================
*/
package main

import (
	"fmt"

	"opensro.online/server/internal/agent/publicstats"
	"opensro.online/server/internal/game/action"
	"opensro.online/server/internal/game/caravan"
	"opensro.online/server/internal/game/companion"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/alchemy"
	"opensro.online/server/internal/game/progression"
	"opensro.online/server/internal/game/quest"
	"opensro.online/server/internal/game/social/party"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
publicRules

Built once at boot: the switches are read from the environment at startup,
so the list cannot change while the GameWorld runs.
================
*/
func publicRules() publicstats.RulesResponse {
	silkRate, _ := action.BetaSilkFromEnv()
	stackSizes, _ := enterworld.StackSizesFromEnv()
	alchemyRates, _ := alchemy.ReinforceRatesFromEnv()
	alchemyMaxPlus, _ := alchemy.ReinforceMaxPlusFromEnv()
	teamDeathRevive, _ := action.TeamDeathReviveFromEnv()
	noAssimilation, _ := alchemy.NoAssimilationFromEnv()
	banditCap, _ := caravan.BanditCapFromEnv()
	dropMix, _ := progression.BetaDropMixFromEnv()
	return publicstats.RulesResponse{
		Rules: []publicstats.Rule{
			{Flag: progression.EnvBetaGrowth, On: progression.BetaGrowthFromEnv().Enabled,
				Native: "Retail experience, skill point and drop rates.",
				Now:    "Raised beta rates for experience, skill points and drops.",
				Why:    "A beta lasts weeks, not years; testers need to reach the content."},
			{Flag: progression.EnvBetaDropMix, On: progression.BetaGrowthFromEnv().Enabled && dropMix != nil,
				Native: "Every kind of item drops at the retail rate.",
				Now:    "Each item kind has its own beta drop rate; by default equipment keeps the raised rate while potions, pills, scrolls, arrows and alchemy materials drop at the retail rate.",
				Why:    "So raised drops bring gear rather than a floor of potions."},
			{Flag: progression.EnvBetaSealRates, On: progression.BetaSealRatesFromEnv().Enabled(),
				Native: "Each Seal of Star, Moon and Sun drops at the rare rate, and degree-9 Star and Moon only from monsters of level 76 to 85.",
				Now:    "Seals of degrees 1 to 8 drop three times as often; at degree 9 Star drops three times and Moon five times as often, Sun as before, and Star and Moon also from monsters of level 86 to 94.",
				Why:    "Beta balance, so testers can try seal gear."},
			{Flag: progression.EnvBetaShopOnlyDegree9, On: progression.BetaShopOnlyDegree9FromEnv().Enabled(),
				Native: "Monsters drop Lucky Powder, magic stones and attribute stones of every degree.",
				Now:    "Degree-9 Lucky Powder, magic stones and attribute stones no longer drop; the Jangan shops sell them.",
				Why:    "Beta balance: the top alchemy degree is bought with gold."},
			{Flag: progression.EnvElixirDropsOff, On: progression.ElixirDropsFromEnv().Off(),
				Native: "Monsters drop elixirs.",
				Now:    "Elixirs no longer drop; the Jangan blacksmith sells them for 1,000,000 gold.",
				Why:    "Beta balance: gold buys enhancement, so elixirs keep a steady price."},
			{Flag: action.EnvBetaSilk, On: silkRate > 0,
				Native: "Silk is bought for the account.",
				Now:    "Every full hour in the world earns silk, up to a cap.",
				Why:    "Lets testers try the Item Mall; beta silk never reaches the launch shard."},
			{Flag: enterworld.EnvBetaStarterKit, On: enterworld.BetaStarterKitEnabled(),
				Native: "Characters start with the retail creation items only.",
				Now:    "Every character also holds a return scroll and a speed scroll that are never used up.",
				Why:    "Testers spend their time on the systems under test, not on walking."},
			{Flag: quest.EnvInstantInventoryExpansion, On: quest.InstantInventoryExpansionFromEnv(),
				Native: "A bigger bag takes effect at the next world entry.",
				Now:    "A bigger bag takes effect at once.",
				Why:    "Relogging to see new slots read as a bug."},
			{Flag: companion.EnvPetPacing, On: companion.PoliciesFromEnv().Pacing,
				Native: "Grab and growth pets run at their full projected speed.",
				Now:    "Grab and growth pets run at four fifths of it.",
				Why:    "Owner-approved beta pacing for pets."},
			{Flag: party.EnvPartyMasteries, On: party.MasteriesFromEnv(),
				Native: "The party board shows the basic member row.",
				Now:    "The party board also shows each member's two main masteries.",
				Why:    "Lets a party see its builds at a glance."},
			{Flag: simulation.EnvBetaPlayerMap, On: simulation.BetaPlayerMapEnabled(),
				Native: "The world map shows only your own position.",
				Now:    "The world map also shows other players.",
				Why:    "Helps testers find each other on a small shard."},
			{Flag: enterworld.EnvStackSizes, On: len(stackSizes) > 0,
				Native: "Retail stack sizes.",
				Now:    "Larger stacks for some item kinds.",
				Why:    "Less bag management during testing."},
			{Flag: alchemy.EnvReinforceRates, On: alchemyRates != nil,
				Native: "Each elixir has its own success chance per plus, at least 10%.",
				Now:    "One gentler table for every elixir: +1 always succeeds, falling to 5% for +11; powders, Luck and premium bonuses still add.",
				Why:    "Players asked for it; the original rates return with one setting."},
			{Flag: alchemy.EnvReinforceMaxPlus, On: alchemyMaxPlus > 0,
				Native: "Items can be reinforced far beyond +11.",
				Now:    fmt.Sprintf("Reinforcement stops at +%d.", alchemyMaxPlus),
				Why:    "The success table ends at +11; the cap keeps gear in the balanced range."},
			{Flag: action.EnvTeamDeathRevive, On: !teamDeathRevive,
				Native: "A death in a free battle or the arena revives on the spot, at any level.",
				Now:    "A free-battle or arena death revives in town above level 10, like any other death.",
				Why:    "A shard setting; the original rule returns with one setting."},
			{Flag: alchemy.EnvNoAssimilation, On: noAssimilation,
				Native: "A successful attribute or magic stone can also reroll a white stat or a magic option (assimilation).",
				Now:    "A successful attribute or magic stone changes only its own stat.",
				Why:    "Players asked for it; the original rule returns with one setting."},
			{Flag: caravan.EnvBanditCap, On: banditCap > 0,
				Native: "A caravan ambush has as many bandits as its cargo's value calls for, with no limit.",
				Now:    fmt.Sprintf("At most %d bandits per ambush.", banditCap),
				Why:    "A full load drew dozens of bandits at once; the cap keeps an ambush fightable."},
		},
		Deviations: []publicstats.Deviation{
			{Title: "Storage Ctrl+click",
				Native: "Items move between bag and storage by dragging.",
				Now:    "Ctrl+click also moves an item between bag and storage."},
			{Title: "Double-click attack",
				Native: "A second click on a monster that drifted can miss it.",
				Now:    "A second press on the same monster within half a second attacks it."},
			{Title: "Skill press while seated",
				Native: "A skill pressed while seated casts from the seat.",
				Now:    "The character stands up, then casts."},
		},
	}
}
