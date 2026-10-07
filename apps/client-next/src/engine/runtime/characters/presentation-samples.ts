/*
===========================================================================

presentation-samples.ts - logical mover selection and frame-clock samples

Builds the same movement inputs for local characters, mounts and remote
entities before any presentation phase reads their poses. The caller owns
sample publication; interpolation remains in pose-presentation.ts.

===========================================================================
*/
import type { EntityState } from "@/engine/contracts/world";
import type { GameplayState } from "@/engine/contracts/gameplay";

/*
================
createPresentationSamples
================
*/
export function createPresentationSamples( entities: readonly EntityState[], gameplay: GameplayState | null ) {
	// CCharactor_GetActiveMoverEntity (0x85E000): while the local player
	// rides, its movement owner moves the mount and the rider sits on it.
	// The mount takes the local pose, samples and movement state; the
	// server keeps one shared mover for both (cosride.go).
	const localGid = gameplay?.pose ? gameplay.localGid : undefined;
	const localMount = localGid === undefined ?
		undefined :
		entities.find( entity => entity.gid === localGid )?.mountedOn;
	/*
	================
	localMover
	================
	*/
	const localMover = ( gid: number ) => !!gameplay?.pose && (gid === gameplay.localGid || gid === localMount);
	/*
	================
	logicalPose

	The worker's latest pose of a character: the local movement owner's for
	the local player (its entity row can still hold the spawn position),
	else the entity row. Presentation draws from it via posePresentation.
	================
	*/
	const logicalPose = ( entity: EntityState ): import("@/engine/contracts/gameplay").Pose =>
		localMover( entity.gid ) ?
			gameplay!.pose! :
			{ regionId: entity.regionId, x: entity.x, y: entity.y, z: entity.z, angle: entity.heading };
	// Timed samples draw on the frame clock: the local player from its
	// movement owner, every other character from its stepped path.
	// Entity rows and the local movement state publish the same four fields.
	/*
	================
	Sampled
	================
	*/
	type Sampled = Pick<
		EntityState,
		"poseAtMs" | "moving" | "movementPath" | "movementRevision" | "movementTransition"
	>;
	const samples = new Map<number, import("@/engine/contracts/pose-presentation").SampleInput>();
	/*
	================
	sample
	================
	*/
	const sample = ( gid: number, source: Sampled ) => {
		if ( source.poseAtMs === undefined ) return;
		samples.set( gid, {
			atMs: source.poseAtMs,
			revision: source.movementRevision ?? 0,
			moving: !!source.moving && source.movementTransition?.pathEligible !== false,
			transition: source.movementTransition,
			...(source.movementPath ?
				{
					from: source.movementTransition?.pathEligible === false ?
						undefined :
						source.movementPath.from,
					to: source.movementPath.to,
					durationMs: source.movementPath.durationMs,
					...(source.movementPath.displacement ?
						{ startedAtMs: source.movementPath.startedAtMs, displacement: true } :
						{})
				} :
				{})
		} );
	};
	for ( const entity of entities ) if ( !localMover( entity.gid ) ) sample( entity.gid, entity );
	if ( gameplay?.pose ) sample( gameplay.localGid, gameplay );
	if ( gameplay?.pose && localMount !== undefined ) sample( localMount, gameplay );
	return { localMover, logicalPose, samples };
}
