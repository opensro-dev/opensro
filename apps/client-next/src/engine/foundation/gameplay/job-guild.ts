/*
===========================================================================

job-guild.ts - the job guilds: menu rows, requests and answers

A trader, thief or hunter guild NPC (capability bits 0x80000, 0x100000 and
0x200000) lists CIFNPCTalk_AppendJobMenuRows (5D79E0) for its job: join
when the player has no job; withdraw and the alias when the player belongs
to it. Their confirmations send

	0x7439 [u32 npc][u8 job]               -> 0xB439 [1][job][grade][u32 exp]
	0x7661 [u32 npc]                       -> 0xB661 [1]
	0x7620 [u32 npc][u8 mode][ascii alias] -> 0xB620 [1][mode][ascii alias]
	0x737E [u32 npc][u8 job][u8 kind]      -> 0xB37E [1][job][kind][u8 n]
	                                           { [u8 rank][ascii alias][u8 grade][u32 value] }
	0x77BE [u32 npc][u8 mode]              -> 0xB7BE [1][mode][u32 reward]
	0x75EE [u32 npc]                       -> 0xB5EE (previous job information)

and a refusal is [2][code] in notice category 0x18 (the alias answer also
echoes mode and alias). The rank, contribution and previous-job rows are
not offered until the server owns them.

Wearing or removing a job suit starts a dress bar instead of answering:
0x3434 [u32 gid][2][2][u8 seconds] (CICUser_SetActionProgressDurationSeconds),
and the item move is answered when the bar ends.

===========================================================================
*/
import type { WireFrame } from "@/engine/contracts/network";
import { constantNativeNotice } from "./native-notice";
import type { SystemNotice } from "./system-notices";

export const OP_JOB_JOIN = 0x7439;
export const OP_JOB_JOIN_RESPONSE = 0xb439;
export const OP_JOB_WITHDRAW = 0x7661;
export const OP_JOB_WITHDRAW_RESPONSE = 0xb661;
export const OP_JOB_ALIAS = 0x7620;
export const OP_JOB_ALIAS_RESPONSE = 0xb620;
export const OP_JOB_DRESS_BAR = 0x3434;
export const OP_JOB_RANK = 0x737e;
export const OP_JOB_OUTCOME = 0x77be;
export const OP_JOB_OUTCOME_RESPONSE = 0xb7be;
export const OP_JOB_PREV_INFO = 0x75ee;
export const OP_JOB_PREV_INFO_RESPONSE = 0xb5ee;
// 0x77BE's modes: tell the week's outcome, then collect it.
export const JOB_OUTCOME_QUERY = 0;
export const JOB_OUTCOME_COLLECT = 1;
export const OP_JOB_RANK_RESPONSE = 0xb37e;
// 0x737E's kinds: the activity rank (CIFJobRank) and the contribution rank.
export const JOB_RANK_ACTIVITY = 0;
export const JOB_RANK_CONTRIBUTION = 1;
// CPSMission_OnJobTypeLevelUpdate0x35EE (75F0C0): [u8 job][u8 grade][u32 exp].
export const OP_JOB_EXP_UPDATE = 0x35ee;
const JOB_NOTICE_CATEGORY = 0x18;
// 0x3434's lead bytes for the job dress (kind 2, step 2).
const JOB_DRESS_KIND = 2;
export const JOB_ALIAS_CHECK = 0;
export const JOB_ALIAS_CREATE = 1;

// The capability bits of the trader, thief and hunter guilds.
const JOB_GUILD_TRADER = 0x80000;
const JOB_GUILD_THIEF = 0x100000;
const JOB_GUILD_HUNTER = 0x200000;

/*
================
LocalJob

The joined job (0 none, 1 trader, 2 thief, 3 hunter), its grade and
experience, the alias, and the week's contribution so far (the entry's
job block; the contribution rank window's own entry).
================
*/
export interface LocalJob {
	readonly type: number;
	readonly grade: number;
	readonly exp: number;
	readonly alias: string;
	readonly contribution: number;
}

/*
================
noJob
================
*/
export function noJob(): LocalJob {
	return { type: 0, grade: 0, exp: 0, alias: "", contribution: 0 };
}

/*
================
jobMenuName
================
*/
function jobMenuName( job: number ): string {
	return job === 1 ? "TRADERMENU" : job === 2 ? "THIEFMENU" : "HUNTERMENU";
}

/*
================
jobGuildsOffered

The jobs whose guild an NPC's capability word offers.
================
*/
export function jobGuildsOffered( capabilities: number ): readonly number[] {
	// 5D9100 appends the trader, hunter and thief menus in that order.
	const guilds: number[] = [];
	if ( capabilities & JOB_GUILD_TRADER ) guilds.push( 1 );
	if ( capabilities & JOB_GUILD_HUNTER ) guilds.push( 3 );
	if ( capabilities & JOB_GUILD_THIEF ) guilds.push( 2 );
	return guilds;
}

/*
================
jobMenuRows

5D79E0's rows for each offered guild: join with no job; withdraw, the
alias (create or modify) and, for a thief or hunter, the week's outcome for
a member of that guild; then the contribution and activity ranks and the
previous job information for anyone.
================
*/
export function jobMenuRows(
	guilds: readonly number[],
	job: LocalJob
): readonly { readonly id: string; readonly symbol: string; }[] {
	const rows: { id: string; symbol: string; }[] = [];
	for ( const guild of guilds ) {
		const menu = "UIIT_STT_NPC_CHATTING_" + jobMenuName( guild );
		if ( job.type === 0 ) rows.push( { id: "npc-job-join:" + guild, symbol: menu + "_JOIN" } );
		if ( job.type === guild ) {
			rows.push( { id: "npc-job-withdraw:" + guild, symbol: menu + "_WITHD" } );
			rows.push( { id: "npc-job-alias:" + guild, symbol: menu + (job.alias ? "_ALIASMODIFY" : "_ALIASCREATE") } );
			// Case 0x24: thieves and hunters share the hunters' outcome row.
			if ( guild !== 1 ) {
				rows.push( { id: "npc-job-outcome:" + guild, symbol: "UIIT_STT_NPC_CHATTING_HUNTERMENU_OUTCOME" } );
			}
		}
		// Every visitor then gets the contribution and activity ranks
		// (cases 0x23 and 0x22); thieves share the hunters' contribution row.
		rows.push( {
			id: "npc-job-contribution:" + guild,
			symbol: guild === 1 ?
				"UIIT_STT_NPC_CHATTING_TRADERMENU_DONATIONRANK" :
				"UIIT_STT_NPC_CHATTING_HUNTERMENU_CONTRIBUTERANK"
		} );
		rows.push( { id: "npc-job-rank:" + guild, symbol: menu + "_JOBRANK" } );
		rows.push( { id: "npc-job-previous:" + guild, symbol: "UIIT_STT_NPC_CHATTING_JOBINFO_OLD" } );
	}
	return rows;
}

/*
================
jobJoinRequest
================
*/
export function jobJoinRequest( npc: number, job: number ): WireFrame {
	const payload = new Uint8Array( 5 );
	new DataView( payload.buffer ).setUint32( 0, npc, true );
	payload[4] = job;
	return { opcode: OP_JOB_JOIN, payload };
}

/*
================
jobWithdrawRequest
================
*/
export function jobWithdrawRequest( npc: number ): WireFrame {
	const payload = new Uint8Array( 4 );
	new DataView( payload.buffer ).setUint32( 0, npc, true );
	return { opcode: OP_JOB_WITHDRAW, payload };
}

/*
================
jobAliasRequest
================
*/
export function jobAliasRequest( npc: number, mode: number, alias: string ): WireFrame {
	if ( !/^[A-Za-z0-9_]{0,64}$/.test( alias ) ) throw Error( "Invalid job alias" );
	const payload = new Uint8Array( 7 + alias.length ), v = new DataView( payload.buffer );
	v.setUint32( 0, npc, true );
	payload[4] = mode;
	v.setUint16( 5, alias.length, true );
	for ( let i = 0; i < alias.length; i++ ) payload[7 + i] = alias.charCodeAt( i );
	return { opcode: OP_JOB_ALIAS, payload };
}

/*
================
jobRankRequest
================
*/
export function jobRankRequest( npc: number, job: number, kind: number ): WireFrame {
	const payload = new Uint8Array( 6 );
	new DataView( payload.buffer ).setUint32( 0, npc, true );
	payload[4] = job;
	payload[5] = kind;
	return { opcode: OP_JOB_RANK, payload };
}

/*
================
JobRankRow
================
*/
export interface JobRankRow {
	readonly rank: number;
	readonly alias: string;
	readonly grade: number;
	readonly value: number;
}

/*
================
JobRankList
================
*/
export interface JobRankList {
	readonly job: number;
	readonly kind: number;
	readonly rows: readonly JobRankRow[];
}

/*
================
JobRanks

CGInterface keeps one list per job and kind (GetJobActiveRankRecord 67B300,
GetJobContributionRankRecord 67B330) until the next answer replaces it.
opened names the list whose window the last menu row or answer opened.
================
*/
export interface JobRanks {
	readonly lists: readonly JobRankList[];
	readonly opened: { readonly job: number; readonly kind: number; readonly sequence: number; } | null;
}

/*
================
jobRankAnswer

CPSMission_OnJobRankListResponse0xB37E (763F00): a list replaces the cached
one for its job and kind and opens its window; a refusal [2][code][job]
[kind] is the category 0x18 notice. Null for any other frame.
================
*/
export function jobRankAnswer(
	frame: WireFrame
): { readonly list: JobRankList | null; readonly notice: SystemNotice | null; } | null {
	const p = frame.payload;
	if ( frame.opcode !== OP_JOB_RANK_RESPONSE ) return null;
	if ( p[0] === 2 ) {
		if ( p.length !== 4 ) throw Error( "Invalid job rank refusal" );
		return { list: null, notice: constantNativeNotice( JOB_NOTICE_CATEGORY, p[1]! ) };
	}
	if ( p[0] !== 1 || p.length < 4 ) throw Error( "Invalid job rank list" );
	const v = new DataView( p.buffer, p.byteOffset, p.byteLength ), rows: JobRankRow[] = [];
	let at = 4;
	for ( let i = 0; i < p[3]!; i++ ) {
		if ( at >= p.length ) throw Error( "Truncated job rank list" );
		const rank = p[at]!, alias = readAscii( p, at + 1 );
		if ( alias.next + 5 > p.length ) throw Error( "Truncated job rank list" );
		rows.push( { rank, alias: alias.text, grade: p[alias.next]!, value: v.getUint32( alias.next + 1, true ) } );
		at = alias.next + 5;
	}
	if ( at !== p.length ) throw Error( "Trailing job rank bytes" );
	return { list: { job: p[1]!, kind: p[2]!, rows }, notice: null };
}

/*
================
jobOutcomeRequest
================
*/
export function jobOutcomeRequest( npc: number, mode: number ): WireFrame {
	const payload = new Uint8Array( 5 );
	new DataView( payload.buffer ).setUint32( 0, npc, true );
	payload[4] = mode;
	return { opcode: OP_JOB_OUTCOME, payload };
}

/*
================
jobPrevInfoRequest
================
*/
export function jobPrevInfoRequest( npc: number ): WireFrame {
	const payload = new Uint8Array( 4 );
	new DataView( payload.buffer ).setUint32( 0, npc, true );
	return { opcode: OP_JOB_PREV_INFO, payload };
}

/*
================
JobOutcome

The week's outcome the guild NPC told (0xB7BE mode 0); collected counts
the collections (mode 1), each of which closes the talk.
================
*/
export interface JobOutcome {
	readonly npc: number;
	readonly reward: number;
	readonly sequence: number;
	readonly collected: number;
}

/*
================
jobOutcomeAnswer

CPSMission_OnJobOutcomeResponse0xB7BE (75D5C0): mode 0 turns the talk into
the outcome page (CIFNPCTalk_ShowJobOutcome 5D7870); mode 1 stores the
reward, closes the talk and prints UIIT_MSG_OUTCOM_COMPLETE (system chat
type 5) with the amount grouped by thousands; [2][code][mode] is the
category 0x18 notice. Null for any other frame.
================
*/
export function jobOutcomeAnswer(
	frame: WireFrame,
	outcome: JobOutcome | null,
	npc: number
): { readonly outcome: JobOutcome | null; readonly notice: SystemNotice | null; } | null {
	const p = frame.payload;
	if ( frame.opcode !== OP_JOB_OUTCOME_RESPONSE ) return null;
	if ( p[0] === 2 ) {
		if ( p.length !== 3 ) throw Error( "Invalid job outcome refusal" );
		return { outcome, notice: constantNativeNotice( JOB_NOTICE_CATEGORY, p[1]! ) };
	}
	if ( p[0] !== 1 || p.length !== 6 || p[1]! > JOB_OUTCOME_COLLECT ) throw Error( "Invalid job outcome" );
	const reward = new DataView( p.buffer, p.byteOffset, p.byteLength ).getUint32( 2, true );
	const sequence = (outcome?.sequence ?? 0) + 1, collected = outcome?.collected ?? 0;
	if ( p[1] === JOB_OUTCOME_QUERY ) return { outcome: { npc, reward, sequence, collected }, notice: null };
	return {
		outcome: { npc, reward: 0, sequence, collected: collected + 1 },
		notice: {
			key: "UIIT_MSG_OUTCOM_COMPLETE",
			value: 0,
			arguments: [ reward.toLocaleString( "en-US" ) ],
			formatKinds: [ "s" ],
			nativeType: 5
		}
	};
}

/*
================
jobPrevInfoAnswer

CPSMission_OnPrevJobInfoResponse0xB5EE (75C650): [2][code] is the
category 0x18 notice (code 0x29: UIIT_MSG_JOBINFO_OLD_NOTEXIST). The [1]
answer carries a character's pre-tri-job levels for CIFPrevJobInfo; no
port character has them, so the server never sends it.
================
*/
export function jobPrevInfoAnswer( frame: WireFrame ): SystemNotice | null {
	const p = frame.payload;
	if ( frame.opcode !== OP_JOB_PREV_INFO_RESPONSE ) return null;
	if ( p[0] !== 2 || p.length !== 2 ) throw Error( "Unsupported previous job information" );
	return constantNativeNotice( JOB_NOTICE_CATEGORY, p[1]! );
}

/*
================
readAscii
================
*/
function readAscii( p: Uint8Array, at: number ): { text: string; next: number; } {
	if ( p.length < at + 2 ) throw Error( "Truncated job alias" );
	const n = p[at]! | p[at + 1]! << 8;
	if ( p.length < at + 2 + n ) throw Error( "Truncated job alias" );
	return { text: String.fromCharCode( ...p.subarray( at + 2, at + 2 + n ) ), next: at + 2 + n };
}

/*
================
jobGuildAnswer

The job after a guild answer and the notice it raises, or null for any
other frame. 75C520, 75F5B0 and 75F7A0 announce success in chat; a refusal
is the category 0x18 notice.
================
*/
export function jobGuildAnswer(
	frame: WireFrame,
	job: LocalJob
): { readonly job: LocalJob; readonly notice: SystemNotice | null; } | null {
	const p = frame.payload;
	if ( frame.opcode === OP_JOB_JOIN_RESPONSE || frame.opcode === OP_JOB_WITHDRAW_RESPONSE ) {
		if ( p[0] === 2 ) {
			if ( p.length !== 2 ) throw Error( "Invalid job guild refusal" );
			return { job, notice: constantNativeNotice( JOB_NOTICE_CATEGORY, p[1]! ) };
		}
		if ( frame.opcode === OP_JOB_WITHDRAW_RESPONSE ) {
			if ( p.length !== 1 || p[0] !== 1 ) throw Error( "Invalid job withdrawal" );
			return { job: noJob(), notice: { key: "UIIT_MSG_JOBGUILD_WITHD_COMPELET", value: 0 } };
		}
		if ( p.length !== 7 || p[0] !== 1 ) throw Error( "Invalid job join" );
		const exp = new DataView( p.buffer, p.byteOffset, p.byteLength ).getUint32( 3, true );
		return {
			job: { type: p[1]!, grade: p[2]!, exp, alias: "", contribution: 0 },
			notice: { key: "UIIT_MSG_JOBGUILD_JOIN_COMPELET", value: 0 }
		};
	}
	if ( frame.opcode !== OP_JOB_ALIAS_RESPONSE ) return null;
	if ( p[0] === 2 ) {
		if ( p.length < 3 || readAscii( p, 3 ).next !== p.length ) throw Error( "Invalid job alias refusal" );
		return { job, notice: constantNativeNotice( JOB_NOTICE_CATEGORY, p[1]! ) };
	}
	if ( p[0] !== 1 || p.length < 2 ) throw Error( "Invalid job alias answer" );
	const alias = readAscii( p, 2 );
	if ( alias.next !== p.length ) throw Error( "Invalid job alias answer" );
	if ( p[1] !== JOB_ALIAS_CREATE ) return { job, notice: null };
	return {
		job: { ...job, alias: alias.text },
		notice: { key: job.alias ? "UIIT_MSG_ALIAS_MODIFY_COMPLETE" : "UIIT_MSG_ALIAS_CREATE_COMPLETE", value: 0 }
	};
}

/*
================
jobExpUpdate

75F0C0 stores the new grade and experience, then reports the change in the
system chat (type 1): UIIT_STT_JOB_EXP_<JOB>_GET or _LOST with the
difference, and on a grade rise the UIIT_STT_JOB_LVUP_<JOB>_CLASS banner
naming the new grade title (UIIT_STT_CLASS_[EU_]<JOB>_<grade> by country).

Across a grade change the native chat line reads CLevelData, which this
worker does not hold, so those lines wait for the level data to reach it:
a rise prints _GET with row(old grade)+0x1C - old exp + new exp, a fall
_LOST with row(new grade)+0x20 - new exp + old exp. The server never
lowers a grade (progression AddJobExp floors a loss at zero).
================
*/
export function jobExpUpdate(
	frame: WireFrame,
	job: LocalJob,
	country: number | undefined
): { readonly job: LocalJob; readonly notices: readonly SystemNotice[]; } | null {
	const p = frame.payload;
	if ( frame.opcode !== OP_JOB_EXP_UPDATE ) return null;
	if ( p.length !== 6 ) throw Error( "Invalid job experience update" );
	const grade = p[1]!, exp = new DataView( p.buffer, p.byteOffset, p.byteLength ).getUint32( 2, true );
	const next = { ...job, grade, exp };
	const name = [ "", "MERCHANT", "THIEF", "HUNTER" ][job.type];
	if ( !name ) return { job: next, notices: [] };
	if ( grade > job.grade ) {
		const title = country === 0 ?
			"UIIT_STT_CLASS_" + name + "_" + grade :
			country === 1 ?
			"UIIT_STT_CLASS_EU_" + name + "_" + grade :
			null;
		return {
			job: next,
			notices: [ {
				key: "UIIT_STT_JOB_LVUP_" + name + "_CLASS",
				value: 0,
				localizedArguments: [ title ],
				formatKinds: [ "s" ],
				bannerOnly: true
			} ]
		};
	}
	if ( grade < job.grade || exp === job.exp ) return { job: next, notices: [] };
	// 75F0C0 compares the two as signed 32-bit values.
	const gained = (exp | 0) > (job.exp | 0);
	return {
		job: next,
		notices: [ {
			key: "UIIT_STT_JOB_EXP_" + name + (gained ? "_GET" : "_LOST"),
			value: gained ? exp - job.exp : job.exp - exp,
			nativeType: 1
		} ]
	};
}

/*
================
jobDressSeconds

The seconds of a job dress bar 0x3434 starts for gid, or null.
================
*/
export function jobDressSeconds( frame: WireFrame, gid: number ): number | null {
	const p = frame.payload;
	if ( frame.opcode !== OP_JOB_DRESS_BAR || p.length !== 7 || p[4] !== JOB_DRESS_KIND ) return null;
	return new DataView( p.buffer, p.byteOffset, 4 ).getUint32( 0, true ) === gid ? p[6]! : null;
}
