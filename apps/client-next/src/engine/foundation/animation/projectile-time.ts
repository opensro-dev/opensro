/*
===========================================================================

projectile-time.ts - closed-form clock of a constant-speed straight mover

Pure algebra for the straight lane of CIDecoSkillEffectEntity_StepLinearTrajectory
(8D8580), with no runtime or global owner. The target is captured when the
mover is created; the regional 3D distance sets the travel fraction.

===========================================================================
*/
import type { CharacterActor } from "@/engine/contracts/character";
import { planeKindOf } from "@/engine/foundation/world/plane";
import { normalizeRegionXZ, worldDelta } from "@/engine/foundation/world/world-space";

type Position = CharacterActor["pose"];

// Units a homing step may fall short of its end and still arrive.
const ARRIVAL_EPSILON = 1e-6;

/*
================
ProjectileArc
================
*/
export interface ProjectileArc {
	readonly amplitudePermille: number;
	readonly rotationRadians: number;
}

/*
================
ProjectileSample
================
*/
export type ProjectileSample =
	| { readonly phase: "delay"; }
	| { readonly phase: "travel"; readonly pose: Position; }
	| { readonly phase: "arrived"; readonly at: number; readonly pose: Position; };

/*
================
projectileSpace

Two regions share one coordinate projection unless a dungeon is involved.
================
*/
export function projectileSpace( start: number, end: number ): boolean {
	return planeKindOf( start ) === "outdoor" && planeKindOf( end ) === "outdoor" || start === end;
}

/*
================
sampleProjectile

8D8580 -> Navigation_StepTowards (879650): each tick moves speed * dt toward
the captured end and arrives once the remaining distance squared is at most
the step squared. A mover authored with speed 0 therefore arrives on its first
tick when its start is its end (skill 216's flame), and never moves otherwise
(skill 198's curse): it stays at its start until its visual ends.
================
*/
export function sampleProjectile(
	start: Position,
	end: Position,
	speed: number,
	delay: number,
	elapsed: number,
	arc?: ProjectileArc
): ProjectileSample {
	if (
		!Number.isFinite( speed ) || speed < 0 || !Number.isFinite( delay ) || delay < 0 || !Number.isFinite( elapsed )
	) {
		throw new Error( "Invalid projectile clock" );
	}
	if ( !projectileSpace( start.regionId, end.regionId ) ) {
		throw new Error( "Projectile requires linked dungeon coordinate projection" );
	}
	if ( arc && (!Number.isFinite( arc.amplitudePermille ) || !Number.isFinite( arc.rotationRadians )) ) {
		throw new Error( "Invalid projectile arc" );
	}
	if ( elapsed < delay ) return { phase: "delay" };
	// SWorld vtable +0x1c -> 888140 returns zero sector displacement in dungeons.
	const [dx, dz] = worldDelta( start, end );
	const distance = Math.hypot( dx, end.y - start.y, dz );
	if ( !distance ) return { phase: "arrived", at: delay, pose: { ...end } };
	if ( !speed ) return { phase: "travel", pose: start };
	const travel = distance / speed;
	if ( elapsed - delay >= travel ) return { phase: "arrived", at: delay + travel, pose: { ...end } };
	const fraction = (elapsed - delay) / travel;
	// 8d8580 computes the sine envelope; 8d5f20 applies a separate render
	// offset. It must never lengthen travel or alter the captured destination.
	const height = arc ?
		Math.fround(
			Math.fround( Math.sin( Math.fround( Math.fround( fraction ) * 3.1415927410125732 ) ) ) *
				Math.fround( distance ) * arc.amplitudePermille / 1000
		) :
		0;
	const sideways = arc ? Math.fround( -Math.fround( Math.sin( arc.rotationRadians ) ) * height ) : 0;
	const next = normalizeRegionXZ(
		start.regionId,
		start.x + dx * fraction + Math.fround( Math.cos( start.yaw ) ) * sideways,
		start.z + dz * fraction + Math.fround( Math.sin( start.yaw ) ) * sideways
	);
	const lift = arc ? Math.fround( Math.fround( Math.cos( arc.rotationRadians ) ) * height ) : 0;
	return {
		phase: "travel",
		pose: {
			regionId: next.regionId,
			x: next.x,
			y: start.y + (end.y - start.y) * fraction + lift,
			z: next.z,
			yaw: start.yaw
		}
	};
}

/*
================
HomingProjectile

A straight mover that follows its target: where it is now (region-local,
like any pose) and the scene time it was last stepped.
================
*/
export interface HomingProjectile {
	position: Position;
	previous: number;
}

/*
================
stepHomingProjectile

A deliberate deviation from 8D8580, which steps toward the end captured at
launch: a monster that moves after the cast left the native shot landing
where the monster had been, flying through or past the body. Here each step
moves speed * dt toward the target's live end, as Navigation_StepTowards
does toward its fixed one, and arrives once the remaining distance is at
most the step: the shot always lands on the body, and exactly when it
reaches it. now and the returned arrival time are scene seconds.
================
*/
export function stepHomingProjectile(
	state: HomingProjectile,
	end: Position,
	speed: number,
	now: number
): ProjectileSample {
	if ( !Number.isFinite( speed ) || speed < 0 || !Number.isFinite( now ) ) {
		throw new Error( "Invalid projectile clock" );
	}
	const start = state.position;
	if ( !projectileSpace( start.regionId, end.regionId ) ) {
		throw new Error( "Projectile requires linked dungeon coordinate projection" );
	}
	const dt = Math.max( 0, now - state.previous );
	state.previous = now;
	const [dx, dz] = worldDelta( start, end );
	const dy = end.y - start.y;
	const distance = Math.hypot( dx, dy, dz ), step = speed * dt;
	// A step that ends exactly on the target arrives despite float rounding.
	if ( distance <= step + ARRIVAL_EPSILON ) {
		state.position = { ...end };
		return { phase: "arrived", at: speed ? now - (step - distance) / speed : now, pose: { ...end } };
	}
	const k = step / distance;
	const next = normalizeRegionXZ( start.regionId, start.x + dx * k, start.z + dz * k );
	state.position = {
		regionId: next.regionId,
		x: next.x,
		y: start.y + dy * k,
		z: next.z,
		yaw: start.yaw
	};
	return { phase: "travel", pose: state.position };
}
