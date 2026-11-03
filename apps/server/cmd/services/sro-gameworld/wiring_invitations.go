/*
===========================================================================

wiring_invitations.go - the shared consent rule across invitation lanes.

One player holds one unanswered proposal at a time, whatever its lane. The
lanes keep their own state; this file only connects their pending checks.

===========================================================================
*/
package main

import (
	"opensro.online/server/internal/game/action"
	"opensro.online/server/internal/game/social/guild"
	"opensro.online/server/internal/game/social/mentor"
	"opensro.online/server/internal/game/social/party"
)

/*
================
connectInvitationLanes

Shares the single outstanding-consent rule across party, guild, mentor,
and resurrection invitations without moving their state ownership.
================
*/
func connectInvitationLanes(
	parties *party.Runtime,
	guildInvites *guild.InviteRuntime,
	unions *guild.UnionRuntime,
	mentorInvites *mentor.InviteRuntime,
	items *action.Runtime,
) {
	resurrections := items.ResurrectionConsent()
	exchanges := items.ExchangeConsent()
	instanceInvites := items.InstanceInviteConsent()
	parties.AddConsentArm(exchanges)
	parties.AddConsentArm(guildInvites)
	parties.AddConsentArm(unions)
	parties.AddConsentArm(mentorInvites)
	parties.AddConsentArm(resurrections)
	parties.AddConsentArm(instanceInvites)

	// A player holds one unanswered proposal at a time, whatever its
	// lane (the server's unique transaction insert): every lane refuses to
	// propose while another's proposal waits.
	partyPending := parties.Registry().HasPendingInviteFor
	guildInvites.PeerPending = func(divisionID, name string) bool {
		return partyPending(divisionID, name) ||
			unions.HasPendingInvite(divisionID, name) ||
			mentorInvites.HasPendingInvite(divisionID, name) ||
			resurrections.HasPendingInvite(divisionID, name) ||
			exchanges.HasPendingInvite(divisionID, name) ||
			instanceInvites.HasPendingInvite(divisionID, name)
	}
	unions.PeerPending = func(divisionID, name string) bool {
		return partyPending(divisionID, name) ||
			guildInvites.HasPendingInvite(divisionID, name) ||
			mentorInvites.HasPendingInvite(divisionID, name) ||
			resurrections.HasPendingInvite(divisionID, name) ||
			exchanges.HasPendingInvite(divisionID, name) ||
			instanceInvites.HasPendingInvite(divisionID, name)
	}
	mentorInvites.PeerPending = func(divisionID, name string) bool {
		return partyPending(divisionID, name) ||
			guildInvites.HasPendingInvite(divisionID, name) ||
			unions.HasPendingInvite(divisionID, name) ||
			resurrections.HasPendingInvite(divisionID, name) ||
			exchanges.HasPendingInvite(divisionID, name) ||
			instanceInvites.HasPendingInvite(divisionID, name)
	}
	items.ProposalPending = func(divisionID, name string) bool {
		return partyPending(divisionID, name) ||
			guildInvites.HasPendingInvite(divisionID, name) ||
			unions.HasPendingInvite(divisionID, name) ||
			mentorInvites.HasPendingInvite(divisionID, name) ||
			exchanges.HasPendingInvite(divisionID, name) ||
			instanceInvites.HasPendingInvite(divisionID, name)
	}
	parties.PartyChanged = items.NotePartyChanged
}
