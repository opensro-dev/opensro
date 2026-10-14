/*
===========================================================================

public_rules.go - the "Original vs OpenSRO" list the public API serves (P9)

Every player-facing port-only switch, read through the same function the
game reads it with, so the site shows the shard's live setting, plus the
deliberate deviations that have no switch. Operator-only switches (movement
clip telemetry, data paths, GM commands) are not player-facing and are left
out; unlistedSettings names each shard setting left out and why, and a test
keeps every setting either here or there.

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
	"opensro.online/server/internal/game/item/commerce"
	"opensro.online/server/internal/game/progression"
	"opensro.online/server/internal/game/quest"
	"opensro.online/server/internal/game/social/party"
	"opensro.online/server/internal/game/world/simulation"
)

// unlistedSettings are the shard settings the rules page leaves out, each
// with the reason; every other setting is a rule (public_rules_test.go).
var unlistedSettings = map[string]string{
	action.EnvGMSetSpeed: "a GM command, not a rule players meet",
}

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
	growth := progression.BetaGrowthFromEnv()
	betaMastery, _ := progression.BetaMasteryFromEnv()
	shopGoods, _ := commerce.ExtraGoodsFromEnv()
	mallGoods, _ := commerce.MallExtraGoodsFromEnv()
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
			{Flag: progression.EnvBetaSkillExpRate, On: growth.Enabled,
				Native: "Skill experience comes at the retail rate.",
				Now:    fmt.Sprintf("Skill experience comes %d times as fast.", growth.SkillExpRate),
				Why:    "Testers can train skills without long grinding."},
			{Flag: progression.EnvBetaDropRate, On: growth.Enabled,
				Native: "A kill rolls each of its drops once.",
				Now:    fmt.Sprintf("A kill rolls each of its drops %d times.", growth.DropRate),
				Why:    "So plus upgrades and alchemy can be tried without long farming."},
			{Flag: progression.EnvBetaDropCap, On: growth.Enabled && growth.DropCap > 0,
				Native: "A monster drops as many items as its rolls give, within its own capacity.",
				Now:    fmt.Sprintf("A kill drops at most %d ordinary items.", growth.DropCap),
				Why:    "The raised rate shows as better finds rather than a carpet of potions."},
			{Flag: progression.EnvBetaGoldRate, On: growth.Enabled,
				Native: "Gold heaps hold the retail amount.",
				Now:    fmt.Sprintf("Each gold heap holds %d times the retail amount.", growth.GoldRate),
				Why:    "Testers can afford the shop goods the beta sells for gold."},
			{Flag: progression.EnvBetaRareRate, On: growth.Enabled,
				Native: "Rare (Seal) equipment drops at the retail chance.",
				Now:    fmt.Sprintf("Rare (Seal) equipment drops %d times as often.", growth.RareRate),
				Why:    "Beta balance, so testers can try seal gear."},
			{Flag: progression.EnvBetaMastery, On: betaMastery > 0,
				Native: "The total of all mastery levels is capped at 300 for Chinese characters and at twice the level, up to 240, for Europeans.",
				Now:    fmt.Sprintf("Every character may train masteries up to a total of %d; each mastery's own limits stay.", betaMastery),
				Why:    "Testers can try every mastery tree on one character."},
			{Flag: progression.EnvBetaSealRates, On: progression.BetaSealRatesFromEnv().Enabled(),
				Native: "Each Seal of Star, Moon and Sun drops at the rare rate, and degree-9 Star and Moon only from monsters of level 76 to 85.",
				Now:    "Seals of degrees 1 to 8 drop three times as often; at degree 9 Star drops three times and Moon five times as often, Sun as before, and Star and Moon also from monsters of level 86 to 94.",
				Why:    "Beta balance, so testers can try seal gear."},
			{Flag: progression.EnvSunEventOnly, On: progression.SunEventOnlyFromEnv().Enabled(),
				Native: "Degree-9 Seal of Sun gear drops from monsters, and the honor shop lists it.",
				Now:    "Degree-9 Seal of Sun gear neither drops nor is sold; it comes back only as an event reward. Sun gear you already own is kept, and every other drop keeps its chance.",
				Why:    "Requested for the beta, to keep the top seal rare; the original returns with one setting."},
			{Flag: progression.EnvBetaShopOnlyDegree9, On: progression.BetaShopOnlyDegree9FromEnv().Enabled(),
				Native: "Monsters drop magic stones and attribute stones of every degree; Lucky Powder never drops.",
				Now:    "Degree-9 magic stones and attribute stones no longer drop; Jangan's smith and grocer sell them, and every town's grocer sells degree-9 Lucky Powder.",
				Why:    "Beta balance: the top alchemy degree is bought with gold."},
			{Flag: progression.EnvElixirDropsOff, On: progression.ElixirDropsFromEnv().Off(),
				Native: "Monsters drop elixirs.",
				Now:    "Elixirs no longer drop; the Jangan blacksmith sells them for 1,000,000 gold.",
				Why:    "Beta balance: gold buys enhancement, so elixirs keep a steady price."},
			{Flag: progression.EnvAlchemyMaterialDropsOff, On: progression.MaterialDropsFromEnv().Off(),
				Native: "Monsters drop alchemy materials (claws, hides and other monster parts).",
				Now:    "Alchemy materials no longer drop.",
				Why:    "On the beta the stones they once made are bought, so the materials only piled up on the ground."},
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
			{Flag: action.EnvStorageAutoStack, On: action.StorageAutoStackFromEnv(),
				Native: "A quick move into or out of storage takes the first empty slot.",
				Now:    "A quick move lands on a matching stack with room first.",
				Why:    "Owner decision: storage fills less with split stacks."},
			{Flag: action.EnvReverseReturnMap, On: action.ReverseReturnMapFromEnv(),
				Native: "The reverse return scroll offers the last recall point and the last death point.",
				Now:    "It also offers town gates and unique monster areas on the map.",
				Why:    "Later clients offer a map choice; the destinations are places the original already sends players to."},
			{Flag: commerce.EnvShopExtraGoods, On: len(shopGoods) > 0,
				Native: "NPC shops sell the v1.150 stock.",
				Now:    "Some NPC shops also sell extra goods for gold, such as elixirs and degree-9 stones.",
				Why:    "Beta balance: gold buys enhancement."},
			{Flag: commerce.EnvMallExtraGoods, On: len(mallGoods) > 0,
				Native: "The Item Mall lists the v1.150 goods.",
				Now:    "It also lists avatar outfits the client ships but its mall never listed.",
				Why:    "Beta balance: the outfits were already in the game data."},
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
			{Flag: action.EnvMostWanted, On: action.MostWantedFromEnv(),
				Native: "Job kills earn job EXP only.",
				Now: "Each job kill while suited adds to a streak; every 10 is a wanted level (up to 10), worth +1 STR, +1 INT and +1% move speed each, " +
					"and 1%/2%/3% more damage dealt and less taken at levels 3/5/10. Killing a wanted player pays 100,000,000 gold per level. " +
					"Kills of your own account, party, guild or union never count, and one victim counts at most 3 times an hour. Death or taking the suit off ends the streak.",
				Why: "Requested for the beta, to reward job-war streaks and put a price on them; the original returns with one setting."},
			{Flag: action.EnvFastDenReturn, On: action.FastDenReturnFromEnv(),
				Native: "The Bandit Den Return Scroll counts down 5 minutes before the thief arrives.",
				Now:    "It counts down 2 minutes 30 seconds.",
				Why:    "Owner-approved for the beta; the original countdown returns with one setting."},
			{Flag: action.EnvDenNoTransportSummon, On: action.DenNoTransportSummonFromEnv(),
				Native: "A transport can be summoned while the Bandit Den Return Scroll counts down.",
				Now:    "No transport can be summoned during that countdown.",
				Why:    "Thieves left stolen goods on the ground and loaded them just before arriving; the original rule returns with one setting."},
			{Flag: enterworld.EnvMallTransportSpeed, On: enterworld.MallTransportSpeedFromEnv(),
				Native: "Item Mall transports (buffalo, rhinoceros, white elephant) move at their listed speed.",
				Now:    "They move 50% faster.",
				Why:    "Requested for the beta; the original speed returns with one setting."},
			{Flag: action.EnvAnnounceSoxDrop, On: action.AnnouncementsFromEnv().SoxDrop,
				Native: "A Seal of Star, Moon or Sun drop is not announced.",
				Now:    "Everyone sees \"<player> dropped <item> from <monster>\".",
				Why:    "Requested for the beta; the original quiet drop returns with one setting."},
			{Flag: action.EnvAnnouncePlus, On: action.AnnouncementsFromEnv().Plus,
				Native: "Reinforcement results are private.",
				Now:    "Everyone sees \"<player> advanced <item> to +N\" for +8 to +11.",
				Why:    "Requested for the beta; the original returns with one setting."},
			{Flag: action.EnvAnnounceJobKill, On: action.AnnouncementsFromEnv().JobKill,
				Native: "Job kills are not announced.",
				Now:    "Everyone sees which job killed which (\"Thief killed Trader\"), no names.",
				Why:    "Requested for the beta; the original returns with one setting."},
			{Flag: action.EnvThiefPartyBonus, On: action.ThiefPartyBonusFromEnv(),
				Native: "A party's thieves change nothing about its monster rewards.",
				Now:    "Each party member dressed as a thief adds 10% monster EXP and gold for the party, up to 50%.",
				Why:    "Requested for the beta, to make thieves welcome in hunting parties; the original returns with one setting."},
			{Flag: action.EnvTradeJobBonus, On: action.TradeJobBonusFromEnv(),
				Native: "A trade sale's party hunters take their usual share and the trader its usual profit.",
				Now:    "The trader earns 10% more profit per party hunter nearby (up to 20%), and each hunter 10% more gold per party trader (up to 60%).",
				Why:    "Requested for the beta, so traders and hunters gain from escorting each other; the original returns with one setting."},
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
			{Title: "Word wrap",
				Native: "Long text breaks at whatever letter reaches the edge, even mid-word.",
				Now:    "Chat, tooltips and window text break between words."},
			{Title: "Alchemy stone result",
				Native: "A successful magic or attribute stone says the item's enhancement level.",
				Now:    "It says which stat the stone changed, from the old value to the new."},
		},
	}
}
