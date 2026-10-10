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
	"opensro.online/server/internal/data/store"
	"opensro.online/server/internal/game/action"
	"opensro.online/server/internal/game/world/monster"
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
	hooks := []simulation.TickHook{
		game.movement.GroundTickHook(),
		game.items.TickHook(),
		// Direction walks continue leg by leg on the mission clock.
		game.movement.DirectionTickHook(),
		func(nowMs int64) []simulation.DivisionFrames {
			return game.items.AdvanceSkillObjects(nowMs, sessionViews(ticker.Source))
		},
		// A marked player's moves reach its hunter out of sight (hntp).
		func(nowMs int64) []simulation.DivisionFrames {
			return game.items.AdvanceHuntingPoints(nowMs, sessionViews(ticker.Source))
		},
		game.questMarkerTick(),
		func(nowMs int64) []simulation.DivisionFrames {
			game.parties.ExpireInvitations(nowMs)
			return game.parties.MemberUpdates(sessionViews(ticker.Source), nowMs)
		},
		// The fortress war's schedule edges run on the mission clock.
		game.siege.Tick,
		game.guildWars.Tick,
		// The job guilds' week closes on the mission clock (jobrank.go).
		func(nowMs int64) []simulation.DivisionFrames {
			game.items.JobWeekTick(game.divisionID, nowMs)
			return nil
		},
	}
	// Native/off installs no hook and takes no extra session snapshots.
	if game.betaSilk != nil {
		hooks = append(hooks, func(nowMs int64) []simulation.DivisionFrames {
			return game.betaSilkTick(ticker, nowMs)
		})
	}
	ticker = worldsession.NewTicker(game.hub, game.items.NpcRoster, hooks...)
	ticker.Source.(*worldsession.Bridge).PopulationLease = game.items.CharacterPopulationLease
	ticker.BeforeHooks = []simulation.TickHook{game.items.MonsterActionTickHook()}
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
		ticker.Monsters = &simulation.MonsterMoverOps{
			Monsters:       game.deps.MonsterState,
			TacticsFor:     monster.ResolveTactics,
			TerrainHeight:  game.water.TerrainHeightAt,
			PlanPath:       game.water.PlanMonsterPath,
			PlanPathFrom:   game.water.PlanMonsterPathFrom,
			PlanRoute:      game.water.PlanMonsterRoute,
			MessageBlockAt: game.water.MessageBlockAt,
			Rand:           rand.Float64,
			AttackPlan:     game.items.MonsterAttackPlan,
			RunAction:      game.items.RunMonsterAction,
			// The Bard's Noise: the acquisition scan reads each player's
			// first-attack protection from the effect owner.
			FirstAttackGuard: game.items.FirstAttackGuard,
			// 5464E0: a candidate player's companions are weighed with it.
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
) error {
	if ownedShardID == "" {
		return fmt.Errorf("EnterWorld authentication: worker shard id is required")
	}
	if err := auth.ValidateSecret(secret); err != nil {
		return fmt.Errorf("EnterWorld authentication: %w", err)
	}
	verifier := auth.Verifier(secret, time.Now)
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
) error {
	if ownedShardID == "" {
		return fmt.Errorf("transport admission: worker shard id is required")
	}
	if err := auth.ValidateSecret(secret); err != nil {
		return fmt.Errorf("transport admission: %w", err)
	}
	verify := auth.TransportAdmissionVerifier(secret, time.Now)
	hub.SetHelloAuth(func(token []byte) (transport.AdmissionIdentity, error) {
		claims, err := verify(string(token))
		if err != nil {
			return transport.AdmissionIdentity{}, err
		}
		if claims.ShardID != ownedShardID {
			return transport.AdmissionIdentity{}, fmt.Errorf(
				"transport admission shard %q does not match worker %q",
				claims.ShardID,
				ownedShardID,
			)
		}
		return transport.AdmissionIdentity{
			AccountID: claims.AccountID,
			ShardID:   claims.ShardID,
		}, nil
	})
	log.Info("transport: authenticated HELLO verifier uses a process-local one-use key")
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
	return func(_ *transport.Session, enter transport.EnterWorld) (bool, uint32) {
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
