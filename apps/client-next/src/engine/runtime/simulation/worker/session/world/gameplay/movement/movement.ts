/*
===========================================================================

movement.ts - local navigation prediction and authoritative acknowledgements

Owns movement intent, admitted navigation and receipt ordering. Accepted
endpoints retain valid progress made during transport; clipping, rejection
and native corrections remain authoritative.

A direction walk (a click that missed the ground, direction-movement.ts)
has no endpoint: the walker renews its leg along the heading until the
local clip blocks it or the server corrects it, and reconciles toward the
server's walk every 500 ms (directionDrift).

===========================================================================
*/
import { MOVEMENT_RECEIPT_TIMEOUT } from "@/engine/contracts/network";

import { positionSkillGoal } from "@/engine/foundation/gameplay/position-skill";
import {
	correctWalkingHistory,
	extendWalkingHistory,
	rewindWalkingHistory
} from "@/engine/foundation/gameplay/walking-history";
import type { NavOwner, NavOwnerSpan } from "@/engine/foundation/navigation/dungeon-ownership";
import {
	type MovementSegment,
	movementHeading,
	decodeNativeMovement,
	poseDistance,
	clientWalkingStep,
	clientPlanarDistance,
	clientWalkingDirection,
	clientWalkingVector,
	sampleMovement,
	interpolateMovement as interpolate,
	REGION_SIZE,
	movementModeTransition,
	movementSpeedTransition,
	validMovementSpeed,
	movementDuration
} from "@/engine/foundation/gameplay/native-movement";
import { createNavigation } from "./navigation/navigation";
import { admitPose, decodeMovementReceipt, receiptWorld } from "@/engine/foundation/gameplay/movement-wire";
import type { Pose } from "@/engine/contracts/gameplay";
import { displacementSegment } from "@/engine/foundation/gameplay/cast-displacement";
import {
	DRIFT_PERIOD_MS,
	directionDrift,
	directionLegBlocked,
	directionLegEnd,
	directionMoveBody,
	directionPoint,
	modelYaw
} from "@/engine/foundation/gameplay/direction-movement";
const ENDPOINT_EPSILON = .01;
const PRESENTATION_LOOKAHEAD_MS = 100;
const NAVIGATION_STOP = 1;
const NAVIGATION_REJECT = 0x10000000;
const MOVEMENT_RECEIPT_TIMEOUT_MS = 10000;

// Opcode of the replacement client's predicted-movement envelope
// (transport.OpPredictedMove): [u8 1|2][u32 id] then the native body.
const OP_PREDICTED_MOVE = 9;
const ENVELOPE_PLAYER = 1;
const ENVELOPE_COS = 2;
// 0x769E tag 1 carries the vehicle's 0x7738 body.
const COS_MOVEMENT_TAG = 1;
// How long a walk held for a cast waits for the server's settle before it
// follows the server again: a deferred cast never settles.
const CAST_HOLD_MS = 1500;
// A held player rejoins the server's walk no faster than this multiple of its
// own speed, so the catch-up reads as walking, never as a slide or a jump.
const CATCHUP_SPEED_FACTOR = 1.3;

/*
================
WalkLead

Who is ahead on the local walk, by one delivery.

	client  the client started it from its own command (a ground click, a
	        direction walk, a predicted run-up). The server starts the same
	        walk when the command reaches it, so the server stands where the
	        client stood one delivery ago.
	server  the client is replaying a walk the server started (a chase, a
	        follow, a leg a receipt replaced). The client stands where the
	        server stood one delivery ago.

It decides what a command that may stop the walk does locally (holdForCast).
The server acts where the command finds it. On a client-led walk that is
where the client stands at the press: stop there. On a server-led walk it is
where the client will stand when the answer arrives: keep walking, and the
server's stop or new leg lands under the player's feet.
================
*/
type WalkLead = "client" | "server";

/*
================
DirectionReference

Where the server's direction walk is: it left `from` at `start` along
heading and stops after limit units (Infinity until a leg ended blocked).
================
*/
interface DirectionReference {
	from: Pose;
	start: number;
	heading: number;
	limit: number;
}

// A receipt that moves the player further than this from the predicted pose
// is reported: it is visible on screen.
const REANCHOR_REPORT_UNITS = 1;
// The first reports in full, then every REANCHOR_REPORT_EVERY-th with the
// running count, so a recurring snap never goes silent.
const MAX_REANCHOR_REPORTS = 1;
const REANCHOR_REPORT_EVERY = 50;
const RECENT_REANCHOR_LIMIT = 16;

/*
================
sameNavigationSpace

Outdoor regions form one plane, so any two outdoor poses can be measured
and walked between; a dungeon region is its own space.
================
*/
function sameNavigationSpace( a: Pose, b: Pose ): boolean {
	if ( !((a.regionId | b.regionId) & 0x8000) ) return true;
	return a.regionId === b.regionId;
}

/*
================
planarOffset

b - a on the ground plane in world units. Outdoor regions share one plane;
callers check sameNavigationSpace first.
================
*/
function planarOffset( a: Pose, b: Pose ): [number, number] {
	return [
		b.x - a.x + ((b.regionId & 255) - (a.regionId & 255)) * REGION_SIZE,
		b.z - a.z + ((b.regionId >>> 8) - (a.regionId >>> 8)) * REGION_SIZE
	];
}

/*
================
ReceiptReconciliation

What a latest-command receipt does to the predicted walk: keep it (and walk
on to the server's stop over segment), settle on the stop the prediction has
already passed, or take the server's pose. tail and remaining are the
predicted and the server's distances to the stop, for the report.
================
*/
interface ReceiptReconciliation<Segment> {
	readonly kind: "keep" | "settle" | "server";
	readonly reason: string;
	readonly segment?: Segment | null;
	readonly tail?: number;
	readonly remaining?: number;
}

/*
================
createMovement
================
*/
export function createMovement(
	send: ( frame: import("@/engine/contracts/network").WireFrame ) => void,
	record: ( event: import("@/engine/contracts/movement-diagnostic").MovementDiagnostic ) => void = () => {}
) {
	const navigation = createNavigation();
	let minimapQueries: readonly Pose[] = [];
	let pose: Pose | null = null,
		authoritative: Pose | null = null,
		segment:
			| (MovementSegment & {
				timing: "speed" | "server";
				// Who is ahead on this walk (see WalkLead).
				lead: WalkLead;
				owners?: readonly NavOwnerSpan[];
				castToken?: number;
				fixedTiming?: boolean;
				admitted?: boolean;
				admittedFrom?: Pose;
				admittedTo?: Pose;
				history?: readonly Pose[];
				presentationHistory?: readonly Pose[];
				walking?: { direction: readonly [number, number]; speed: number; };
				// A leg of a direction walk; blocked legs end the walk.
				direction?: { heading: number; blocked: boolean; };
			})
			| null = null;
	// The direction walk in progress: its heading, the drift speed factor,
	// the next reconciliation and the server walk it reconciles toward.
	let walk: { heading: number; factor: number; nextDrift: number; reference: DirectionReference | null; } | null =
		null;
	let navigationRequestId: number | undefined;
	let navigationFailure: { region: number; requestId?: number; error: string; } | undefined;
	let navigationRegion: number | undefined, owner: NavOwner | undefined;
	let surfaceCursor: import("@/engine/contracts/navigation").SurfaceCursor = {};
	let speed = 50, walkSpeed = 20, runSpeed = 50, mode = 3;
	let movementRevision = 0;
	let transitionOwner: NavOwner | undefined;
	let transition: import("@/engine/contracts/gameplay").MovementTransition = {
		relocation: 0,
		reason: "spawn",
		eligible: false
	};
	// Simulation time of the last stepped pose; presentation extrapolates from it.
	let poseAtMs = 0;
	let nextId = 0, acknowledged = 0, error: string | null = null;
	let life: "alive" | "dead" = "alive";
	// A server-driven walk started locally before its acknowledgement (a
	// pickup run-up): where it leaves from and the goal the server will walk.
	let predicted: { from: Pose; to: Pose; } | null = null;
	// A walk stopped where a cast command left it (holdForCast): the held
	// pose, when the hold lapses, and the server walk to follow if it does.
	let castHold: {
		pose: Pose;
		since: number;
		until: number;
		resume: Parameters<typeof bindOwners>[0] | null;
	} | null = null;
	let reanchorReports = 0;
	const recentReanchors: { revision: number; source: string; distance: number; before: Pose; after: Pose; }[] = [];
	const pending = new Map<number, {
		to: Pose;
		sent: number;
		direction?: number;
	}>();
	/*
================
beginTransition

Publish presentation intent separately from authoritative motion. Generation
changes survive multiple worker steps being delivered in one publication.
================
	*/
	function beginTransition( reason: typeof transition.reason, reset = false ) {
		movementRevision++;
		transitionOwner = owner;
		transition = { relocation: transition.relocation + Number( reset ), reason, eligible: !reset };
	}
	/*
================
admitCorrection

Only complete, non-sliding navigation coverage permits a cosmetic glide.
This query does not change the authoritative pose or its surface owner.
================
	*/
	function admitCorrection( before: Pose, after: Pose ) {
		const query = { slide: false, sourceOwner: transitionOwner, status: 0 };
		const resolved = sameNavigationSpace( before, after ) ?
			navigation.clip( before, after, query ) :
			null;
		const eligible = !!resolved && !(query.status & NAVIGATION_REJECT) &&
			Math.hypot( poseDistance( resolved, after ), resolved.y - after.y ) < ENDPOINT_EPSILON;
		// A newer input can replace the reason before publication, but cannot
		// erase a discontinuity through disconnected navigation.
		transition = {
			...transition,
			relocation: transition.relocation + Number( !eligible ),
			eligible,
			corridor: eligible ? { from: before, to: after } : undefined
		};
	}
	/*
================
retainCorrectionHistory

A terminal receipt can adjust its endpoint while presentation still follows
the accepted walk behind it. Join that walk to the newly certified connector;
the tiny connector alone cannot prove the last actually displayed position.
================
	*/
	function retainCorrectionHistory( points: readonly Pose[] | undefined, before: Pose, after: Pose ) {
		if ( !transition.eligible || !points?.length || segment?.fixedTiming ) return;
		const walkingPath = correctWalkingHistory( {
			points,
			from: before,
			to: after,
			sourceOwner: transitionOwner,
			clip: navigation.clip
		} );
		if ( !walkingPath ) return;
		transition = { ...transition, walkingPath };
		if ( segment ) {
			segment.history = walkingPath;
			segment.presentationHistory = undefined;
		}
	}
	/*
================
samePredictedGoal

The server's walk goal matches a predicted goal: the wire carries whole
units, so compare within one unit on the same region.
================
	*/
	function samePredictedGoal( goal: Pose, to: Pose | undefined ): boolean {
		return !!to && to.regionId === goal.regionId && Math.abs( to.x - goal.x ) <= 1 &&
			Math.abs( to.z - goal.z ) <= 1;
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
	/*
================
noteReanchor

The player's pose moved by more than REANCHOR_REPORT_UNITS for a reason
other than walking: say what moved it, how far and with what inputs, so a
reported snap is never a guess. Called with the final committed pose.
================
	*/
	function noteReanchor(
		source: string,
		before: Pose,
		after: Pose,
		details: { atMs: number; [key: string]: unknown; }
	) {
		admitCorrection( before, after );
		const jump = sameNavigationSpace( before, after ) ? poseDistance( before, after ) : Infinity;
		transition = { ...transition, logicalDistance: jump };
		if ( jump > 0 ) {
			record( {
				kind: "movement-diagnostic",
				event: "correction",
				simulationAtMs: details.atMs,
				source,
				distance: jump,
				before: { ...before },
				after: { ...after },
				...(typeof details.id === "number" ? { requestId: details.id } : {})
			} );
		}
		if ( jump < REANCHOR_REPORT_UNITS ) return;
		reanchorReports++;
		recentReanchors.push( {
			revision: movementRevision,
			source,
			distance: jump,
			before: { ...before },
			after: { ...after }
		} );
		if ( recentReanchors.length > RECENT_REANCHOR_LIMIT ) recentReanchors.shift();
		if ( reanchorReports > MAX_REANCHOR_REPORTS && reanchorReports % REANCHOR_REPORT_EVERY !== 0 ) return;
		console.warn(
			`[SRO movement] logical ${source}: ${jump.toFixed( 1 )} units (${reanchorReports} corrections)`,
			{ before, after, revision: movementRevision, transition, ...details }
		);
	}
	/*
================
reconcileReceipt

Decides what the latest command's receipt does to the predicted walk (see
receive). authoritative is the server's pose at the receipt, to its stop.

The prediction is kept only while it is still short of the stop: the
projection of predicted - authoritative onto the server's remaining walk
must not pass its end. A clear route alone does not prove that: with the
server stopped at 110, a prediction at 119 is 9 from the stop and 10 from
the server, yet past it. A prediction behind the server walks on and
catches up no faster than CATCHUP_SPEED_FACTOR.
================
	*/
	function reconcileReceipt(
		accepted: boolean,
		predicted: Pose | null,
		predictedOwner: NavOwner | undefined,
		server: NonNullable<typeof segment>,
		to: Pose
	): ReceiptReconciliation<NonNullable<typeof segment>> {
		if ( !accepted ) return { kind: "server", reason: "rejected" };
		if ( !predicted || !authoritative ) return { kind: "server", reason: "no prediction" };
		if ( !sameNavigationSpace( predicted, to ) || !sameNavigationSpace( authoritative, to ) ) {
			return { kind: "server", reason: "other dungeon" };
		}
		const tail = poseDistance( predicted, to ), remaining = poseDistance( authoritative, to );
		const walked = planarOffset( authoritative, predicted ), ahead = planarOffset( authoritative, to );
		if ( walked[0] * ahead[0] + walked[1] * ahead[1] > remaining * (remaining + ENDPOINT_EPSILON) ) {
			return { kind: "settle", reason: "passed the server's stop", tail, remaining };
		}
		if (
			!navigation.available( predicted )
		) return { kind: "server", reason: "route blocked", tail, remaining };
		if ( tail < ENDPOINT_EPSILON ) return { kind: "keep", reason: "at the stop", segment: null, tail, remaining };
		const duration = tail <= remaining ?
			server.duration * tail / remaining :
			Math.max( server.duration, tail / (speed * CATCHUP_SPEED_FACTOR) * 1000 );
		// A prediction short of the server's own distance to the stop is still
		// ahead of it; one further away now follows the server.
		const leads = tail <= remaining;
		return {
			kind: "keep",
			reason: leads ? "ahead of the server" : "behind the server",
			segment: { ...server, duration, lead: leads ? "client" : "server" },
			tail,
			remaining
		};
	}
	/*
================
keepCastHold

A receipt for a walk sent before the hold confirms the server's path; the
player stays held and that path becomes the one to follow if the hold lapses.
================
	*/
	function keepCastHold() {
		if ( !castHold ) return;
		castHold.resume = segment;
		pose = castHold.pose;
		segment = null;
		walk = null;
	}
	/*
================
rejoinServerWalk

The cast hold ends without a settle: the server never stopped, so its walk
went on. Walk from the held pose to that walk's end; from here on the client
follows the server (WalkLead "server").

A refusal arrives one round trip after the press. The hold stopped a
client-led walk where the server stood when the command reached it; one
round trip later the server is one delivery further on, so the held player
is exactly the server-led replica of that walk. Resume at walking speed:
catching up instead would put the player ahead of where a later stop lands.

A hold that lapsed (no answer at all) waited longer than that. It catches
up, arriving when the walk it stopped would have, but never faster than
CATCHUP_SPEED_FACTOR, so it reads as walking and not as a slide or a jump.
================
	*/
	function rejoinServerWalk( now: number, refused: boolean ) {
		if ( !castHold ) return;
		const held = castHold.pose, resume = castHold.resume;
		castHold = null;
		beginTransition( "cast" );
		if ( !resume ) return;
		const remaining = poseDistance( held, resume.to ),
			arrival = resume.start + resume.duration,
			fastest = remaining / (speed * CATCHUP_SPEED_FACTOR) * 1000;
		pose = held;
		poseAtMs = now;
		segment = remaining ?
			bindOwners( {
				from: held,
				to: { ...resume.to, angle: movementHeading( held, resume.to ) },
				start: now,
				timing: "speed",
				lead: "server",
				duration: refused ? movementDuration( remaining, speed ) : Math.max( arrival - now, fastest )
			} ) :
			null;
	}
	/*
================
bindOwners
================
	*/
	function bindOwners( value: NonNullable<typeof segment> ) {
		const query: { slide: boolean; sourceOwner?: NavOwner; owners?: readonly NavOwnerSpan[]; status: number; } = {
			slide: false,
			sourceOwner: owner,
			status: 0
		};
		const resolved = navigation.clip( value.from, value.to, query );
		const admitted = !!resolved && !(query.status & (NAVIGATION_STOP | NAVIGATION_REJECT)) &&
			poseDistance( resolved, value.to ) < ENDPOINT_EPSILON;
		return {
			...value,
			admittedFrom: value.from,
			walking: undefined,
			presentationHistory: undefined,
			admitted,
			owners: admitted ? query.owners : undefined
		};
	}
	/*
================
directionSegment

The next intended leg of a direction walk. Actual elapsed steps acquire
navigation contacts in advanceTo; absent coverage pauses accepted movement.
The separate presentation corridor bounds cosmetic extrapolation.
================
	*/
	function directionSegment(
		from: Pose,
		heading: number,
		now: number,
		lead: WalkLead,
		factor = 1,
		predictOnly = false
	) {
		if ( predictOnly && !navigation.available( from ) ) return null;
		const to = directionLegEnd( from, heading );
		return bindOwners( {
			from,
			to: { ...to, angle: heading },
			start: now,
			timing: "speed" as const,
			lead,
			duration: movementDuration( poseDistance( from, to ), speed * factor ),
			direction: { heading, blocked: false }
		} );
	}

	/*
================
referenceAt
================
	*/
	function referenceAt( reference: DirectionReference, now: number ): Pose {
		const travelled = Math.min( reference.limit, Math.max( 0, now - reference.start ) * speed / 1000 );
		return directionPoint( reference.from, reference.heading, travelled );
	}
	/*
================
driftWalk

Every DRIFT_PERIOD_MS of a direction walk, re-aim the local walker toward
the server's walk (directionDrift). A changed aim or speed factor starts a
new leg from the live pose; the walk's own heading is kept for later legs.
================
	*/
	function driftWalk( now: number ) {
		if ( !walk?.reference || !segment?.direction || segment.direction.blocked || now < walk.nextDrift ) return;
		walk.nextDrift = now + DRIFT_PERIOD_MS;
		advanceTo( now );
		if ( !segment || !pose || !walk ) return;
		const local = pose;
		const drift = directionDrift( local, referenceAt( walk.reference, now ), walk.heading, speed );
		if ( drift.heading === segment.direction.heading && drift.factor === walk.factor ) return;
		walk.factor = drift.factor;
		owner = liveOwner( now );
		pose = navigation.surface( local, pose ?? local, owner, surfaceCursor );
		poseAtMs = now;
		segment = directionSegment( pose, drift.heading, now, segment.lead, drift.factor );
	}
	/*
================
advanceTo

Bring the rest pose to now along the walk in progress, stamped with the
time it was taken at. Every re-anchor (a click, a direction walk, a
receipt, a server walk, a correction, a cast) starts here. Starting from
the last stepped pose instead drops the time since that worker step: the
presentation extrapolates every sample along its velocity, so the
shortfall is drawn as a back-step of up to one step's travel, felt on
every high-latency acknowledgement and skill press.
================
	*/
	function advanceTo( now: number ) {
		if ( !pose || now < poseAtMs ) return;
		if ( segment ) {
			const elapsed = Math.max( 0, now - Math.max( poseAtMs, segment.start ) );
			if ( !elapsed ) return;
			if ( segment.fixedTiming ) {
				owner = liveOwner( now );
				pose = navigation.surface( sampleMovement( segment, now ), pose, owner, surfaceCursor );
				owner = surfaceCursor.owner ?? owner;
			} else {
				// Native walking consumes a finite elapsed step from the accepted
				// position. The client scalar step is uncapped; each accepted
				// query consumes elapsed time exactly once.
				if ( !segment.walking ) {
					const walkingSpeed = segment.duration > 0 ?
						poseDistance( segment.from, segment.to ) / segment.duration * 1000 :
						0;
					// Native destination vectors are stored floats, including a
					// destination copied from a higher-precision receipt envelope.
					segment.to = interpolate( segment.to, {
						...segment.to,
						x: Math.fround( segment.to.x ),
						y: Math.fround( segment.to.y ),
						z: Math.fround( segment.to.z )
					}, 1 );
					const direction = planarOffset( segment.from, segment.to );
					segment.walking = {
						direction: segment.direction ?
							clientWalkingVector( modelYaw( segment.direction.heading ) ) :
							clientWalkingDirection( direction ),
						speed: walkingSpeed
					};
				}
				// Native navigation stores its direction when the leg starts.
				// Re-normalizing rounded accepted positions changes the leg on every frame.
				const rate = segment.walking.speed / 1000;
				const delta = planarOffset( pose, segment.to );
				const { step, arrived } = clientWalkingStep(
					segment.walking.speed,
					elapsed / 1000,
					segment.walking.direction,
					segment.direction && !segment.direction.blocked ? Infinity : clientPlanarDistance( delta )
				);
				const desired = interpolate( pose, {
					...pose,
					x: Math.fround( Math.fround( pose.x ) + step[0] ),
					y: Math.fround( pose.y ),
					z: Math.fround( Math.fround( pose.z ) + step[1] ),
					angle: segment.to.angle
				}, 1 );
				const query: { slide: boolean; sourceOwner?: NavOwner; owner?: NavOwner; status: number; } = {
					slide: false,
					sourceOwner: owner,
					status: 0
				};
				const resolved = navigation.clip( pose, desired, query );
				if ( query.status & NAVIGATION_REJECT ) {
					// Native 86D6D0 retires a rejected move without accepting the
					// collision candidate, even when its coordinates are unchanged.
					segment = null;
					walk = null;
				} else if ( !resolved ) {
					// Intent is not proof of a traversable step. Keep the accepted
					// pose until coverage arrives; consume the elapsed time so a
					// late product cannot release a backlog through geometry.
					segment = {
						...segment,
						from: pose,
						start: now,
						duration: rate ? poseDistance( pose, segment.to ) / rate : Infinity,
						admitted: false
					};
				} else {
					segment.admittedFrom ??= pose;
					segment.history = extendWalkingHistory( {
						points: segment.history,
						from: pose,
						to: resolved,
						sourceOwner: owner,
						clip: navigation.clip
					} );
					segment.presentationHistory = undefined;
					pose = resolved;
					const walkingPath = { from: segment.admittedFrom, to: pose };
					transition = {
						...transition,
						walkingPath: segment.history,
						turn: transition.turn ? { ...transition.turn, outgoing: walkingPath } : undefined
					};
					owner = query.owner;
					surfaceCursor = {};
					const blocked = !!(query.status & NAVIGATION_STOP);
					if ( blocked || arrived ) {
						segment = null;
						walk = null;
					} else {
						segment = {
							...segment,
							from: pose,
							start: now,
							duration: rate ? poseDistance( pose, segment.to ) / rate : Infinity,
							owners: undefined
						};
					}
				}
			}
		}
		poseAtMs = now;
	}

	/*
	================
	presentationPath

	The goal is intent. Only a separately checked short corridor authorizes
	cosmetic extrapolation; a blocked future step is never simulated here.
	================
	*/
	function presentationPath() {
		if ( !segment || !pose ) return undefined;
		if ( segment.fixedTiming ) {
			return {
				from: segment.from,
				to: segment.to,
				durationMs: segment.duration,
				displacement: true,
				startedAtMs: segment.start
			};
		}
		const desired = sampleMovement( segment, segment.start + PRESENTATION_LOOKAHEAD_MS );
		const query = { slide: false, sourceOwner: owner, status: 0 };
		const resolved = navigation.clip( pose, desired, query );
		const admitted = !!resolved && !(query.status & (NAVIGATION_STOP | NAVIGATION_REJECT)) &&
			poseDistance( resolved, desired ) < ENDPOINT_EPSILON;
		segment.admitted = admitted;
		if ( admitted ) {
			segment.admittedTo = resolved!;
		}
		segment.admittedFrom ??= pose;
		// Keep the accepted past even when the next lookahead hits a wall.
		// Main-thread stalls and worker catch-up can omit every intermediate
		// publication; the displayed pose still belongs to this admitted walk.
		if ( segment.admittedFrom ) {
			const walkingPath = { from: segment.admittedFrom, to: admitted ? resolved! : pose };
			segment.presentationHistory ??= admitted ?
				extendWalkingHistory( {
					points: segment.history,
					from: pose,
					to: resolved!,
					sourceOwner: owner,
					clip: navigation.clip
				} ) :
				segment.history;
			transition = {
				...transition,
				walkingPath: segment.presentationHistory,
				turn: transition.turn ? { ...transition.turn, outgoing: walkingPath } : undefined
			};
		}
		return {
			from: pose,
			to: admitted ? resolved! : pose,
			durationMs: Math.min( PRESENTATION_LOOKAHEAD_MS, segment.duration )
		};
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
			advanceTo( now );
			authoritative = pose;
			segment = null;
			walk = null;
			acknowledged = nextId;
			pending.clear();
			error = null;
			life = "dead";
			beginTransition( "death", true );
		},
		/*
================
displace
================
		*/
		displace( command: import("@/engine/contracts/gameplay").CastDisplacement, now: number ) {
			if ( !pose ) return;
			advanceTo( now );
			beginTransition( "displacement", true );
			const from = pose,
				next = displacementSegment( from, command, now );
			// Acceptance replaces the held walk. Its timeout must never restore
			// the pre-displacement position after the dash or teleport arrives.
			castHold = null;
			// Displacement walks from the cell under the actor (native 8797C0
			// steps through navigation; the server's QueryMovement walks from the
			// stored source cell). Keep the owner: clearing it here re-guessed the
			// start surface from height and could drop a dash off a deck.
			// Requests already sent stay pending: the server still answers each
			// one after the displacement, and its receipt owns the outcome.
			surfaceCursor = {};
			walk = null;
			pose = authoritative = navigation.surface( next.from, from, owner );
			poseAtMs = now;
			segment = next.duration ?
				bindOwners( {
					...next,
					from: pose,
					timing: "server",
					lead: "server",
					fixedTiming: true,
					castToken: command.kind === 8 ? command.token : undefined
				} ) :
				null;
			return now + next.duration;
		},
		/*
================
holdForCast

A targeted skill or attack command makes the server settle its walk where
the command finds it (enterBasicAttackRange: SettleLive, then the B2F5
correction before the cast), or re-route from that point to approach. With
equal latency both ways that is where the local walk stands when the command
is sent. Walking on until the correction lands overshot by speed x RTT, and
the correction then pulled the player back, past MAX_CORRECTION_DISTANCE as
a snap. End the local walk here; receipts still update the authority, the
correction or a new move ends the hold, and a hold that lapses follows the
server's walk again.

That holds only for a walk the client leads (WalkLead). On a server-led walk
(a chase) the client stands one delivery behind the server: the server acts
on the command where the client will stand when the answer arrives, so the
walk goes on and the stop or the re-planned leg lands under it. Holding such
a walk froze it at every press of a skill spam and lost that walking time
for good: the next leg started from the held point while the server ran on,
and reaching range pulled the player 28 to 120 units forward at once
(production recording, 2026-10-04, 360 ms round trip).
================
		*/
		holdForCast( now: number ) {
			if ( !segment || !pose || segment.castToken !== undefined || segment.lead === "server" ) return;
			advanceTo( now );
			castHold = { pose, since: now, until: now + CAST_HOLD_MS, resume: segment };
			segment = null;
			walk = null;
			beginTransition( "cast" );
		},
		/*
================
castRefused

The server did not act on the command the walk was held for: it refused it
(B245 [2, code], or the B2CD action notice) or queued it behind its open
command (B2CD count 2). Either way it never touched movement (offensiveCost
runs at the press, 58D8F0; a queued command waits in 4AD630), so its walk
went on: rejoin it now rather than when the hold lapses.
================
		*/
		castRefused( now: number ) {
			rejoinServerWalk( now, true );
		},
		/*
================
cancelCast
================
		*/
		cancelCast( token: number, now: number ) {
			if ( segment?.castToken === token ) {
				advanceTo( now );
				authoritative = pose;
				poseAtMs = now;
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
			advanceTo( now );
			return pose;
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
			advanceTo( now );
			const picked = navigation.pick( query ),
				from = pose,
				to = positionSkillGoal( from, query, picked );
			const clipped = to ? navigation.clip( from, to, { slide: false, sourceOwner: liveOwner( now ) } ) : null;
			return clipped;
		},
		pick: navigation.pick,
		surface: navigation.surface,
		clipMovement: navigation.clip,
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
			advanceTo( now );
			mode = value;
			speed = value === 2 ? walkSpeed : runSpeed;
			if ( !segment ) return;
			// A displacement (knockback, dash) is action state 4/5, not the
			// navigation channel 858450 switches: its authored timing stands.
			if ( segment.fixedTiming && value !== 0 && value !== 4 ) return;
			const next = movementModeTransition( segment, value, speed, now );
			pose = navigation.surface( next.pose, pose ?? next.pose, owner );
			poseAtMs = now;
			// A server-led walk was re-timed by the server from its own live point.
			if ( segment.timing === "server" || !next.segment ) authoritative = pose;
			if ( !next.segment ) walk = null;
			segment = next.segment ? bindOwners( { ...segment, ...next.segment, from: pose } ) : null;
		},
		/*
================
speeds
================
		*/
		speeds( walk: number, run: number, now: number ) {
			if ( ![ walk, run ].every( validMovementSpeed ) ) {
				throw Error( "Invalid movement speed channels" );
			}
			advanceTo( now );
			const previous = speed;
			walkSpeed = walk;
			runSpeed = run;
			speed = mode === 2 ? walk : run;
			if ( segment && !segment.fixedTiming && speed !== previous ) {
				const next = movementSpeedTransition( segment, previous, speed, now );
				pose = navigation.surface( next.from, pose ?? next.from, owner );
				poseAtMs = now;
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
			advanceTo( now );
			const decoded = decodeNativeMovement( p, pose );
			if ( decoded.gid !== gid ) {
				return;
			}
			// A source-less angular acknowledgement leaves the path running.
			if ( decoded.kind === "keep" ) return;
			advanceTo( now );
			const current = pose;
			castHold = null;
			beginTransition( "native" );
			const adopt = predicted !== null && decoded.kind !== "direction" &&
				samePredictedGoal( predicted.to, decoded.to );
			predicted = null;
			authoritative = reconcile( decoded.from );
			if ( adopt ) {
				// The predicted run-up is the walk the server just started from
				// the same place: keep the progress made while the request was in
				// flight instead of stepping back to the server's start.
				walk = null;
				pose = current;
				segment = bindOwners( {
					from: current,
					to: decoded.to,
					start: now,
					timing: "speed",
					lead: "client",
					duration: movementDuration( poseDistance( current, decoded.to ), speed )
				} );
				return;
			}
			pose = authoritative;
			noteReanchor( "native move from its source", current, pose, { atMs: now, kind: decoded.kind } );
			if ( decoded.kind === "direction" ) {
				const heading = decoded.heading!;
				walk = {
					heading,
					factor: 1,
					nextDrift: now + DRIFT_PERIOD_MS,
					reference: { from: pose, start: now, heading, limit: Infinity }
				};
				segment = directionSegment( pose, heading, now, "server" );
				return;
			}
			walk = null;
			segment = bindOwners( {
				from: pose,
				to: decoded.to,
				start: now,
				timing: "speed",
				lead: "server",
				duration: movementDuration( poseDistance( pose, decoded.to ), speed )
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
			transition = { ...transition, walkingPath: undefined };
			surfaceCursor = {};
			owner = navigation.relocate( kept );
			if ( segment ) {
				segment = {
					...segment,
					owners: undefined,
					admitted: false,
					admittedFrom: undefined,
					admittedTo: undefined,
					history: undefined,
					presentationHistory: undefined
				};
			} // Spawn may precede collision admission. Stationary actors never enter
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
			minimapQueries = poses.map( value => admitPose( value ) );
		},
		/*
================
seed
================
		*/
		seed( value: Pose ) {
			beginTransition( "spawn", true );
			life = "alive";
			surfaceCursor = {};
			owner = undefined;
			acknowledged = nextId;
			pose = authoritative = navigation.surface( admitPose( value ) );
			poseAtMs = 0;
			segment = null;
			walk = null;
			predicted = null;
			castHold = null;
			pending.clear();
			error = null;
		},
		/*
================
correct
================
		*/
		correct( value: Pose, now?: number ) {
			// What the walk was doing when the correction came, for the report:
			// a large one names its cause (a hold the server never settled, a
			// walk of the wrong lead) instead of only its size.
			const context = {
				held: castHold && now !== undefined ? now - castHold.since : null,
				lead: segment?.lead ?? castHold?.resume?.lead ?? null,
				moving: !!segment
			};
			castHold = null;
			const wasDisplacing = segment?.fixedTiming;
			const publishedHistory = segment?.presentationHistory ?? transition.walkingPath;
			if ( now !== undefined ) advanceTo( now );
			// The display can already occupy the checked lookahead. Advance
			// retires that cache, so retain its proven return edges beforehand.
			const previousHistory = wasDisplacing || segment?.fixedTiming ?
				undefined :
				(pose && rewindWalkingHistory( publishedHistory, pose )) ?? segment?.history ?? transition.walkingPath;
			beginTransition( "correction" );
			const before = pose;
			// A live source correction ends motion, but is not a new spawn.
			// Resolve its surface through the existing navigation owner before
			// retiring the segment; server endpoint Y can be below a hill/deck.
			pose = authoritative = reconcile( admitPose( value ) );
			if ( before ) {
				noteReanchor( "server correction", before, pose, {
					atMs: now ?? poseAtMs,
					pending: pending.size,
					latest: nextId,
					...context
				} );
				retainCorrectionHistory( previousHistory, before, pose );
			}
			predicted = null;
			surfaceCursor = {};
			segment = null;
			walk = null;
			error = null;
			// The server processes requests in order and answers each one. A
			// request still pending was sent before this correction reached us
			// but runs after it (a click's cancel stop, then the click's walk),
			// so keep it and resume its walk from the corrected pose. Dropping
			// it marked the server's walk stale: the character froze locally
			// while the server walked it on.
			const latest = pending.get( nextId );
			if ( latest && latest.direction === undefined && now !== undefined ) {
				const query: { slide: boolean; sourceOwner?: NavOwner; owners?: readonly NavOwnerSpan[]; } = {
					slide: false,
					sourceOwner: owner
				};
				const clipped = navigation.available( pose ) ? latest.to : null;
				if ( clipped ) {
					segment = {
						from: pose,
						to: { ...clipped, angle: movementHeading( pose, clipped ) },
						start: now,
						timing: "speed",
						lead: "client",
						duration: movementDuration( poseDistance( pose, clipped ), speed ),
						owners: query.owners,
						admitted: true,
						history: transition.walkingPath
					};
				}
			}
		},
		/*
================
request
================
		*/
		request( value: Pose, now: number, cosGid?: number ) {
			if ( life === "dead" ) throw new Error( "Movement while dead" );
			castHold = null;
			if ( !pose || pending.size >= 32 || nextId === 0xffffffff ) {
				throw new Error( "Movement command capacity exceeded or player absent" );
			}
			const publishedHistory = segment?.presentationHistory;
			const incoming = segment?.admittedTo ?
				{
					from: segment.admittedFrom ?? segment.from,
					to: segment.admittedTo
				} :
				undefined;
			advanceTo( now );
			const incomingHistory = rewindWalkingHistory( publishedHistory, pose ) ?? segment?.history;
			const p = admitPose( value ),
				to = { ...p, x: Math.trunc( p.x ), y: Math.trunc( p.y ), z: Math.trunc( p.z ) },
				id = nextId + 1;
			if ( cosGid !== undefined && (!Number.isInteger( cosGid ) || cosGid < 1 || cosGid > 0xffffffff) ) {
				throw Error( "Invalid COS owner" );
			}
			const offset = cosGid === undefined ? 0 : 5,
				payload = new Uint8Array( 14 + offset ),
				v = new DataView( payload.buffer );
			payload[0] = cosGid === undefined ? ENVELOPE_PLAYER : ENVELOPE_COS;
			if ( cosGid !== undefined ) {
				v.setUint32( 5, cosGid, true );
				payload[9] = COS_MOVEMENT_TAG;
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
			const clipped = navigation.available( pose ) ? to : null;
			const frame = { opcode: OP_PREDICTED_MOVE, payload };
			send( frame );
			record( { kind: "movement-diagnostic", event: "request", simulationAtMs: now, requestId: id, target: to } );
			nextId = id;
			beginTransition( "input" );
			pending.set( id, { to, sent: now } );
			error = null;
			walk = null;
			predicted = null;
			if ( clipped ) {
				segment = {
					from: pose,
					to: { ...clipped, angle: movementHeading( pose, clipped ) },
					start: now,
					timing: "speed",
					lead: "client",
					duration: movementDuration( poseDistance( pose, clipped ), speed ),
					owners: query.owners,
					admitted: true,
					history: incomingHistory
				};
				if ( incoming ) {
					transition = {
						...transition,
						turn: { incoming, outgoing: { from: segment.from, to: segment.from } }
					};
				}
			}
			return frame;
		},
		/*
================
predictApproach

Start a walk the server drives (a pickup run-up: 0x72CD answered by a
movement ack toward the item) the moment it is requested, rather than one
round trip later. Only from rest or an acknowledged path, and only over
complete navigation coverage, like request. The acknowledgement adopts the
walk (native); a refusal ends it (endPrediction).
================
		*/
		predictApproach( value: Pose, now: number ): boolean {
			if ( !pose || life === "dead" || walk || pending.size || predicted ) return false;
			advanceTo( now );
			const current = pose,
				to = admitPose( value ),
				query: { slide: boolean; sourceOwner?: NavOwner; owners?: readonly NavOwnerSpan[]; } = {
					slide: false,
					sourceOwner: owner
				};
			const clipped = navigation.available( current ) ? to : null;
			if ( !clipped || poseDistance( clipped, to ) >= ENDPOINT_EPSILON ) return false;
			beginTransition( "input" );
			predicted = { from: current, to };
			pose = current;
			segment = {
				from: current,
				to: { ...clipped, angle: movementHeading( current, clipped ) },
				start: now,
				timing: "speed",
				lead: "client",
				duration: movementDuration( poseDistance( current, clipped ), speed ),
				owners: query.owners,
				admitted: true
			};
			return true;
		},
		/*
================
endPrediction

The server refused the walk it was predicted to drive: it never left the
start, so walk back there.
================
		*/
		endPrediction( now: number ) {
			if ( !predicted || !pose ) return;
			advanceTo( now );
			const from = predicted.from, current = pose;
			predicted = null;
			beginTransition( "cast" );
			pose = current;
			segment = bindOwners( {
				from: current,
				to: { ...from, angle: movementHeading( current, from ) },
				start: now,
				timing: "speed",
				lead: "server",
				duration: movementDuration( poseDistance( current, from ), speed )
			} );
		},
		/*
================
predicting
================
		*/
		predicting(): boolean {
			return predicted !== null;
		},
		/*
================
direct

The ground-pick miss of CGInterface_MoveToWorldPoint (0x6932A0): a 0x7738
mode-0 GO command along heading (or 0x769E tag 1 for the ridden vehicle),
and the local walk starts at once. Like request, the leg is predicted only
over complete navigation coverage; otherwise the receipt starts the walk.
================
		*/
		direct( heading: number, now: number, cosGid?: number ) {
			if ( life === "dead" ) throw new Error( "Movement while dead" );
			castHold = null;
			if ( !pose || pending.size >= 32 || nextId === 0xffffffff ) {
				throw new Error( "Movement command capacity exceeded or player absent" );
			}
			if ( cosGid !== undefined && (!Number.isInteger( cosGid ) || cosGid < 1 || cosGid > 0xffffffff) ) {
				throw Error( "Invalid COS owner" );
			}
			advanceTo( now );
			const body = directionMoveBody( heading ),
				id = nextId + 1,
				offset = cosGid === undefined ? 0 : 5,
				payload = new Uint8Array( 5 + offset + body.length ),
				v = new DataView( payload.buffer );
			payload[0] = cosGid === undefined ? ENVELOPE_PLAYER : ENVELOPE_COS;
			v.setUint32( 1, id, true );
			if ( cosGid !== undefined ) {
				v.setUint32( 5, cosGid, true );
				payload[9] = COS_MOVEMENT_TAG;
			}
			payload.set( body, 5 + offset );
			const frame = { opcode: OP_PREDICTED_MOVE, payload };
			send( frame );
			record( { kind: "movement-diagnostic", event: "request", simulationAtMs: now, requestId: id, heading } );
			nextId = id;
			beginTransition( "input" );
			const leg = directionSegment( pose, heading, now, "client", 1, true );
			pending.set( id, {
				to: leg?.to ?? directionLegEnd( pose, heading ),
				sent: now,
				direction: heading
			} );
			error = null;
			walk = { heading, factor: 1, nextDrift: now + DRIFT_PERIOD_MS, reference: null };
			if ( leg ) segment = leg;
			return frame;
		},
		/*
================
receive
================
		*/
		receive( payload: Uint8Array, now: number, expectedGid?: number ) {
			const r = decodeMovementReceipt( payload, expectedGid );
			const sent = pending.get( r.id )?.sent;
			const receiptEvent: import("@/engine/contracts/movement-diagnostic").MovementDiagnostic = {
				kind: "movement-diagnostic",
				event: "receipt",
				simulationAtMs: now,
				requestId: r.id,
				serverTimeMs: r.serverTimeMs,
				accepted: r.accepted,
				stale: r.id <= acknowledged,
				...(sent === undefined ? {} : { simulationRoundTripMs: Math.max( 0, now - sent ) })
			};
			if ( r.id <= acknowledged ) {
				record( receiptEvent );
				return;
			}
			const command = pending.get( r.id );
			if ( !command ) {
				throw new Error( "Unsolicited movement receipt" );
			}
			const world = receiptWorld( r ), to = world.spawn, s = world.segment;
			record( {
				...receiptEvent,
				serverGoal: to,
				...(s ? { serverFrom: s.from, serverDepartAtMs: s.startedAtMs, serverArriveAtMs: s.arrivesAtMs } : {})
			} );
			let replacement: typeof segment = null;
			if ( s ) {
				const from = s.from;
				const t = Math.max(
					0,
					Math.min( 1, (r.serverTimeMs - s.startedAtMs) / (s.arrivesAtMs - s.startedAtMs) )
				);
				replacement = {
					from: world.ground?.pose ?? interpolate( from, to, t ),
					to,
					start: now,
					timing: "server",
					lead: "server",
					duration: world.ground ?
						movementDuration(
							poseDistance( world.ground.pose, to ),
							world.ground.speed
						) :
						Math.max( 0, s.arrivesAtMs - r.serverTimeMs )
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
			const previousGoal = segment?.to;
			const previousTurn = transition.turn;
			const wasDisplacing = segment?.fixedTiming;
			const publishedHistory = segment?.presentationHistory ?? transition.walkingPath;
			advanceTo( now );
			const previousHistory = wasDisplacing || segment?.fixedTiming ?
				undefined :
				(pose && rewindWalkingHistory( publishedHistory, pose )) ?? segment?.history ?? transition.walkingPath;
			const previousPath = segment?.admittedFrom && pose ?
				{
					from: segment.admittedFrom,
					to: segment.admittedTo ?? pose
				} :
				previousHistory?.length ?
				{ from: previousHistory[0]!, to: previousHistory.at( -1 )! } :
				undefined;
			if ( command.direction !== undefined && r.accepted && walk ) {
				beginTransition( "receipt" );
				const before = pose;
				// The server walks from its live point; its first leg is the
				// reference the local walk reconciles toward. A leg shorter than
				// a full one ended on a contact and stops the reference there.
				const heading = command.direction, start = replacement?.from ?? to;
				const blocked = !s || directionLegBlocked( poseDistance( s.from, to ) );
				walk.reference = {
					from: start,
					start: now,
					heading,
					limit: blocked ? poseDistance( start, to ) : Infinity
				};
				const walkingOwner = owner;
				authoritative = reconcile( start );
				if ( segment?.direction && !blocked ) owner = walkingOwner;
				else {
					// Nothing was predicted: follow the server's leg.
					pose = authoritative;
					segment = replacement ?
						bindOwners( { ...replacement, from: pose, direction: { heading, blocked } } ) :
						null;
					if ( !replacement ) {
						pose = authoritative = { ...pose, angle: heading };
						walk = null;
					}
				}
				acknowledged = r.id;
				for ( const id of pending.keys() ) if ( id <= r.id ) pending.delete( id );
				error = null;
				keepCastHold();
				if ( before && pose ) {
					noteReanchor( "direction receipt", before, pose, { atMs: now, id: r.id } );
					retainCorrectionHistory( previousHistory, before, pose );
				}
				return;
			}
			if ( command.direction !== undefined ) walk = null;
			beginTransition( "receipt" );
			const predictedOwner = owner;
			authoritative = reconcile( replacement?.from ?? to );
			// A receipt confirms an endpoint, not the client's old frame. The
			// server's endpoint is the one that counts, and it is accepted from
			// wherever the prediction has walked to as long as that walk can
			// still reach it. Do not require the client's own clip to land on
			// the same point: the two clips run on separate navigation copies
			// and differ by fractions of a unit at walls (the client backs the
			// whole chord off a contact, the server only the crossed axis), and
			// the predicted pose may have crossed a region seam the server's
			// sample has not. Either mismatch put the player back where the
			// server stood at the receipt, one round trip of walking behind
			// (a ~19 unit snap at 190 ms on production). Server clipping,
			// rejection or a blocked route still wins.
			const predicted = pose;
			const reconciled: ReceiptReconciliation<NonNullable<typeof segment>> = replacement ?
				reconcileReceipt( r.accepted, predicted, predictedOwner, replacement, to ) :
				{ kind: "server", reason: r.accepted ? "no server walk" : "rejected" };
			if ( reconciled.kind === "keep" ) {
				pose = predicted!;
				owner = predictedOwner;
				replacement = reconciled.segment ?? null;
			} else if ( reconciled.kind === "settle" ) {
				// The prediction already passed the server's stop: settle on the
				// stop, never back at the server's start.
				pose = authoritative = reconcile( to );
				replacement = null;
			} else pose = authoritative;
			segment = replacement ? bindOwners( { ...replacement, from: pose } ) : null;
			const sameGoal = previousGoal && previousGoal.regionId === to.regionId &&
				previousGoal.x === to.x && previousGoal.z === to.z;
			if ( segment && reconciled.kind === "keep" && previousPath && sameGoal ) {
				segment.admittedFrom = previousPath.from;
			}
			if ( segment && reconciled.kind === "keep" ) segment.history = previousHistory;
			if ( !segment && reconciled.kind === "keep" ) transition = { ...transition, walkingPath: previousHistory };
			acknowledged = r.id;
			for ( const id of pending.keys() ) {
				if ( id <= r.id ) {
					pending.delete( id );
				}
			}
			error = r.accepted ? null : r.error ?? "Movement rejected";
			keepCastHold();
			if ( predicted ) {
				noteReanchor( "receipt " + reconciled.kind + ": " + reconciled.reason, predicted, pose!, {
					atMs: now,
					id: r.id,
					latest: nextId,
					ageMs: now - command.sent,
					serverTimeMs: r.serverTimeMs,
					accepted: r.accepted,
					tail: reconciled.tail,
					remaining: reconciled.remaining,
					to
				} );
				retainCorrectionHistory( previousHistory, predicted, pose! );
			}
			// The journal may coalesce the click and this receipt. Its original
			// admitted path then never reached presentation; the rebased path
			// alone cannot prove that the last drawn pose is still safe.
			if ( reconciled.kind === "keep" && transition.eligible && previousPath ) {
				transition = {
					...transition,
					previousPath,
					turn: sameGoal ? previousTurn : segment ?
						{ incoming: previousPath, outgoing: { from: segment.from, to: segment.from } } :
						undefined
				};
			}
		},
		/*
================
step
================
		*/
		step( now: number ) {
			const oldest = pending.entries().next().value;
			if ( oldest && now - oldest[1].sent > MOVEMENT_RECEIPT_TIMEOUT_MS ) {
				throw new Error(
					`Movement receipt timed out: command ${oldest[0]}, age ${
						now - oldest[1].sent
					} ms, pending ${pending.size}`,
					{ cause: MOVEMENT_RECEIPT_TIMEOUT }
				);
			}
			// No settle came: the server is still walking. Rejoin its path.
			if ( castHold && now >= castHold.until ) rejoinServerWalk( now, false );
			driftWalk( now );
			if ( !segment ) {
				return false;
			}
			advanceTo( now );
			if ( segment?.fixedTiming && now >= segment.start + segment.duration ) {
				// Authored displacement arrival is its action clock, independent
				// of ordinary navigation's scalar arrival and collision status.
				authoritative = pose;
				segment = null;
				walk = null;
			}
			const remaining = segment && pose ? planarOffset( pose, segment.to ) : undefined;
			if (
				segment?.direction && pose && remaining && segment.walking &&
				remaining[0] * segment.walking.direction[0] + remaining[1] * segment.walking.direction[1] <= 0
			) {
				authoritative = pose;
				const direction = segment.direction, lead = segment.lead;
				segment = null;
				if ( direction && !direction.blocked && walk ) {
					segment = directionSegment( pose, direction.heading, now, lead, walk.factor );
				} else if ( direction ) walk = null;
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
				movementPath: presentationPath(),
				movementRevision,
				movementTransition: { ...transition, pathEligible: segment?.admitted === true },
				movementDiagnostics: { total: reanchorReports, recent: recentReanchors.slice() },
				navigationFloor: navigation.floor( pose, owner ),
				minimapFloors: Object.fromEntries(
					minimapQueries.map( p => [ [ p.regionId, p.x, p.y, p.z ].join( ":" ), navigation.floor( p ) ] )
				),
				navigationRegion,
				navigationBlock: navigation.block( pose, owner ),
				navigationOwner: owner ? { ...owner } : undefined,
				moving: !!segment && segment.duration > 0,
				// The heading of the direction walk in progress, if any.
				directionWalk: walk ? walk.heading : undefined,
				pose,
				poseAtMs,
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
			beginTransition( "clear", true );
			life = "alive";
			acknowledged = nextId;
			minimapQueries = [];
			speed = runSpeed = 50;
			walkSpeed = 20;
			mode = 3;
			surfaceCursor = {};
			owner = undefined;
			pose = authoritative = null;
			poseAtMs = 0;
			segment = null;
			walk = null;
			predicted = null;
			castHold = null;
			pending.clear();
			error = null;
			navigation.clear();
			navigationRegion = undefined;
			navigationRequestId = undefined;
			navigationFailure = undefined;
		}
	};
}
