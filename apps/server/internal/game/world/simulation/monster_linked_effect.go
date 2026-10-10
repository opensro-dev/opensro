/*
===========================================================================

monster_linked_effect.go - atomic publication of linked recipient identities

No damage or duration lives here. The action owner installs/removes projection
rows so both bootstrap and live interest serialize the same active effects.

===========================================================================
*/

package simulation

import "opensro.online/server/internal/game/world/monster"

const maxMonsterSpawnSkills = 255

/*
================
MonsterLinkedEffect

An admitted recipient identity, independent of its source command.
================
*/
type MonsterLinkedEffect struct {
	GID    uint32
	Effect monster.AttachedSkill
}

/*
================
InstallMonsterLinkedEffects

Validate every recipient before mutating any projection. The count includes
self buffs because both producers share the spawn row's single byte count.
================
*/
func (s *MonsterState) InstallMonsterLinkedEffects(division string, plans []MonsterLinkedEffect) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	seen := make(map[uint32]bool, len(plans))
	for _, plan := range plans {
		state := s.populationForObject(division, plan.GID)
		row, exists := state.instances.lookup(plan.GID)
		if !exists || row.CurrentHP == 0 || plan.Effect.SkillID == 0 || plan.Effect.Token == 0 || seen[plan.GID] {
			return false
		}
		seen[plan.GID] = true
		if spawnEffectCount(row) >= maxMonsterSpawnSkills {
			return false
		}
	}
	for _, plan := range plans {
		state := s.populationForObject(division, plan.GID)
		row := state.instances.get(plan.GID)
		row.LinkedEffects = row.LinkedEffects.With(plan.Effect)
		state.instances.set(plan.GID, row)
	}
	return true
}

/*
================
RemoveMonsterLinkedEffect

Removed/dead actors tolerate a late pair teardown; tokens are never recycled
into another actor's projection.
================
*/
func (s *MonsterState) RemoveMonsterLinkedEffect(division string, gid, token uint32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.populationForObject(division, gid)
	row, exists := state.instances.lookup(gid)
	if !exists {
		return
	}
	row.LinkedEffects = row.LinkedEffects.Without(token)
	state.instances.set(gid, row)
}
