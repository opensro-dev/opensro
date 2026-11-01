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
	"opensro.online/server/internal/game/action"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/loot"
	"opensro.online/server/internal/game/progression"
	"opensro.online/server/internal/game/quest"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
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

/*
================
questDefinitionLoader
================
*/
type questDefinitionLoader func() (*quest.Definitions, error)

/*
================
authorityPlane
================
*/
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
	if err := loot.InitCap110(textdataDir, monster.Cap110Enabled()); err != nil {
		return authorityPlane{}, err
	}
	textdata.Items.ApplyAttackPetExpansion()
	// Port-only, not native: raise configured stack caps before any owner
	// reads a reference row. Unset keeps every itemdata cap.
	stackSizes, err := enterworld.StackSizesFromEnv()
	if err != nil {
		return authorityPlane{}, err
	}
	raised, err := textdata.Items.ApplyStackSizes(stackSizes)
	if err != nil {
		return authorityPlane{}, err
	}
	if len(stackSizes) > 0 {
		log.Infof("items: %s=%s raised %d item cap(s) (port-only, not native)", enterworld.EnvStackSizes, stackSizes, raised)
	}
	// Port-only, not native: trade goods are worth TradeGoodsPriceFactor times
	// their v1.150 price (SRO_TRADE_TIERS, enterworld/tradegoodsprice.go).
	if action.TradeTiersFromEnv() {
		scaled, err := textdata.Items.ApplyTradeGoodsPriceFactor(enterworld.TradeGoodsPriceFactor)
		if err != nil {
			return authorityPlane{}, err
		}
		log.Infof("trade: %s priced %d trade goods x%d (port-only, not native)", action.EnvTradeTiers, scaled, enterworld.TradeGoodsPriceFactor)
	}
	// Port-only, not native: the Item Mall transports move 1.5 times as fast.
	if enterworld.MallTransportSpeedFromEnv() {
		scaled, err := textdata.Items.ApplyMallTransportSpeed()
		if err != nil {
			return authorityPlane{}, err
		}
		log.Infof("cos: %s scaled %d mall transport(s) (port-only, not native)", enterworld.EnvMallTransportSpeed, scaled)
	}
	// Port-only, not native: the Item Mall vehicles and pets move faster
	// (1.3 times by default, game/tunables).
	rideFactor, rideOn, err := enterworld.MallRideSpeedFromEnv()
	if err != nil {
		return authorityPlane{}, err
	}
	if rideOn {
		scaled, err := textdata.Items.ApplyMallRideSpeed(rideFactor)
		if err != nil {
			return authorityPlane{}, err
		}
		log.Infof("cos: %s=%v scaled %d mall vehicle and pet row(s) (port-only, not native)",
			enterworld.EnvMallRideSpeed, rideFactor, scaled)
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

	levelCap, err := progression.LevelCapFromEnv()
	if err != nil {
		authorityStore.Close()
		return authorityPlane{}, err
	}
	// The published skill tier and the level cap are separate switches
	// (the build's skill import and database snapshot publish the rows;
	// SRO_LEVEL_CAP admits the levels). A table whose top tier outruns
	// the cap leaves those ranks board-visible but unlearnable, answered
	// only by the generic mastery refusal, so say so once at startup.
	if highest := textdata.Skills.MaxMasteryRequirement(); highest > 100 && highest > levelCap {
		log.Warnf("skills: loaded table requires mastery %d but %s caps progression at %d; "+
			"the top ranks will show on the board and refuse learning (raise the cap or rebuild without the database tier)",
			highest, progression.EnvLevelCap, levelCap)
	}
	config := agentapi.Config{
		Store:                 authorityStore,
		CharacterPresentation: characterPresentationProjector(characterRoster, textdata.Levels),
		CharacterCreationValid: func(character *domain.Character) bool {
			return enterworld.CharacterCreationValid(character, characterRoster)
		},
		CharacterNameAllowed:    textdata.NameFilter.Allowed,
		ShardID:                 ownedShard.ID,
		AgentSessionVerifier:    sessionVerifier,
		PrivateNetwork:          os.Getenv("SRO_GAMEWORLD_PRIVATE_NETWORK") == "1",
		Readiness:               ready,
		EnterWorldAuthSecret:    enterWorldSecret,
		MarksDir:                os.Getenv(agentapi.EnvMarksDir),
		MaintenanceGatePath:     strings.TrimSpace(os.Getenv(agentapi.EnvMaintenanceGate)),
		MaintenanceMessage:      os.Getenv(agentapi.EnvMaintenanceMessage) == "1",
		AuthoredAreas:           devPaths.AuthoredAreas,
		BenchmarkFixtureControl: os.Getenv(agentapi.EnvBenchmarkFixtureControl) == "1",
		LevelCap:                levelCap,
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
	deps.MutateCharacterDeferred = func(character *enterworld.Character, label string, mutate func()) {
		if character != nil {
			label += " " + character.Name
		}
		authorityStore.MutateCharacterDeferred(character, label, mutate)
	}
	deps.MutateCharactersDeferred = authorityStore.MutateCharactersDeferred
	deps.UpdateCharacterDeferred = func(character *enterworld.Character, label string, update func() bool) bool {
		if character != nil {
			label += " " + character.Name
		}
		return authorityStore.UpdateCharacterDeferred(character, label, update)
	}
	deps.UpdateCharactersDeferred = authorityStore.UpdateCharactersDeferred
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
readinessProgress

What readiness asks of the simulation: nil while ticks keep finishing
(simulation.Ticker.Progress).
================
*/
type readinessProgress interface {
	Progress(now time.Time) error
}

/*
================
configureReadiness

Installs readyCheck as the transport's readiness probe. Liveness
(/transport/healthz) stays independent of it.
================
*/
func configureReadiness(
	ts *transport.Server,
	authorityStore *store.Store,
	ready *readiness.Gate,
	progress readinessProgress,
) {
	ts.SetReadyCheck(readyCheck(authorityStore, ready, progress))
}

/*
================
tickCommit

The store's batch as the ticker drives it: one transaction per tick for the
tick-only doors (store tickbatch.go). A failed flush is already loud: the
store logs PERSIST FAILING and readiness fails on its health.
================
*/
func tickCommit(authorityStore *store.Store) simulation.TickCommit {
	return simulation.TickCommit{
		Begin: authorityStore.BeginTickBatch,
		Flush: func() simulation.TickCommitStats {
			stats := authorityStore.FlushTickBatch()
			return simulation.TickCommitStats{Deferred: stats.Deferred, Rows: stats.Rows, Elapsed: stats.Duration,
				Flushed: stats.Flushed, Failed: stats.Err != nil}
		},
	}
}

/*
================
readyCheck

The probe answers from atomics only: the process gate, the tick progress
and the store's published health. It never takes the store or gameplay
locks, so a stalled tick or a writer queued behind a reader yields a prompt
not-ready instead of a probe that waits with them (#570).
================
*/
func readyCheck(authorityStore *store.Store, ready *readiness.Gate, progress readinessProgress) func() error {
	return func() error {
		if !ready.Ready() {
			return fmt.Errorf("process is not accepting traffic")
		}
		if err := progress.Progress(time.Now()); err != nil {
			return err
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
	}
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
