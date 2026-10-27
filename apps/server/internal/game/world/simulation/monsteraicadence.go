package simulation

import "opensro.online/server/internal/game/world/monster"

// AI cadence belongs to MonsterState, independently of mover snapshots. A
// rejected/stale movement commit must not rewind a consumed scan deadline.
// Timers survive idle/wander/combat transitions and disappear with their GID.
// This is a Go simulation adapter to the bounded retail-server timer model, not an
// activation of a native C++ replacement or a claim of v1.150 equivalence.
func (s *MonsterState) checkAITimer(divisionID string, gid uint32, id monster.AITimerID, nowMs int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.populationForObject(divisionID, gid)
	instance, exists := state.instances.lookup(gid)
	if !exists || instance.CurrentHP == 0 {
		return false
	}
	return s.aiTimersLocked(state, instance, nowMs).CheckTimer(id, uint32(nowMs))
}

func (s *MonsterState) selectedAITimerReady(divisionID string, gid uint32, nowMs int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.populationForObject(divisionID, gid)
	instance, exists := state.instances.lookup(gid)
	if !exists || instance.CurrentHP == 0 {
		return false
	}
	return s.aiTimersLocked(state, instance, nowMs).CheckSelectedTimer(uint32(nowMs))
}

// CompleteMonsterSkillCommand owns the event-4 success timer independently
// of detached mover plans. Retaliation can replace a target without rewinding
// an accepted command's timer. Called inside the division action transaction.
func (s *MonsterState) CompleteMonsterSkillCommand(divisionID string, gid uint32, duration uint32, nowMs int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.populationForObject(divisionID, gid)
	instance, exists := state.instances.lookup(gid)
	if !exists || instance.CurrentHP == 0 {
		return
	}
	s.aiTimersLocked(state, instance, nowMs).SetTimer(10, duration, 0, uint32(nowMs), true,
		func() uint32 { return monster.SummonRandomWord(s.random()) })
}

// Lazy entity initialization is distinct from consuming a decision's timer.
// Callers hold s.mu. FOLLOW stages CheckTimer on a detached value until commit.
func (s *MonsterState) aiTimersLocked(state *divisionMonsterState, instance monster.Instance, nowMs int64) *monster.AITimeManager {
	gid := instance.Gid
	if state.aiTimers == nil {
		state.aiTimers = make(map[uint32]*monster.AITimeManager)
	}
	timers := state.aiTimer(gid)
	if timers == nil {
		timers = monster.NewAITimeManager()
		next := func() uint32 { return monster.SummonRandomWord(s.random()) }
		// First-bank setup includes discarded CRT draws.
		timers.SetTimer(0, 300, 0, uint32(nowMs), false, next)
		timers.SetTimer(6, 1500, 0, uint32(nowMs), false, next)
		timers.SetTimer(7, 30, 0, uint32(nowMs), false, next)
		timers.InitAcquisitionTimer(uint8(instance.Nest.NativeTacticsFlags), next)
		state.aiTimers[gid] = timers
	}
	return timers
}

func (ops *MonsterMoverOps) acquisitionReady(divisionID string, instance monster.Instance, nowMs int64) bool {
	return ops.Monsters.checkAITimer(divisionID, instance.Gid, monster.TimerIDAcquisition, nowMs)
}

// FOLLOW OnEnter replaces Timer 0 with 100ms (the retail server's timer
// setter). CommitMover
// calls this with s.mu held, after rejecting stale/dead plans. Replans within
// FOLLOW preserve the existing deadline. Timer 0's first-bank setter ignores
// the timestamp argument; its immediate flag and discarded RNG draw remain.
func (s *MonsterState) beginFollowCadenceLocked(state *divisionMonsterState, gid uint32) {
	if timers := state.aiTimer(gid); timers != nil {
		timers.SetTimer(0, 100, 0, 0, false, func() uint32 { return monster.SummonRandomWord(s.random()) })
	}
}
