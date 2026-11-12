/*
===========================================================================

coverage_test.go - every shard setting is modelled or exempt, and every
modelled setting moves the output (#2542)

===========================================================================
*/
package main

import (
	"bytes"
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"testing"

	"opensro.online/server/internal/catalogue"
	"opensro.online/server/internal/config/shardsettings"
	"opensro.online/server/internal/data/store"
	"opensro.online/server/internal/game/action"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/alchemy"
	"opensro.online/server/internal/game/item/commerce"
	"opensro.online/server/internal/game/progression"
	"opensro.online/server/internal/game/quest"
	"opensro.online/server/internal/game/tunables"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/testsupport/alexandriatest"
	"opensro.online/server/internal/testsupport/gamedatatest"
	"opensro.online/server/internal/testsupport/tier"
)

/*
================
TestEveryShardSettingIsModelledOrExempt

The coverage table's partition: every shard setting is read by a named
catalogue function or listed with a reason, never both, never neither,
with no stale names - and the Setting.Catalogue flag agrees with the
table, so the pin in resolveShardSettings cannot drift from it either.
================
*/
func TestEveryShardSettingIsModelledOrExempt(t *testing.T) {
	known := map[string]bool{}
	for _, s := range shardsettings.Settings {
		known[s.Env] = true
		reader, modelled := catalogueReaders[s.Env]
		reason, exempt := catalogueExempt[s.Env]
		switch {
		case modelled && exempt:
			t.Errorf("%s is both modelled and exempt; pick one", s.Env)
		case !modelled && !exempt:
			t.Errorf("%s is neither modelled nor exempt: model it in the catalogue or list it in coverage.go with a reason", s.Env)
		case modelled && strings.TrimSpace(reader) == "":
			t.Errorf("%s is modelled by an unnamed function; name it in coverage.go", s.Env)
		case exempt && strings.TrimSpace(reason) == "":
			t.Errorf("%s is exempt without a reason", s.Env)
		}
		if s.Catalogue != modelled {
			t.Errorf("%s: Catalogue flag is %v but coverage.go models it %v; the flag and the table must agree", s.Env, s.Catalogue, modelled)
		}
	}
	for env := range catalogueReaders {
		if !known[env] {
			t.Errorf("catalogueReaders names %s, which is not a shard setting", env)
		}
	}
	for env := range catalogueExempt {
		if !known[env] {
			t.Errorf("catalogueExempt names %s, which is not a shard setting", env)
		}
	}
}

/*
================
flipFixture

The loaded textdata the flip builds share: items, skills, levels, the
commerce catalogue and the baseline monsters, items and NPCs, each
loaded once under the deployment defaults.
================
*/
type flipFixture struct {
	dir      string
	header   catalogue.Header
	skills   *enterworld.TextdataSkills
	levels   enterworld.LevelDataSource
	items    *enterworld.TextdataItems
	itemID   func(string) uint32
	monsters []catalogue.Monster
	shops    *commerce.Catalog
	npcs     []catalogue.Npc
	alchemy  *alchemy.Catalog
}

/*
================
newFlipFixture
================
*/
func newFlipFixture(t *testing.T) *flipFixture {
	t.Helper()
	dir := gamedatatest.TextdataDir(t)
	fx := &flipFixture{dir: dir, header: catalogue.Header{SchemaVersion: catalogue.SchemaVersion}}
	fx.skills = enterworld.NewTextdataSkills(dir)
	fx.levels = enterworld.NewTextdataLevels(dir)
	fx.items = enterworld.NewTextdataItems(dir)
	fx.itemID = func(codename string) uint32 {
		if ref, ok := fx.items.ItemRefByCodename(codename); ok {
			return ref.RefObjID
		}
		return 0
	}
	population, err := shardPopulation(dir, monster.Cap110Enabled())
	if err != nil {
		t.Fatal(err)
	}
	if fx.monsters, err = catalogue.BuildMonsters(catalogue.MonsterSource{Template: population,
		ItemID: fx.itemID, Skills: fx.skills}); err != nil {
		t.Fatal(err)
	}
	if fx.shops, err = shardShops(dir, fx.items); err != nil {
		t.Fatal(err)
	}
	fx.npcs = catalogue.BuildNpcs(catalogue.NpcSource{Roster: simulation.LoadNpcWorldRoster(dir), Shops: fx.shops})
	if fx.alchemy, err = alchemy.LoadCatalog(dir, fx.items); err != nil {
		t.Fatal(err)
	}
	return fx
}

/*
================
build

One family's published output under the caller's environment, or a
fatal test error.
================
*/
func (fx *flipFixture) build(t *testing.T, family string) []byte {
	t.Helper()
	data, err := fx.buildErr(t, family)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

/*
================
buildErr

The same build, with the monsters family's errors returned instead of
fatal: a refusal flip asserts on them.
================
*/
func (fx *flipFixture) buildErr(t *testing.T, family string) ([]byte, error) {
	t.Helper()
	marshal := func(v any) []byte {
		data, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	switch family {
	case "droptables":
		return marshal(catalogue.BuildDropTables(fx.monsters, fx.itemID, fx.header)), nil
	case "levels":
		mastery, err := progression.BetaMasteryFromEnv()
		if err != nil {
			t.Fatal(err)
		}
		cap, err := progression.LevelCapFromEnv()
		if err != nil {
			t.Fatal(err)
		}
		return marshal(catalogue.BuildLevels(fx.header, fx.levels, mastery, cap)), nil
	case "items":
		items, err := shardItems(fx.dir)
		if err != nil {
			t.Fatal(err)
		}
		return marshal(catalogue.BuildItems(catalogue.ItemSource{Refs: itemRefs(items),
			Usable: action.ItemUseSupported, PetGearFactor: store.DefaultPetGearDropFactor})), nil
	case "shops":
		shops, err := shardShops(fx.dir, fx.items)
		if err != nil {
			t.Fatal(err)
		}
		return marshal(shops.Tabs), nil
	case "trade":
		settings, err := tunables.Parse(os.Getenv)
		if err != nil {
			t.Fatal(err)
		}
		restore := tunables.Swap(settings)
		defer restore()
		quotes := catalogue.BuildTrade(fx.shops.TradeQuotations, func(id uint32) *enterworld.ItemRef {
			if ref, ok := fx.items.ItemRefByID(id); ok {
				return ref
			}
			return nil
		}, func(id uint32) bool {
			for _, npc := range fx.npcs {
				if npc.ID == id {
					return true
				}
			}
			return false
		})
		if action.TradeTiersFromEnv() {
			route, codenames := action.TradeRouteProfit(fx.shops), map[uint32]string{}
			for _, npc := range fx.npcs {
				codenames[npc.ID] = npc.Codename
			}
			catalogue.ApplyTradeRoutes(quotes, func(npcID, itemID uint32) int64 {
				return route(codenames[npcID], itemID)
			}, action.RaiseRouteProfit)
		}
		return marshal(quotes), nil
	case "monsters":
		population, err := shardPopulation(fx.dir, monster.Cap110Enabled())
		if err != nil {
			return nil, err
		}
		monsters, err := catalogue.BuildMonsters(catalogue.MonsterSource{Template: population,
			ItemID: fx.itemID, Skills: fx.skills})
		if err != nil {
			return nil, err
		}
		return marshal(monsters), nil
	case "npcs-alexandria":
		// The shipped test data carries no alexandria-overlay.json, so
		// the roster cannot move over it; both sides build over one
		// synthesized variant directory instead (alexandriatest), the
		// off side first, under the posture the caller set.
		native := simulation.LoadNpcWorldRoster(fx.dir)
		if len(native) == 0 {
			t.Fatal("no NPC roster to vary")
		}
		variant := alexandriatest.Variant(t, strconv.FormatUint(uint64(native[0].RefObjID), 10))
		side := func() []byte {
			return marshal(catalogue.BuildNpcs(catalogue.NpcSource{
				Roster: simulation.LoadNpcWorldRoster(variant.Dir), Shops: fx.shops}))
		}
		base := side()
		t.Setenv("SRO_ALEXANDRIA", "on")
		if got := side(); bytes.Equal(base, got) {
			t.Fatal("SRO_ALEXANDRIA=on left npcs.json unchanged over the variant roster")
		}
		return nil, nil
	case "quests":
		itemSource, err := shardItems(fx.dir)
		if err != nil {
			t.Fatal(err)
		}
		catalog := quest.NewCatalog(fx.dir)
		definitions, err := quest.LoadDefinitions(catalog, itemSource)
		if err != nil {
			t.Fatal(err)
		}
		return marshal(catalogue.BuildQuests(catalogue.QuestSource{Catalog: catalog, Definitions: definitions,
			Text: englishText(fx.dir), Listed: listedQuests(fx.dir), ItemID: fx.itemID, Levels: fx.levels})), nil
	case "events":
		return marshal(catalogue.BuildEvents(fx.header, fx.itemID)), nil
	case "alchemy":
		catalog := *fx.alchemy
		rates, err := alchemy.ReinforceRatesFromEnv()
		if err != nil {
			t.Fatal(err)
		}
		catalog.Rates = rates
		maxPlus, err := alchemy.ReinforceMaxPlusFromEnv()
		if err != nil {
			t.Fatal(err)
		}
		catalog.MaxPlus = maxPlus
		rows := skillRows(fx.skills)
		return marshal(catalogue.BuildAlchemy(fx.header, catalogue.AlchemySource{Catalog: &catalog,
			ItemID: fx.itemID, Skills: rows, SkillName: func(row enterworld.SkillRow) string {
				return row.Codename
			}})), nil
	}
	t.Fatalf("unknown flip family %q", family)
	return nil, nil
}

/*
================
TestModelledSettingsMoveTheOutput

The sensitivity half of the contract: the catalogue is built under the
deployment defaults, each modelled setting is flipped to a non-default
value on top of its prerequisites, and the output it feeds must differ -
a reader that ignores its setting fails here. Prerequisites pin the
posture a setting only matters in (a tomb rate needs the tomb), on both
sides alike. The beta rules ride droptables.json's beta object, which
the deployed shard runs with beta growth on, so their flips sit on that
posture too; the unique rules stand outside it (#2537), so their flips
sit on the defaults alone. Two settings can show no flip over the
shipped data
(their variant populations are not in it); flipUnitProven names the
unit test that proves each of those readers instead.
================
*/
func TestModelledSettingsMoveTheOutput(t *testing.T) {
	tier.Long(t, "every modelled catalogue setting moves the output it feeds")
	for _, s := range shardsettings.Settings {
		t.Setenv(s.Env, "")
	}
	if err := resolveShardSettings(); err != nil {
		t.Fatal(err)
	}
	fx := newFlipFixture(t)
	const halloweenWindow = "2026-10-24T00:00:00Z/2026-11-15T00:00:00Z"
	growth := map[string]string{"SRO_BETA_GROWTH": "on"}
	// A refusal flip proves the switch is read where its variant data is
	// not shipped: the flipped build must refuse, naming the gate, where
	// the base build succeeded. On a deploy whose data carries the
	// variant populations the same flip changes monsters.json instead.
	flips := []struct {
		env     string
		value   string
		also    map[string]string
		family  string
		refusal string
	}{
		{"SRO_BETA_GROWTH", "on", nil, "droptables", ""},
		{"SRO_BETA_SKILL_EXP_RATE", "37", growth, "droptables", ""},
		{"SRO_BETA_DROP_RATE", "4", growth, "droptables", ""},
		{"SRO_BETA_DROP_CAP", "12", growth, "droptables", ""},
		{"SRO_BETA_GOLD_RATE", "3", growth, "droptables", ""},
		{"SRO_BETA_RARE_RATE", "3", growth, "droptables", ""},
		{"SRO_BETA_SEAL_RATES", "on", growth, "droptables", ""},
		{"SRO_BETA_DROP_MIX", "on", growth, "droptables", ""},
		{"SRO_BETA_SHOP_ONLY_DEGREE9", "on", growth, "droptables", ""},
		{"SRO_ELIXIR_DROPS_OFF", "off", growth, "droptables", ""},
		{"SRO_ALCHEMY_MATERIAL_DROPS_OFF", "on", growth, "droptables", ""},
		{"SRO_SUN_EVENT_ONLY", "on", nil, "droptables", ""},
		{"SRO_SUN_DROP_RATE", "5", growth, "droptables", ""},
		{"SRO_UNIQUE_GAP_EXEMPT", "off", nil, "droptables", ""},
		{"SRO_UNIQUE_POP_TICKET", "off", nil, "droptables", ""},
		{"SRO_UNIQUE_MAX_LEVEL_LOOT", "on", nil, "droptables", ""},
		{"SRO_UNIQUE_SEAL_DROPS", "on", nil, "droptables", ""},
		{"SRO_BETA_MASTERY", "off", nil, "levels", ""},
		{"SRO_BETA_GROWTH_BANDS", "1-110:2", growth, "quests", ""},
		{"SRO_LEVEL_CAP", "110", nil, "levels", ""},
		{"SRO_STACK_SIZES", "off", nil, "items", ""},
		{"SRO_TRADE_TIERS", "off", nil, "items", ""},
		{"SRO_SHOP_EXTRA_GOODS", "off", nil, "shops", ""},
		{"SRO_JANGAN_ALCHEMY_SHOP", "off", nil, "shops", ""},
		{"SRO_JANGAN_XL_POTIONS", "off", nil, "shops", ""},
		{"SRO_TRADE_ROUTE_DONWHANG_PERCENT", "150", nil, "trade", ""},
		{"SRO_TRADE_ROUTE_HOTAN_PERCENT", "160", nil, "trade", ""},
		{"SRO_TRADE_ROUTE_SAMARKAND_PERCENT", "170", nil, "trade", ""},
		{"SRO_TRADE_ROUTE_CONSTANTINOPLE_PERCENT", "180", nil, "trade", ""},
		{"SRO_CAP110_MONSTERS", "on", map[string]string{"SRO_ALEXANDRIA": "on"}, "monsters", "cap110-monsters.txt"},
		{"SRO_PHARAOH_TOMB", "on", nil, "monsters", "SRO_PHARAOH_TOMB requires"},
		{"SRO_CAP110_TEMPLE", "on", nil, "monsters", "SRO_CAP110_TEMPLE requires"},
		{"SRO_SKYTEMPLE", "on", nil, "monsters", "skytemple population"},
		{"SRO_ARABIA", "on", nil, "monsters", "arabia population"},
		{"SRO_CAP110_QINSHI", "on", nil, "monsters", "SRO_CAP110_QINSHI requires"},
		{"SRO_QINSHI_TOMB", "off", nil, "monsters", ""},
		{"SRO_ALEXANDRIA", "on", nil, "npcs-alexandria", ""},
		{"SRO_HALLOWEEN_EVENT", "on", map[string]string{"SRO_HALLOWEEN_EVENT_WINDOW": halloweenWindow}, "events", ""},
		{"SRO_HALLOWEEN_EVENT_WINDOW", "2026-10-25T00:00:00Z/2026-11-16T00:00:00Z",
			map[string]string{"SRO_HALLOWEEN_EVENT": "on", "SRO_HALLOWEEN_EVENT_WINDOW": halloweenWindow}, "events", ""},
		{"SRO_ATTACK_PET_EXPANSION", "on", nil, "items", ""},
		{"SRO_ALCHEMY_RATES", "off", nil, "alchemy", ""},
		{"SRO_ALCHEMY_MAX_PLUS", "11", nil, "alchemy", ""},
	}
	// flipUnitProven names the modelled settings whose flip no build
	// over the shipped data can show, and the test that proves the
	// reader instead. Every other modelled setting flips above.
	flipUnitProven := map[string]string{
		"SRO_CAP110_LEVEL100":           "the level-100 slice's admission is pinned by the monster package's TestCap110Level100SliceFollowsItsFlag; over the shipped data the cap-110 build refuses before the slice can differ",
		"SRO_PHARAOH_TOMB_GEAR_RATE":    "the modelled counts are pinned by the catalogue package's TestPharaohGearRateScalesTheTombDrops and the env read by the monster package's TestAppendPharaohTombReadsTheGearRate; over the shipped data the tomb build refuses before the rate can differ",
		"SRO_ALEXANDRIA_HIDE_D11_GOODS": "the withholding is pinned by the commerce package's TestWithholdAlexandriaD11 and the env read by its TestAlexandriaHideD11GoodsFromEnv; over the shipped data no Alexandria shop tab exists, so no shop output can differ",
	}
	flipped := map[string]bool{}
	for _, flip := range flips {
		flipped[flip.env] = true
		t.Run(flip.env, func(t *testing.T) {
			for env, value := range flip.also {
				t.Setenv(env, value)
			}
			if flip.refusal != "" {
				if _, err := fx.buildErr(t, flip.family); err != nil {
					t.Fatalf("base build refused: %v", err)
				}
				t.Setenv(flip.env, flip.value)
				_, err := fx.buildErr(t, flip.family)
				if err == nil || !strings.Contains(err.Error(), flip.refusal) {
					t.Fatalf("%s=%q: build error %v, want a refusal naming %q - the switch was not read", flip.env, flip.value, err, flip.refusal)
				}
				return
			}
			base := fx.build(t, flip.family)
			if base == nil {
				return // the family asserted the flip internally
			}
			t.Setenv(flip.env, flip.value)
			if got := fx.build(t, flip.family); bytes.Equal(base, got) {
				t.Fatalf("%s=%q left the %s output unchanged; the catalogue reader named in coverage.go does not read it", flip.env, flip.value, flip.family)
			}
		})
	}
	for env := range catalogueReaders {
		if !flipped[env] && flipUnitProven[env] == "" {
			t.Errorf("%s is modelled in coverage.go but has no flip here; add one or name its unit proof", env)
		}
	}
	for env := range flipUnitProven {
		if _, modelled := catalogueReaders[env]; !modelled {
			t.Errorf("%s has a unit proof but is not modelled in coverage.go", env)
		}
	}
}
