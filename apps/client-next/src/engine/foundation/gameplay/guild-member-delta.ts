/*
===========================================================================

guild-member-delta.ts - the 0x3B29 member delta: one row's legs and its texts

subOp 6 changes one member and subOp 0x16 a batch under one mask; both
read each member's legs in the client applicator's order (5E9D20) and
announce an offline flip the way the client's slot updater does
(5F3340). social-guild-union.ts lends the frame cursor and keeps the
rest of the 0x3B29 sub-ops.

===========================================================================
*/
import type { Guild } from "./social-roster";
import type { SystemNotice } from "./system-notices";

// The legs the applicator knows (&1 level .. &0x40 role); any other bit
// leaves the rest of the frame unreadable.
const MEMBER_DELTA_LEGS = 0x7f;

/*
================
GuildMemberDeltaInput

The frame cursor social-guild-union.ts lends, the guild block the delta
applies to and the local character's name (the self arm of 5F3340).
================
*/
export interface GuildMemberDeltaInput {
	readonly batch: boolean;
	readonly guild: Guild;
	readonly localName: string;
	readonly u8: () => number;
	readonly u32: () => number;
	readonly str: () => string;
}

/*
================
GuildMemberDeltaResult

The guild with the deltas applied, the connect and disconnect texts the
flips print, and a degraded step when a delta named a member the roster
does not hold.
================
*/
export interface GuildMemberDeltaResult {
	readonly guild: Guild;
	readonly notices: readonly SystemNotice[];
	readonly degradation?: { readonly code: string; readonly message: string; };
}

/*
================
readGuildMemberDelta

Reads a subOp 6 or 0x16 body after its subOp byte and applies it.
================
*/
export function readGuildMemberDelta( input: GuildMemberDeltaInput ): GuildMemberDeltaResult {
	const u8 = input.u8, u32 = input.u32, str = input.str, localName = input.localName;
	let guild = input.guild, degradation: GuildMemberDeltaResult["degradation"];
	const count = input.batch ? u8() : 1, commonMask = input.batch ? u8() : undefined;
	const flips: SystemNotice[] = [];
	for ( let i = 0; i < count; i++ ) {
		const id = u32(), mask = commonMask ?? u8(), old = guild.members.find( m => m.id === id );
		if ( mask & ~MEMBER_DELTA_LEGS ) throw Error( "Unsupported guild member delta" );
		// 5E9D20 @5e9d90: the applicator order is &1 level (+0x24),
		// &2 offline (+0x26), &4 grade (+0x25), then donated,
		// permissions, name, role. (The &1/&2 legs stood swapped
		// here until #2281; both are u8 reads, so only the field
		// assignment was wrong.)
		const patch = {
			...(mask & 1 ? { level: u8() } : {}),
			...(mask & 2 ? { offline: u8() } : {}),
			...(mask & 4 ? { grade: u8() } : {}),
			...(mask & 8 ? { donated: u32() } : {}),
			...(mask & 16 ? { permissions: u32() } : {}),
			...(mask & 32 ? { name: str() } : {}),
			...(mask & 64 ? { role: u8() } : {})
		};
		// A member this roster does not hold: the block was built before
		// a join this client never received. The legs are read above, so
		// the frame still parses; the delta is dropped as a degraded step
		// instead of ending the session (#3211). INFERENCE: the client
		// applicator (5E9D20) finds the member by jid before writing, so a
		// missing row has nothing to write; no trace of the miss arm.
		if ( !old ) {
			degradation = {
				code: "guild-member-delta-unknown",
				message: "Guild member delta for unheld member " + id
			};
			continue;
		}
		// 5F3340: the announcement block is gated on the offline
		// leg (mask &2 @5f3392) and prints only on an actual
		// flip; the native row latch (+0x374, set @5f36d1 and
		// cleared by the disconnect arm @5f38e0) collapses to
		// the transition itself in this state model.
		if ( (mask & 2) && patch.offline !== undefined && patch.offline !== old.offline ) {
			const self = old.name === localName;
			if ( patch.offline === 0 ) {
				if ( self ) {
					// @5f3687: the self leg prints the guild notice
					// text when one exists - the object at record
					// +0x50 (buffer +0x54, size +0x64), which the
					// 5E4710 write arm fills from wire string #1
					// @5e4a64: the port's subject. Contents is
					// wire string #2, the window body.
					if ( guild.subject ) {
						flips.push( {
							key: "UIIT_MSG_GUILD_CONNECT_NOTICE",
							value: 0,
							nativeType: 0,
							arguments: [ guild.subject ]
						} );
					}
				} else {
					flips.push( {
						key: "UIIT_MSG_GUILD_CONNECT",
						value: 0,
						nativeType: 0,
						arguments: [ old.name ]
					} );
				}
			} else if ( !self ) {
				flips.push( {
					key: "UIIT_MSG_GUILD_DISCONNECT",
					value: 0,
					nativeType: 0,
					arguments: [ old.name ]
				} );
			}
		}
		guild = { ...guild, members: guild.members.map( m => m.id === id ? { ...m, ...patch } : m ) };
	}
	return { guild, notices: flips, degradation };
}
