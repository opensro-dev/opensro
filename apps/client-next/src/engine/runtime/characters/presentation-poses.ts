/*
===========================================================================

presentation-poses.ts - the frame's drawn poses, resolved once

Unreal ticks movement before attached components and particles read the
body, so nothing attached ever reads last frame's body. This stage is
that tick for the port's presenter: after the action phase (the
navigation-hold decision reads its waiting and active casts) and before
presentation-events and actor-motion, every presented character's drawn
pose is resolved exactly once - the presenter pose and the navigation
hold's release, rebind or carry - and published as a map. Effects,
sockets, damage anchors and the actors themselves then all read the
same pose. The pose's angle stays the presenter's; a caster's tracked
yaw (8DC440 castFacing) is applied by actor-motion on top, as it
already overrides the angle of a held pose.

===========================================================================
*/
import { STALL_TITLE_MODE } from "@/engine/foundation/gameplay/interaction-approach";
import type { CastState, GameplayState, Pose, VitalState } from "@/engine/contracts/gameplay";
import type { CharacterLayer } from "@/engine/contracts/character";
import type { EntityState } from "@/engine/contracts/world";
import type { CharacterPresentationState } from "./internal/presentation-contract";

/*
================
DrawnPosesFrame

Everything the resolution reads: the presented set and the frame's
indexes, the per-actor presentation states (their navigation holds are
resolved, and released or rebound, here), and the action phase's
waiting and active casts.
================
*/
export interface DrawnPosesFrame {
	readonly selected: readonly EntityState[];
	readonly gameplay: GameplayState | null;
	readonly seconds: number;
	readonly logicalPose: ( entity: EntityState ) => Pose;
	readonly localMover: ( gid: number ) => boolean;
	readonly states: Map<number, CharacterPresentationState>;
	readonly castByActor: ReadonlyMap<number, CastState>;
	readonly actionLayersByActor: ReadonlyMap<number, CharacterLayer[]>;
	readonly waitingActors: ReadonlySet<number>;
	readonly vitalsByGid: ReadonlyMap<number, VitalState>;
	readonly health: { dead( gid: number ): boolean; } | undefined;
	// The action phase's pending deaths: a death whose hit lands in this
	// frame's events still glides this frame and settles the next;
	// presentation-events recomputes the set after its hits for
	// actor-motion's own death state.
	readonly pendingDeaths: ReadonlySet<number>;
	readonly statusOwner: {
		view( gid: number, mask: number, bodyVisual: number ): { readonly poseLocked: boolean; };
	};
	readonly posePresentation: {
		pose( gid: number, native: Pose, seconds: number, settledTranslation: boolean ): Pose;
		carry( gid: number, pose: Pose ): void;
	};
}

/*
================
resolveDrawnPoses

One drawn pose per presented character, presenter first: the sampled
pose at the frame clock, then the navigation hold over it. A hold the
walk left behind releases through the presenter's carry, so the body
recovers from the held pose at walking speed instead of jumping.
================
*/
export function resolveDrawnPoses( frame: DrawnPosesFrame ): Map<number, Pose> {
	const {
		selected,
		gameplay,
		seconds,
		logicalPose,
		localMover,
		states,
		castByActor,
		actionLayersByActor,
		waitingActors,
		vitalsByGid,
		health,
		pendingDeaths,
		statusOwner,
		posePresentation
	} = frame;
	const drawn = new Map<number, Pose>();
	for ( const entity of selected ) {
		const nativePose = logicalPose( entity );
		const authorityDead = entity.appearanceState?.[0] !== undefined ?
			entity.appearanceState[0] === 2 :
			vitalsByGid.get( entity.gid )?.hp === 0;
		// Wire death retires targeting immediately; the native result
		// vector starts death presentation only when its hit is applied.
		const dead = (authorityDead || !!health?.dead( entity.gid )) && !pendingDeaths.has( entity.gid );
		let renderPose = posePresentation.pose(
			entity.gid,
			nativePose,
			seconds,
			dead && entity.moving === false
		);
		const state = states.get( entity.gid );
		// 8DD550 drives skill travel through 8797C0 and 86D5C0 even
		// after state 9 exits. Its position owner is not the walk hold.
		if ( state?.navigationHold ) {
			const local = localMover( entity.gid );
			const movementRevision = (local ? gameplay?.movementRevision : entity.movementRevision) ?? 0;
			const displaced = (local ? gameplay?.movementPath : entity.movementPath)?.displacement === true ||
				(local ? gameplay?.movementTransition : entity.movementTransition)?.reason === "displacement";
			if ( displaced || dead || entity.mountedOn ) {
				state.navigationHold = undefined;
			} else if ( state.navigationHold.revision !== movementRevision ) {
				const cast = castByActor.get( entity.gid );
				const waiting = !dead && waitingActors.has( entity.gid );
				const activeCast = !!cast && cast.cancelledAtMs === undefined &&
					(actionLayersByActor.get( entity.gid )?.length ?? 0) > 0;
				let requestedMoving = (local ? gameplay?.moving : entity.moving) ?? false;
				const statusMask = vitalsByGid.get( entity.gid )?.abnormal ?? 0;
				const poseFrozen = statusOwner.view(
					entity.gid,
					statusMask,
					entity.appearanceState?.[2] ?? 0
				).poseLocked;
				const stall = !dead && entity.appearanceState?.[6] === STALL_TITLE_MODE;
				if ( poseFrozen || stall ) requestedMoving = false;
				const hold = state.navigationHold.logical;
				const samePoint = hold.regionId === nativePose.regionId && hold.x === nativePose.x &&
					hold.y === nativePose.y && hold.z === nativePose.z;
				// A stationary heading correction (0xB2F5 before the next cast in
				// a chain) bumps movementRevision without moving the settled
				// server position. Rebind the hold so the body does not decay to
				// the server pose and leave ground cast VFX behind at the hold.
				if ( !requestedMoving && (waiting || activeCast || samePoint) ) {
					state.navigationHold.revision = movementRevision;
					state.navigationHold.logical = { ...nativePose };
				} else {
					// Native stops the walk at WAIT and has one pose; the port's logical
					// walk may have moved on under the hold. A new walk releases it from
					// the held pose at walking speed (posePresentation.carry): jumping
					// there in one frame read as the camera height snapping on a slope.
					// Skill travel, death and a ride own the position themselves.
					posePresentation.carry( entity.gid, state.navigationHold.pose );
					renderPose = posePresentation.pose( entity.gid, nativePose, seconds, false );
					state.navigationHold = undefined;
				}
			}
		}
		if ( state?.navigationHold ) {
			// The hold freezes XYZ; the yaw keeps tracking the cast
			// target in actor-motion (8DC440 castFacing overrides it).
			renderPose = { ...state.navigationHold.pose, angle: renderPose.angle };
		}
		drawn.set( entity.gid, renderPose );
	}
	return drawn;
}
