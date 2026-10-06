/*
===========================================================================

wiring_guildwar.go - join guild war to the shared invitation ownership rule

All existing proposal lanes retain their owners. Each sees guild-war
proposals through the same pending predicate and 3393 consent dispatcher.

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
warInvitationLanes
================
*/
type warInvitationLanes struct {
	parties *party.Runtime
	guilds  *guild.InviteRuntime
	unions  *guild.UnionRuntime
	mentors *mentor.InviteRuntime
	items   *action.Runtime
	wars    *guild.WarRuntime
}

/*
================
connectWarInvitations
================
*/
func connectWarInvitations(lanes warInvitationLanes) {
	lanes.parties.AddConsentArm(lanes.wars)
	lanes.wars.PeerPending = lanes.guilds.PeerPending
	other := lanes.wars.PeerPending
	lanes.wars.PeerPending = func(division, name string) bool {
		return other(division, name) || lanes.guilds.HasPendingInvite(division, name)
	}
	lanes.guilds.PeerPending = withWarPending(lanes.guilds.PeerPending, lanes.wars)
	lanes.unions.PeerPending = withWarPending(lanes.unions.PeerPending, lanes.wars)
	lanes.mentors.PeerPending = withWarPending(lanes.mentors.PeerPending, lanes.wars)
	lanes.items.ProposalPending = withWarPending(lanes.items.ProposalPending, lanes.wars)
}

/*
================
withWarPending
================
*/
func withWarPending(previous func(string, string) bool, wars *guild.WarRuntime) func(string, string) bool {
	return func(division, name string) bool {
		return previous != nil && previous(division, name) || wars.HasPendingInvite(division, name)
	}
}
