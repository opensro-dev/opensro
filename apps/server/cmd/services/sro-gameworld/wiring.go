/*
===========================================================================

wiring.go - GameWorld construction, service ownership and orderly shutdown

===========================================================================
*/
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
	"golang.org/x/sync/errgroup"
	"opensro.online/server/internal/agent/api"
	"opensro.online/server/internal/cluster/shard"
	"opensro.online/server/internal/data/store"
	"opensro.online/server/internal/game/action"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/game/world/worldarea"
	"opensro.online/server/internal/gamedata"
	"opensro.online/server/internal/platform/history"
	"opensro.online/server/internal/platform/readiness"
	"opensro.online/server/internal/security/auth"
	"opensro.online/server/internal/transport"
)

const (
	networkDrainTimeout = 15 * time.Second
	leaseReleaseTimeout = 3 * time.Second
	// bootFillLimit bounds how long admission waits for the population's boot
	// fill (about 13 s for the full world); past it players are admitted.
	bootFillLimit = 60 * time.Second

	envAgentURL        = "SRO_AGENT_URL"
	envRequireAgentURL = "SRO_AGENT_URL_REQUIRE"
	envAgentIdentity   = "SRO_AGENT_IDENTITY_FILE"
	envSessionKeys     = "SRO_AGENT_SESSION_PUBLIC_KEYS_PATH"
)

/*
================================================================================
GameWorld application ownership

The composition root owns every long-lived service. Construction publishes the
shard lease before opening durable authority and rolls back in reverse order.
Run cancels all background work on the first fatal error or process signal,
closes admission immediately, drains both listeners concurrently, joins every
writer, releases the lease, and closes durable authority last.
================================================================================
*/

type gameWorldApplication struct {
	history         *history.Journal
	skillCache      io.Closer
	itemCache       io.Closer
	populationCache io.Closer
	heightCache     io.Closer
	transport       *transport.Server
	authority       *store.Store
	controlAPI      *agentapi.API
	controlErrors   <-chan error
	reporter        *shard.Reporter
	ticker          *simulation.Ticker
	readiness       *readiness.Gate
	leaseOwned      bool
	// population and shardID let Run hold admission until the boot fill.
	population *simulation.MonsterState
	shardID    string
}

/*
================
newGameWorldApplication
================
*/
func newGameWorldApplication(
	startupContext context.Context,
	ts *transport.Server,
	ownedShard shard.Definition,
) (application *gameWorldApplication, resultErr error) {
	application = &gameWorldApplication{
		transport: ts,
		readiness: readiness.NewGate(),
	}
	ownedApplication := application
	defer func() {
		if resultErr != nil {
			ownedApplication.rollback()
		}
	}()

	dataPaths, err := gamedata.Resolve()
	if err != nil {
		return nil, fmt.Errorf("game data: %w", err)
	}
	log.Infof(
		"game data: bundle=%q manifest=%s",
		dataPaths.BundleRoot,
		dataPaths.ManifestDigest,
	)
	authoredAreas, err := worldarea.LoadAuthority(dataPaths.WorldAuthorityDir)
	if err != nil {
		return nil, fmt.Errorf("authored world areas: %w", err)
	}
	devPaths := enterworld.DevPathsFromEnv(
		dataPaths.CharacterAuthorityDir,
		dataPaths.TextdataDir,
	)
	devPaths.AuthoredAreas = authoredAreas
	devPaths.StructureZones = filepath.Join(dataPaths.WorldAuthorityDir, "structure-zones.json")
	characterRoster, err := enterworld.LoadRoster(devPaths.RosterPath)
	if err != nil {
		return nil, fmt.Errorf("character roster: %w", err)
	}

	agentURL, err := configuredAgentURL()
	if err != nil {
		return nil, err
	}
	application.reporter, err = shard.NewReporter(
		agentURL,
		os.Getenv(envAgentIdentity),
		ownedShard.ID,
		func() int { return ts.Hub.Population(ownedShard.ID) },
	)
	if err != nil {
		return nil, fmt.Errorf("shard heartbeat: %w", err)
	}
	if err := application.reporter.Acquire(startupContext, shard.DefaultAcquireTimeout); err != nil {
		return nil, fmt.Errorf(
			"acquiring shard %q lease before authority startup: %w",
			ownedShard.ID,
			err,
		)
	}
	application.leaseOwned = true

	accountIDs, err := application.reporter.AccountIDs(startupContext)
	if err != nil {
		return nil, fmt.Errorf("agent account directory: %w", err)
	}
	sessionVerifier, err := auth.NewAgentSessionVerifier(
		os.Getenv(envSessionKeys),
	)
	if err != nil {
		return nil, fmt.Errorf("agent session verifier: %w", err)
	}
	enterWorldSecret, err := auth.NewRandomSecret()
	if err != nil {
		return nil, fmt.Errorf("EnterWorld process key: %w", err)
	}
	authority, err := openAuthorityPlane(
		ts,
		ownedShard,
		accountIDs,
		application.readiness,
		devPaths,
		characterRoster,
		sessionVerifier,
		enterWorldSecret,
	)
	if err != nil {
		return nil, err
	}
	application.authority = authority.store
	application.controlAPI = authority.agentAPI
	journal, err := history.Open(filepath.Join(store.DirForShardFromEnv(ownedShard.ID), "history.sqlite"), "gameworld", ownedShard.ID, history.Build())
	if err != nil {
		return nil, fmt.Errorf("operator history: %w", err)
	}
	application.history = journal
	ts.Hub.History = historyObserver{journal}
	log.AddHook(journal)
	historyHandler, err := history.OperatorHandler(journal, filepath.Join(store.DirForShardFromEnv(ownedShard.ID), "operator-token"))
	if err != nil {
		return nil, err
	}
	authority.agentAPI.InstallHistory(historyHandler)
	if err := configureCatalogues(authority.textdata.Skills, authority.textdata.Items, boundedCataloguesFromEnv()); err != nil {
		return nil, err
	}
	application.skillCache = authority.textdata.Skills
	application.itemCache = authority.textdata.Items

	gameplay, err := newGameplayPlane(
		ts,
		authority.store,
		ownedShard,
		devPaths,
		characterRoster,
		dataPaths.WorldAuthorityDir,
		authoredAreas,
		authority.textdata,
	)
	if err != nil {
		return nil, fmt.Errorf("gameplay construction: %w", err)
	}
	gameplay.installSessionLifecycle(ts.Hub, authority.store)
	if gameplay.deps.MonsterState != nil {
		if err := gameplay.deps.MonsterState.EnableDormantStorage(); err != nil {
			return nil, fmt.Errorf("dormant monster storage: %w", err)
		}
		application.populationCache = gameplay.deps.MonsterState
		application.population = gameplay.deps.MonsterState
	}
	if err := gameplay.water.EnableBoundedHeightCache(2 << 20); err != nil {
		return nil, fmt.Errorf("terrain height cache: %w", err)
	}
	application.heightCache = gameplay.water
	// Built after gameplay construction: the static item rows name the
	// alchemy and Magic Pop catalogues it configures.
	references, err := enterworld.NewBrowserReferences(enterworld.BrowserReferenceSources{
		Skills:       gameplay.deps.Skills,
		ItemCommands: authority.textdata.Items,
		StaticItems:  enterworld.StaticRefItemRows(gameplay.deps),
		Monsters:     enterworld.PublicMonsterRefObjRows(gameplay.deps.MonsterState),
		HuntingGuide: gameplay.deps.MonsterState.HuntingGuide(),
	})
	if err != nil {
		return nil, fmt.Errorf("browser references: %w", err)
	}
	gameplay.deps.BrowserReferences = references
	ts.SetPublicReferences(references)
	if err := gameplay.register(ts.Hub, authority.loadQuests); err != nil {
		return nil, fmt.Errorf("gameplay wiring: %w", err)
	}
	if err := installEnterWorldVerifier(
		ts.Hub,
		ownedShard.ID,
		enterWorldSecret,
	); err != nil {
		return nil, err
	}
	if err := installTransportAdmissionVerifier(
		ts.Hub,
		ownedShard.ID,
		enterWorldSecret,
	); err != nil {
		return nil, err
	}

	controlAddr, err := controlListenAddress(ownedShard.ControlURL)
	if err != nil {
		return nil, err
	}
	peerReferences, err := gameplay.items.PreparePeerItemReferences(authority.textdata.Items.ItemCommandReferences())
	if err != nil {
		return nil, fmt.Errorf("peer item references: %w", err)
	}
	application.ticker = gameplay.newMissionTicker(peerReferences)
	configureReadiness(ts, authority.store, application.readiness, application.ticker)
	gameplay.installFollowFixture(authority.agentAPI, application.ticker)
	authority.agentAPI.InstallPassiveCriticalFixture(gameplay.passiveCriticalReader())
	installMonsterQuery(authority.agentAPI, gameplay.deps.MonsterState, ownedShard.ID)
	installObservatory(authority.agentAPI, gameplay.deps.MonsterState, ts.Hub, authority.store, ownedShard.ID)
	installOperatorNotices(authority.agentAPI, ts.Hub, ownedShard.ID)
	// GM and operator silk grants (operator tooling): the account wallet, and
	// one history event per committed grant.
	gameplay.items.SilkWallet = authority.store
	gameplay.items.RecordSilkGrant = func(grant action.SilkGrant) { journal.Record(silkGrantEvent(grant)) }
	if err := installPlayerOperations(authority.agentAPI, gameplay, ts.Hub, authority.store, ownedShard.ID); err != nil {
		return nil, fmt.Errorf("player operations: %w", err)
	}
	application.controlErrors, err = authority.agentAPI.Start(controlAddr)
	if err != nil {
		return nil, fmt.Errorf("GameWorld control API: %w", err)
	}
	application.shardID = ownedShard.ID
	// The listener also serves the liveness check (/transport/healthz), so it
	// starts now; only readiness, and with it admission, waits in Run.
	if err := ts.Start(); err != nil {
		return nil, fmt.Errorf("transport: %w", err)
	}

	if err := startupContext.Err(); err != nil {
		return nil, fmt.Errorf("startup cancelled: %w", err)
	}
	return application, nil
}

/*
================
configuredAgentURL
================
*/
func configuredAgentURL() (string, error) {
	agentURL := strings.TrimSpace(os.Getenv(envAgentURL))
	if agentURL != "" {
		return agentURL, nil
	}
	if os.Getenv(envRequireAgentURL) == "1" {
		return "", fmt.Errorf(
			"%s is required for this GameWorld process",
			envAgentURL,
		)
	}
	return agentapi.DefaultAgentBaseURL, nil
}

/*
================
admit

Opens readiness once the monster population's boot fill has settled. The
first population passes place every nest's monsters (tens of thousands of
spawns, each with ground placement), and those ticks run for hundreds of
milliseconds. Readiness gates admission-token minting and readyz, so no
new session starts into those ticks; the transport listener is already up
because it also answers the liveness check. INFERENCE: the native
GameServer finishes loading its worlds before it accepts clients; this is
the port's equivalent boundary. bootFillLimit is liveness only, well under
the job's healthy_deadline: a fill that never settles opens with a warning.
================
*/
func (application *gameWorldApplication) admit(ctx context.Context) error {
	started := time.Now()
	settled := func() bool {
		population := application.population
		return population == nil || population.PopulationSettled(application.shardID)
	}
	open := func(settled bool) {
		application.readiness.Open()
		// The title and login follow admission, not the lease alone: a
		// leased but loading shard showed online with an empty roster.
		if application.reporter != nil {
			application.reporter.MarkAdmitting()
		}
		fill := "population settled"
		if !settled {
			fill = "population NOT settled; admitted at the bound"
		}
		log.Infof(
			"shard: GameWorld %q owns state and transport under its Agent lease (%s after %s)",
			application.shardID,
			fill,
			time.Since(started).Round(time.Millisecond),
		)
	}
	admitWhenSettled(ctx, admission{
		settled: settled,
		open:    open,
		poll:    simulation.DefaultTickInterval,
		limit:   bootFillLimit,
	})
	return nil
}

/*
================
admission

What admitWhenSettled needs, as plain functions so the order is testable.
================
*/
type admission struct {
	settled     func() bool
	open        func(settled bool)
	poll, limit time.Duration
}

/*
================
admitWhenSettled

Waits for the boot fill (or its bound), then opens readiness and says
which it was. A run cancelled before that never opens: Run is about to
drain.
================
*/
func admitWhenSettled(ctx context.Context, a admission) {
	settled := awaitBootFill(ctx, a.settled, a.poll, a.limit)
	if !settled && ctx.Err() == nil {
		log.Warnf("shard: monster population still filling after %s; admitting players anyway", a.limit)
	}
	select {
	case <-ctx.Done():
		return
	default:
	}
	a.open(settled)
}

/*
================
awaitBootFill

Polls settled every poll until it holds (true), the limit passes or ctx
ends (false).
================
*/
func awaitBootFill(ctx context.Context, settled func() bool, poll, limit time.Duration) bool {
	deadline := time.NewTimer(limit)
	defer deadline.Stop()
	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	for !settled() {
		select {
		case <-ctx.Done():
			return false
		case <-deadline.C:
			return false
		case <-ticker.C:
		}
	}
	return true
}

/*
================
Run
================
*/
func (application *gameWorldApplication) Run(ctx context.Context) error {
	runContext, cancelRun := context.WithCancel(ctx)
	group, groupContext := errgroup.WithContext(runContext)

	group.Go(func() error {
		if err := application.reporter.Run(groupContext); err != nil {
			return fmt.Errorf("renewing Agent shard lease: %w", err)
		}
		return nil
	})
	group.Go(func() error {
		application.ticker.Run(groupContext)
		return nil
	})
	// Shutdown joins admission before it closes readiness and drains, so a
	// late admit can never start the transport behind the drain.
	admitted := make(chan struct{})
	group.Go(func() error {
		defer close(admitted)
		return application.admit(groupContext)
	})
	group.Go(func() error {
		return waitForServeError(
			groupContext,
			"GameWorld control API",
			application.controlErrors,
		)
	})
	group.Go(func() error {
		return waitForServeError(
			groupContext,
			"game transport",
			application.transport.Err(),
		)
	})

	<-groupContext.Done()
	cancelRun()
	<-admitted
	application.readiness.Close()

	drainErr := application.drainNetwork()
	runErr := group.Wait()
	for _, cache := range []io.Closer{application.skillCache, application.itemCache} {
		if cache != nil {
			drainErr = errors.Join(drainErr, cache.Close())
		}
	}
	if application.populationCache != nil {
		drainErr = errors.Join(drainErr, application.populationCache.Close())
	}
	if application.heightCache != nil {
		drainErr = errors.Join(drainErr, application.heightCache.Close())
	}
	application.releaseLease()
	if application.authority != nil {
		log.Info("shutdown: closing the authority store")
		application.authority.Close()
	}
	application.leaseOwned = false
	log.Info("shutdown: complete")
	if application.history != nil {
		drainErr = errors.Join(drainErr, application.history.Close())
	}

	return errors.Join(runErr, drainErr)
}

/*
================
waitForServeError
================
*/
func waitForServeError(
	ctx context.Context,
	name string,
	errors <-chan error,
) error {
	select {
	case <-ctx.Done():
		return nil
	case err, open := <-errors:
		if ctx.Err() != nil {
			return nil //nolint:nilerr // shutdown already began; errors during it are expected
		}
		if !open || err == nil {
			return fmt.Errorf("%s stopped unexpectedly", name)
		}
		return fmt.Errorf("%s stopped unexpectedly: %w", name, err)
	}
}

/*
================
drainNetwork
================
*/
func (application *gameWorldApplication) drainNetwork() error {
	application.readiness.Close()
	log.Info("shutdown: draining the game transport and control API")

	ctx, cancel := context.WithTimeout(
		context.Background(),
		networkDrainTimeout,
	)
	defer cancel()
	var drains errgroup.Group
	if application.transport != nil {
		drains.Go(func() error {
			if err := application.transport.Shutdown(ctx); err != nil {
				return fmt.Errorf("transport drain: %w", err)
			}
			return nil
		})
	}
	if application.controlAPI != nil {
		drains.Go(func() error {
			if err := application.controlAPI.Shutdown(ctx); err != nil {
				return fmt.Errorf("control API drain: %w", err)
			}
			return nil
		})
	}
	return drains.Wait()
}

/*
================
releaseLease
================
*/
func (application *gameWorldApplication) releaseLease() {
	if !application.leaseOwned || application.reporter == nil {
		return
	}
	ctx, cancel := context.WithTimeout(
		context.Background(),
		leaseReleaseTimeout,
	)
	defer cancel()
	if err := application.reporter.Release(ctx); err != nil {
		log.Warnf("shutdown: releasing shard lease: %v", err)
	}
}

/*
================
rollback
================
*/
func (application *gameWorldApplication) rollback() {
	application.readiness.Close()
	if err := application.drainNetwork(); err != nil &&
		!errors.Is(err, http.ErrServerClosed) {
		log.Warnf("startup rollback: %v", err)
	}
	for _, cache := range []io.Closer{application.skillCache, application.itemCache} {
		if cache != nil {
			if err := cache.Close(); err != nil {
				log.Warnf("startup rollback reference cache: %v", err)
			}
		}
	}
	if application.populationCache != nil {
		if err := application.populationCache.Close(); err != nil {
			log.Warnf("startup rollback population cache: %v", err)
		}
	}
	if application.heightCache != nil {
		if err := application.heightCache.Close(); err != nil {
			log.Warnf("startup rollback height cache: %v", err)
		}
	}
	if application.authority != nil {
		application.authority.Close()
	}
	application.releaseLease()
	application.leaseOwned = false
	if application.history != nil {
		_ = application.history.Close()
	}
}

// boundedCatalogueEnv opts a small host into the disk-backed skill and item
// catalogues. Off (the default) keeps the loader's decoded rows resident:
// a bounded miss decodes JSON under one mutex (~96 us and 19 KB per lookup,
// measured 2026-10-11), and the tick looks skills up for every learned
// skill of every player, which overran production's 100 ms budget.
// Resident rows cost ~80 MiB (51.6 skills + 27.5 items). Not gameplay.
const boundedCatalogueEnv = "SRO_BOUNDED_CATALOGUE"

// boundedCatalogueRows is the resident window of each bounded catalogue.
const boundedCatalogueRows = 512

/*
================
boundedCataloguesFromEnv
================
*/
func boundedCataloguesFromEnv() bool {
	return strings.TrimSpace(os.Getenv(boundedCatalogueEnv)) == "1"
}

// boundableCatalogue is the one capability configureCatalogues needs.
type boundableCatalogue interface {
	UseBoundedCache(capacity int) error
}

/*
================
configureCatalogues

Chooses where the immutable skill and item rows live. Bounded swaps each
loader's resident rows for a disk-backed LRU; resident leaves them as Load
built them, which is also the boot path with less work (no re-encode).
================
*/
func configureCatalogues(skills, items boundableCatalogue, bounded bool) error {
	if !bounded {
		return nil
	}
	if err := skills.UseBoundedCache(boundedCatalogueRows); err != nil {
		return fmt.Errorf("skill cache: %w", err)
	}
	if err := items.UseBoundedCache(boundedCatalogueRows); err != nil {
		return fmt.Errorf("item cache: %w", err)
	}
	return nil
}

/*
================
loadOwnedShard
================
*/
func loadOwnedShard() (shard.Definition, error) {
	catalog, catalogPath, err := shard.LoadFromEnv()
	if err != nil {
		return shard.Definition{}, fmt.Errorf("shard catalog %s: %w", catalogPath, err)
	}
	shardID := strings.TrimSpace(os.Getenv("SRO_SHARD_ID"))
	if shardID == "" {
		return shard.Definition{}, fmt.Errorf("SRO_SHARD_ID is required for a GameWorld process")
	}
	ownedShard, ok := catalog.Resolve(shardID)
	if !ok || ownedShard.ID != shardID {
		return shard.Definition{}, fmt.Errorf(
			"GameWorld shard %q is absent from %s",
			shardID,
			catalogPath,
		)
	}
	if !ownedShard.Enabled {
		return shard.Definition{}, fmt.Errorf("GameWorld shard %q is disabled", shardID)
	}
	return ownedShard, nil
}

/*
================
controlListenAddress
================
*/
func controlListenAddress(rawURL string) (string, error) {
	if override := strings.TrimSpace(os.Getenv("SRO_GAMEWORLD_CONTROL_ADDR")); override != "" {
		if _, _, err := net.SplitHostPort(override); err != nil {
			return "", fmt.Errorf(
				"SRO_GAMEWORLD_CONTROL_ADDR %q must be host:port: %w",
				override,
				err,
			)
		}
		return override, nil
	}
	endpoint, err := url.Parse(rawURL)
	if err != nil || endpoint.Scheme != "http" || endpoint.Host == "" {
		return "", fmt.Errorf("GameWorld control URL %q must be an absolute http URL", rawURL)
	}
	if endpoint.Path != "" && endpoint.Path != "/" {
		return "", fmt.Errorf("GameWorld control URL %q must not contain a path", rawURL)
	}
	return endpoint.Host, nil
}
