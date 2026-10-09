/*
===========================================================================

wiring_authority.go - wiring authority

===========================================================================
*/
package main

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/agent/api"
	"opensro.online/server/internal/cluster/shard"
	"opensro.online/server/internal/data/store"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/progression"
	"opensro.online/server/internal/game/quest"
	"opensro.online/server/internal/platform/readiness"
	"opensro.online/server/internal/security/auth"
	"opensro.online/server/internal/transport"
)

const storeReadyFailedWritesFloor = 3

/*
================================================================================
Authority composition

The store is the sole durable gameplay authority. The agent HTTP surface and
every game lane receive that same authority before network traffic starts.
================================================================================
*/

type questDefinitionLoader func() (*quest.Definitions, error)

type authorityPlane struct {
	store      *store.Store
	agentAPI   *agentapi.API
	loadQuests questDefinitionLoader
	textdata   *enterworld.TextdataCatalogs
}

/*
================
characterPresentationProjector
================
*/
func characterPresentationProjector(
	roster *enterworld.Roster,
	levels enterworld.LevelDataSource,
) agentapi.CharacterPresentationProjector {
	return func(character *domain.Character) agentapi.CharacterPresentation {
		entry := enterworld.ResolveLocalPlayerEntry(character, roster)
		loadout := entry.VisualLoadout
		raceIndex := domain.RaceEurope
		if entry.RaceKey == enterworld.RaceKeyChina {
			raceIndex = domain.RaceChina
		}
		gender := domain.GenderFemale
		if entry.SexSelector1AC != 0 {
			gender = domain.GenderMale
		}
		return agentapi.CharacterPresentation{
			ExperiencePercent: enterworld.CharacterExperiencePercent(character, levels),
			RaceIndex:         raceIndex,
			Gender:            gender,
			VisualLoadout: agentapi.CharacterVisualLoadout{
				ModelCodename:    loadout.ModelCodename,
				Items:            characterItems(loadout.Items),
				Avatars:          characterItems(loadout.Avatars),
				AnimationSetName: loadout.AnimationSetName,
				HeightScale:      loadout.HeightScale,
				VolumeScale:      loadout.VolumeScale,
			},
		}
	}
}

/*
================
characterItems
================
*/
func characterItems(items []enterworld.VisualItem) []agentapi.CharacterItem {
	out := make([]agentapi.CharacterItem, 0, len(items))
	for _, item := range items {
		out = append(out, agentapi.CharacterItem{RefObjID: item.RefObjID, Plus: item.Plus})
	}
	return out
}

/*
================
openAuthorityPlane
================
*/
func openAuthorityPlane(
	ts *transport.Server,
	ownedShard shard.Definition,
	accountIDs []string,
	ready *readiness.Gate,
	devPaths enterworld.DevPaths,
	characterRoster *enterworld.Roster,
	sessionVerifier *auth.AgentSessionVerifier,
	enterWorldSecret []byte,
) (authorityPlane, error) {
	textdataDir := devPaths.TextdataDir
	textdata, err := enterworld.LoadTextdataCatalogs(textdataDir)
	if err != nil {
		return authorityPlane{}, err
	}
	options := store.OptionsFromEnv()
	options.DefaultSkills = enterworld.DefaultSkillSeeder(textdata.Skills)
	options.DefaultInventory = enterworld.StarterInventorySeeder(
		characterRoster,
		textdata.Items,
		devPaths.EquipItemsEnabled,
	)

	questCatalog := quest.NewCatalog(textdataDir)
	loadQuests := sync.OnceValues(func() (*quest.Definitions, error) {
		return quest.LoadDefinitions(questCatalog, enterworld.NewTextdataItems(questCatalog.Dir()))
	})
	options.DefaultQuests = func(raceKey string) ([]enterworld.ActiveQuestRecord, error) {
		definitions, err := loadQuests()
		if err != nil {
			return nil, err
		}
		return quest.DefaultQuestSeeder(definitions)(raceKey)
	}

	authorityStore, err := store.Open(store.DirForShardFromEnv(ownedShard.ID), options)
	if err != nil {
		return authorityPlane{}, fmt.Errorf("store: %w", err)
	}
	authorityStore.ReapMaturedDeletions()
	if err := authorityStore.ValidateShardState([]string{ownedShard.ID}); err != nil {
		authorityStore.Close()
		return authorityPlane{}, fmt.Errorf("shard %q authority: %w", ownedShard.ID, err)
	}
	if err := authorityStore.ValidateCharacterOwners(accountIDs); err != nil {
		authorityStore.Close()
		return authorityPlane{}, fmt.Errorf(
			"shard %q character ownership: %w",
			ownedShard.ID,
			err,
		)
	}
	gmIdentities, err := store.GMCharactersFromEnv()
	if err != nil {
		authorityStore.Close()
		return authorityPlane{}, fmt.Errorf("GM allowlist: %w", err)
	}
	authorityStore.ReconcileGMPrivilege(gmIdentities)

	config := agentapi.Config{
		Store:                 authorityStore,
		CharacterPresentation: characterPresentationProjector(characterRoster, textdata.Levels),
		CharacterCreationValid: func(character *domain.Character) bool {
			return enterworld.CharacterCreationValid(character, characterRoster)
		},
		ShardID:                 ownedShard.ID,
		AgentSessionVerifier:    sessionVerifier,
		PrivateNetwork:          os.Getenv("SRO_GAMEWORLD_PRIVATE_NETWORK") == "1",
		Readiness:               ready,
		EnterWorldAuthSecret:    enterWorldSecret,
		MarksDir:                os.Getenv(agentapi.EnvMarksDir),
		MaintenanceGatePath:     strings.TrimSpace(os.Getenv(agentapi.EnvMaintenanceGate)),
		AuthoredAreas:           devPaths.AuthoredAreas,
		BenchmarkFixtureControl: os.Getenv(agentapi.EnvBenchmarkFixtureControl) == "1",
		LevelCap:                progression.LevelCap,
		SkillGroup: func(id uint32) (uint32, bool) {
			row, ok := textdata.Skills.SkillByID(id)
			return row.Group, ok
		},
	}
	config.CharacterInPlay = func(divisionID, characterName string) bool {
		key := divisionID + ":" + strings.ToLower(characterName)
		_, bound := ts.Hub.BoundSession(key)
		return bound
	}
	config.AcquireCharacterControl = func(
		divisionID string,
		characterName string,
	) (func(), bool) {
		key := divisionID + ":" + strings.ToLower(characterName)
		lease, acquired := ts.Hub.AcquireBindingControl(key)
		if !acquired {
			return nil, false
		}
		return lease.Release, true
	}
	api, err := agentapi.New(config)
	if err != nil {
		authorityStore.Close()
		return authorityPlane{}, fmt.Errorf("agentapi: %w", err)
	}

	return authorityPlane{
		store:      authorityStore,
		agentAPI:   api,
		loadQuests: loadQuests,
		textdata:   textdata,
	}, nil
}

/*
================
newBootstrapDependencies
================
*/
func newBootstrapDependencies(
	authorityStore *store.Store,
	paths enterworld.DevPaths,
	characterRoster *enterworld.Roster,
	ownedShard shard.Definition,
	textdata *enterworld.TextdataCatalogs,
) (*enterworld.Deps, error) {
	deps, err := enterworld.NewDevDepsWithRoster(paths, textdata, characterRoster)
	if err != nil {
		return nil, err
	}
	deps.Characters = authorityStore.Characters()
	deps.ResolveDivisionID = func(requested string) string {
		if requested == ownedShard.ID {
			return ownedShard.ID
		}
		return ""
	}
	deps.MutateCharacter = func(character *enterworld.Character, label string, mutate func()) {
		if character != nil {
			label += " " + character.Name
		}
		authorityStore.MutateCharacter(character, label, mutate)
	}
	deps.MutateCharacters = func(characters []*enterworld.Character, label string, mutate func()) {
		authorityStore.MutateCharacters(characters, label, mutate)
	}
	deps.UpdateCharacter = func(character *enterworld.Character, label string, update func() bool) bool {
		if character != nil {
			label += " " + character.Name
		}
		return authorityStore.UpdateCharacter(character, label, update)
	}
	deps.UpdateCharacters = authorityStore.UpdateCharacters
	deps.UpdateTrade = authorityStore.UpdateTrade
	deps.CloseJobWeek = authorityStore.CloseJobWeek
	deps.JobRankings = authorityStore.JobRankings
	deps.ReadCharacter = func(divisionID string, read func()) {
		authorityStore.ReadState(read)
	}
	deps.CanEnterWorldRegion = func(character *enterworld.Character, regionID uint16) bool {
		return paths.AuthoredAreas.CanEnterRegion(regionID, character != nil && character.GMPrivilege)
	}
	deps.Letters = authorityStore.Letters()
	deps.Guilds = authorityStore.Guilds()
	deps.GuildWars = authorityStore.GuildWars()
	deps.TrainingCamps = authorityStore.TrainingCamps()
	deps.Fortresses = authorityStore.Fortresses()
	deps.Alliances = authorityStore.Alliances()
	return deps, nil
}

/*
================
configureStoreReadiness
================
*/
func configureStoreReadiness(
	ts *transport.Server,
	authorityStore *store.Store,
	ready *readiness.Gate,
) {
	ts.SetReadyCheck(func() error {
		if !ready.Ready() {
			return fmt.Errorf("process is not accepting traffic")
		}
		health := authorityStore.Health()
		if health.FailedWrites < storeReadyFailedWritesFloor {
			return nil
		}
		return fmt.Errorf(
			"authority store persist failing: %d consecutive failed write(s) since %s: %s",
			health.FailedWrites,
			health.FailingSince.Format(time.RFC3339),
			health.LastError,
		)
	})
}

/*
================
logAuthorityReady
================
*/
func logAuthorityReady(authorityStore *store.Store, deps *enterworld.Deps) {
	characterCount := 0
	divisionIDs := authorityStore.DivisionIDs()
	for _, divisionID := range divisionIDs {
		characterCount += len(deps.CharactersForDivision(divisionID))
	}

	ground := authorityStore.GroundSnapshotForRestore()
	meta := authorityStore.MetaView()
	log.Infof(
		"store: authority ready: %d character(s) across %d division(s), %d ground item(s), gidCounter=%d",
		characterCount,
		len(divisionIDs),
		ground.ItemCount(),
		meta.GidCounter,
	)
}
