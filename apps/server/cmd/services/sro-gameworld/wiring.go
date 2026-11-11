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
	"opensro.online/server/internal/game/moderation"
	"opensro.online/server/internal/game/social/chatlog"
	"opensro.online/server/internal/game/tunables"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/platform/census"
	"opensro.online/server/internal/platform/connjournal"
	"opensro.online/server/internal/platform/economy"
	"opensro.online/server/internal/platform/history"
	"opensro.online/server/internal/platform/processguard"
	"opensro.online/server/internal/platform/readiness"
	"opensro.online/server/internal/platform/sealboard"
	"opensro.online/server/internal/platform/settingsapi"
	"opensro.online/server/internal/security/auth"
	"opensro.online/server/internal/transport"
)

const (
	networkDrainTimeout = 15 * time.Second
	leaseReleaseTimeout = 3 * time.Second
	// shutdownBudget is the named ceiling for the whole shutdown tail
	// (#3186): a release update may cost the world two minutes of
	// downtime, and the tail must fit well inside that. The final
	// shutdown log line names the budget and warns when the tail goes
	// over it.
	shutdownBudget = 30 * time.Second
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
	history *history.Journal
	// connJournal is the connection census (port-only, #2716); nil off.
	connJournal *connjournal.Journal
	// economy is the staff economy view's aggregates (port-only); nil off.
	economy *economy.Service
	// sealBoard is the weekly seal-drop counter (port-only); nil off.
	sealBoard *sealboard.Service
	// census is the player census over the connection journal
	// (port-only, #2717); nil while the journal is off.
	census *census.Service
	// mutes is the staff chat mute book (#2671): opened before admission,
	// reaped on a timer.
	mutes *moderation.Book
	// certificateWatch is the served WebTransport pair's staff alert
	// (ops only, #1447); it reports nothing outside the public posture.
	certificateWatch *transport.CertificateWatch
	// killStatsFlush writes the final kill-stat rollup at shutdown
	// (port-only, #1639); nil while SRO_KILL_STATS is off.
	killStatsFlush  func() error
	settings        *settingsapi.SQLiteStore
	skillCache      io.Closer
	itemCache       io.Closer
	populationCache io.Closer
	transport       *transport.Server
	authority       *store.Store
	controlAPI      *agentapi.API
	publicAPI       *publicAPI
	controlErrors   <-chan error
	reporter        *shard.Reporter
	ticker          *simulation.Ticker
	// returnArrivals is the action runtime's return lane
	// (RunReturnArrivals): it completes the return-scroll arrivals
	// the tick hands off, beside the ticker in Run.
	returnArrivals func(context.Context)
	readiness      *readiness.Gate
	leaseOwned     bool
	// guard is the shard process guard when this construction won it
	// (the standby handover's probe or wait, opensro-ops#281); nil on a
	// boot whose caller holds the guard. Closed by rollback, or at the
	// end of Run's shutdown tail after the authority store.
	guard *processguard.Guard
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
	boot standbyBoot,
) (application *gameWorldApplication, resultErr error) {
	// Canary standby handover (opensro-ops#281, port-only, not native):
	// armed, the standby-health listener comes up before anything else
	// and the guard probe decides whether this process is the shard's
	// writer or its loaded standby. Unarmed, none of this runs.
	standbyWaiting := false
	var standbyGuard *processguard.Guard
	if boot.armed {
		var err error
		standbyGuard, standbyWaiting, err = probeStandbyGuard(startupContext, ts, ownedShard, boot)
		if err != nil {
			return nil, err
		}
	}
	// The liveness listener comes first (#3186): /transport/healthz
	// answers from process start, before any of the load below, and
	// names the boot phase it is in. Game routes and the WebTransport
	// listener stay closed until Start at the end of construction -
	// readiness still belongs to the gate. A standby skips the public
	// listener here: its port stays unbound until the takeover (Start
	// binds it then), and the standby-health listener answers liveness
	// meanwhile.
	if !standbyWaiting {
		if err := ts.StartHealth(); err != nil {
			return nil, fmt.Errorf("transport health listener: %w", err)
		}
	}
	application = &gameWorldApplication{
		transport: ts,
		readiness: readiness.NewGate(),
		guard:     standbyGuard,
	}
	ownedApplication := application
	defer func() {
		if resultErr != nil {
			ownedApplication.rollback()
		}
	}()

	// Port-only tunables (game/tunables): a value the registry refuses
	// stops the boot here, before anything reads it.
	if err := tunables.Load(); err != nil {
		return nil, fmt.Errorf("tunables: %w", err)
	}
	// Canary standby (opensro-ops#281): armed, the save-free load and
	// any guard wait happen here, ahead of everything that needs the
	// save (the settings store first of them, below).
	var inputs saveFreeInputs
	var loaded authorityTextdata
	if boot.armed {
		var guard *processguard.Guard
		var err error
		inputs, loaded, guard, err = loadStandbyInputs(startupContext, ts, ownedShard, boot, standbyWaiting)
		if err != nil {
			return nil, err
		}
		if guard != nil {
			application.guard = guard
		}
	}
	// Port-only: the staff console's approved overrides (settings.db, next
	// to the shard's other operator stores) lie over the environment.
	settingsStore, err := settingsapi.OpenSQLiteStore(filepath.Join(store.DirForShardFromEnv(ownedShard.ID), settingsapi.FileName))
	if err != nil {
		return nil, fmt.Errorf("settings store: %w", err)
	}
	application.settings = settingsStore
	overrides, err := settingsStore.Overrides()
	if err != nil {
		return nil, fmt.Errorf("settings store: %w", err)
	}
	rejected, err := tunables.InstallBoot(overrides)
	if err != nil {
		return nil, fmt.Errorf("tunables: %w", err)
	}
	for key, cause := range rejected {
		// A stored value this build no longer accepts: the key boots on the
		// environment or its default, the row is dropped and the audit says
		// why, once (the next boot no longer finds it).
		log.WithError(cause).WithField("key", key).Warn("tunables: stored override refused at boot; using the environment")
		if err := settingsStore.DropOverride(key, settingsapi.AuditEntry{At: time.Now().UnixMilli(), Who: "gameworld",
			Name: "boot", Action: settingsapi.ActionBoot, Key: key, To: overrides[key], Result: settingsapi.ResultRefused,
			Error: cause.Error()}); err != nil {
			return nil, fmt.Errorf("settings store: %w", err)
		}
	}
	if len(overrides) > 0 {
		log.Infof("tunables: %d stored override(s) from %s", len(overrides), settingsapi.FileName)
	}
	// Diagnostics only: the tick-overrun spike journal (transport
	// tick_spikes.go) appends one JSON line per burst next to the shard's
	// other operator stores. A failure here never stops the boot.
	if transport.TickSpikeJournalEnabled() {
		spikePath := filepath.Join(store.DirForShardFromEnv(ownedShard.ID), transport.TickSpikeFileName)
		if err := ts.Hub.EnableTickSpikeJournal(spikePath); err != nil {
			log.WithError(err).WithField("path", spikePath).Warn("tick spike journal: running without it")
		} else {
			log.WithField("path", spikePath).Info("tick spike journal: appending burst records")
		}
	}
	// Diagnostics only, and off unless SRO_TICK_TRACE arms it: the
	// slow-tick Perfetto traces (transport tick_trace.go, #2301)
	// rotate beside the journal. A failure here never stops the boot.
	if transport.TickTraceEnabled() {
		traceDir := store.DirForShardFromEnv(ownedShard.ID)
		if err := ts.Hub.EnableTickTrace(traceDir); err != nil {
			log.WithError(err).WithField("dir", traceDir).Warn("tick trace: running without it")
		} else {
			log.WithField("dir", traceDir).Info("tick trace: armed, one Perfetto file per slow tick")
		}
	}
	if !boot.armed {
		var err error
		inputs, err = loadSaveFreeInputs()
		if err != nil {
			return nil, err
		}
	}
	dataPaths, devPaths := inputs.dataPaths, inputs.devPaths
	authoredAreas, characterRoster := inputs.authoredAreas, inputs.roster

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
	if !boot.armed {
		var err error
		loaded, err = loadAuthorityTextdata(devPaths)
		if err != nil {
			return nil, err
		}
	}
	authority, err := openAuthorityPlane(
		startupContext,
		ts,
		ownedShard,
		accountIDs,
		application.readiness,
		devPaths,
		characterRoster,
		sessionVerifier,
		enterWorldSecret,
		loaded,
	)
	if err != nil {
		return nil, err
	}
	ts.SetBootPhase(transport.BootPhaseWiring)
	application.authority = authority.store
	application.controlAPI = authority.agentAPI
	journal, err := history.Open(filepath.Join(store.DirForShardFromEnv(ownedShard.ID), "history.sqlite"), "gameworld", ownedShard.ID, history.Build())
	if err != nil {
		return nil, fmt.Errorf("operator history: %w", err)
	}
	application.history = journal
	ts.Hub.History = historyObserver{journal}
	// Port-only, not native (SRO_CONNECTION_JOURNAL): wiring_census.go.
	if err := installConnectionJournal(application, ts, journal, ownedShard.ID); err != nil {
		return nil, err
	}
	log.AddHook(journal)
	// Port-only: the staff console's read of every tunable and SRO_ flag.
	settingsHandler, err := settingsapi.Handler(filepath.Join(store.DirForShardFromEnv(ownedShard.ID), "operator-token"),
		settingsapi.Getenv, &settingsapi.Service{Store: application.settings, Now: func() int64 { return time.Now().UnixMilli() },
			NewID: func() string { id, _ := history.NewID(); return id }})
	if err != nil {
		return nil, fmt.Errorf("settings: %w", err)
	}
	historyHandler, err := history.OperatorHandler(journal, filepath.Join(store.DirForShardFromEnv(ownedShard.ID), "operator-token"))
	if err != nil {
		return nil, err
	}
	authority.agentAPI.InstallHistory(historyHandler)
	authority.agentAPI.InstallSettings(settingsapi.Path, settingsHandler)
	// Port-only: the staff console's search of the moderation chat log.
	chatLogHandler, err := chatlog.OperatorHandler(store.DirForShardFromEnv(ownedShard.ID),
		filepath.Join(store.DirForShardFromEnv(ownedShard.ID), "operator-token"))
	if err != nil {
		return nil, fmt.Errorf("chat log: %w", err)
	}
	authority.agentAPI.InstallChatLog(chatlog.OperatorPath, chatLogHandler)
	// Port-only: the staff console's census reads over the journal
	// (part 2, #2717); installs only while the journal runs.
	if err := installCensusReads(application, ownedShard.ID); err != nil {
		return nil, err
	}
	if err := installEconomy(application, &authority, ownedShard.ID); err != nil {
		return nil, err
	}
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
	installCertificateWatch(application, ts, ownedShard.ID)
	if err := installSealBoard(application, gameplay, ownedShard.ID); err != nil {
		return nil, err
	}
	installSunDropCount(gameplay)
	gameplay.installSessionLifecycle(ts.Hub, authority.store)
	if gameplay.deps.MonsterState != nil {
		if err := gameplay.deps.MonsterState.EnableDormantStorage(); err != nil {
			return nil, fmt.Errorf("dormant monster storage: %w", err)
		}
		application.populationCache = gameplay.deps.MonsterState
		application.population = gameplay.deps.MonsterState
	}
	// Built after gameplay construction: the static item rows name the
	// alchemy and Magic Pop catalogues it configures.
	references, err := enterworld.NewBrowserReferences(enterworld.BrowserReferenceSources{
		Skills:              gameplay.deps.Skills,
		ItemCommands:        authority.textdata.Items,
		StaticItems:         enterworld.StaticRefItemRows(gameplay.deps),
		Monsters:            enterworld.PublicMonsterRefObjRows(gameplay.deps.MonsterState),
		FortressUpgradeRefs: gameplay.items.FortressUpgradeReferences(),
		HuntingGuide:        gameplay.deps.MonsterState,
		// Empty unless SRO_REVERSE_RETURN_MAP is on (port-only, not native).
		ReverseMapPoints: gameplay.items.ReverseReturnMapPoints(),
	})
	if err != nil {
		return nil, fmt.Errorf("browser references: %w", err)
	}
	gameplay.deps.BrowserReferences = references
	ts.SetPublicReferences(references)
	// Port-only, not native (SRO_PREMIUM_UPGRADE): wiring_premium.go.
	gameplay.wirePremiumUpgrade()
	// Port-only, not native (SRO_KILL_STATS): wiring_killstats.go.
	gameplay.wireKillStats(application)
	// Port-only, not native (SRO_SEAL_CENSUS): wiring_sealcensus.go.
	gameplay.wireSealCensus(application)
	if err := gameplay.register(ts.Hub, authority.loadQuests); err != nil {
		return nil, fmt.Errorf("gameplay wiring: %w", err)
	}
	if err := installEnterWorldVerifier(
		ts.Hub,
		ownedShard.ID,
		enterWorldSecret,
		authority.agentAPI,
	); err != nil {
		return nil, err
	}
	if err := installTransportAdmissionVerifier(
		ts.Hub,
		ownedShard.ID,
		enterWorldSecret,
		authority.agentAPI,
	); err != nil {
		return nil, err
	}
	authority.agentAPI.InstallAccountRevoker(func(accountID string) {
		ts.Hub.EvictAccount(accountID)
	})

	controlAddr, err := controlListenAddress(ownedShard.ControlURL)
	if err != nil {
		return nil, err
	}
	peerReferences, err := gameplay.items.PreparePeerItemReferences(authority.textdata.Items.ItemCommandReferences())
	if err != nil {
		return nil, fmt.Errorf("peer item references: %w", err)
	}
	application.ticker = gameplay.newMissionTicker(peerReferences)
	application.returnArrivals = gameplay.items.RunReturnArrivals
	application.ticker.Commit = tickCommit(authority.store)
	configureReadiness(ts, authority.store, application.readiness, application.ticker)
	gameplay.installFollowFixture(authority.agentAPI, application.ticker)
	authority.agentAPI.InstallPassiveCriticalFixture(gameplay.passiveCriticalReader())
	installMonsterQuery(authority.agentAPI, gameplay.deps.MonsterState, ownedShard.ID)
	installObservatory(authority.agentAPI, gameplay.deps.MonsterState, ts.Hub, authority.store, ownedShard.ID)
	installOperatorNotices(authority.agentAPI, ts.Hub, ownedShard.ID, gameplay.chatFeed)
	// GM and operator silk grants (operator tooling): the account wallet, and
	// one history event per committed grant.
	gameplay.items.SilkWallet = authority.store
	gameplay.items.RecordSilkGrant = func(grant action.SilkGrant) { journal.Record(silkGrantEvent(grant)) }
	// Crest uploads and staff removals (#2282): the crest store over
	// the marks directory, and one audit event per committed change.
	gameplay.items.Crests = authority.crests
	gameplay.items.RecordCrestChange = func(change action.CrestChange) { journal.Record(crestChangeEvent(change)) }
	// The mute book opens before admission: a muted character's first
	// bind after a restart must already find its record (#2671).
	application.mutes, err = openMuteBook(ts.Hub, ownedShard.ID)
	if err != nil {
		return nil, err
	}
	gameplay.mutes = application.mutes
	if err := installPlayerOperations(authority.agentAPI, gameplay, ts.Hub, authority.store, ownedShard.ID); err != nil {
		return nil, fmt.Errorf("player operations: %w", err)
	}
	application.publicAPI = installPublicAPI(gameplay, ts.Hub, authority.store, ownedShard.ID, application.sealBoard, application.census)
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
	// Admission is the filling phase (#3186): healthz says so until the
	// population settles and the gate opens.
	application.transport.SetBootPhase(transport.BootPhaseFilling)
	started := time.Now()
	settled := func() bool {
		population := application.population
		return population == nil || population.PopulationSettled(application.shardID)
	}
	open := func(settled bool) {
		application.transport.SetBootPhase(transport.BootPhaseReady)
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
	if application.returnArrivals != nil {
		group.Go(func() error {
			application.returnArrivals(groupContext)
			return nil
		})
	}
	if application.economy != nil {
		group.Go(func() error {
			application.economy.Run(groupContext)
			return nil
		})
	}
	if application.sealBoard != nil {
		group.Go(func() error {
			application.sealBoard.Run(groupContext)
			return nil
		})
	}
	if application.mutes != nil {
		group.Go(func() error {
			runMuteReaper(groupContext, application.mutes)
			return nil
		})
	}
	if application.certificateWatch != nil {
		group.Go(func() error {
			ticker := time.NewTicker(transport.CertificateCheckInterval)
			defer ticker.Stop()
			application.certificateWatch.Run(groupContext, ticker.C)
			return nil
		})
	}
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

	// The shutdown tail runs in one order and says how long each step
	// took (#3186): stop admission, drain the sessions, commit the
	// final tick, close the authority store, settle the small doors -
	// and the history journal closes LAST, so every one of those lines
	// is still recorded. The whole tail answers to shutdownBudget.
	<-groupContext.Done()
	shutdownStarted := time.Now()
	cancelRun()
	<-admitted
	log.Infof("shutdown: admission stopped in %s", time.Since(shutdownStarted).Round(time.Millisecond))
	application.readiness.Close()

	drainStarted := time.Now()
	drainErr := application.drainNetwork()
	log.Infof("shutdown: sessions drained in %s", time.Since(drainStarted).Round(time.Millisecond))

	runErr := group.Wait()
	// The tick has stopped: no kill can be queued after this, and the queue
	// commits before the authority store closes below.
	if application.publicAPI != nil {
		drainErr = errors.Join(drainErr, application.publicAPI.Close())
	}
	for _, cache := range []io.Closer{application.skillCache, application.itemCache} {
		if cache != nil {
			drainErr = errors.Join(drainErr, cache.Close())
		}
	}
	if application.populationCache != nil {
		drainErr = errors.Join(drainErr, application.populationCache.Close())
	}
	// The tick has stopped and the flush loop has joined: the counter's
	// last flush lands before its store closes.
	if application.sealBoard != nil {
		drainErr = errors.Join(drainErr, application.sealBoard.Close())
	}
	application.releaseLease()
	if application.authority != nil {
		storeStarted := time.Now()
		log.Info("shutdown: closing the authority store")
		application.authority.Close()
		log.Infof("shutdown: authority store closed in %s", time.Since(storeStarted).Round(time.Millisecond))
	}
	application.leaseOwned = false
	// The tick has stopped, so the rollup is final: write it before the
	// economy history closes (#1639).
	if application.killStatsFlush != nil {
		if err := application.killStatsFlush(); err != nil {
			log.WithError(err).Warn("kill stats: final flush failed")
		}
	}
	if application.economy != nil {
		drainErr = errors.Join(drainErr, application.economy.Close())
	}
	if application.settings != nil {
		drainErr = errors.Join(drainErr, application.settings.Close())
	}
	if application.history != nil {
		drainErr = errors.Join(drainErr, application.history.Close())
	}
	if application.connJournal != nil {
		drainErr = errors.Join(drainErr, application.connJournal.Close())
	}
	shutdownTotal := time.Since(shutdownStarted).Round(time.Millisecond)
	if shutdownTotal > shutdownBudget {
		log.Warnf("shutdown: complete in %s, OVER the %s budget", shutdownTotal, shutdownBudget)
	} else {
		log.Infof("shutdown: complete in %s (budget %s)", shutdownTotal, shutdownBudget)
	}
	// The guard is the boot's outermost lock: it goes last, after the
	// authority store, so a standby waiting on it wakes to a shard
	// whose writer holds nothing (opensro-ops#281).
	application.closeGuard("shutdown")

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
	if application.publicAPI != nil {
		if err := application.publicAPI.Close(); err != nil {
			log.Warnf("startup rollback public api: %v", err)
		}
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
	if application.authority != nil {
		application.authority.Close()
	}
	application.releaseLease()
	application.leaseOwned = false
	if application.economy != nil {
		_ = application.economy.Close()
	}
	if application.sealBoard != nil {
		_ = application.sealBoard.Close()
	}
	if application.settings != nil {
		_ = application.settings.Close()
	}
	// The history journal closes last, as in Run: every rollback line
	// above is still recorded (#3186).
	if application.history != nil {
		_ = application.history.Close()
	}
	application.closeGuard("startup rollback")
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
