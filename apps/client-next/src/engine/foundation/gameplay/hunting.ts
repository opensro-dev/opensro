/*
===========================================================================

hunting.ts - hunting points: the detection effect and the tracked pose

A detection skill (B5ED) can mark its owner as a hunting point that the
minimap and the world map draw. The point keeps the last tracked movement
(0x30E3); while its owner is in view, the live entity is the newer pose.

===========================================================================
*/
import type { AttachedEffectReference } from "./attached-effects";
import type { EntityState } from "@/engine/contracts/world";

export interface HuntingPoint {
	readonly gid: number;
	readonly token: number;
	readonly name: string;
	readonly regionId: number;
	readonly x: number;
	readonly y: number;
	readonly z: number;
	readonly angle: number;
}

export type HuntingPose = Pick<HuntingPoint, "regionId" | "x" | "y" | "z" | "angle">;

const MAX_DETECTION_NAME = 4096;
const TRACKED_MOVEMENT_LENGTH = 20;

/*
================
detectionEffect

776600: three dwords, byte-length string, config-gated duration. The name
belongs to the effect; it is not a second entity-spawn message.
================
*/
export function detectionEffect( p: Uint8Array, refs: ReadonlyMap<number, AttachedEffectReference> ) {
	if ( p.length < 14 ) throw Error( "Truncated detection effect" );
	const v = new DataView( p.buffer, p.byteOffset, p.byteLength ),
		skill = v.getUint32( 0, true ),
		token = v.getUint32( 4, true ),
		owner = v.getUint32( 8, true ),
		n = v.getUint16( 12, true ),
		ref = refs.get( skill );
	if ( !ref || ref.huntingPoint === undefined || ref.stealthDuration === undefined ) {
		throw Error( "Missing detection effect authority" );
	}
	if ( n > MAX_DETECTION_NAME || p.length !== 14 + n + (ref.stealthDuration ? 4 : 0) ) {
		throw Error( "Invalid detection effect length" );
	}
	return {
		skill,
		token,
		owner,
		name: new TextDecoder( "windows-1252" ).decode( p.subarray( 14, 14 + n ) ),
		duration: ref.stealthDuration ? v.getUint32( 14 + n, true ) : 0,
		track: ref.huntingPoint
	};
}

/*
================
huntingMovement

A tracked movement (0x30E3) moves its owner's hunting point.
================
*/
export function huntingMovement( points: readonly HuntingPoint[], p: Uint8Array ): readonly HuntingPoint[] {
	if ( p.length !== TRACKED_MOVEMENT_LENGTH ) throw Error( "Invalid tracked movement" );
	const v = new DataView( p.buffer, p.byteOffset, p.byteLength ),
		gid = v.getUint32( 16, true ),
		regionId = v.getUint16( 0, true ),
		x = v.getFloat32( 2, true ),
		y = v.getFloat32( 6, true ),
		z = v.getFloat32( 10, true ),
		angle = v.getUint16( 14, true );
	if ( ![ x, y, z ].every( Number.isFinite ) ) throw Error( "Invalid tracked coordinates" );
	return points.map( row => row.gid === gid ? { ...row, regionId, x, y, z, angle } : row );
}

/*
================
huntingPose

Where a hunting point's marker stands and faces. Native movement (#3721)
sends one B738 per move and no per-tick 0x30E3, so a hunted peer in view
walks and turns on its entity while the point keeps its last sample: the
live entity's pose and heading win, the point stands in out of view.
================
*/
export function huntingPose( point: HuntingPoint, entities: readonly EntityState[] ): HuntingPose {
	const live = entities.find( e => e.gid === point.gid );
	if ( !live ) return point;
	return { regionId: live.regionId, x: live.x, y: live.y, z: live.z, angle: live.heading };
}
