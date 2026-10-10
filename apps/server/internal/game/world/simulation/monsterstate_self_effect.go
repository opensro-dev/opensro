package simulation

import (
	"opensro.online/server/internal/game/world/monster"
	"sort"
)

// SelectConditionalSkill consumes a health entry before command admission,
// in RefObjChar default-slot registration order (53F780/561A50). No RNG.
// No active archive scan: caller names the live actor it is deciding for.
func (s *MonsterState) SelectConditionalSkill(division string, gid uint32) (uint32, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.populationForObject(division, gid)
	r, ok := state.instances.hot[gid]
	if !ok {
		return 0, false
	}
	instance := state.instances.hotValue(gid, r)
	if instance.CurrentHP == 0 {
		return 0, false
	}
	for _, skill := range instance.Ref.DefaultSkillIDs {
		if skill == 0 {
			continue
		}
		for slot, condition := range instance.Nest.ConditionalSkills {
			if condition.SkillID != skill || condition.ConditionType != 0 || instance.ConditionalUsed&(1<<slot) != 0 {
				continue
			}
			if monster.ConditionalHealthEligible(instance.CurrentHP, instance.EffectiveMaxHP(), condition.Data) {
				instance.ConditionalUsed |= 1 << slot
				state.instances.set(gid, instance)
				return skill, true
			}
		}
	}
	return 0, false
}

func (s *MonsterState) InstallMonsterSelfEffect(division string, gid uint32, effect monster.SelfEffect, now int64) bool {
	if effect.Token == 0 || effect.SkillID == 0 || effect.UntilMs <= now {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.populationForObject(division, gid)
	r, ok := state.instances.hot[gid]
	if !ok {
		return false
	}
	i := state.instances.hotValue(gid, r)
	if i.CurrentHP == 0 || i.Motion.StateAt(now) != 0 {
		return false
	}
	// A consumed conditional can install only its own default skill once.
	slot := -1
	for n, c := range i.Nest.ConditionalSkills {
		if c.SkillID == effect.SkillID && i.ConditionalUsed&(1<<n) != 0 {
			slot = n
			break
		}
	}
	if slot < 0 || i.SelfEffects[slot].Token != 0 {
		return false
	}
	i.SelfEffects[slot] = effect
	state.instances.set(gid, i)
	return true
}

/*
================
InstallMonsterTargetEffect

Another actor's buff instance on a living monster (Vital Spot's bbuf). An
instance with the same program tag is replaced: INFERENCE, the recast
refreshes the same skill line, as a player's same-group buff does. The
replaced token is returned so the caller can retire it on the wire. A
monster already holding eight such instances refuses the ninth.
================
*/
func (s *MonsterState) InstallMonsterTargetEffect(division string, gid uint32, effect monster.SelfEffect, now int64) (uint32, bool) {
	if effect.Token == 0 || effect.SkillID == 0 || effect.UntilMs <= now {
		return 0, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.populationForObject(division, gid)
	r, ok := state.instances.hot[gid]
	if !ok {
		return 0, false
	}
	i := state.instances.hotValue(gid, r)
	if i.CurrentHP == 0 {
		return 0, false
	}
	slot, replaced := -1, uint32(0)
	for n, e := range i.TargetEffects {
		if e.Token != 0 && e.Tag == effect.Tag {
			slot, replaced = n, e.Token
			break
		}
		if e.Token == 0 && slot < 0 {
			slot = n
		}
	}
	if slot < 0 || replaced == 0 && spawnEffectCount(i) >= maxMonsterSpawnSkills {
		return 0, false
	}
	i.TargetEffects[slot] = effect
	state.instances.set(gid, i)
	return replaced, true
}

/*
================
spawnEffectCount

The effect records the create row carries (BuildMonsterCreateRow): every
self, target and linked instance. The row's count is one byte.
================
*/
func spawnEffectCount(i monster.Instance) int {
	count := i.LinkedEffects.Len()
	for _, e := range i.SelfEffects {
		if e.Token != 0 {
			count++
		}
	}
	for _, e := range i.TargetEffects {
		if e.Token != 0 {
			count++
		}
	}
	return count
}

type MonsterSelfEffectRetirement struct {
	DivisionID string
	GID        uint32
	Tokens     []uint32
}

// Expiry/death retires only indexed effects. Unrelated sleeping actors are
// never read or deserialized; removed actors lose their sparse record too.
func (s *MonsterState) RetireMonsterSelfEffects(now int64) []MonsterSelfEffectRetirement {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []MonsterSelfEffectRetirement
	for _, key := range s.populationKeys() {
		state := s.populationForLease(key.division, key.lease)
		owners := make(map[uint32]bool, len(state.instances.selfEffects)+len(state.instances.targetEffects))
		for gid := range state.instances.selfEffects {
			owners[gid] = true
		}
		for gid := range state.instances.targetEffects {
			owners[gid] = true
		}
		for gid := range owners {
			r, ok := state.instances.hot[gid]
			if !ok {
				panic("monster effect lost resident owner")
			}
			i := state.instances.hotValue(gid, r)
			row := MonsterSelfEffectRetirement{DivisionID: key.division, GID: gid}
			for n, e := range i.SelfEffects {
				if e.Token != 0 && (i.CurrentHP == 0 || !e.Active(now)) {
					row.Tokens = append(row.Tokens, e.Token)
					i.SelfEffects[n] = monster.SelfEffect{}
				}
			}
			for n, e := range i.TargetEffects {
				if e.Token != 0 && (i.CurrentHP == 0 || !e.Active(now)) {
					row.Tokens = append(row.Tokens, e.Token)
					i.TargetEffects[n] = monster.SelfEffect{}
				}
			}
			if len(row.Tokens) > 0 {
				state.instances.set(gid, i)
				out = append(out, row)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].DivisionID != out[j].DivisionID {
			return out[i].DivisionID < out[j].DivisionID
		}
		return out[i].GID < out[j].GID
	})
	return out
}
