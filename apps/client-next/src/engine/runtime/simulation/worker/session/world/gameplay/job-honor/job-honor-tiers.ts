/*
===========================================================================

job-honor-tiers.ts - the tier title roster (port-only 0x3FC7, not native)

#3530: with SRO_JOB_HONOR on, the server pushes the division's tier
roster - the entered characters holding a tier for the job they are
dressed as - whenever the bytes change, and the nameplate draws the
tier's coloured title by the name (overhead-layout.ts). Nothing
arrives with the flag off, and a client that predates the frame
drops the unknown opcode.

Body, little-endian: u16 count, then count x { u16 len, name bytes,
u8 tier } (1 Silver, 2 Gold, 3 King), names in sorted order.

===========================================================================
*/
import type { Degradation, WireFrame } from "@/engine/contracts/network";
import { viewOf } from "@/engine/foundation/binary/byte-view";
import { recordDegradation, type DegradationRecord } from "@/engine/foundation/session/degradation-record";

export const OP_JOB_HONOR_TIER_ROSTER = 0x3fc7;
const MAX_TIER_ROSTER = 1024;

/*
================
decodeName

One name off the wire, byte by byte: the bytes are the stored name,
the same text the spawn row's name field carries, so the nameplate
matches against the same string. A byte above ascii can never equal
a character name; it is kept as its code point and never matches.
================
*/
function decodeName( payload: Uint8Array, offset: number, length: number ): string {
	let name = "";
	for ( let i = 0; i < length; i++ ) {
		name += String.fromCharCode( payload[offset + i]! );
	}
	return name;
}

/*
================
createJobHonorTiers
================
*/
export function createJobHonorTiers() {
	let tiers: ReadonlyMap<string, number> = new Map();
	const record: DegradationRecord = { degradations: [], degradedKeys: new Set() };
	const machine = {
		/*
 ================
 receive

 Consumes a tier roster frame; false for any other opcode. The
 roster replaces the last one wholesale, so a character that lost
 its tier never keeps the title. A malformed roster is a protocol
 error, never a partial list.
 ================
 */
		receive( frame: WireFrame ): boolean {
			if ( frame.opcode !== OP_JOB_HONOR_TIER_ROSTER ) return false;
			const p = frame.payload, v = viewOf( p );
			if ( p.length < 2 ) throw new Error( "Invalid job honor tier roster" );
			const count = v.getUint16( 0, true );
			if ( count > MAX_TIER_ROSTER ) throw new Error( "Invalid job honor tier count" );
			const next = new Map<string, number>();
			let offset = 2;
			for ( let i = 0; i < count; i++ ) {
				if ( offset + 2 > p.length ) throw new Error( "Invalid job honor tier name" );
				const length = v.getUint16( offset, true );
				offset += 2;
				if ( offset + length + 1 > p.length ) throw new Error( "Invalid job honor tier name" );
				const name = decodeName( p, offset, length );
				const tier = p[offset + length]!;
				offset += length + 1;
				if ( tier < 1 || tier > 3 ) throw new Error( "Invalid job honor tier" );
				next.set( name, tier );
			}
			if ( offset !== p.length ) throw new Error( "Invalid job honor tier roster" );
			tiers = next;
			return true;
		},
		/*
 ================
 receiveContained

 receive with the damage fenced in: a malformed roster keeps the
 last good one and is recorded as a degradation (once per distinct
 message), so this lane can only ever lose its own titles, never
 the session - the containment the market lane learned.
 ================
 */
		receiveContained( frame: WireFrame ): boolean {
			try {
				return machine.receive( frame );
			} catch ( error ) {
				recordDegradation(
					record,
					"job_honor_tier_roster_rejected",
					error instanceof Error ? error.message : String( error )
				);
				return true;
			}
		},
		tiers: () => tiers,
		/*
 ================
 takeDegradations

 Drains the contained rejections for the world's degraded reports.
 ================
 */
		takeDegradations(): Degradation[] {
			return record.degradations.splice( 0 );
		},
		/*
 ================
 clear
 ================
 */
		clear() {
			tiers = new Map();
		}
	};
	return machine;
}
