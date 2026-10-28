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
	"strconv"

	"opensro.online/server/internal/agent/publicstats"
	"opensro.online/server/internal/game/action"
	"opensro.online/server/internal/game/caravan"
	"opensro.online/server/internal/game/companion"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/alchemy"
	"opensro.online/server/internal/game/item/commerce"
	"opensro.online/server/internal/game/progression"
	"opensro.online/server/internal/game/quest"
	"opensro.online/server/internal/game/tunables"
	"opensro.online/server/internal/game/world/alexandria"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/movement"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/game/world/townarea"
	"opensro.online/server/internal/platform/economy"
)

// unlistedSettings are the shard settings the rules page leaves out, each
// with the reason; every other setting is a rule (public_rules_test.go).
var unlistedSettings = map[string]string{
	action.EnvGMSetSpeed:            "a GM command, not a rule players meet",
	monster.EnvTrainingDummy:        "GM-only training targets",
	simulation.EnvBetaUniqueRespawn: "unique monster spawn timing is not published",
	// The tunables a rule's own text shows (game/tunables).
	"SRO_MOST_WANTED_TEST_KILLS":      "shown in the Most Wanted test pacing rule",
	"SRO_MOST_WANTED_TEST_BOUNTY":     "shown in the Most Wanted test pacing rule",
	"SRO_JOB_KILL_NOTICES_PER_MINUTE": "shown in the job-kill repeat rule",
	"SRO_JOB_KILL_REPEAT_MINUTES":     "shown in the job-kill repeat rule",
	"SRO_SOX_NOTICE_MIN_DEGREE":       "shown in the seal-drop notice rule",
	// Client presentation values (uiTuning): the Short notices row is a
	// player's own Experimental choice, and the chat wait is the port's.
	"SRO_SHORT_NOTICE_SHOW_MS":           "timing of a player's own Experimental Short notices row",
	"SRO_SHORT_NOTICE_FADE_MS":           "timing of a player's own Experimental Short notices row",
	"SRO_CHAT_ACK_TIMEOUT_MS":            "the client's wait before warning that a chat line may not have arrived",
	"SRO_MOST_WANTED_NOTICES_PER_MINUTE": "a cap on the Most Wanted notices, an operator throttle",
	// The trade group: each number is shown in its own rule's text.
	"SRO_TRADE_ROUTE_DONWHANG_PERCENT":        "shown in the trade tiers rule",
	"SRO_TRADE_ROUTE_HOTAN_PERCENT":           "shown in the trade tiers rule",
	"SRO_TRADE_ROUTE_SAMARKAND_PERCENT":       "shown in the trade tiers rule",
	"SRO_TRADE_ROUTE_CONSTANTINOPLE_PERCENT":  "shown in the trade tiers rule",
	"SRO_THIEF_PARTY_PERCENT_EACH":            "shown in the thief party bonus rule",
	"SRO_THIEF_PARTY_PERCENT_CAP":             "shown in the thief party bonus rule",
	"SRO_TRADE_JOB_TRADER_PERCENT_PER_HUNTER": "shown in the trade job bonus rule",
	"SRO_TRADE_JOB_TRADER_PERCENT_CAP":        "shown in the trade job bonus rule",
	"SRO_TRADE_JOB_HUNTER_PERCENT_PER_TRADER": "shown in the trade job bonus rule",
	"SRO_TRADE_JOB_HUNTER_PERCENT_CAP":        "shown in the trade job bonus rule",
	// Deployment plumbing: how the shard's transport is served, not a
	// rule players meet (their client is offered the same world either
	// way).
	"SRO_WEBTRANSPORT_PUBLIC": "operator transport serving switch, not a gameplay rule",
	// Build-time inputs (#1745): the variant build's import tools read
	// them when the server data is built; the GameWorld reads neither,
	// so there is no running rule for the page to state.
	"SRO_CAP110_ITEMS":  "degree-11 equipment build input, consumed when the server data is built",
	"SRO_CAP110_SKILLS": "degree-11 skill build input, consumed when the server data is built",
	// Staff telemetry (#1639): name-free kill and EXP rates for the
	// staff console; players never meet it.
	action.EnvKillStats: "staff kill and EXP rate telemetry, not a rule players meet",
	// Staff telemetry (#1691): name-free seal counts by degree and tier
	// for the staff console; players never meet it.
	economy.EnvSealCensus: "staff seal census telemetry, not a rule players meet",
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
	silkActiveOnly, _ := action.BetaSilkActiveOnlyFromEnv()
	stackSizes, _ := enterworld.StackSizesFromEnv()
	alchemyRates, _ := alchemy.ReinforceRatesFromEnv()
	alchemyMaxPlus, _ := alchemy.ReinforceMaxPlusFromEnv()
	teamDeathRevive, _ := action.TeamDeathReviveFromEnv()
	noAssimilation, _ := alchemy.NoAssimilationFromEnv()
	banditCap, _ := caravan.BanditCapFromEnv()
	dropMix, _ := progression.BetaDropMixFromEnv()
	growth := progression.BetaGrowthFromEnv()
	betaMastery, _ := progression.BetaMasteryFromEnv()
	levelCap, _ := progression.LevelCapFromEnv()
	pharaohGear, _ := monster.PharaohGearRateFromEnv()
	alexandriaOn, _ := alexandria.EnabledFromEnv()
	shopGoods, _ := commerce.ExtraGoodsFromEnv()
	janganAlchemyOn, _ := commerce.JanganAlchemyShopActiveFromEnv()
	thiefCartTownAreasOn, _ := townarea.ThiefCartTownAreasFromEnv()
	mallGoods, _ := commerce.MallExtraGoodsFromEnv()
	rideFactor, rideOn, _ := enterworld.MallRideSpeedFromEnv()
	flagWar, _ := action.FlagWarSettingsFromEnv()
	settings := tunables.Current()
	testKills := settings.Int(tunables.MostWantedTestKills)
	return publicstats.RulesResponse{
		Rules: []publicstats.Rule{
			{Flag: progression.EnvLevelCap, On: levelCap == progression.ExpansionLevelCap,
				Native: "The character level cap is 90.",
				Now:    "The character level cap is 110, using the installed v1.150 experience curve.",
				Why:    "Optional progression for the Alexandria expansion."},
			{Flag: monster.EnvCap110Monsters, On: monster.Cap110Enabled(),
				Native: "The v1.150 monster population and equipment drop catalog.",
				Now:    "Alexandria field monsters at levels 100-110 and database equipment drops through level 110.",
				Why:    "An outdoor leveling route for the optional cap-110 expansion."},
			{Flag: monster.EnvCap110Level100, On: monster.Cap110Level100Enabled(),
				Native: "The expansion field population stands or falls as one switch.",
				Now:    "Alexandria's level-100 field monsters are admitted for hunting; levels 101-110 follow the population switch.",
				Why:    "Levels 95-99 gain a hunting ground at the level-100 monsters ahead of the full expansion."},
			{Flag: enterworld.EnvCap110SetBonuses, On: enterworld.SetBonusesEnabled(),
				Native: "The v1.150 client has no degree-10/11 equipment sets; a worn piece grants its own stats only.",
				Now:    "Wearing 2, 4 or 6 pieces of one degree-11 set activates that tier - the highest met replaces the lower - reinforcing each worn piece and raising abnormal resistance.",
				Why:    "Completes the optional cap-110 expansion's equipment sets."},
			{Flag: monster.EnvPharaohTomb, On: monster.PharaohTombEnabled(),
				Native: "There is no Pharaoh Tomb.",
				Now:    "The Pharaoh Tombs open from Kings Valley, each a shared area of level 100-110 monsters and bosses.",
				Why:    "Endgame hunting for the optional cap-110 expansion."},
			{Flag: monster.EnvCap110Qinshi, On: monster.QinshiEnabled(),
				Native: "The Qin-Shi Emperor Tomb holds its v1.150 monsters.",
				Now:    "The tomb's upper floors also hold the later database's level 81-110 monsters.",
				Why:    "Levels 91-99 gain a hunting ground in the optional cap-110 expansion."},
			{Flag: monster.EnvPharaohGearRate, On: pharaohGear > 1,
				Native: "A Pharaoh Tomb elite or unique rolls its equipment at the original rate.",
				Now:    fmt.Sprintf("A Pharaoh Tomb elite or unique rolls its equipment %d times as often.", pharaohGear),
				Why:    "The tomb's gear is rare by design; testers need to see it."},
			{Flag: alexandria.EnvAlexandria, On: alexandriaOn,
				Native: "The world is the v1.150 map; Alexandria is not part of it.",
				Now:    "The Alexandria city is open, with its gates, ferry and townspeople.",
				Why:    "Optional Alexandria expansion, imported from a later client."},
			{Flag: companion.EnvAttackPetExpansion, On: companion.AttackPetExpansionEnabled(),
				Native: "Growth pets use their original skills and stats.",
				Now:    "Bear, wolf, penguin and crow have two active and three passive skill rows. Monster equipment drops can also yield same-degree pet gear with up to six bonuses; the database controls its relative drop chance.",
				Why:    "Owner-approved attack-pet expansion; competitive and instanced worlds keep original stats."},
			{Flag: progression.EnvBetaGrowth, On: progression.BetaGrowthFromEnv().Enabled,
				Native: "Retail experience, skill point and drop rates.",
				Now:    "Raised beta rates for experience, skill points and drops.",
				Why:    "A beta lasts weeks, not years; testers need to reach the content."},
			{Flag: progression.EnvBetaGrowthBands, On: growth.Enabled && growth.Bands.Count > 0,
				Native: "Every level earns experience and skill points at the retail rate.",
				Now:    growth.Bands.Describe(),
				Why:    "Fast early levels, then a steadier climb through the cap-110 levels."},
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
				Now:    "Seals of degrees 1 to 8 drop three times as often; at degree 9 Star drops three times and Moon twice as often, Sun as before, and Star and Moon also from monsters of level 86 to 94.",
				Why:    "Beta balance, so testers can try seal gear."},
			{Flag: progression.EnvSunEventOnly, On: progression.SunEventOnlyFromEnv().Enabled(),
				Native: "Degree-9 Seal of Sun gear drops from monsters, and the honor shop lists it.",
				Now:    "Degree-9 Seal of Sun gear drops only from unique monsters; Sun gear you already own is kept, and every other drop keeps its chance. No shop sells it either way.",
				Why:    "Requested for the beta, to keep the top seal rare; the original returns with one setting."},
			{Flag: progression.EnvSunDropRate, On: progression.SunRateFromEnv().Active(),
				Native: "Degree-9 Seal of Sun gear almost never drops from monsters.",
				Now:    fmt.Sprintf("It drops from monsters of level 76 to 90 at up to %d times its original chance, but never wider than the Moon class, so a Sun stays about twice as rare as a Moon.", progression.SunRateFromEnv().MassMultiplier()),
				Why:    "Owner-approved for the beta so the top seal enters the economy from hunting; shops never sell it, and the original chance returns with one setting."},
			{Flag: progression.EnvUniqueGapExempt, On: progression.UniqueRulesFromEnv().GapExempt,
				Native: "A monster's drops thin out once you are more than six levels above it and stop entirely far above it, uniques included.",
				Now:    "A unique's drops ignore the level gap: a level-90 hunter loots Tiger Girl as fully as a level-20 one.",
				Why:    "Owner-approved for the beta, so capped hunters can still hunt the low-level uniques; the original returns with one setting."},
			{Flag: progression.EnvUniquePopTicket, On: progression.UniqueRulesFromEnv().PopTicket,
				Native: "A unique's assigned rewards are rolled per kill, and the level-gap rule can refuse every one of them.",
				Now:    "Every unique kill leaves exactly one Magic Pop ticket.",
				Why:    "Owner-approved for the beta, so every unique kill pays its ticket; the original returns with one setting."},
			{Flag: progression.EnvUniqueMaxLevelLoot, On: progression.UniqueRulesFromEnv().MaxLevelLoot,
				Native: "A unique's equipment is rolled from the table of the unique's own level.",
				Now:    "A unique within ten levels of the highest installed loot level rolls its equipment from that level: degree 9 with the native catalog, degree 11 with the cap-110 catalog installed. Every other unique keeps its own level's table.",
				Why:    "Opt-in beta tuning, so the endgame uniques pay the endgame table; off keeps the native equipment table for every unique."},
			{Flag: progression.EnvUniqueSealDrops, On: progression.UniqueRulesFromEnv().SealDrops,
				Native: "A unique drops sealed (SoX) equipment only from its ordinary rare roll, without a guaranteed seal reward.",
				Now:    "Every unique kill leaves one Seal of Star of the top degree and one Seal of Moon on a kill in ten. No Seal of Sun is guaranteed; it drops only from the ordinary rare roll.",
				Why:    "Opt-in beta tuning to reward competition for uniques; off keeps ordinary rare rolls without a guaranteed seal reward."},
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
				Why:    "A community vote brought the parts back, so they drop as in the original; this only applies if the removal is switched back on."},
			{Flag: action.EnvBetaSilk, On: silkRate > 0,
				Native: "Silk is bought for the account.",
				Now:    "Every full hour in the world earns silk, up to a cap.",
				Why:    "Lets testers try the Item Mall; beta silk never reaches the launch shard."},
			{Flag: action.EnvBetaSilkActiveOnly, On: silkRate > 0 && silkActiveOnly,
				Native: "Silk is bought for the account.",
				Now:    "The hourly silk counts only while you play: each action keeps it going for ten minutes.",
				Why:    "A character left idle in town earned as much as one playing."},
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
				Now:    "It also offers places on the map: the later client's own list when the server imported it, else town gates and unique monster areas.",
				Why:    "Later clients offer a map choice; the destinations are places the original already sends players to."},
			{Flag: enterworld.EnvHuntingGuide, On: enterworld.HuntingGuideFromEnv(),
				Native: "The world map shows no monster information.",
				Now:    "Players who turn on the Experimental Hunting guide see approximate hunting areas, with species and levels, on the world map.",
				Why:    "Owner-approved beginner guide; each player opts in, and it is off for everyone else."},
			{Flag: commerce.EnvShopExtraGoods, On: len(shopGoods) > 0,
				Native: "NPC shops sell the v1.150 stock.",
				Now:    "Some NPC shops also sell extra goods for gold, such as elixirs and degree-9 stones.",
				Why:    "Beta balance: gold buys enhancement."},
			{Flag: commerce.EnvJanganAlchemyShop, On: janganAlchemyOn,
				Native: "Jangan's grocer is Grocery Trader Jinjin, her alchemy goods share one tab, and the smith sells the elixirs.",
				Now:    "Jangan's grocer is OpenSRO Alchemy - Jangan: elixirs and powders, attribute stones and alchemy stones on three tabs of their own, the elixirs moved from the smith.",
				Why:    "A player's request: one alchemy shop in Jangan."},
			{Flag: commerce.EnvMallExtraGoods, On: len(mallGoods) > 0,
				Native: "The Item Mall lists the v1.150 goods.",
				Now:    "It also lists avatar outfits the client ships but its mall never listed.",
				Why:    "Beta balance: the outfits were already in the game data."},
			{Flag: action.EnvNameChangeScroll, On: action.NameChangeScrollFromEnv(),
				Native: "A character's name never changes.",
				Now:    "The Item Mall sells a name-change scroll for 500 silk; the new name applies at the next login, renames cool down for 30 days, and the old name stays reserved for 90 days.",
				Why:    "Owner decision (#1689): a paid, rate-limited way to fix a name."},
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
			{Flag: caravan.EnvStarAmbush, On: caravan.StarAmbushFromEnv(),
				Native: "A caravan ambush has one bandit per cargo star, sometimes one more, and rarely twice as many.",
				Now:    "An ambush is fixed by the cargo's stars: 1 star 2 bandits, 2 stars 3, 3 stars 4, 4 stars 7, 5 stars 10.",
				Why:    "Requested for the beta so a bigger cargo draws a clearly bigger ambush; the original returns with one setting."},
			{Flag: monster.EnvJobMonsterSpeed, On: monster.JobMonsterSpeedFromEnv(),
				Native: "Thief and hunter monsters move at their original speed.",
				Now:    "Thief and hunter monsters walk and run 20% faster.",
				Why:    "Requested for the beta so job ambushes keep up with a transport; the original returns with one setting."},
			{Flag: action.EnvTransportFollow, On: action.TransportFollowFromEnv(),
				Native: "A trade transport you step off stays where it stands, and you cannot walk more than 100 m away from it.",
				Now:    "A trade transport you step off follows you on foot, like a pet.",
				Why:    "Requested for the beta so a trader or a thief can fight or loot beside the cargo; the original returns with one setting."},
			{Flag: action.EnvTransportFollowTether, On: action.TransportFollowFromEnv() && action.TransportFollowTetherFromEnv(),
				Native: "A trade transport you step off stays put; the 100 m limit holds you near where it stands.",
				Now:    "While your transport follows you, you still cannot walk more than 100 m away from it.",
				Why:    "So a trader cannot run far ahead of a transport held back by a fight; it applies only while transports follow."},
			{Flag: action.EnvJobGoodsTeleport, On: action.JobGoodsTeleportFromEnv(),
				Native: "With a trade transport out you cannot use a town gate, a return scroll or the reverse return (ferries are allowed).",
				Now:    "You can use town gates, return scrolls and the reverse return with a trade transport out; the transport travels with you.",
				Why:    "Requested for the beta, for testing trade runs; the original returns with one setting."},
			{Flag: townarea.EnvThiefCartTownAreas, On: thiefCartTownAreasOn,
				Native: "A thief with a transport out is stopped at a town's protected ground; the outer streets of some towns are open ground the cart can reach.",
				Now:    "The cart is also stopped at the walls of those towns (Hotan, Constantinople and Samarkand).",
				Why:    "Owner decision: thieves should not bring stolen goods into any part of a town."},
			{Flag: action.EnvMostWanted, On: action.MostWantedFromEnv(),
				Native: "Job kills earn job EXP only.",
				Now: "Each job kill while suited adds to a streak; every 10 is a wanted level (up to 10), worth +1 STR, +1 INT and +1% move speed each, " +
					"and 1%/2%/3% more damage dealt and less taken at levels 3/5/10. Killing a wanted player pays 100,000,000 gold per level. " +
					"Kills of your own account, party, guild or union never count, and one victim counts at most 3 times an hour. Death or taking the suit off ends the streak.",
				Why: "Requested for the beta, to reward job-war streaks and put a price on them; the original returns with one setting."},
			{Flag: action.EnvMostWantedTest, On: action.MostWantedFromEnv() && action.MostWantedTestFromEnv(),
				Native: "There is no Most Wanted rule.",
				Now: fmt.Sprintf("Most Wanted is in test pacing: %d job kills a wanted level (level 10 at %d), the same player counts every time, and the bounty is %s gold per level.",
					testKills, 10*testKills, groupedGold(settings.Int(tunables.MostWantedTestBounty))),
				Why: "Temporary, for testing the rule quickly without moving real gold; the ordinary rule returns with one setting."},
			{Flag: action.EnvMostWantedBountyTransfer, On: action.MostWantedFromEnv() && action.MostWantedBountyTransferFromEnv(),
				Native: "There is no bounty; a kill moves no gold.",
				Now:    "A wanted player's bounty comes out of their own gold: the killer takes up to 100,000,000 per wanted level, never more than the wanted player carries. No new gold enters the game.",
				Why:    "Requested for the beta so a bounty puts the wanted player's own gold at stake; the original returns with one setting."},
			{Flag: action.EnvMostWantedStreakNotice, On: action.MostWantedFromEnv() && action.MostWantedStreakNoticeFromEnv(),
				Native: "There is no Most Wanted rule.",
				Now:    "After every job kill that counts toward Most Wanted, you see your progress to the next wanted level, such as \"Most Wanted: 3/10 to LV 1\". Only you see it.",
				Why:    "Requested after players could not tell whether their kills counted; the rule without the notice returns with one setting."},
			{Flag: action.EnvMostWantedCOSKills, On: action.MostWantedFromEnv() && action.MostWantedCOSKillsFromEnv(),
				Native: "There is no Most Wanted rule.",
				Now:    "A job kill your attack pet or other companion delivers counts toward your streak, wanted level and bounties as if you had dealt the blow yourself.",
				Why:    "Requested so a companion's killing blow is not lost to the rule; the original returns with one setting."},
			{Flag: action.EnvTradeTiers, On: action.TradeTiersFromEnv(),
				Native: "Every trade good is worth 348 gold, a caravan's stars count its goods against the trader's level, and every route pays its listed price.",
				Now: "Trade goods are worth 21 times as much (7,308 for a 348 good): buying, selling, stolen goods at the Den and the cargo value all scale, so profit, trader job EXP, hunter shares, the weekly trade pool and the fortress trade tax are 21 times larger too. " +
					"Stars follow the cargo's gold value: 1 star below 10M, 2 from 10M, 3 from 20M, 4 from 35M, 5 from 50M (bandits, the trade-scale menu and the stars in the transport's bag follow them). " +
					fmt.Sprintf("Selling between Jangan and another town raises the PROFIT (sale minus cost), both ways: Donwhang +%d%%, Hotan +%d%%, Samarkand +%d%%, Constantinople +%d%%.",
						settings.Int(tunables.TradeRouteDonwhangPercent), settings.Int(tunables.TradeRouteHotanPercent),
						settings.Int(tunables.TradeRouteSamarkandPercent), settings.Int(tunables.TradeRouteConstantinoplePercent)),
				Why: "Requested for the beta so bigger loads and longer roads pay; the original returns with one setting."},
			{Flag: action.EnvTeleportGrace, On: action.TeleportGraceFromEnv(),
				Native: "Only a revival makes you briefly untouchable; a scroll, gate or move arrives as attackable as you left.",
				Now:    "After a return scroll, reverse return scroll, gate or move, you are untouchable for 6 seconds (11 in a fortress war) once loading ends: monsters ignore you and you cannot attack.",
				Why:    "Requested so a slow load cannot end with monsters already attacking; the original returns with one setting."},
			{Flag: action.EnvJobSuitHidesArmor, On: action.JobSuitHidesArmorFromEnv(),
				Native: "Only a thief suit hides the armour worn under it; trader and hunter suits are drawn over it.",
				Now:    "Trader and hunter suits hide worn armour too.",
				Why:    "Requested for the beta, so every job suit reads clearly; the original look returns with one setting."},
			{Flag: action.EnvMoonlitThiefSuit, On: action.MoonlitThiefSuitFromEnv(),
				Native: "The level-20 thief suits look as they always have: a black hood with red trim, or the European suit's own look.",
				Now:    "They wear the Moonlit Ash look: the higher thief suit's outfit in ash-silver silk with pale-gold trim. Only the look changes.",
				Why:    "Picked for the beta by the community's balance advisor; the original look returns with one setting."},
			{Flag: action.EnvFastDenReturn, On: action.FastDenReturnFromEnv(),
				Native: "The Bandit Den Return Scroll counts down 5 minutes before the thief arrives.",
				Now:    "It counts down 2 minutes 30 seconds.",
				Why:    "Owner-approved for the beta; the original countdown returns with one setting."},
			{Flag: action.EnvUniqueDamageRank, On: action.UniqueDamageRankFromEnv(),
				Native: "When a unique dies, the shard is told only who killed it.",
				Now:    "Everyone who damaged it also sees the twelve players who did the most damage, with each one's share and party place, and their own place.",
				Why:    "So every attacker sees what their part was. Off by default; the original notice alone is the native setting."},
			{Flag: movement.EnvCaveSeamStep, On: movement.CaveSeamStepFromEnv(),
				Native: "A monster chasing you through a cave can get stuck pacing at a few doorways.",
				Now:    "It steps through those doorways and keeps following.",
				Why:    "The original's whole-unit steps stop just short of a few doorways; the original step returns with one setting."},
			{Flag: movement.EnvCaveModernChase, On: movement.CaveModernChaseFromEnv(),
				Native: "A monster chasing you through a cave follows the original's room-by-room route, which can leave it pacing back and forth in some rooms.",
				Now:    "It follows a modern path planner that finds the way through the rooms and corridors to you.",
				Why:    "Owner-approved for gameplay: in tests the original route lost more chases; the original route returns with one setting."},
			{Flag: movement.EnvCaveObstacleEscape, On: movement.CaveObstacleEscapeFromEnv(),
				Native: "A monster standing inside the space of a cave pillar cannot step further in, which blocks two ramps in the Donwhang cave for chasing monsters.",
				Now:    "A monster can walk out of a pillar's space in any direction; walking into one from outside is still blocked. Players' own walking is unchanged.",
				Why:    "Owner-approved for gameplay: those ramps trapped chasing monsters; the original pillar test returns with one setting."},
			{Flag: movement.EnvClosedObjectRescue, On: movement.ClosedObjectRescueFromEnv(),
				Native: "A character that logs in inside a tree trunk or a rock stays there and cannot walk out.",
				Now:    "Logging in inside one moves the character to the nearest open ground.",
				Why:    "A player was stuck inside a tree with every move refused; the original returns with one setting."},
			{Flag: movement.EnvDeepWaterRefusal, On: movement.DeepWaterRefusalFromEnv(),
				Native: "A character can walk anywhere the ground allows, including along the bottom of deep water.",
				Now:    "A click on a spot more than 12 units under water is refused.",
				Why:    "An early port rule kept characters out of deep water; the original returns with one setting."},
			{Flag: action.EnvDenNoTransportSummon, On: action.DenNoTransportSummonFromEnv(),
				Native: "A transport can be summoned while the Bandit Den Return Scroll counts down.",
				Now:    "No transport can be summoned during that countdown.",
				Why:    "Thieves left stolen goods on the ground and loaded them just before arriving; the original rule returns with one setting."},
			{Flag: action.EnvGachaPoolV2, On: action.GachaPoolV2FromEnv(),
				Native: "Magic Pop's six weapon prizes are the plain Lv76 weapons, and the losing pool carries no Hwan scroll.",
				Now:    "The six weapons pay their Star (Seal of Star) step at 30 in 10000 each, and a losing ticket can draw a Hwan scroll at 300 in 10000.",
				Why:    "Owner-approved for the beta so a ticket can pay a Star weapon; the retail pool returns with one setting."},
			{Flag: action.EnvTraderCargoDrop, On: action.TraderCargoDropFromEnv(),
				Native: "A trader killed while riding a transport, or logging out with one, takes the goods with it; only the transport's own death drops them.",
				Now: "A trader killed while riding drops the goods around the transport. Logging out outside a town kills a loaded transport where it stands and drops " +
					"its goods; in town the transport is dismissed as before. A parked transport stays when its trader dies, and a server restart drops nothing.",
				Why: "Traders made goods vanish by dying on the transport or logging out when thieves or hunters caught them; the original rule returns with one setting."},
			{Flag: action.EnvPremiumUpgrade, On: action.PremiumUpgradeFromEnv(),
				Native: "A premium time ticket refuses while any premium already runs; the running one must expire first.",
				Now: "Using a strictly higher-grade premium of the same family (a bigger daily allowance first, then a longer period) replaces the running lower one. " +
					"The replaced ticket's unused time is added to the new ticket's end and the daily allowance restarts with the new ticket; lower or equal grades still refuse.",
				Why: "Owner request (#1624): a better premium bought with silk could not be used until a four-week ticket expired."},
			{Flag: enterworld.EnvMallTransportSpeed, On: enterworld.MallTransportSpeedFromEnv(),
				Native: "Item Mall transports (buffalo, rhinoceros, white elephant) move at their listed speed.",
				Now:    "They move 50% faster.",
				Why:    "Requested for the beta; the original speed returns with one setting."},
			{Flag: enterworld.EnvMallRideSpeed, On: rideOn,
				Native: "Item Mall riding vehicles (ostrich, scarab, tiger) and pets move at their listed speed.",
				Now:    fmt.Sprintf("They move %.0f%% faster.", (rideFactor-1)*100),
				Why:    "Requested for the beta; the original speed returns with one setting."},
			{Flag: action.EnvAnnounceSoxDrop, On: action.AnnouncementsFromEnv().SoxDrop,
				Native: "A Seal of Star, Moon or Sun drop is not announced.",
				Now: fmt.Sprintf("Everyone sees a seal drop notice for degree %d and above; a Sun's leads with SEAL OF SUN and its line is coloured, as the Star's and Moon's are by their tiers.",
					settings.Int(tunables.SoxNoticeMinDegree)),
				Why: "Requested for the beta; the original quiet drop returns with one setting."},
			{Flag: action.EnvSealBoard, On: action.SealBoardFromEnv(),
				Native: "A seal drop is the looter's own business; nothing tallies them.",
				Now:    "The site's seal board counts the week's Star, Moon and Sun drops for each job alias, resetting every Monday. Aliases only: no character names, and a player who hides from the public pages is not on it.",
				Why:    "Owner-approved vote (#1604 P9), so the week's seal luck is visible without naming anyone's characters."},
			{Flag: action.EnvAnnouncePlus, On: action.AnnouncementsFromEnv().Plus,
				Native: "Reinforcement results are private.",
				Now:    "Everyone sees \"<player> advanced <item> (<seal>) to +N\" for +7 and above.",
				Why:    "Requested for the beta; the original returns with one setting."},
			{Flag: action.EnvAnnouncePlusTiers, On: action.AnnouncementsFromEnv().PlusTiers,
				Native: "A +10 or +11 success reads like any other.",
				Now:    "A +10 plus notice leads with \"Great success!\" and one of +11 or above with \"LEGENDARY!\"; +7 to +9 read as before.",
				Why:    "Owner-approved (#1739); the plain lines return with one setting."},
			{Flag: action.EnvAnnounceJobKill, On: action.AnnouncementsFromEnv().JobKill,
				Native: "Job kills are not announced.",
				Now:    "Everyone sees which job killed which (\"Thief killed Trader\"), no names.",
				Why:    "Requested for the beta; the original returns with one setting."},
			{Flag: action.EnvAnnounceMostWanted, On: action.AnnouncementsFromEnv().MostWanted,
				Native: "Most Wanted levels and bounties are not announced.",
				Now:    "Everyone sees a player's new Most Wanted level and each claimed bounty, by job alias, no names.",
				Why:    "Requested for the beta; the original returns with one setting."},
			{Flag: action.EnvAlchemyBreakNotice, On: action.AnnouncementsFromEnv().Break,
				Native: "An item destroyed by reinforcement is not announced.",
				Now:    "Everyone sees the destroyed item of a player who opted in on the account page, named by job alias (\"a player\" when they set none), never by character name.",
				Why:    "Owner-approved vote (#1604 P7.2); the original silence returns with one setting, and the opt-in stays off until a player turns it on."},
			{Flag: action.EnvJobKillNoticeRepeat, On: action.AnnouncementsFromEnv().JobKillRepeat,
				Native: fmt.Sprintf("The same two players' job kills are announced at most once every %d minutes.",
					settings.Int(tunables.JobKillRepeatMinutes)),
				Now: fmt.Sprintf("Every job kill between the same two players is announced, up to %d notices a minute.",
					settings.Int(tunables.JobKillNoticesPerMinute)),
				Why: "Requested for the beta, so repeated kills between one pair can be seen; the window returns with one setting."},
			{Flag: action.EnvThiefPartyBonus, On: action.ThiefPartyBonusFromEnv(),
				Native: "A party's thieves change nothing about its monster rewards.",
				Now: fmt.Sprintf("Each party member dressed as a thief adds %d%% monster EXP and gold for the party, up to %d%%.",
					settings.Int(tunables.ThiefPartyPercentEach), settings.Int(tunables.ThiefPartyPercentCap)),
				Why: "Requested for the beta, to make thieves welcome in hunting parties; the original returns with one setting."},
			{Flag: action.EnvTradeJobBonus, On: action.TradeJobBonusFromEnv(),
				Native: "A trade sale's party hunters take their usual share and the trader its usual profit.",
				Now: fmt.Sprintf("The trader earns %d%% more profit per party hunter nearby (up to %d%%), and each hunter %d%% more gold per party trader (up to %d%%).",
					settings.Int(tunables.TradeJobTraderPercentPerHunter), settings.Int(tunables.TradeJobTraderPercentCap),
					settings.Int(tunables.TradeJobHunterPercentPerTrader), settings.Int(tunables.TradeJobHunterPercentCap)),
				Why: "Requested for the beta, so traders and hunters gain from escorting each other; the original returns with one setting."},
			{Flag: monster.EnvQinShiTomb, On: monster.QinShiTombFromEnv(),
				Native: "Qin-Shi Emperor Tomb has its geometry and entrance in the v1.150 client, without monster spawns or walk-in gates; its door moves nobody.",
				Now:    "Qin-Shi Emperor Tomb is open on all six floors: its door by Jangan admits players of level 70 and above, its gates move between floors, and its monsters are awake.",
				Why:    "Opens the Jangan tomb dungeon shipped in the v1.150 client assets (GH #1013); the original returns with one setting."},
			{Flag: action.EnvFlagWarStartUTC, On: true,
				Native: "Capture the Flag runs on the reference server's event schedule.",
				Now: fmt.Sprintf("One Capture the Flag match a day at %02d:%02d UTC; sign-up opens ten minutes before the start at Event So-Ok (level 20 and up).",
					flagWar.StartMinuteOfDay/60, flagWar.StartMinuteOfDay%60),
				Why: "The owner's daily slot, so the one match a day is a meeting point."},
			{Flag: action.EnvFlagWarMinPlayers, On: true,
				Native: "A Capture the Flag match starts when enough players have registered for two teams.",
				Now:    fmt.Sprintf("A match starts with at least %d registered players, split evenly between the two teams; the winning team takes a trophy.", flagWar.MinPlayers),
				Why:    "The owner's house minimum."},
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

/*
================
groupedGold

A gold amount with comma thousands separators, as the rules page writes it.
================
*/
func groupedGold(gold int64) string {
	digits := strconv.FormatInt(gold, 10)
	var out []byte
	for i := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, digits[i])
	}
	return string(out)
}
