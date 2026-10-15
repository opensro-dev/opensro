/*
===========================================================================

overrides.go - stored overrides over the environment, and the live swap

Port-only, not native. An approved change from the staff console is an
override: a key and the raw value an operator chose, kept by the store
(platform/settingsapi) and read back at boot after the environment. This
file validates one proposed value with the registry's own rules, layers a
set of overrides over the environment into a new snapshot, and installs a
live change: only Live keys are swapped in at once; a Restart key waits
for the next boot, so a value copied into reference rows or a persisted
meaning never changes under a running shard.

Writers are serialized (Apply holds one lock across read, build and
store), so two approvals arriving together can never install a snapshot
built from a stale base.

===========================================================================
*/
package tunables

import (
	"fmt"
	"os"
	"strings"
	"sync"
)

var (
	// writeMu serializes every writer of the installed snapshot.
	writeMu sync.Mutex
	// bootOverrides are the stored overrides the boot installed; FromEnv
	// applies them, so a restart-class value approved earlier is the one
	// the boot copies into its rows.
	bootOverrides map[string]string
)

/*
================
InstallBoot

The boot's snapshot: the environment with the stored overrides laid over
it. A stored value the registry refuses stops the boot, as an
environment value does.
================
*/
func InstallBoot(overrides map[string]string) error {
	s, err := WithOverrides(os.Getenv, overrides)
	if err != nil {
		return err
	}
	copied := make(map[string]string, len(overrides))
	for key, raw := range overrides {
		copied[key] = raw
	}
	writeMu.Lock()
	bootOverrides = copied
	current.Store(s)
	writeMu.Unlock()
	return nil
}

/*
================
bootOverride
================
*/
func bootOverride(key string) (string, bool) {
	writeMu.Lock()
	defer writeMu.Unlock()
	raw, ok := bootOverrides[key]
	return raw, ok
}

/*
================
Lookup

The registry row of key, and whether there is one.
================
*/
func Lookup(key string) (Setting, bool) {
	for _, s := range Registry {
		if s.Key == key {
			return s, true
		}
	}
	return Setting{}, false
}

/*
================
Validate

The value raw would give key, by the same rules the boot applies; the
refusal names the variable and the accepted values. An unknown key is
refused too: a proposal for it could never apply. ClearOverride gives
the environment's value.
================
*/
func Validate(key, raw string) (Value, error) {
	s, ok := Lookup(key)
	if !ok {
		return Value{}, fmt.Errorf("unknown setting %q", key)
	}
	if strings.TrimSpace(raw) == ClearOverride {
		return parseOne(s, strings.TrimSpace(os.Getenv(s.Env)))
	}
	return parseOne(s, strings.TrimSpace(raw))
}

/*
================
WithOverrides

A snapshot of getenv with overrides (key -> raw value) laid over it; an
override's Source is "store". Every refused override is reported, so a
stored value a newer binary tightened is seen, not silently dropped.
================
*/
func WithOverrides(getenv func(string) string, overrides map[string]string) (*Settings, error) {
	base, err := Parse(getenv)
	if err != nil {
		return nil, err
	}
	return layer(base, overrides, func(Setting) bool { return true })
}

/*
================
layer

base with the overrides whose row keep accepts.
================
*/
func layer(base *Settings, overrides map[string]string, keep func(Setting) bool) (*Settings, error) {
	values := make(map[string]Value, len(base.values))
	for key, v := range base.values {
		values[key] = v
	}
	var problems []string
	for key, raw := range overrides {
		s, ok := Lookup(key)
		if !ok {
			problems = append(problems, fmt.Sprintf("unknown setting %q", key))
			continue
		}
		if !keep(s) {
			continue
		}
		v, err := parseOne(s, strings.TrimSpace(raw))
		if err != nil {
			problems = append(problems, err.Error())
			continue
		}
		v.Source = "store"
		values[key] = v
	}
	if len(problems) > 0 {
		return nil, fmt.Errorf("stored settings: %s", strings.Join(problems, "; "))
	}
	return &Settings{values: values}, nil
}

// ClearOverride is the proposal value that removes a stored override, so
// the key reads its environment value (or its default) again.
const ClearOverride = "env"

/*
================
Apply

Installs a live change of key to raw over the current snapshot and
returns whether it applied now: a Restart key is not swapped (false, it
waits for the next boot's stored overrides). The value is validated first;
nothing changes on a refusal.
================
*/
func Apply(key, raw string) (applied bool, err error) {
	s, ok := Lookup(key)
	if !ok {
		return false, fmt.Errorf("unknown setting %q", key)
	}
	if strings.TrimSpace(raw) == ClearOverride {
		// Back to the environment's value, as a boot without the override.
		raw = os.Getenv(s.Env)
	}
	if _, err := parseOne(s, strings.TrimSpace(raw)); err != nil {
		return false, err
	}
	if s.Class != Live {
		return false, nil
	}
	writeMu.Lock()
	defer writeMu.Unlock()
	next, err := layer(Current(), map[string]string{key: raw}, func(Setting) bool { return true })
	if err != nil {
		return false, err
	}
	current.Store(next)
	return true, nil
}
