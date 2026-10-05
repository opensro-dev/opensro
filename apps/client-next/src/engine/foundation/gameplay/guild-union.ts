/*
===========================================================================

guild-union.ts - the guild union's requests and the rights grant

CIFAllianceGuild (5F7860 / 5F5690) invites the selected player's guild
into the union (0x7379 [u32 gid]), leaves it (0x7795) or expels a guild by
id (0x7680 [u32 guild]); CIFGuildGrantPower (5EE1C0) sends the members'
rights (0x744E [u8 count] then [u32 jid][u32 rights]). The social owner
routes these commands here once it has checked the player's guild.

===========================================================================
*/
import type { WireFrame } from "@/engine/contracts/network";

const MAX_GRANTS = 255;

/*
================
UnionCommand
================
*/
export type UnionCommand =
	| { readonly kind: "guild-union-invite"; readonly gid: number; }
	| { readonly kind: "guild-union-leave"; }
	| { readonly kind: "guild-union-kick"; readonly id: number; }
	| {
		readonly kind: "guild-permissions";
		readonly grants: readonly { readonly id: number; readonly permissions: number; }[];
	};

/*
================
unionRequest
================
*/
export function unionRequest( c: UnionCommand ): WireFrame {
	const bytes: number[] = [];
	const u32 = ( n: number ) => {
		if ( !Number.isInteger( n ) || n < 0 || n > 0xffffffff ) throw Error( "Invalid social reference" );
		bytes.push( n & 255, n >>> 8 & 255, n >>> 16 & 255, n >>> 24 );
	};
	const frame = ( opcode: number ) => ({ opcode, payload: Uint8Array.from( bytes ) });
	switch ( c.kind ) {
		case "guild-union-invite":
			u32( c.gid );
			return frame( 0x7379 );
		case "guild-union-leave":
			return frame( 0x7795 );
		case "guild-union-kick":
			u32( c.id );
			return frame( 0x7680 );
		case "guild-permissions":
			if ( c.grants.length > MAX_GRANTS ) throw Error( "Too many rights" );
			bytes.push( c.grants.length );
			for ( const grant of c.grants ) {
				u32( grant.id );
				u32( grant.permissions );
			}
			return frame( 0x744e );
	}
}
