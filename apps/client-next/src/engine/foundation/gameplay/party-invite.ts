/*
===========================================================================

party-invite.ts - who a party invitation goes to, and the proposer's line

NetClient_SendPartyInvite70D5 (6FD830) and NetClient_SendPartyJoinRequest751A
(6FDA50) resolve the selected target the same way before they send: only a
character other than the local player, a player as it is, a mounted horse or
vehicle as its rider, anything else nothing. Then both print
UIIT_STT_PARTY_PROPOSAL_PENDING with the invitee's name on the party chat
channel, so the proposer sees the invitation leave.

===========================================================================
*/
import type { EntityState } from "@/engine/contracts/world";
import type { SystemNotice } from "./system-notices";

// CGInterface_ChatSystemMessageFormatted channel 4: party.
const PARTY_CHANNEL = 4;

/*
================
PartyInviteLookup

The entity table reads the resolution needs: an entity by gid, and the
character riding a horse (CICharactor_GetMountedHorseOrVehicle).
================
*/
export interface PartyInviteLookup {
	readonly entity: ( gid: number ) => EntityState | undefined;
	readonly rider: ( gid: number ) => EntityState | undefined;
}

/*
================
partyInviteTarget

The player a press on `gid` invites, or null when 6FD830 / 6FDA50 would
send nothing: no entity, the local player, or a character that is neither
a player nor a ridden COS (a monster, an NPC, an empty horse).
================
*/
export function partyInviteTarget(
	gid: number,
	localGid: number,
	lookup: PartyInviteLookup
): EntityState | null {
	const entity = lookup.entity( gid );
	if ( !entity || entity.gid === localGid ) return null;
	if ( entity.kind === "player" ) return entity;
	if ( entity.kind !== "cos" ) return null;
	const rider = lookup.rider( gid );
	return rider?.kind === "player" ? rider : null;
}

/*
================
partyProposalPendingNotice

The line both composers print before sending: "[%s]is requesting to party"
with the invitee's name.
================
*/
export function partyProposalPendingNotice( name: string ): SystemNotice {
	return { key: "UIIT_STT_PARTY_PROPOSAL_PENDING", value: 0, arguments: [ name ], nativeType: PARTY_CHANNEL };
}
