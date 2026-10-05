/*
===========================================================================

monstertick_targeted.go - a monster action's private consequences

The public action frames and these tails share one RunMonsterLeg
transaction, but have deliberately different audiences.

===========================================================================
*/

package simulation

/*
================
deliverMonsterTargetFrames

Resolve each recipient against the coordinator's already-sampled session
set. It must not poll SessionSource again: one tick owns one immutable
participant snapshot, and a second read can both reorder lifecycle changes
and make a private consequence miss its original actor.
================
*/
func deliverMonsterTargetFrames(
	divisionID string,
	private []MonsterPrivateFrames,
	sessions []SessionSnapshot,
	push Pusher,
) {
	for _, recipient := range private {
		if len(recipient.Frames) == 0 {
			continue
		}
		for _, session := range sessions {
			if session.DivisionID == divisionID && session.CharacterID == recipient.CharacterID {
				push.PushToSession(session.SessionID, recipient.Frames)
				break
			}
		}
	}
}
