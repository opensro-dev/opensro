/*
===========================================================================

motion.ts - Remote movement owns sampled paths and authoritative progress; only settlement ends travel.

===========================================================================
*/
import type { SurfaceCursor, SurfaceResolver } from "@/engine/contracts/navigation";
import {
	type MovementSegment,
	movementHeading,
	decodeNativeMovement,
	poseDistance,
	movementGait,
	sampleMovement as sample,
	movementModeTransition,
	movementSpeedTransition
} from "@/engine/foundation/gameplay/native-movement";
import type { EntityState } from "@/engine/contracts/world";
import type { Pose } from "@/engine/contracts/gameplay";
import { displacementSegment } from "@/engine/foundation/gameplay/cast-displacement";
// Motion owns sampled pose and path activity. Entity metadata remains owned by
// createEntities; presentation must not infer path completion from packet gaps.
/*
================
MotionUpdate
================
*/
type MotionUpdate = Pick<EntityState, "gid" | "regionId" | "x" | "y" | "z" | "heading" | "moving" | "movementPath">;
/*
================
duration
================
*/
function duration( from: Pose, to: Pose, entity: EntityState ): number {
	const distance = poseDistance( from, to ),
		speed = (movementGait( entity.movementMode ) === "walk" ? entity.walkSpeed : entity.runSpeed) ?? 0;
	if ( (!Number.isFinite( speed ) || speed <= 0) && distance > 0 ) throw new Error( "Moving entity has no speed" );
	return distance ? distance / speed * 1000 : 0;
}
/*
================
update
================
*/
function update( gid: number, pose: Pose, moving = false ): MotionUpdate {
	return { gid, moving, regionId: pose.regionId, x: pose.x, y: pose.y, z: pose.z, heading: pose.angle };
}
/*
================
createEntityMotion
================
*/
export function createEntityMotion( surface: SurfaceResolver = pose => pose ) {
	const cursors = new Map<number, SurfaceCursor>();
	/*
================
resolve
================
	*/
	function resolve( gid: number, pose: Pose, reference: Pose ) {
		let cursor = cursors.get( gid );
		if ( !cursor ) {
			cursor = {};
			cursors.set( gid, cursor );
		}
		return surface( pose, reference, cursor );
	}
	const active = new Map<
		number,
		(MovementSegment & { previous?: Pose; castToken?: number; fixedTiming?: boolean; })
	>();
	return {
		/*
================
source

0x30E3 publishes progress while travel remains active. Only the separate
settlement/correction packet clears it; a position sample is not a stop.
Retail 0x775CB0 calls source reseed (0x86D9D0), without the halt
(0x86C9C0) used by 0x775B50. An idle source update stays idle.
================
		*/
		source( entity: EntityState, pose: Pose, now: number ): MotionUpdate {
			const segment = active.get( entity.gid );
			const reference = segment?.previous ??
				{ regionId: entity.regionId, x: entity.x, y: entity.y, z: entity.z, angle: entity.heading };
			const resolved = resolve( entity.gid, pose, reference );
			if ( segment && !segment.fixedTiming ) {
				active.set( entity.gid, {
					...segment,
					from: resolved,
					previous: resolved,
					start: now,
					duration: duration( resolved, segment.to, entity )
				} );
			}
			return {
				...update( entity.gid, resolved, !!segment || !!entity.moving ),
				movementPath: segment ? { from: resolved, to: segment.to } : entity.movementPath
			};
		},
		/*
================
correct
================
		*/
		correct( entity: EntityState, pose: Pose ): MotionUpdate {
			const reference = active.get( entity.gid )?.previous ??
				{ regionId: entity.regionId, x: entity.x, y: entity.y, z: entity.z, angle: entity.heading };
			const resolved = resolve( entity.gid, pose, reference );
			active.delete( entity.gid );
			return update( entity.gid, resolved );
		},
		/*
================
displace
================
		*/
		displace( entity: EntityState, command: import("@/engine/contracts/gameplay").CastDisplacement, now: number ) {
			const previous = active.get( entity.gid ),
				from = previous ?
					sample( previous, now ) :
					{ regionId: entity.regionId, x: entity.x, y: entity.y, z: entity.z, angle: entity.heading };
			const segment = displacementSegment( from, command, now );
			active.set( entity.gid, {
				...segment,
				fixedTiming: true,
				castToken: command.kind === 8 ? command.token : undefined
			} );
			return update( entity.gid, resolve( entity.gid, sample( segment, now ), from ), segment.duration > 0 );
		},
		/*
================
castArrival
================
		*/
		castArrival( token: number ) {
			for ( const segment of active.values() ) {
				if ( segment.castToken === token ) return segment.start + segment.duration;
			}
			return undefined;
		},
		/*
================
cancelCast
================
		*/
		cancelCast( token: number, now: number ) {
			const updates: MotionUpdate[] = [];
			for ( const [gid, segment] of active ) {
				if ( segment.castToken === token ) {
					updates.push(
						update( gid, resolve( gid, sample( segment, now ), segment.previous ?? segment.from ) )
					);
					active.delete( gid );
				}
			}
			return updates;
		},
		// Navigation admission refreshes the surface reference, not the path
		// clock, destination or cast/displacement ownership.
		/*
================
surfaceReference
================
		*/
		surfaceReference( entity: EntityState ) {
			const segment = active.get( entity.gid );
			if ( segment ) {
				segment.previous = {
					regionId: entity.regionId,
					x: entity.x,
					y: entity.y,
					z: entity.z,
					angle: entity.heading
				};
			}
			cursors.delete( entity.gid );
		},
		/*
================
spawn
================
		*/
		spawn( entity: EntityState, now: number ) {
			if ( !entity.spawnDestination ) return;
			const from = { regionId: entity.regionId, x: entity.x, y: entity.y, z: entity.z, angle: entity.heading },
				to = entity.spawnDestination;
			active.set( entity.gid, {
				from,
				to: { ...to, angle: movementHeading( from, to ) },
				start: now,
				duration: duration( from, to, entity )
			} );
		},
		/*
================
receive
================
		*/
		receive( p: Uint8Array, entity: EntityState, now: number ) {
			const previous = active.get( entity.gid ),
				published = { regionId: entity.regionId, x: entity.x, y: entity.y, z: entity.z, angle: entity.heading };
			// Reception precedes this tick's motion step. Re-aim from the path
			// at reception time, not the previous journal sample: otherwise
			// every source-less chase refresh discards one tick of travel.
			const current = previous ? sample( previous, now ) : published;
			const decoded = decodeNativeMovement( p, current ),
				from = resolve( entity.gid, decoded.from, previous?.previous ?? published ),
				to = decoded.to;
			active.set( entity.gid, { from, to, start: now, duration: duration( from, to, entity ) } );
			return { from, to };
		},
		/*
================
speeds
================
		*/
		speeds( previous: EntityState, next: EntityState, now: number ): MotionUpdate | null {
			const segment = active.get( next.gid );
			if ( !segment || segment.fixedTiming ) return null;
			const walk = movementGait( next.movementMode ) === "walk",
				before = (walk ? previous.walkSpeed : previous.runSpeed) ?? 0,
				after = (walk ? next.walkSpeed : next.runSpeed) ?? 0;
			if ( before === after ) return null;
			const retimed = movementSpeedTransition( segment, before, after, now ),
				pose = resolve( next.gid, retimed.from, segment.previous ?? segment.from );
			if ( retimed.duration ) active.set( next.gid, { ...segment, ...retimed, from: pose, previous: pose } );
			else active.delete( next.gid );
			return update( next.gid, pose, retimed.duration > 0 );
		},
		/*
================
stopForDeath
================
		*/
		stopForDeath( entity: EntityState, now: number ): MotionUpdate {
			const segment = active.get( entity.gid ),
				reference = { regionId: entity.regionId, x: entity.x, y: entity.y, z: entity.z, angle: entity.heading };
			const pose = segment ?
				resolve( entity.gid, sample( segment, now ), segment.previous ?? segment.from ) :
				reference;
			// 8D57E2..8D57F1: death keeps action bits 4/5 (forced impact
			// displacement), but stops ordinary navigation and cast-owned rush.
			const displaced = !!segment?.fixedTiming && segment.castToken === undefined;
			if ( !displaced ) {
				active.delete( entity.gid );
				cursors.delete( entity.gid );
			}
			return update( entity.gid, pose, displaced && now < segment!.start + segment!.duration );
		},
		/*
================
mode
================
		*/
		mode( entity: EntityState, now: number ): MotionUpdate | null {
			const segment = active.get( entity.gid );
			if ( !segment ) return null;
			const speed = (movementGait( entity.movementMode ) === "walk" ? entity.walkSpeed : entity.runSpeed) ?? 0;
			const next = movementModeTransition( segment, entity.movementMode ?? 3, speed, now );
			const pose = resolve( entity.gid, next.pose, segment.previous ?? segment.from );
			if ( next.segment ) active.set( entity.gid, { ...next.segment, from: pose, previous: pose } );
			else active.delete( entity.gid );
			return update( entity.gid, pose, !!next.segment );
		},
		/*
================
step
================
		*/
		step( now: number ): MotionUpdate[] {
			const changed: MotionUpdate[] = [];
			for ( const [gid, segment] of active ) {
				const pose = resolve( gid, sample( segment, now ), segment.previous ?? segment.from );
				segment.previous = pose;
				changed.push( {
					...update( gid, pose, now < segment.start + segment.duration ),
					movementPath: { from: segment.from, to: segment.to }
				} );
				if ( now >= segment.start + segment.duration ) active.delete( gid );
			}
			return changed;
		},
		/*
================
remove
================
		*/
		remove( gid: number ) {
			active.delete( gid );
			cursors.delete( gid );
		},
		/*
================
clear
================
		*/
		clear() {
			active.clear();
			cursors.clear();
		}
	};
}
