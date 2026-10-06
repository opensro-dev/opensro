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
// One open guild vote (826610's vote tail, 0x3A6C type 1): the master
// release vote is kind 0. remainingMs is as of its arrival.
/*
================
GuildVote
================
*/
export interface GuildVote {
	readonly id: number;
	readonly kind: number;
	readonly remainingMs: number;
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
	readonly self: number;
	readonly leader: number;
	readonly options: number;
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
