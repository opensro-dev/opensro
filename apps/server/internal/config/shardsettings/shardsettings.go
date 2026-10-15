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

===========================================================================
*/
package shardsettings

import "strings"

// Source says who fills a setting's job variable.
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
Setting

One shard setting. Catalogue marks the settings sro-catalogue reads (what
the site publishes depends on them); the rest are server only.
================
*/
type Setting struct {
	Env       string
	NomadVar  string
	Default   string
	Catalogue bool
	Source    Source
}

// Settings is the table, in the job file's order.
var Settings = []Setting{
	{Env: "SRO_BETA_STARTER_KIT", NomadVar: "beta_starter_kit", Default: "on"},
	{Env: "SRO_BETA_PLAYER_MAP", NomadVar: "beta_player_map", Default: "on"},
	{Env: "SRO_BETA_MASTERY", NomadVar: "beta_mastery", Default: "on", Catalogue: true, Source: Built},
	{Env: "SRO_BETA_SILK", NomadVar: "beta_silk", Default: "50"},
	{Env: "SRO_PARTY_MASTERIES", NomadVar: "party_masteries", Default: "on"},
	// Grab and growth pets at four fifths of their run speed (#320): never
	// deployed, so the deployment default stays native.
	{Env: "SRO_PET_PACING", NomadVar: "pet_pacing", Default: "off"},
	{Env: "SRO_STORAGE_AUTO_STACK", NomadVar: "storage_auto_stack", Default: "on"},
	// Owner, 2026-10-14: the Bandit Den scroll counts down half as long.
	{Env: "SRO_FAST_DEN_RETURN", NomadVar: "fast_den_return", Default: "on"},
	// Owner, 2026-10-15: a login inside a closed object (a tree trunk) is
	// rescued to the nearest open ground.
	{Env: "SRO_CLOSED_OBJECT_RESCUE", NomadVar: "closed_object_rescue", Default: "on"},
	// Owner, 2026-10-14: the job-kill streak, its bonuses and bounty.
	{Env: "SRO_MOST_WANTED", NomadVar: "most_wanted", Default: "on"},
	// Owner and the balance advisor, 2026-10-15: the bounty is the victim's gold.
	{Env: "SRO_MOST_WANTED_BOUNTY_TRANSFER", NomadVar: "most_wanted_bounty_transfer", Default: "on"},
	// Owner, 2026-10-14: trade stars by cargo gold, Jangan route profit.
	{Env: "SRO_TRADE_TIERS", NomadVar: "trade_tiers", Default: "on", Catalogue: true},
	// Owner, 2026-10-14: trader and hunter suits hide worn armour.
	{Env: "SRO_JOB_SUIT_HIDES_ARMOR", NomadVar: "job_suit_hides_armor", Default: "on"},
	// Owner, 2026-10-15: level-20 thief suits wear the Moonlit Ash look.
	{Env: "SRO_MOONLIT_THIEF_SUIT", NomadVar: "moonlit_thief_suit", Default: "on"},
	// Owner, 2026-10-14: no transport summon during the Bandit Den countdown.
	{Env: "SRO_DEN_NO_TRANSPORT_SUMMON", NomadVar: "den_no_transport_summon", Default: "on"},
	// Owner, 2026-10-14: a trader's goods drop on rider death and field logout.
	{Env: "SRO_TRADER_CARGO_DROP", NomadVar: "trader_cargo_drop", Default: "on"},
	// Owner, 2026-10-14: Item Mall transports move 1.5 times as fast.
	{Env: "SRO_MALL_TRANSPORT_SPEED", NomadVar: "mall_transport_speed", Default: "on"},
	// Owner, 2026-10-14: shard notices for seal drops, +8..+11 and job kills.
	{Env: "SRO_ANNOUNCE_SOX_DROP", NomadVar: "announce_sox_drop", Default: "on"},
	{Env: "SRO_ANNOUNCE_PLUS", NomadVar: "announce_plus", Default: "on"},
	{Env: "SRO_ANNOUNCE_JOB_KILL", NomadVar: "announce_job_kill", Default: "on"},
	// Owner, 2026-10-14: +10% party EXP and gold per thief (cap 50%).
	{Env: "SRO_THIEF_PARTY_BONUS", NomadVar: "thief_party_bonus", Default: "on"},
	// Owner, 2026-10-14: trader +10%/hunter (cap 20%), hunter +10%/trader (cap 60%).
	{Env: "SRO_TRADE_JOB_BONUS", NomadVar: "trade_job_bonus", Default: "on"},
	{Env: "SRO_REVERSE_RETURN_MAP", NomadVar: "reverse_return_map", Default: ""},
	{Env: "SRO_GM_SETSPEED", NomadVar: "gm_setspeed", Default: "on"},
	{Env: "SRO_ALCHEMY_RATES", NomadVar: "alchemy_rates", Default: "on", Catalogue: true},
	{Env: "SRO_ALCHEMY_MAX_PLUS", NomadVar: "alchemy_max_plus", Default: "on", Catalogue: true},
	{Env: "SRO_TEAM_DEATH_REVIVE", NomadVar: "team_death_revive", Default: "on"},
	{Env: "SRO_BETA_GROWTH", NomadVar: "beta_growth", Default: "on", Catalogue: true},
	{Env: "SRO_BETA_SKILL_EXP_RATE", NomadVar: "beta_skill_exp_rate", Default: "100", Catalogue: true},
	{Env: "SRO_BETA_DROP_RATE", NomadVar: "beta_drop_rate", Default: "20", Catalogue: true},
	{Env: "SRO_BETA_DROP_MIX", NomadVar: "beta_drop_mix", Default: "on", Catalogue: true},
	{Env: "SRO_SHOP_EXTRA_GOODS", NomadVar: "shop_extra_goods", Default: "on", Catalogue: true},
	{Env: "SRO_MALL_EXTRA_GOODS", NomadVar: "mall_extra_goods", Default: "on"},
	{Env: "SRO_ALCHEMY_NO_ASSIMILATION", NomadVar: "alchemy_no_assimilation", Default: "on"},
	// Owner, 2026-10-14: 8 per ambush (was 16), thieves and hunters alike.
	{Env: "SRO_CARAVAN_BANDIT_CAP", NomadVar: "caravan_bandit_cap", Default: "8"},
	// Owner, 2026-10-15: a fixed ambush per caravan star (2/3/4/7/10).
	{Env: "SRO_CARAVAN_STAR_AMBUSH", NomadVar: "caravan_star_ambush", Default: "on"},
	// Owner, 2026-10-15: thief and hunter monsters move 20% faster.
	{Env: "SRO_JOB_MONSTER_SPEED", NomadVar: "job_monster_speed", Default: "on"},
	// Owner, 2026-10-15: a dismounted trade transport follows its trader.
	{Env: "SRO_TRANSPORT_FOLLOW", NomadVar: "transport_follow", Default: "on"},
	// Owner, 2026-10-15: town teleport with a trade transport out (testing).
	{Env: "SRO_JOB_GOODS_TELEPORT", NomadVar: "job_goods_teleport", Default: "on"},
	{Env: "SRO_ELIXIR_DROPS_OFF", NomadVar: "elixir_drops_off", Default: "on", Catalogue: true},
	// Owner, 2026-10-14: alchemy materials no longer drop; "off" is native.
	{Env: "SRO_ALCHEMY_MATERIAL_DROPS_OFF", NomadVar: "alchemy_material_drops_off", Default: "on", Catalogue: true},
	{Env: "SRO_BETA_DROP_CAP", NomadVar: "beta_drop_cap", Default: "16", Catalogue: true},
	{Env: "SRO_BETA_GOLD_RATE", NomadVar: "beta_gold_rate", Default: "250", Catalogue: true},
	{Env: "SRO_INSTANT_INVENTORY_EXPANSION", NomadVar: "instant_inventory_expansion", Default: "on"},
	{Env: "SRO_BETA_RARE_RATE", NomadVar: "beta_rare_rate", Default: "5", Catalogue: true},
	{Env: "SRO_BETA_SHOP_ONLY_DEGREE9", NomadVar: "beta_shop_only_degree9", Default: "on", Catalogue: true},
	{Env: "SRO_BETA_SEAL_RATES", NomadVar: "beta_seal_rates", Default: "on", Catalogue: true},
	// Owner, 2026-10-14: degree-9 Sun gear only from events (no drop, no shop).
	{Env: "SRO_SUN_EVENT_ONLY", NomadVar: "sun_event_only", Default: "on", Catalogue: true},
	// Owner, 2026-10-13 (F7): Lucky Powder stacks to 500; 2026-10-14:
	// arrows and bolts stack to 2500. "off" is native.
	{Env: "SRO_STACK_SIZES", NomadVar: "stack_sizes", Default: "arrow=2500,bolt=2500,luckypowder=500", Source: Built},
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
