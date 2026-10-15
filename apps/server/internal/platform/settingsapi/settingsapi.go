/*
===========================================================================

settingsapi.go - the staff console's read of the shard's settings

Port-only, not native. GET /internal/operations/settings/v1 answers the
staff console on the same host (operatorauth, the shard's operator-token)
with every tunable (game/tunables: value, default, bounds, class,
source) and every shard setting (config/shardsettings, the table a
deployment chooses from), read-only. The flags are an allow-list: a
variable outside that table (a webhook, a URL, identity, a path) is never
listed, so a new secret cannot leak by being added to the environment.

Writes (proposals, two-person approval, the audit) come later on the same
prefix; until then this handler answers GET only.

===========================================================================
*/
package settingsapi

import (
	"encoding/json"
	"net/http"
	"os"
	"sort"
	"strings"

	"opensro.online/server/internal/config/shardsettings"
	"opensro.online/server/internal/game/tunables"
	"opensro.online/server/internal/platform/operatorauth"
)

// Path is the handler's route on the operator listener.
const Path = "/internal/operations/settings/v1"

// schemaVersion is the response's shape; the console checks it.
const schemaVersion = 1

/*
================
SettingRow

One tunable as the console shows it.
================
*/
type SettingRow struct {
	Key     string    `json:"key"`
	Env     string    `json:"env"`
	Group   string    `json:"group"`
	Label   string    `json:"label"`
	Help    string    `json:"help"`
	Kind    string    `json:"kind"`
	Unit    string    `json:"unit"`
	Value   float64   `json:"value"`
	Enabled bool      `json:"enabled"`
	Default float64   `json:"default"`
	Native  *string   `json:"native"`
	Min     float64   `json:"min"`
	Max     float64   `json:"max"`
	Allowed []float64 `json:"allowed"`
	Step    float64   `json:"step"`
	Class   string    `json:"class"`
	Source  string    `json:"source"`
}

/*
================
FlagRow

One SRO_ variable: its raw value and, when it reads as a switch, whether
it is on (nil otherwise).
================
*/
type FlagRow struct {
	Env   string `json:"env"`
	Value string `json:"value"`
	On    *bool  `json:"on"`
}

/*
================
Response
================
*/
type Response struct {
	SchemaVersion int          `json:"schemaVersion"`
	Settings      []SettingRow `json:"settings"`
	Flags         []FlagRow    `json:"flags"`
}

/*
================
Handler

The GET handler under the guard of tokenPath. No token file: the endpoint
answers nobody, as the other operator readers do.
================
*/
func Handler(tokenPath string, getenv func(string) string) (http.Handler, error) {
	guard, found, err := operatorauth.Load(tokenPath, "settings")
	if err != nil {
		return nil, err
	}
	if !found {
		return http.NotFoundHandler(), nil
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !guard.Allows(r) {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(Build(tunables.Current(), getenv))
	}), nil
}

/*
================
Build

The response for one snapshot and one environment.
================
*/
func Build(s *tunables.Settings, getenv func(string) string) Response {
	out := Response{SchemaVersion: schemaVersion, Settings: []SettingRow{}, Flags: []FlagRow{}}
	tunableEnv := map[string]bool{}
	for _, setting := range tunables.Registry {
		tunableEnv[setting.Env] = true
		v := s.Value(setting.Key)
		row := SettingRow{Key: setting.Key, Env: setting.Env, Group: setting.Group, Label: setting.Label,
			Help: setting.Help, Kind: kindName(setting.Kind), Unit: setting.Unit, Value: v.Number,
			Enabled: v.Enabled, Default: setting.Default, Min: setting.Min, Max: setting.Max,
			Allowed: setting.Allowed, Step: setting.Step, Class: className(setting.Class), Source: v.Source}
		if setting.Native != "" {
			native := setting.Native
			row.Native = &native
		}
		out.Settings = append(out.Settings, row)
	}
	for _, setting := range shardsettings.Settings {
		if tunableEnv[setting.Env] {
			continue
		}
		value := getenv(setting.Env)
		out.Flags = append(out.Flags, FlagRow{Env: setting.Env, Value: value, On: switchValue(value)})
	}
	sort.Slice(out.Flags, func(i, j int) bool { return out.Flags[i].Env < out.Flags[j].Env })
	return out
}

/*
================
switchValue
================
*/
func switchValue(value string) *bool {
	var on bool
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "on", "1", "true":
		on = true
	case "off", "0", "false", "":
		on = false
	default:
		return nil
	}
	return &on
}

/*
================
kindName
================
*/
func kindName(kind tunables.Kind) string {
	if kind == tunables.Factor {
		return "factor"
	}
	return "int"
}

/*
================
className
================
*/
func className(class tunables.Class) string {
	if class == tunables.Live {
		return "live"
	}
	return "restart"
}

// Getenv is the process environment, for the GameWorld's wiring.
var Getenv = os.Getenv
