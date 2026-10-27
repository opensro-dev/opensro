/*
===========================================================================

unique_loot_probe_test.go - offline unique-loot measurement with deployed rules

Opt-in measurement through the production kill planner, including the unique
prepass, assigned rewards, ordinary rolls, drop cap and final seal rewards.
Reads only licensed game data and a supplied settings snapshot. Never opens
the live authority, starts a network session or spawns anything in the shard.

===========================================================================
*/
package action

import (
	"encoding/json"
	"math/rand/v2"
	"os"
	"strconv"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/loot"
	"opensro.online/server/internal/game/progression"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/testsupport/gamedatatest"
	"opensro.online/server/internal/testsupport/tier"
)

const (
	uniqueProbeKills     = 10000
	uniqueProbeBatchSize = 100
	uniqueProbeSeed      = 20261025
	uniqueProbeLevel     = 90
)

/*
================
uniqueLootMeasurement

Counts actual ground-item plans, not notices. Per-tier counts include any
extra seals from ordinary rare rolls; under the ratified shapes every Sun
comes from those ordinary rolls, as no Sun is guaranteed. Batch histograms
show how many Sun items a group of 100 kills generated before any
announcement suppression.
================
*/
type uniqueLootMeasurement struct {
	Monster            string         `json:"monster"`
	MonsterLevel       uint8          `json:"monsterLevel"`
	Rarity             uint8          `json:"rarity"`
	Kills              int            `json:"kills"`
	Seed               uint64         `json:"seed"`
	Items              int            `json:"items"`
	Seals              [3]int         `json:"sealsStarMoonSun"`
	KillsWithSeal      [3]int         `json:"killsWithStarMoonSun"`
	SealDegrees        map[int]int    `json:"sealDegrees"`
	NormalClasses      map[int]int    `json:"normalEquipmentClasses"`
	SunPerHundredKills map[string]int `json:"sunPer100KillsHistogram"`
}

/*
================
TestUniqueLootDistributionProbe

Run explicitly with SRO_UNIQUE_LOOT_PROBE=1 and SRO_UNIQUE_LOOT_SETTINGS
naming a JSON object of the deployed SRO_* loot settings. A deterministic
PCG supplies the production RNG's inclusive 0..32767 domain. This measures
the loot planner, not combat, persistence, pickup or the notice UI.
================
*/
func TestUniqueLootDistributionProbe(t *testing.T) {
	if os.Getenv("SRO_UNIQUE_LOOT_PROBE") != "1" {
		t.Skip("set SRO_UNIQUE_LOOT_PROBE=1 to measure unique loot")
	}
	tier.Long(t, "measure unique rewards across 70,000 full kill plans")
	settings := uniqueProbeSettings(t)
	dir := gamedatatest.TextdataDir(t)
	items := shippedItems(t)
	refs := monster.LoadMonsterRefs(dir)
	codes := []string{
		"MOB_CH_TIGERWOMAN", "MOB_EU_KERBEROS", "MOB_AM_IVY",
		"MOB_OA_URUCHI", "MOB_KK_ISYUTARU", "MOB_TK_BONELORD", "MOB_RM_TAHOMET",
	}
	var results []uniqueLootMeasurement
	for _, code := range codes {
		t.Run(code, func(t *testing.T) {
			rt, _, player, target := newCombatTestRuntimeAtLevel(t, 1, 1)
			found := false
			for _, ref := range refs {
				if ref.Codename == code {
					target.Ref, found = ref, true
					break
				}
			}
			if !found {
				t.Fatalf("unique reference %s is absent", code)
			}
			target.Nest.HasRarityOverride = false
			if !RollsUniqueDrops(target.Rarity()) {
				t.Fatalf("%s has non-unique rarity %d", code, target.Rarity())
			}
			// No combat runs: the hunter level is used only by drop admission.
			player.Level = testInt64(uniqueProbeLevel)
			rt.deps.(*enterworld.Deps).Items = items
			rt.deps.(*enterworld.Deps).MagicOptions = enterworld.NewTextdataMagicOptions(dir)
			uniqueProbeConfigure(t, rt)
			if err := rt.ValidateLootReferences(); err != nil {
				t.Fatal(err)
			}
			// No seal of this unique, guaranteed or ordinary, may pass the
			// degree its drop-table level offers: the top degree for a
			// top-band unique, its own degree below the band. The
			// guaranteed Star carries that degree; ordinary rare rolls may
			// land below it, as their native class rows do.
			tableLevel := rt.uniqueDropTableLevel(target)
			starGroup, ok := loot.SealGroupAt(tableLevel, enterworld.SealStar)
			if !ok {
				t.Fatalf("%s has no seal group at drop-table level %d", code, tableLevel)
			}
			wantDegree := starGroup/3 + 1
			seed := uint64(target.Ref.RefObjID)
			source := rand.New(rand.NewPCG(uniqueProbeSeed, seed))
			rt.DropRoll = func() (uint32, error) { return source.Uint32N(dropRollDomain), nil }
			result := uniqueLootMeasurement{
				Monster: code, MonsterLevel: target.Ref.Level, Rarity: target.Rarity(), Seed: seed,
				SealDegrees: map[int]int{}, NormalClasses: map[int]int{}, SunPerHundredKills: map[string]int{},
			}
			batchSun := 0
			for kill := 0; kill < uniqueProbeKills; kill++ {
				drops := planUniqueKill(rt, player, target)
				result.Kills++
				result.Items += len(drops)
				var seals [3]int
				for _, drop := range drops {
					ref, ok := items.ItemRefByCodename(drop.Codename)
					if !ok || ref == nil {
						t.Fatalf("unresolvable generated drop %s", drop.Codename)
					}
					seal, sealed := enterworld.EquipmentSeal(ref)
					if !sealed {
						if ref.TypeIDs[0] == 3 && ref.TypeIDs[1] == 1 {
							result.NormalClasses[int(ref.NativeFields.Get("itemClass"))]++
						}
						continue
					}
					seals[seal]++
					degree := enterworld.EquipmentDegree(ref)
					result.SealDegrees[degree]++
					if degree > wantDegree {
						t.Fatalf("kill generated degree-%d seal %s, above the degree-%d top of drop-table level %d", degree, drop.Codename, wantDegree, tableLevel)
					}
				}
				for seal, count := range seals {
					result.Seals[seal] += count
					if count > 0 {
						result.KillsWithSeal[seal]++
					}
				}
				if seals[enterworld.SealStar] == 0 {
					t.Fatalf("kill %d generated no Star", kill+1)
				}
				batchSun += seals[enterworld.SealSun]
				if (kill+1)%uniqueProbeBatchSize == 0 {
					result.SunPerHundredKills[strconv.Itoa(batchSun)]++
					batchSun = 0
				}
			}
			if result.Seals[enterworld.SealMoon] == 0 {
				t.Fatal("Moon rewards never generated")
			}
			results = append(results, result)
			t.Logf("kills=%d Star/Moon/Sun=%v kills-with-seal=%v degrees=%v normal-classes=%v items/kill=%.3f Sun-per-100=%v",
				result.Kills, result.Seals, result.KillsWithSeal, result.SealDegrees, result.NormalClasses,
				float64(result.Items)/float64(result.Kills), result.SunPerHundredKills)
		})
	}
	if t.Failed() {
		return
	}
	if destination := os.Getenv("SRO_UNIQUE_LOOT_OUTPUT"); destination != "" {
		report := struct {
			Settings map[string]string       `json:"settings"`
			Seed     int                     `json:"seed"`
			Results  []uniqueLootMeasurement `json:"results"`
		}{settings, uniqueProbeSeed, results}
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(destination, append(data, '\n'), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

/*
================
uniqueProbeSettings

Import only the supplied loot settings, confined to this test process.
================
*/
func uniqueProbeSettings(t *testing.T) map[string]string {
	t.Helper()
	data, err := os.ReadFile(os.Getenv("SRO_UNIQUE_LOOT_SETTINGS"))
	if err != nil {
		t.Fatal(err)
	}
	var settings map[string]string
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatal(err)
	}
	for name, value := range settings {
		t.Setenv(name, value)
	}
	if settings[progression.EnvLevelCap] != "90" {
		t.Fatal("this measurement requires the deployed cap-90 configuration")
	}
	return settings
}

/*
================
uniqueProbeConfigure

The loot-related GameWorld wiring, using its real environment parsers.
Attack-pet expansion is disabled on the measured deployment.
================
*/
func uniqueProbeConfigure(t *testing.T, rt *Runtime) {
	t.Helper()
	growth := progression.BetaGrowthFromEnv()
	if growth.Enabled {
		rt.DropPassRate, rt.DropCap = growth.DropRate, growth.DropCap
		rt.GoldRate, rt.RareRate = growth.GoldRate, growth.RareRate
		mix, err := progression.BetaDropMixFromEnv()
		if err != nil {
			t.Fatal(err)
		}
		rt.DropMix = mix
	}
	rt.UniqueRules = progression.UniqueRulesFromEnv()
	rt.SealRates = progression.BetaSealRatesFromEnv()
	rt.SunRate = progression.SunRateFromEnv()
	rt.SunEventOnly = progression.SunEventOnlyFromEnv()
	rt.ShopOnly = progression.BetaShopOnlyDegree9FromEnv()
	rt.ElixirDrops = progression.ElixirDropsFromEnv()
	if os.Getenv("SRO_ATTACK_PET_EXPANSION") != "off" || os.Getenv("SRO_CAP110_MONSTERS") != "off" {
		t.Fatal("this cap-90 measurement does not install optional expansion catalogs")
	}
}
