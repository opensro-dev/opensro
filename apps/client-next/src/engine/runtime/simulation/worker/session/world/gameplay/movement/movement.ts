/*
===========================================================================

movement.ts - local navigation prediction and authoritative acknowledgements

Owns movement intent, admitted navigation and receipt ordering. Accepted
endpoints retain valid progress made during transport; clipping, rejection
and native corrections remain authoritative.

===========================================================================
*/
import { positionSkillGoal } from "@/engine/foundation/gameplay/position-skill";
import type { NavOwner, NavOwnerSpan } from "@/engine/foundation/navigation/dungeon-ownership";
import {
	type MovementSegment,
	movementHeading,
	decodeNativeMovement,
	poseDistance,
	sampleMovement,
	interpolateMovement as interpolate,
	movementModeTransition,
	movementSpeedTransition
} from "@/engine/foundation/gameplay/native-movement";
import { createNavigation } from "./navigation/navigation";
import type { Pose } from "@/engine/contracts/gameplay";
import { displacementSegment } from "@/engine/foundation/gameplay/cast-displacement";
const ENDPOINT_EPSILON = .01;
const DUNGEON_HEIGHT_EPSILON = 2;

/*
================
createMovement
================
*/
export function createMovement( send: ( frame: import("@/engine/contracts/network").WireFrame ) => void ) {
	const navigation = createNavigation();
	let minimapQueries: readonly Pose[] = [];
	let pose: Pose | null = null,
		authoritative: Pose | null = null,
		segment:
			| (MovementSegment & {
				timing: "speed" | "server";
				owners?: readonly NavOwnerSpan[];
				castToken?: number;
				fixedTiming?: boolean;
			})
			| null = null;
	let navigationRequestId: number | undefined;
	let navigationFailure: { region: number; requestId?: number; error: string; } | undefined;
	let navigationRegion: number | undefined, owner: NavOwner | undefined;
	let surfaceCursor: import("@/engine/contracts/navigation").SurfaceCursor = {};
	let speed = 50, walkSpeed = 20, runSpeed = 50, mode = 3;
	let movementRevision = 0;
	let nextId = 0, acknowledged = 0, error: string | null = null;
	let life: "alive" | "dead" = "alive";
	const pending = new Map<number, {
		to: Pose;
		sent: number;
		predictedEnd: Pose | null;
	}>();
	/*
================
admit
================
	*/
	function admit( value: unknown ): Pose {
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
reconcile
================
	*/
	function reconcile( from: Pose ): Pose {
		if ( pose && owner ) {
			const query: { slide: boolean; sourceOwner?: NavOwner; owner?: NavOwner; } = {
				slide: false,
				sourceOwner: owner
			};
			const resolved = navigation.clip( pose, from, query );
			if ( resolved && poseDistance( resolved, from ) < .01 ) {
				owner = query.owner;
				return { ...from, y: resolved.y };
			}
		}
		owner = undefined;
		return navigation.surface( from );
	}
	/*
================
bindOwners
================
	*/
	function bindOwners( value: NonNullable<typeof segment> ) {
		const query: { slide: boolean; sourceOwner?: NavOwner; owners?: readonly NavOwnerSpan[]; } = {
			slide: false,
			sourceOwner: owner
		};
		const resolved = navigation.clip( value.from, value.to, query );
		return { ...value, owners: resolved && poseDistance( resolved, value.to ) < .01 ? query.owners : undefined };
	}
	/*
================
liveOwner
================
	*/
	function liveOwner( now: number ) {
		if ( !segment?.owners ) return owner;
		const t = segment.duration ? Math.max( 0, Math.min( 1, (now - segment.start) / segment.duration ) ) : 1;
		let result: NavOwner | undefined;
		for ( const span of segment.owners ) {
			if ( t >= span.from && t <= span.to ) result = { placement: span.placement, cell: span.cell };
		}
		return result;
	}
	return {
		/*
================
life
================
		*/
		life( value: 1 | 2, now: number ) {
			// Death retires commands, not just their current animation. A late
			// receipt must not restart the corpse or time out its pending queue.
			// Keep the chosen walk/run channel for the subsequent revival.
			if ( value === 1 ) {
				life = "alive";
				return;
			}
			if ( life === "dead" ) return;
			if ( segment ) pose = navigation.surface( sampleMovement( segment, now ), pose ?? segment.from, owner );
			authoritative = pose;
			segment = null;
			acknowledged = nextId;
			pending.clear();
			error = null;
			life = "dead";
		},
		/*
================
displace
================
		*/
		displace( command: import("@/engine/contracts/gameplay").CastDisplacement, now: number ) {
			if ( !pose ) return;
			owner = liveOwner( now );
			const from = segment ? sampleMovement( segment, now ) : pose,
				next = displacementSegment( from, command, now );
			// Displacement walks from the cell under the actor (native 8797C0
			// steps through navigation; the server's QueryMovement walks from the
			// stored source cell). Keep the owner: clearing it here re-guessed the
			// start surface from height and could drop a dash off a deck.
			acknowledged = nextId;
			pending.clear();
			surfaceCursor = {};
			pose = authoritative = navigation.surface( next.from, from, owner );
			segment = next.duration ?
				bindOwners( {
					...next,
					from: pose,
					timing: "server",
					fixedTiming: true,
					castToken: command.kind === 8 ? command.token : undefined
				} ) :
				null;
			return now + next.duration;
		},
		/*
================
cancelCast
================
		*/
		cancelCast( token: number, now: number ) {
			if ( segment?.castToken === token ) {
				pose = authoritative = navigation.surface(
					sampleMovement( segment, now ),
					pose ?? segment.from,
					owner
				);
				segment = null;
			}
		},
		// Live local pose at `now`: the rest pose advanced along any segment.
		/*
================
current
================
		*/
		current( now: number ) {
			return segment ? sampleMovement( segment, now ) : pose;
		},
		/*
================
constrainDisplacement
================
		*/
		constrainDisplacement(
			command: import("@/engine/contracts/gameplay").CastDisplacement,
			from: Pose,
			local = false,
			now = 0
		) {
			if ( command.kind === 2 ) return command;
			const to = { ...command.destination, angle: from.angle },
				resolved = navigation.clip( from, to, {
					slide: true,
					sourceOwner: local ? liveOwner( now ) : undefined
				} );
			return resolved ? { ...command, destination: resolved } : command;
		},
		/*
================
groundSkillGoal
================
		*/
		groundSkillGoal( query: import("@/engine/contracts/navigation").GroundPickQuery, now: number ) {
			if ( !pose ) return null;
			const picked = navigation.pick( query ),
				from = segment ? sampleMovement( segment, now ) : pose,
				to = positionSkillGoal( from, query, picked );
			const clipped = to ? navigation.clip( from, to, { slide: false, sourceOwner: liveOwner( now ) } ) : null;
			return clipped;
		},
		pick: navigation.pick,
		surface: navigation.surface,
		/*
================
heading
================
		*/
		heading( angle: number ) {
			if ( !Number.isFinite( angle ) || angle < 0 || angle > 65535 ) throw new Error( "Invalid heading" );
			if ( pose ) pose = { ...pose, angle };
			if ( authoritative ) authoritative = { ...authoritative, angle };
			if ( segment ) segment = { ...segment, from: { ...segment.from, angle }, to: { ...segment.to, angle } };
		},
		/*
================
mode
================
		*/
		mode( value: number, now = 0 ) {
			mode = value;
			speed = value === 2 ? walkSpeed : runSpeed;
			if ( !segment ) return;
			const next = movementModeTransition( segment, value, speed, now, segment.timing === "server" );
			pose = navigation.surface( next.pose, pose ?? next.pose, owner );
			if ( segment.timing === "server" || !next.segment ) authoritative = pose;
			segment = next.segment ?
				(segment.timing === "server" ?
					segment :
					bindOwners( { ...next.segment, from: pose, timing: segment.timing } )) :
				null;
		},
		/*
================
speeds
================
		*/
		speeds( walk: number, run: number, now: number ) {
			if ( ![ walk, run ].every( n => Number.isFinite( n ) && n > 0 ) ) {
				throw Error( "Invalid movement speed channels" );
			}
			const previous = speed;
			walkSpeed = walk;
			runSpeed = run;
			speed = mode === 2 ? walk : run;
			if ( segment && !segment.fixedTiming && speed !== previous ) {
				const next = movementSpeedTransition( segment, previous, speed, now );
				pose = navigation.surface( next.from, pose ?? next.from, owner );
				segment = next.duration ? bindOwners( { ...segment, ...next, from: pose } ) : null;
			}
		},
		/*
================
native
================
		*/
		native( p: Uint8Array, now: number, gid: number ) {
			if ( !pose || life === "dead" ) {
				return;
			}
			const current = segment ? sampleMovement( segment, now ) : pose;
			const decoded = decodeNativeMovement( p, current );
			if ( decoded.gid !== gid ) {
				return;
			}
			movementRevision++;
			authoritative = reconcile( decoded.from );
			pose = authoritative;
			segment = bindOwners( {
				from: pose,
				to: decoded.to,
				start: now,
				timing: "speed",
				duration: poseDistance( pose, decoded.to ) / speed * 1000
			} );
		},
		/*
================
navigation
================
		*/
		navigation( region: number, bundle: unknown, requestId?: number ) {
			const kept = navigation.anchor( owner );
			try {
				navigation.install( region, bundle );
				navigationFailure = undefined;
				navigationRequestId = requestId;
			} catch ( cause ) {
				navigationFailure = {
					region,
					requestId,
					error: "Navigation admission failed for region " + region + ": " + String( cause )
				};
				return;
			}
			navigationRegion = region;
			surfaceCursor = {};
			owner = navigation.relocate( kept );
			if ( segment ) segment = { ...segment, owners: undefined };
			// Spawn may precede collision admission. Stationary actors never enter
			// the movement-step surface resolver, so finish grounding here.
			else if ( pose ) pose = authoritative = navigation.surface( pose, pose, owner );
		},
		/*
================
minimapFloors
================
		*/
		minimapFloors( poses: readonly Pose[] ) {
			if ( !Array.isArray( poses ) || poses.length > 4096 ) throw Error( "Minimap floor query budget" );
			minimapQueries = poses.map( admit );
		},
		/*
================
seed
================
		*/
		seed( value: Pose ) {
			movementRevision++;
			life = "alive";
			surfaceCursor = {};
			owner = undefined;
			acknowledged = nextId;
			pose = authoritative = navigation.surface( admit( value ) );
			segment = null;
			pending.clear();
			error = null;
		},
		/*
================
correct
================
		*/
		correct( value: Pose ) {
			movementRevision++;
			// A live source correction ends motion, but is not a new spawn.
			// Resolve its surface through the existing navigation owner before
			// retiring the segment; server endpoint Y can be below a hill/deck.
			pose = authoritative = reconcile( admit( value ) );
			surfaceCursor = {};
			segment = null;
			acknowledged = nextId;
			pending.clear();
			error = null;
		},
		/*
================
request
================
		*/
		request( value: Pose, now: number, cosGid?: number ) {
			if ( life === "dead" ) throw new Error( "Movement while dead" );
			if ( !pose || pending.size >= 32 || nextId === 0xffffffff ) {
				throw new Error( "Movement command capacity exceeded or player absent" );
			}
			const p = admit( value ),
				to = { ...p, x: Math.trunc( p.x ), y: Math.trunc( p.y ), z: Math.trunc( p.z ) },
				id = nextId + 1;
			if ( cosGid !== undefined && (!Number.isInteger( cosGid ) || cosGid < 1 || cosGid > 0xffffffff) ) {
				throw Error( "Invalid COS owner" );
			}
			const offset = cosGid === undefined ? 0 : 5,
				payload = new Uint8Array( 14 + offset ),
				v = new DataView( payload.buffer );
			payload[0] = cosGid === undefined ? 1 : 2;
			if ( cosGid !== undefined ) {
				v.setUint32( 5, cosGid, true );
				payload[9] = 1;
			}
			v.setUint32( 1, id, true );
			payload[5 + offset] = 1;
			v.setUint16( 6 + offset, to.regionId, true );
			v.setInt16( 8 + offset, to.x, true );
			v.setInt16( 10 + offset, to.y, true );
			v.setInt16( 12 + offset, to.z, true );
			// Only predict over admitted complete coverage. Unknown object/deck or
			// dungeon collision stays at the confirmed pose until the server answers.
			const query: { slide: boolean; sourceOwner?: NavOwner; owners?: readonly NavOwnerSpan[]; } = {
				slide: false,
				sourceOwner: owner
			};
			const clipped = navigation.clip( pose, to, query );
			const frame = { opcode: 9, payload };
			send( frame );
			nextId = id;
			movementRevision++;
			pending.set( id, { to, sent: now, predictedEnd: clipped } );
			error = null;
			if ( clipped ) {
				segment = {
					from: pose,
					to: { ...clipped, angle: movementHeading( pose, clipped ) },
					start: now,
					timing: "speed",
					duration: poseDistance( pose, clipped ) / speed * 1000,
					owners: query.owners
				};
			}
			return frame;
		},
		/*
================
receive
================
		*/
		receive( payload: Uint8Array, now: number, expectedGid?: number ) {
			const r = JSON.parse( new TextDecoder( "utf-8", { fatal: true } ).decode( payload ) ) as {
				v: number;
				id: number;
				gid: number;
				accepted: boolean;
				serverTimeMs: number;
				error?: string;
				world: {
					spawn: Pose;
					moveSegment?: {
						from: Pose;
						startedAtMs: number;
						arrivesAtMs: number;
					};
				};
			};
			if (
				(expectedGid !== undefined && r.gid !== expectedGid) || r.v !== 1 || !Number.isInteger( r.id ) ||
				typeof r.accepted !== "boolean" || !Number.isFinite( r.serverTimeMs )
			) {
				throw new Error( "Invalid movement receipt" );
			}
			if ( r.id <= acknowledged ) {
				return;
			}
			const command = pending.get( r.id );
			if ( !command ) {
				throw new Error( "Unsolicited movement receipt" );
			}
			const to = admit( r.world?.spawn ), s = r.world.moveSegment;
			let replacement: typeof segment = null;
			if ( s ) {
				const from = admit( s.from );
				if (
					!Number.isFinite( s.startedAtMs ) || !Number.isFinite( s.arrivesAtMs ) ||
					s.arrivesAtMs <= s.startedAtMs
				) {
					throw new Error( "Invalid authoritative movement clock" );
				}
				const t = Math.max(
					0,
					Math.min( 1, (r.serverTimeMs - s.startedAtMs) / (s.arrivesAtMs - s.startedAtMs) )
				);
				replacement = {
					from: interpolate( from, to, t ),
					to,
					start: now,
					timing: "server",
					duration: Math.max( 0, s.arrivesAtMs - r.serverTimeMs )
				};
			}
			// Receipts acknowledge commands, not the latest intent. An older
			// response must not reinstall its path over a newer prediction.
			// Keep navigation ownership with the current path until its receipt.
			if ( r.id < nextId ) {
				authoritative = replacement?.from ?? to;
				acknowledged = r.id;
				for ( const id of pending.keys() ) if ( id <= r.id ) pending.delete( id );
				return;
			}
			const predictedOwner = owner;
			authoritative = reconcile( replacement?.from ?? to );
			// A receipt confirms an endpoint, not the client's old frame. Turning
			// during transit changes the two start positions; collinearity and
			// a fixed distance cap cannot prove whether that intent was accepted.
			// Retain progress only for the confirmed endpoint and a still-clear
			// route from the predicted position. Server clipping/rejection wins.
			const predicted = pose, predictedEnd = command.predictedEnd;
			let confirmedPrediction = false;
			if (
				r.accepted && replacement && predicted && predictedEnd &&
				predicted.regionId === authoritative.regionId && predictedEnd.regionId === to.regionId &&
				poseDistance( predictedEnd, to ) < ENDPOINT_EPSILON &&
				(!(to.regionId & 0x8000) || Math.abs( predictedEnd.y - to.y ) < DUNGEON_HEIGHT_EPSILON)
			) {
				const remaining = poseDistance( authoritative, to ), tail = poseDistance( predicted, to );
				const route = navigation.clip( predicted, to, { slide: false, sourceOwner: predictedOwner } );
				confirmedPrediction = tail <= remaining && !!route &&
					poseDistance( route, to ) < ENDPOINT_EPSILON &&
					(!(to.regionId & 0x8000) || Math.abs( route.y - to.y ) < DUNGEON_HEIGHT_EPSILON);
				if ( confirmedPrediction ) {
					replacement = tail < ENDPOINT_EPSILON ?
						null :
						{ ...replacement, duration: replacement.duration * tail / remaining };
				}
			}
			pose = confirmedPrediction && predicted ? predicted : authoritative;
			if ( confirmedPrediction ) owner = predictedOwner;
			movementRevision++;
			segment = replacement ? bindOwners( { ...replacement, from: pose } ) : null;
			acknowledged = r.id;
			for ( const id of pending.keys() ) {
				if ( id <= r.id ) {
					pending.delete( id );
				}
			}
			error = r.accepted ? null : r.error ?? "Movement rejected";
		},
		/*
================
step
================
		*/
		step( now: number ) {
			if ( pending.size && now - pending.values().next().value!.sent > 10000 ) {
				throw new Error( "Movement receipt timed out; resynchronize session" );
			}
			if ( !segment ) {
				return false;
			}
			const t = segment.duration ? Math.max( 0, Math.min( 1, (now - segment.start) / segment.duration ) ) : 1;
			const previous = pose ?? segment.from;
			if ( segment.owners ) {
				owner = undefined;
				for ( const span of segment.owners ) {
					if ( t >= span.from && t <= span.to ) owner = { placement: span.placement, cell: span.cell };
				}
			}
			// A segment that begins on terrain has no precomputed object spans.
			// Acquire and retain an object owner as each step enters its surface,
			// just as remote motion does, instead of reselecting nearest terrain Y.
			pose = navigation.surface( sampleMovement( segment, now ), previous, owner, surfaceCursor );
			owner = surfaceCursor.owner ?? owner;
			if ( t === 1 ) {
				authoritative = pose;
				segment = null;
			}
			return true;
		},
		/*
================
state
================
		*/
		state() {
			return {
				navigationRequestId,
				navigationFailure,
				movementPath: segment ? { from: segment.from, to: segment.to } : undefined,
				movementRevision,
				navigationFloor: navigation.floor( pose, owner ),
				minimapFloors: Object.fromEntries(
					minimapQueries.map( p => [ [ p.regionId, p.x, p.y, p.z ].join( ":" ), navigation.floor( p ) ] )
				),
				navigationRegion,
				navigationBlock: navigation.block( pose, owner ),
				navigationOwner: owner ? { ...owner } : undefined,
				moving: !!segment && segment.duration > 0,
				pose,
				authoritativePose: authoritative,
				pendingMoves: pending.size,
				acknowledgedMove: acknowledged,
				error
			};
		},
		/*
================
clear
================
		*/
		clear() {
			life = "alive";
			acknowledged = nextId;
			minimapQueries = [];
			speed = runSpeed = 50;
			walkSpeed = 20;
			mode = 3;
			surfaceCursor = {};
			owner = undefined;
			pose = authoritative = null;
			segment = null;
			pending.clear();
			error = null;
			navigation.clear();
			navigationRegion = undefined;
			navigationRequestId = undefined;
			navigationFailure = undefined;
		}
	};
}
