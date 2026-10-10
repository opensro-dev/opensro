package simulation

import (
	"fmt"
	"opensro.online/server/internal/game/world/monster"
)

// DeliverMonsterHelp is the server-internal event ingress (53FE00, event 1).
// It is deliberately not exposed as a client opcode. Script/event producers
// supply the native payload; they cannot directly assign a monster target.
func (s *MonsterState) DeliverMonsterHelp(division string, receiver uint32, payload []byte) error {
	event, err := monster.DecodeHelpEvent(payload)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.populationForObject(division, receiver)
	if state == nil {
		return fmt.Errorf("help receiver division unavailable")
	}
	instance, ok := state.instances.lookup(receiver)
	if !ok || instance.CurrentHP == 0 {
		return fmt.Errorf("help receiver unavailable")
	}
	instance.Help = instance.Help.Replace(event)
	state.instances.set(receiver, instance)
	state.behavior.set(receiver, 0)
	return nil
}

// handleHelp owns one pending-event tick, including rejected requests. Native
// 53FF09 skips ordinary OnTick whenever 53FE30 consumed an event. Navigation
// executes outside the population lock; inbox generation and mover admission
// prevent a replacement event or fresh hit from being overwritten afterward.
func (ops *MonsterMoverOps) handleHelp(division string, expected monster.Instance, mover monster.MoverState, players []playerPose, now int64) ([]Frame, bool) {
	event, pending := expected.Help.Pending()
	if !pending {
		return nil, false
	}
	s := ops.Monsters
	s.mu.Lock()
	state := s.populationForObject(division, expected.Gid)
	instance, exists := state.instances.lookup(expected.Gid)
	if !exists || instance.Help != expected.Help || state.movers.get(expected.Gid) != mover {
		s.mu.Unlock()
		return nil, true
	}
	source, sourceExists := state.instances.lookup(event.SenderGID)
	s.mu.Unlock()
	validSource := sourceExists && source.CurrentHP != 0 && monster.AllowsTargetStatus(instance.Ref.TidWord, instance.Nest.NativeTacticsFlags, 0)
	if !sourceExists {
		_, validSource = eligiblePlayerByGid(instance, players, event.SenderGID)
	}
	target, validTarget := eligiblePlayerByGid(instance, players, event.TargetGID)
	accepted := instance.Nest.HasControls && instance.Nest.Controls.HelpReceiverHeader(event, mover.InNativeBattle(), instance.CurrentHP, instance.EffectiveMaxHP()) && validSource && validTarget && ordinaryPlayerHostility(instance, target)
	live := mover.LivePoseAt(now, ops.TerrainHeight)
	if accepted {
		distance := tacticsDistance3D(live, monster.Pose{RegionID: target.Pose.RegionID, X: target.Pose.X, Y: target.Pose.Y, Z: target.Pose.Z})
		accepted = monsterWithinHomeTrace(instance, live, distance)
	}
	if accepted && !IsDungeonRegion(live.RegionID) {
		// The receiver tests bit 0 only. Do not turn the distinct native bit
		// 28 into bit 0, or use clipped distance as an invented sight gate.
		accepted = false
		if ops.hasPlanner() {
			path := ops.planPath(navSight, live, mover.LiveNavOwner(now), monster.Pose{RegionID: target.Pose.RegionID, X: target.Pose.X, Y: target.Pose.Y, Z: target.Pose.Z})
			accepted = path != nil && path.Result()&monster.NavResultClipped == 0
		}
	}
	next := mover
	var frames []Frame
	if accepted {
		next.Pose = live
		mustMoverTransition(&next, monster.MoverEventHelpAccepted, event.TargetGID)
		frames = []Frame{correctionFrame(instance.Gid, live)}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state = s.populationForObject(division, instance.Gid)
	current, exists := state.instances.lookup(instance.Gid)
	currentSource, currentSourceExists := state.instances.lookup(event.SenderGID)
	// The entire value is the admission token. Checking only HP misses a
	// same-target hit, changed maximum HP, home, or summon ownership while
	// geometry runs. Keep new actor fields inside this boundary automatically.
	if !exists || current != instance || current.Motion.StateAt(now) != 0 ||
		state.movers.get(instance.Gid) != mover || currentSourceExists != sourceExists || (sourceExists && currentSource != source) {
		return nil, true
	}
	if accepted && !s.commitMoverLocked(state, instance.Gid, next) {
		return nil, true
	}
	current = state.instances.get(instance.Gid)
	if accepted && next.TargetGID() != 0 {
		// 53FFE0 changes primary identity/time; it does not manufacture damage
		// or replace the independent secondary remembered opponent.
		current.Opponents[0].GID = event.TargetGID
		current.Opponents[0].LastHitMs = uint32(now)
	}
	current.Help = current.Help.Consumed()
	state.instances.set(instance.Gid, current)
	return frames, true
}
