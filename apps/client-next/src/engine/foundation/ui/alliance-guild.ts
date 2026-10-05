/*
===========================================================================

alliance-guild.ts - what the guild window's union tab shows and allows

CIFAllianceGuild (resinfo\ifallianceguild.txt) lists the union's guilds
from the 0x341E list and shows the leading guild and the selected one.
Its three commands follow CIFAllianceGuild_RefreshButtons (5F6880): only
a guild master gets any; outside a union he may invite; inside one the
leading guild's master may invite, leave and expel, any other master
only leave.

===========================================================================
*/
import type { SocialState } from "@/engine/foundation/gameplay/social";

/*
================
AllianceButtons
================
*/
export interface AllianceButtons {
	readonly invite: boolean;
	readonly exit: boolean;
	readonly expel: boolean;
}

/*
================
allianceButtons
================
*/
export function allianceButtons( social: SocialState | null | undefined ): AllianceButtons {
	const none = { invite: false, exit: false, expel: false };
	const guild = social?.guild;
	if ( !guild ) return none;
	const master = guild.members.find( m => m.grade === 0 );
	if ( !master || master.name !== social.localName ) return none;
	const alliances = social.alliances ?? [];
	if ( alliances.length === 0 ) return { invite: true, exit: false, expel: false };
	const leader = alliances.find( row => row.id === social.allianceMaster );
	if ( !leader ) return { invite: true, exit: false, expel: false };
	return leader.master === social.localName ?
		{ invite: true, exit: true, expel: true } :
		{ invite: false, exit: true, expel: false };
}

/*
================
allianceLeader

The union's leading guild row (5F6880 finds it by the 0x341E master id).
================
*/
export function allianceLeader( social: SocialState | null | undefined ) {
	return social?.alliances?.find( row => row.id === social.allianceMaster ) ?? null;
}
