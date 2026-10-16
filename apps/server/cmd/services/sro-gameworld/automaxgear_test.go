/*
===========================================================================

automaxgear_test.go - the auto max-gear switch, its config and its picks

===========================================================================
*/
package main

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
)

/*
================
TestAutoMaxGearConfigFromEnv

Both settings: unset is native (off), and each value the operator can write
resolves. The environment is restored by t.Setenv.
================
*/
func TestAutoMaxGearConfigFromEnv(t *testing.T) {
	t.Setenv(EnvAutoMaxGear, "")
	t.Setenv(EnvAutoMaxGearSeal, "")
	t.Setenv(EnvAutoMaxGearPlus, "")
	if cfg := autoMaxGearConfigFromEnv(); cfg.enabled || cfg.tier != "C" || cfg.plus != 12 {
		t.Fatalf("unset: %+v, want off, C, 12 (native)", cfg)
	}
	t.Setenv(EnvAutoMaxGear, "off")
	if cfg := autoMaxGearConfigFromEnv(); cfg.enabled {
		t.Fatalf("off: %+v, want off", cfg)
	}
	for _, on := range []string{"on", "1", "true", "ON"} {
		t.Setenv(EnvAutoMaxGear, on)
		if cfg := autoMaxGearConfigFromEnv(); !cfg.enabled {
			t.Fatalf("%q: %+v, want on", on, cfg)
		}
	}
	t.Setenv(EnvAutoMaxGear, "on")
	t.Setenv(EnvAutoMaxGearSeal, "moon")
	t.Setenv(EnvAutoMaxGearPlus, "7")
	if cfg := autoMaxGearConfigFromEnv(); !cfg.enabled || cfg.tier != "B" || cfg.plus != 7 {
		t.Fatalf("moon +7: %+v, want on, B, 7", cfg)
	}
	t.Setenv(EnvAutoMaxGearSeal, "pluto")
	t.Setenv(EnvAutoMaxGearPlus, "99")
	if cfg := autoMaxGearConfigFromEnv(); cfg.tier != "C" || cfg.plus != 12 {
		t.Fatalf("bad values: %+v, want the defaults C, 12", cfg)
	}
}

func TestSealTierLetter(t *testing.T) {
	cases := map[string]string{"sun": "C", "moon": "B", "star": "A", "C": "C", "b": "B", "A": "A"}
	for in, want := range cases {
		got, ok := sealTierLetter(in)
		if !ok || got != want {
			t.Fatalf("sealTierLetter(%q) = %q,%v want %q,true", in, got, ok, want)
		}
	}
	if _, ok := sealTierLetter("pluto"); ok {
		t.Fatal("an unknown seal name must not resolve")
	}
}

/*
================
TestAutoMaxGearPools

A level frames the pools: no candidate above the level and every pool
ordered strongest first.
================
*/
func TestAutoMaxGearPools(t *testing.T) {
	worn, part, acc := autoMaxGearPools(90, "C")
	if len(worn) == 0 || len(part) == 0 || len(acc) == 0 {
		t.Fatalf("pools empty at 90: worn=%d part=%d acc=%d", len(worn), len(part), len(acc))
	}
	for key, pool := range worn {
		for i := 1; i < len(pool); i++ {
			if pool[i-1].level < pool[i].level {
				t.Fatalf("%s: pool is not strongest first: %+v", key, pool)
			}
			if pool[i].level > 90 {
				t.Fatalf("%s: candidate above the level: %+v", key, pool[i])
			}
		}
	}
	if low, _, _ := autoMaxGearPools(1, "C"); len(low) > len(worn) {
		t.Fatalf("level 1 admits more pieces than level 90: %d > %d", len(low), len(worn))
	}
}

func TestGearPieceKeyAndDegree(t *testing.T) {
	cases := map[string]string{
		"ITEM_CH_SWORD_04_C_RARE":       "ITEM_CH_SWORD_DD",
		"ITEM_CH_SWORD_04_A":            "ITEM_CH_SWORD_DD",
		"ITEM_CH_SWORD_04_A_RARE":       "ITEM_CH_SWORD_DD",
		"ITEM_CH_SWORD_01_B":            "ITEM_CH_SWORD_DD",
		"ITEM_CH_SPEAR_01_A_DEF":        "ITEM_CH_SPEAR_DD",
		"ITEM_CH_M_CLOTHES_01_BA_A_DEF": "ITEM_CH_M_CLOTHES_DD_BA",
	}
	for in, want := range cases {
		if got := gearPieceKey(in); got != want {
			t.Fatalf("gearPieceKey(%q) = %q, want %q", in, got, want)
		}
	}
	if d, ok := gearDegree("ITEM_CH_SWORD_04_C_RARE"); !ok || d != 4 {
		t.Fatalf("gearDegree = %d,%v, want 4,true", d, ok)
	}
	if !gearDowngrade("ITEM_CH_SWORD_04_C_RARE", "ITEM_CH_SWORD_03_C_RARE") {
		t.Fatal("expected a lower degree to be a downgrade")
	}
	if gearDowngrade("ITEM_CH_SWORD_03_C_RARE", "ITEM_CH_SWORD_04_C_RARE") {
		t.Fatal("a higher degree must not be a downgrade")
	}
}

func TestWornIdentityFromDefGear(t *testing.T) {
	c := &enterworld.Character{MissionInventory: []enterworld.InventoryRow{
		{Slot: 1, Codename: "ITEM_CH_M_CLOTHES_01_BA_A_DEF"},
		{Slot: 6, Codename: "ITEM_CH_SPEAR_01_A_DEF"},
	}}
	race, gender, class := wornIdentity(c)
	if race != "CH" || gender != "M" || class != "CLOTHES" {
		t.Fatalf("wornIdentity = %q,%q,%q, want CH,M,CLOTHES", race, gender, class)
	}
}

/*
================
TestApplyAutoMaxGearOffWritesNoFrame

The flag off path is native: the rule returns no frame and touches nothing.
================
*/
func TestApplyAutoMaxGearOffWritesNoFrame(t *testing.T) {
	t.Setenv(EnvAutoMaxGear, "")
	if frames := (&gameplayPlane{}).applyAutoMaxGear(&enterworld.Character{}); frames != nil {
		t.Fatalf("flag off must write no frame: %+v", frames)
	}
	t.Setenv(EnvAutoMaxGear, "off")
	if frames := (&gameplayPlane{}).applyAutoMaxGear(&enterworld.Character{}); frames != nil {
		t.Fatalf("flag off must write no frame: %+v", frames)
	}
}
