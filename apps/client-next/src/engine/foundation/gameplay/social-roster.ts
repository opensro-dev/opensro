import type { GuildWarTerms } from "./guild-war";

/*
===========================================================================

social-roster.ts - immutable party, guild and war roster records

Wire decoding publishes these records; presentation and commands share
their types without owning another roster.

===========================================================================
*/
/*
================
PartyMember
================
*/
export interface PartyMember {
	readonly guild?: string;
	readonly native41?: number;
	// 75DB30 +0x50/+0x54 (mask bit 8): the member's main mastery ids, 0 when untrained.
	readonly primaryMastery?: number;
	readonly secondaryMastery?: number;
	// Resolved from the member's model reference by the world catalog (0 China, 1 Europe).
	readonly country?: number;
	readonly id: number;
	readonly name: string;
	readonly model: number;
	readonly level: number;
	readonly status: number;
	readonly region: number;
	readonly x: number;
	readonly y: number;
	readonly z: number;
	readonly war: number;
}
/*
================
GuildMember
================
*/
export interface GuildMember {
	readonly warScore?: number;
	readonly warKills?: number;
	readonly warDeaths?: number;
	readonly id: number;
	readonly name: string;
	readonly grade: number;
	readonly level: number;
	readonly donated: number;
	readonly permissions: number;
	readonly grant: string;
	readonly model: number;
	readonly role: number;
	readonly offline: number;
}

/*
================
guildRankSymbol

The text symbol the member slot's grade column (native control 13,
GDR_GMS_GRADE) shows for a grade byte. CIFGuildMemberSlot_PopulateFromMember
(0x5F2ED0) switches on the grade at member+0x25: 0 is the master, 5 a vice
master, 10 an ordinary member, and no other grade is ever given text (the
native switch has no default case, so the column stays empty). The grant
name is not this column: it renders only in the name column's grant mode.
================
*/
export function guildRankSymbol( grade: number ): string | null {
	switch ( grade ) {
		case 0:
			return "UIIT_STT_LEADER";
		case 5:
			return "UIIT_STT_GUILD_STAFF";
		case 10:
			return "UIIT_STT_GUILDSMAN";
		default:
			return null;
	}
}

/*
================
guildMemberTextColor

The colour of every text column in one member row. PopulateFromMember
(0x5F2ED0) writes the font colour slots (0x7817B0: mode 5 writes all four
states, mode 3 writes only the offline state) and the member's offline
byte (member+0x26) picks the render state: a master row (grade 0) is
gold 0xFFFFB541 online, darkening to 0xFFA17233 offline; every other
grade is white 0xFFFFFFFF online and grey 0xFF808080 offline. Returned
as float RGBA for the text board.
================
*/
export function guildMemberTextColor(
	grade: number,
	offline: number
): readonly [number, number, number, number] {
	if ( grade === 0 ) {
		return offline ? [ 0xa1 / 255, 0x72 / 255, 0x33 / 255, 1 ] : [ 1, 0xb5 / 255, 0x41 / 255, 1 ];
	}
	return offline ? [ 0x80 / 255, 0x80 / 255, 0x80 / 255, 1 ] : [ 1, 1, 1, 1 ];
}
// One open guild vote (826610's vote tail, 0x3A6C type 1): the master
// release vote is kind 0. remainingMs is as of its arrival. candidates
// arrive once the vote row asks for them (0x7332 -> 0xB332, 82CEE0).
/*
================
GuildVote
================
*/
export interface GuildVote {
	readonly id: number;
	readonly kind: number;
	readonly remainingMs: number;
	readonly candidates?: readonly GuildVoteCandidate[];
}

// One candidate by option index: the member's jid and the ballots it holds.
/*
================
GuildVoteCandidate
================
*/
export interface GuildVoteCandidate {
	readonly id: number;
	readonly ballots: number;
}
/*
================
Guild
================
*/
export interface Guild {
	readonly votes?: readonly GuildVote[];
	readonly crest?: number;
	readonly flags?: number;
	readonly id: number;
	readonly name: string;
	readonly level: number;
	readonly gp: number;
	readonly subject: string;
	readonly contents: string;
	readonly members: readonly GuildMember[];
}
// 75AD90 / 828D50: peer-relative projection; retain opaque native words by offset.
/*
================
GuildWar
================
*/
export interface GuildWar {
	readonly id: number;
	readonly enemyId: number;
	readonly name: string;
	readonly type: number;
	readonly localScore: number;
	readonly enemyScore: number;
	readonly word38: number;
	readonly word3c: number;
	readonly ending?: boolean;
	readonly clockAt?: number;
}

/*
================
SocialState
================
*/
export interface SocialState {
	readonly soldierAttributeSequence?: number;
	readonly wars?: readonly GuildWar[];
	readonly warPending?: 0 | 1 | 2;
	readonly warCountdown?: { readonly remaining: number; readonly nextAt: number; };
	readonly warResult?: {
		readonly key: string;
		readonly additionalKey?: string;
		readonly names: readonly string[];
		readonly sequence: number;
	};
	readonly roleUpdates?: readonly { name: string; role: number; }[];
	readonly crestUpdates?: readonly {
		name: string;
		guildId?: number;
		crest?: number;
		allianceId?: number;
		allianceCrest?: number;
	}[];
	readonly localName: string;
	// The local player's PARTY member id (0xB0D5/0xB452 result); 0 outside a
	// party. The guild roster finds the local player with guildSelf instead.
	readonly self: number;
	readonly leader: number;
	readonly options: number;
	// The job-party flag (SPartyData +0x0C): the local player was dressed in
	// a job suit when the party settings arrived (81B710 from vtable +0xA8).
	// The party window then names every member "*name" with no guild.
	readonly jobParty?: boolean;
	readonly members: readonly PartyMember[];
	readonly guild: Guild | null;
	readonly alliances?: readonly {
		id: number;
		name: string;
		level: number;
		master: string;
		model: number;
		flags: number;
	}[];
	readonly allianceMaster?: number;
	readonly allianceCrests?: readonly [number, number];
	readonly invitation: {
		readonly type: 1 | 2 | 3 | 5 | 6 | 10;
		readonly war?: GuildWarTerms;
		readonly options?: number;
		readonly gid: number;
	} | null;
	// The open resurrection question (0x3393 type 4, box kind 4); its gid is
	// the caster. It has its own slot so it never displaces an invitation
	// and no invitation displaces it.
	readonly resurrection?: { readonly gid: number; readonly mutation?: boolean; };
	// The open quest instance invitation (0x3393 type 7, 0x7644E0's type-7
	// arm); its gid is the party leader. Its own slot for the same reason:
	// the native prompt lives in its own invite struct beside the others.
	readonly instance?: { readonly gid: number; };
	// Native war-proposal replies reach the same system-message board as the
	// fortress announcements. The gameplay owner drains this each frame.
	readonly notice?: import("./system-notices").SystemNotice;
	// Diagnostic state, not player-facing invented text. These native bodies
	// require a formatted message/modal owner beyond the constant dispatcher.
	readonly unresolvedNotice?: { readonly category: number; readonly code: number; };
	// The war compensation the guild manager quoted (0xB140 [1][u32]); the
	// claim box (5D4050) asks before 0x73F7 collects it.
	readonly compensation?: number;
	readonly error: string | null;
}

/*
================
guildSelf

The local player's guild member row, by name: the guild roster carries no
party id, and social.self is the party's (0 outside a party), which left a
guild master without a party unable to donate, title, assign roles or
manage the guild.
================
*/
export function guildSelf( social: SocialState | null | undefined ): GuildMember | undefined {
	if ( !social?.guild || !social.localName ) return undefined;
	return social.guild.members.find( member => member.name === social.localName );
}
