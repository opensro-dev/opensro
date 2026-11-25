/*
===========================================================================

styria.ts - the Styria Clash status feed (port-only 0x3FC8, not native)

#3648 slice 5: with SRO_STYRIA_CLASH on, the server sends a Styria
Clash status snapshot (internal/game/item/wire/styria.go) whenever
an applicant's, registrant's or entered participant's standing
changes. This owner decodes the latest snapshot for the
registration, score and result windows; nothing arrives when the
flag is off. A result snapshot is also latched: the participant is
returned to town as the world closes, and the result window holds
the verdict until the player dismisses it.

Body, little-endian (56 bytes): u8 phase (0 registration, 1 match,
2 result), u32 matchSeq, u64 nextStartMs, u64 windowOpensMs,
u64 phaseEndsMs, u8 ownSide (0 Hunters, 1 Thieves, 255 none),
u8 ownState (0 watching, 1 applied, 2 entered), u16 hunters,
u16 thieves, u16 scoreHunters, u16 scoreThieves, u8 outcome
(0 open, 1 Hunters, 2 Thieves, 3 draw), u32 jobExp, u32 skillExp,
u64 serverNowMs.

===========================================================================
*/
import type { StyriaFeed, StyriaPhase } from "@/engine/contracts/styria";
import type { WireFrame } from "@/engine/contracts/network";
import { viewOf } from "@/engine/foundation/binary/byte-view";

export const OP_STYRIA_STATUS = 0x3fc8;
const BODY_BYTES = 56;

const PHASES: readonly StyriaPhase[] = [ "registration", "match", "result" ];

/*
================
createStyria
================
*/
export function createStyria() {
	let feed: StyriaFeed | null = null;
	let latched: StyriaFeed | null = null;
	// Never reset: a revision restarting after a teleport would look
	// like the standing standing still to the windows.
	let revision = 0;

	return {
		/*
 ================
 receive

 Consumes a Styria status snapshot; false for any other opcode. A
 snapshot replaces the feed wholesale; a result also latches. A
 malformed snapshot is a protocol error.
 ================
 */
		receive( frame: WireFrame ): boolean {
			if ( frame.opcode !== OP_STYRIA_STATUS ) return false;
			const p = frame.payload;
			if ( p.length !== BODY_BYTES ) throw new Error( "Invalid styria status length" );
			const phase = PHASES[p[0]!];
			if ( !phase ) throw new Error( "Invalid styria status phase" );
			const sideByte = p[29]!;
			if ( sideByte !== 0 && sideByte !== 1 && sideByte !== 255 ) {
				throw new Error( "Invalid styria status side" );
			}
			const state = p[30]!;
			if ( state > 2 ) throw new Error( "Invalid styria status state" );
			const outcome = p[39]!;
			if ( outcome > 3 ) throw new Error( "Invalid styria status outcome" );
			const v = viewOf( p );
			const next: StyriaFeed = {
				phase,
				matchSeq: v.getUint32( 1, true ),
				nextStartMs: Number( v.getBigUint64( 5, true ) ),
				windowOpensMs: Number( v.getBigUint64( 13, true ) ),
				phaseEndsMs: Number( v.getBigUint64( 21, true ) ),
				ownSide: sideByte === 255 ? -1 : sideByte,
				ownState: state,
				hunters: v.getUint16( 31, true ),
				thieves: v.getUint16( 33, true ),
				scoreHunters: v.getUint16( 35, true ),
				scoreThieves: v.getUint16( 37, true ),
				outcome,
				jobExp: v.getUint32( 40, true ),
				skillExp: v.getUint32( 44, true ),
				serverNowMs: Number( v.getBigUint64( 48, true ) ),
				revision: ++revision
			};
			feed = next;
			if ( next.phase === "result" ) latched = next;
			return true;
		},
		current: () => feed,
		// The latched result (a phase-2 snapshot), or null. The HUD
		// owns its dismissal; a later match's snapshot never clears
		// it from here.
		result: () => latched,
		/*
 ================
 clear

 Drops the feed and the latch but keeps the revision (see above).
 ================
 */
		clear() {
			feed = null;
			latched = null;
		}
	};
}
