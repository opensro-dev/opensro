/*
===========================================================================

count-job.ts - a premium package's limited uses and their chat commands

A Gold Time package's limited uses (UIL1: instant return, reverse return,
resurrection N times a day) live on CIFMagicStateBoard's item count map,
keyed by the limited item, and on a kind-5 slot keyed by the package
(CIFMagicStateBoard_SetSlotRemainingTime 6E6E00 draws its bar against the
package's Param1):

	0x3021 start   76F820  [u32 package][u32 remaining seconds][u32 item][u8 uses]
	0x36FC end     76F920  [u32 package][u32 item]
	0x76FD use     7024F0  [u32 package][u32 item] (+[u8 choice])
	0xB6FD answer  770820  [1][u32 package][u32 item] | [2][u8 code]

The chat commands /Return, /Reverse Return and /Resurrection (6AD990) look
for a row whose item matches the command and check, in order: any row at
all (0xCC), a use left (0xC6), the player alive (0x89; for /Resurrection
dead, 0x87), no transport out (0x5E) and not in PvP state 2 (0x75). Each
refusal is a category-1 notice the client raises itself.

INFERENCE: the native client compares the command's first word only, so
the two-word English "/Reverse Return" never matches; the port compares the
whole command.

===========================================================================
*/
import type { WireFrame } from "@/engine/contracts/network";

export const OP_COUNT_JOB_START = 0x3021;
export const OP_COUNT_JOB_END = 0x36fc;
export const OP_COUNT_JOB_USE = 0x76fd;
export const OP_COUNT_JOB_ANSWER = 0xb6fd;

// The category-1 refusals 6AD990 raises before sending.
export const COUNT_JOB_NONE = 0xcc;
export const COUNT_JOB_USED_UP = 0xc6;
export const COUNT_JOB_DEAD = 0x89;
export const COUNT_JOB_ALIVE = 0x87;
export const COUNT_JOB_TRANSPORT = 0x5e;
export const COUNT_JOB_PVP = 0x75;

// The reverse return's two points (0x7495 type 5 choices).
export const REVERSE_RETURN_LAST_RECALL = 2;
export const REVERSE_RETURN_LAST_DEATH = 3;

// Item type words (bits 0xFFFC) of the three limited items:
// (tid1 << 2) | (tid2 << 5) | (tid3 << 7) | (tid4 << 11).
const TYPE_MASK = 0xfffc;
const RETURN_SCROLL_TYPE = 0x09ec; // 3/3/3/1
const REVERSE_RETURN_TYPE = 0x19ec; // 3/3/3/3
const RESURRECTION_TYPE = 0x36ec; // 3/3/13/6

export type PremiumCommand = "return" | "reverse-return" | "resurrection";

/*
================
CountJobRow
================
*/
export interface CountJobRow {
	readonly packageRefObjId: number;
	readonly itemRefObjId: number;
	readonly uses: number;
	readonly remainingSec: number;
	readonly receivedAtMs: number;
}

export type CountJobUpdate =
	| { readonly kind: "set"; readonly row: CountJobRow; }
	| { readonly kind: "remove"; readonly packageRefObjId: number; readonly itemRefObjId: number; }
	| { readonly kind: "spent"; readonly packageRefObjId: number; readonly itemRefObjId: number; }
	| { readonly kind: "refused"; readonly code: number; };

/*
================
countJobPacket
================
*/
export function countJobPacket( frame: WireFrame, nowMs: number ): CountJobUpdate | null {
	const op = frame.opcode, p = frame.payload;
	if ( op !== OP_COUNT_JOB_START && op !== OP_COUNT_JOB_END && op !== OP_COUNT_JOB_ANSWER ) return null;
	const v = new DataView( p.buffer, p.byteOffset, p.byteLength );
	if ( op === OP_COUNT_JOB_START ) {
		if ( p.length !== 13 ) throw Error( "Invalid count job row" );
		return {
			kind: "set",
			row: {
				packageRefObjId: v.getUint32( 0, true ),
				remainingSec: v.getUint32( 4, true ),
				itemRefObjId: v.getUint32( 8, true ),
				uses: p[12]!,
				receivedAtMs: nowMs
			}
		};
	}
	if ( op === OP_COUNT_JOB_END ) {
		if ( p.length !== 8 ) throw Error( "Invalid count job end" );
		return { kind: "remove", packageRefObjId: v.getUint32( 0, true ), itemRefObjId: v.getUint32( 4, true ) };
	}
	if ( p[0] === 1 ) {
		if ( p.length !== 9 ) throw Error( "Invalid count job answer" );
		return { kind: "spent", packageRefObjId: v.getUint32( 1, true ), itemRefObjId: v.getUint32( 5, true ) };
	}
	if ( p[0] !== 2 || p.length !== 2 ) throw Error( "Invalid count job refusal" );
	return { kind: "refused", code: p[1]! };
}

/*
================
applyCountJob

The rows after one update; a spent use counts one down
(CIFMagicStateBoard_DecrementItemCountJob 6E6830).
================
*/
export function applyCountJob( rows: readonly CountJobRow[], update: CountJobUpdate ): readonly CountJobRow[] {
	switch ( update.kind ) {
		case "set":
			return [
				...rows.filter( r => r.itemRefObjId !== update.row.itemRefObjId ),
				update.row
			];
		case "remove":
			return rows.filter( r => r.itemRefObjId !== update.itemRefObjId );
		case "spent":
			return rows.map( r =>
				r.itemRefObjId === update.itemRefObjId && r.packageRefObjId === update.packageRefObjId ?
					{ ...r, uses: Math.max( 0, r.uses - 1 ) } :
					r
			);
	}
	return rows;
}

/*
================
premiumCommand

The command a chat line names, by the localized command words.
================
*/
export function premiumCommand( text: string, copy: ( symbol: string ) => string ): PremiumCommand | null {
	const line = text.trim().toLowerCase();
	if ( !line.startsWith( "/" ) ) return null;
	if ( line === copy( "UIIT_STT_PREMIUM_COMMAND_RETURN_HIGH_SPEED" ).trim().toLowerCase() ) return "return";
	if ( line === copy( "UIIT_STT_PREMIUM_COMMAND_REVERSE_RETURN" ).trim().toLowerCase() ) return "reverse-return";
	if ( line === copy( "UIIT_STT_PREMIUM_COMMAND_RESURRECTION_100%" ).trim().toLowerCase() ) return "resurrection";
	return null;
}

/*
================
commandItemType

The limited item type a command spends.
================
*/
function commandItemType( command: PremiumCommand ): number {
	switch ( command ) {
		case "return":
			return RETURN_SCROLL_TYPE;
		case "reverse-return":
			return REVERSE_RETURN_TYPE;
		case "resurrection":
			return RESURRECTION_TYPE;
	}
}

/*
================
CountJobFacts

What 6AD990 reads about the local player.
================
*/
export interface CountJobFacts {
	readonly alive: boolean;
	readonly transportOut: boolean;
	readonly pvpState: number;
}

/*
================
premiumCommandAdmission

6AD990's checks for one command: the row it spends, or the notice code it
raises. typeFlags resolves a limited item's type word.
================
*/
export function premiumCommandAdmission(
	command: PremiumCommand,
	rows: readonly CountJobRow[],
	typeFlags: ( itemRefObjId: number ) => number | undefined,
	facts: CountJobFacts
): { readonly row: CountJobRow; } | { readonly code: number; } {
	if ( !rows.length ) return { code: COUNT_JOB_NONE };
	const type = commandItemType( command );
	const row = rows.find( r => ((typeFlags( r.itemRefObjId ) ?? 0) & TYPE_MASK) === type );
	if ( !row ) return { code: COUNT_JOB_NONE };
	if ( row.uses === 0 ) return { code: COUNT_JOB_USED_UP };
	if ( command === "resurrection" ) {
		if ( facts.alive ) return { code: COUNT_JOB_ALIVE };
	} else if ( !facts.alive ) return { code: COUNT_JOB_DEAD };
	if ( facts.transportOut ) return { code: COUNT_JOB_TRANSPORT };
	if ( facts.pvpState === 2 ) return { code: COUNT_JOB_PVP };
	return { row };
}

/*
================
countJobUseRequest
================
*/
export function countJobUseRequest( row: CountJobRow, choice?: number ) {
	const payload = new Uint8Array( choice === undefined ? 8 : 9 ), v = new DataView( payload.buffer );
	v.setUint32( 0, row.packageRefObjId, true );
	v.setUint32( 4, row.itemRefObjId, true );
	if ( choice !== undefined ) payload[8] = choice;
	return { opcode: OP_COUNT_JOB_USE, payload };
}

/*
================
countJobFraction

The kind-5 bar: the row's remaining time against the package's period.
================
*/
export function countJobFraction( row: CountJobRow, periodSec: number, nowMs: number ): number {
	if ( periodSec <= 0 ) return 0;
	const left = row.remainingSec * 1000 - Math.max( 0, nowMs - row.receivedAtMs );
	return Math.min( 1, Math.max( 0, left / (periodSec * 1000) ) );
}

/*
================
CountJobReference

A package's or limited item's board reference: its type word, icon and
name, and for a package its period (Param1, seconds).
================
*/
export interface CountJobReference {
	readonly typeFlags: number;
	readonly periodSec: number;
	readonly icon?: string;
	readonly name?: string;
}

/*
================
createCountJobs

The owner of the local player's limited-use rows, the references that
draw them, and a reverse return waiting for its destination.
================
*/
export function createCountJobs() {
	let rows: readonly CountJobRow[] = [];
	let choosing: CountJobRow | null = null;
	const references = new Map<number, CountJobReference>();
	return {
		/*
		================
		reference

		Admit one item reference row (login snapshot or opcode 14).
		================
		*/
		reference( row: {
			readonly refObjId: number;
			readonly typeFlags: number;
			readonly nativeFields?: { readonly itemParam1_29c?: number; };
			readonly icon?: string;
			readonly name?: string;
		} ) {
			const period = row.nativeFields?.itemParam1_29c ?? 0;
			references.set( row.refObjId, {
				typeFlags: row.typeFlags,
				periodSec: Number.isInteger( period ) && period > 0 ? period : 0,
				...(row.icon ? { icon: row.icon } : {}),
				...(row.name ? { name: row.name } : {})
			} );
		},
		/*
		================
		receive

		The update a frame carried, applied; null when it was not a
		count-job frame.
		================
		*/
		receive( frame: WireFrame, nowMs: number ): CountJobUpdate | null {
			const update = countJobPacket( frame, nowMs );
			if ( update ) rows = applyCountJob( rows, update );
			return update;
		},
		/*
		================
		admit

		6AD990's checks for a command against the live rows.
		================
		*/
		admit( command: PremiumCommand, facts: CountJobFacts ) {
			return premiumCommandAdmission( command, rows, id => references.get( id )?.typeFlags, facts );
		},
		/*
		================
		choose

		A reverse return that passed its checks waits for its point (the
		confirm box 6AD990 opens, type 0x24); null clears it.
		================
		*/
		choose( row: CountJobRow | null ) {
			choosing = row;
		},
		choosing: () => choosing,
		/*
		================
		clear

		World leave empties the rows; references persist.
		================
		*/
		clear() {
			rows = [];
			choosing = null;
		},
		/*
		================
		reset
		================
		*/
		reset() {
			rows = [];
			choosing = null;
			references.clear();
		},
		/*
		================
		state

		Rows whose package reference is known, ready for the board.
		================
		*/
		state() {
			return rows.flatMap( row => {
				const reference = references.get( row.packageRefObjId );
				const item = references.get( row.itemRefObjId );
				return reference ? [ { ...row, reference, ...(item?.name ? { itemName: item.name } : {}) } ] : [];
			} );
		}
	};
}
