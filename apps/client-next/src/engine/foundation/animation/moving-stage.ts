/*
===========================================================================

moving-stage.ts - skill projectile movement, fade envelopes, and basis vectors

Owns the native arithmetic for moving skill effect stages: target group
distribution (0x008DDDE0), fade alpha envelopes (0x008D3E60 / 0x008D41B0),
radial destination offsets (0x008D8E30), per-frame straight-line step
advancement across region boundaries (0x00879650), and projectile basis
orientation (0x008D9233..0x008D9304).

===========================================================================
*/

import type { CharacterActor } from "@/engine/contracts/character";
import { radians } from "@/engine/foundation/math/angles";
import { normalizeRegionXZ, regionOffsetXZ, worldDelta } from "@/engine/foundation/world/world-space";
import { projectileSpace } from "./projectile-time";

type Position = CharacterActor["pose"];

/*
================
movingTargets

8DDDE0: one instance receives the whole vector; multiple instances cycle
through its first group. Extra visual copies never acquire damage ownership.
================
*/
export function movingTargets( count: number, primary: number, targets: readonly number[] ) {
	if ( !Number.isInteger( count ) || count < 1 || count > 128 ) throw Error( "Invalid moving effect count" );
	if ( count === 1 ) return [ { target: primary, owns: true, all: true } ];
	return targets.length ?
		Array.from( { length: count }, ( _, i ) => ({
			target: targets[i % targets.length]!,
			owns: i < targets.length,
			all: false
		}) ) :
		[];
}

/*
================
movingFade

8D3E60 / 8D41B0: float progress, signed delta, truncating 8-bit alpha.
================
*/
export function movingFade( progress: number, delta: number, duration: number, ending: boolean ) {
	if ( duration <= 0 ) return { progress: 1, opacity: 1 };
	const f = Math.fround, next = Math.min( 1, f( progress + f( 1 / f( duration ) ) * f( delta ) ) );
	return { progress: next, opacity: Math.trunc( ending ? 255 - 255 * next : 255 * next ) / 255 };
}

/*
================
radialDestination

8D8E30: radial options are relative to the caller's original source pose,
not the effect's sampled bone offset. Authored angles are published in degrees.
================
*/
export function radialDestination( source: Position, degrees: number, distance: number ): Position {
	const f = Math.fround, angle = f( f( source.yaw ) + f( degrees / 360 * 6.283185482025146 ) );
	return {
		...source,
		yaw: radians( angle ),
		x: f( f( source.x ) + f( f( distance ) * f( Math.sin( angle ) ) ) ),
		y: f( source.y ),
		z: f( f( source.z ) + f( f( distance ) * -f( Math.cos( angle ) ) ) )
	};
}

/*
================
stepMoving

879650: reaching the endpoint (including equality) completes this step.
Explicit float stores preserve the native intermediate rounding.
================
*/
export function stepMoving( start: Position, end: Position, step: number ): { arrived: boolean; pose: Position; } {
	if ( !Number.isFinite( step ) || step < 0 ) throw Error( "Invalid moving effect step" );
	if ( !projectileSpace( start.regionId, end.regionId ) ) {
		throw Error( "Projectile requires linked dungeon coordinate projection" );
	}
	const f = Math.fround;
	const [ox, oz] = regionOffsetXZ( start.regionId, end.regionId );
	const dx = f( f( end.x - start.x ) + ox ),
		dy = f( end.y - start.y ),
		dz = f( f( end.z - start.z ) + oz );
	const squared = dx * dx + dy * dy + dz * dz;
	if ( step * step >= squared ) return { arrived: true, pose: { ...end, yaw: start.yaw } };
	if ( !squared ) return { arrived: false, pose: start };
	const inverse = f( 1 / f( Math.sqrt( f( squared ) ) ) ),
		x = f( start.x + f( f( dx * inverse ) * step ) ),
		y = f( start.y + f( f( dy * inverse ) * step ) ),
		z = f( start.z + f( f( dz * inverse ) * step ) );
	const next = normalizeRegionXZ( start.regionId, x, z );
	return {
		arrived: false,
		pose: {
			regionId: next.regionId,
			x: next.x,
			y,
			z: next.z,
			yaw: start.yaw
		}
	};
}

/*
================
projectileBasis

8D9233..8D9304: model basis points from destination back to source.
Keep this in retail model space; the renderer removes the GLB local Z flip.
================
*/
export function projectileBasis(
	start: Position,
	end: Position,
	verticalFallback = true
): CharacterActor["effectBasis"] {
	const f = Math.fround, [rawX, rawZ] = worldDelta( end, start );
	let x = f( rawX ), y = f( start.y - end.y ), z = f( rawZ );
	const normalize = ( x: number, y: number, z: number ) => {
		const length = f( Math.sqrt( f( x * x + y * y + z * z ) ) );
		if ( !length ) return [ x, y, z ] as const;
		const inverse = f( 1 / length );
		return [ f( x * inverse ), f( y * inverse ), f( z * inverse ) ] as const;
	};
	if ( !(x * x + y * y + z * z > 0) ) return undefined;
	[x, y, z] = normalize( x, y, z );
	if ( verticalFallback && (y >= .9999989867210388 || y <= -.9999989867210388) ) {
		x = 1;
		y = 0;
		z = 0;
	}
	const r = [ z, 0, f( -x ) ] as const,
		u = normalize( f( -x * y ), f( z * z + x * x ), f( -y * z ) ),
		right = normalize( ...r );
	return [ ...right, ...u, x, y, z ];
}
