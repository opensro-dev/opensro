/*
===========================================================================

motion.ts - Remote movement owns sampled paths and authoritative progress; only settlement ends travel.

A remote walking a direction (0xB738 mode 0 with a source) has no arrival:
its path is renewed leg by leg along the heading until a correction
(0xB2F5) or a new path replaces it. 0xB2CF turns it.

===========================================================================
*/
import type { SurfaceCursor, SurfaceResolver } from "@/engine/contracts/navigation";
import type { NavOwner } from "@/engine/foundation/navigation/dungeon-ownership";
import {
	type MovementSegment,
	REGION_SIZE,
	movementHeading,
	decodeNativeMovement,
	poseDistance,
	movementGait,
	sampleMovement as sample,
	movementModeTransition,
	movementSpeedTransition,
	movementDuration,
	clientWalkingStep,
	clientWalkingDirection,
	clientWalkingVector,
	clientPlanarDistance,
	interpolateMovement
} from "@/engine/foundation/gameplay/native-movement";
import type { EntityState } from "@/engine/contracts/world";
import type { Pose } from "@/engine/contracts/gameplay";
import { displacementSegment } from "@/engine/foundation/gameplay/cast-displacement";
import { directionLegEnd, modelYaw } from "@/engine/foundation/gameplay/direction-movement";
import {
	correctWalkingHistory,
	extendWalkingHistory,
	rewindWalkingHistory
} from "@/engine/foundation/gameplay/walking-history";
const ENDPOINT_EPSILON = 0.001;
const PRESENTATION_LOOKAHEAD_MS = 100;
/*
================
MotionClip
================
*/
export type MotionClip = ( from: Pose, to: Pose, query: {
	slide: boolean;
	sourceOwner?: NavOwner;
	owner?: NavOwner;
	status?: number;
} ) => Pose | null;
/*
================
RemoteSegment
================
*/
type RemoteSegment = MovementSegment & {
	walkingPath?: readonly Pose[];
	presentationHistory?: { from: Pose; to: Pose; points?: readonly Pose[]; result: readonly Pose[]; };
	previous?: Pose;
	at?: number;
	vector?: readonly [number, number];
	speed?: number;
	blocked?: boolean;
	arrived?: boolean;
	castToken?: number;
	fixedTiming?: boolean;
	direction?: number;
};
// Motion owns sampled pose and path activity. Entity metadata remains owned by
// createEntities; presentation must not infer path completion from packet gaps.
/*
================
MotionUpdate
================
*/
type MotionUpdate = Pick<
	EntityState,
	"gid" | "regionId" | "x" | "y" | "z" | "heading" | "moving" | "movementPath" | "poseAtMs"
>;
/*
================
duration
================
*/
function duration( from: Pose, to: Pose, entity: EntityState ): number {
	const speed = (movementGait( entity.movementMode ) === "walk" ? entity.walkSpeed : entity.runSpeed) ?? 0;
	return movementDuration( poseDistance( from, to ), speed );
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
export function createEntityMotion( surface: SurfaceResolver = pose => pose, clip: MotionClip = () => null ) {
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
	const active = new Map<number, RemoteSegment>();
	/*
================
candidate

The server's goal is intent. Compute only this elapsed step before querying
geometry; one elapsed candidate enters geometry, never the full destination chord.
================
	*/
	function candidate( segment: RemoteSegment, pose: Pose, elapsed: number ) {
		const destination = segment.to;
		const dx = destination.x - pose.x + ((destination.regionId & 255) - (pose.regionId & 255)) * REGION_SIZE;
		const dz = destination.z - pose.z + ((destination.regionId >>> 8) - (pose.regionId >>> 8)) * REGION_SIZE;
		const remaining = poseDistance( pose, segment.to );
		segment.vector ??= remaining ? clientWalkingDirection( [ dx, dz ] ) : [ 0, 0 ];
		const step = clientWalkingStep(
			segment.speed ?? 0,
			elapsed / 1000,
			segment.vector,
			segment.direction === undefined ? clientPlanarDistance( [ dx, dz ] ) : Infinity
		);
		const next = interpolateMovement( pose, {
			...pose,
			x: Math.fround( Math.fround( pose.x ) + step.step[0] ),
			y: pose.y,
			z: Math.fround( Math.fround( pose.z ) + step.step[1] ),
			angle: segment.to.angle
		}, 1 );
		return { pose: next, arrived: step.arrived };
	}
	/*
================
advance
================
	*/
	function advance( gid: number, segment: RemoteSegment, now: number ) {
		const previous = segment.previous ?? segment.from;
		if ( segment.fixedTiming ) return resolve( gid, sample( segment, now ), previous );
		const elapsed = Math.max( 0, now - (segment.at ?? segment.start) );
		if ( !elapsed || segment.blocked || segment.arrived ) return previous;
		const next = candidate( segment, previous, elapsed );
		const desired = next.pose;
		const query = {
			slide: false,
			sourceOwner: cursors.get( gid )?.owner,
			owner: undefined as NavOwner | undefined,
			status: 0
		};
		const accepted = clip( previous, desired, query );
		segment.at = now;
		if ( query.status & 0x10000000 ) {
			segment.blocked = true;
			return previous;
		}
		if ( !accepted ) return previous;
		segment.walkingPath = extendWalkingHistory( {
			points: segment.walkingPath,
			from: previous,
			to: accepted,
			sourceOwner: query.sourceOwner,
			clip
		} );
		segment.blocked = !!(query.status & 1);
		segment.previous = accepted;
		segment.arrived = next.arrived;
		const cursor = cursors.get( gid ) ?? {};
		cursor.owner = query.owner;
		cursors.set( gid, cursor );
		return accepted;
	}
	/*
================
advanceCommand

A native command advances before replacing its leg. Preserve the checked
lookahead already displayed only when it contains the newly accepted pose.
A changed terrain interpolation cannot authorize a new connection.
================
	*/
	function advanceCommand( gid: number, segment: RemoteSegment, now: number ) {
		const published = segment.presentationHistory?.result;
		const pose = advance( gid, segment, now );
		if ( !segment.fixedTiming ) {
			segment.walkingPath = rewindWalkingHistory( published, pose ) ?? segment.walkingPath;
			segment.presentationHistory = undefined;
		}
		return pose;
	}
	/*
================
presentationPath

Only a fully admitted short corridor permits extrapolation. Failed lookahead
does not move the actor or replace its actual-time collision result.
================
	*/
	function presentationPath(
		gid: number,
		segment: RemoteSegment,
		pose: Pose
	): NonNullable<EntityState["movementPath"]> {
		if ( segment.fixedTiming ) {
			return {
				from: segment.from,
				to: segment.to,
				durationMs: segment.duration,
				displacement: true,
				startedAtMs: segment.start
			};
		}
		const desired = candidate( segment, pose, PRESENTATION_LOOKAHEAD_MS ).pose;
		const query = { slide: false, sourceOwner: cursors.get( gid )?.owner, status: 0 };
		const checked = segment.blocked || segment.arrived ?
			null :
			clip( pose, desired, query );
		const admitted = checked && !(query.status & 0x10000001) &&
			poseDistance( checked, desired ) < ENDPOINT_EPSILON;
		const to = admitted ? checked : pose;
		let walkingPath = segment.walkingPath;
		if ( admitted ) {
			const cache = segment.presentationHistory;
			if (
				cache && cache.from === pose && cache.points === segment.walkingPath &&
				cache.to.regionId === to.regionId && cache.to.x === to.x && cache.to.y === to.y && cache.to.z === to.z
			) {
				walkingPath = cache.result;
			} else {
				walkingPath = extendWalkingHistory( {
					points: segment.walkingPath,
					from: pose,
					to,
					sourceOwner: query.sourceOwner,
					clip
				} );
				segment.presentationHistory = { from: pose, to, points: segment.walkingPath, result: walkingPath };
			}
		}
		return {
			from: pose,
			to,
			walkingPath,
			durationMs: PRESENTATION_LOOKAHEAD_MS
		};
	}
	/*
================
groundCorrection

A source receipt may retire native travel while its accepted history still
carries visible recovery. Certify its connector before replacing that proof;
authored displacements and disconnected navigation never inherit it.
================
	*/
	function groundCorrection( entity: EntityState, pose: Pose ) {
		const segment = active.get( entity.gid );
		const reference = segment?.previous ??
			{ regionId: entity.regionId, x: entity.x, y: entity.y, z: entity.z, angle: entity.heading };
		const sourceOwner = cursors.get( entity.gid )?.owner;
		const resolved = resolve( entity.gid, pose, reference );
		const walkingPath = segment?.fixedTiming || entity.movementPath?.displacement ?
			undefined :
			correctWalkingHistory( {
				points: segment?.presentationHistory?.result ?? segment?.walkingPath ??
					entity.movementPath?.walkingPath,
				from: reference,
				to: resolved,
				sourceOwner,
				clip
			} );
		return { segment, resolved, walkingPath };
	}
	/*
================
directionLeg

The next direction leg retains intent; advance checks each elapsed step.
================
	*/
	function directionLeg( entity: EntityState, pose: Pose, heading: number, now: number ) {
		const to = directionLegEnd( pose, heading );
		return {
			from: pose,
			to,
			start: now,
			duration: duration( pose, to, entity ),
			speed: (movementGait( entity.movementMode ) === "walk" ? entity.walkSpeed : entity.runSpeed) ?? 0,
			direction: heading,
			vector: clientWalkingVector( modelYaw( heading ) ),
			previous: pose
		};
	}
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
			const { segment, resolved, walkingPath } = groundCorrection( entity, pose );
			let path: EntityState["movementPath"] = walkingPath ?
				{ from: resolved, to: resolved, walkingPath } :
				undefined;
			if ( segment?.fixedTiming ) {
				path = {
					from: segment.from,
					to: segment.to,
					durationMs: segment.duration,
					displacement: true,
					startedAtMs: segment.start
				};
			}
			if ( segment && !segment.fixedTiming ) {
				const durationMs = duration( resolved, segment.to, entity );
				active.set( entity.gid, {
					...segment,
					from: resolved,
					previous: resolved,
					start: now,
					duration: durationMs,
					at: now,
					arrived: false,
					blocked: false,
					walkingPath,
					presentationHistory: undefined
				} );
				path = { from: resolved, to: segment.to, durationMs };
			}
			return {
				...update( entity.gid, resolved, !!segment || !!entity.moving ),
				movementPath: segment ? presentationPath( entity.gid, active.get( entity.gid )!, resolved ) : path
			};
		},
		/*
================
correct
================
		*/
		correct( entity: EntityState, pose: Pose ): MotionUpdate {
			const { resolved, walkingPath } = groundCorrection( entity, pose );
			active.delete( entity.gid );
			return {
				...update( entity.gid, resolved ),
				movementPath: walkingPath ? { from: resolved, to: resolved, walkingPath } : undefined
			};
		},
		/*
================
displace
================
		*/
		displace( entity: EntityState, command: import("@/engine/contracts/gameplay").CastDisplacement, now: number ) {
			const previous = active.get( entity.gid ),
				from = previous ?
					advance( entity.gid, previous, now ) :
					{ regionId: entity.regionId, x: entity.x, y: entity.y, z: entity.z, angle: entity.heading };
			const segment = displacementSegment( from, command, now );
			active.set( entity.gid, {
				...segment,
				fixedTiming: true,
				castToken: command.kind === 8 ? command.token : undefined
			} );
			return {
				...update( entity.gid, resolve( entity.gid, sample( segment, now ), from ), segment.duration > 0 ),
				movementPath: {
					from: segment.from,
					to: segment.to,
					durationMs: segment.duration,
					displacement: true,
					startedAtMs: segment.start
				},
				poseAtMs: now
			};
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
				segment.walkingPath = undefined;
				segment.presentationHistory = undefined;
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
				duration: duration( from, to, entity ),
				speed: (movementGait( entity.movementMode ) === "walk" ? entity.walkSpeed : entity.runSpeed) ?? 0
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
			const current = previous ? advanceCommand( entity.gid, previous, now ) : published;
			const decoded = decodeNativeMovement( p, current );
			// A source-less angular acknowledgement changes nothing in motion.
			if ( decoded.kind === "keep" ) {
				return previous ?
					presentationPath( entity.gid, previous, current ) :
					{ from: published, to: published };
			}
			const sourceOwner = cursors.get( entity.gid )?.owner;
			const from = resolve( entity.gid, decoded.from, previous?.previous ?? published );
			const walkingPath = previous?.fixedTiming || entity.movementPath?.displacement ?
				undefined :
				correctWalkingHistory( {
					points: previous?.walkingPath ?? entity.movementPath?.walkingPath,
					from: current,
					to: from,
					sourceOwner,
					clip
				} );
			if ( decoded.kind === "direction" ) {
				const leg = directionLeg( entity, from, decoded.heading!, now );
				const next = { ...leg, walkingPath };
				active.set( entity.gid, next );
				return presentationPath( entity.gid, next, from );
			}
			const to = decoded.to, durationMs = duration( from, to, entity );
			active.set( entity.gid, {
				from,
				to,
				start: now,
				duration: durationMs,
				walkingPath,
				speed: (movementGait( entity.movementMode ) === "walk" ? entity.walkSpeed : entity.runSpeed) ?? 0
			} );
			return presentationPath( entity.gid, active.get( entity.gid )!, from );
		},
		/*
================
steer

0xB2CF (CPSMission_OnEntityUpdateAngle0xB2CF 0x775A90): the target yaw of
another mover. A direction walk continues from where it is along the new
heading (CNavigationController_SetTargetYaw rewrites the walk vector); an
idle mover turns where it stands. A destination walk keeps its own facing.
================
		*/
		steer( entity: EntityState, heading: number, now: number ): MotionUpdate | null {
			const segment = active.get( entity.gid );
			if ( segment?.direction !== undefined ) {
				const pose = advanceCommand( entity.gid, segment, now );
				const leg = directionLeg( entity, pose, heading, now );
				const continued = { ...leg, walkingPath: segment.walkingPath };
				active.set( entity.gid, continued );
				return {
					...update( entity.gid, pose, true ),
					heading,
					movementPath: presentationPath( entity.gid, continued, pose )
				};
			}
			if ( segment ) return null;
			return {
				...update( entity.gid, {
					regionId: entity.regionId,
					x: entity.x,
					y: entity.y,
					z: entity.z,
					angle: heading
				} )
			};
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
			const pose = advanceCommand( next.gid, segment, now );
			const retimed = movementSpeedTransition( { ...segment, from: pose, start: now }, before, after, now );
			if ( retimed.duration && !segment.blocked ) {
				active.set( next.gid, {
					...segment,
					...retimed,
					from: pose,
					previous: pose,
					at: now,
					speed: after
				} );
			} else active.delete( next.gid );
			return {
				...update( next.gid, pose, active.has( next.gid ) ),
				movementPath: active.has( next.gid ) ?
					presentationPath( next.gid, active.get( next.gid )!, pose ) :
					segment.walkingPath ?
					{ from: pose, to: pose, walkingPath: segment.walkingPath } :
					undefined
			};
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
				advance( entity.gid, segment, now ) :
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
			const mode = entity.movementMode ?? 3;
			// Displacement keeps its authored timing (action state 4/5, not 858450's channel).
			if ( segment.fixedTiming && mode !== 0 && mode !== 4 ) return null;
			const speed = (movementGait( entity.movementMode ) === "walk" ? entity.walkSpeed : entity.runSpeed) ?? 0;
			const pose = advanceCommand( entity.gid, segment, now );
			const next = movementModeTransition( { ...segment, from: pose, start: now }, mode, speed, now );
			if ( next.segment && !segment.blocked ) {
				active.set( entity.gid, {
					...next.segment,
					from: pose,
					previous: pose,
					direction: segment.direction,
					vector: segment.vector,
					walkingPath: segment.walkingPath,
					speed
				} );
			} else active.delete( entity.gid );
			return {
				...update( entity.gid, pose, active.has( entity.gid ) ),
				movementPath: active.has( entity.gid ) ?
					presentationPath( entity.gid, active.get( entity.gid )!, pose ) :
					segment.fixedTiming || !segment.walkingPath ?
					undefined :
					{ from: pose, to: pose, walkingPath: segment.walkingPath }
			};
		},
		/*
================
step
================
		*/
		step( now: number ): MotionUpdate[] {
			const changed: MotionUpdate[] = [];
			for ( const [gid, segment] of active ) {
				const pose = advance( gid, segment, now );
				segment.previous = pose;
				const done = segment.fixedTiming ?
					now >= segment.start + segment.duration :
					segment.blocked || segment.arrived;
				changed.push( {
					...update( gid, pose, !done ),
					movementPath: presentationPath( gid, segment, pose ),
					// Presentation draws the path on the frame clock from sample times.
					poseAtMs: now
				} );
				if ( !done ) continue;
				// A direction walk has no arrival; its next leg starts here at
				// the speed the finished leg was timed with.
				if ( segment.direction !== undefined && !segment.blocked ) {
					const speed = segment.speed ?? 0;
					const to = directionLegEnd( pose, segment.direction );
					active.set( gid, {
						from: pose,
						to,
						start: now,
						duration: speed > 0 ? poseDistance( pose, to ) / speed * 1000 : 0,
						direction: segment.direction,
						speed,
						previous: pose
					} );
					continue;
				}
				active.delete( gid );
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
