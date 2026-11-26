/*
===========================================================================

config_test.go - the caravan hire fee is a shard setting

SRO_CARAVAN_HIRE_FEE names the gold one hire costs (owner, 2026-11-26:
default 1,500,000). These pin what the setting accepts, what it refuses,
that its bound is a purse's, and that a configured fee is the one a hire
charges.

===========================================================================
*/
package caravanai

import (
	"strings"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/item/wire"
)

/*
==================
TestParseHireFee
==================
*/
func TestParseHireFee(t *testing.T) {
	for raw, want := range map[string]int64{"15000": 15_000, " 1500000 ": 1_500_000, "1": 1, "100000000": MaxHireFeeGold} {
		if fee, ok := ParseHireFee(raw); !ok || fee != want {
			t.Fatalf("ParseHireFee(%q) = %d, %v; want %d", raw, fee, ok, want)
		}
	}
	for _, raw := range []string{"", "0", "-5", "100000001", "1.5e6", "1,500,000", "off", "lots"} {
		if fee, ok := ParseHireFee(raw); ok {
			t.Fatalf("ParseHireFee(%q) accepted %d", raw, fee)
		}
	}
}

/*
==================
TestHireFeeBoundIsAPurse

No fee can exceed what one purse holds.
==================
*/
func TestHireFeeBoundIsAPurse(t *testing.T) {
	if MaxHireFeeGold != int64(wire.MaxGold) {
		t.Fatalf("MaxHireFeeGold = %d, want wire.MaxGold %d", MaxHireFeeGold, wire.MaxGold)
	}
}

/*
==================
TestConfigFromEnvHireFee

Unset is the default; a valid value is taken; a refused one keeps the
default and is reported.
==================
*/
func TestConfigFromEnvHireFee(t *testing.T) {
	t.Setenv(EnvHireFee, "")
	if cfg := ConfigFromEnv(); cfg.HireFeeGold != DefaultHireFeeGold || cfg.RefusedHireFee != "" {
		t.Fatalf("unset: fee %d refused %q, want the default %d", cfg.HireFeeGold, cfg.RefusedHireFee, DefaultHireFeeGold)
	}
	t.Setenv(EnvHireFee, "15000")
	if cfg := ConfigFromEnv(); cfg.HireFeeGold != 15_000 || cfg.RefusedHireFee != "" {
		t.Fatalf("15000: fee %d refused %q", cfg.HireFeeGold, cfg.RefusedHireFee)
	}
	t.Setenv(EnvHireFee, "free")
	if cfg := ConfigFromEnv(); cfg.HireFeeGold != DefaultHireFeeGold || cfg.RefusedHireFee != "free" {
		t.Fatalf("free: fee %d refused %q, want the default and the value reported", cfg.HireFeeGold, cfg.RefusedHireFee)
	}
}

/*
==================
TestConfiguredHireFeeIsCharged

A runtime configured with a fee charges exactly that fee for a hire and
quotes it in the hire's refusal; one configured with none charges the
default.
==================
*/
func TestConfiguredHireFeeIsCharged(t *testing.T) {
	for _, configured := range []int64{0, 15_000} {
		want := configured
		if want == 0 {
			want = DefaultHireFeeGold
		}
		var asked int64
		affordable := false
		rt := NewRuntime(Config{Enabled: true, HireFeeGold: configured}, nil, nil, WorldHooks{
			Player: func(_, name string) (PlayerPosition, bool) {
				return PlayerPosition{ObservedPlayer: ObservedPlayer{Name: name, JobType: domain.JobTrader, HPPercent: 100}, At: TownWaypoint(TownJangan)}, true
			},
			OnHireFee: func(_, _ string, fee int64) bool {
				asked = fee
				return affordable
			},
		})
		t.Cleanup(func() { _ = rt.Close() })
		rt.EnsureDivisionRoster("division")
		rt.manualRuns = true
		if got := rt.HireFee(); got != want {
			t.Fatalf("configured %d: HireFee %d, want %d", configured, got, want)
		}
		reply := rt.HireAgent("division", "MerchantLin", domain.JobTrader, "[AI]Hunter_Kwan", 1_000)
		if asked != want || !strings.Contains(reply, GoldText(want)) {
			t.Fatalf("configured %d: charged %d, reply %q; want %d quoted", configured, asked, reply, want)
		}
		affordable = true
		if reply := rt.HireAgent("division", "MerchantLin", domain.JobTrader, "[AI]Hunter_Kwan", 2_000); asked != want || !strings.Contains(reply, "joins you") {
			t.Fatalf("configured %d: the paid hire charged %d, reply %q", configured, asked, reply)
		}
	}
}

/*
==================
TestCaravansFlagOnAndOff

Unset and "off" leave the agents off (the native world); "on" runs them.
Main's slice-1 test, kept across the fork's rewrite.
==================
*/
func TestCaravansFlagOnAndOff(t *testing.T) {
	for raw, want := range map[string]bool{"": false, "off": false, "on": true} {
		t.Setenv(EnvAICaravans, raw)
		if cfg := ConfigFromEnv(); cfg.Enabled != want {
			t.Fatalf("%s=%q: Enabled %v, want %v", EnvAICaravans, raw, cfg.Enabled, want)
		}
	}
}
