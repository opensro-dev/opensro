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
player for Ivy and Cerberus, a fixed point for Hidden Treasure 5.
FixedPosition says the quest named Position; otherwise the action owner
fills it with the player's live pose.
LifetimeMs, when set, is the quest's own removal timer (the Hidden
Treasure script registers one of 300 s whose handler sends the monster
to life state 3). TacticsFrom, when set, gives the monster that nest's own
hostility and skills, for a monster with no summon tactics row.
HasRarity overrides the reference's grade with Rarity (the v1.188
summon scroll spawns its monsters at grade 7, action/partysummonscroll.go).
TacticsGrade is the grade its default tactics are chosen by (the v1.188
summon scroll asks with 0, the Pandora waves with their grade 7).
Aggressive fills a gap of the port's tactics data: a monster whose
tactics carry no controls attacks on sight (with eventAggressiveSight
when no sight range is named) instead of standing passive; tactics that
carry controls keep their own hostility, as natively.
================
*/
type QuestMonsterSpawn struct {
	Division      string
	Population    instance.Lease
	Codename      string
	Position      Spawn
	FixedPosition bool
	NowMs         int64
	RadiusMin     float64
	RadiusSpan    float64
	LifetimeMs    int64
	TacticsFrom   *monster.NestRow
	Rarity        uint8
	HasRarity     bool
	Aggressive    bool
	TacticsGrade  uint8
}

// eventAggressiveSight is the sight an event monster forced aggressive
// gets when its tactics name none: the temptation fallback's 100.
const eventAggressiveSight = temptationFallbackSight

/*
================
SpawnQuestMonster

The retail server's quest scripts (Ivy guardian, 0 + 20; Cerberus lure,
20 + 80; Hidden Treasure guardian, 20 + 80) draw a float32 angle, then a
float32 radius, before entering the world factory. Its heading is the
truncated angle word, not a normalized movement heading. The factory's
spawn base arms the ordinary timers too: the Ivy guardian
MOB_QT_02_PUNISHER_CLON leaves after its 300 s.
================
*/
func (s *MonsterState) SpawnQuestMonster(request QuestMonsterSpawn) bool {
	_, spawned := s.SpawnQuestMonsterGID(request)
	return spawned
}

/*
================
SpawnQuestMonsterGID

SpawnQuestMonster, answering the landed monster's gid for an owner that
follows it (action/pandorabox.go's waves).
================
*/
func (s *MonsterState) SpawnQuestMonsterGID(request QuestMonsterSpawn) (uint32, bool) {
	if !FeaturesAt(request.Position.RegionID).QuestMonsterSpawns {
		return 0, false
	}
	ref, exists := s.ReferenceByCodename(request.Codename)
	if !exists || ref.MaxHP == 0 {
		return 0, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.populationForLease(request.Division, request.Population)
	if state == nil || s.counter >= domain.MaxMonsterGIDCounter {
		return 0, false
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
		return 0, false
	}
	s.counter++
	actor := monster.Instance{
		Gid: monster.GidBase + s.counter, Ref: ref, Spawn: spawn, NestDetached: true,
		SpawnHeading: uint16(angle), Nest: monster.NestRow{SpawnPoint: spawn, PolicyPinned: true},
	}
	if tactics, found := monster.ResolveSummonTactics(ref, request.TacticsGrade, s.random); found {
		actor.Nest.NativeTacticsFlags = tactics.NativeFlags
		actor.Nest.TargetPolicy = tactics.TargetPolicy
		if tactics.HasControls {
			actor.Nest.Controls, actor.Nest.HasControls = tactics.Controls, true
			actor.Nest.ConditionalSkills = tactics.ConditionalSkills
			actor.Nest.SightRange = float64(tactics.Controls.SightRange)
			actor.Nest.Aggressive = tactics.Controls.AggressType == 0
		}
	}
	if from := request.TacticsFrom; from != nil {
		actor.Nest.NativeTacticsFlags, actor.Nest.TargetPolicy = from.NativeTacticsFlags, from.TargetPolicy
		actor.Nest.Controls, actor.Nest.HasControls = from.Controls, from.HasControls
		actor.Nest.ConditionalSkills = from.ConditionalSkills
		actor.Nest.SightRange, actor.Nest.Aggressive = from.SightRange, from.Aggressive
	}
	if request.HasRarity {
		actor.Nest.RarityOverride, actor.Nest.HasRarityOverride = request.Rarity, true
	}
	// The grade decides the maximum (grade 7 is x30), so the monster is
	// filled only once its grade is final: it spawns at full health.
	actor.CurrentHP = actor.EffectiveMaxHP()
	if request.Aggressive && !actor.Nest.HasControls {
		actor.Nest.Aggressive = true
		if actor.Nest.SightRange <= 0 {
			actor.Nest.SightRange = eventAggressiveSight
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
	state.listRegionGid(spawn.RegionID, actor.Gid)
	armLifetimeLocked(state, actor, request.NowMs)
	armQuestLifetimeLocked(state, actor.Gid, request.NowMs+request.LifetimeMs, request.LifetimeMs > 0)
	return actor.Gid, true
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

/*
================
QuestMonsterAlive

Whether the monster gid names still stands, alive, in the division's
populations: a kill, a timeout and a population's retirement all answer
false.
================
*/
func (s *MonsterState) QuestMonsterAlive(division string, gid uint32) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.populationForObject(division, gid)
	if state == nil || !state.instances.contains(gid) {
		return false
	}
	return state.instances.get(gid).CurrentHP > 0
}
