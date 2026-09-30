/*
===========================================================================

portrait.ts - borrowed character resources for synchronous portraits

===========================================================================
*/
import type { WorldTexture } from "@/engine/contracts/texture";
import { viewProjection } from "@/engine/foundation/rendering/world-math";
import { previewYaw } from "@/engine/foundation/math/angles";
import type { CharacterActor, CharacterModel } from "@/engine/contracts/character";
import type { GeometryCommands, ImageCommands, GeometryDraw } from "../internal/gpu-contract";

const DEFAULT_PREVIEW_YAW = 0.100000001;
const PORTRAIT_PITCH = 0.2;
const PORTRAIT_DISTANCE = 6.5;
const PREVIEW_NEAR = 0.01;
const PREVIEW_FAR = 500000;
const DOLL_ASPECT = 176 / 318;

/*
================
PortraitSource

Borrow immutable model and image resources; the world character owner retains their lifetime.
================
*/
export interface PortraitSource {
	readonly actor: CharacterActor;
	readonly model: CharacterModel;
	readonly images: readonly WorldTexture[];
}
// Synchronous GPU projection. Model/bitmap lifetime stays with world characters;
// this owner owns only its pose, preview geometry and texture uploads.

/*
================
createPortrait

Own preview pose and GPU resources while borrowing the original source textures.
================
*/
export function createPortrait(
	preview: {
		retain( ids: readonly string[] ): void;
		actors( actors: readonly CharacterActor[] ): void;

		/*
		================
		prepare

		Reset pose time when identity changes and project the actor through the authored preview camera.
		================
		*/
		prepare(
			geometry: GeometryCommands,
			images: ImageCommands,
			origin: number,
			view?: Float32Array,
			preview?: boolean
		): readonly GeometryDraw[];
		borrowModel( id: string, model: CharacterModel, images: readonly WorldTexture[] ): void;
		socket(
			actors: readonly CharacterActor[],
			gid: number,
			bone: string,
			offset: readonly [number, number, number]
		): { x: number; y: number; z: number; } | null;

		/*
		================
		invalidate

		Release device-bound preview handles while preserving the borrowed source.
		================
		*/
		invalidate(): void;

		/*
		================
		dispose

		Release preview GPU resources without closing the world owner's images.
		================
		*/
		dispose( geometry: GeometryCommands | null, images: ImageCommands | null ): void;
	}
) {
	let source: CharacterModel | null = null, started = 0, identity: number | undefined;
	preview.retain( [] );
	return {
		prepare(
			value: PortraitSource | null,
			geometry: GeometryCommands,
			images: ImageCommands,
			dollYaw?: number,
			seconds = 0
		) {
			if ( !value ) {
				preview.actors( [] );
				source = null;
				identity = undefined;
				return preview.prepare( geometry, images, 0 );
			}
			if ( source !== value.model || identity !== value.actor.gid ) {
				started = seconds;
				identity = value.actor.gid;
				if ( source !== value.model ) {
					preview.actors( [] );
					preview.prepare( geometry, images, 0 );
					preview.borrowModel( value.actor.model, value.model, value.images );
					source = value.model;
				}
			}
			const actor: CharacterActor = {
				...value.actor,
				animationLod: undefined,
				modelAnimation: undefined,
				pose: { regionId: 0, x: 0, y: 0, z: 0, yaw: previewYaw( dollYaw ?? DEFAULT_PREVIEW_YAW ) },
				mountedOn: undefined,
				attachment: undefined,
				opacity: 1,
				layers: undefined,
				clip: (dollYaw === undefined ? undefined : value.actor.previewClip) ??
					value.model.clips.find( c => c.name === "stand" )?.name ?? value.actor.clip,
				time: dollYaw === undefined ? 0 : Math.max( 0, seconds - started ),
				loop: true,
				scale: 1
			};
			if ( dollYaw !== undefined ) {
				preview.actors( [ actor ] );
				return preview.prepare(
					geometry,
					images,
					0,
					viewProjection( {
						eye: [ 0, 9, -40 ],
						target: [ 0, 9, 0 ],
						fov: Math.PI / 6,
						near: PREVIEW_NEAR,
						far: PREVIEW_FAR
					}, DOLL_ASPECT ),
					true
				);
			}
			const head = preview.socket( [ actor ], actor.gid, "Bip01 Head", [ 0, 1, 0 ] );
			if ( !head ) return [];
			const target = [ head.x, head.y, head.z ] as const,
				eye = [
					head.x,
					head.y + Math.sin( PORTRAIT_PITCH ) * PORTRAIT_DISTANCE,
					head.z - Math.cos( PORTRAIT_PITCH ) * PORTRAIT_DISTANCE
				] as const;
			preview.actors( [ actor ] );
			return preview.prepare(
				geometry,
				images,
				0,
				viewProjection( { eye, target, fov: Math.PI / 6, near: PREVIEW_NEAR, far: PREVIEW_FAR }, 1 ),
				true
			);
		},
		invalidate() {
			preview.invalidate();
		},
		dispose( geometry: GeometryCommands | null, images: ImageCommands | null ) {
			preview.dispose( geometry, images );
			source = null;
		}
	};
}
