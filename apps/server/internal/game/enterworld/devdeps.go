package enterworld

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/game/world/worldarea"
)

// Environment overrides for development reference data. Durable character
// state is intentionally absent: the authority store is the only character
// source in every runtime mode.
const (
	// EnvMissionChatPath overrides the mission chat config file.
	EnvMissionChatPath = "SRO_MISSION_CHAT_PATH"
	// EnvMissionEquipItems controls mission equipment emission (default on).
	EnvMissionEquipItems = "MISSION_EQUIP_ITEMS"
)

// Module-relative development defaults.
var (
	// mission-chat.json is owned by this server.
	devDefaultMissionChatPath = filepath.Join("config", "mission-chat.json")
)

// DevPaths names every data source the dev Deps read. RosterPath is the
// verified server-owned playable-character identity catalogue; browser
// presentation metadata is deliberately absent from this composition. The remaining
// zero-value fields take their module-relative defaults. Characters are deliberately NOT a path: the
// authority store owns them (server.go injects Deps.Characters after Open).
type DevPaths struct {
	RosterPath        string
	TextdataDir       string
	MissionChatPath   string
	EquipItemsEnabled bool
	StarterKitEnabled bool
	NpcSpawns         NpcSpawnConfig
	MonsterSpawns     MonsterSpawnConfig
	AuthoredAreas     *worldarea.Catalog
	// StructureZones is world-authority/structure-zones.json: the event
	// zones the fortress worlds' structures stand on.
	StructureZones string
}

// DevPathsFromEnv resolves bootstrap-owned optional environment settings
// around the two required roots supplied by the composition layer.
func DevPathsFromEnv(characterAuthorityDir, textdataDir string) DevPaths {
	return DevPaths{
		RosterPath:        filepath.Join(characterAuthorityDir, "catalog.json"),
		TextdataDir:       textdataDir,
		MissionChatPath:   os.Getenv(EnvMissionChatPath),
		EquipItemsEnabled: os.Getenv(EnvMissionEquipItems) != "0",
		StarterKitEnabled: BetaStarterKitEnabled(),
		NpcSpawns:         NpcSpawnConfigFromEnv(),
		MonsterSpawns:     MonsterSpawnConfigFromEnv(),
	}
}

// NewDevDeps assembles the server-authoritative Deps from an eagerly loaded
// semantic textdata view, the character roster, mission chat config and
// division policy. Browser-owned model/audio/minimap catalogues are not loaded
// here.
//
// Characters and persistence are NOT assembled here: the authority store
// owns both (ADR-1 D2/D3), and the server wiring injects
// Deps.Characters / Deps.MutateCharacter / Deps.ResolveDivisionID after
// store.Open. The server wiring also APPENDS the division ground rows to
// ObjectListRows after construction (the item lane owns the registry).
func NewDevDeps(paths DevPaths, textdata *TextdataCatalogs) (*Deps, error) {
	if err := validateLoadedTextdata(textdata); err != nil {
		return nil, err
	}
	rosterPath := paths.RosterPath
	if rosterPath == "" {
		return nil, fmt.Errorf("bootstrap: server character-authority catalogue path is required")
	}
	roster, err := LoadRoster(rosterPath)
	if err != nil {
		return nil, fmt.Errorf("bootstrap: load server character-authority catalogue: %w", err)
	}
	return NewDevDepsWithRoster(paths, textdata, roster)
}

// NewDevDepsWithRoster assembles gameplay with the already validated roster
// owned by the process composition root. Character-select projection and
// enter-world bootstrap must read the same catalogue instance.
func NewDevDepsWithRoster(paths DevPaths, textdata *TextdataCatalogs, roster *Roster) (*Deps, error) {
	if err := validateLoadedTextdata(textdata); err != nil {
		return nil, err
	}
	if roster == nil {
		return nil, fmt.Errorf("bootstrap: loaded character roster is required")
	}

	// Characters come from the authority store, injected by the server
	// wiring AFTER this constructor (Deps.Characters, Deps.MutateCharacter,
	// Deps.ResolveDivisionID). Bare NewDevDeps serves an empty source:
	// every enter-world answers characterNotFound, which is the correct
	// posture for a Deps that nobody attached a store to (tests build
	// their own sources; production always injects).
	source := StaticCharacterSource{}

	// The composition root materializes these tables before opening network
	// admission. Reuse that one view here; never hide filesystem work in the
	// first EnterWorld request.
	textdataDir := paths.TextdataDir
	items := textdata.Items
	levels := textdata.Levels
	skills := textdata.Skills
	magicOptions := textdata.MagicOptions

	// Mission chat config (bootstrap systemMessages).
	chatPath := paths.MissionChatPath
	if chatPath == "" {
		chatPath = devDefaultMissionChatPath
	}
	chatConfig := LoadMissionChatConfig(chatPath)

	npcSpawns := paths.NpcSpawns
	if len(npcSpawns.Roster) == 0 {
		npcSpawns.Roster = simulation.LoadNpcWorldRoster(textdataDir)
		var gateErr error
		npcSpawns.Roster, gateErr = simulation.AppendTeleportGates(textdataDir, npcSpawns.Roster)
		if gateErr != nil {
			return nil, gateErr
		}
		if len(npcSpawns.Roster) == 0 && npcSpawns.Enabled {
			return nil, fmt.Errorf(
				"bootstrap: NPC spawning is enabled but the shipped npcpos/characterdata roster is empty under %s",
				textdataDir,
			)
		}
	}
	if npcSpawns.Enabled {
		if err := simulation.ValidateNpcRoster(npcSpawns.Roster); err != nil {
			return nil, fmt.Errorf("bootstrap: invalid NPC roster: %w", err)
		}
	}

	// Monster population (monster-live wave), ON BY DEFAULT (human
	// ruling seq388; MISSION_SPAWN_MONSTERS=0 is the kill switch). The
	// registry only exists while enabled - the killed path pays nothing.
	// Emission is per-viewer interest-scoped (worldgeom.InterestVisible); the
	// Resource-authored areas contribute ordinary template nests before the
	// registry is constructed; there is no per-session synthetic population.
	monsterSpawns := paths.MonsterSpawns
	var monsters *simulation.MonsterState
	if monsterSpawns.Enabled {
		template := monster.LoadTemplate(textdataDir)
		resolvedTemplate, err := appendAuthoredAreaPopulation(template, paths.AuthoredAreas)
		if err != nil {
			return nil, fmt.Errorf("bootstrap: %w", err)
		}
		template = resolvedTemplate
		template, err = appendFortressStructures(template, textdataDir, paths.StructureZones)
		if err != nil {
			return nil, fmt.Errorf("bootstrap: %w", err)
		}
		template, err = withMonsterSummonReferences(template, skills)
		if err != nil {
			return nil, fmt.Errorf("bootstrap: %w", err)
		}
		monsters = simulation.NewMonsterState(template)
		log.Infof("bootstrap: monster population ON (kill switch MISSION_SPAWN_MONSTERS=0): template %d nest rows / %d spawnable types; per-viewer native 320-unit block interest",
			len(template.Nests), len(template.SpawnableRefs()))
	} else {
		log.Infof("bootstrap: monster population KILLED via MISSION_SPAWN_MONSTERS=0 (retail default is ON)")
	}

	deps := &Deps{
		Roster:            roster,
		Characters:        source,
		Items:             items,
		Levels:            levels,
		Skills:            skills,
		MagicOptions:      magicOptions,
		EquipItemsEnabled: paths.EquipItemsEnabled,
		NpcSpawns:         npcSpawns,
		ResolveDivisionID: DevResolveDivisionIDFromCatalog(source),
		SystemMessages: func(character *Character) interface{} {
			return BuildSystemMessages(chatConfig, character)
		},
		// RefObj mirror rows: the FULL playable-character catalogue, NPC
		// roster, every itemdata-reachable COS, and FULL spawnable monster
		// roster (WIP seq140 C1 - the client seeds ONCE per session; a
		// refObjId first seen on a later 0x30D7 is silently dropped, so
		// the snapshot must carry every type that can ever stream).
		RefObjSnapshot: func() []RefObjRow {
			rows := CharacterModelRefObjSnapshot(roster)
			rows = append(rows, npcSpawns.NpcRefObjSnapshot()...)
			for _, ref := range items.SummonableCharacterRefs() {
				rows = append(rows, RefObjRow{
					RefObjID:                   ref.RefObjID,
					TidWord:                    ref.TidWord,
					Codename:                   ref.Codename,
					NameStrID:                  ref.NameStrID,
					Name:                       ref.Name,
					Level:                      ref.Level,
					MaxHP:                      ref.MaxHP,
					MountedAttackCapability210: ref.MountedAttackCapability210,
					Kind:                       "cos",
				})
			}
			return append(rows, monsterSpawns.MonsterRefObjSnapshot(monsters)...)
		},
	}
	if paths.StarterKitEnabled {
		deps.StarterKit = ResolveStarterKit(items)
		deps.StarterRefills = ResolveStarterRefills(items)
		log.Infof("bootstrap: beta starter kit ON (%s): %d items, backfilled on entry and never spent; %d potion families refilled to a full stack on entry", EnvBetaStarterKit, len(deps.StarterKit), len(deps.StarterRefills))
	}
	// Object-list rows: the NPC and monster legs are env-gated here; the
	// server wiring APPENDS the division ground drops from the item
	// lane's registry (the Node order is NPC rows first, then ground
	// rows). Monster rows ride between the two - POLICY: no native pin on
	// the intra-list ordering of monster vs NPC vs ground rows exists yet.
	deps.ObjectListRows = func(divisionID string, character *Character, entry *LocalPlayerEntry) []Packet {
		rows := npcSpawns.NpcObjectListRows(entry)
		instances := monsterSpawns.MonsterObjectListInstances(monsters, divisionID, entry)
		RecordMonsterObjectList(monsters, divisionID, character, instances)
		return append(rows, MonsterObjectListRows(
			monsters,
			divisionID,
			instances,
			time.Now().UnixMilli(),
			deps.SpawnTerrainHeight,
		)...)
	}
	// The server wiring hands the SAME registry to the tick's mover leg
	// (simulation.MonsterMoverOps); nil when the gate is off.
	deps.MonsterState = monsters
	return deps, nil
}

func validateLoadedTextdata(textdata *TextdataCatalogs) error {
	if textdata == nil || textdata.Items == nil || textdata.Levels == nil ||
		textdata.Skills == nil || textdata.MagicOptions == nil {
		return fmt.Errorf("bootstrap: loaded authoritative textdata catalogues are required")
	}
	return nil
}
