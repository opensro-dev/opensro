package simulation

import (
	"maps"
	"math"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/world/monster"
	"sort"
)

// CommitSummon owns both consumption of the AI damage accumulator and all
// generated instances. No independently respawning nest slots are created.
// Geometry rejection skips that spawn, as the native factory may return null;
// malformed references refuse before consuming the encounter transition.
func (s *MonsterState) CommitSummon(divisionID string, expected monster.Instance, skill monster.SummonSkill, nowMs, endsAtMs int64, actionRanges map[uint32]float64) ([]monster.Instance, bool) {
	return s.BeginSummon(divisionID, expected, skill, nowMs, nowMs, endsAtMs, actionRanges)
}

type pendingMonsterSummon struct {
	Skill        monster.SummonSkill
	ReleaseAtMs  int64
	ActionRanges map[uint32]float64
}

// BeginSummon reserves the action immediately. Its world factory runs only at
// the casting boundary (586700 -> 593540 -> 596E50), never at cast admission.
func (s *MonsterState) BeginSummon(divisionID string, expected monster.Instance, skill monster.SummonSkill, nowMs, releaseAtMs, endsAtMs int64, actionRanges map[uint32]float64) ([]monster.Instance, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.populationForObject(divisionID, expected.Gid)
	parent, ok := state.instances.lookup(expected.Gid)
	if _, pending := state.pendingSummons[expected.Gid]; pending {
		return nil, false
	}
	if !ok || releaseAtMs < nowMs || endsAtMs < releaseAtMs || !skill.Present || !monster.SummonDue(parent) || parent.CurrentHP != expected.CurrentHP || parent.DamageSinceSummon != expected.DamageSinceSummon {
		return nil, false
	}
	if reach, exists := actionRanges[parent.Ref.RefObjID]; !exists || reach < 0 || math.IsNaN(reach) || math.IsInf(reach, 0) {
		return nil, false
	}
	for _, entry := range skill.Entries {
		if entry.RefObjID == 0 {
			continue
		}
		ref, exists := s.template.Refs[entry.RefObjID]
		if !exists || ref.MaxHP == 0 || entry.Maximum < entry.Minimum || entry.Maximum > math.MaxInt32 {
			return nil, false
		}
	}
	if _, ok := state.movers.lookup(parent.Gid); !ok {
		return nil, false
	}
	parent.SummonActionUntilMs = endsAtMs
	// 59B755 returns from starting the action, then 59B787 dispatches event
	// 4; 558FE5 -> 562540 consumes ssou damage NOW, not at release/recovery.
	parent.DamageSinceSummon = 0
	parent.LastSummonCommandMs = uint32(nowMs)
	if endsAtMs == nowMs {
		parent.SummonActionUntilMs = 0
	}
	if releaseAtMs > nowMs {
		if state.pendingSummons == nil {
			state.pendingSummons = make(map[uint32]pendingMonsterSummon)
		}
		state.pendingSummons[parent.Gid] = pendingMonsterSummon{Skill: skill, ReleaseAtMs: releaseAtMs, ActionRanges: maps.Clone(actionRanges)}
		state.instances.set(parent.Gid, parent)
		return nil, true
	}
	return s.createSummonLocked(state, parent, skill, nowMs, actionRanges)
}

func (s *MonsterState) createSummonLocked(state *divisionMonsterState, parent monster.Instance, skill monster.SummonSkill, nowMs int64, actionRanges map[uint32]float64) ([]monster.Instance, bool) {
	mover, ok := state.movers.lookup(parent.Gid)
	if !ok {
		return nil, false
	}
	pose := mover.LivePoseAt(nowMs, nil)
	var created []monster.Instance
	for _, entry := range skill.Entries {
		if entry.RefObjID == 0 {
			continue
		}
		ref := s.template.Refs[entry.RefObjID]
		// 597024 consumes one heading draw per tuple, including zero counts.
		headingFraction := float32(float64(monster.SummonRandomWord(s.random())) / 32767)
		heading := float64(float32(float64(headingFraction) * 6.2831854820251465))
		count := min(monster.SummonRandomWord(s.random())%(entry.Maximum-entry.Minimum+1)+entry.Minimum, 50)
		radius := parent.BodyRadius() + ref.BodyRadius + 30
		for i := uint32(0); i < count; i++ {
			if s.counter >= domain.MaxMonsterGIDCounter {
				break
			}
			// 531240: 70/101 outer third, 20/101 middle, 11/101 inner;
			// then a uniform radius within that third and a uniform angle.
			third := float64(float32(radius / 3))
			roll := monster.SummonRandomWord(s.random()) % 101
			base := 0.0
			if roll < 70 {
				base = 2 * third
			} else if roll < 90 {
				base = third
			}
			distance := float64(float32(base + float64(monster.SummonRandomWord(s.random()))/32767*third))
			angle := float64(float32(float64(monster.SummonRandomWord(s.random())) / 32767 * 6.2831854820251465))
			spawn := normalizeGeneratedMonsterSpawn(monster.SpawnPoint{RefObjID: ref.RefObjID, RegionID: pose.RegionID, X: pose.X + math.Cos(angle)*distance, Y: pose.Y, Z: pose.Z + math.Sin(angle)*distance})
			spawn, valid := s.resolveSpawnGround(spawn, pose.Y)
			if !valid {
				continue
			}
			s.counter++
			// Native 5977xx passes no CNest to CMonster_SpawnInstance. The
			// authored tactics must not turn this generated pose into a home nest.
			child := monster.Instance{Gid: monster.GidBase + s.counter, Ref: ref, Spawn: spawn, SummonerGID: parent.Gid, NestDetached: true,
				Nest: monster.NestRow{SpawnPoint: spawn, HasRarityOverride: true, RarityOverride: entry.Grade & 15}}
			child.CurrentHP = child.EffectiveMaxHP()
			if tactics, found := monster.ResolveSummonTactics(ref, entry.Grade, s.random); found {
				child.SummonSightRange = tactics.SightRange
				child.Nest.NativeTacticsFlags = tactics.NativeFlags
				child.Nest.TargetPolicy = tactics.TargetPolicy
				if tactics.HasControls {
					child.Nest.Controls, child.Nest.HasControls = tactics.Controls, true
					child.Nest.ConditionalSkills = tactics.ConditionalSkills
					child.Nest.SightRange = float64(tactics.Controls.SightRange)
					child.Nest.Aggressive = tactics.Controls.AggressType == 0
				}
			}
			child.SummonerFollowRange = 200 + actionRanges[parent.Ref.RefObjID]
			state.instances.set(child.Gid, child)
			armLifetimeLocked(state, child, nowMs)
			childMover := monster.NewSpawnMover(child, nowMs)
			childMover.Activity = monster.NewActivityCadence(uint32(nowMs), s.randomWord())
			childMover.Pose.Heading = HeadingWordFromRadians(heading)
			// 5977DF..5977FF -> 5591B0: actual CSNM 11, mode 2 producer.
			// The summon provenance alone is not the CTactics control binding.
			if err := childMover.BindController(monster.ControlSummoned, parent.Gid); err != nil {
				panic(err)
			}
			childMover.BehaviorDeadlineMs = nowMs + monster.NativeIdleDelayMs(s.randomWord)
			state.movers.set(child.Gid, childMover)
			state.behavior.set(child.Gid, 0)
			state.byRegion[spawn.RegionID] = append(state.byRegion[spawn.RegionID], child.Gid)
			created = append(created, child)
		}
	}
	parent = finishSummonAction(parent, nowMs)
	state.instances.set(parent.Gid, parent)
	return created, true
}

// Recovery releases the action reservation. Selector completion already
// consumed its accumulator at command admission; later hits belong to the
// next decision, including hits received while the cast was still preparing.
func finishSummonAction(instance monster.Instance, nowMs int64) monster.Instance {
	if instance.SummonActionUntilMs != 0 && nowMs >= instance.SummonActionUntilMs {
		instance.SummonActionUntilMs = 0
	}
	return instance
}

// RejectSummonCommand applies 558F70(-1) -> 562540's unique-selector effect.
// It creates no children/action reservation and preserves target and motion.
// Ordinary selectors do not own this accumulator transition.
func (s *MonsterState) RejectSummonCommand(divisionID string, gid uint32, nowMs int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.populationForObject(divisionID, gid)
	parent, exists := state.instances.lookup(gid)
	if !exists || parent.CurrentHP == 0 || monster.UniqueSummonPolicy(parent.Ref.Codename) == monster.NoSummonPolicy {
		return
	}
	parent.DamageSinceSummon = 0
	parent.LastSummonCommandMs = uint32(nowMs)
	state.instances.set(gid, parent)
}

// AdvanceSummons is a simulation-clock transition, not a side effect of reads.
// A dead or retired caster cannot release a pending wave. Ordered GIDs retain
// deterministic entropy/identity allocation when several casts mature together.
func (s *MonsterState) AdvanceSummons(nowMs int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, key := range s.populationKeys() {
		state := s.populationForLease(key.division, key.lease)
		gids := make([]uint32, 0, len(state.pendingSummons))
		for gid, pending := range state.pendingSummons {
			if nowMs >= pending.ReleaseAtMs {
				gids = append(gids, gid)
			}
		}
		sort.Slice(gids, func(i, j int) bool { return gids[i] < gids[j] })
		for _, gid := range gids {
			pending := state.pendingSummons[gid]
			delete(state.pendingSummons, gid)
			parent, ok := state.instances.lookup(gid)
			if !ok || parent.CurrentHP == 0 {
				continue
			}
			s.createSummonLocked(state, parent, pending.Skill, nowMs, pending.ActionRanges)
		}
	}
}
