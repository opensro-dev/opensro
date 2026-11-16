/*
===========================================================================

social.ts - party and invitation state and wire

Owns the decoded party roster and the pending invitations. Guild,
union and guild-war frames are decoded by social-guild-union, which
this module calls for those opcodes through the same frame cursor;
the trailing-bytes rule below covers both lanes. The shared member
record it decodes rows with lives in party-member.ts. Outgoing
commands live in social-request. A frame is decoded completely
before any state is published.

===========================================================================
*/
import { GUILD_WAR_PROPOSAL } from "./guild-war";
import type { WireFrame } from "@/engine/contracts/network";
import { resolveNativeNotice, type NativeNoticeContext } from "./native-notice";
import { NO_JOB_CLASS } from "./job-guild";
import type { SocialState, PartyMember } from "./social-roster";
import { assertUniqueRosterRows } from "./social-roster";
import { socialGuildUnionPacket } from "./social-guild-union";
export type { SocialState, PartyMember, GuildMember, GuildVote, Guild, GuildWar } from "./social-roster";
import { readPartyMember, unknownPartyMember } from "./party-member";
// The 0x3393 type a resurrection skill proposes to a dead player.
export const RESURRECTION_PROPOSAL = 4;
// The 0x3393 type a revival with an rmut skill proposes (7644E0 case 7).
export const MUTATION_PROPOSAL = 8;
// The 0x3393 type inviting a party member into a quest instance
// (0x7644E0's type-7 arm, the box at 0x75C160). NOTE: the handler's jump
// table is indexed from 0, so in table-case terms this is case 6.
export const INSTANCE_PROPOSAL = 7;
// 7644E0 case 6: a guild master proposes a union (confirm box 0x1D).
export const UNION_PROPOSAL = 6;
export { socialRequest, type SocialCommand } from "./social-request";

/*
================
emptySocial
================
*/
export function emptySocial( localName = "" ): SocialState {
	return { localName, self: 0, leader: 0, options: 0, members: [], guild: null, invitation: null, error: null };
}

/*
================
withoutResurrection

The state with the resurrection question closed. The slot is optional and
dropped rather than nulled, so a state that never held a question keeps
the exact shape it had before the slot existed.
================
*/
export function withoutResurrection( state: SocialState ): SocialState {
	if ( !state.resurrection ) {
		return state;
	}
	const { resurrection: _closed, ...rest } = state;
	return rest;
}

/*
================
withoutInstance

The state with the quest instance invitation closed; the slot is dropped
rather than nulled, as withoutResurrection.
================
*/
export function withoutInstance( state: SocialState ): SocialState {
	if ( !state.instance ) {
		return state;
	}
	const { instance: _closed, ...rest } = state;
	return rest;
}

/*
================
socialPacket

Decode a complete frame before publishing any state. Only evidenced
server-emitted arms enter this projection.
================
*/
export function socialPacket(
	state: SocialState,
	frame: WireFrame,
	noticeContext: NativeNoticeContext & { readonly now?: number; readonly localJob?: number; } = {}
): SocialState | null {
	const op = frame.opcode, p = frame.payload;
	if (
		![
			0x3393,
			0xb71b,
			0xb465,
			0xb0d5,
			0xb452,
			0xb51a,
			0xb095,
			0xb34a,
			0xb2db,
			0x35d6,
			0x3e58,
			0x32c4,
			0xb663,
			0x3b29,
			0xb56e,
			0xb66e,
			0xb77a,
			0xb40f,
			0xb2bc,
			0xb65f,
			// Union invite/leave/expel and the rights grant answer
			// [1] or [2][code] as category 0x10 notices (75CCA0, 75CCF0,
			// 75CD40, 75CB80).
			0xb379,
			0xb795,
			0xb680,
			0xb44e,
			0x341e,
			0x32bb,
			0x34f3,
			0x37d4,
			0xb3f0,
			0xb322,
			0xb7d4,
			0xb140,
			0xb3f7,
			0xb6dc,
			0xb330,
			0xb332,
			0xb515,
			0x3a6c
		].includes( op )
	) {
		return null;
	}
	const v = new DataView( p.buffer, p.byteOffset, p.byteLength );
	let o = 0;
	/*
	================
	take
	================
	*/
	function take( n: number ) {
		if ( o + n > p.length ) {
			throw Error( "Truncated social frame" );
		}
		const at = o;
		o += n;
		return at;
	}
	const u8 = () => v.getUint8( take( 1 ) ),
		u16 = () => v.getUint16( take( 2 ), true ),
		i16 = () => v.getInt16( take( 2 ), true ),
		u32 = () => v.getUint32( take( 4 ), true );
	/*
	================
	str
	================
	*/
	function str() {
		const n = u16();
		if ( n > 2048 ) {
			throw Error( "Social string budget" );
		}
		const at = take( n );
		return new TextDecoder( "utf-8", { fatal: true } ).decode( p.subarray( at, at + n ) );
	}
	/*
	================
	partyMember
	================
	*/
	function partyMember( old?: PartyMember ): PartyMember {
		const decoded = readPartyMember( p, o, old );
		o = decoded.end;
		return decoded.member;
	}
	let next: SocialState = { ...state, error: null, notice: undefined, notices: undefined, degradation: undefined };
	// Guild, union and guild-war opcodes decode in the guild lane
	// (social-guild-union) through this frame cursor; it returns null
	// for the party and invitation opcodes below.
	const guildNext = socialGuildUnionPacket( {
		state,
		next,
		op,
		lead: p[0] ?? -1,
		noticeContext,
		u8,
		u32,
		str,
		skipRest: () => {
			o = p.length;
		}
	} );
	if ( guildNext ) {
		next = guildNext;
	} else if ( op === 0x3393 ) {
		const type = u8();
		if (
			type !== 1 && type !== 2 && type !== 3 && type !== RESURRECTION_PROPOSAL && type !== 5 &&
			type !== UNION_PROPOSAL && type !== INSTANCE_PROPOSAL && type !== MUTATION_PROPOSAL &&
			type !== GUILD_WAR_PROPOSAL
		) {
			return null;
		}
		const gid = u32();
		const war = type === GUILD_WAR_PROPOSAL ?
			{ name: str(), mode: u8(), period: u32(), scoreIndex: u8(), stake: u32() } :
			undefined;
		if ( !gid ) {
			throw Error( "Invalid invitation" );
		}
		// 7644E0 types 4 and 8 carry only the caster: {u8 type, u32 casterGid}.
		// They fill their own slot and leave a pending invitation untouched.
		// Type 7 carries the whole invite struct (0x764380): past the leader
		// gid, u32, u8, u32, u8, u8, u32, u32, a u16 member count and that
		// many member gids. The client acts on no field past the leader, but
		// the frame is still walked whole, as the native reader walks it.
		if ( type === INSTANCE_PROPOSAL ) {
			u32();
			u8();
			u32();
			u8();
			u8();
			u32();
			u32();
			const members = u16();
			for ( let i = 0; i < members; i++ ) {
				u32();
			}
		}
		next = type === INSTANCE_PROPOSAL ?
			{ ...next, instance: { gid } } :
			type === RESURRECTION_PROPOSAL || type === MUTATION_PROPOSAL ?
			{ ...next, resurrection: { gid, ...(type === MUTATION_PROPOSAL ? { mutation: true } : {}) } } :
			{
				...next,
				...(war ? { warPending: 2 as const } : {}),
				invitation: {
					type,
					gid,
					...(war ? { war } : {}),
					...(type === 2 || type === 3 ? { options: u8() } : {})
				}
			};
	} // 75B450 / 75B4B0 / 75B520 use category ONE, unlike invite failures.
	// Membership remains authoritative on 3E58, not these acknowledgements.
	else if ( op === 0xb095 || op === 0xb34a || op === 0xb2db ) {
		if ( u8() === 2 ) {
			const resolution = resolveNativeNotice( 1, u8(), noticeContext );
			next = {
				...next,
				notice: resolution.kind === "notice" ? resolution.notice : undefined,
				unresolvedNotice: resolution.kind === "context" ?
					{ category: resolution.category, code: resolution.code } :
					undefined
			};
		}
	} // 75B100: success has no body or membership authority. Failure uses
	// category 2. Other one-byte result flags are native no-ops.
	else if ( op === 0xb51a ) {
		const result = u8();
		if ( result === 2 ) next = { ...next, error: null, notice: partyNotice( u8() ) ?? undefined };
	} else if ( op === 0xb0d5 || op === 0xb452 ) {
		const result = u8();
		if ( result === 1 ) {
			next = { ...next, self: u32() };
		} else if ( result === 2 ) {
			const code = u8();
			// 75B170 calls 5C8020(6) on timeout: retire the existing party
			// prompt, NOT create a dialog or emit the category-2 guide.
			next = op === 0xb452 && code === 0x10 ?
				{
					...next,
					error: null,
					invitation: next.invitation?.type === 2 || next.invitation?.type === 3 ? null : next.invitation
				} :
				{ ...next, error: null, notice: partyNotice( code ) ?? undefined };
		} else {
			throw Error( "Invalid party result" );
		}
	} else if ( op === 0x35d6 ) {
		const flags = u8();
		if ( flags & ~3 ) {
			throw Error( "Invalid party flags" );
		}
		if ( flags & 1 ) {
			// 81B710: the settings also latch whether the local player is in job
			// mode (class 4 is none), the party window's job-party flag.
			next = {
				...next,
				leader: u32(),
				options: u8(),
				members: [],
				jobParty: (noticeContext.localJob ?? NO_JOB_CLASS) !== NO_JOB_CLASS
			};
		}
		if ( next.options & ~7 ) {
			throw Error( "Invalid party options" );
		}
		if ( flags & 2 ) {
			const n = u8();
			if ( n > 8 ) {
				throw Error( "Party roster budget" );
			}
			const members = Array.from( { length: n }, () => partyMember() );
			assertUniqueRosterRows( members, 8 );
			next = { ...next, members };
		}
	} else if ( op === 0x3e58 ) {
		// 761870 prints each membership change as a type-4 guide line
		// alongside the roster mutation. Each arm resolves its name
		// before it mutates: the row itself on a join, the pre-update
		// roster on a leave or a delegation.
		const type = u8();
		if ( type === 1 ) {
			u8();
			next = {
				...next,
				self: 0,
				leader: 0,
				options: 0,
				members: [],
				notice: { key: "UIIT_MSG_PARTY_BROKEN", value: 0, nativeType: 4 }
			};
		} else if ( type === 2 ) {
			// 761870 appends the row unconditionally, so a JOIN for a
			// member the roster already holds reaches this client only
			// as a replay on kept state after a resume. Refresh the held
			// member from the row instead of doubling it, and drop a
			// stranger's row when the roster is full, the way type 6
			// drops a row for a member the roster does not hold.
			// The JOIN line names the row itself and prints on every
			// row, appended, refreshed or dropped.
			const start = o;
			const row = partyMember();
			next = {
				...next,
				notice: { key: "UIIT_MSG_PARTY_JOIN", value: 0, nativeType: 4, arguments: [ row.name ] }
			};
			const old = next.members.find( m => m.id === row.id );
			if ( old ) {
				o = start;
				const member = partyMember( old );
				next = { ...next, members: next.members.map( m => m.id === old.id ? member : m ) };
			} else if ( next.members.length < 8 ) {
				next = { ...next, members: [ ...next.members, row ] };
			}
		} else if ( type === 3 ) {
			const id = u32();
			// Reason 3 (SERVER_MIGRATION) is a debug-only print and any
			// other reason carries no line; only 1, 2 and 4 print.
			const reason = u8();
			const name = id === next.self ? next.localName : next.members.find( m => m.id === id )?.name;
			const key = reason === 1 ?
				"UIIT_MSG_PARTY_LOGOUT" :
				reason === 2 ?
				"UIIT_MSG_PARTY_SECEDE" :
				reason === 4 ?
				"UIIT_MSG_PARTY_BOOTED" :
				null;
			if ( key !== null && name ) {
				next = { ...next, notice: { key, value: 0, nativeType: 4, arguments: [ name ] } };
			}
			next = id === next.self ?
				{ ...next, self: 0, leader: 0, options: 0, members: [] } :
				{ ...next, members: next.members.filter( m => m.id !== id ) };
		} else if ( type === 9 ) {
			// The server sends type 9 after the leader's type-3 leave when
			// the lead passes to the longest-standing member (party
			// registry Leave, #3438).
			const id = u32();
			const name = next.members.find( m => m.id === id )?.name;
			next = { ...next, leader: id };
			if ( name ) {
				next = {
					...next,
					notice: { key: "UIIT_MSG_PARTY_HOST_ROLE_DELEGATED", value: 0, nativeType: 4, arguments: [ name ] }
				};
			}
		} else if ( type === 6 ) {
			// 761870 type 6 reads the whole masked row (75DB30), then applies
			// only its level (2, 821F60), status (4, 821FD0), position (0x20,
			// 822110), guild (0x40, 822090) and masteries (8, 8221D0) to the
			// member the header names. Each setter walks the roster and skips
			// an id it does not hold, so a row for an unknown member changes
			// nothing; the row's own id, name, model and 0x80 byte are read
			// and dropped.
			const id = u32(), old = next.members.find( m => m.id === id );
			const row = partyMember( old ?? unknownPartyMember( id ) );
			if ( old ) {
				const member = { ...row, id: old.id, name: old.name, model: old.model, native41: old.native41 };
				next = { ...next, members: next.members.map( m => m.id === id ? member : m ) };
			}
		} else {
			throw Error( "Unsupported party update" );
		}
	}
	if ( o !== p.length ) {
		throw Error( "Trailing social bytes" );
	}
	return next;
}

import { partyNotice } from "./party-notices";
