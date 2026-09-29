/*
===========================================================================

questmonster.go - script-created monsters in an admitted population

Quest callbacks use the ordinary monster identity, movement and visibility
owners. Generated actors have no respawning nest and cannot escape the exact
population generation that admitted their player.

===========================================================================
*/
package simulation

import (
	"math"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/monster"
)

const questGuardianRadius = 20
const nativeRandomMaximum = 32767
const nativeCircleRadians = 6.2831854820251465

/*
================
QuestMonsterSpawn

The action owner supplies residency and its live pose under division authority.
================
*/
type QuestMonsterSpawn struct {
	Division   string
	Population instance.Lease
	Codename   string
	Position   Spawn
	NowMs      int64
}

/*
================
SpawnQuestGuardian

8B9B60 draws a float32 angle and radius before entering the world factory.
Its heading is the truncated angle word, not a normalized movement heading.
================
*/
func (s *MonsterState) SpawnQuestGuardian(request QuestMonsterSpawn) bool {
	if IsDungeonRegion(request.Position.RegionID) {
		return false
	}
	ref, exists := s.ReferenceByCodename(request.Codename)
	if !exists || ref.MaxHP == 0 {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.populationForLease(request.Division, request.Population)
	if state == nil || s.counter >= domain.MaxMonsterGIDCounter {
		return false
	}
	angleFraction := float32(float64(s.randomWord()) / nativeRandomMaximum)
	angle := float64(float32(float64(angleFraction) * nativeCircleRadians))
	radiusFraction := float32(float64(s.randomWord()) / nativeRandomMaximum)
	radius := float64(float32(float64(radiusFraction) * questGuardianRadius))
	center := request.Position
	spawn := normalizeGeneratedMonsterSpawn(monster.SpawnPoint{
		RefObjID: ref.RefObjID, RegionID: center.RegionID,
		X: center.X + math.Cos(angle)*radius, Y: center.Y, Z: center.Z + math.Sin(angle)*radius,
	})
	spawn, valid := s.resolveSpawnGround(spawn, center.Y)
	if !valid {
		return false
	}
	s.counter++
	actor := monster.Instance{
		Gid: monster.GidBase + s.counter, Ref: ref, Spawn: spawn, NestDetached: true,
		SpawnHeading: uint16(angle), Nest: monster.NestRow{SpawnPoint: spawn, PolicyPinned: true},
	}
	actor.CurrentHP = actor.EffectiveMaxHP()
	if tactics, found := monster.ResolveSummonTactics(ref, 0, s.random); found {
		actor.Nest.NativeTacticsFlags = tactics.NativeFlags
		actor.Nest.TargetPolicy = tactics.TargetPolicy
		if tactics.HasControls {
			actor.Nest.Controls, actor.Nest.HasControls = tactics.Controls, true
			actor.Nest.ConditionalSkills = tactics.ConditionalSkills
			actor.Nest.SightRange = float64(tactics.Controls.SightRange)
			actor.Nest.Aggressive = tactics.Controls.AggressType == 0
		}
	}
	mover := monster.NewSpawnMover(actor, request.NowMs)
	mover.Activity = monster.NewActivityCadence(uint32(request.NowMs), s.randomWord())
	if state.movers == nil {
		state.movers = make(moverStorage)
	}
	state.instances.set(actor.Gid, actor)
	state.movers.set(actor.Gid, mover)
	state.behavior.set(actor.Gid, 0)
	state.byRegion[spawn.RegionID] = append(state.byRegion[spawn.RegionID], actor.Gid)
	return true
}
