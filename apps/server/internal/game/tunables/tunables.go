/*
===========================================================================

tunables.go - the GameWorld's tunable values and their one snapshot

Port-only, not native. A tunable is a number behind a port-only rule that
an operator may set without a code release: its environment variable, its
kind, the value the code used before it became tunable (the default, so an
unset variable changes nothing), its bounds and its class. Registry is the
one table; Parse reads it once from the environment at boot and refuses a
value it cannot accept (out of bounds, not a number) instead of clamping.

The result is an immutable Settings behind an atomic pointer. An action
reads Current() once at its start and keeps that snapshot to its end, so it
never sees two values of one setting. Restart-class values are applied at
boot (they are copied into reference rows or persisted meanings); Live
values are read per action, and a later live store may replace the
snapshot without a restart.

===========================================================================
*/
package tunables

import (
	"errors"
	"fmt"
	"math"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

// Kind is how a tunable's environment value is read.
type Kind uint8

const (
	// Int is a whole number; unset means Default.
	Int Kind = iota
	// Factor is a switch with a value: unset, "off", "0" or "false" is
	// native (disabled), "on", "1" or "true" is Default, a decimal is the
	// factor itself.
	Factor
)

// Class says when a change can take effect.
type Class uint8

const (
	// Restart values are applied at boot only.
	Restart Class = iota
	// Live values are read per action from the snapshot.
	Live
	// Entry values are sent to a client at world entry from the snapshot:
	// a change reaches each player at their next world entry.
	Entry
)

// The registry's keys, as code reads them.
const (
	MallRideSpeed              = "mall_ride_speed"
	MostWantedTestKills        = "most_wanted_test_kills"
	MostWantedTestBounty       = "most_wanted_test_bounty"
	JobKillNoticesPerMinute    = "job_kill_notices_per_minute"
	MostWantedNoticesPerMinute = "most_wanted_notices_per_minute"
	JobKillRepeatMinutes       = "job_kill_repeat_minutes"
	ShortNoticeShowMs          = "short_notice_show_ms"
	ShortNoticeFadeMs          = "short_notice_fade_ms"
	ChatAckTimeoutMs           = "chat_ack_timeout_ms"
	// The trade group (SRO_TRADE_JOB_BONUS, SRO_THIEF_PARTY_BONUS and
	// SRO_TRADE_TIERS' Jangan routes; game/action).
	TradeJobTraderPercentPerHunter  = "trade_job_trader_percent_per_hunter"
	TradeJobTraderPercentCap        = "trade_job_trader_percent_cap"
	TradeJobHunterPercentPerTrader  = "trade_job_hunter_percent_per_trader"
	TradeJobHunterPercentCap        = "trade_job_hunter_percent_cap"
	ThiefPartyPercentEach           = "thief_party_percent_each"
	ThiefPartyPercentCap            = "thief_party_percent_cap"
	TradeRouteDonwhangPercent       = "trade_route_donwhang_percent"
	TradeRouteHotanPercent          = "trade_route_hotan_percent"
	TradeRouteSamarkandPercent      = "trade_route_samarkand_percent"
	TradeRouteConstantinoplePercent = "trade_route_constantinople_percent"
)

/*
================
Setting

One tunable. Allowed, when set, is the exact list of accepted values
(within Min..Max). Native says what the shard does with the value off or
unset when that differs from Default. Group, Label, Unit and Step are the
staff console's presentation.
================
*/
type Setting struct {
	Key      string
	Env      string
	Kind     Kind
	Default  float64
	Min, Max float64
	Allowed  []float64
	Step     float64
	Class    Class
	Group    string
	Label    string
	Unit     string
	Native   string
	Help     string
}

// Registry is the table, one row per tunable.
var Registry = []Setting{
	{Key: MallRideSpeed, Env: "SRO_MALL_RIDE_SPEED", Kind: Factor, Default: 1.3, Min: 1, Max: 3, Step: 0.05,
		Class: Restart, Group: "Movement", Label: "Mall vehicle and pet speed", Unit: "factor",
		Native: "off: mall vehicles and pets move at their listed speed",
		Help:   "Item Mall vehicle and pet move speed factor; copied into the reference rows at boot."},
	{Key: MostWantedTestKills, Env: "SRO_MOST_WANTED_TEST_KILLS", Kind: Int, Default: 5, Min: 1, Max: 10,
		Allowed: []float64{1, 2, 5, 10}, Step: 1, Class: Restart, Group: "Most Wanted",
		Label: "Test pacing: kills per level", Unit: "count",
		Help: "Counted job kills per wanted level under SRO_MOST_WANTED_TEST; divides 10, the ordinary rule's kills per level, because a saved streak keeps its count."},
	{Key: MostWantedTestBounty, Env: "SRO_MOST_WANTED_TEST_BOUNTY", Kind: Int, Default: 1_000_000, Min: 0,
		Max: 100_000_000, Step: 1, Class: Live, Group: "Most Wanted", Label: "Test pacing: bounty per level",
		Unit: "gold", Help: "Gold a wanted level is worth under SRO_MOST_WANTED_TEST; read at each payout."},
	{Key: JobKillNoticesPerMinute, Env: "SRO_JOB_KILL_NOTICES_PER_MINUTE", Kind: Int, Default: 6, Min: 1, Max: 30,
		Step: 1, Class: Live, Group: "Notices", Label: "Job-kill notices per minute", Unit: "count",
		Help: "Job-kill notices a division may post a minute."},
	{Key: MostWantedNoticesPerMinute, Env: "SRO_MOST_WANTED_NOTICES_PER_MINUTE", Kind: Int, Default: 6, Min: 1,
		Max: 30, Step: 1, Class: Live, Group: "Notices", Label: "Most Wanted notices per minute", Unit: "count",
		Help: "Most Wanted level and bounty notices a division may post a minute."},
	{Key: JobKillRepeatMinutes, Env: "SRO_JOB_KILL_REPEAT_MINUTES", Kind: Int, Default: 10, Min: 1, Max: 60,
		Step: 1, Class: Live, Group: "Notices", Label: "Job-kill pair quiet time", Unit: "minutes",
		Help: "How long one killer/victim pair's job-kill notice stays quiet (off with SRO_JOB_KILL_NOTICE_REPEAT)."},
	// The client tuning the world-entry bootstrap carries (enterworld
	// UiTuning; the client's ui-tuning.ts holds the same bounds). A fade is
	// at most 2000 and a banner at least 3000, so a fade never outlasts it.
	{Key: ShortNoticeShowMs, Env: "SRO_SHORT_NOTICE_SHOW_MS", Kind: Int, Default: 5000, Min: 3000, Max: 7000,
		Step: 500, Class: Entry, Group: "Client", Label: "Short notices: time shown", Unit: "ms",
		Help: "How long a notice banner shows with the Experimental \"Short notices\" row on (its default). A player who turns the row off sees the original 7 seconds."},
	{Key: ShortNoticeFadeMs, Env: "SRO_SHORT_NOTICE_FADE_MS", Kind: Int, Default: 1000, Min: 500, Max: 2000,
		Step: 100, Class: Entry, Group: "Client", Label: "Short notices: fade", Unit: "ms",
		Help: "How much of a short notice's time is its fade-out."},
	{Key: ChatAckTimeoutMs, Env: "SRO_CHAT_ACK_TIMEOUT_MS", Kind: Int, Default: 10000, Min: 5000, Max: 30000,
		Step: 1000, Class: Entry, Group: "Client", Label: "Chat confirmation wait", Unit: "ms",
		Help: "How long the client waits for the server to confirm a chat line before saying it may not have arrived (the original has no such wait)."},
	// The trade group: the port-only party bonuses and route profit, read
	// once per sale or per reward (game/action). Each default is the value
	// the rule shipped with, so a shard that sets nothing pays as before.
	{Key: TradeJobTraderPercentPerHunter, Env: "SRO_TRADE_JOB_TRADER_PERCENT_PER_HUNTER", Kind: Int, Default: 10,
		Min: 0, Max: 50, Step: 5, Class: Live, Group: "Trade", Label: "Trader bonus per hunter", Unit: "percent",
		Help: "Extra profit a trader earns per party hunter nearby under SRO_TRADE_JOB_BONUS."},
	{Key: TradeJobTraderPercentCap, Env: "SRO_TRADE_JOB_TRADER_PERCENT_CAP", Kind: Int, Default: 20, Min: 0, Max: 100,
		Step: 5, Class: Live, Group: "Trade", Label: "Trader bonus cap", Unit: "percent",
		Help: "The most a trader's profit rises under SRO_TRADE_JOB_BONUS."},
	{Key: TradeJobHunterPercentPerTrader, Env: "SRO_TRADE_JOB_HUNTER_PERCENT_PER_TRADER", Kind: Int, Default: 10,
		Min: 0, Max: 50, Step: 5, Class: Live, Group: "Trade", Label: "Hunter bonus per trader", Unit: "percent",
		Help: "Extra gold each hunter earns per party trader under SRO_TRADE_JOB_BONUS."},
	{Key: TradeJobHunterPercentCap, Env: "SRO_TRADE_JOB_HUNTER_PERCENT_CAP", Kind: Int, Default: 60, Min: 0, Max: 200,
		Step: 5, Class: Live, Group: "Trade", Label: "Hunter bonus cap", Unit: "percent",
		Help: "The most a hunter's gold share rises under SRO_TRADE_JOB_BONUS."},
	{Key: ThiefPartyPercentEach, Env: "SRO_THIEF_PARTY_PERCENT_EACH", Kind: Int, Default: 10, Min: 0, Max: 50,
		Step: 5, Class: Live, Group: "Trade", Label: "Thief party bonus per thief", Unit: "percent",
		Help: "Extra monster EXP and gold a party earns per member dressed as a thief under SRO_THIEF_PARTY_BONUS."},
	{Key: ThiefPartyPercentCap, Env: "SRO_THIEF_PARTY_PERCENT_CAP", Kind: Int, Default: 50, Min: 0, Max: 200,
		Step: 5, Class: Live, Group: "Trade", Label: "Thief party bonus cap", Unit: "percent",
		Help: "The most a party's monster EXP and gold rise under SRO_THIEF_PARTY_BONUS."},
	{Key: TradeRouteDonwhangPercent, Env: "SRO_TRADE_ROUTE_DONWHANG_PERCENT", Kind: Int, Default: 50, Min: 0,
		Max: 500, Step: 10, Class: Live, Group: "Trade", Label: "Jangan-Donwhang profit bonus", Unit: "percent",
		Help: "Extra profit on goods sold between Jangan and Donwhang, both ways, under SRO_TRADE_TIERS."},
	{Key: TradeRouteHotanPercent, Env: "SRO_TRADE_ROUTE_HOTAN_PERCENT", Kind: Int, Default: 100, Min: 0, Max: 500,
		Step: 10, Class: Live, Group: "Trade", Label: "Jangan-Hotan profit bonus", Unit: "percent",
		Help: "Extra profit on goods sold between Jangan and Hotan, both ways, under SRO_TRADE_TIERS."},
	{Key: TradeRouteSamarkandPercent, Env: "SRO_TRADE_ROUTE_SAMARKAND_PERCENT", Kind: Int, Default: 200, Min: 0,
		Max: 500, Step: 10, Class: Live, Group: "Trade", Label: "Jangan-Samarkand profit bonus", Unit: "percent",
		Help: "Extra profit on goods sold between Jangan and Samarkand, both ways, under SRO_TRADE_TIERS."},
	{Key: TradeRouteConstantinoplePercent, Env: "SRO_TRADE_ROUTE_CONSTANTINOPLE_PERCENT", Kind: Int, Default: 300,
		Min: 0, Max: 500, Step: 10, Class: Live, Group: "Trade", Label: "Jangan-Constantinople profit bonus",
		Unit: "percent",
		Help: "Extra profit on goods sold between Jangan and Constantinople, both ways, under SRO_TRADE_TIERS."},
}

/*
================
Value

One tunable's resolved value. Enabled is false only for a Factor switched
off; Source is "default" or "env".
================
*/
type Value struct {
	Number  float64
	Enabled bool
	Source  string
}

/*
================
Settings

The immutable snapshot. Never modified after Parse returns it.
================
*/
type Settings struct {
	values map[string]Value
}

/*
================
Parse

Reads every registry row from getenv. Every refused value is reported,
not only the first, so one boot shows the operator the whole list.
================
*/
func Parse(getenv func(string) string) (*Settings, error) {
	values := make(map[string]Value, len(Registry))
	var problems []error
	for _, s := range Registry {
		v, err := parseOne(s, strings.TrimSpace(getenv(s.Env)))
		if err != nil {
			problems = append(problems, err)
			continue
		}
		values[s.Key] = v
	}
	if len(problems) > 0 {
		return nil, errors.Join(problems...)
	}
	return &Settings{values: values}, nil
}

/*
================
parseOne
================
*/
func parseOne(s Setting, raw string) (Value, error) {
	if s.Kind == Factor {
		switch strings.ToLower(raw) {
		case "", "off", "0", "false":
			return Value{Number: 1, Enabled: false, Source: source(raw)}, nil
		case "on", "1", "true":
			return Value{Number: s.Default, Enabled: true, Source: "env"}, nil
		}
	}
	if raw == "" {
		return Value{Number: s.Default, Enabled: true, Source: "default"}, nil
	}
	n, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
		return Value{}, fmt.Errorf("%s=%q: want %s", s.Env, raw, s.want())
	}
	if s.Kind == Int && n != math.Trunc(n) {
		return Value{}, fmt.Errorf("%s=%q: want a whole number, %s", s.Env, raw, s.want())
	}
	if n < s.Min || n > s.Max || (len(s.Allowed) > 0 && !slices.Contains(s.Allowed, n)) {
		return Value{}, fmt.Errorf("%s=%q: want %s", s.Env, raw, s.want())
	}
	return Value{Number: n, Enabled: true, Source: "env"}, nil
}

/*
================
source
================
*/
func source(raw string) string {
	if raw == "" {
		return "default"
	}
	return "env"
}

/*
================
Setting.want

The accepted values, for a refusal message.
================
*/
func (s Setting) want() string {
	if len(s.Allowed) > 0 {
		parts := make([]string, len(s.Allowed))
		for i, a := range s.Allowed {
			parts[i] = strconv.FormatFloat(a, 'f', -1, 64)
		}
		return "one of " + strings.Join(parts, ", ")
	}
	bounds := strconv.FormatFloat(s.Min, 'f', -1, 64) + ".." + strconv.FormatFloat(s.Max, 'f', -1, 64)
	if s.Kind == Factor {
		return "off, on (" + strconv.FormatFloat(s.Default, 'f', -1, 64) + ") or a factor " + bounds
	}
	return bounds
}

/*
================
Settings.Value

The value of key. An unknown key is a programming error.
================
*/
func (s *Settings) Value(key string) Value {
	v, ok := s.values[key]
	if !ok {
		panic("tunables: no setting " + key)
	}
	return v
}

/*
================
Settings.Int
================
*/
func (s *Settings) Int(key string) int64 {
	return int64(s.Value(key).Number)
}

/*
================
Settings.Factor

A Factor's value and whether it is switched on.
================
*/
func (s *Settings) Factor(key string) (float64, bool) {
	v := s.Value(key)
	return v.Number, v.Enabled
}

/*
================
FromEnv

One tunable read from the process environment now, or the stored override
the boot installed (InstallBoot). Restart-class values are applied at boot
through it (the reference rows they scale exist only then); everything
read per action uses Current() instead.
================
*/
func FromEnv(key string) (Value, error) {
	for _, s := range Registry {
		if s.Key != key {
			continue
		}
		if raw, stored := bootOverride(key); stored {
			v, err := parseOne(s, strings.TrimSpace(raw))
			v.Source = "store"
			return v, err
		}
		return parseOne(s, strings.TrimSpace(os.Getenv(s.Env)))
	}
	panic("tunables: no setting " + key)
}

//============================================================================

var (
	current  atomic.Pointer[Settings]
	loadOnce sync.Once
	loadErr  error
)

/*
================
Load

Parses the process environment once and installs the snapshot. The
GameWorld calls it at boot so a refused value stops the boot; a later
call returns the first result.
================
*/
func Load() error {
	loadOnce.Do(func() {
		s, err := Parse(os.Getenv)
		if err != nil {
			loadErr = err
			return
		}
		current.CompareAndSwap(nil, s)
	})
	return loadErr
}

/*
================
Current

The installed snapshot. Before Load (a tool or a test that never booted
the GameWorld) it loads the environment; a value the environment refuses
there falls back to the registry defaults, since only the GameWorld's
boot decides to refuse.
================
*/
func Current() *Settings {
	if s := current.Load(); s != nil {
		return s
	}
	if Load() != nil {
		defaults, _ := Parse(func(string) string { return "" })
		current.CompareAndSwap(nil, defaults)
	}
	return current.Load()
}

/*
================
Swap

Installs s and returns a function that restores the previous snapshot.
The boot's stored overrides and tests use it; live changes go through
Apply. Both swap and restore hold the writers' lock (overrides.go), so a
restore can never interleave with another writer.
================
*/
func Swap(s *Settings) (restore func()) {
	Current()
	writeMu.Lock()
	previous := current.Swap(s)
	writeMu.Unlock()
	return func() {
		writeMu.Lock()
		current.Store(previous)
		writeMu.Unlock()
	}
}
