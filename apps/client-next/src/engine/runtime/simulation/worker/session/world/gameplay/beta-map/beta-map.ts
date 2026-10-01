/*
===========================================================================

beta-map.ts - the beta world map roster (port-only 0x3FB0, not native)

When the operator enables it (SRO_BETA_PLAYER_MAP), the server pushes the
ground position of every player in the division every two seconds. This
owner decodes the latest roster for the world map (M); retail shows only
the local player and party members, and nothing arrives when it is off.

Body, little-endian: u16 count, then count x { u32 gid, u16 region,
f32 x, f32 z, u8 nameLength, name bytes }.

===========================================================================
*/
import type { WireFrame } from "@/engine/contracts/network";
import type { BetaMapPlayer } from "@/engine/contracts/gameplay";

export const OP_BETA_PLAYER_MAP = 0x3fb0;
const MAX_PLAYERS = 1024;

/*
================
createBetaPlayerMap
================
*/
export function createBetaPlayerMap() {
	let players: readonly BetaMapPlayer[] = [];
	const names = new TextDecoder( "utf-8", { fatal: true } );
	return {
		/*
================
receive

Consumes a roster frame; false for any other opcode. A malformed roster
is a protocol error, never a partial list.
================
		*/
		receive( frame: WireFrame ): boolean {
			if ( frame.opcode !== OP_BETA_PLAYER_MAP ) return false;
			const p = frame.payload, v = new DataView( p.buffer, p.byteOffset, p.byteLength );
			if ( p.length < 2 ) throw new Error( "Invalid beta player map" );
			const count = v.getUint16( 0, true );
			if ( count > MAX_PLAYERS ) throw new Error( "Invalid beta player map count" );
			const next: BetaMapPlayer[] = [];
			let at = 2;
			for ( let i = 0; i < count; i++ ) {
				if ( at + 15 > p.length ) throw new Error( "Truncated beta player map" );
				const gid = v.getUint32( at, true ), regionId = v.getUint16( at + 4, true );
				const x = v.getFloat32( at + 6, true ), z = v.getFloat32( at + 10, true ), length = p[at + 14]!;
				at += 15;
				if ( at + length > p.length || !Number.isFinite( x ) || !Number.isFinite( z ) ) {
					throw new Error( "Invalid beta player map row" );
				}
				next.push( { gid, regionId, x, z, name: names.decode( p.subarray( at, at + length ) ) } );
				at += length;
			}
			if ( at !== p.length ) throw new Error( "Trailing beta player map bytes" );
			players = next;
			return true;
		},
		players: () => players,
		/*
================
clear
================
		*/
		clear() {
			players = [];
		}
	};
}
