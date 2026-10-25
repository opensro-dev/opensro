/*
===========================================================================

unique-rank.ts - a unique kill's damage ranking (port-only 0x3FBC, not native)

When the operator enables it (SRO_UNIQUE_DAMAGE_RANK), everyone who
damaged a dying unique receives the twelve players who did the most damage
and their own place. This owner decodes the latest ranking for the ranking
window; nothing arrives when it is off.

Body, little-endian: u32 refObjID, u64 totalDamage, u8 count, then
count x { u8 nameLength, name bytes, u32 damage, u8 partyRank }, then
u16 selfRank, u32 selfDamage, u8 selfPartyRank.

===========================================================================
*/
import type { WireFrame } from "@/engine/contracts/network";
import type { UniqueDamageRank, UniqueDamageRow } from "@/engine/contracts/unique-rank";

export const OP_UNIQUE_DAMAGE_RANK = 0x3fbc;
const MAX_ROWS = 12;
const HEADER_BYTES = 13;
const SELF_BYTES = 7;

/*
================
createUniqueDamageRank
================
*/
export function createUniqueDamageRank() {
	let ranking: UniqueDamageRank | null = null;
	// Never reset: the window opens on a rise, so a sequence restarting after
	// a teleport would hide every later kill.
	let sequence = 0;
	const names = new TextDecoder( "utf-8", { fatal: true } );
	return {
		/*
================
receive

Consumes a ranking frame; false for any other opcode. The unique's name
comes from the bootstrap's monster references. A malformed ranking is a
protocol error, never a partial list.
================
		*/
		receive( frame: WireFrame, refs: ReadonlyMap<number, { readonly name: string; }> ): boolean {
			if ( frame.opcode !== OP_UNIQUE_DAMAGE_RANK ) return false;
			const p = frame.payload, v = new DataView( p.buffer, p.byteOffset, p.byteLength );
			if ( p.length < HEADER_BYTES + SELF_BYTES ) throw new Error( "Truncated unique damage rank" );
			const refObjId = v.getUint32( 0, true ), total = Number( v.getBigUint64( 4, true ) ), count = p[12]!;
			if ( count > MAX_ROWS ) throw new Error( "Invalid unique damage rank count" );
			const rows: UniqueDamageRow[] = [];
			let at = HEADER_BYTES;
			for ( let i = 0; i < count; i++ ) {
				if ( at + 1 > p.length ) throw new Error( "Truncated unique damage rank row" );
				const length = p[at]!;
				if ( at + 1 + length + 5 > p.length ) throw new Error( "Truncated unique damage rank row" );
				const name = names.decode( p.subarray( at + 1, at + 1 + length ) );
				at += 1 + length;
				rows.push( { name, damage: v.getUint32( at, true ), partyRank: p[at + 4]! } );
				at += 5;
			}
			if ( at + SELF_BYTES !== p.length ) throw new Error( "Invalid unique damage rank tail" );
			ranking = {
				sequence: ++sequence,
				refObjId,
				uniqueName: refs.get( refObjId )?.name ?? "",
				total,
				rows,
				selfRank: v.getUint16( at, true ),
				selfDamage: v.getUint32( at + 2, true ),
				selfPartyRank: p[at + 6]!
			};
			return true;
		},
		current: () => ranking,
		/*
================
clear

Drops the ranking but keeps the sequence (see above).
================
		*/
		clear() {
			ranking = null;
		}
	};
}
