/*
===========================================================================

monsterstate_damage.go - damage plans against monsters

===========================================================================
*/

package simulation

import (
	"math"
	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/monster"
	"sort"
)

/*
==================
CombatCandidatesInSphere

CombatCandidatesInSphere reads only already-materialized populations and
samples all movers at one instant under one lock. It scans linearly, then
sorts the nearby result, avoiding a full-world copy/sort and N lock crossings.
==================
*/
func (s *MonsterState) CombatCandidatesInSphere(divisionID string, center Spawn, reach float64, nowMs int64) []monster.Instance {
	return s.combatCandidates(divisionID, instance.Pack(1, 1), center, reach, nowMs, false)
}

/*
==================
CombatCandidatesForChain

CombatCandidatesForChain uses center-to-center distance and nearest-first
order. Native 58C8DC..58C97A does not add body radii as sphere areas do.
The primary remains the center for a targeted chain; candidates are not
admitted merely because they are near a previously selected secondary.
==================
*/
func (s *MonsterState) CombatCandidatesForChain(divisionID string, center Spawn, reach float64, nowMs int64) []monster.Instance {
	return s.combatCandidates(divisionID, instance.Pack(1, 1), center, reach, nowMs, true)
}

/*
================
CombatCandidatesInWorld
================
*/
func (s *MonsterState) CombatCandidatesInWorld(divisionID string, world instance.ID, center Spawn, reach float64, nowMs int64, nearest bool) []monster.Instance {
	return s.combatCandidates(divisionID, world, center, reach, nowMs, nearest)
}

/*
================
combatCandidates
================
*/
func (s *MonsterState) combatCandidates(divisionID string, world instance.ID, center Spawn, reach float64, nowMs int64, nearest bool) []monster.Instance {
	lease, exists := s.PopulationLease(divisionID, world)
	if !exists {
		return nil
	}
	return s.CombatCandidatesInPopulation(divisionID, lease, center, reach, nowMs, nearest)
}

/*
================
CombatCandidatesInPopulation
================
*/
func (s *MonsterState) CombatCandidatesInPopulation(divisionID string, lease instance.Lease, center Spawn, reach float64, nowMs int64, nearest bool) []monster.Instance {
	if reach < 0 || math.IsNaN(reach) || math.IsInf(reach, 0) {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.populationForLease(divisionID, lease)
	if state == nil {
		return nil
	}
	var out []monster.Instance
	var distances map[uint32]float64
	if nearest {
		distances = make(map[uint32]float64)
	}
	for gid, mover := range state.movers.values() {
		pose := mover.LivePoseAt(nowMs, nil)
		// Region arithmetic masks the dungeon bit. Do not let matching low
		// bits alias indoor and outdoor coordinates into a valid area target.
		if IsDungeonRegion(center.RegionID) != IsDungeonRegion(pose.RegionID) {
			continue
		}
		distance := WorldDistance2D(center, Spawn{RegionID: pose.RegionID, X: pose.X, Y: pose.Y, Z: pose.Z})
		distance = math.Hypot(distance, pose.Y-center.Y)
		limit := reach
		if !nearest {
			_, radius := state.instances.metadata(gid)
			limit += radius
		}
		if distance <= limit {
			instance, exists := state.instances.lookup(gid)
			// A fortress structure takes only a weapon's basic attack aimed
			// at it (CGObjPC_CanAttackTarget 52BF90), so it is never an
			// area, chain or secondary victim.
			if !exists || instance.CurrentHP == 0 || instance.Ref.Structure {
				continue
			}
			out = append(out, instance)
			if nearest {
				distances[gid] = distance
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if nearest && distances[out[i].Gid] != distances[out[j].Gid] {
			return distances[out[i].Gid] < distances[out[j].Gid]
		}
		return out[i].Gid < out[j].Gid
	})
	return out
}

/*
================
MonsterDamagePlan
================
*/
type MonsterDamagePlan struct {
	StatusHit abnormal.HitContext
	// Abnormal holds the statuses 590680 rolled for this impact, applied
	// only if the monster survives it (593F0C after 4FC).
	Abnormal []abnormal.Record
	// Prepared outside character and population mutation doors; never resolved during commit.
	AbnormalSources         map[uint32]MonsterAbnormalSource
	GID, ExpectedHP, Damage uint32
	// CreditGID is the source after authority-owned attribution (for example,
	// a COS owner's GID). Zero represents an unattributed hit.
	CreditGID uint32
	Knockdown *MonsterKnockdownPlan
	Knockback *MonsterKnockdownPlan
}

/*
==================
ApplyDamageBatch

ApplyDamageBatch validates the complete live HP snapshot before any write.
The division's character transaction owns cost/rewards around this single
population door. A stale victim refuses the whole plan, never half an AoE.
==================
*/
func (s *MonsterState) ApplyDamageBatch(divisionID string, plans []MonsterDamagePlan) ([]MonsterDamageResult, bool) {
	if len(plans) == 0 || len(plans) > 255 {
		return nil, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.populationForObject(divisionID, plans[0].GID)
	seen := make(map[uint32]bool, len(plans))
	for _, plan := range plans {
		instance, ok := state.instances.lookup(plan.GID)
		if !ok || seen[plan.GID] {
			return nil, false
		}
		asPlanned := instance.CurrentHP != 0 && instance.CurrentHP == plan.ExpectedHP
		if !asPlanned || !validImpactDisplacement(plan.Knockdown, plan.Knockback) || !validAbnormalSources(plan) {
			return nil, false
		}
		seen[plan.GID] = true
	}
	results := make([]MonsterDamageResult, 0, len(plans))
	for _, plan := range plans {
		results = append(results, s.applyDamageLocked(divisionID, state, plan))
	}
	return results, true
}

/*
==================
ApplyDamageSequences

ApplyDamageSequences is ApplyDamageBatch for multi-impact areas: every
victim's first plan must match its live HP before any write, then each
victim takes its impacts in order until the first fatal one.
==================
*/
func (s *MonsterState) ApplyDamageSequences(divisionID string, sequences [][]MonsterDamagePlan) ([][]MonsterDamageResult, bool) {
	if len(sequences) == 0 || len(sequences) > 255 || len(sequences[0]) == 0 {
		return nil, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.populationForObject(divisionID, sequences[0][0].GID)
	seen := make(map[uint32]bool, len(sequences))
	for _, plans := range sequences {
		if len(plans) == 0 || len(plans) > 255 {
			return nil, false
		}
		gid := plans[0].GID
		instance, ok := state.instances.lookup(gid)
		if !ok || seen[gid] || instance.CurrentHP == 0 || instance.CurrentHP != plans[0].ExpectedHP {
			return nil, false
		}
		for _, plan := range plans {
			if plan.GID != gid || !validImpactDisplacement(plan.Knockdown, plan.Knockback) || !validAbnormalSources(plan) {
				return nil, false
			}
		}
		seen[gid] = true
	}
	results := make([][]MonsterDamageResult, 0, len(sequences))
	for _, plans := range sequences {
		out := make([]MonsterDamageResult, 0, len(plans))
		for _, plan := range plans {
			result := s.applyDamageLocked(divisionID, state, plan)
			out = append(out, result)
			if result.Fatal {
				break
			}
		}
		results = append(results, out)
	}
	return results, true
}

/*
==================
MonsterDamageResult

MonsterDamageResult is the committed outcome of one hit against a
simulation-owned monster. It is a value snapshot: callers cannot mutate the
population by retaining it.
==================
*/
type MonsterDamageResult struct {
	// Population is captured with HP and contributions, before the source
	// can be retired. Reward origin must not infer a default wire world.
	Population instance.Lease
	// Contributions is a detached snapshot delivered only at the fatal transition.
	Contributions []MonsterContribution
	Instance      monster.Instance
	BeforeHP      uint32
	CurrentHP     uint32
	// Damage retains the committed hit for feedback; Applied is only the HP debit.
	Damage  uint32
	Applied uint32
	// Fatal is true only for the hit that transitions a live monster from
	// positive HP to zero. Population removal remains an explicit Defeat
	// lifecycle step so the combat lane can publish the death result first.
	Fatal bool
	// Abnormal reports the break/application consequences of this impact.
	Abnormal  MonsterAbnormalEffects
	Knockdown *MonsterKnockdownPlan
	Knockback *MonsterKnockdownPlan
}

/*
==================
ApplyDamage

ApplyDamage subtracts damage from the one authoritative CurrentHP field
while holding the population lock. Concurrent hits therefore observe one
ordered HP history, Applied records only the HP actually removed, and
exactly one hit can own the fatal transition.

The method deliberately does not call Defeat. Combat must first send the
fatal action-result row (whose death bit drives the client animation), then
advance population lifecycle through Defeat at its evidenced removal point.
==================
*/
func (s *MonsterState) ApplyDamage(
	divisionID string,
	gid uint32,
	damage uint32,
) (MonsterDamageResult, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	state := s.populationForObject(divisionID, gid)
	_, ok := state.instances.lookup(gid)
	if !ok {
		return MonsterDamageResult{}, false
	}
	return s.applyDamageLocked(divisionID, state, MonsterDamagePlan{GID: gid, Damage: damage}), true
}
