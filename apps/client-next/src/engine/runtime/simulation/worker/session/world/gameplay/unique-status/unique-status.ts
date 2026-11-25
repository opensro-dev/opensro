/*
===========================================================================

unique-status.ts - the Unique Status feed (port-only, not native)

When the operator enables it (SRO_UNIQUE_STATUS), every entered client
receives every unique's state for the Unique Status window: one snapshot
(0x3FC4) when its session is first seen, then an update (0x3FC5) per
unique whose state changed. This owner decodes them into the latest
feed; nothing arrives when it is off, and a server with the flag off
sends nothing at all.

Body, little-endian: snapshot is u64 serverNowMs, u16 count, then the
rows; an update is u64 serverNowMs and one row. A row is u32 refObjID,
u8 nameLength, name bytes, u8 level, u16 regionID, u8 alive, u64
diedAtMs (0 = never), u8 killerLength, killer bytes.

===========================================================================
*/
import type { WireFrame } from "@/engine/contracts/network";
import type { UniqueStatusFeed, UniqueStatusRow } from "@/engine/contracts/unique-status";

export const OP_UNIQUE_STATUS_SNAPSHOT = 0x3fc4;
export const OP_UNIQUE_STATUS_UPDATE = 0x3fc5;
const MAX_ROWS = 256;
const HEADER_BYTES = 8;

const names = new TextDecoder( "utf-8", { fatal: true } );

/*
================
readUniqueStatusRow

One row off the wire at `at`, and the offset just past it. The name and
killer are the server's UTF-8 text; malformed bytes are a protocol
error, never a partial row.
================
*/
function readUniqueStatusRow( p: Uint8Array, v: DataView, at: number ): readonly [UniqueStatusRow, number] {
	if ( at + 5 > p.length ) throw new Error( "Truncated unique status row" );
	const refObjId = v.getUint32( at, true );
	at += 4;
	const nameLength = p[at]!;
	at += 1;
	if ( at + nameLength + 13 > p.length ) throw new Error( "Truncated unique status row" );
	const name = names.decode( p.subarray( at, at + nameLength ) );
	at += nameLength;
	const level = p[at]!;
	const regionId = v.getUint16( at + 1, true );
	const alive = p[at + 3] !== 0;
	const diedAtMs = Number( v.getBigUint64( at + 4, true ) );
	at += 12;
	const killerLength = p[at]!;
	at += 1;
	if ( at + killerLength > p.length ) throw new Error( "Truncated unique status row" );
	const killer = names.decode( p.subarray( at, at + killerLength ) );
	return [ { refObjId, name, level, regionId, alive, diedAtMs, killer }, at + killerLength ];
}

/*
================
createUniqueStatus
================
*/
export function createUniqueStatus() {
	let feed: UniqueStatusFeed | null = null;
	// Never reset: the HUD opens and alerts on a rise, so a revision
	// restarting after a teleport would hide every later change.
	let revision = 0;
	return {
		/*
================
receive

Consumes a status frame; false for any other opcode. A snapshot
replaces the list wholesale; an update replaces (or first learns) its
one row. A malformed frame is a protocol error, never a partial list.
================
		*/
		receive( frame: WireFrame ): boolean {
			if ( frame.opcode !== OP_UNIQUE_STATUS_SNAPSHOT && frame.opcode !== OP_UNIQUE_STATUS_UPDATE ) return false;
			const p = frame.payload, v = new DataView( p.buffer, p.byteOffset, p.byteLength );
			if ( p.length < HEADER_BYTES ) throw new Error( "Truncated unique status frame" );
			const serverNowMs = Number( v.getBigUint64( 0, true ) );
			if ( frame.opcode === OP_UNIQUE_STATUS_SNAPSHOT ) {
				if ( p.length < HEADER_BYTES + 2 ) throw new Error( "Truncated unique status snapshot" );
				const count = v.getUint16( HEADER_BYTES, true );
				if ( count > MAX_ROWS ) throw new Error( "Invalid unique status count" );
				const rows: UniqueStatusRow[] = [];
				let at = HEADER_BYTES + 2;
				for ( let i = 0; i < count; i++ ) {
					const [row, next] = readUniqueStatusRow( p, v, at );
					rows.push( row );
					at = next;
				}
				if ( at !== p.length ) throw new Error( "Invalid unique status snapshot tail" );
				feed = { serverNowMs, rows, revision: ++revision };
				return true;
			}
			const [row, next] = readUniqueStatusRow( p, v, HEADER_BYTES );
			if ( next !== p.length ) throw new Error( "Invalid unique status update tail" );
			const rows = feed ? feed.rows.filter( ( r ) => r.refObjId !== row.refObjId ) : [];
			rows.push( row );
			rows.sort( ( a, b ) => a.refObjId - b.refObjId );
			feed = { serverNowMs, rows, revision: ++revision };
			return true;
		},
		current: () => feed,
		/*
================
clear

Drops the feed but keeps the revision (see above). The server sends a
fresh snapshot when the new session is first seen.
================
		*/
		clear() {
			feed = null;
		}
	};
}
