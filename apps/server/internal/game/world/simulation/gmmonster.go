/*
===========================================================================

gmmonster.go - GM /LOADMONSTER: monsters created at the GM's position

Native 520A40 (CGObjPC_OnCommandSummonOrKillMob, the SpecialCommand
LOADMONSTER case) creates count instances of one monster reference at the
GM's own position, each with a random heading, through the ordinary world
factory. They are ordinary monsters: production AI, aggro, attacks, damage
and death. Like script-created monsters they have no respawning nest.

===========================================================================
*/
package simulation

import (
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/monster"
)

// 520A5B..520A69 clamp the requested count to 1..250.
const gmMonsterMaxCount = 250

/*
================
GMMonsterSpawn

The action owner supplies residency and the GM's live pose under division
authority.
================
*/
type GMMonsterSpawn struct {
	Division   string
	Population instance.Lease
	RefObjID   uint32
	Count      uint8
	Type       uint8
	Position   Spawn
	NowMs      int64
}

/*
================
GMSpawnRarity

520D90 resolves the requested type byte against the reference's own type
(+0x89): a unique (3) or type-8 reference keeps its own; otherwise a
requested normal (0), champion (1), giant (4) or 7 is kept and any other
value falls back to the reference's type.
================
*/
func GMSpawnRarity(requested, referenceType uint8) uint8 {
	requested &= 0x0f
	own := referenceType & 0x0f
	if own == 3 || own == 8 {
		return own
	}
	switch requested {
	case 0, 1, 4, 7:
		return requested
	}
	return own
}

/*
================
SpawnGMMonsters

Returns how many monsters were created; zero refuses the command. Every
instance shares the GM's point; only its heading is drawn, as rand()/32767
of a full turn in radians (520B85..520BB3), passed as a float to the world
factory (5F6EB0) like the nest path's heading.

INFERENCE: SpawnHeading is the wire word, which the client decodes as
degrees * 65535 / 360 (the inverse of its serializer sub_877cc0). The
server's own spawn-record writer was not traced, so a native quirk there is
not ruled out; this takes the nest path's radians -> word conversion so the
client sees the drawn direction.
================
*/
func (s *MonsterState) SpawnGMMonsters(request GMMonsterSpawn) int {
	count := int(request.Count)
	if count < 1 {
		count = 1
	}
	if count > gmMonsterMaxCount {
		count = gmMonsterMaxCount
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ref, ok := s.template.Refs[request.RefObjID]
	if !ok || ref.MaxHP == 0 {
		return 0
	}
	state := s.populationForLease(request.Division, request.Population)
	if state == nil {
		return 0
	}
	center := request.Position
	spawn := normalizeGeneratedMonsterSpawn(monster.SpawnPoint{
		RefObjID: ref.RefObjID, RegionID: center.RegionID, X: center.X, Y: center.Y, Z: center.Z,
	})
	spawn, valid := s.resolveSpawnGround(spawn, center.Y)
	if !valid {
		return 0
	}
	rarity := GMSpawnRarity(request.Type, ref.MonsterType)
	created := 0
	for ; created < count; created++ {
		if s.counter >= domain.MaxMonsterGIDCounter {
			break
		}
		angle := float64(float32(float64(float32(float64(s.randomWord())/nativeRandomMaximum)) * nativeCircleRadians))
		s.counter++
		actor := monster.Instance{
			Gid: monster.GidBase + s.counter, Ref: ref, Spawn: spawn, NestDetached: true,
			SpawnHeading: HeadingWordFromRadians(angle),
			Nest: monster.NestRow{
				SpawnPoint: spawn, PolicyPinned: true, HasRarityOverride: true, RarityOverride: rarity,
			},
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
			state.movers = newMoverStorage(nil)
		}
		state.instances.set(actor.Gid, actor)
		state.movers.set(actor.Gid, mover)
		state.behavior.set(actor.Gid, 0)
		state.byRegion[spawn.RegionID] = append(state.byRegion[spawn.RegionID], actor.Gid)
	}
	return created
}
