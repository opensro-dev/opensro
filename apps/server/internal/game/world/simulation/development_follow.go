/*
===========================================================================

development_follow.go - scripted actors for the follow fixture (development only)

===========================================================================
*/

package simulation

import (
	"fmt"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
)

/*
==================
DevelopmentCreateLeader

DevelopmentCreateLeader is a non-respawning scripted actor, using a shipped
reference and the production spawn-ground resolver. No child factory lives
here: children must be released by BeginSummon/AdvanceSummons.
==================
*/
func (s *MonsterState) DevelopmentCreateLeader(division string, refID uint32, pose monster.Pose, until int64) (monster.Instance, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ref, ok := s.template.Refs[refID]
	if !ok || s.counter >= domain.MaxMonsterGIDCounter {
		return monster.Instance{}, fmt.Errorf("invalid fixture reference or GID capacity")
	}
	spawn := normalizeGeneratedMonsterSpawn(monster.SpawnPoint{RefObjID: refID, RegionID: pose.RegionID, X: pose.X, Y: pose.Y, Z: pose.Z})
	spawn, ok = s.resolveSpawnGround(spawn, pose.Y)
	if !ok {
		return monster.Instance{}, fmt.Errorf("fixture leader ground rejected")
	}
	s.counter++
	instance := monster.Instance{Gid: monster.GidBase + s.counter, Ref: ref, Spawn: spawn, Nest: monster.NestRow{SpawnPoint: spawn, PolicyPinned: true}}
	instance.CurrentHP = instance.EffectiveMaxHP()
	mover := monster.NewSpawnMover(instance, s.nowMillis())
	mover.Activity = monster.NewActivityCadence(uint32(s.nowMillis()), s.randomWord())
	mustMoverTransition(&mover, monster.MoverEventSpawnHoldElapsed, 0)
	mover.BehaviorDeadlineMs = until
	state := s.division(division)
	if state.movers == nil {
		state.movers = newMoverStorage(nil)
	}
	state.instances.set(instance.Gid, instance)
	state.movers.set(instance.Gid, mover)
	state.behavior.set(instance.Gid, 0)
	state.byRegion[spawn.RegionID] = append(state.byRegion[spawn.RegionID], instance.Gid)
	return instance, nil
}

// Script only the fixture leader. Children always use ordinary MonsterMoverOps.
/*
================
DevelopmentMoveLeader
================
*/
func (ops *MonsterMoverOps) DevelopmentMoveLeader(division string, gid uint32, destination monster.Pose, now int64) ([]Frame, error) {
	instance, ok := ops.Monsters.Get(division, gid)
	if !ok || instance.CurrentHP == 0 {
		return nil, fmt.Errorf("leader absent/dead")
	}
	if now < instance.SummonActionUntilMs {
		return nil, fmt.Errorf("summon recovery still owns leader")
	}
	mover, ok := ops.Monsters.Mover(division, gid)
	if !ok {
		return nil, fmt.Errorf("leader mover absent")
	}
	if mover.Mode() == monster.MoverWandering {
		mover.Pose = mover.LivePoseAt(now, ops.TerrainHeight)
		mover.From, mover.To = monster.Pose{}, monster.Pose{}
		mover.DepartMs, mover.ArriveMs = 0, 0
		mustMoverTransition(&mover, monster.MoverEventSegmentArrived, 0)
	}
	if mover.Mode() != monster.MoverIdle {
		return nil, fmt.Errorf("leader not script-idle")
	}
	mustMoverTransition(&mover, monster.MoverEventStartWander, 0)
	return ops.commitSegment(division, instance, mover, destination, instance.RunSpeed(), wire.MoveStateRun, now), nil
}

/*
================
DevelopmentStopLeader
================
*/
func (ops *MonsterMoverOps) DevelopmentStopLeader(division string, gid uint32, now, until int64) []Frame {
	mover, ok := ops.Monsters.Mover(division, gid)
	if !ok {
		return nil
	}
	if mover.Mode() == monster.MoverIdle {
		mover.BehaviorDeadlineMs = until
		ops.Monsters.CommitMover(division, gid, mover)
		return nil
	}
	if mover.Mode() != monster.MoverWandering {
		return nil
	}
	mover.Pose = mover.LivePoseAt(now, ops.TerrainHeight)
	mover.From, mover.To = monster.Pose{}, monster.Pose{}
	mover.DepartMs, mover.ArriveMs = 0, 0
	mustMoverTransition(&mover, monster.MoverEventSegmentArrived, 0)
	mover.BehaviorDeadlineMs = until
	return ops.Monsters.CommitMoverFrames(division, gid, mover, []Frame{correctionFrame(gid, mover.Pose)})
}

/*
================
DevelopmentActorSnapshot
================
*/
type DevelopmentActorSnapshot struct {
	SelfEffects     monster.SelfEffects `json:"selfEffects"`
	ConditionalUsed uint8               `json:"conditionalUsed"`
	ActionUntilMs   int64               `json:"actionUntilMs"`
	GID             uint32              `json:"gid"`
	Reference       uint32              `json:"reference"`
	Summoner        uint32              `json:"summoner"`
	Name            string              `json:"name"`
	Mode            string              `json:"mode"`
	Event           string              `json:"event"`
	Serial          uint64              `json:"serial"`
	Target          uint32              `json:"target"`
	HP              uint32              `json:"hp"`
	Pose            monster.Pose        `json:"pose"`
	To              monster.Pose        `json:"to"`
	ArriveMs        int64               `json:"arriveMs"`
	FollowRange     float64             `json:"followRange"`
	Timer0, Timer7  monster.AITimerEntry
}

/*
================
DevelopmentFollowSnapshot
================
*/
func (s *MonsterState) DevelopmentFollowSnapshot(division string, leader uint32, now int64) []DevelopmentActorSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.division(division)
	var out []DevelopmentActorSnapshot
	// Unique leaders and summoned children cannot enter dormant storage.
	// Do not deserialize the entire sleeping world for each fixture observation.
	for gid, resident := range state.instances.hot {
		if gid != leader && resident.summonerGID != leader {
			continue
		}
		instance := state.instances.hotValue(gid, resident)
		m := state.movers.get(gid)
		row := DevelopmentActorSnapshot{GID: gid, Reference: instance.Ref.RefObjID, Summoner: instance.SummonerGID, Name: instance.Ref.Name, HP: instance.CurrentHP, Mode: m.Mode().String(), Event: m.LastEvent().String(), Serial: m.TransitionSerial(), Target: m.TargetGID(), Pose: m.LivePoseAt(now, nil), To: m.To, ArriveMs: m.ArriveMs, FollowRange: instance.SummonerFollowRange}
		if timers := state.aiTimer(gid); timers != nil {
			row.Timer0 = timers.GetTimer(0)
			row.Timer7 = timers.GetTimer(7)
		}
		row.ActionUntilMs = instance.SummonActionUntilMs
		row.SelfEffects = instance.SelfEffects
		row.ConditionalUsed = instance.ConditionalUsed
		out = append(out, row)
	}
	return out
}

/*
================
DevelopmentRemoveFamily
================
*/
func (s *MonsterState) DevelopmentRemoveFamily(division string, leader uint32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.division(division)
	for gid, resident := range state.instances.hot {
		if gid == leader || resident.summonerGID == leader {
			instance := state.instances.hotValue(gid, resident)
			state.instances.remove(gid)
			delete(state.contributions, gid)
			state.releaseApproachActor(gid)
			state.movers.remove(gid)
			state.behavior.remove(gid)
			delete(state.aiTimers, gid)
			delete(state.storedAITimers, gid)
			delete(state.pendingSummons, gid)
			removeRegionGid(state.byRegion, instance.Spawn.RegionID, gid)
		}
	}
}

/*
================
DevelopmentRemoveLeader
================
*/
func (s *MonsterState) DevelopmentRemoveLeader(division string, leader uint32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.division(division)
	if instance, ok := state.instances.lookup(leader); ok {
		state.instances.remove(leader)
		delete(state.contributions, leader)
		state.releaseApproachActor(leader)
		state.movers.remove(leader)
		state.behavior.remove(leader)
		delete(state.aiTimers, leader)
		delete(state.storedAITimers, leader)
		delete(state.pendingSummons, leader)
		removeRegionGid(state.byRegion, instance.Spawn.RegionID, leader)
	}
}
