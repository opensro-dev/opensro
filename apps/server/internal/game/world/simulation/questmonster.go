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

// The v1.188 servers' three-band landing rings: a rand() % 101 roll below
// the outer cut lands in the outer third, below the inner cut in the
// middle third, else in the inner third. The event spawner cuts at 50 and
// 80; the monster factory's own radius sample at 70 and 90.
const (
	ringRollSpan        = 101
	ringThirds          = 3
	eventRingOuterCut   = 50
	eventRingInnerCut   = 80
	factoryRingOuterCut = 70
	factoryRingInnerCut = 90
)

/*
================
SpawnRing

A native three-band landing ring: Radius, and the roll cuts that pick
its outer and middle thirds. The zero ring is none.
================
*/
type SpawnRing struct {
	Radius   float64
	OuterCut uint32
	InnerCut uint32
}

/*
================
EventSpawnerRing

The v1.188 event spawner's ring of radius (the Pandora waves): half
the monsters stand in the outer third, so a wave surrounds its centre
rather than piling on it.
================
*/
func EventSpawnerRing(radius float64) SpawnRing {
	return SpawnRing{Radius: radius, OuterCut: eventRingOuterCut, InnerCut: eventRingInnerCut}
}

/*
================
FactoryRing

The v1.188 monster factory's radius sample (its spawn entry hands the
radius to the banded sampler before it clamps the point to a loaded
region): seven in ten land in the outer third. The Pandora box lands on
one of 20 from its opener.
================
*/
func FactoryRing(radius float64) SpawnRing {
	return SpawnRing{Radius: radius, OuterCut: factoryRingOuterCut, InnerCut: factoryRingInnerCut}
}

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
Ring, when set, replaces RadiusMin and RadiusSpan with a native
three-band ring (SpawnRing), and a point the ground refuses lands on
Position instead, as the event spawner's does (the Pandora waves and
box, action/pandorabox.go; inference for the factory ring: the native
factory clamps its point into a loaded region rather than refuse it, and
the centre is the nearest such point the port knows). BodyStatus is the
body mode the monster spawns in (monster.Instance BodyStatus).
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
	// Ring is a native three-band landing ring (see the banner).
	Ring         SpawnRing
	BodyStatus   uint8
	LifetimeMs   int64
	TacticsFrom  *monster.NestRow
	Rarity       uint8
	HasRarity    bool
	Aggressive   bool
	TacticsGrade uint8
	// AvoidClosedObjects redraws the landing point (up to questSpawnAttempts times) while it is
	// inside a closed object, so the monster can be hit (port-only, not native: the retail
	// scripts take their first draw). Ignored until SetSpawnClosedObjectCheck is wired.
	AvoidClosedObjects bool
	// AnnounceUnique is the v1.188 in-layer spawner's announce argument
	// (it passes 1 to the monster factory): a rarity-3 kind tells the
	// division it appeared.
	AnnounceUnique bool
}

// questSpawnAttempts bounds the redraws of an AvoidClosedObjects spawn; the last draw stands.
const questSpawnAttempts = 24

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
QuestMonsterSpawn.atNamedPoint

Whether the monster lands on the point the owner named, with no radius
sample. A cave floor refuses the outdoor radius sample (PlaneFeatures.
QuestMonsterSpawns), but a named point samples nothing: it is grounded as
a cave nest's own respawn is (the same normalize and ground pair), which
is how the temple's Haroeris lands in his sanctum (action/
temple_sanctum.go).
================
*/
func (request QuestMonsterSpawn) atNamedPoint() bool {
	return request.FixedPosition && request.RadiusMin == 0 && request.RadiusSpan == 0 && request.Ring.Radius == 0
}

/*
================
SpawnRing.distance

The landing distance: band is the rand() roll that picks the outer,
middle or inner third by the ring's cuts; fraction is the uniform draw
that places the point inside that third. float32 steps as natively.
================
*/
func (ring SpawnRing) distance(band, fraction uint32) float64 {
	third := float64(float32(ring.Radius / ringThirds))
	base := third
	switch roll := band % ringRollSpan; {
	case roll < ring.OuterCut:
		base = third + third
	case roll >= ring.InnerCut:
		base = 0
	}
	return float64(float32(float64(float32(float64(fraction)/nativeRandomMaximum))*third + base))
}

/*
================
SpawnQuestMonsterGID

SpawnQuestMonster, answering the landed monster's gid for an owner that
follows it (action/pandorabox.go's waves).
================
*/
func (s *MonsterState) SpawnQuestMonsterGID(request QuestMonsterSpawn) (uint32, bool) {
	if !FeaturesAt(request.Position.RegionID).QuestMonsterSpawns && !request.atNamedPoint() {
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
	center := request.Position
	var (
		angle float64
		spawn monster.SpawnPoint
	)
	for attempt := 1; ; attempt++ {
		angleFraction := float32(float64(s.randomWord()) / nativeRandomMaximum)
		angle = float64(float32(float64(angleFraction) * nativeCircleRadians))
		var radius float64
		if request.Ring.Radius > 0 {
			radius = request.Ring.distance(s.randomWord(), s.randomWord())
		} else {
			radiusFraction := float32(float64(s.randomWord()) / nativeRandomMaximum)
			radius = float64(float32(float64(radiusFraction)*request.RadiusSpan + request.RadiusMin))
		}
		spawn = normalizeGeneratedMonsterSpawn(monster.SpawnPoint{
			RefObjID: ref.RefObjID, RegionID: center.RegionID,
			X: center.X + math.Cos(angle)*radius, Y: center.Y, Z: center.Z + math.Sin(angle)*radius,
		})
		grounded, valid := s.resolveSpawnGround(spawn, center.Y)
		if !valid && request.Ring.Radius > 0 {
			// A ring lands a refused point on its centre.
			spawn = normalizeGeneratedMonsterSpawn(monster.SpawnPoint{
				RefObjID: ref.RefObjID, RegionID: center.RegionID, X: center.X, Y: center.Y, Z: center.Z,
			})
			grounded, valid = s.resolveSpawnGround(spawn, center.Y)
		}
		if !valid {
			return 0, false
		}
		spawn = grounded
		if !request.AvoidClosedObjects || s.closedObject == nil || attempt >= questSpawnAttempts ||
			!s.closedObject(Spawn{RegionID: spawn.RegionID, X: spawn.X, Y: spawn.Y, Z: spawn.Z}) {
			break
		}
	}
	s.counter++
	actor := monster.Instance{
		Gid: monster.GidBase + s.counter, Ref: ref, Spawn: spawn, NestDetached: true,
		SpawnHeading: uint16(angle), Nest: monster.NestRow{SpawnPoint: spawn, PolicyPinned: true},
		BodyStatus: request.BodyStatus,
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
	if request.AnnounceUnique && actor.IsUnique() {
		state.uniqueNotices = append(state.uniqueNotices, uniqueNotice(uniqueNoticeAppear, ref.RefObjID, ""))
		state.markUniqueSpawn(ref.RefObjID, actor.Gid, spawn.RegionID, request.NowMs)
	}
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

/*
================
ConsumeMonsterHP

The native resource cost on a monster (the v1.188 character's consume
door, which the Pandora job drains its box through): cost takes HP, but
a cost that would reach zero leaves 1, so a cost never kills. The HP
moves under the combat cause (4), so the monster's vitals publish on the
next pass as a hit's do. Returns the HP left; false when gid stands in
no population or is dead.
================
*/
func (s *MonsterState) ConsumeMonsterHP(division string, gid, cost uint32) (uint32, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state, slot := s.locateObject(division, gid)
	if state == nil {
		return 0, false
	}
	row := state.instances.slotValue(gid, &slot)
	if row.CurrentHP == 0 {
		return 0, false
	}
	if cost >= row.CurrentHP {
		cost = row.CurrentHP - 1
	}
	if cost == 0 {
		return row.CurrentHP, true
	}
	row.CurrentHP -= cost
	state.instances.set(gid, row)
	s.noteVitalsLocked(division, gid, VitalsSourceCombatDamage)
	return row.CurrentHP, true
}

/*
================
ExpireQuestMonster

Ends a quest or event monster's life at nowMs: its removal timer falls
due, so the next lifetime pass removes it as a timed-out monster leaves
(no death, no loot) and the interest pass tells its viewers. The Pandora
box leaves this way when its event ends.
================
*/
func (s *MonsterState) ExpireQuestMonster(division string, gid uint32, nowMs int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.populationForObject(division, gid)
	if state == nil || !state.instances.contains(gid) {
		return
	}
	armQuestLifetimeLocked(state, gid, nowMs, true)
}
