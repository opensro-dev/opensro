/*
===========================================================================

job-honor.ts - the Job Honor ranking feed (port-only 0x3FC6, not native)

#3530: with SRO_JOB_HONOR on, the server sends one full snapshot of
the job honor board (current and previous periods) with the viewer's
own standing whenever any of it changes. This owner decodes the
latest snapshot for the ranking window; nothing arrives when the
flag is off.

Body, little-endian: u64 serverNowMs, u64 nextChangeMs,
u64 previousPeriodEndMs (0 = none), u16 currentCount, rows,
u16 previousCount, rows, then the own block (see the server wire,
internal/game/item/wire/jobhonor.go). Row: u8 job, u8 aliasLength,
alias bytes, u32 points, u8 tierHeld, u8 tierTonight. Own: u8
present; when 1: u8 job, u16 place, u8 tierHeld, u8 tierTonight,
u32 capLeft, 3 x u32 today (trader, thief, hunter), 3 x u32 month.

===========================================================================
*/
import type { JobHonorFeed, JobHonorOwn, JobHonorRow } from "@/engine/contracts/job-honor";
import type { WireFrame } from "@/engine/contracts/network";
import { viewOf } from "@/engine/foundation/binary/byte-view";

export const OP_JOB_HONOR_SNAPSHOT = 0x3fc6;
const MAX_ROWS = 256;
const HEADER_BYTES = 24;

/*
================
createJobHonor
================
*/
export function createJobHonor() {
	let feed: JobHonorFeed | null = null;
	// Never reset: a revision restarting after a teleport would look
	// like the board standing still to the window.
	let revision = 0;
	const names = new TextDecoder( "utf-8", { fatal: true } );

	/*
 ================
 readRows

 count rows at `at`; returns them and the offset past them. A short
 or oversized frame is a protocol error, never a partial board.
 ================
 */
	function readRows( payload: Uint8Array, at: number, count: number ): { rows: JobHonorRow[]; next: number; } {
		if ( count > MAX_ROWS ) throw new Error( "Invalid job honor row count" );
		const view = viewOf( payload );
		const rows: JobHonorRow[] = [];
		for ( let i = 0; i < count; i++ ) {
			if ( at + 2 > payload.length ) throw new Error( "Truncated job honor row" );
			const job = payload[at]!;
			const length = payload[at + 1]!;
			if ( at + 2 + length + 6 > payload.length ) throw new Error( "Truncated job honor row" );
			const alias = names.decode( payload.subarray( at + 2, at + 2 + length ) );
			at += 2 + length;
			rows.push( {
				job,
				alias,
				points: view.getUint32( at, true ),
				tier: payload[at + 4]!,
				tonight: payload[at + 5]!
			} );
			at += 6;
		}
		return { rows, next: at };
	}

	return {
		/*
 ================
 receive

 Consumes a job honor snapshot; false for any other opcode. A
 snapshot replaces the feed wholesale, so a row the server dropped
 never lingers. A malformed snapshot is a protocol error.
 ================
 */
		receive( frame: WireFrame ): boolean {
			if ( frame.opcode !== OP_JOB_HONOR_SNAPSHOT ) return false;
			const p = frame.payload, v = viewOf( p );
			if ( p.length < HEADER_BYTES + 2 + 1 ) throw new Error( "Truncated job honor snapshot" );
			const serverNowMs = Number( v.getBigUint64( 0, true ) );
			const nextChangeMs = Number( v.getBigUint64( 8, true ) );
			const previousEndMs = Number( v.getBigUint64( 16, true ) );
			const current = readRows( p, HEADER_BYTES + 2, v.getUint16( HEADER_BYTES, true ) );
			let at = current.next;
			if ( at + 2 > p.length ) throw new Error( "Truncated job honor snapshot" );
			const previous = readRows( p, at + 2, v.getUint16( at, true ) );
			at = previous.next;
			if ( at + 1 > p.length ) throw new Error( "Truncated job honor snapshot" );
			const present = p[at]!;
			if ( present !== 0 && present !== 1 ) throw new Error( "Invalid job honor own block" );
			let own: JobHonorOwn = {
				present: false,
				job: 0,
				place: 0,
				tier: 0,
				tonight: 0,
				capLeft: 0,
				today: [ 0, 0, 0 ],
				month: [ 0, 0, 0 ]
			};
			if ( present === 1 ) {
				if ( at + 34 !== p.length ) throw new Error( "Invalid job honor own block" );
				const today: [number, number, number] = [
					v.getUint32( at + 10, true ),
					v.getUint32( at + 14, true ),
					v.getUint32( at + 18, true )
				];
				const month: [number, number, number] = [
					v.getUint32( at + 22, true ),
					v.getUint32( at + 26, true ),
					v.getUint32( at + 30, true )
				];
				own = {
					present: true,
					job: p[at + 1]!,
					place: v.getUint16( at + 2, true ),
					tier: p[at + 4]!,
					tonight: p[at + 5]!,
					capLeft: v.getUint32( at + 6, true ),
					today,
					month
				};
			} else if ( at + 1 !== p.length ) {
				throw new Error( "Invalid job honor own block" );
			}
			feed = {
				serverNowMs,
				nextChangeMs,
				previousEndMs,
				current: current.rows,
				previous: previous.rows,
				own,
				revision: ++revision
			};
			return true;
		},
		current: () => feed,
		/*
 ================
 clear

 Drops the feed but keeps the revision (see above).
 ================
 */
		clear() {
			feed = null;
		}
	};
}
