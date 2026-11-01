# ===========================================================================
# gameworld.nomad.hcl - Nomad lifecycle, placement, and secret delivery for the service.
# ===========================================================================

variable "node_kernel" {
  type        = string
  default     = "windows"
  description = "attr.kernel.name of the nodes that may run this job (windows or linux)"
}

variable "task_user" {
  type        = string
  default     = ""
  description = "OS account the task runs as; empty keeps the Nomad client's account"
}

variable "task_uid" {
  type    = number
  default = -1
}

variable "task_gid" {
  type    = number
  default = -1
}

variable "datacenters" {
  type    = list(string)
  default = ["dc1"]
}

variable "nomad_namespace" {
  type = string
}

variable "shard_id" {
  type = string
}

variable "binary_path" {
  type = string
}

variable "catalog_path" {
  type = string
}

variable "authority_dir" {
  type = string
}

variable "server_game_data_root" {
  type = string
}

variable "server_game_data_manifest_digest" {
  type = string
}

variable "cert_dir" {
  type = string
}

variable "transport_cert_file" {
  type    = string
  default = ""
}

variable "transport_key_file" {
  type    = string
  default = ""
}

variable "transport_tls_id" {
  type = string
}

# "on" serves WebTransport as a public endpoint (SRO_WEBTRANSPORT_PUBLIC):
# the GameWorld then requires the explicit TLS pair above and the allowed
# origins, and the WT listener may bind beyond loopback. "off" (default)
# keeps WebTransport a loopback/development transport.
variable "webtransport_public" {
  type    = string
  default = "off"
}

# The public UDP host:port the WT listener binds when webtransport_public
# is on. Empty (default) keeps the Nomad-allocated loopback bind; the
# deployer refuses the public switch without it.
variable "transport_wt_addr" {
  type    = string
  default = ""
}

variable "host_network" {
  type    = string
  default = "loopback"
}

variable "control_port" {
  type = number
}

variable "transport_port" {
  type = number
}

variable "release_id" {
  type = string
}

variable "private_network" {
  type    = string
  default = "0"
}

# "1" serves /debug/pprof/ on its own loopback listener at pprof_addr
# (transport/pprof.go; sro-nomad deploy -pprof, on by default for the first
# shard). The address must be a loopback IP; the GameWorld refuses any other.
variable "transport_pprof" {
  type    = string
  default = "0"
}

variable "pprof_addr" {
  type    = string
  default = "127.0.0.1:8792"
}

variable "allowed_origins" {
  type = string
}

variable "gm_characters" {
  type    = string
  default = ""
}

variable "move_path_guard" {
  type    = string
  default = "enforce"
}

variable "move_client_clip" {
  type    = string
  default = "apply"
}

# Beta operator switches ("on" or "off"). beta_starter_kit gives every
# character an unlimited return scroll and +100% speed scroll (backfilled on
# entry); beta_player_map shows every online player on the world map (M).
variable "beta_starter_kit" {
  type    = string
  default = "off"
}

variable "beta_player_map" {
  type    = string
  default = "off"
}

# Beta builds share the 5000 total mastery budget. Set off for native
# CH 300 / EU min(2 * level, 240); individual mastery ceilings never change.
variable "beta_mastery" {
  type    = string
  default = "on"
}

# Port-only, not native: 110 raises the stopping level on the installed
# v1.150 curve. Off keeps cap 90; expansion content is enabled separately.
# Owner, 2026-10-27: the default is the native 90 (the non-native rule: the
# off value is native); the cap-110 expansion names 110 when it ships (#1751).
variable "level_cap" {
  type    = string
  default = "90"
}

# Port-only, not native: optional database-authored Alexandria field content.
variable "cap110_monsters" {
  type    = string
  default = "off"
}

# Port-only, not native: the level-100 slice of that population, admitted
# for hunting separately (#1741); switches on with the cap-110 catalogue.
variable "cap110_level100" {
  type    = string
  default = "off"
}

# Port-only, not native: degree-10/11 equipment set bonuses (#1747). The
# v1.150 client ships no set rows, so off is the native state.
variable "cap110_set_bonuses" {
  type    = string
  default = "off"
}

# Port-only, not native: the degree-11 equipment and skill tiers (#1745),
# consumed at build time by the variant build's import tools; the
# GameWorld reads neither at runtime.
variable "cap110_items" {
  type    = string
  default = "off"
}

variable "cap110_skills" {
  type    = string
  default = "off"
}

# Port-only, not native: the optional Pharaoh Tomb 1-3 population. It needs
# cap110_monsters on as well; GameWorld refuses to start otherwise.
variable "pharaoh_tomb" {
  type    = string
  default = "off"
}

# Port-only, not native: the optional Qin-Shi tomb upper-floor population
# (#1800). It needs cap110_monsters on as well; GameWorld refuses to start
# otherwise.
variable "cap110_qinshi" {
  type    = string
  default = "off"
}

# Port-only, not native: the temple band instances (#1800 step 3). It
# needs cap110_monsters on as well; GameWorld refuses to start otherwise.
variable "cap110_temple" {
  type    = string
  default = "off"
}

# Port-only, not native: the name-change scroll (#1689) - the Item Mall
# lists it for 500 silk and a use queues a rename for the next login.
# "off" is native: names never change.
variable "name_change_scroll" {
  type    = string
  default = "off"
}

# Port-only, not native: a Pharaoh Tomb elite or unique rolls its equipment
# this many times as often. "1" is native; "2" to "20" raises it.
variable "pharaoh_tomb_gear_rate" {
  type    = string
  default = "1"
}

# beta_silk is the silk one in-world hour earns into the account's real Item
# Mall wallet (BUG-062, port-only): earning pauses at 1500, and a new account
# starts with 300. Earned silk is REAL and survives "off", which only stops
# new credits; a launch wipes or zeroes it. "on" is 50.
variable "beta_silk" {
  type    = string
  default = "off"
}

# beta_silk_active_only credits the beta silk hour only while the character
# has acted within ten minutes (port-only, not native). "off" is the shipped
# rule: world-ready time earns.
variable "beta_silk_active_only" {
  type    = string
  default = "off"
}

# maintenance_gate_path is the maintenance release gate (ops/release
# deploy.py): while the file exists, EnterWorld tokens go only to the
# accounts it lists. Empty disables the gate.
variable "maintenance_gate_path" {
  type    = string
  default = ""
}

# pet_pacing moves grab and growth pets at four fifths of their run speed
# (port-only, not native). "off" is native.
variable "pet_pacing" {
  type    = string
  default = "off"
}

# storage_auto_stack lets a storage quick move (right-click, Ctrl+click) land
# on a matching stack (port-only, not native; owner decision 2026-10-11).
# "off" restores the native empty-slot rule.
variable "storage_auto_stack" {
  type    = string
  default = "on"
}

# fast_den_return halves the Bandit Den Return Scroll's countdown (port-only,
# not native; owner decision 2026-10-14). "off" restores the native 300 s.
variable "fast_den_return" {
  type    = string
  default = "on"
}

# unique_damage_rank shows everyone who damaged a unique the twelve players
# who did the most damage when it dies (port-only, not native). "off", the
# default, keeps the native killer notice alone.
variable "unique_damage_rank" {
  type    = string
  default = "off"
}

# cave_seam_step lets a monster on the native cave route step through a
# doorway whose landing point a whole-unit step would stop short of
# (port-only, not native; owner decision 2026-10-26). "off" is the
# original step, which can leave the monster pacing at such a doorway.
variable "cave_seam_step" {
  type    = string
  default = "on"
}

# cave_modern_chase sends a monster chasing through a cave along the port's
# planner (A* over the cells, string-pulled) instead of the original's
# region route (port-only, not native; owner decision 2026-10-26). "off" is
# the original route, which can leave a monster pacing on a cell line.
variable "cave_modern_chase" {
  type    = string
  default = "on"
}

# cave_obstacle_escape lets a mover whose step starts inside a cave
# obstacle circle walk out of it (port-only, not native; owner decision
# 2026-10-26). "off" is the original circle test, which stops every step
# deeper into a circle that covers a ramp doorway.
variable "cave_obstacle_escape" {
  type    = string
  default = "on"
}

# closed_object_rescue moves a character that logs in inside a closed object
# (a tree trunk or rock no walk can leave) to the nearest open ground
# (port-only, not native; owner decision 2026-10-15). "off" keeps the login
# rescue to blocked tiles, islands and sealed decks.
variable "closed_object_rescue" {
  type    = string
  default = "on"
}

# deep_water_refusal refuses a click whose destination lies more than 12
# units under a water surface (port-only, not native: the original server
# reads no water height). "off" is native; with it on, a character walked
# onto a deep seabed cannot click its way out (BR-261023-2025-B5DA).
variable "deep_water_refusal" {
  type    = string
  default = "off"
}

# most_wanted counts a suited player's job kills into a wanted level
# (bonuses per level, a bounty on their head) (port-only, not native;
# owner decision 2026-10-14). "off" is native.
variable "most_wanted" {
  type    = string
  default = "on"
}

# most_wanted_test is Most Wanted's temporary test pacing: 5 kills a level,
# no anti-farm window, a 1,000,000 gold bounty a level (port-only, not
# native). "off", the default, is the ordinary rule (owner 2026-10-15: a
# shard-wide rule is the wrong tool for one tester's checks).
# If it is ever switched on, DO NOT switch it off again without clearing
# every Most Wanted streak in the same window (issue #1061): a test-earned
# level is worth 100,000,000 gold a level under the ordinary rule.
variable "most_wanted_test" {
  type    = string
  default = "off"
}

# most_wanted_test_kills and most_wanted_test_bounty are the test pacing's
# kills per wanted level (1, 2, 5 or 10) and gold per level (0..100000000);
# they act only with most_wanted_test on (port-only, game/tunables).
variable "most_wanted_test_kills" {
  type    = string
  default = "5"
}

variable "most_wanted_test_bounty" {
  type    = string
  default = "1000000"
}

# job_kill_notices_per_minute and most_wanted_notices_per_minute cap each
# notice kind per division a minute (1..30); job_kill_repeat_minutes is how
# long one killer/victim pair stays quiet (1..60) with the repeat rule off
# (port-only, game/tunables).
variable "job_kill_notices_per_minute" {
  type    = string
  default = "6"
}

variable "most_wanted_notices_per_minute" {
  type    = string
  default = "6"
}

variable "job_kill_repeat_minutes" {
  type    = string
  default = "10"
}

variable "sox_notice_min_degree" {
  type    = string
  default = "6"
}

# short_notice_show_ms (3000..7000) and short_notice_fade_ms (500..2000) time
# the Experimental "Short notices" banner; chat_ack_timeout_ms (5000..30000)
# is the client's wait for a chat confirmation. Each player takes them at
# the next world entry (port-only, game/tunables class Entry).
variable "short_notice_show_ms" {
  type    = string
  default = "5000"
}

variable "short_notice_fade_ms" {
  type    = string
  default = "1000"
}

variable "chat_ack_timeout_ms" {
  type    = string
  default = "10000"
}

# most_wanted_bounty_transfer pays a wanted player's bounty out of that
# player's own gold, at most what they carry, instead of minting it
# (port-only, not native; owner decision 2026-10-15). "off" mints it.
variable "most_wanted_bounty_transfer" {
  type    = string
  default = "on"
}

# most_wanted_streak_notice tells the killer "Most Wanted: N/10 to LV L"
# after every counted kill (port-only, not native; owner decision
# 2026-10-24). "off" says nothing until a new wanted level.
variable "most_wanted_streak_notice" {
  type    = string
  default = "off"
}

# most_wanted_cos_kills counts a job kill delivered on a companion for
# Most Wanted like a player kill, credited to the companion's owner
# (port-only, not native; owner decision 2026-10-28). "off", the
# default, is native.
variable "most_wanted_cos_kills" {
  type    = string
  default = "off"
}

# teleport_grace makes a teleport arrival (scroll, gate, relocation)
# untouchable for the revival's grace from game-ready (port-only, not
# native; owner decision 2026-10-25). "off" is native.
variable "teleport_grace" {
  type    = string
  default = "off"
}

# premium_upgrade lets a strictly higher-grade premium ticket replace the
# running lower one (owner decision 2026-10-25, #1624). Port-only, not
# native; "off" refuses a second ticket while one runs.
variable "premium_upgrade" {
  type    = string
  default = "off"
}

# kill_stats counts kills and EXP per online hour by level band for the
# staff console (#1639, #1604 P10). Port-only, not native; "off" counts
# nothing and the economy route answers 404.
variable "kill_stats" {
  type    = string
  default = "off"
}

# trade_tiers rates a caravan's stars by its cargo's gold value (5M, 10M,
# 20M, 35M, 50M) and raises trade profit between Jangan and Donwhang +50%,
# Hotan +100%, Samarkand +200%, Constantinople +300% (port-only, not
# native; owner decision 2026-10-14). "off" is native.
variable "trade_tiers" {
  type    = string
  default = "on"
}

# trade_route_*_percent are the Jangan route profit bonuses under
# trade_tiers (0..500 each; port-only, game/tunables, live: a staff
# approval changes them without a restart).
variable "trade_route_donwhang_percent" {
  type    = string
  default = "50"
}

variable "trade_route_hotan_percent" {
  type    = string
  default = "100"
}

variable "trade_route_samarkand_percent" {
  type    = string
  default = "200"
}

variable "trade_route_constantinople_percent" {
  type    = string
  default = "300"
}

# job_suit_hides_armor makes trader and hunter suits hide the armour worn
# under them, as thief suits do (port-only, not native; owner decision
# 2026-10-14). "off" is native, and the default again since the owner's
# 2026-10-24 decision.
variable "job_suit_hides_armor" {
  type    = string
  default = "off"
}

# moonlit_thief_suit draws the level-20 thief suits in the Moonlit Ash
# look (port-only, not native; owner decision 2026-10-15). "off" is native.
variable "moonlit_thief_suit" {
  type    = string
  default = "on"
}

# den_no_transport_summon refuses a transport summon during the Bandit Den
# Return Scroll's countdown (port-only, not native; owner decision
# 2026-10-14). "off" restores the native rule, which allows it.
variable "den_no_transport_summon" {
  type    = string
  default = "on"
}

# gacha_pool_v2 swaps Magic Pop's six weapon prizes for the Lv76 Star
# steps and adds a Hwan waste row (port-only, not native; owner decision
# 2026-10-20). "off" restores the retail gachaitemset.txt pool.
variable "gacha_pool_v2" {
  type    = string
  default = "on"
}

# trader_cargo_drop keeps a loaded transport's goods in the world when its
# trader dies riding it or logs out outside a town (port-only, not native;
# owner decision 2026-10-14). "off" is native.
variable "trader_cargo_drop" {
  type    = string
  default = "on"
}

# mall_transport_speed makes the Item Mall transports (buffalo, rhinoceros,
# white elephant) walk and run 1.5 times as fast (port-only, not native;
# owner decision 2026-10-14). "off" is native.
variable "mall_transport_speed" {
  type    = string
  default = "on"
}

# mall_ride_speed makes the Item Mall's riding vehicles and pets walk and
# run 1.3 times as fast (port-only, not native; owner decision 2026-10-15).
# "off" is native.
variable "mall_ride_speed" {
  type    = string
  default = "on"
}

# announce_sox_drop, announce_plus and announce_job_kill send shard notices
# for a seal drop, an item advanced to +7 or above and a job kill (port-only,
# not native; owner decision 2026-10-14, lowered to +7 and uncapped
# 2026-10-18). "off" is native: no notice.
variable "announce_sox_drop" {
  type    = string
  default = "on"
}

# seal_board counts sealed drops for the site's weekly seal board, by job
# alias with Star, Moon and Sun apart (port-only, not native; owner
# decision 2026-10-25, #1604 P9). "off" is native: nothing counts.
variable "seal_board" {
  type    = string
  default = "off"
}

# seal_census opens the staff-only seal census route (the console's
# sealcensus tab counts seal items by degree and tier, name-free; port-only,
# not native; owner decision 2026-10-26, #1604 P13 / #1691). "off" is
# native: the route does not exist.
variable "seal_census" {
  type    = string
  default = "off"
}

variable "announce_plus" {
  type    = string
  default = "on"
}

# announce_plus_tiers leads a +10 plus notice with "Great success!" and
# a +11 or higher one with "LEGENDARY!" (port-only, not native; owner
# decision 2026-10-26, #1739). "off" is the plain plus line.
variable "announce_plus_tiers" {
  type    = string
  default = "off"
}

variable "announce_job_kill" {
  type    = string
  default = "on"
}

# job_kill_notice_repeat announces every job kill between the same pair,
# with no 10-minute pair window (port-only, not native; owner decision
# 2026-10-15). The per-minute shard cap stays. "off" restores the window.
variable "job_kill_notice_repeat" {
  type    = string
  default = "on"
}

# announce_most_wanted announces a new Most Wanted level and a claimed
# bounty by job alias on the job-kill channel (port-only, not native; owner
# decision 2026-10-15). "off" is native: no notice.
variable "announce_most_wanted" {
  type    = string
  default = "on"
}

# alchemy_break_notice announces the destroyed reinforcement item of a
# character who opted in on the account page, by job alias or "a player",
# never a character name (port-only, not native; owner-approved vote
# 2026-10-26, #1604 P7.2 / #1738). "off" is native: no notice.
variable "alchemy_break_notice" {
  type    = string
  default = "off"
}

# thief_party_bonus gives a party +10% monster EXP and gold per member
# dressed as a thief, up to +50% (port-only, not native; owner decision
# 2026-10-14). "off" is native.
variable "thief_party_bonus" {
  type    = string
  default = "on"
}

# thief_party_percent_each and thief_party_percent_cap are thief_party_bonus'
# per-thief percent (0..50) and its cap (0..200) (port-only, game/tunables,
# live).
variable "thief_party_percent_each" {
  type    = string
  default = "10"
}

variable "thief_party_percent_cap" {
  type    = string
  default = "50"
}

# trade_job_bonus pays a trader +10% profit per party hunter (up to 20%)
# and each hunter +10% gold per party trader (up to 60%) on a trade sale
# (port-only, not native; owner decision 2026-10-14). "off" is native.
variable "trade_job_bonus" {
  type    = string
  default = "on"
}

# trade_job_*_percent_* are trade_job_bonus' percents: the trader's per
# hunter (0..50) and cap (0..100), each hunter's per trader (0..50) and cap
# (0..200) (port-only, game/tunables, live).
variable "trade_job_trader_percent_per_hunter" {
  type    = string
  default = "10"
}

variable "trade_job_trader_percent_cap" {
  type    = string
  default = "20"
}

variable "trade_job_hunter_percent_per_trader" {
  type    = string
  default = "10"
}

variable "trade_job_hunter_percent_cap" {
  type    = string
  default = "60"
}

# raised_gold_caps raises the stall price cap to ten billion and the
# warehouse gold deposit cap to one billion (port-only, not native).
# Anything but "on" keeps the native caps (one billion and one hundred
# million) and the native u32 stall-offer wire.
variable "raised_gold_caps" {
  type    = string
  default = "off"
}

# reverse_return_map adds map destinations to the reverse return scroll
# (port-only, not native). Anything but "on" keeps the native two points.
# On, the destinations are the VSRO client's refoptionalteleport table when
# the game data was built after scripts/import_vsro_optional_teleport.py,
# else the inferred town gates and unique monster areas.
variable "reverse_return_map" {
  type    = string
  default = ""
}

# hunting_guide publishes the optional static atlas (port-only, not native).
# Unset or off builds and publishes nothing extra.
variable "hunting_guide" {
  type    = string
  default = ""
}

# Optional GM-only target references; off leaves the native roster unchanged.
variable "gm_training_dummy" {
  type    = string
  default = "off"
}

# gm_setspeed admits a GM's /SETSPEED (port-only, not native; owner decision
# 2026-10-12). Anything but "on" refuses it, as v1.150 has no such command.
variable "gm_setspeed" {
  type    = string
  default = "on"
}

# alchemy_rates replaces each elixir's authored reinforcement chance with one
# table by plus (port-only, not native; owner decision 2026-10-11): "on" is
# +1 100% ... +11 5%, a comma list such as "100,80,70" is custom, "off" is
# the original game. A malformed value stops GameWorld at boot.
variable "alchemy_rates" {
  type    = string
  default = "on"
}

# alchemy_max_plus caps reinforcement (port-only, not native; owner decision
# 2026-10-18 reversed 2026-10-13): "on" is +11, a number is that plus, "off"
# is the original game's own ceiling. A malformed value stops GameWorld at
# boot.
variable "alchemy_max_plus" {
  type    = string
  default = "off"
}

# team_death_revive: "on" (native, the default; owner decision 2026-10-13)
# revives a free-battle or arena death where it fell at any level; "off" is
# port-only, the town rule. A malformed value stops GameWorld at boot.
variable "team_death_revive" {
  type    = string
  default = "on"
}

# flagwar_start_utc / flagwar_min_players: the daily Capture the Flag
# match (#887, owner 2026-10-24): its UTC start as HH:MM and the house
# minimum (2-16). Malformed values stop GameWorld at boot.
variable "flagwar_start_utc" {
  type    = string
  default = "20:00"
}

variable "flagwar_min_players" {
  type    = string
  default = "4"
}

# beta_growth holds every level's EXP and skill EXP to the level-1 kill pace,
# multiplies skill EXP by beta_skill_exp_rate on top, makes a kill's assigned
# rewards and ordinary drop passes beta_drop_rate times over (the unique
# prepass and the gold heap count stay native), multiplies gold heaps by
# beta_gold_rate and makes a rare (SoX) equipment drop beta_rare_rate times as
# likely. beta_drop_cap keeps a kill to that many ordinary items (a uniform
# random subset; gold heaps and a unique's prepass are never cut; 0 = no cap).
# "off" restores the native rates.
variable "attack_pet_expansion" {
  type = string
  default = "off"
}

variable "beta_growth" {
  type    = string
  default = "off"
}

# beta_growth_bands gives level bands their own EXP/SP multiplier: "pace"
# holds a level to the level-1 pace, a number is that many times native
# (owner, 2026-10-24). Empty keeps every level at the pace.
variable "beta_growth_bands" {
  type    = string
  default = ""
}

# whisper_job_alias arms the port-only job-alias whisper rules (#1982;
# owner, 2026-10-28: default ON): a whisper typed to a dressed player's
# job alias resolves to them, and the whisper block list also matches a
# dressed sender's alias. "off" restores the native name-only whisper
# resolution. The alias/name collision refusals are unconditional and
# not behind this switch.
variable "whisper_job_alias" {
  type    = string
  default = "on"
}

variable "beta_skill_exp_rate" {
  type    = string
  default = "1"
}

variable "beta_drop_rate" {
  type    = string
  default = "1"
}

# beta_drop_mix gives item families their own beta drop rate (port-only, not
# native; owner decision 2026-10-11). "on" keeps potions, cures, scrolls,
# ammo, elixirs, stones and tablets at the native rate while equipment and a
# monster's assigned rewards keep beta_drop_rate; "family=rate,..." tunes it
# (families: equipment, assigned, potion, cure, scroll, elixir, ammo, stone,
# tablet); "off" is the one uniform beta rate. A malformed value stops
# GameWorld at boot.
variable "beta_drop_mix" {
  type    = string
  default = "off"
}

# shop_extra_goods adds NPC shop goods beyond the v1.150 stock (port-only,
# not native; owner decisions 2026-10-11 and 2026-10-13). "on" sells the
# elixirs at 1M and the degree 9 Luck, Steady, Astral, Athanasia and Repair
# stones at 5M at Jangan's smith, the other degree 9 stones at 2M at Jangan's
# grocer and the vigor potions at every herbalist (commerce/extragoods.go);
# "tab:item:price,..." is an operator list; "off" is the v1.150 stock.
# A malformed value stops GameWorld at boot.
variable "shop_extra_goods" {
  type    = string
  default = "on"
}

# jangan_alchemy_shop renames Jangan's grocer "OpenSRO Alchemy - Jangan"
# and regroups the extra-goods alchemy stock onto three tabs (port-only,
# not native; owner decision 2026-10-28, #1850). It acts only while
# shop_extra_goods is "on"; "off" restores the native name and tabs.
# A malformed value stops GameWorld at boot.
variable "jangan_alchemy_shop" {
  type    = string
  default = "on"
}

# mall_extra_goods lists authored Item Mall packages the v1.150 mall does not
# sell (port-only, not native; owner decision 2026-10-13). "on" adds the fur,
# Halloween, pirate, holy knight and wing outfits at their authored prices
# (commerce/mallextra.go); "tab:package,..." is an operator list; "off" is
# the shipped mall. A malformed value stops GameWorld at boot.
variable "mall_extra_goods" {
  type    = string
  default = "on"
}

# alchemy_no_assimilation removes stone assimilation (port-only, not native;
# owner decision 2026-10-13): "on" makes a successful attribute or magic
# stone change only its own stat; "off" is the original game. A malformed
# value stops GameWorld at boot.
variable "alchemy_no_assimilation" {
  type    = string
  default = "on"
}

# caravan_bandit_cap caps the bandits per caravan ambush, the thieves sent
# at a trader and the hunters sent at a thief alike (port-only, not native;
# owner decisions 2026-10-13 and 2026-10-14, 16 then 8): a count, or "off"
# for the original game's uncapped count. A malformed value stops GameWorld
# at boot.
variable "caravan_bandit_cap" {
  type    = string
  default = "8"
}

# caravan_star_ambush replaces the rolled ambush count with a fixed one per
# caravan star: 1 star 2, 2 stars 3, 3 stars 4, 4 stars 7, 5 stars 10
# (port-only, not native; owner decision 2026-10-15). "off" is the
# original roll, capped by caravan_bandit_cap.
variable "caravan_star_ambush" {
  type    = string
  default = "on"
}

# job_monster_speed makes the job system's thief and hunter monsters walk
# and run 20% faster (port-only, not native; owner decision 2026-10-15).
# "off" is their original speed.
variable "job_monster_speed" {
  type    = string
  default = "on"
}

# transport_follow has a trade transport its trader or thief stepped off
# walk after them instead of holding them within 100 m of it (port-only,
# not native; owner decision 2026-10-18 reversed 2026-10-15). "off" is the
# original parked transport and its tether (0x4FD1D0).
variable "transport_follow" {
  type    = string
  default = "off"
}

# transport_follow_tether keeps the 100 m tether on a following transport:
# the trader may not walk more than 100 m from where it walks (port-only,
# not native; owner decision 2026-10-24). It acts only with transport_follow
# on; with that off the original parked transport tethers as always.
variable "transport_follow_tether" {
  type    = string
  default = "on"
}

# job_goods_teleport lets a trader with a transport out use town gates,
# return scrolls and the reverse return, the transport travelling along
# (port-only, not native; owner decision 2026-10-18 reversed 2026-10-15).
# "off" is native.
variable "job_goods_teleport" {
  type    = string
  default = "off"
}

# thief_cart_town_areas stops a thief's transport at the town walls that
# regioninfo.txt places in battlefield regions, not only at a town's
# protected regions (port-only, not native; owner decision 2026-10-28,
# #1985). "off" is native. A malformed value stops GameWorld at boot.
variable "thief_cart_town_areas" {
  type    = string
  default = "on"
}

# beta_shop_only_degree9 keeps degree-9 Lucky Powder, magic stones and
# attribute stones out of every monster drop (port-only, not native, beta
# balance; owner 2026-10-13): players buy them from the Jangan shops. "off"
# keeps today's drops.
variable "beta_shop_only_degree9" {
  type    = string
  default = "off"
}

# elixir_drops_off keeps every elixir out of monster drops (port-only, not
# native, beta balance; owner 2026-10-13): the blacksmith's shop_extra_goods
# sells them instead, so "on" with a shop list lacking any of the eight
# elixirs stops GameWorld at boot. "off" is the original game's drops.
variable "elixir_drops_off" {
  type    = string
  default = "on"
}

# alchemy_material_drops_off keeps every alchemy material (type 3/3/11/4,
# the grey bags of monster parts) out of monster drops (port-only, not
# native, beta balance; owner 2026-10-14). A community vote on 2026-10-24
# brought the parts back, so the default is "off", the original game's
# drops; "on" restores the beta removal.
variable "alchemy_material_drops_off" {
  type    = string
  default = "off"
}

variable "beta_drop_cap" {
  type    = string
  default = "0"
}

# 100 keeps gold per kill near its level before #463 stopped multiplying the
# heap count by beta_drop_rate (owner, 2026-10-10; rate lowered 250 to 100,
# owner 2026-10-19).
variable "beta_gold_rate" {
  type    = string
  default = "1"
}

variable "beta_rare_rate" {
  type    = string
  default = "1"
}

# beta_seal_rates multiplies each rare seal's chance on top of beta_rare_rate
# (port-only, not native, beta balance; owner 2026-10-13): degrees 1-8 make
# Star, Moon and Sun three times as likely, degree 9 Star three times and
# Moon twice, Sun as it is. "off" keeps every seal at beta_rare_rate.
variable "beta_seal_rates" {
  type    = string
  default = "off"
}

# sun_event_only keeps degree-9 Seal of Sun gear out of non-unique monster
# drops; unique monsters may still drop it, and gear players own is kept
# (port-only, not native; owner decision 2026-10-14, narrowed to non-unique
# kills 2026-10-18, gate default flipped off 2026-10-20 when drops became
# the Sun source at sun_drop_rate). NPC shops never sell it either way:
# that withholding is permanent and does not read this.
variable "sun_event_only" {
  type    = string
  default = "off"
}

# sun_drop_rate widens the degree-9 Sun class of the rare drop table by
# 30 times this rate on levels 76-90 (port-only, not native; owner
# decision 2026-10-20). "0" is native.
variable "sun_drop_rate" {
  type    = string
  default = "2"
}

# unique_gap_exempt lets a unique's drops ignore the native level-gap
# admission (4C1840), so a capped hunter still loots low-level uniques
# (port-only, not native; owner decision 2026-10-20). "off" is native.
variable "unique_gap_exempt" {
  type    = string
  default = "on"
}

# unique_pop_ticket leaves exactly one Magic Pop ticket per unique kill:
# the native assigned cards are retired into the one guaranteed card
# (port-only, not native; owner decision 2026-10-20). "off" is native.
variable "unique_pop_ticket" {
  type    = string
  default = "on"
}

# unique_max_level_loot rolls a top-band unique's equipment at the shard's
# highest installed level (degree 9 at cap 90, degree 11 at cap 110)
# rather than the unique's own level; a unique counts as top-band within
# ten levels of that level, and every other unique keeps its own level
# (port-only, not native; owner decision 2026-10-25, narrowed 2026-10-26).
# Opt-in: "off" is native.
variable "unique_max_level_loot" {
  type    = string
  default = "off"
}

# unique_seal_drops guarantees one Seal of Star of the top degree per
# unique kill and a Seal of Moon on a kill in ten; no Seal of Sun is
# guaranteed (port-only, not native; owner decision 2026-10-25, narrowed
# 2026-10-26). "off" is native and is the default.
variable "unique_seal_drops" {
  type    = string
  default = "off"
}

# beta_unique_respawn draws respawning unique monster nests over 45-90 minutes
# instead of the operator backup's 20-36 minute rows (port-only, not native;
# owner decision 2026-10-18). "off" keeps the authored row.
variable "beta_unique_respawn" {
  type    = string
  default = "off"
}

# stack_sizes raises item stack caps per group (port-only, not native), e.g.
# "potion=2000,elixir=50". Groups: potion, petpotion, universalpill, elixir,
# luckypowder, magicstone, attrstone (stones merge only with an equal
# assimilation value), arrow, bolt.
# The deployment default raises Lucky Powder to 500 (owner, 2026-10-13) and
# arrows and bolts to 2500 (owner, 2026-10-14).
# Empty or "off" keeps every itemdata cap; an unknown group or a cap outside
# 1..65535 stops GameWorld at boot, so a host's own stack_sizes may name a
# group only once a release that knows it is live.
variable "stack_sizes" {
  type    = string
  default = "arrow=2500,bolt=2500,luckypowder=500"
}

# qinshi_tomb opens Qin-Shi Emperor Tomb on all six floors (79 monster
# species, 230 skills, 1,033 spawn nests, 68 teleport links and 64 walk-in
# event zones) (port-only, not native; owner decision 2026-10-17, opened
# in the shard defaults 2026-10-21, GH #1013). "off" is native.
variable "qinshi_tomb" {
  type    = string
  default = "on"
}

# alexandria registers the Alexandria city a variant game data carries: its
# regions, gates and NPCs (port-only, not native). "off" is native; "on"
# refuses to start without the variant's overlay.
variable "alexandria" {
  type    = string
  default = "off"
}

# The community site's public read API listener (internal/agent/publicstats):
# a loopback address, or "off". sro-nomad gives the first shard the default
# and every other shard "off".
variable "public_api_addr" {
  type    = string
  default = "127.0.0.1:8790"
}

# The GameWorld's tick must never wait behind another job's burst.
# Nomad turns a task's cpu (MHz) into its cgroup CPU weight, so under
# contention each task's share is its own reservation over the sum of
# the contending reservations; weights cost nothing on an idle host.
# The default therefore outweighs the Agent's reservation (500) by
# more than ten to one. An operator value (-gameworld-cpu) replaces
# it: keep it at least ten times the largest other reservation on the
# host, and below the node's total compute, or the job cannot place.
variable "cpu" {
  type    = number
  default = 6000
}

# The live shard keeps about 560 MB of heap and 970 MB of runtime memory
# (2026-10-28, #1943). At 1024 the soft limit below (768MiB) sat under
# that: the collector ran about once a second, its assists and write
# barriers landing on the tick, with RSS 800 MB of a 1024 MB cgroup.
# 3072 gives a 2304MiB soft limit and room for GOGC below to work.
variable "memory_mb" {
  type    = number
  default = 3072
}

# GOGC for the GameWorld (owner decision 2026-10-28, #1943): 200 lets
# the heap grow to three times the live set between collections, half
# as many cycles as Go's default 100, still bounded by GOMEMLIMIT.
variable "gogc" {
  type    = string
  default = "200"
}

# sro-nomad replaces __SHARD_ID__ with the validated catalog ID before
# submitting this template. Nomad job IDs are block labels, not HCL values,
# so they cannot reference an input variable directly.
job "sro-gameworld-__SHARD_ID__" {
  namespace   = var.nomad_namespace
  datacenters = var.datacenters
  type        = "service"

  meta {
    sro_release_id       = var.release_id
    sro_transport_tls_id = var.transport_tls_id
  }

  constraint {
    attribute = "${attr.kernel.name}"
    value     = var.node_kernel
  }

  # A shard is stateful. Only a node whose operator-owned metadata explicitly
  # claims this shard may run its sole writer.
  constraint {
    attribute = "${meta.sro_shards}"
    operator  = "set_contains"
    value     = var.shard_id
  }

  update {
    max_parallel      = 1
    min_healthy_time  = "10s"
    healthy_deadline  = "3m"
    progress_deadline = "5m"
    auto_revert       = true
  }

  group "gameworld" {
    count = 1

    ephemeral_disk {
      size = 2048
    }

    network {
      mode = "host"

      port "control" {
        static       = var.control_port
        host_network = var.host_network
      }

      # One number is reserved for both the TCP WebSocket/HTTP listener and
      # the UDP WebTransport listener. raw_exec uses host networking, so no
      # port translation is involved.
      port "transport" {
        static       = var.transport_port
        host_network = var.host_network
      }
    }

    restart {
      attempts = 5
      interval = "1m"
      # A crashed process cannot release its 10-second Agent ownership lease.
      # Wait beyond that lease before starting a fresh process identity so one
      # crash consumes one restart attempt rather than several refused boots.
      delay = "12s"
      mode  = "fail"
    }

    reschedule {
      attempts       = 10
      interval       = "1h"
      delay          = "5s"
      delay_function = "exponential"
      max_delay      = "2m"
      unlimited      = false
    }

    task "gameworld" {
      driver = "raw_exec"
      # Empty on Windows (the Nomad service account runs the task). On Linux
      # the deployer names a dedicated unprivileged account.
      user = var.task_user

      config {
        command = var.binary_path
        # Keep host-level control-plane and cloud credentials out of this
        # unisolated native process. Nomad's allocation variables remain.
        denied_envvars = [
          "NOMAD_TOKEN",
          "NOMAD_LICENSE",
          "NOMAD_LICENSE_PATH",
          "CONSUL_*",
          "VAULT_*",
          "AWS_*",
          "AZURE_*",
          "ARM_*",
          "GOOGLE_*",
          "GITHUB_*",
          "GITLAB_*",
        ]
      }

      env {
        # Soft Go-runtime budget: three quarters of the task's memory, the
        # rest left to non-heap memory. It was a flat 128MiB, below what one
        # player's world already keeps live (about 100MB of monsters, nav and
        # references): the collector then ran almost without pause and took
        # 82% of 3.6 cores with one player online.
        GOMEMLIMIT                              = "${floor(var.memory_mb * 3 / 4)}MiB"
        GOGC                                    = var.gogc
        SRO_SHARD_CATALOG_PATH                  = var.catalog_path
        SRO_SHARD_ID                            = var.shard_id
        SRO_AGENT_URL_REQUIRE                   = "1"
        SRO_AGENT_IDENTITY_FILE                 = "${NOMAD_SECRETS_DIR}/nomad_sro_agent.jwt"
        SRO_AGENT_SESSION_PUBLIC_KEYS_PATH      = "${NOMAD_SECRETS_DIR}/agent-session-public-keys.json"
        SRO_AUTHORITY_STATE_DIR                 = var.authority_dir
        SRO_AUTHORITY_REQUIRE                   = "1"
        SRO_GM_CHARACTERS                       = var.gm_characters
        SRO_SERVER_GAME_DATA_ROOT               = var.server_game_data_root
        SRO_SERVER_GAME_DATA_MANIFEST_DIGEST    = var.server_game_data_manifest_digest
        SRO_GAMEWORLD_CONTROL_ADDR              = "${NOMAD_IP_control}:${NOMAD_PORT_control}"
        SRO_GAMEWORLD_PRIVATE_NETWORK           = var.private_network
        SRO_BENCHMARK_FIXTURE_CONTROL           = var.host_network == "loopback" && var.nomad_namespace == "default" ? "1" : "0"
        SRO_MOVE_PATH_GUARD                     = var.move_path_guard
        SRO_MOVE_CLIENT_CLIP                    = var.move_client_clip
        SRO_BETA_STARTER_KIT                    = var.beta_starter_kit
        SRO_BETA_PLAYER_MAP                     = var.beta_player_map
        SRO_BETA_MASTERY                        = var.beta_mastery
        SRO_LEVEL_CAP                      = var.level_cap
        SRO_CAP110_MONSTERS                = var.cap110_monsters
        SRO_CAP110_LEVEL100                = var.cap110_level100
        SRO_CAP110_SET_BONUSES             = var.cap110_set_bonuses
        SRO_CAP110_ITEMS                   = var.cap110_items
        SRO_CAP110_SKILLS                  = var.cap110_skills
        SRO_PHARAOH_TOMB                   = var.pharaoh_tomb
        SRO_CAP110_QINSHI                  = var.cap110_qinshi
        SRO_CAP110_TEMPLE                  = var.cap110_temple
        SRO_NAME_CHANGE_SCROLL             = var.name_change_scroll
        SRO_PHARAOH_TOMB_GEAR_RATE         = var.pharaoh_tomb_gear_rate
        SRO_BETA_SILK                           = var.beta_silk
        SRO_BETA_SILK_ACTIVE_ONLY               = var.beta_silk_active_only
        SRO_MAINTENANCE_GATE_PATH               = var.maintenance_gate_path
        SRO_PET_PACING                          = var.pet_pacing
        SRO_STORAGE_AUTO_STACK                  = var.storage_auto_stack
        SRO_FAST_DEN_RETURN                     = var.fast_den_return
        SRO_UNIQUE_DAMAGE_RANK                  = var.unique_damage_rank
        SRO_CLOSED_OBJECT_RESCUE                = var.closed_object_rescue
        SRO_CAVE_SEAM_STEP                      = var.cave_seam_step
        SRO_CAVE_MODERN_CHASE                   = var.cave_modern_chase
        SRO_CAVE_OBSTACLE_ESCAPE                = var.cave_obstacle_escape
        SRO_DEEP_WATER_REFUSAL                  = var.deep_water_refusal
        SRO_MOST_WANTED                         = var.most_wanted
        SRO_MOST_WANTED_TEST                    = var.most_wanted_test
        SRO_MOST_WANTED_TEST_KILLS              = var.most_wanted_test_kills
        SRO_MOST_WANTED_TEST_BOUNTY             = var.most_wanted_test_bounty
        SRO_MOST_WANTED_BOUNTY_TRANSFER         = var.most_wanted_bounty_transfer
        SRO_MOST_WANTED_STREAK_NOTICE           = var.most_wanted_streak_notice
        SRO_MOST_WANTED_COS_KILLS               = var.most_wanted_cos_kills
        SRO_TRADE_TIERS                         = var.trade_tiers
        SRO_TELEPORT_GRACE                      = var.teleport_grace
        SRO_PREMIUM_UPGRADE                     = var.premium_upgrade
        SRO_KILL_STATS                          = var.kill_stats
        SRO_TRADE_ROUTE_DONWHANG_PERCENT        = var.trade_route_donwhang_percent
        SRO_TRADE_ROUTE_HOTAN_PERCENT           = var.trade_route_hotan_percent
        SRO_TRADE_ROUTE_SAMARKAND_PERCENT       = var.trade_route_samarkand_percent
        SRO_TRADE_ROUTE_CONSTANTINOPLE_PERCENT  = var.trade_route_constantinople_percent
        SRO_JOB_SUIT_HIDES_ARMOR                = var.job_suit_hides_armor
        SRO_MOONLIT_THIEF_SUIT                  = var.moonlit_thief_suit
        SRO_DEN_NO_TRANSPORT_SUMMON             = var.den_no_transport_summon
        SRO_GACHA_POOL_V2                       = var.gacha_pool_v2
        SRO_TRADER_CARGO_DROP                   = var.trader_cargo_drop
        SRO_MALL_TRANSPORT_SPEED                = var.mall_transport_speed
        SRO_MALL_RIDE_SPEED                     = var.mall_ride_speed
        SRO_ANNOUNCE_SOX_DROP                   = var.announce_sox_drop
        SRO_SEAL_BOARD                          = var.seal_board
        SRO_SEAL_CENSUS                         = var.seal_census
        SRO_ANNOUNCE_PLUS                       = var.announce_plus
        SRO_ANNOUNCE_PLUS_TIERS                 = var.announce_plus_tiers
        SRO_ANNOUNCE_JOB_KILL                   = var.announce_job_kill
        SRO_JOB_KILL_NOTICE_REPEAT              = var.job_kill_notice_repeat
        SRO_JOB_KILL_NOTICES_PER_MINUTE         = var.job_kill_notices_per_minute
        SRO_MOST_WANTED_NOTICES_PER_MINUTE      = var.most_wanted_notices_per_minute
        SRO_JOB_KILL_REPEAT_MINUTES             = var.job_kill_repeat_minutes
        SRO_SOX_NOTICE_MIN_DEGREE               = var.sox_notice_min_degree
        SRO_SHORT_NOTICE_SHOW_MS                = var.short_notice_show_ms
        SRO_SHORT_NOTICE_FADE_MS                = var.short_notice_fade_ms
        SRO_CHAT_ACK_TIMEOUT_MS                 = var.chat_ack_timeout_ms
        SRO_ANNOUNCE_MOST_WANTED                = var.announce_most_wanted
        SRO_ALCHEMY_BREAK_NOTICE                = var.alchemy_break_notice
        SRO_THIEF_PARTY_BONUS                   = var.thief_party_bonus
        SRO_THIEF_PARTY_PERCENT_EACH            = var.thief_party_percent_each
        SRO_THIEF_PARTY_PERCENT_CAP             = var.thief_party_percent_cap
        SRO_TRADE_JOB_BONUS                     = var.trade_job_bonus
        SRO_TRADE_JOB_TRADER_PERCENT_PER_HUNTER = var.trade_job_trader_percent_per_hunter
        SRO_TRADE_JOB_TRADER_PERCENT_CAP        = var.trade_job_trader_percent_cap
        SRO_TRADE_JOB_HUNTER_PERCENT_PER_TRADER = var.trade_job_hunter_percent_per_trader
        SRO_TRADE_JOB_HUNTER_PERCENT_CAP        = var.trade_job_hunter_percent_cap
        SRO_RAISED_GOLD_CAPS                    = var.raised_gold_caps
        SRO_REVERSE_RETURN_MAP                  = var.reverse_return_map
        SRO_GM_SETSPEED                         = var.gm_setspeed
        SRO_GM_TRAINING_DUMMY                   = var.gm_training_dummy
        SRO_ALCHEMY_RATES                       = var.alchemy_rates
        SRO_ALCHEMY_MAX_PLUS                    = var.alchemy_max_plus
        SRO_TEAM_DEATH_REVIVE                   = var.team_death_revive
        SRO_FLAGWAR_START_UTC                   = var.flagwar_start_utc
        SRO_FLAGWAR_MIN_PLAYERS                 = var.flagwar_min_players
        SRO_ATTACK_PET_EXPANSION                = var.attack_pet_expansion
        SRO_BETA_GROWTH                         = var.beta_growth
        SRO_BETA_GROWTH_BANDS                   = var.beta_growth_bands
        SRO_WHISPER_JOB_ALIAS                   = var.whisper_job_alias
        SRO_BETA_SKILL_EXP_RATE                 = var.beta_skill_exp_rate
        SRO_BETA_DROP_RATE                      = var.beta_drop_rate
        SRO_BETA_GOLD_RATE                      = var.beta_gold_rate
        SRO_BETA_RARE_RATE                      = var.beta_rare_rate
        SRO_BETA_SEAL_RATES                     = var.beta_seal_rates
        SRO_SUN_EVENT_ONLY                      = var.sun_event_only
        SRO_SUN_DROP_RATE                       = var.sun_drop_rate
        SRO_UNIQUE_GAP_EXEMPT                   = var.unique_gap_exempt
        SRO_UNIQUE_POP_TICKET                   = var.unique_pop_ticket
        SRO_UNIQUE_MAX_LEVEL_LOOT               = var.unique_max_level_loot
        SRO_UNIQUE_SEAL_DROPS                   = var.unique_seal_drops
        SRO_BETA_UNIQUE_RESPAWN                 = var.beta_unique_respawn
        SRO_BETA_DROP_CAP                       = var.beta_drop_cap
        SRO_BETA_DROP_MIX                       = var.beta_drop_mix
        SRO_SHOP_EXTRA_GOODS                    = var.shop_extra_goods
        SRO_JANGAN_ALCHEMY_SHOP                 = var.jangan_alchemy_shop
        SRO_MALL_EXTRA_GOODS                    = var.mall_extra_goods
        SRO_ALCHEMY_NO_ASSIMILATION             = var.alchemy_no_assimilation
        SRO_CARAVAN_BANDIT_CAP                  = var.caravan_bandit_cap
        SRO_CARAVAN_STAR_AMBUSH                 = var.caravan_star_ambush
        SRO_JOB_MONSTER_SPEED                   = var.job_monster_speed
        SRO_TRANSPORT_FOLLOW                    = var.transport_follow
        SRO_TRANSPORT_FOLLOW_TETHER             = var.transport_follow_tether
        SRO_JOB_GOODS_TELEPORT                  = var.job_goods_teleport
        SRO_THIEF_CART_TOWN_AREAS               = var.thief_cart_town_areas
        SRO_ELIXIR_DROPS_OFF                    = var.elixir_drops_off
        SRO_ALCHEMY_MATERIAL_DROPS_OFF          = var.alchemy_material_drops_off
        SRO_BETA_SHOP_ONLY_DEGREE9              = var.beta_shop_only_degree9
        SRO_STACK_SIZES                         = var.stack_sizes
        SRO_QINSHI_TOMB                         = var.qinshi_tomb
        SRO_ALEXANDRIA                          = var.alexandria
        SRO_PUBLIC_API_ADDR                     = var.public_api_addr
        SRO_PUBLIC_API_TOKEN_PATH               = "${NOMAD_SECRETS_DIR}/public-api-token"
        SRO_STAFF_WEBHOOK_PATH                  = "${NOMAD_SECRETS_DIR}/staff-webhook"
        TRANSPORT_WT_ADDR                       = var.transport_wt_addr != "" ? var.transport_wt_addr : "${NOMAD_IP_transport}:${NOMAD_PORT_transport}"
        TRANSPORT_WS_ADDR                       = "${NOMAD_IP_transport}:${NOMAD_PORT_transport}"
        TRANSPORT_CERT_DIR                      = var.cert_dir
        TRANSPORT_CERT_FILE                     = var.transport_cert_file
        TRANSPORT_KEY_FILE                      = var.transport_key_file
        TRANSPORT_ALLOWED_ORIGINS               = var.allowed_origins
        SRO_TRANSPORT_TLS_ID                    = var.transport_tls_id
        SRO_WEBTRANSPORT_PUBLIC                 = var.webtransport_public
        TRANSPORT_PPROF                         = var.transport_pprof
        TRANSPORT_PPROF_ADDR                    = var.pprof_addr
        SRO_RELEASE_ID                          = var.release_id
        SRO_HUNTING_GUIDE                       = var.hunting_guide
      }

      template {
        data = <<EOH
{{ with nomadVar "nomad/jobs/sro-gameworld-__SHARD_ID__/gameworld/gameworld" }}
{{ .agent_session_public_keys }}
{{ end }}
EOH

        destination = "secrets/agent-session-public-keys.json"
        change_mode = "noop"
        perms       = "0600"
        uid         = var.task_uid
        gid         = var.task_gid
      }

      # The privacy-write token. index, not .public_api_token: a missing map
      # key must render empty, never the template's "<no value>". An empty
      # or short file leaves the write unserved. The GameWorld reads it once at boot, so a
      # rotated token applies at the next restart rather than kicking players.
      template {
        data = <<EOH
{{ with nomadVar "nomad/jobs/sro-gameworld-__SHARD_ID__/gameworld/gameworld" }}{{ index . "public_api_token" }}{{ end }}
EOH

        destination = "secrets/public-api-token"
        change_mode = "noop"
        perms       = "0600"
        uid         = var.task_uid
        gid         = var.task_gid
      }

      # The staff Discord webhook for ops-only alerts (the served
      # WebTransport pair, #1447). index, as above: absent renders empty,
      # and the GameWorld then only logs. It reads the file at every post,
      # so a rotated webhook applies without a restart.
      template {
        data = <<EOH
{{ with nomadVar "nomad/jobs/sro-gameworld-__SHARD_ID__/gameworld/gameworld" }}{{ index . "staff_discord_webhook" }}{{ end }}
EOH

        destination = "secrets/staff-webhook"
        change_mode = "noop"
        perms       = "0600"
        uid         = var.task_uid
        gid         = var.task_gid
      }

      identity {
        name        = "sro_agent"
        aud         = ["sro-agent"]
        file        = true
        ttl         = "5m"
        change_mode = "noop"
      }

      # Resolve Agent through Nomad native service discovery. An empty
      # snapshot renders no URL and remains watched. The application-level
      # SRO_AGENT_URL_REQUIRE gate then refuses startup without falling back
      # to loopback; a later registration change restarts the task.
      template {
        data = <<EOH
{{ range nomadService 1 (env "NOMAD_ALLOC_ID") "sro-agent" }}
SRO_AGENT_URL=http://{{ .Address }}:{{ .Port }}
{{ end }}
EOH

        destination = "local/agent.env"
        env         = true
        change_mode = "restart"
        perms       = "0600"
        uid         = var.task_uid
        gid         = var.task_gid
      }

      service {
        provider = "nomad"
        name     = "sro-gameworld-${var.shard_id}"
        port     = "transport"
        tags     = ["shard:${var.shard_id}"]

        check {
          name     = "liveness"
          type     = "http"
          path     = "/transport/healthz"
          interval = "10s"
          timeout  = "2s"

          check_restart {
            limit = 3
            grace = "30s"
          }
        }

        # Readiness is intentionally not a restart trigger. Dependency or
        # authority degradation removes the shard from service without
        # turning a shared outage into a fleet-wide restart storm.
        check {
          name      = "readiness"
          type      = "http"
          path      = "/transport/readyz"
          interval  = "5s"
          timeout   = "2s"
          on_update = "require_healthy"
        }
      }

      resources {
        cpu    = var.cpu
        memory = var.memory_mb
      }

      logs {
        max_files     = 10
        max_file_size = 100
      }

      shutdown_delay = "5s"
      kill_timeout   = "60s"
    }
  }
}
