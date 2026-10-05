/*
===========================================================================

wiring_runtime.go - long-lived world services and authentication admission.

===========================================================================
*/
package main

import (
	"fmt"
	"math/rand"
	"time"

	log "github.com/sirupsen/logrus"
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
func (game *gameplayPlane) newMissionTicker() *simulation.Ticker {
	// The ticker and bootstrap must see the same NPC roster. Shipped static
	// rows have Patrol=false; this also prevents the old three-fixture ticker
	// from broadcasting movements for objects production never spawned.
	var ticker *simulation.Ticker
	ticker = worldsession.NewTicker(
		game.hub,
		game.items.NpcRoster,
		game.items.TickHook(),
		// Direction walks continue leg by leg on the mission clock.
		game.movement.DirectionTickHook(),
		func(nowMs int64) []simulation.DivisionFrames {
			return game.items.AdvanceSkillObjects(nowMs, ticker.Source.SnapshotSessions())
		},
		game.questMarkerTick(),
		func(nowMs int64) []simulation.DivisionFrames {
			game.parties.ExpireInvitations(nowMs)
			return game.parties.MemberUpdates(ticker.Source.SnapshotSessions(), nowMs)
		},
		// The fortress war's schedule edges run on the mission clock.
		game.siege.Tick,
	)
	ticker.Source.(*worldsession.Bridge).PopulationLease = game.items.CharacterPopulationLease
	ticker.BeforeHooks = []simulation.TickHook{game.items.MonsterActionTickHook()}
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
