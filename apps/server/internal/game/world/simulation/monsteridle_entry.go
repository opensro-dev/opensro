package simulation

import "opensro.online/server/internal/game/world/monster"

// The retail server's IDLE entry owns the home check. Its IDLE tick does not repeat it
// while waiting; an inherited movement can cross the home boundary meanwhile.
// Callers stage this callback before publishing their enclosing transaction.
func (ops *MonsterMoverOps) planIdleEntry(actor monster.Instance, mover monster.MoverState, now int64) (monster.MoverState, []Frame) {
	if !mover.TakeIdleEntry() {
		return mover, nil
	}
	if !needsHoming(actor, mover.LivePoseAt(now, ops.GroundHeight)) {
		mover.ResetStuck()
		mover.ClearHomingGoal()
		return mover, nil
	}
	return ops.planReturnLeg(actor, mover, monster.MoverEventHomingRequired, now)
}

func (ops *MonsterMoverOps) commitIdleEntry(division string, actor monster.Instance, mover monster.MoverState, frames []Frame, now int64) []Frame {
	before := ops.Monsters.prepareNavigation(division, actor.Gid)
	mover, home := ops.planIdleEntry(actor, mover, now)
	return ops.Monsters.commitNavigation(division, before, mover, append(frames, home...))
}
