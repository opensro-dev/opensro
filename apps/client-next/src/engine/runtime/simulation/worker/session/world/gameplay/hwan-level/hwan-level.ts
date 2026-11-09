/*
===========================================================================

hwan-level.ts - the Blue Berserk level (port-only 0x3FC0, not native)

With the server's cap-110 Blue Berserk quests, a character who finished the
Army / Military Test line is told its Blue Berserk level (0 to 6) at world
entry and whenever it rises. This owner decodes the frame whole or not at
all and keeps the latest level per object; every rule (what raises it, what
it absorbs, what the scrolls need) is the server's. Nothing arrives on a
native shard.

Body, little-endian: u32 object id, u8 level.

The later client draws a blue gauge and a title for the level; this client
shows the title through the server's notice and keeps the level here for the
gauge skin (hwan-skin.ts swaps the Berserk gauge to the pmi_jahwan_02 art; the
server also sends a notice with the title).

===========================================================================
*/
import type { WireFrame } from "@/engine/contracts/network";

export const OP_HWAN_LEVEL = 0x3fc0;
// Mirrors the server's domain.HwanLevelMax.
export const HWAN_LEVEL_MAX = 6;
const BODY_BYTES = 5;

/*
================
createHwanLevel
================
*/
export function createHwanLevel() {
	const levels = new Map<number, number>();
	return {
		/*
================
receive

Consumes a level frame; false for any other opcode. A malformed frame is a
protocol error, never a partial one.
================
		*/
		receive( frame: WireFrame ): boolean {
			if ( frame.opcode !== OP_HWAN_LEVEL ) return false;
			const p = frame.payload;
			if ( p.length !== BODY_BYTES ) throw Error( "Invalid Blue Berserk level length" );
			const level = p[4]!;
			if ( level > HWAN_LEVEL_MAX ) throw Error( "Invalid Blue Berserk level" );
			const gid = new DataView( p.buffer, p.byteOffset, p.byteLength ).getUint32( 0, true );
			if ( level === 0 ) levels.delete( gid );
			else levels.set( gid, level );
			return true;
		},
		/*
================
levelOf

The level last told for one object (0 when none).
================
		*/
		levelOf: ( gid: number ): number => levels.get( gid ) ?? 0,
		/*
================
clear
================
		*/
		clear() {
			levels.clear();
		}
	};
}
