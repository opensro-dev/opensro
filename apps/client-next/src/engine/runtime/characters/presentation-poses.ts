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
import type { GameplayState, MovementPath, Pose, VitalState } from "@/engine/contracts/gameplay";
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
/*
================
holdSurvivesRevision

Whether a navigation hold outlives a new movement revision: only a
stationary one, the heading-only 0xB2F5 a chained cast sends at the
settled server point (#1233). A correction that moves the actor ends
the hold, as the native B2F5 handler (775B50) writes the wire position
to the mover: absorbing it while a cast animated let a unique that
casts back to back (the Tiger Girl, BR-261127-0952-B0CA) stand at its
old drawn spot, unturned, then jump metres when the hold finally ended.
================
*/
export function holdSurvivesRevision( hold: Pose, pose: Pose, moving: boolean ): boolean {
	return !moving && hold.regionId === pose.regionId && hold.x === pose.x && hold.y === pose.y &&
		hold.z === pose.z;
}

export interface DrawnPosesFrame {
	readonly selected: readonly EntityState[];
	readonly gameplay: GameplayState | null;
	readonly seconds: number;
	readonly logicalPose: ( entity: EntityState ) => Pose;
	readonly localMover: ( gid: number ) => boolean;
	readonly states: Map<number, CharacterPresentationState>;
	readonly vitalsByGid: ReadonlyMap<number, VitalState>;
	readonly health: { dead( gid: number ): boolean; } | undefined;
	// The action phase's pending deaths: a death whose hit lands in this
	// frame's events still glides this frame and settles the next;
	// presentation-events recomputes the set after its hits for
	// actor-motion's own death state.
	readonly pendingDeaths: ReadonlySet<number>;
	// The targets whose knockback hit has not landed yet, and the owner that
	// holds them there and plays their recoil from the hit (8D5440).
	readonly pendingKnockbacks: ReadonlySet<number>;
	readonly knockbacks: {
		pose(
			gid: number,
			path: MovementPath | undefined,
			pending: boolean,
			seconds: number,
			drawn: Pose
		): Pose | undefined;
	};
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
		vitalsByGid,
		health,
		pendingDeaths,
		pendingKnockbacks,
		knockbacks,
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
				let requestedMoving = (local ? gameplay?.moving : entity.moving) ?? false;
				const statusMask = vitalsByGid.get( entity.gid )?.abnormal ?? 0;
				const poseFrozen = statusOwner.view(
					entity.gid,
					statusMask,
					entity.appearanceState?.[2] ?? 0
				).poseLocked;
				const stall = !dead && entity.appearanceState?.[6] === STALL_TITLE_MODE;
				if ( poseFrozen || stall ) requestedMoving = false;
				// A stationary heading correction (0xB2F5 before the next cast in
				// a chain) bumps movementRevision without moving the settled
				// server position. Rebind the hold so the body does not decay to
				// the server pose and leave ground cast VFX behind at the hold.
				if ( holdSurvivesRevision( state.navigationHold.logical, nativePose, requestedMoving ) ) {
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
		// A struck character's recoil starts when its hit lands, not when
		// the result arrived (knockback-presentation.ts). The local player
		// keeps the main-thread mover's own displacement.
		if ( !localMover( entity.gid ) ) {
			renderPose = knockbacks.pose(
				entity.gid,
				entity.movementPath,
				pendingKnockbacks.has( entity.gid ),
				seconds,
				renderPose
			) ?? renderPose;
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
