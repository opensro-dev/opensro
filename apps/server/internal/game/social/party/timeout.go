/*
===========================================================================

timeout.go - retire unanswered party proposals on the mission clock.

===========================================================================
*/
package party

/*
================
ExpireInvitations

46CE20 supplies 2C10; the shared reply owner notifies both participants.
B452 retires the invitee prompt without inventing another local deadline.
================
*/
func (r *Runtime) ExpireInvitations(nowMs int64) {
	for _, invite := range r.registry.ExpirePendingInvites(nowMs) {
		r.notifyInvitationFailure(invite, partyFailureTimeout)
	}
}
