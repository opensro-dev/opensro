/*
===========================================================================

attendance.ts - the attendance calendar codec (port-only 0x3FD2 / 0x7FD2, not native)

#3255: with SRO_ATTENDANCE on, the server sends one snapshot of the
character's attendance month (0x3FD2) on first sight and whenever it
changes; the calendar window's check-in and reward claims are the
request 0x7FD2. A client that predates the frames drops 0x3FD2 and never
sends 0x7FD2.

Snapshot body, little-endian (see the server wire,
internal/game/item/wire/attendance.go): u32 monthKey (YYYYMM), u8 today
(1..31), u8 daysInMonth, u32 checked (bit d-1 for day d), u8
continuityClaimed, u8 accumulationClaimed (bit i for tier i), u8
continuityTiers then that many (u8 days, u16 rewardLen, reward ASCII),
u8 accumulationTiers then the same, u64 nextResetMs, u64 serverNowMs.

Request body: empty or u8 0 checks in today; u8 1, u8 tier claims a
continuity tier; u8 2, u8 tier claims an accumulation tier.

===========================================================================
*/
import type { AttendanceFeed, AttendanceTier } from "@/engine/contracts/attendance";
import type { WireFrame } from "@/engine/contracts/network";
import { viewOf } from "@/engine/foundation/binary/byte-view";

export const OP_ATTENDANCE_SNAPSHOT = 0x3fd2;
export const OP_ATTENDANCE_REQUEST = 0x7fd2;
// The 0x7FD2 operation bytes.
export const ATTENDANCE_OP_CHECK_IN = 0;
export const ATTENDANCE_OP_CLAIM_CONTINUITY = 1;
export const ATTENDANCE_OP_CLAIM_ACCUMULATION = 2;
// A claim mask is one byte: eight tiers at most.
export const ATTENDANCE_MAX_TIERS = 8;

// u32 monthKey, u8 today, u8 daysInMonth, u32 checked, two u8 claim masks.
const HEAD_BYTES = 12;
const CLOCK_BYTES = 16;

export type AttendanceSnapshotFields = Omit<AttendanceFeed, "revision">;
export type AttendanceColumn = "continuity" | "accumulation";

/*
================
readTiers

One tier list at `at`; returns it and the offset past it. A short list
is a protocol error, never a partial calendar.
================
*/
function readTiers( p: Uint8Array, at: number ): { tiers: AttendanceTier[]; next: number; } {
	if ( at + 1 > p.length ) throw new Error( "Truncated attendance snapshot" );
	const count = p[at]!;
	if ( count > ATTENDANCE_MAX_TIERS ) throw new Error( "Invalid attendance tier count" );
	const v = viewOf( p );
	const tiers: AttendanceTier[] = [];
	at += 1;
	for ( let i = 0; i < count; i++ ) {
		if ( at + 3 > p.length ) throw new Error( "Truncated attendance snapshot" );
		const days = p[at]!;
		const length = v.getUint16( at + 1, true );
		at += 3;
		if ( at + length > p.length ) throw new Error( "Truncated attendance snapshot" );
		let reward: string;
		try {
			reward = new TextDecoder( "utf-8", { fatal: true } ).decode( p.subarray( at, at + length ) );
		} catch {
			throw new Error( "Invalid attendance reward" );
		}
		tiers.push( { days, reward } );
		at += length;
	}
	return { tiers, next: at };
}

/*
================
decodeAttendanceSnapshot

The feed fields of a 0x3FD2 body. A truncated, oversized or malformed
body throws.
================
*/
export function decodeAttendanceSnapshot( payload: Uint8Array ): AttendanceSnapshotFields {
	if ( payload.length < HEAD_BYTES + 2 + CLOCK_BYTES ) throw new Error( "Truncated attendance snapshot" );
	const v = viewOf( payload );
	const monthKey = v.getUint32( 0, true );
	const today = payload[4]!;
	const daysInMonth = payload[5]!;
	if ( today < 1 || today > 31 || daysInMonth < 1 || daysInMonth > 31 ) {
		throw new Error( "Invalid attendance month" );
	}
	const continuity = readTiers( payload, HEAD_BYTES );
	const accumulation = readTiers( payload, continuity.next );
	if ( accumulation.next + CLOCK_BYTES !== payload.length ) throw new Error( "Invalid attendance snapshot" );
	return {
		monthKey,
		today,
		daysInMonth,
		checked: v.getUint32( 6, true ),
		continuityClaimed: payload[10]!,
		accumulationClaimed: payload[11]!,
		continuityTiers: continuity.tiers,
		accumulationTiers: accumulation.tiers,
		nextResetMs: Number( v.getBigUint64( accumulation.next, true ) ),
		serverNowMs: Number( v.getBigUint64( accumulation.next + 8, true ) )
	};
}

/*
================
encodeAttendanceCheckIn

Check in today.
================
*/
export function encodeAttendanceCheckIn(): Uint8Array {
	return Uint8Array.of( ATTENDANCE_OP_CHECK_IN );
}

function tierRequest( op: number, tierIndex: number ): Uint8Array {
	if ( !Number.isInteger( tierIndex ) || tierIndex < 0 || tierIndex >= ATTENDANCE_MAX_TIERS ) {
		throw new Error( "Invalid attendance tier" );
	}
	return Uint8Array.of( op, tierIndex );
}

/*
================
encodeAttendanceClaimContinuity
================
*/
export function encodeAttendanceClaimContinuity( tierIndex: number ): Uint8Array {
	return tierRequest( ATTENDANCE_OP_CLAIM_CONTINUITY, tierIndex );
}

/*
================
encodeAttendanceClaimAccumulation
================
*/
export function encodeAttendanceClaimAccumulation( tierIndex: number ): Uint8Array {
	return tierRequest( ATTENDANCE_OP_CLAIM_ACCUMULATION, tierIndex );
}

/*
================
dayChecked

Whether day (1..31) is checked in the bit set.
================
*/
export function dayChecked( checked: number, day: number ): boolean {
	if ( !Number.isInteger( day ) || day < 1 || day > 31 ) return false;
	return ((checked >>> (day - 1)) & 1) === 1;
}

/*
================
tierClaimed

Whether tier `index` is received in the claim mask.
================
*/
export function tierClaimed( mask: number, index: number ): boolean {
	if ( !Number.isInteger( index ) || index < 0 || index >= ATTENDANCE_MAX_TIERS ) return false;
	return ((mask >>> index) & 1) === 1;
}

/*
================
accumulationReached

The checked days of the month: what the accumulation tiers count. The
server decides the claim; this only tells the window which Receive to
offer.
================
*/
export function accumulationReached( checked: number ): number {
	let n = checked >>> 0, count = 0;
	while ( n ) {
		count += n & 1;
		n >>>= 1;
	}
	return count;
}

/*
================
continuityReached

The longest run of consecutive checked days in the month: what the
continuity tiers count (the server's rule, platform/attendance).
================
*/
export function continuityReached( checked: number ): number {
	let best = 0, run = 0;
	for ( let day = 1; day <= 31; day++ ) {
		run = dayChecked( checked, day ) ? run + 1 : 0;
		if ( run > best ) best = run;
	}
	return best;
}

/*
================
tierEarned

Whether the month's checked days reach tier `index` of the column. The
window's hint for the Receive buttons; the server's word is the claim.
================
*/
export function tierEarned( feed: AttendanceFeed, column: AttendanceColumn, index: number ): boolean {
	const tier = (column === "continuity" ? feed.continuityTiers : feed.accumulationTiers)[index];
	if ( !tier ) return false;
	return (column === "continuity" ? continuityReached( feed.checked ) : accumulationReached( feed.checked )) >=
		tier.days;
}
