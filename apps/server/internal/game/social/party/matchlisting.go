/*
===========================================================================

matchlisting.go - resolves the current party's matching listing owner.

The matching lane reads this name through a production-wired seam. It never
uses it as permission to change another character's registration.

===========================================================================
*/
package party

/*
================
MatchListingOwner

A member sees the leader's listing; a solo player sees their own. Resolve
membership live so leaving or changing leaders cannot keep an old pin.
================
*/
func (r *Runtime) MatchListingOwner(divisionID, name string) string {
	snapshot, partied := r.registry.PartyOf(divisionID, name)
	if !partied {
		return name
	}
	for _, member := range snapshot.Members {
		if member.MemberID == snapshot.LeaderID {
			return member.Name
		}
	}
	return name
}
