/*
===========================================================================

caravanbandit.go - bandits generated around a trade caravan

Caravan_SpawnBandits (60BF30) hands CMonster_SpawnInstance the vehicle's
position, the bandit reference, its numbered tactics, a random heading and
a 150-unit spawn radius. The actor joins the trader's population like any
other generated monster but belongs to no nest and never respawns.

===========================================================================
*/
package simulation

import (
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/monster"
)

// caravanBanditRadius is the native spawn radius around the vehicle.
const caravanBanditRadius = 150

/*
================
BanditTables

The native thief and hunter tables over the whole reference catalog (job
monsters have no nest, so the spawnable set would miss them). Built once;
the template never changes.
================
*/
func (s *MonsterState) BanditTables() *monster.BanditTables {
	s.banditsOnce.Do(func() {
		refs := make([]monster.MonsterRef, 0, len(s.template.Refs))
		for _, ref := range s.template.Refs {
			refs = append(refs, ref)
		}
		s.bandits = monster.NewBanditTables(refs)
	})
	return s.bandits
}

/*
================
CaravanBanditSpawn

The action owner supplies the trader's population, the vehicle position
and every native draw already made: reference, tactics, heading, rarity.
================
*/
type CaravanBanditSpawn struct {
	Division   string
	Population instance.Lease
	Ref        monster.MonsterRef
	Vehicle    Spawn
	// HeadingRadians is the float32 angle the native draws per bandit.
	HeadingRadians float32
	Tactics        monster.SummonTactics
	// Rarity is the low nibble the native rolls: 1 for a 2% champion.
	Rarity uint8
	NowMs  int64
}

/*
================
SpawnCaravanBandit

Place the bandit by the native nest placement around the vehicle (radius
sample, region clamp, collision and ground) and admit it detached from any
nest. Its heading is the truncated angle word, as other generated monsters.
================
*/
func (s *MonsterState) SpawnCaravanBandit(request CaravanBanditSpawn) bool {
	if IsDungeonRegion(request.Vehicle.RegionID) || request.Ref.MaxHP == 0 {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.populationForLease(request.Division, request.Population)
	if state == nil || s.counter >= domain.MaxMonsterGIDCounter {
		return false
	}
	centre := monster.SpawnPoint{
		RefObjID: request.Ref.RefObjID, RegionID: request.Vehicle.RegionID,
		X: request.Vehicle.X, Y: request.Vehicle.Y, Z: request.Vehicle.Z,
	}
	nest := monster.NestRow{
		SpawnPoint: centre, GenerateRadius: caravanBanditRadius, PolicyPinned: true,
		HasRarityOverride: true, RarityOverride: request.Rarity,
	}
	spawn, placement := s.placeNativeSpawn(nest, request.Ref, request.Rarity)
	if placement != spawnPlaced {
		return false
	}
	nest.SpawnPoint = spawn
	nest.NativeTacticsFlags = request.Tactics.NativeFlags
	nest.TargetPolicy = request.Tactics.TargetPolicy
	if request.Tactics.HasControls {
		nest.Controls, nest.HasControls = request.Tactics.Controls, true
		nest.ConditionalSkills = request.Tactics.ConditionalSkills
		nest.SightRange = float64(request.Tactics.Controls.SightRange)
		nest.Aggressive = request.Tactics.Controls.AggressType == 0
	}
	s.counter++
	actor := monster.Instance{
		Gid: monster.GidBase + s.counter, Ref: request.Ref, Spawn: spawn, NestDetached: true,
		SpawnHeading: uint16(request.HeadingRadians), Nest: nest,
	}
	actor.CurrentHP = actor.EffectiveMaxHP()
	mover := monster.NewSpawnMover(actor, request.NowMs)
	mover.Activity = monster.NewActivityCadence(uint32(request.NowMs), s.randomWord())
	if state.movers == nil {
		state.movers = newMoverStorage(nil)
	}
	state.instances.set(actor.Gid, actor)
	state.movers.set(actor.Gid, mover)
	state.behavior.set(actor.Gid, 0)
	state.byRegion[spawn.RegionID] = append(state.byRegion[spawn.RegionID], actor.Gid)
	return true
}
