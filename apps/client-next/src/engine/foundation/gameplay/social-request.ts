/*
===========================================================================

social-request.ts - outgoing party, guild and union commands

Validates command inputs and encodes native request payloads. Incoming
rosters, invitations and guild updates remain owned by social.ts.

===========================================================================
*/
import type { WireFrame } from "@/engine/contracts/network";
import { UNION_PROPOSAL, type SocialState } from "./social";
import { unionRequest, type UnionCommand } from "./guild-union";

/*
================
SocialCommand
================
*/
export type SocialCommand = {
	kind: "party-invite";
	gid: number;
	options: number;
} | {
	kind: "party-kick";
	id: number;
} | {
	kind: "party-leave";
} | {
	kind: "social-consent";
	accept: boolean;
	automatic?: boolean;
} | {
	kind: "resurrection-consent";
	accept: boolean;
} | {
	kind: "guild-create";
	gid: number;
	name: string;
} | {
	kind: "guild-invite";
	gid: number;
} | {
	kind: "guild-kick";
	name: string;
} | {
	kind: "guild-leave" | "guild-dissolve";
	gid: number;
} | {
	kind: "guild-notice";
	subject: string;
	contents: string;
} | {
	kind: "guild-donate";
	amount: number;
} | {
	kind: "guild-title";
	id: number;
	name: string;
} | {
	kind: "guild-role";
	id: number;
	role: number;
} | {
	kind: "guild-level-up" | "guild-compensation" | "guild-compensation-claim" | "guild-release";
	gid: number;
} | {
	kind: "guild-master-leave";
	gid: number;
	id: number;
} | {
	kind: "guild-vote";
	gid: number;
	vote: number;
	option: number;
} | UnionCommand;

/*
================
socialRequest
================
*/
export function socialRequest( state: SocialState, c: SocialCommand ): WireFrame {
	const bytes: number[] = [];
	/*
	================
	uint
	================
	*/
	function uint( n: number, max: number ) {
		if ( !Number.isInteger( n ) || n < 0 || n > max ) {
			throw Error( "Invalid social reference" );
		}
	}
	/*
	================
	u8
	================
	*/
	function u8( n: number ) {
		uint( n, 255 );
		bytes.push( n );
	}
	/*
	================
	u32
	================
	*/
	function u32( n: number ) {
		uint( n, 0xffffffff );
		bytes.push( n & 255, n >>> 8 & 255, n >>> 16 & 255, n >>> 24 );
	}
	/*
	================
	str
	================
	*/
	function str( value: string, max: number ) {
		const encoded = new TextEncoder().encode( value );
		if ( !encoded.length || encoded.length > max || value.includes( "\0" ) ) {
			throw Error( "Invalid social text" );
		}
		bytes.push( encoded.length & 255, encoded.length >>> 8 );
		bytes.push( ...encoded );
	}
	let opcode: number;
	switch ( c.kind ) {
		case "party-invite":
			if ( c.options & ~7 ) {
				throw Error( "Invalid party options" );
			}
			if ( state.leader && state.self !== state.leader && !(state.options & 4) ) {
				throw Error( "Only the party leader may invite" );
			}
			u32( c.gid );
			if ( !state.leader ) {
				u8( c.options );
			}
			opcode = state.leader ? 0x751a : 0x70d5;
			break;
		case "party-kick":
			if (
				!state.self || state.self !== state.leader || c.id === state.self ||
				!state.members.some( m => m.id === c.id )
			) {
				throw Error( "Invalid party removal" );
			}
			u32( c.id );
			opcode = 0x7664;
			break;
		case "party-leave":
			if ( !state.leader ) {
				throw Error( "You are not in a party" );
			}
			opcode = 0x704f;
			break;
		case "social-consent":
			if ( !state.invitation ) {
				throw Error( "Invitation is no longer available" );
			}
			if ( c.accept ) {
				u8( 1 );
				u8( 1 );
			} else if ( state.invitation.type !== 1 ) {
				// CGInterface_OnMsgBoxResult 6971B0: the union box (case 0x1A)
				// refuses with {2, 0}, the guild box (case 0xC) {2, 0x16}.
				u8( 2 );
				u8(
					state.invitation.type === 2 ?
						0x0c :
						state.invitation.type === 3 ?
						0x17 :
						state.invitation.type === UNION_PROPOSAL ?
						0 :
						0x16
				);
			} else {
				u8( 1 );
				u8( c.automatic ? 0 : 2 );
			}
			opcode = 0x3393;
			break;
		case "resurrection-consent":
			if ( !state.resurrection ) {
				throw Error( "Resurrection is no longer available" );
			}
			// Box kind 4 answers through CGInterface_OnMsgBoxResult case 1 as
			// {1, button}: button 1 is yes (526020), 2 is no (52C800).
			u8( 1 );
			u8( c.accept ? 1 : 2 );
			opcode = 0x3393;
			break;
		case "guild-create":
			if ( state.guild ) {
				throw Error( "You are already in a guild" );
			}
			u32( c.gid );
			str( c.name, 127 );
			opcode = 0x7663;
			break;
		case "guild-invite":
			u32( c.gid );
			opcode = 0x73ad;
			break;
		case "guild-kick":
			str( c.name, 127 );
			opcode = 0x74b1;
			break;
		case "guild-leave":
		case "guild-dissolve":
			u32( c.gid );
			opcode = c.kind === "guild-leave" ? 0x756e : 0x766e;
			break;
		case "guild-notice":
			str( c.subject, 128 );
			str( c.contents, 1024 );
			opcode = 0x777a;
			break;
		case "guild-donate":
			if ( !c.amount ) {
				throw Error( "Donation must be positive" );
			}
			u32( c.amount );
			opcode = 0x740f;
			break;
		case "guild-title":
			u32( c.id );
			str( c.name, 127 );
			opcode = 0x72bc;
			break;
		case "guild-role":
			u32( c.id );
			u8( c.role );
			opcode = 0x765f;
			break;
		// The guild manager's rows (5DA1B0): level-up window 0x73F0 (5EF8B0),
		// war compensation 0x7140 then the claim box's 0x73F7, the release
		// box's 0x76DC, the master-leave box's 0x77D4 [npc][member] and the
		// election window's 0x7330 [npc][vote][option] (5EFF00).
		case "guild-level-up":
		case "guild-compensation":
		case "guild-compensation-claim":
		case "guild-release":
			u32( c.gid );
			opcode = {
				"guild-level-up": 0x73f0,
				"guild-compensation": 0x7140,
				"guild-compensation-claim": 0x73f7,
				"guild-release": 0x76dc
			}[c.kind];
			break;
		case "guild-master-leave":
			u32( c.gid );
			u32( c.id );
			opcode = 0x77d4;
			break;
		case "guild-vote":
			u32( c.gid );
			u32( c.vote );
			u8( c.option );
			opcode = 0x7330;
			break;
		// The union's requests and the rights grant (guild-union.ts).
		case "guild-union-invite":
		case "guild-union-leave":
		case "guild-union-kick":
		case "guild-permissions":
			if ( !state.guild ) throw Error( "You are not in a guild" );
			return unionRequest( c );
	}
	if ( c.kind.startsWith( "guild-" ) && c.kind !== "guild-create" && !state.guild ) {
		throw Error( "You are not in a guild" );
	}
	return { opcode, payload: Uint8Array.from( bytes ) };
}
