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

// Maximum guilds in a union (CIFAllianceGuild_RebuildSlots 5F6FA0 @ 5F719F pushes 8).
export const ALLIANCE_MAX_GUILDS = 8;

/*
================
guildMemberLimit

Native lookup in 0x00BE4490 (CIFGuildMember) / 0x00BE5F80 (CIFAllianceGuild)
by guild level 1..5.
================
*/
export function guildMemberLimit( level: number ): number {
	switch ( level ) {
		case 1:
			return 15;
		case 2:
			return 20;
		case 3:
			return 25;
		case 4:
			return 35;
		case 5:
			return 50;
		default:
			return 0;
	}
}

/*
================
guildMemberCountText

CIFGuildMember_RefreshFromGuildData (5E2C00 @ 5E2D72) and
CIFGuildMember_ReadGuildMetadataUpdate (5E4710 @ 5E47AA) format
GDR_GUILD_INFO_GUILD_MEMBER_NUM (ID 42) with u"%d / %d" (0x00BDC618).
================
*/
export function guildMemberCountText( count: number, level: number ): string {
	return `${count} / ${guildMemberLimit( level )}`;
}

/*
================
allianceGuildCountText

CIFAllianceGuild_RebuildSlots (5F6FA0 @ 5F7195) formats
GDR_ALLIANCE_INFO_NUM (ID 29) with u"%d/%d" (0x00BDB810) and cap 8.
================
*/
export function allianceGuildCountText( count: number ): string {
	return `${count}/${ALLIANCE_MAX_GUILDS}`;
}

/*
================
allianceMemberCountText

CIFAllianceGuild_SelectAlly (5F72B0 @ 5F754E) formats
GDR_GUILD_DETAIL_MEMBER_NUM (ID 51) with u"%d/%d" (0x00BDB810) and
0x00BE5F80[selected.level].
================
*/
export function allianceMemberCountText( count: number, level: number ): string {
	return `${count}/${guildMemberLimit( level )}`;
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
