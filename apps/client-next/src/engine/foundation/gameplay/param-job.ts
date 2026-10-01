/*
===========================================================================

param-job.ts - item parameter jobs on the buff board (kind 4)

EXP and skill-EXP scrolls run as timed ParamKeeper jobs on the server. The
client draws each as a CIFMagicStateBoard row keyed by the internal param
item (TID 3/3/3/10), through CGInterface_UpdateMagicStateSlot kind 4:

	0x3602 start   76F6E0  [u32 owner][u32 remaining seconds][u32 item ref]
	0x32AF resume  76F750  the same body, after world entry
	0x36D4 end     76F7C0  [u32 owner][u32 item ref]

6E6E00 kind 4 counts the row down from the remaining seconds against the
item's duration, one bar.

===========================================================================
*/
import type { WireFrame } from "@/engine/contracts/network";

export const OP_PARAM_JOB_START = 0x3602;
export const OP_PARAM_JOB_RESUME = 0x32af;
export const OP_PARAM_JOB_END = 0x36d4;

// TID 3/3/3/10 in the itemdata type word, the bits 0xFFFC keeps:
// (3 << 2) | (3 << 5) | (3 << 7) | (10 << 11).
const PARAM_ITEM_TYPE = 0x51ec;
const TYPE_MASK = 0xfffc;

/*
================
ParamJobRow
================
*/
export interface ParamJobRow {
	readonly itemRefObjId: number;
	readonly remainingSec: number;
	readonly receivedAtMs: number;
}

/*
================
ParamJobReference
================
*/
export interface ParamJobReference {
	readonly durationSec: number;
	readonly icon?: string;
	readonly name?: string;
}

export type ParamJobUpdate =
	| { readonly kind: "set"; readonly row: ParamJobRow; }
	| { readonly kind: "remove"; readonly itemRefObjId: number; };

/*
================
paramJobReference

A param item's board reference: Param1 is its duration in seconds.
================
*/
export function paramJobReference( row: {
	readonly typeFlags: number;
	readonly nativeFields?: { readonly itemParam1_29c?: number; };
	readonly icon?: string;
	readonly name?: string;
} ): ParamJobReference | null {
	if ( (row.typeFlags & TYPE_MASK) !== PARAM_ITEM_TYPE ) return null;
	const duration = row.nativeFields?.itemParam1_29c;
	if ( duration === undefined ) return null;
	if ( !Number.isInteger( duration ) || duration <= 0 || duration > 0xffffffff ) {
		throw Error( "Invalid param job duration" );
	}
	return { durationSec: duration, ...(row.icon ? { icon: row.icon } : {}), ...(row.name ? { name: row.name } : {}) };
}

/*
================
paramJobPacket
================
*/
export function paramJobPacket( frame: WireFrame, nowMs: number ): ParamJobUpdate | null {
	const op = frame.opcode, p = frame.payload;
	if ( op !== OP_PARAM_JOB_START && op !== OP_PARAM_JOB_RESUME && op !== OP_PARAM_JOB_END ) return null;
	const v = new DataView( p.buffer, p.byteOffset, p.byteLength );
	if ( op === OP_PARAM_JOB_END ) {
		if ( p.length !== 8 ) throw Error( "Invalid param job end" );
		const itemRefObjId = v.getUint32( 4, true );
		if ( !itemRefObjId ) throw Error( "Invalid param job reference" );
		return { kind: "remove", itemRefObjId };
	}
	if ( p.length !== 12 ) throw Error( "Invalid param job row" );
	const itemRefObjId = v.getUint32( 8, true );
	if ( !itemRefObjId ) throw Error( "Invalid param job reference" );
	return { kind: "set", row: { itemRefObjId, remainingSec: v.getUint32( 4, true ), receivedAtMs: nowMs } };
}

/*
================
paramJobFraction

The bar left: remaining against the item's duration, clamped to [0, 1].
================
*/
export function paramJobFraction( row: ParamJobRow, reference: ParamJobReference, nowMs: number ): number {
	const left = row.remainingSec * 1000 - Math.max( 0, nowMs - row.receivedAtMs );
	return Math.min( 1, Math.max( 0, left / (reference.durationSec * 1000) ) );
}

// 6E6150 keys rows by kind and id; the server bounds live jobs the same way.
const PARAM_JOB_CAPACITY = 8;

/*
================
createParamJobs

The owner of the local player's param job rows and the references that
give them a duration and an icon.
================
*/
export function createParamJobs() {
	let rows: readonly ParamJobRow[] = [];
	const references = new Map<number, ParamJobReference>();
	return {
		/*
================
reference

Admit one item reference row (login snapshot or opcode 14).
================
		*/
		reference( row: Parameters<typeof paramJobReference>[0] & { readonly refObjId: number; } ) {
			const reference = paramJobReference( row );
			if ( reference ) references.set( row.refObjId, reference );
		},
		/*
================
receive

True when the frame was a param job row.
================
		*/
		receive( frame: WireFrame, nowMs: number ): boolean {
			const update = paramJobPacket( frame, nowMs );
			if ( !update ) return false;
			const id = update.kind === "remove" ? update.itemRefObjId : update.row.itemRefObjId;
			const kept = rows.filter( row => row.itemRefObjId !== id );
			if ( update.kind === "remove" ) {
				rows = kept;
				return true;
			}
			if ( kept.length >= PARAM_JOB_CAPACITY ) throw Error( "Param job capacity" );
			rows = [ ...kept, update.row ];
			return true;
		},
		/*
================
clear

World leave and the board reset empty the rows; references persist.
================
		*/
		clear() {
			rows = [];
		},
		/*
================
reset
================
		*/
		reset() {
			rows = [];
			references.clear();
		},
		/*
================
state

Rows whose item reference is known, ready for the board.
================
		*/
		state() {
			return rows.flatMap( row => {
				const reference = references.get( row.itemRefObjId );
				return reference ? [ { ...row, reference } ] : [];
			} );
		}
	};
}
