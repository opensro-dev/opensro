/*
===========================================================================

progression.ts - the local player's level, experience, gold and points

The worker's progression plane: the char-data bootstrap seeds it, then the
stat acks (0xB27A/0xB552), the points channel (0x30B3 types 1-3: gold,
skill points, stat points; sub_779C70) and the mastery ack (0xB165) move
it. Experience deltas (0x30D2) belong to the level owner, not here.

===========================================================================
*/
import { playerStats, type PlayerStats } from "./player-stats";
import type { SystemNotice } from "./system-notices";

/*
================
Progression
================
*/
export interface Progression {
	readonly stats?: PlayerStats;
	readonly level?: number;
	readonly maxLevel?: number;
	readonly experience?: string;
	readonly skillExperience?: number;
	readonly gold?: string;
	readonly skillPoints?: number;
	readonly statPoints?: number;
	readonly masteries: readonly { id: number; level: number; }[];
	readonly error?: string;
}

/*
================
progressionPacket

The next plane for a progression frame, or null when the frame is not one.
================
*/
export function progressionPacket( state: Progression, opcode: number, p: Uint8Array ): Progression | null {
	if ( opcode === 0x343c ) return { ...state, stats: playerStats( p ) };
	// 75BA00/75BA50: only the success acknowledgement spends the point;
	// the subsequent 343C owns derived stats and leaves current HP/MP alone.
	if ( opcode === 0xb27a || opcode === 0xb552 ) {
		if ( p[0] === 1 && p.length === 1 ) {
			if ( state.statPoints === undefined || state.statPoints === 0 ) {
				throw Error( "Stat acknowledgement without available point" );
			}
			return { ...state, statPoints: state.statPoints - 1, error: undefined };
		}
		if ( p[0] === 2 && p.length === 2 ) return { ...state, error: "Stat allocation rejected: " + p[1] };
		throw Error( "Invalid stat allocation acknowledgement" );
	}
	const v = new DataView( p.buffer, p.byteOffset, p.byteLength );
	if ( opcode === 0x30b3 ) {
		// Type 1: [u8 1][u64 balance][u8 notify]; the notice is the caller's.
		if ( p[0] === 1 ) {
			if ( p.length !== 10 ) throw new Error( "Invalid gold update" );
			return { ...state, gold: v.getBigUint64( 1, true ).toString() };
		}
		if ( p[0] === 2 ) {
			if ( p.length !== 6 ) throw new Error( "Invalid skill points update" );
			return { ...state, skillPoints: v.getUint32( 1, true ) };
		}
		if ( p[0] === 3 ) {
			if ( p.length !== 3 ) throw new Error( "Invalid stat points update" );
			return { ...state, statPoints: v.getUint16( 1, true ) };
		}
		return null;
	}
	if ( opcode === 0xb165 ) {
		if ( p[0] === 2 && p.length === 2 ) return { ...state, error: undefined };
		if ( p[0] !== 1 || p.length !== 6 ) throw new Error( "Invalid mastery response" );
		const id = v.getUint32( 1, true );
		if ( !id ) throw new Error( "Invalid mastery ID" );
		return {
			...state,
			error: undefined,
			masteries: [ ...state.masteries.filter( row => row.id !== id ), { id, level: p[5]! } ]
		};
	}
	return null;
}

/*
================
skillPointNotice

CPSMission_OnPointUpdate30B3 type 2 (779DD9): with its notify byte set the
client compares the new value with the one it held. A loss prints
UIIT_MSG_JSERR_SINCE_YOU_DIE_IN_MURDERER_SP_DEPRIVED_BY_SERVER, anything
else UIIT_STT_SKILL_POINT_RECOVER_RESULT, to the system chat (type 0) and
the notice banner. The server sets the byte only for a death's skill-point
loss (progression applyDeathPenalty).
================
*/
export function skillPointNotice( held: number | undefined, p: Uint8Array ): SystemNotice | null {
	if ( p[0] !== 2 || p.length !== 6 || p[5] === 0 || held === undefined ) return null;
	const next = new DataView( p.buffer, p.byteOffset, p.byteLength ).getUint32( 1, true );
	const lost = held - next;
	return lost > 0 ?
		{
			key: "UIIT_MSG_JSERR_SINCE_YOU_DIE_IN_MURDERER_SP_DEPRIVED_BY_SERVER",
			value: lost,
			nativeType: 0,
			banner: true
		} :
		{ key: "UIIT_STT_SKILL_POINT_RECOVER_RESULT", value: next - held, nativeType: 0, banner: true };
}

/*
================
bootstrapProgression

The plane from the enter-world character snapshot; every scalar is range
checked because the snapshot crosses the worker boundary as JSON.
================
*/
export function bootstrapProgression( value: unknown ): Progression {
	const c = (value as {
		character?: {
			level?: number;
			maxLevel?: number;
			experience?: number | string;
			skillExp?: number;
			gold?: number | string;
			skillPoints?: number;
			statPoints?: number;
			masteries?: { id: number; level: number; }[];
		};
	}).character;
	function integer( n: number | undefined, max: number ) {
		if ( n !== undefined && (!Number.isSafeInteger( n ) || n < 0 || n > max) ) {
			throw new Error( "Invalid progression scalar" );
		}
		return n;
	}
	const raw = c?.gold;
	if (
		raw !== undefined &&
		((typeof raw !== "string" && typeof raw !== "number") ||
			(typeof raw === "number" && !Number.isSafeInteger( raw )) || !/^\d+$/.test( String( raw ) ) ||
			BigInt( raw ) > 0xffffffffffffffffn)
	) throw new Error( "Invalid progression gold" );
	const rows = c?.masteries ?? [];
	if ( !Array.isArray( rows ) || rows.length > 256 ) throw new Error( "Invalid masteries" );
	const seen = new Set<number>();
	const masteries = rows.map( row => {
		integer( row.id, 0xffffffff );
		integer( row.level, 255 );
		if ( !row.id || row.level === undefined || seen.has( row.id ) ) throw new Error( "Invalid mastery" );
		seen.add( row.id );
		return { ...row };
	} );
	const xp = c ? (c.experience ?? 0) : undefined;
	if (
		xp !== undefined &&
		((typeof xp !== "string" && typeof xp !== "number") ||
			(typeof xp === "number" && !Number.isSafeInteger( xp )) || !/^\d+$/.test( String( xp ) ) ||
			BigInt( xp ) > 0x7fffffffffffffffn)
	) throw Error( "Invalid progression experience" );
	return {
		level: integer( c?.level, 255 ),
		maxLevel: integer( c?.maxLevel ?? c?.level, 255 ),
		experience: xp === undefined ? undefined : String( xp ),
		skillExperience: integer( c ? (c.skillExp ?? 0) : undefined, 0xffffffff ),
		gold: raw === undefined ? undefined : String( raw ),
		skillPoints: integer( c ? (c.skillPoints ?? 0) : undefined, 0xffffffff ),
		statPoints: integer( c?.statPoints, 65535 ),
		masteries
	};
}
