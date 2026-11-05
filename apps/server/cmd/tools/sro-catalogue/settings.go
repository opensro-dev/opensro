/*
===========================================================================

settings.go - which shard settings the catalogue models, and why not the rest

The catalogue describes a default deploy, so resolveShardSettings fills
every modelled setting the operator left unset with its deployment
default before any reader looks. This file is the one owner of that
list (#2542): a setting is either modelled here - named with the probe
value proving a build with it differs - or listed in unmodelledSettings
with the reason it has no published surface. A setting in neither list,
or in both, fails the coverage test; a hand-set flag on the settings
table is exactly the drift this replaces (SRO_ATTACK_PET_EXPANSION was
read by the item and pet-gear builders for months under no flag at all).

===========================================================================
*/

package main

import "opensro.online/server/internal/config/shardsettings"

/*
================
modelledSetting

One setting the catalogue reads. probe is the value the coverage test
builds with; companions are held at the same values on both builds, so
the difference is the probed setting's alone. refuses marks a probe
value a build with the shipped data must refuse. variantOnly names why
no shipped-data probe can reach the reader; it is the modelled form of
a reason, and the coverage test requires it to be non-empty.
================
*/
type modelledSetting struct {
	env         string
	probe       string
	companions  map[string]string
	refuses     bool
	variantOnly string
}

var modelledSettings = []modelledSetting{
	// What the shard runs changes what the site publishes: population,
	// levels, shops, prices, drop tables, event rows.
	{env: "SRO_ATTACK_PET_EXPANSION", probe: "on"},
	{env: "SRO_BETA_MASTERY", probe: "off"},
	{env: "SRO_LEVEL_CAP", probe: "110"},
	{env: "SRO_CAP110_MONSTERS", probe: "on", refuses: true},
	{env: "SRO_CAP110_LEVEL100", variantOnly: "read only inside the cap110 population build, which needs the cap110 data files the shipped v1.150 set does not carry; no probe build can reach the branch"},
	{env: "SRO_PHARAOH_TOMB", probe: "on", refuses: true},
	{env: "SRO_CAP110_QINSHI", probe: "on", refuses: true},
	{env: "SRO_CAP110_TEMPLE", probe: "on", refuses: true},
	{env: "SRO_HALLOWEEN_EVENT", probe: "on", companions: map[string]string{
		"SRO_HALLOWEEN_EVENT_WINDOW": "2026-10-31T00:00:00Z/2026-11-07T00:00:00Z"}},
	{env: "SRO_HALLOWEEN_EVENT_WINDOW", probe: "2026-10-31T00:00:00Z/2026-11-07T00:00:00Z", companions: map[string]string{
		"SRO_HALLOWEEN_EVENT": "on"}},
	{env: "SRO_TRADE_TIERS", probe: "off"},
	// The route percents reach trade.json through the tunables
	// snapshot, which a process parses once at boot (tunables.Load):
	// a fresh catalogue process reads them, an in-process probe build
	// cannot move them, so no probe in this test can show their effect.
	{env: "SRO_TRADE_ROUTE_DONWHANG_PERCENT", variantOnly: "read through the tunables snapshot a process parses once at boot; an in-process probe build cannot move it"},
	{env: "SRO_TRADE_ROUTE_HOTAN_PERCENT", variantOnly: "read through the tunables snapshot a process parses once at boot; an in-process probe build cannot move it"},
	{env: "SRO_TRADE_ROUTE_SAMARKAND_PERCENT", variantOnly: "read through the tunables snapshot a process parses once at boot; an in-process probe build cannot move it"},
	{env: "SRO_TRADE_ROUTE_CONSTANTINOPLE_PERCENT", variantOnly: "read through the tunables snapshot a process parses once at boot; an in-process probe build cannot move it"},
	{env: "SRO_ALCHEMY_RATES", probe: "off"},
	{env: "SRO_ALCHEMY_MAX_PLUS", probe: "on"},
	{env: "SRO_BETA_GROWTH", probe: "on"},
	{env: "SRO_BETA_GROWTH_BANDS", probe: "1-110:2", companions: map[string]string{
		"SRO_BETA_GROWTH": "on"}},
	// The growth rates parse only while the growth runs (growth.go),
	// so each rate's probe holds the growth on and flips the rate.
	{env: "SRO_BETA_SKILL_EXP_RATE", probe: "2", companions: map[string]string{
		"SRO_BETA_GROWTH": "on"}},
	{env: "SRO_BETA_DROP_RATE", probe: "2", companions: map[string]string{
		"SRO_BETA_GROWTH": "on"}},
	{env: "SRO_BETA_DROP_MIX", probe: "on"},
	{env: "SRO_SHOP_EXTRA_GOODS", probe: "off"},
	{env: "SRO_JANGAN_ALCHEMY_SHOP", probe: "off"},
	{env: "SRO_ELIXIR_DROPS_OFF", probe: "off"},
	{env: "SRO_ALCHEMY_MATERIAL_DROPS_OFF", probe: "on"},
	{env: "SRO_BETA_DROP_CAP", probe: "3", companions: map[string]string{
		"SRO_BETA_GROWTH": "on"}},
	{env: "SRO_BETA_GOLD_RATE", probe: "2", companions: map[string]string{
		"SRO_BETA_GROWTH": "on"}},
	{env: "SRO_BETA_RARE_RATE", probe: "2", companions: map[string]string{
		"SRO_BETA_GROWTH": "on"}},
	{env: "SRO_BETA_SHOP_ONLY_DEGREE9", probe: "on"},
	{env: "SRO_BETA_SEAL_RATES", probe: "on"},
	{env: "SRO_SUN_EVENT_ONLY", probe: "on"},
	{env: "SRO_SUN_DROP_RATE", probe: "3"},
	{env: "SRO_STACK_SIZES", probe: "off"},
	{env: "SRO_QINSHI_TOMB", probe: "off"},
	{env: "SRO_ALEXANDRIA", variantOnly: "gates the Alexandria NPC overlay and population, and the shipped data carries no Alexandria overlay; no probe build can show its effect"},
}

// unmodelledSettings is every registry setting the catalogue does not
// read, with the reason it has no published surface. Most are runtime
// rules the GameWorld applies live; where a setting is stated for
// players, the shard rules page (cmd/services/sro-gameworld) is the
// surface that states it, and the reason says so.
var unmodelledSettings = map[string]string{
	"SRO_BETA_STARTER_KIT":                    "grants a starter kit at login; nothing the catalogue publishes depends on it",
	"SRO_BETA_PLAYER_MAP":                     "publishes live player positions to the beta map at runtime; the catalogue publishes no positions",
	"SRO_CAP110_SET_BONUSES":                  "a worn set-bonus rule the GameWorld applies; item rows publish their own fields, and the rule is stated on the shard rules page",
	"SRO_CAP110_ITEMS":                        "consumed when the server data is built, not at runtime; the catalogue reads the built data, and the switch is stated on the shard rules page",
	"SRO_CAP110_SKILLS":                       "consumed when the server data is built, not at runtime; the catalogue reads the built data, and the switch is stated on the shard rules page",
	"SRO_PHARAOH_TOMB_GEAR_RATE":              "read while appending the tomb population, but the rate changes no published row; it is stated on the shard rules page",
	"SRO_HALLOWEEN_HELMET_SEAL":               "the event's helmet-seal drop rule at runtime; the catalogue publishes the event window and box, not drop rules",
	"SRO_BETA_SILK":                           "the beta silk grant rule; nothing the catalogue publishes depends on it",
	"SRO_BETA_SILK_ACTIVE_ONLY":               "the beta silk grant rule; nothing the catalogue publishes depends on it",
	"SRO_MAINTENANCE_MESSAGE":                 "the maintenance login banner, served by the agent API; stated on the shard rules page, never in the catalogue",
	"SRO_PET_PACING":                          "companion movement pacing; no published row depends on it",
	"SRO_STORAGE_AUTO_STACK":                  "a storage interaction rule; no published row depends on it",
	"SRO_FAST_DEN_RETURN":                     "the den return rule; no published row depends on it",
	"SRO_UNIQUE_DAMAGE_RANK":                  "the unique-kill damage ranking display; no published row depends on it",
	"SRO_CLOSED_OBJECT_RESCUE":                "a movement placement rescue; no published row depends on it",
	"SRO_CAVE_SEAM_STEP":                      "a cave movement rule; no published row depends on it",
	"SRO_CAVE_MODERN_CHASE":                   "a cave monster chase rule; no published row depends on it",
	"SRO_CAVE_OBSTACLE_ESCAPE":                "a cave movement rule; no published row depends on it",
	"SRO_DEEP_WATER_REFUSAL":                  "a movement refusal rule; no published row depends on it",
	"SRO_MOST_WANTED":                         "the most-wanted bounty rules; no published row depends on them",
	"SRO_MOST_WANTED_TEST":                    "the most-wanted rehearsal switch; no published row depends on it",
	"SRO_MOST_WANTED_TEST_KILLS":              "stated on the shard rules page; no published row depends on it",
	"SRO_MOST_WANTED_TEST_BOUNTY":             "stated on the shard rules page; no published row depends on it",
	"SRO_MOST_WANTED_BOUNTY_TRANSFER":         "the most-wanted bounty transfer rule; no published row depends on it",
	"SRO_MOST_WANTED_STREAK_NOTICE":           "a chat notice switch; no published row depends on it",
	"SRO_MOST_WANTED_COS_KILLS":               "the most-wanted COS kill rule; no published row depends on it",
	"SRO_TELEPORT_GRACE":                      "the revival teleport grace rule; stated on the shard rules page, and no published row depends on it",
	"SRO_PREMIUM_UPGRADE":                     "premium ticket behaviour at use time; item rows publish their own fields",
	"SRO_NAME_CHANGE_SCROLL":                  "name-change scroll behaviour at use time; no published row depends on it",
	"SRO_KILL_STATS":                          "kill statistics recording; no published row depends on it",
	"SRO_JOB_SUIT_HIDES_ARMOR":                "the job suit display rule; no published row depends on it",
	"SRO_JOB_SUIT_TIERS":                      "the job suit buff rule; no published row depends on it",
	"SRO_MOONLIT_THIEF_SUIT":                  "the moonlit thief suit rule; no published row depends on it",
	"SRO_DEN_NO_TRANSPORT_SUMMON":             "the den transport summon rule; no published row depends on it",
	"SRO_GACHA_POOL_V2":                       "the Magic Pop pool choice at draw time; the catalogue publishes no pool, and the switch is stated on the shard rules page",
	"SRO_TRADER_CARGO_DROP":                   "the trader cargo drop rule; no published row depends on it",
	"SRO_MALL_TRANSPORT_SPEED":                "mall transport speed at runtime; no published row depends on it",
	"SRO_MALL_RIDE_SPEED":                     "mall ride speed at runtime; no published row depends on it",
	"SRO_ANNOUNCE_SOX_DROP":                   "a chat notice switch; no published row depends on it",
	"SRO_SEAL_BOARD":                          "the seal board display rule; no published row depends on it",
	"SRO_SEAL_CENSUS":                         "economy census collection; no published row depends on it",
	"SRO_SUN_DROP_COUNT":                      "the runtime Sun drop count rule; the published drop tables carry chances, not counts",
	"SRO_ANNOUNCE_PLUS":                       "a chat notice switch; no published row depends on it",
	"SRO_ANNOUNCE_PLUS_TIERS":                 "a chat notice tier rule; no published row depends on it",
	"SRO_ANNOUNCE_JOB_KILL":                   "a chat notice switch; no published row depends on it",
	"SRO_JOB_KILL_NOTICE_REPEAT":              "stated on the shard rules page; no published row depends on it",
	"SRO_JOB_KILL_NOTICES_PER_MINUTE":         "stated on the shard rules page; no published row depends on it",
	"SRO_MOST_WANTED_NOTICES_PER_MINUTE":      "stated on the shard rules page; no published row depends on it",
	"SRO_JOB_KILL_REPEAT_MINUTES":             "stated on the shard rules page; no published row depends on it",
	"SRO_SOX_NOTICE_MIN_DEGREE":               "stated on the shard rules page; no published row depends on it",
	"SRO_SHORT_NOTICE_SHOW_MS":                "stated on the shard rules page; no published row depends on it",
	"SRO_SHORT_NOTICE_FADE_MS":                "stated on the shard rules page; no published row depends on it",
	"SRO_CHAT_ACK_TIMEOUT_MS":                 "stated on the shard rules page; no published row depends on it",
	"SRO_ANNOUNCE_MOST_WANTED":                "a chat notice switch; no published row depends on it",
	"SRO_ALCHEMY_BREAK_NOTICE":                "a chat notice switch; no published row depends on it",
	"SRO_THIEF_PARTY_BONUS":                   "a party reward split decided at sale time; trade quotes do not depend on it, and the rule is stated on the shard rules page",
	"SRO_THIEF_PARTY_PERCENT_EACH":            "a party reward split decided at sale time; stated on the shard rules page",
	"SRO_THIEF_PARTY_PERCENT_CAP":             "a party reward split decided at sale time; stated on the shard rules page",
	"SRO_TRADE_JOB_BONUS":                     "a party reward split decided at sale time; trade quotes do not depend on it, and the rule is stated on the shard rules page",
	"SRO_TRADE_JOB_TRADER_PERCENT_PER_HUNTER": "a party reward split decided at sale time; stated on the shard rules page",
	"SRO_TRADE_JOB_TRADER_PERCENT_CAP":        "a party reward split decided at sale time; stated on the shard rules page",
	"SRO_TRADE_JOB_HUNTER_PERCENT_PER_TRADER": "a party reward split decided at sale time; stated on the shard rules page",
	"SRO_TRADE_JOB_HUNTER_PERCENT_CAP":        "a party reward split decided at sale time; stated on the shard rules page",
	"SRO_RAISED_GOLD_CAPS":                    "the gold cap rule; no published row depends on it",
	"SRO_REVERSE_RETURN_MAP":                  "a reverse-scroll map data override the runtime loads by path; nothing published reads it",
	"SRO_HUNTING_GUIDE":                       "the in-game hunting guide's data; the catalogue publishes no guide surface",
	"SRO_GM_SETSPEED":                         "a GM tool rule; no published row depends on it",
	"SRO_GM_TRAINING_DUMMY":                   "the GM training dummy switch; no published row depends on it",
	"SRO_TEAM_DEATH_REVIVE":                   "the party revive rule; no published row depends on it",
	"SRO_FLAGWAR_START_UTC":                   "the flag war schedule; the catalogue publishes no schedule",
	"SRO_FLAGWAR_MIN_PLAYERS":                 "the flag war entry rule; the catalogue publishes no schedule",
	"SRO_MALL_EXTRA_GOODS":                    "the item mall's runtime list; the catalogue publishes NPC shops only",
	"SRO_ALCHEMY_NO_ASSIMILATION":             "the alchemy assimilation rule; the published alchemy table carries rates and the ceiling only",
	"SRO_CARAVAN_BANDIT_CAP":                  "the caravan ambush rule; no published row depends on it",
	"SRO_CARAVAN_STAR_AMBUSH":                 "the caravan ambush rule; no published row depends on it",
	"SRO_JOB_MONSTER_SPEED":                   "job monster speed at runtime; no published row depends on it",
	"SRO_JOB_MONSTER_AGGRESSION":              "job monster aggression at runtime; no published row depends on it",
	"SRO_JOB_TRANSPORT_SPEED":                 "job transport speed at runtime; no published row depends on it",
	"SRO_TRANSPORT_FOLLOW":                    "the transport follow rule; no published row depends on it",
	"SRO_TRANSPORT_FOLLOW_TETHER":             "the transport follow rule; no published row depends on it",
	"SRO_JOB_GOODS_TELEPORT":                  "the job goods teleport rule; no published row depends on it",
	"SRO_WHISPER_JOB_ALIAS":                   "the whisper job alias rule; no published row depends on it",
	"SRO_THIEF_CART_TOWN_AREAS":               "the thief cart town-area rule; no published row depends on it",
	"SRO_BETA_UNIQUE_RESPAWN":                 "unique respawn timing, which is never published; no published row depends on it",
	"SRO_UNIQUE_GAP_EXEMPT":                   "a unique loot rule; stated on the shard rules page, while the catalogue publishes native unique counts only",
	"SRO_UNIQUE_POP_TICKET":                   "a unique loot rule; stated on the shard rules page, while the catalogue publishes native unique counts only",
	"SRO_UNIQUE_MAX_LEVEL_LOOT":               "a unique loot rule; stated on the shard rules page, while the catalogue publishes native unique counts only",
	"SRO_UNIQUE_SEAL_DROPS":                   "a unique loot rule; stated on the shard rules page, while the catalogue publishes native unique counts only",
	"SRO_WEBTRANSPORT_PUBLIC":                 "the transport listener rule; stated on the shard rules page, and no published row depends on it",
}

// modelledEnv is the lookup ModelledSetting answers from, built once
// from modelledSettings; the coverage test holds the two in step.
var modelledEnv = func() map[string]bool {
	out := make(map[string]bool, len(modelledSettings))
	for _, m := range modelledSettings {
		out[m.env] = true
	}
	return out
}()

/*
================
ModelledSetting

Whether the catalogue models this setting: the predicate
ApplyCatalogueDefaults fills deployment defaults through.
================
*/
func ModelledSetting(s shardsettings.Setting) bool {
	return modelledEnv[s.Env]
}
