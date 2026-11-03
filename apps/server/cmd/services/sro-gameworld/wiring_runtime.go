/*
===========================================================================

wiring_runtime.go - long-lived world services and authentication admission.

===========================================================================
*/
package main

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"time"

	log "github.com/sirupsen/logrus"
	agentapi "opensro.online/server/internal/agent/api"
	"opensro.online/server/internal/data/store"
	"opensro.online/server/internal/game/action"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/movement"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/security/auth"
	"opensro.online/server/internal/transport"
	"opensro.online/server/internal/transport/worldsession"
)

/*
================================================================================
Long-lived runtime services

The simulation ticker derives from the process context and is joined during
shutdown. Enter-world authentication is installed before listeners start.
================================================================================
*/

/*
================
newMissionTicker

Compose action, transient-object, quest and party phases before admission.
All world lifetimes share the coordinator's simulation clock.
================
*/
func (game *gameplayPlane) newMissionTicker(peerReferences *action.PeerReferenceCatalog) *simulation.Ticker {
	// The ticker and bootstrap must see the same NPC roster. Shipped static
	// rows have Patrol=false; this also prevents the old three-fixture ticker
	// from broadcasting movements for objects production never spawned.
	var ticker *simulation.Ticker
	// A slow action tick names its sub-step on /transport/metrics slow_hooks.
	game.items.Steps.Slow = func(name string, elapsed time.Duration) {
		game.hub.RecordSlowStep("action.TickHook/"+name, elapsed)
	}
	// A slow publication-gate hold names its holder there too
	// (publication_holds), so a waiting step is not mistaken for the stall.
	game.items.PublicationHold = game.hub.RecordPublicationHold
	// Every hook carries the name slow-hook reports print (#2294): the
	// closures below would otherwise report as newMissionTicker.funcN.
	hooks := []simulation.NamedTickHook{
		{Name: "movement-ground", Hook: game.movement.GroundTickHook()},
		{Name: "action", Hook: game.items.TickHook()},
		// Direction walks continue leg by leg on the mission clock.
		{Name: "movement-direction", Hook: game.movement.DirectionTickHook()},
		{Name: "skill-objects", Hook: func(nowMs int64) []simulation.DivisionFrames {
			return game.items.AdvanceSkillObjects(nowMs, sessionViews(ticker.Source))
		}},
		// Drops enter and leave each scene with its range (#617).
		{Name: "ground-scope", Hook: func(nowMs int64) []simulation.DivisionFrames {
			game.items.AdvanceGroundScope(nowMs, sessionViews(ticker.Source), ticker.Source.(*worldsession.Bridge).SceneSession)
			return nil
		}},
		// A marked player's moves reach its hunter out of sight (hntp).
		{Name: "hunting-points", Hook: func(nowMs int64) []simulation.DivisionFrames {
			return game.items.AdvanceHuntingPoints(nowMs, sessionViews(ticker.Source))
		}},
		{Name: "quest-markers", Hook: game.questMarkerTick()},
		{Name: "parties", Hook: func(nowMs int64) []simulation.DivisionFrames {
			game.parties.ExpireInvitations(nowMs)
			return game.parties.MemberUpdates(sessionViews(ticker.Source), nowMs)
		}},
		// The fortress war's schedule edges run on the mission clock.
		{Name: "siege", Hook: game.siege.Tick},
		{Name: "guild-wars", Hook: game.guildWars.Tick},
		// The job guilds' week closes on the mission clock (jobrank.go).
		{Name: "job-week", Hook: func(nowMs int64) []simulation.DivisionFrames {
			game.items.JobWeekTick(game.divisionID, nowMs)
			return nil
		}},
	}
	// Native/off installs no hook and takes no extra session snapshots.
	if game.betaSilk != nil {
		hooks = append(hooks, simulation.NamedTickHook{Name: "beta-silk", Hook: func(nowMs int64) []simulation.DivisionFrames {
			return game.betaSilkTick(ticker, nowMs)
		}})
	}
	ticker = worldsession.NewTicker(game.hub, game.items.NpcRoster, hooks...)
	ticker.Source.(*worldsession.Bridge).PopulationLease = game.items.CharacterPopulationLease
	// A quest instance's exit gate joins its world's NPC scope pass.
	ticker.WorldNpcs = game.items.QuestGateNpcs
	ticker.BeforeHooks = []simulation.NamedTickHook{{Name: "monster-actions", Hook: game.items.MonsterActionTickHook()}}
	// A peer's spawn row is preceded by the item references it names (#340).
	ticker.ItemReferences = peerReferences.Frames
	// A deadlocked tick dumps its stacks and exits for the supervisor to
	// restart (tick_watchdog.go); the dumps sit beside the authority store.
	ticker.StallExit = simulation.TickStallExitFromEnv()
	ticker.StallDumpDir = os.Getenv(simulation.EnvTickStallDumpDir)
	if ticker.StallDumpDir == "" {
		ticker.StallDumpDir = filepath.Join(".state", "stall-dumps")
		if dir := store.DirFromEnv(); dir != "" {
			ticker.StallDumpDir = filepath.Join(dir, "stall-dumps")
		}
	}
	log.Infof("simulation: a tick stuck %v dumps to %s and exits for restart (%s, 0 = never)", ticker.StallExit, ticker.StallDumpDir, simulation.EnvTickStallExit)
	ticker.PlayerMap = simulation.BetaPlayerMapEnabled()
	if ticker.PlayerMap {
		log.Infof("simulation: beta world map roster ON (%s)", simulation.EnvBetaPlayerMap)
	}
	if game.deps.MonsterState != nil {
		navigation := &simulation.NavigationMetrics{}
		game.hub.SetNavigationMetrics(navigation.Snapshot)
		// The mover's routes come from the plane that owns the
		// region: the open-terrain search outdoors, the cave's
		// region route inside a dungeon floor (#1645/#1646).
		planes := movement.NewPlanes(game.movement, game.water)
		ticker.Monsters = &simulation.MonsterMoverOps{
			Navigation: navigation,
			Monsters:   game.deps.MonsterState,
			TacticsFor: monster.ResolveTactics,
			// #1695: live heights come from the plane that owns the
			// region (the deck/cave-floor surface nearest the caller),
			// not the bare terrain sampler.
			GroundHeight: func(pose monster.Pose) (float64, bool) {
				return planes.For(pose.RegionID).HeightAt(simulation.Spawn{
					RegionID: pose.RegionID, X: pose.X, Y: pose.Y, Z: pose.Z,
				})
			},
			PlanPath:     game.water.PlanMonsterPath,
			PlanPathFrom: game.water.PlanMonsterPathFrom,
			PlanRoute: func(from monster.Pose, fromOwner simulation.NavOwner, goal monster.Pose) *monster.NavigationRoute {
				plane, ok := planes.Between(from.RegionID, goal.RegionID)
				if !ok {
					// No plane owns a route across the outdoor/cave
					// boundary: the mover waits and replans, as it
					// does for any unresolved route.
					return monster.UnresolvedNavigationRoute(goal, monster.NavigationRouteBlocked)
				}
				return plane.NextWaypoint(from, fromOwner, goal)
			},
			// The failed-route memo may answer a retry only where the
			// plane's routes are pure (#1820 part B).
			RouteMemo: func(from, goal monster.Pose) bool {
				plane, ok := planes.Between(from.RegionID, goal.RegionID)
				return ok && plane.PureRoutes()
			},
			// Slow-leg attribution diffs this per leg; a nonzero delta
			// is a first-use load the boot preload missed (#1820 C).
			NavCacheMisses: game.water.NavCacheMisses,
			MessageBlockAt: game.water.MessageBlockAt,
			Rand:           rand.Float64,
			AttackPlan:     game.items.MonsterAttackPlan,
			RunAction:      game.items.RunMonsterAction,
			// The Bard's Noise: the acquisition scan reads each player's
			// first-attack protection from the effect owner.
			FirstAttackGuard: game.items.FirstAttackGuard,
			// The server's target picker: a candidate player's companions are weighed with it.
			Companions: game.items.CompanionTargets,
		}
		log.Infof(
			"simulation: monster mover wired with %d template nest row(s)",
			game.deps.MonsterState.TemplateSize(),
		)
	}

	return ticker
}

/*
================
sessionViews

The hooks' light session views: the bridge builds them without the peer
presentation (worldsession.Bridge.SnapshotSessionViews); any other source
is viewed through its full snapshots.
================
*/
func sessionViews(source simulation.SessionSource) []simulation.SessionView {
	if viewer, ok := source.(interface {
		SnapshotSessionViews() []simulation.SessionView
	}); ok {
		return viewer.SnapshotSessionViews()
	}
	snaps := source.SnapshotSessions()
	views := make([]simulation.SessionView, len(snaps))
	for i := range snaps {
		views[i] = snaps[i].View()
	}
	return views
}

/*
================
installEnterWorldVerifier
================
*/
func installEnterWorldVerifier(
	hub *transport.Hub,
	ownedShardID string,
	secret []byte,
	api *agentapi.API,
) error {
	if ownedShardID == "" {
		return fmt.Errorf("EnterWorld authentication: worker shard id is required")
	}
	if err := auth.ValidateSecret(secret); err != nil {
		return fmt.Errorf("EnterWorld authentication: %w", err)
	}
	verifier := auth.Verifier(secret, time.Now)
	if api != nil {
		verifier = api.WrapEnterWorldVerifier(verifier)
	}
	hub.SetEnterWorldAuth(enterWorldVerifierForShard(ownedShardID, verifier))
	log.Info("transport: EnterWorld verifier uses a process-local one-use key")
	return nil
}

/*
================
installTransportAdmissionVerifier
================
*/
func installTransportAdmissionVerifier(
	hub *transport.Hub,
	ownedShardID string,
	secret []byte,
	api *agentapi.API,
) error {
	if ownedShardID == "" {
		return fmt.Errorf("transport admission: worker shard id is required")
	}
	if err := auth.ValidateSecret(secret); err != nil {
		return fmt.Errorf("transport admission: %w", err)
	}
	ledger := auth.NewTransportAdmissionLedger(secret, time.Now)
	verify := ledger.Verify
	if api != nil {
		verify = api.WrapTransportAdmissionVerifier(verify)
	}
	hub.SetHelloCommitAuth(func(token []byte) (transport.AdmissionIdentity, func() error, error) {
		claims, err := verify(string(token))
		if err != nil {
			return transport.AdmissionIdentity{}, nil, err
		}
		if claims.ShardID != ownedShardID {
			return transport.AdmissionIdentity{}, nil, fmt.Errorf(
				"transport admission shard %q does not match worker %q",
				claims.ShardID,
				ownedShardID,
			)
		}
		commit := func() error {
			// A revocation landing between HELLO and WELCOME still
			// refuses: the check rides the commit, not only the verify.
			if api != nil && api.TicketRevoked(string(token)) {
				return auth.ErrTransportAdmissionExpired
			}
			return ledger.Consume(string(token))
		}
		return transport.AdmissionIdentity{
			AccountID: claims.AccountID,
			ShardID:   claims.ShardID,
		}, commit, nil
	})
	log.Info("transport: authenticated HELLO verifier uses a process-local one-use key, consumed at WELCOME")
	return nil
}

/*
================
enterWorldVerifierForShard
================
*/
func enterWorldVerifierForShard(
	ownedShardID string,
	verifier auth.VerifyFunc,
) transport.EnterWorldAuthFunc {
	return func(session *transport.Session, enter transport.EnterWorld) (bool, uint32) {
		if session != nil && session.Evicted() {
			return false, auth.DenyCodeUnauthorized
		}
		if enter.Division != ownedShardID {
			log.WithFields(log.Fields{
				"workerShard": ownedShardID,
				"division":    enter.Division,
				"character":   enter.CharName,
			}).Warn("auth: EnterWorld refused for foreign shard")
			return false, auth.DenyCodeUnauthorized
		}
		err := verifier(string(enter.AuthToken), enter.Division, enter.CharName)
		if err == nil {
			return true, 0
		}
		log.WithFields(log.Fields{
			"division":  enter.Division,
			"character": enter.CharName,
			"reason":    err,
		}).Warn("auth: EnterWorld token refused")
		return false, auth.DenyCodeUnauthorized
	}
}
