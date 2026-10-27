package simulation

import "sort"

// ArmNestFromReward is the event-0 count arm of the retail server's
// aggression update. Reward
// settlement owns when it is called; corpse removal owns the refill timer.
func (s *MonsterState) ArmNestFromReward(division string, gid uint32, count uint16) {
	if count < 2 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.populationForObject(division, gid)
	instance, live := state.instances.lookup(gid)
	if !live || instance.CurrentHP != 0 {
		return
	}
	if index, ok := state.gidNests[gid]; ok {
		state.nests[index].partyArmed = true
	}
}

// MonsterContribution records the damage argument at the NPC damage boundary,
// not HP removed. The retail server adds that dword before
// forwarding to its hit processing; upstream damage calculation is a separate contract.
type MonsterContribution struct {
	CreditGID uint32
	Damage    uint32
}

// Callers hold MonsterState.mu, including validation and the corresponding HP
// write. There is no separate public recording API that could credit a refused
// hit or race the fatal snapshot. Native dword addition wraps on overflow.
func (state *divisionMonsterState) recordContribution(gid, creditGID, damage uint32) {
	if creditGID == 0 || damage == 0 {
		return
	}
	if state.contributions == nil {
		state.contributions = make(map[uint32]map[uint32]uint32)
	}
	if state.contributions[gid] == nil {
		state.contributions[gid] = make(map[uint32]uint32)
	}
	state.contributions[gid][creditGID] += damage
}

func (state *divisionMonsterState) contributionSnapshot(gid uint32) []MonsterContribution {
	var out []MonsterContribution
	for actor, damage := range state.contributions[gid] {
		out = append(out, MonsterContribution{CreditGID: actor, Damage: damage})
	}
	// The source map is ordered by unsigned object ID in the research binary.
	// Reward-group ordering is a different container and must not use this sort.
	sort.Slice(out, func(i, j int) bool { return out[i].CreditGID < out[j].CreditGID })
	return out
}
