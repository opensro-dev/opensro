/*
===========================================================================

shardsettings.go - the shard settings and their deployment defaults

One table owns every shard-wide setting a deployment chooses: its
environment variable, the GameWorld job's Nomad variable and the value a
deployment uses when the operator names none. sro-nomad builds the job's
variables from it, and sro-catalogue resolves the settings it publishes
from it, so the site describes the shard a default deploy runs. The
GameWorld itself never reads this table: its own unset value stays native.

The job file repeats each default in its variable block (for a manual
"nomad job run"); a test pins the two equal.

Port-only beta rules (SRO_BETA_*) default native here (#606): off, rate 1,
no bands. A deployment names its own beta values in its environment; ours
are ops/release/shard-settings.json, which deploy.py passes to sro-nomad.

===========================================================================
*/
package shardsettings

import "strings"

/*
================
Source

Who fills a setting's job variable.
================
*/
type Source int

const (
	// FromTable: sro-nomad passes the operator's value, else Default.
	FromTable Source = iota
	// Built: sro-nomad computes the value itself (a state file or a parsed
	// list); the table still holds the default the job file and the
	// catalogue use.
	Built
)

/*
================
ValueValidation

The gameplay-owned parser the deployer applies to an effective job value.
These beta controls are port-only, not native. Validation never chooses a
default or changes the setting's value.
================
*/
type ValueValidation int

const (
	ValueUnchecked ValueValidation = iota
	ValueSwitch
	ValueBetaGrowth
	ValueBetaDropMix
	ValueBetaSilk
	// ValueBetaGrowthBands is progression.ParseGrowthBands's band list.
	ValueBetaGrowthBands
)

/*
================
Setting

One shard setting. Catalogue marks the settings sro-catalogue reads (what
the site publishes depends on them); the rest are server only.
================
*/
type Setting struct {
	Env        string
	NomadVar   string
	Default    string
	Catalogue  bool
	Source     Source
	Validation ValueValidation
}

// Settings is the table, in the job file's order.
var Settings = []Setting{
	{Env: "SRO_ATTACK_PET_EXPANSION", NomadVar: "attack_pet_expansion", Default: "off"},
	{Env: "SRO_BETA_STARTER_KIT", NomadVar: "beta_starter_kit", Default: "off", Validation: ValueSwitch},
	{Env: "SRO_BETA_PLAYER_MAP", NomadVar: "beta_player_map", Default: "off", Validation: ValueSwitch},
	{Env: "SRO_BETA_MASTERY", NomadVar: "beta_mastery", Default: "on", Catalogue: true, Source: Built,
		Validation: ValueSwitch},
	// Port-only, not native: the 110 cap. Owner, 2026-10-27: the default is
	// the native 90 (the non-native rule: the off value is native); the
	// cap-110 expansion names 110 when it ships (#1751).
	{Env: "SRO_LEVEL_CAP", NomadVar: "level_cap", Default: "90", Catalogue: true},
	// Port-only, not native: database-authored Alexandria field population.
	{Env: "SRO_CAP110_MONSTERS", NomadVar: "cap110_monsters", Default: "off", Catalogue: true},
	// Port-only, not native: the level-100 slice of that population,
	// admitted for hunting separately (#1741); switches on with the
	// cap-110 catalogue (#1607).
	{Env: "SRO_CAP110_LEVEL100", NomadVar: "cap110_level100", Default: "off", Catalogue: true},
	// Port-only, not native: degree-10/11 equipment set bonuses (#1747);
	// the v1.150 client ships no set rows, so off is the native state.
	{Env: "SRO_CAP110_SET_BONUSES", NomadVar: "cap110_set_bonuses", Default: "off", Catalogue: true},
	// Port-only, not native: the degree-11 equipment and skill tiers
	// (#1745). Consumed at build time, not at runtime: the variant build's
	// import tools (cap110_item_import.py, cap110_skill_import.py) read
	// them when the server data is built, and the GameWorld reads neither.
	// Server only: the catalogue reads the built data, not its inputs.
	{Env: "SRO_CAP110_ITEMS", NomadVar: "cap110_items", Default: "off"},
	{Env: "SRO_CAP110_SKILLS", NomadVar: "cap110_skills", Default: "off"},
	// Port-only, not native: the Pharaoh Tomb 1-3 population and gates. Needs
	// SRO_CAP110_MONSTERS (its equipment drops).
	{Env: "SRO_PHARAOH_TOMB", NomadVar: "pharaoh_tomb", Default: "off", Catalogue: true},
	// Port-only, not native: a Pharaoh Tomb elite or unique rolls equipment this
	// many times as often (1 is native; 2 to 20). Server only: the site's drop
	// lists describe the native rule.
	{Env: "SRO_PHARAOH_TOMB_GEAR_RATE", NomadVar: "pharaoh_tomb_gear_rate", Default: "1"},
	{Env: "SRO_BETA_SILK", NomadVar: "beta_silk", Default: "off", Validation: ValueBetaSilk},
	{Env: "SRO_BETA_SILK_ACTIVE_ONLY", NomadVar: "beta_silk_active_only", Default: "off", Validation: ValueSwitch},
	// Grab and growth pets at four fifths of their run speed (#320): never
	// deployed, so the deployment default stays native.
	{Env: "SRO_PET_PACING", NomadVar: "pet_pacing", Default: "off"},
	{Env: "SRO_STORAGE_AUTO_STACK", NomadVar: "storage_auto_stack", Default: "on"},
	// Owner, 2026-10-14: the Bandit Den scroll counts down half as long.
	{Env: "SRO_FAST_DEN_RETURN", NomadVar: "fast_den_return", Default: "on"},
	// Port-only, off by default: a unique's attackers see its damage ranking.
	{Env: "SRO_UNIQUE_DAMAGE_RANK", NomadVar: "unique_damage_rank", Default: "off"},
	// Owner, 2026-10-15: a login inside a closed object (a tree trunk) is
	// rescued to the nearest open ground.
	{Env: "SRO_CLOSED_OBJECT_RESCUE", NomadVar: "closed_object_rescue", Default: "on"},
	// Owner, 2026-10-26: the native cave route's doorway seam step (#1698):
	// a short landing step that would stay short of the doorway rounds on
	// toward it. Off is the native step.
	{Env: "SRO_CAVE_SEAM_STEP", NomadVar: "cave_seam_step", Default: "on"},
	{Env: "SRO_CAVE_MODERN_CHASE", NomadVar: "cave_modern_chase", Default: "on"},
	{Env: "SRO_CAVE_OBSTACLE_ESCAPE", NomadVar: "cave_obstacle_escape", Default: "on"},
	// Port-only, not native: the deep-water destination refusal. Off, as the
	// original server; on, a character on a deep seabed was stuck there.
	{Env: "SRO_DEEP_WATER_REFUSAL", NomadVar: "deep_water_refusal", Default: "off"},
	// Owner, 2026-10-14: the job-kill streak, its bonuses and bounty.
	{Env: "SRO_MOST_WANTED", NomadVar: "most_wanted", Default: "on"},
	// Owner, 2026-10-15: temporary Most Wanted test pacing.
	{Env: "SRO_MOST_WANTED_TEST", NomadVar: "most_wanted_test", Default: "off"},
	// Owner, 2026-10-15 (F10): the test pacing's numbers (game/tunables).
	{Env: "SRO_MOST_WANTED_TEST_KILLS", NomadVar: "most_wanted_test_kills", Default: "5"},
	{Env: "SRO_MOST_WANTED_TEST_BOUNTY", NomadVar: "most_wanted_test_bounty", Default: "1000000"},
	// Owner and the balance advisor, 2026-10-15: the bounty is the victim's gold.
	{Env: "SRO_MOST_WANTED_BOUNTY_TRANSFER", NomadVar: "most_wanted_bounty_transfer", Default: "on"},
	// Owner, 2026-10-24: the killer's own progress notice per counted kill.
	{Env: "SRO_MOST_WANTED_STREAK_NOTICE", NomadVar: "most_wanted_streak_notice", Default: "off"},
	// Owner, 2026-10-14: trade stars by cargo gold, Jangan route profit.
	{Env: "SRO_TRADE_TIERS", NomadVar: "trade_tiers", Default: "on", Catalogue: true},
	// Owner, 2026-10-25: the revival's grace after a teleport arrival
	// (port-only, not native; off is native, #1518).
	{Env: "SRO_TELEPORT_GRACE", NomadVar: "teleport_grace", Default: "off", Catalogue: true},
	// Owner, 2026-10-25: a strictly higher-grade premium ticket replaces
	// the running lower one; the replaced ticket's remaining time moves
	// the new ticket's end later (#1624, port-only, not native; "off"
	// refuses a second ticket while one runs).
	{Env: "SRO_PREMIUM_UPGRADE", NomadVar: "premium_upgrade", Default: "off"},
	// Owner, 2026-10-25: count kills and EXP per online hour by level
	// band for the staff console (#1639, port-only, not native; "off"
	// counts nothing and the economy route answers 404).
	{Env: "SRO_KILL_STATS", NomadVar: "kill_stats", Default: "off"},
	// F10 trade group (game/tunables): the Jangan route profit percents. The
	// site's trade quotes read them, so the catalogue does too.
	{Env: "SRO_TRADE_ROUTE_DONWHANG_PERCENT", NomadVar: "trade_route_donwhang_percent", Default: "50", Catalogue: true},
	{Env: "SRO_TRADE_ROUTE_HOTAN_PERCENT", NomadVar: "trade_route_hotan_percent", Default: "100", Catalogue: true},
	{Env: "SRO_TRADE_ROUTE_SAMARKAND_PERCENT", NomadVar: "trade_route_samarkand_percent", Default: "200", Catalogue: true},
	{Env: "SRO_TRADE_ROUTE_CONSTANTINOPLE_PERCENT", NomadVar: "trade_route_constantinople_percent", Default: "300",
		Catalogue: true},
	// Owner, 2026-10-14: trader and hunter suits hide worn armour. Owner,
	// 2026-10-24: off (native) again; the rule stays available.
	{Env: "SRO_JOB_SUIT_HIDES_ARMOR", NomadVar: "job_suit_hides_armor", Default: "off"},
	// Owner, 2026-10-15: level-20 thief suits wear the Moonlit Ash look.
	{Env: "SRO_MOONLIT_THIEF_SUIT", NomadVar: "moonlit_thief_suit", Default: "on"},
	// Owner, 2026-10-14: no transport summon during the Bandit Den countdown.
	{Env: "SRO_DEN_NO_TRANSPORT_SUMMON", NomadVar: "den_no_transport_summon", Default: "on"},
	// Owner, 2026-10-20: Magic Pop prize pool v2 (port-only); the site
	// publishes the pool's odds, so the catalogue reads the setting too.
	{Env: "SRO_GACHA_POOL_V2", NomadVar: "gacha_pool_v2", Default: "on", Catalogue: true},
	// Owner, 2026-10-14: a trader's goods drop on rider death and field logout.
	{Env: "SRO_TRADER_CARGO_DROP", NomadVar: "trader_cargo_drop", Default: "on"},
	// Owner, 2026-10-14: Item Mall transports move 1.5 times as fast.
	{Env: "SRO_MALL_TRANSPORT_SPEED", NomadVar: "mall_transport_speed", Default: "on"},
	// Owner, 2026-10-15: Item Mall vehicles and pets move 30% faster.
	{Env: "SRO_MALL_RIDE_SPEED", NomadVar: "mall_ride_speed", Default: "on"},
	// Owner, 2026-10-14: shard notices for seal drops, +7 and above, and job kills.
	{Env: "SRO_ANNOUNCE_SOX_DROP", NomadVar: "announce_sox_drop", Default: "on"},
	// Owner, 2026-10-25 (#1604 P9): the weekly seal board counts Star,
	// Moon and Sun drops per job alias. Off is native: nothing counts.
	{Env: "SRO_SEAL_BOARD", NomadVar: "seal_board", Default: "off"},
	// Owner, 2026-10-26 (#1604 P13, #1691): the staff seal census counts
	// seal items on the shard by degree and tier, name-free, staff-only.
	// Off is native: the route does not exist.
	{Env: "SRO_SEAL_CENSUS", NomadVar: "seal_census", Default: "off"},
	{Env: "SRO_ANNOUNCE_PLUS", NomadVar: "announce_plus", Default: "on"},
	// Owner, 2026-10-26 (#1739): the plus notice leads louder at +10 and
	// +11 and above. Off is the plain line, byte for byte.
	{Env: "SRO_ANNOUNCE_PLUS_TIERS", NomadVar: "announce_plus_tiers", Default: "off"},
	{Env: "SRO_ANNOUNCE_JOB_KILL", NomadVar: "announce_job_kill", Default: "on"},
	// Owner, 2026-10-15: every job kill of a pair is announced (no window).
	{Env: "SRO_JOB_KILL_NOTICE_REPEAT", NomadVar: "job_kill_notice_repeat", Default: "on"},
	// Owner, 2026-10-15 (F10): the notice caps and the pair window (game/tunables).
	{Env: "SRO_JOB_KILL_NOTICES_PER_MINUTE", NomadVar: "job_kill_notices_per_minute", Default: "6"},
	{Env: "SRO_MOST_WANTED_NOTICES_PER_MINUTE", NomadVar: "most_wanted_notices_per_minute", Default: "6"},
	{Env: "SRO_JOB_KILL_REPEAT_MINUTES", NomadVar: "job_kill_repeat_minutes", Default: "10"},
	{Env: "SRO_SOX_NOTICE_MIN_DEGREE", NomadVar: "sox_notice_min_degree", Default: "6"},
	// F10 uiTuning: the client values sent at world entry (game/tunables).
	{Env: "SRO_SHORT_NOTICE_SHOW_MS", NomadVar: "short_notice_show_ms", Default: "5000"},
	{Env: "SRO_SHORT_NOTICE_FADE_MS", NomadVar: "short_notice_fade_ms", Default: "1000"},
	{Env: "SRO_CHAT_ACK_TIMEOUT_MS", NomadVar: "chat_ack_timeout_ms", Default: "10000"},
	// Owner, 2026-10-15: Most Wanted levels and claimed bounties are announced.
	{Env: "SRO_ANNOUNCE_MOST_WANTED", NomadVar: "announce_most_wanted", Default: "on"},
	// Owner-approved vote, 2026-10-26 (#1604 P7.2 / #1738): an opted-in
	// player's destroyed reinforcement item is announced by job alias.
	// Off is native, in code and in the deployed default alike.
	{Env: "SRO_ALCHEMY_BREAK_NOTICE", NomadVar: "alchemy_break_notice", Default: "off"},
	// Owner, 2026-10-14: +10% party EXP and gold per thief (cap 50%).
	{Env: "SRO_THIEF_PARTY_BONUS", NomadVar: "thief_party_bonus", Default: "on"},
	// F10 trade group (game/tunables): the per-thief percent and its cap.
	{Env: "SRO_THIEF_PARTY_PERCENT_EACH", NomadVar: "thief_party_percent_each", Default: "10"},
	{Env: "SRO_THIEF_PARTY_PERCENT_CAP", NomadVar: "thief_party_percent_cap", Default: "50"},
	// Owner, 2026-10-14: trader +10%/hunter (cap 20%), hunter +10%/trader (cap 60%).
	{Env: "SRO_TRADE_JOB_BONUS", NomadVar: "trade_job_bonus", Default: "on"},
	// F10 trade group (game/tunables): the four trader and hunter percents.
	{Env: "SRO_TRADE_JOB_TRADER_PERCENT_PER_HUNTER", NomadVar: "trade_job_trader_percent_per_hunter", Default: "10"},
	{Env: "SRO_TRADE_JOB_TRADER_PERCENT_CAP", NomadVar: "trade_job_trader_percent_cap", Default: "20"},
	{Env: "SRO_TRADE_JOB_HUNTER_PERCENT_PER_TRADER", NomadVar: "trade_job_hunter_percent_per_trader", Default: "10"},
	{Env: "SRO_TRADE_JOB_HUNTER_PERCENT_CAP", NomadVar: "trade_job_hunter_percent_cap", Default: "60"},
	{Env: "SRO_REVERSE_RETURN_MAP", NomadVar: "reverse_return_map", Default: ""},
	{Env: "SRO_HUNTING_GUIDE", NomadVar: "hunting_guide", Default: ""},
	{Env: "SRO_GM_SETSPEED", NomadVar: "gm_setspeed", Default: "on"},
	{Env: "SRO_GM_TRAINING_DUMMY", NomadVar: "gm_training_dummy", Default: "off"},
	{Env: "SRO_ALCHEMY_RATES", NomadVar: "alchemy_rates", Default: "on", Catalogue: true},
	// Owner, 2026-10-18: reversed the 2026-10-13 +11 cap; the deployment
	// default stays native ("off", ceiling 250).
	{Env: "SRO_ALCHEMY_MAX_PLUS", NomadVar: "alchemy_max_plus", Default: "off", Catalogue: true},
	{Env: "SRO_TEAM_DEATH_REVIVE", NomadVar: "team_death_revive", Default: "on"},
	// Owner, 2026-10-24: the daily Capture the Flag match (#887) - its UTC
	// start (HH:MM) and the house minimum; the GameWorld refuses bad values.
	{Env: "SRO_FLAGWAR_START_UTC", NomadVar: "flagwar_start_utc", Default: "20:00"},
	{Env: "SRO_FLAGWAR_MIN_PLAYERS", NomadVar: "flagwar_min_players", Default: "4"},
	{Env: "SRO_BETA_GROWTH", NomadVar: "beta_growth", Default: "off", Catalogue: true, Validation: ValueSwitch},
	// Unset keeps every level at the level-1 pace; our deployment's bands
	// (#1606: 1-59 at the pace, 60-110 on one smooth curve) are in
	// ops/release/shard-settings.json.
	{Env: "SRO_BETA_GROWTH_BANDS", NomadVar: "beta_growth_bands", Default: "",
		Catalogue: true, Validation: ValueBetaGrowthBands},
	{Env: "SRO_BETA_SKILL_EXP_RATE", NomadVar: "beta_skill_exp_rate", Default: "1", Catalogue: true,
		Validation: ValueBetaGrowth},
	{Env: "SRO_BETA_DROP_RATE", NomadVar: "beta_drop_rate", Default: "1", Catalogue: true, Validation: ValueBetaGrowth},
	{Env: "SRO_BETA_DROP_MIX", NomadVar: "beta_drop_mix", Default: "off", Catalogue: true, Validation: ValueBetaDropMix},
	{Env: "SRO_SHOP_EXTRA_GOODS", NomadVar: "shop_extra_goods", Default: "on", Catalogue: true},
	{Env: "SRO_MALL_EXTRA_GOODS", NomadVar: "mall_extra_goods", Default: "on"},
	{Env: "SRO_ALCHEMY_NO_ASSIMILATION", NomadVar: "alchemy_no_assimilation", Default: "on"},
	// Owner, 2026-10-14: 8 per ambush (was 16), thieves and hunters alike.
	{Env: "SRO_CARAVAN_BANDIT_CAP", NomadVar: "caravan_bandit_cap", Default: "8"},
	// Owner, 2026-10-15: a fixed ambush per caravan star (2/3/4/7/10).
	{Env: "SRO_CARAVAN_STAR_AMBUSH", NomadVar: "caravan_star_ambush", Default: "on"},
	// Owner, 2026-10-15: thief and hunter monsters move 20% faster.
	{Env: "SRO_JOB_MONSTER_SPEED", NomadVar: "job_monster_speed", Default: "on"},
	// Owner, 2026-10-18: reversed the 2026-10-15 follow setting; the
	// deployment default stays native ("off").
	{Env: "SRO_TRANSPORT_FOLLOW", NomadVar: "transport_follow", Default: "off"},
	// Owner, 2026-10-24: a following transport keeps the 100 m tether. It
	// acts only with SRO_TRANSPORT_FOLLOW on.
	{Env: "SRO_TRANSPORT_FOLLOW_TETHER", NomadVar: "transport_follow_tether", Default: "on"},
	// Owner, 2026-10-18: reversed the 2026-10-15 testing switch; the
	// deployment default stays native ("off", town gates refuse a transport).
	{Env: "SRO_JOB_GOODS_TELEPORT", NomadVar: "job_goods_teleport", Default: "off"},
	{Env: "SRO_ELIXIR_DROPS_OFF", NomadVar: "elixir_drops_off", Default: "on", Catalogue: true},
	// Community vote, 2026-10-24: the monster parts drop again, so the
	// deployment default is native ("off"); "on" restores the 2026-10-14
	// beta removal.
	{Env: "SRO_ALCHEMY_MATERIAL_DROPS_OFF", NomadVar: "alchemy_material_drops_off", Default: "off", Catalogue: true},
	{Env: "SRO_BETA_DROP_CAP", NomadVar: "beta_drop_cap", Default: "0", Catalogue: true, Validation: ValueBetaGrowth},
	// Owner, 2026-10-19: gold falls at 100 times retail (was 250).
	{Env: "SRO_BETA_GOLD_RATE", NomadVar: "beta_gold_rate", Default: "1", Catalogue: true, Validation: ValueBetaGrowth},
	{Env: "SRO_INSTANT_INVENTORY_EXPANSION", NomadVar: "instant_inventory_expansion", Default: "on"},
	{Env: "SRO_BETA_RARE_RATE", NomadVar: "beta_rare_rate", Default: "1", Catalogue: true, Validation: ValueBetaGrowth},
	{Env: "SRO_BETA_SHOP_ONLY_DEGREE9", NomadVar: "beta_shop_only_degree9", Default: "off", Catalogue: true,
		Validation: ValueSwitch},
	{Env: "SRO_BETA_SEAL_RATES", NomadVar: "beta_seal_rates", Default: "off", Catalogue: true, Validation: ValueSwitch},
	// Owner, 2026-10-14 (narrowed 2026-10-18): degree-9 Sun gear only from
	// unique monsters; superseded 2026-10-20 for drops (Sun drops at the
	// rate below; this gate now defaults off and only an operator's explicit
	// "on" still excludes). The shop half never returned: NPC Sun withholding
	// is permanent and does not read this setting (action/commerce.go).
	{Env: "SRO_SUN_EVENT_ONLY", NomadVar: "sun_event_only", Default: "off", Catalogue: true},
	// Owner, 2026-10-20: the degree-9 Sun drop rate (class mass 30 x rate
	// on levels 76-90). Owner, 2026-10-25 (#1605 P1): the widened class
	// is capped at the Moon class's width, so a Sun expects about twice
	// a Moon's kills; the cap supersedes the eleven-a-week calibration.
	{Env: "SRO_SUN_DROP_RATE", NomadVar: "sun_drop_rate", Default: "2", Catalogue: true},
	// Owner, 2026-10-20: uniques ignore the level-gap drop admission and
	// always leave exactly one Magic Pop ticket; "off" is native.
	{Env: "SRO_UNIQUE_GAP_EXEMPT", NomadVar: "unique_gap_exempt", Default: "on", Catalogue: true},
	{Env: "SRO_UNIQUE_POP_TICKET", NomadVar: "unique_pop_ticket", Default: "on", Catalogue: true},
	// Owner, 2026-10-25 (narrowed 2026-10-26): a unique in the top loot
	// band (own level within ten of the highest installed level: degree 9
	// at cap 90, degree 11 at cap 110) rolls its equipment at that level;
	// every other unique keeps its own level. Opt-in: "off" is native.
	{Env: "SRO_UNIQUE_MAX_LEVEL_LOOT", NomadVar: "unique_max_level_loot", Default: "off", Catalogue: true},
	// Owner, 2026-10-25 (narrowed 2026-10-26): every unique kill leaves one
	// Seal of Star of the top degree and a Moon one in ten; no guaranteed
	// Sun. Opt-in: "off" is native.
	{Env: "SRO_UNIQUE_SEAL_DROPS", NomadVar: "unique_seal_drops", Default: "off", Catalogue: true},
	// Owner, 2026-10-18: respawning unique nests draw over 45-90 minutes
	// instead of the operator backup's 20-36 minute rows. "off" keeps the
	// authored row.
	{Env: "SRO_BETA_UNIQUE_RESPAWN", NomadVar: "beta_unique_respawn", Default: "off", Validation: ValueSwitch},
	// Owner, 2026-10-13 (F7): Lucky Powder stacks to 500; 2026-10-14:
	// arrows and bolts stack to 2500. "off" is native. The catalogue
	// publishes the caps the shard runs, so it reads the setting too.
	{Env: "SRO_STACK_SIZES", NomadVar: "stack_sizes", Default: "arrow=2500,bolt=2500,luckypowder=500", Catalogue: true, Source: Built},
	// Owner, 2026-10-17: Qin-Shi Emperor Tomb monsters, skills and walk-in
	// portals (port-only, not native: v1.150 shipped Tomb visual assets
	// before the Legend IV server tables); opened in the shard defaults
	// 2026-10-21 (GH #1013). "off" is native.
	{Env: "SRO_QINSHI_TOMB", NomadVar: "qinshi_tomb", Default: "on", Catalogue: true},
	// The Alexandria city of a build_alexandria_variant.py game root: its
	// regions, gates and NPCs (port-only, not native; internal/game/world/
	// alexandria). "off" is native and hides them from a variant's data;
	// the catalogue's NPC list follows it.
	{Env: "SRO_ALEXANDRIA", NomadVar: "alexandria", Default: "off", Catalogue: true},
	// Port-only, not native: WebTransport as a public endpoint (epic
	// #1451). "off" keeps the loopback/development transport; "on" lets
	// the GameWorld serve WT publicly under its explicit TLS pair and
	// origin list. Server only: clients learn endpoints from the shard
	// catalogue, never from this switch.
	{Env: "SRO_WEBTRANSPORT_PUBLIC", NomadVar: "webtransport_public", Default: "off"},
}

// NotSettings are the job's other SRO_ variables: what a deployment
// derives (identity, paths, topology, release), not a rule it chooses.
// Each says why it is not in Settings; a test keeps the job file, Settings
// and this list in step.
var NotSettings = map[string]string{
	"SRO_SHARD_ID":                         "shard identity, from the catalogue",
	"SRO_SHARD_CATALOG_PATH":               "path to the shard catalogue",
	"SRO_AUTHORITY_STATE_DIR":              "path to the shard's authority store",
	"SRO_SERVER_GAME_DATA_ROOT":            "path to the released server game data",
	"SRO_SERVER_GAME_DATA_MANIFEST_DIGEST": "identity of the released server game data",
	"SRO_GM_CHARACTERS":                    "operator GM grants, from the state directory",
	"SRO_GAMEWORLD_PRIVATE_NETWORK":        "host topology",
	"SRO_BENCHMARK_FIXTURE_CONTROL":        "derived from the host network and namespace",
	"SRO_MOVE_PATH_GUARD":                  "movement enforcement mode, operational",
	"SRO_MOVE_CLIENT_CLIP":                 "movement enforcement mode, operational",
	"SRO_MAINTENANCE_GATE_PATH":            "release path: the maintenance gate file",
	"SRO_PUBLIC_API_ADDR":                  "the public API listener, per shard",
	"SRO_TRANSPORT_TLS_ID":                 "transport certificate identity",
	"SRO_RELEASE_ID":                       "release identity",
}

/*
================
Value

The operator's value for a setting, else its deployment default.
================
*/
func (s Setting) Value(getenv func(string) string) string {
	if v := strings.TrimSpace(getenv(s.Env)); v != "" {
		return v
	}
	return s.Default
}

/*
================
Lookup

The setting named by its environment variable. A name outside the table
is a programming error.
================
*/
func Lookup(env string) Setting {
	for _, s := range Settings {
		if s.Env == env {
			return s
		}
	}
	panic("shardsettings: no setting " + env)
}

/*
================
JobVariables

Adds every table-filled setting to the GameWorld job's variables.
================
*/
func JobVariables(getenv func(string) string, variables map[string]any) map[string]any {
	for _, s := range Settings {
		if s.Source == FromTable {
			variables[s.NomadVar] = s.Value(getenv)
		}
	}
	return variables
}

/*
================
ApplyCatalogueDefaults

For each setting the catalogue reads that the operator left unset, sets
its deployment default through setenv, so the catalogue's readers (the
same parsers the GameWorld uses) see what a default deploy runs. Returns
the names it filled.
================
*/
func ApplyCatalogueDefaults(getenv func(string) string, setenv func(string, string) error) ([]string, error) {
	var filled []string
	for _, s := range Settings {
		if !s.Catalogue || strings.TrimSpace(getenv(s.Env)) != "" || s.Default == "" {
			continue
		}
		if err := setenv(s.Env, s.Default); err != nil {
			return filled, err
		}
		filled = append(filled, s.Env)
	}
	return filled, nil
}
