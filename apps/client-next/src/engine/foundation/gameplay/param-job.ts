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
import { remainedPeriodLine } from "./count-job";

export const OP_PARAM_JOB_START = 0x3602;
export const OP_PARAM_JOB_RESUME = 0x32af;
export const OP_PARAM_JOB_END = 0x36d4;

// TID 3/3/3/10 in the itemdata type word, the bits 0xFFFC keeps:
// (3 << 2) | (3 << 5) | (3 << 7) | (10 << 11).
const PARAM_ITEM_TYPE = 0x51ec;
// TID 3/3/13/4 (Gold/Silver Time tickets) and TID 3/3/13/5 (Skill Time tickets),
// carried on the 0x3602/0x32AF/0x36D4 ParamJob lane by the server
// (apps/server/internal/game/action/premiumticket.go, compositeitem.go).
const GOLD_TIME_ITEM_TYPE = 0x26ec;
const SKILL_TIME_ITEM_TYPE = 0x2eec;
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
	readonly description?: string;
}

// Port-only virtual rows emitted exclusively by the flagged survival arena.
// They reuse the native timed board protocol without creating inventory items.
const SURVIVAL_POWERS: Readonly<Record<number, ParamJobReference>> = {
	0x7f530001: {
		durationSec: 30,
		icon: "port/survival/damage-surge.ddj",
		name: "Damage Surge",
		description: "Random 2x–10x outgoing damage, rolled on pickup."
	},
	0x7f530003: {
		durationSec: 60,
		icon: "port/survival/unstoppable.ddj",
		name: "Unstoppable",
		description: "Immune to abnormal states and knockback."
	},
	0x7f530005: {
		durationSec: 30,
		icon: "port/survival/unkillable.ddj",
		name: "Unkillable",
		description: "Combat damage cannot take your last HP."
	},
	0x7f530006: {
		durationSec: 60,
		icon: "port/survival/arcane-spring.ddj",
		name: "Arcane Spring",
		description: "Skills consume no MP. Cooldowns still apply."
	},
	0x7f530007: {
		durationSec: 60,
		icon: "port/survival/fire-pit.ddj",
		name: "Fire Pit",
		description: "30m burning ground: non-uniques lose 3% maximum HP each second."
	},
	0x7f53000b: {
		durationSec: 30,
		icon: "port/survival/chain-reaction.ddj",
		name: "Chain Reaction",
		description: "Attacks and skills copy damage to up to 10 other enemies within 10m of the target."
	},
	0x7f53000c: {
		durationSec: 30,
		icon: "port/survival/fleetfoot.ddj",
		name: "Fleetfoot",
		description: "+200% movement speed."
	},
	0x7f530008: {
		durationSec: 30,
		icon: "port/survival/mirror-shell.ddj",
		name: "Mirror Shell",
		description: "Return 500% of HP damage taken to the attacker. You still take the hit."
	}
} as const;

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
	const type = row.typeFlags & TYPE_MASK;
	if ( type !== PARAM_ITEM_TYPE && type !== GOLD_TIME_ITEM_TYPE && type !== SKILL_TIME_ITEM_TYPE ) return null;
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

const MS_PER_SECOND = 1000;
const MS_PER_MINUTE = 60000;
const MS_PER_HOUR = 3600000;
// Above this many remaining seconds the kind-4 duration line counts whole
// hours; at or below it, whole minutes (0x6E398E-0x6E399D).
const DURATION_HOURS_ABOVE_SEC = 60000;

/*
================
paramJobRemainingMs

The milliseconds a param job has left: its remaining seconds counted
down from the receipt, the same left the kind-4 bar drains.
================
*/
export function paramJobRemainingMs( row: ParamJobRow, nowMs: number ): number {
	return row.remainingSec * MS_PER_SECOND - Math.max( 0, nowMs - row.receivedAtMs );
}

/*
================
paramJobFraction

The bar left: remaining against the item's duration, clamped to [0, 1].
================
*/
export function paramJobFraction( row: ParamJobRow, reference: ParamJobReference, nowMs: number ): number {
	const left = paramJobRemainingMs( row, nowMs );
	return Math.min( 1, Math.max( 0, left / (reference.durationSec * 1000) ) );
}

/*
================
paramJobHelpText

The kind-4 arm of the board help builder (6E2C90, kind byte 4 at
0x6E386E): the item's name, a blank line, the duration line, a blank
line and the remained period. The duration line ("%s: %d%s" at
0x6E39FB) reads PARAM_DURA with the slot's total duration in whole
hours (PARAM_HOUR) while more than 60000 s remain, else whole minutes
(PARAM_MINUTE at 0x6E3A88); the remained period is the line the timed
arms share. This is the premium service row: a Gold/Premium Time item
(3/3/13/4) rides this lane, so its hover help is these lines, not the
name alone. copy is the client text lookup.
================
*/
export function paramJobHelpText(
	name: string,
	durationSec: number,
	remainingMs: number,
	copy: ( key: string ) => string
): string {
	const durationMs = durationSec * MS_PER_SECOND;
	const duration = remainingMs > DURATION_HOURS_ABOVE_SEC * MS_PER_SECOND ?
		`${copy( "PARAM_DURA" )}: ${Math.trunc( durationMs / MS_PER_HOUR )}${copy( "PARAM_HOUR" )}` :
		`${copy( "PARAM_DURA" )}: ${Math.trunc( durationMs / MS_PER_MINUTE )}${copy( "PARAM_MINUTE" )}`;
	return [ name, " ", duration, " ", remainedPeriodLine( remainingMs, copy ) ].join( "\n" );
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
			if (
				!SURVIVAL_POWERS[id] &&
				kept.filter( row => !SURVIVAL_POWERS[row.itemRefObjId] ).length >= PARAM_JOB_CAPACITY
			) throw Error( "Param job capacity" );
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
				const reference = references.get( row.itemRefObjId ) ?? SURVIVAL_POWERS[row.itemRefObjId];
				return reference ? [ { ...row, reference } ] : [];
			} );
		}
	};
}
