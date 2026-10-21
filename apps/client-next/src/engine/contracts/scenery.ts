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
}
export interface SceneryPresentation {
	readonly emitters: readonly SceneryEmitter[];
	readonly night: boolean;
}
