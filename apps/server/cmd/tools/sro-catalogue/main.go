/*
===========================================================================

main.go - sro-catalogue: write the website's static game catalogue

Loads the server's own reference data (the verified game-data projection,
gamedata.Resolve, unless -textdata names a directory) and writes the
catalogue files and their JSON Schemas into -out. The Node build stage
(scripts/build/catalogue) owns where -out is and adds the client-side
parts; this tool never resolves a .generated path itself.

===========================================================================
*/
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"opensro.online/server/internal/catalogue"
	"opensro.online/server/internal/config/shardsettings"
	"opensro.online/server/internal/game/action"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/alchemy"
	"opensro.online/server/internal/game/item/commerce"
	"opensro.online/server/internal/game/item/loot"
	"opensro.online/server/internal/game/progression"
	"opensro.online/server/internal/game/quest"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/gamedata"
)

/*
================
textdataDir
================
*/
func textdataDir(override string) (string, error) {
	if override != "" {
		return override, nil
	}
	paths, err := gamedata.Resolve()
	if err != nil {
		return "", fmt.Errorf("server game data: %w", err)
	}
	return paths.TextdataDir, nil
}

/*
================
writeJSON
================
*/
func writeJSON(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

/*
================
itemRefs

Every item reference the server loads, in the order the GM catalogue lists
them (BuildItems sorts by id).
================
*/
func itemRefs(source *enterworld.TextdataItems) []*enterworld.ItemRef {
	var refs []*enterworld.ItemRef
	for _, command := range source.ItemCommandReferences() {
		if ref, ok := source.ItemRefByCodename(command.Codename); ok {
			refs = append(refs, ref)
		}
	}
	return refs
}

/*
================
clientReference

An item reference exactly as the server sends it to the client
(commerce_references.go): the Node stage builds the client's own item from
it to run the client's tooltip. It is build input, never published.
================
*/
type clientReference struct {
	DescriptionSymbol string                  `json:"descriptionSymbol,omitempty"`
	ID                uint32                  `json:"refObjId"`
	Type              uint16                  `json:"typeFlags"`
	Name              string                  `json:"name"`
	Icon              string                  `json:"icon,omitempty"`
	NativeFields      enterworld.NativeFields `json:"nativeFields,omitempty"`
}

/*
================
clientReferences
================
*/
func clientReferences(refs []*enterworld.ItemRef, skills enterworld.SkillDataSource) []clientReference {
	out := make([]clientReference, 0, len(refs))
	for _, ref := range refs {
		out = append(out, clientReference{DescriptionSymbol: ref.DescriptionSymbol, ID: ref.RefObjID, Type: ref.TypeFlags(),
			Name: ref.Name, Icon: ref.Icon, NativeFields: enterworld.ItemUseNativeFields(ref, skills)})
	}
	return out
}

/*
================
englishText

The English column (8) of textdataname and textquest, by symbol: the same
text the server and client show.
================
*/
func englishText(dir string) func(string) string {
	names := map[string]string{}
	for _, file := range []string{"textdataname.txt", "textquest.txt"} {
		for _, cols := range enterworld.ReadTextdataFile(filepath.Join(dir, file)) {
			if len(cols) >= 9 && strings.TrimSpace(cols[0]) == "1" {
				names[strings.TrimSpace(cols[1])] = strings.TrimSpace(cols[8])
			}
		}
	}
	return func(symbol string) string { return names[symbol] }
}

/*
================
listedQuests

questdata's codenames in file order (service rows only).
================
*/
func listedQuests(dir string) []string {
	var out []string
	for _, cols := range enterworld.ReadTextdataFile(filepath.Join(dir, "questdata.txt")) {
		if len(cols) >= 3 && strings.TrimSpace(cols[0]) == "1" {
			out = append(out, strings.TrimSpace(cols[2]))
		}
	}
	return out
}

/*
================
skillRows

Every skill row the server loads, through the spawn projection's id list
(the loader's only full enumeration).
================
*/
func skillRows(source *enterworld.TextdataSkills) []enterworld.SkillRow {
	var rows []enterworld.SkillRow
	for _, spawn := range source.SpawnSkillRows() {
		if row, ok := source.SkillByID(spawn.ID); ok {
			rows = append(rows, row)
		}
	}
	return rows
}

/*
================
shardShops

The shops the shard sells from: the same setting (SRO_SHOP_EXTRA_GOODS,
port-only) and the same loader the GameWorld uses, so the published shop
tabs list the extra goods the shard sells. Unset or "off" is the v1.150
stock.
=========}

/*
================
*/
func shardShops(dir string, items enterworld.ItemRefSource) (*commerce.Catalog, error) {
	extra, err := commerce.ExtraGoodsFromEnv()
	if err != nil {
		return nil, err
	}
	shops, err := commerce.LoadWithExtraGoods(dir, items, extra)
	if err != nil {
		return nil, err
	}
	// The GameWorld withholds the same goods permanently (action.ConfigureCommerce).
	shops.WithholdGoods(action.SunShopExcluded)
	// And prices trade goods the same way (SRO_TRADE_TIERS).
	if action.TradeTiersFromEnv() {
		shops.ScaleTradeGoodsPrices(enterworld.TradeGoodsPriceFactor)
	}
	return shops, nil
}

/*
================
shardItems

The item references as the GameWorld holds them: SRO_STACK_SIZES raises
the stack caps (port-only), and SRO_TRADE_TIERS prices trade goods at
TradeGoodsPriceFactor times their v1.150 price.
================
*/
func shardItems(dir string) (*enterworld.TextdataItems, error) {
	items := enterworld.NewTextdataItems(dir)
	sizes, err := enterworld.StackSizesFromEnv()
	if err != nil {
		return nil, err
	}
	if _, err := items.ApplyStackSizes(sizes); err != nil {
		return nil, err
	}
	if action.TradeTiersFromEnv() {
		if _, err := items.ApplyTradeGoodsPriceFactor(enterworld.TradeGoodsPriceFactor); err != nil {
			return nil, err
		}
	}
	return items, nil
}

/*
================
resolveShardSettings

The catalogue describes the shard a default deploy runs: every setting it
reads that the operator left unset takes its deployment default from the
shard settings table (the one sro-nomad deploys from), before any reader
looks. An explicit value, "off" included, still wins.
================
*/
func resolveShardSettings() error {
	filled, err := shardsettings.ApplyCatalogueDefaults(os.Getenv, os.Setenv)
	if err != nil {
		return fmt.Errorf("shard settings: %w", err)
	}
	if len(filled) > 0 {
		fmt.Fprintf(os.Stderr, "shard settings at their deployment defaults: %s\n", strings.Join(filled, ", "))
	}
	return nil
}

/*
================
shardPopulation

The monster template the shard spawns from, for the catalogue: the
retail population plus every switch-gated family the server's bootstrap
appends (enterworld/devdeps.go), with the switches resolved to their
deployment defaults (resolveShardSettings). The shipped Qin-Shi tomb
(SRO_QINSHI_TOMB, default on) was missing here, so its 79 species and
their uniques had no catalogue page (#2025).
================
*/
func shardPopulation(dir string, cap110 bool) (monster.Template, error) {
	template, err := monster.AppendCap110(monster.LoadTemplate(dir), dir, cap110)
	if err != nil {
		return monster.Template{}, fmt.Errorf("catalogue population: %w", err)
	}
	template, err = monster.AppendPharaohTomb(template, dir, monster.PharaohTombEnabled())
	if err != nil {
		return monster.Template{}, fmt.Errorf("catalogue tomb population: %w", err)
	}
	template, err = monster.AppendQinshi(template, dir, monster.QinshiEnabled())
	if err != nil {
		return monster.Template{}, fmt.Errorf("catalogue qinshi population: %w", err)
	}
	if monster.QinShiTombFromEnv() {
		template, err = template.WithQinShiTomb(dir)
		if err != nil {
			return monster.Template{}, fmt.Errorf("catalogue qinshi tomb population: %w", err)
		}
	}
	return template, nil
}

/*
================
run
================
*/
func run(out, release, textdata string) error {
	if err := resolveShardSettings(); err != nil {
		return err
	}
	dir, err := textdataDir(textdata)
	if err != nil {
		return err
	}
	header := catalogue.Header{SchemaVersion: catalogue.SchemaVersion, Release: release,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339)}
	cap110 := monster.Cap110Enabled()
	if err := loot.InitCap110(dir, cap110); err != nil {
		return fmt.Errorf("catalogue loot: %w", err)
	}
	refItems, err := shardItems(dir)
	if err != nil {
		return err
	}
	refs := itemRefs(refItems)
	if len(refs) == 0 {
		return fmt.Errorf("no item references loaded from %s", dir)
	}
	items := catalogue.BuildItems(catalogue.ItemSource{Refs: refs, Usable: action.ItemUseSupported})
	itemSource, err := shardItems(dir)
	if err != nil {
		return err
	}
	questCatalog := quest.NewCatalog(dir)
	definitions, err := quest.LoadDefinitions(questCatalog, itemSource)
	if err != nil {
		return fmt.Errorf("quest definitions: %w", err)
	}
	itemID := func(codename string) uint32 {
		if ref, ok := itemSource.ItemRefByCodename(codename); ok {
			return ref.RefObjID
		}
		return 0
	}
	itemByID := func(id uint32) *enterworld.ItemRef {
		if ref, ok := itemSource.ItemRefByID(id); ok {
			return ref
		}
		return nil
	}
	// SRO_SUN_EVENT_ONLY (port-only, not native): a Sun pick drops nothing
	// on the shard, so its chances leave the published drop lists; every
	// other item keeps its own chance (action/sunevent.go).
	sun := progression.SunEventOnlyFromEnv()
	dropItemID := func(codename string) uint32 {
		if ref, ok := itemSource.ItemRefByCodename(codename); ok && action.SunExcludes(sun, ref) {
			return 0
		}
		return itemID(codename)
	}
	quests := catalogue.BuildQuests(catalogue.QuestSource{Catalog: questCatalog, Definitions: definitions,
		Text: englishText(dir), Listed: listedQuests(dir), ItemID: itemID})
	skillData := enterworld.NewTextdataSkills(dir)
	template, err := shardPopulation(dir, cap110)
	if err != nil {
		return err
	}
	monsters, err := catalogue.BuildMonsters(catalogue.MonsterSource{Template: template, ItemID: dropItemID,
		Skills: skillData})
	if err != nil {
		return fmt.Errorf("monsters: %w", err)
	}
	shops, err := shardShops(dir, itemSource)
	if err != nil {
		return fmt.Errorf("shops: %w", err)
	}
	alchemyCatalog, err := alchemy.LoadCatalog(dir, itemSource)
	if err != nil {
		return fmt.Errorf("alchemy: %w", err)
	}
	// The export describes the shard it is built for, so it reads the same
	// port-only setting the GameWorld does.
	if alchemyCatalog.Rates, err = alchemy.ReinforceRatesFromEnv(); err != nil {
		return err
	}
	if alchemyCatalog.MaxPlus, err = alchemy.ReinforceMaxPlusFromEnv(); err != nil {
		return err
	}
	npcs := catalogue.BuildNpcs(catalogue.NpcSource{Roster: simulation.LoadNpcWorldRoster(dir), Shops: shops})
	placedNpcs := map[uint32]bool{}
	for _, npc := range npcs {
		placedNpcs[npc.ID] = true
	}
	text := englishText(dir)
	betaMastery, err := progression.BetaMasteryFromEnv()
	if err != nil {
		return err
	}
	if _, err := progression.BetaDropMixFromEnv(); err != nil {
		return err
	}
	skillList := skillRows(skillData)
	skills := catalogue.BuildSkills(catalogue.SkillSource{Rows: skillList,
		Handled: func(row enterworld.SkillRow) bool { return action.SkillPressHandled(skillData, row) },
		Plan:    func(id uint32) enterworld.SkillExecutionKind { return skillData.ExecutionPlan(id).Kind() }})
	if err := writeJSON(filepath.Join(out, "_build", "item-references.json"),
		clientReferences(refs, skillData)); err != nil {
		return err
	}
	// Every magicoption.txt definition, as the enter-world magicOptionSnapshot
	// ships it: the website's tooltip bundle resolves a worn item's options
	// through the same rows the game client does.
	if err := writeJSON(filepath.Join(out, "_build", "magic-options.json"),
		enterworld.NewTextdataMagicOptions(dir).AllMagicOptions()); err != nil {
		return err
	}
	tradeQuotes := catalogue.BuildTrade(shops.TradeQuotations, itemByID, func(id uint32) bool { return placedNpcs[id] })
	// SRO_TRADE_TIERS (port-only, not native): the shard's route profit.
	if action.TradeTiersFromEnv() {
		route, codenames := action.TradeRouteProfit(shops), map[uint32]string{}
		for _, npc := range npcs {
			codenames[npc.ID] = npc.Codename
		}
		catalogue.ApplyTradeRoutes(tradeQuotes, func(npcID, itemID uint32) int64 { return route(codenames[npcID], itemID) }, action.RaiseRouteProfit)
	}
	levelCap, err := progression.LevelCapFromEnv()
	if err != nil {
		return err
	}
	files := map[string]any{
		"items.json":    catalogue.ItemsFile{Header: header, Items: items},
		"quests.json":   catalogue.QuestsFile{Header: header, Quests: quests},
		"monsters.json": catalogue.MonstersFile{Header: header, Monsters: monsters},
		"skills.json":   catalogue.SkillsFile{Header: header, Masteries: []catalogue.SkillMastery{}, Skills: skills},
		"regions.json":  catalogue.RegionsFile{Header: header, Regions: []catalogue.Region{}},
		"npcs.json":     catalogue.NpcsFile{Header: header, Npcs: npcs},
		"alchemy.json": catalogue.BuildAlchemy(header, catalogue.AlchemySource{Catalog: alchemyCatalog, ItemID: itemID,
			Skills: skillList, SkillName: func(row enterworld.SkillRow) string {
				if name := text(row.NameSymbol); name != "" {
					return name
				}
				return row.Codename
			}}),
		"search.json":     catalogue.SearchFile{Header: header, Entries: []catalogue.SearchEntry{}},
		"atlas.json":      catalogue.NewAtlas(header),
		"notes.json":      catalogue.NotesFile{Header: header, Entries: []catalogue.NoteEntry{}},
		"levels.json":     catalogue.BuildLevels(header, enterworld.NewTextdataLevels(dir), betaMastery, levelCap),
		"trade.json":      catalogue.TradeFile{Header: header, ThiefRate: catalogue.ThiefRate(), Quotes: tradeQuotes},
		"droptables.json": catalogue.BuildDropTables(monsters, dropItemID, header),
		"summary.json": catalogue.Summarize(header, catalogue.SummaryInput{Items: catalogue.ItemStandings(items),
			Skills: catalogue.SkillStandings(skills), Quests: catalogue.QuestStandings(quests), Monsters: catalogue.MonsterStandings(monsters)}),
	}
	schemas := map[string]any{
		"items.schema.json":      catalogue.Schema("items.json", catalogue.ItemsFile{}),
		"summary.schema.json":    catalogue.Schema("summary.json", catalogue.SummaryFile{}),
		"quests.schema.json":     catalogue.Schema("quests.json", catalogue.QuestsFile{}),
		"monsters.schema.json":   catalogue.Schema("monsters.json", catalogue.MonstersFile{}),
		"droptables.schema.json": catalogue.Schema("droptables.json", catalogue.DropTablesFile{}),
		"skills.schema.json":     catalogue.Schema("skills.json", catalogue.SkillsFile{}),
		"regions.schema.json":    catalogue.Schema("regions.json", catalogue.RegionsFile{}),
		"npcs.schema.json":       catalogue.Schema("npcs.json", catalogue.NpcsFile{}),
		"trade.schema.json":      catalogue.Schema("trade.json", catalogue.TradeFile{}),
		"alchemy.schema.json":    catalogue.Schema("alchemy.json", catalogue.AlchemyFile{}),
		"search.schema.json":     catalogue.Schema("search.json", catalogue.SearchFile{}),
		"atlas.schema.json":      catalogue.Schema("atlas.json", catalogue.AtlasFile{}),
		"notes.schema.json":      catalogue.Schema("notes.json", catalogue.NotesFile{}),
		"levels.schema.json":     catalogue.Schema("levels.json", catalogue.LevelsFile{}),
	}
	for name, value := range files {
		if err := writeJSON(filepath.Join(out, name), value); err != nil {
			return err
		}
	}
	for name, value := range schemas {
		if err := writeJSON(filepath.Join(out, "schema", name), value); err != nil {
			return err
		}
	}
	return nil
}

/*
================
main
================
*/
func main() {
	out := flag.String("out", "", "Directory to write the catalogue into (required)")
	release := flag.String("release", "dev", "Release id the catalogue describes")
	textdata := flag.String("textdata", "", "Server textdata directory (default: the verified game-data projection)")
	flag.Parse()
	if *out == "" {
		fmt.Fprintln(os.Stderr, "sro-catalogue: -out is required")
		os.Exit(2)
	}
	if err := run(*out, *release, *textdata); err != nil {
		fmt.Fprintln(os.Stderr, "sro-catalogue:", err)
		os.Exit(1)
	}
}
