import type { CharacterActor } from "./character";
/** One authored emitter per placement/compound branch/entry, never per material. */
export interface SceneryEmitter {
	readonly id: string;
	readonly placement: string;
	readonly model: string;
	readonly pose: CharacterActor["pose"];
	readonly basis: NonNullable<CharacterActor["effectBasis"]>;
	readonly nightOnly: boolean;
	readonly renderPriority: number;
	/** The dungeon room (DOF block) the placement belongs to; absent outdoors. */
	readonly dungeonBlock?: number;
	/** The bounding sphere of the compound the emitter is attached to, in the
	 * scene frame (centre xyz, radius): the emitter is in view while this is,
	 * as the original ticks attached objects on its compound's visible path
	 * (A9EFB0). */
	readonly holder?: readonly [number, number, number, number];
}
export interface SceneryPresentation {
	readonly emitters: readonly SceneryEmitter[];
	readonly night: boolean;
	/** Whether an emitter's holder met the last view frustum; absent where no
	 * view applies (a preview scene), which keeps every emitter running. */
	readonly holderVisible?: ( emitter: SceneryEmitter ) => boolean;
}
