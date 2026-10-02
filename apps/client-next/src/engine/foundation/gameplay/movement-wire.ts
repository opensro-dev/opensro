/*
===========================================================================

movement-wire.ts - validation of the poses and receipts movement admits

Every pose and movement receipt that reaches the local movement owner
crosses here first. The checks are pure: a malformed value throws before any
owner state changes. Receipts are validated in two steps so a stale
duplicate is dropped by its id before its world is examined.

===========================================================================
*/
import type { Pose } from "@/engine/contracts/gameplay";

/*
================
MovementReceipt

The replacement client's movement acknowledgement envelope.
================
*/
export interface MovementReceipt {
	readonly id: number;
	readonly gid: number;
	readonly accepted: boolean;
	readonly serverTimeMs: number;
	readonly error?: string;
	readonly world: unknown;
}

/*
================
ReceiptWorld

Where the server put the player and, while it walks, the leg it walks.
================
*/
export interface ReceiptWorld {
	readonly spawn: Pose;
	readonly segment?: { readonly from: Pose; readonly startedAtMs: number; readonly arrivesAtMs: number; };
}

/*
================
admitPose

A wire pose inside the coordinate space its region admits: field regions
are 1920-unit cells, dungeon regions (bit 15) a signed 16-bit plane.
================
*/
export function admitPose( value: unknown ): Pose {
	const p = value as Pose;
	if (
		!p || !Number.isInteger( p.regionId ) || p.regionId <= 0 || p.regionId > 65535 ||
		![ p.x, p.y, p.z, p.angle ].every( Number.isFinite ) || (p.regionId & 0x8000 ?
			p.x < -32768 || p.x > 32767 || p.z < -32768 || p.z > 32767 :
			p.x < 0 || p.x >= 1920 || p.z < 0 || p.z >= 1920) ||
		p.y < -32768 || p.y > 32767 || p.angle < 0 || p.angle > 65535
	) {
		throw new Error( "Invalid movement pose" );
	}
	return Object.freeze( { ...p } );
}

/*
================
decodeMovementReceipt

The envelope only; its world is admitted by receiptWorld once the receipt
is known to be current.
================
*/
export function decodeMovementReceipt( payload: Uint8Array, expectedGid?: number ): MovementReceipt {
	const r = JSON.parse( new TextDecoder( "utf-8", { fatal: true } ).decode( payload ) ) as MovementReceipt & {
		v: number;
	};
	if (
		(expectedGid !== undefined && r.gid !== expectedGid) || r.v !== 1 || !Number.isInteger( r.id ) ||
		typeof r.accepted !== "boolean" || !Number.isFinite( r.serverTimeMs )
	) {
		throw new Error( "Invalid movement receipt" );
	}
	return r;
}

/*
================
receiptWorld
================
*/
export function receiptWorld( receipt: MovementReceipt ): ReceiptWorld {
	const world = receipt.world as {
		spawn?: unknown;
		moveSegment?: { from: unknown; startedAtMs: number; arrivesAtMs: number; };
	} | undefined;
	const spawn = admitPose( world?.spawn ), s = world?.moveSegment;
	if ( !s ) return { spawn };
	const from = admitPose( s.from );
	if ( !Number.isFinite( s.startedAtMs ) || !Number.isFinite( s.arrivesAtMs ) || s.arrivesAtMs <= s.startedAtMs ) {
		throw new Error( "Invalid authoritative movement clock" );
	}
	return { spawn, segment: { from, startedAtMs: s.startedAtMs, arrivesAtMs: s.arrivesAtMs } };
}
