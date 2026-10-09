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

const nativeRandomMaximum = 32767
const nativeCircleRadians = 6.2831854820251465

/*
================
QuestMonsterSpawn

The action owner supplies residency and its live pose under division authority.
The monster lands RadiusMin + fraction * RadiusSpan from Position: the
player for Ivy and Cerberus, a fixed point for Hidden Treasure 5 (8C7910).
LifetimeMs, when set, is the quest's own removal timer (8C7910 registers
one of 300 s whose handler, 8BBBE0, sends the monster to life state 3).
================
*/
type QuestMonsterSpawn struct {
	Division   string
	Population instance.Lease
	Codename   string
	Position   Spawn
	NowMs      int64
	RadiusMin  float64
	RadiusSpan float64
	LifetimeMs int64
}

/*
================
SpawnQuestMonster

8B9B60 (Ivy guardian, 0 + 20), 8B2AB0 (Cerberus lure, 20 + 80) and 8C7910
(Hidden Treasure guardian, 20 + 80) draw a float32 angle, then a float32
radius, before entering the world factory. Its heading is the truncated
angle word, not a normalized movement heading. The factory's spawn base
(4C10C0) arms the ordinary timers too: the Ivy guardian
MOB_QT_02_PUNISHER_CLON leaves after its 300 s.
================
*/
func (s *MonsterState) SpawnQuestMonster(request QuestMonsterSpawn) bool {
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
	radius := float64(float32(float64(radiusFraction)*request.RadiusSpan + request.RadiusMin))
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
	armLifetimeLocked(state, actor, request.NowMs)
	armQuestLifetimeLocked(state, actor.Gid, request.NowMs+request.LifetimeMs, request.LifetimeMs > 0)
	return true
}

/*
================
armQuestLifetimeLocked

A quest's removal timer runs beside the spawn base's own: whichever ends
first removes the monster. The caller holds s.mu.
================
*/
func armQuestLifetimeLocked(state *divisionMonsterState, gid uint32, untilMs int64, armed bool) {
	if !armed {
		return
	}
	if lifetime, ok := state.lifetimes[gid]; ok && lifetime.untilMs <= untilMs {
		return
	}
	if state.lifetimes == nil {
		state.lifetimes = make(map[uint32]monsterLifetime)
	}
	state.lifetimes[gid] = monsterLifetime{untilMs: untilMs}
}
