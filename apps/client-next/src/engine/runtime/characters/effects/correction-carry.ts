/*
===========================================================================

correction-carry.ts - a server correction carries the local cast's ground visuals

Port-only consequence of cast prediction (cast-prediction.ts). The original
starts a cast only on the server's answer (B245), which arrives in the same
burst as the walk stop that places the caster (0xB2F5), so a world-placed
cast visual (an AT_ONE static) lands where the server put the body. The
port predicts the cast at the press: its statics spawn where the client's
walk had run ahead, and the stop then corrects the body back along the path
while the statics stayed behind (BR-261113-1118-7667, a 21-unit correction).

So a correction of the local mover (its movement revision changes and the
logical position moves) carries the local caster's world-placed visuals of
a live cast by the same delta: the result is where the original draws them.
A displacement (knockback) and a teleport-sized jump carry nothing: native
leaves a static where it was cast when the caster is thrown or relocated.
An internal of effects.ts.

===========================================================================
*/
import type { MovementPath, MovementTransition, Pose } from "@/engine/contracts/gameplay";
import { MAX_SMOOTHED_CORRECTION } from "@/engine/foundation/gameplay/drawn-step";
import { displaceRegionXZ, sameCoordinateSpace, worldDelta } from "@/engine/foundation/world/world-space";

// A move shorter than this is float noise or a heading-only correction.
const MIN_CARRY_DISTANCE = 1e-3;

/*
================
LocalMovement

The local mover's logical state the carry reads each frame.
================
*/
export interface LocalMovement {
	readonly pose: Pose | null | undefined;
	readonly movementRevision?: number;
	readonly movementPath?: MovementPath;
	readonly movementTransition?: MovementTransition;
}

/*
================
createCorrectionCarry

Remembers the local mover's last revision and pose. step() answers the
world delta a correction moved it by this frame, or null.
================
*/
export function createCorrectionCarry() {
	let revision: number | undefined, last: Pose | undefined;
	return {
		/*
		================
		step
		================
		*/
		step( local: LocalMovement | null ): readonly [number, number, number] | null {
			const pose = local?.pose ?? undefined, previous = last, previousRevision = revision;
			revision = local?.movementRevision;
			last = pose ? { ...pose } : undefined;
			if ( !pose || !previous || previousRevision === undefined || revision === previousRevision ) return null;
			// Only the server placing the body back carries (movement.ts
			// beginTransition( "correction" )). A new click ("input"), a
			// receipt, the press's own cast stop ("cast") or a knockback also
			// bump the revision, and each would nudge a live static by one
			// frame of walking.
			if (
				local!.movementTransition?.reason !== "correction" || local!.movementPath?.displacement === true
			) return null;
			if ( !sameCoordinateSpace( previous.regionId, pose.regionId ) ) return null;
			const [dx, dz] = worldDelta( previous, pose ), dy = pose.y - previous.y;
			const distance = Math.hypot( dx, dy, dz );
			if ( distance < MIN_CARRY_DISTANCE || distance > MAX_SMOOTHED_CORRECTION ) return null;
			return [ dx, dy, dz ];
		},
		/*
		================
		reset
		================
		*/
		reset() {
			revision = undefined;
			last = undefined;
		}
	};
}

// A point in world space: a pose or an effect actor's pose.
type WorldPoint = { readonly regionId: number; readonly x: number; readonly y: number; readonly z: number; };

/*
================
predictedCast

The press's own prediction (cast-prediction.ts tokens are negative) or
the server cast that adopted one: the casts whose visuals spawned ahead
of the server's answer.
================
*/
export function predictedCast( cast: { readonly token: number; readonly predictedToken?: number; } ): boolean {
	return cast.token < 0 || cast.predictedToken !== undefined;
}

/*
================
carriedPose

A world-placed visual's pose moved by a correction's delta, across region
boundaries.
================
*/
export function carriedPose<T extends WorldPoint>( pose: T, delta: readonly [number, number, number] ): T {
	const moved = displaceRegionXZ( pose, delta[0], delta[2] );
	return { ...pose, regionId: moved.regionId, x: moved.x, y: pose.y + delta[1], z: moved.z };
}
