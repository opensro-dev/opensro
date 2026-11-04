/*
===========================================================================

portrait.ts - borrowed character resources for synchronous portraits

A HUD portrait shows a frozen pose (time 0), so an unchanged source returns
its retained draws instead of rebuilding actors, resolving the head and
preparing again. Native renders these portraits only when dirty (player
mini-info 6B748F tests +0x38C; quick-party 5BAE03 tests +0x3DC). The
inventory doll animates every frame (5929D0) and is never retained.

===========================================================================
*/
import type { WorldTexture } from "@/engine/contracts/texture";
import { viewProjection } from "@/engine/foundation/rendering/world-math";
import { previewYaw } from "@/engine/foundation/math/angles";
import type { CharacterActor, CharacterModel } from "@/engine/contracts/character";
import type { PortraitPart, PortraitSource } from "@/engine/contracts/portrait";
import { isCharacterAnimationExtension } from "@/engine/foundation/animation/character-render-plan";
import type { GeometryCommands, ImageCommands, GeometryDraw } from "../internal/gpu-contract";

const DEFAULT_PREVIEW_YAW = 0.100000001;
const PORTRAIT_PITCH = 0.2;
const PORTRAIT_DISTANCE = 6.5;
const PREVIEW_NEAR = 0.01;
const PREVIEW_FAR = 500000;
const DOLL_ASPECT = 176 / 318;
const EMPTY_DRAWS: readonly GeometryDraw[] = Object.freeze( [] );
// Worn glows whose start the doll remembers. A glow missing for a frame (the
// world's particle budget, a program still loading) keeps its start; past this
// many, the ones not shown this frame are forgotten.
const MAX_GLOW_BIRTHS = 64;
// Actor fields the HUD preview replaces; the rest decide what is drawn.
const OVERRIDDEN_ROOT = new Set( [
	"animationLod",
	"modelAnimation",
	"pose",
	"mountedOn",
	"attachment",
	"opacity",
	"layers",
	"time",
	"loop",
	"scale"
] );
const OVERRIDDEN_CHILD = new Set( [ "pose", "time", "opacity", "layers", "animationLod", "modelAnimation" ] );

/*
================
sameValue

Structural equality of plain actor data (numbers, strings, arrays, typed
arrays, plain objects). Snapshots are owned deep copies, so an in-place
edit of a source snapshot can never hide behind shared identity.
================
*/
function sameValue( a: unknown, b: unknown, skip?: ReadonlySet<string> ): boolean {
	if ( Object.is( a, b ) ) return true;
	if ( typeof a !== "object" || typeof b !== "object" || a === null || b === null ) return false;
	if ( ArrayBuffer.isView( a ) || ArrayBuffer.isView( b ) ) {
		if ( !ArrayBuffer.isView( a ) || !ArrayBuffer.isView( b ) || a.constructor !== b.constructor ) return false;
		const x = a as unknown as ArrayLike<number>, y = b as unknown as ArrayLike<number>;
		if ( x.length !== y.length ) return false;
		for ( let i = 0; i < x.length; i++ ) if ( !Object.is( x[i], y[i] ) ) return false;
		return true;
	}
	if ( Array.isArray( a ) !== Array.isArray( b ) ) return false;
	const x = a as Record<string, unknown>, y = b as Record<string, unknown>;
	let count = 0;
	for ( const key in x ) {
		if ( skip?.has( key ) || x[key] === undefined ) continue;
		count++;
		if ( !sameValue( x[key], y[key] ) ) return false;
	}
	for ( const key in y ) if ( !skip?.has( key ) && y[key] !== undefined ) count--;
	return count === 0;
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
		extendBorrowedAnimations( id: string, model: CharacterModel ): void;
		hasModel( id: string ): boolean;
		socket(
			actors: readonly CharacterActor[],
			gid: number,
			bone: string,
			offset: readonly [number, number, number]
		): { x: number; y: number; z: number; } | null;

		/*
		================
		takeDegradations

		The snapshot owner's non-fatal fault reports since the last take.
		================
		*/
		takeDegradations(): import("@/engine/contracts/network").Degradation[];

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
	let source: CharacterModel | null = null, identity: number | undefined;
	// The doll clock: seconds the doll has been drawn since its model or gid last
	// changed, and the frame time it last advanced to (undefined while at rest).
	let drawn = 0, drawnAt: number | undefined;
	// The doll clock at which each worn glow (by gid) first showed, and on which model.
	const glowBirths = new Map<number, { readonly model: string; readonly birth: number; }>();
	let borrowed: readonly PortraitPart[] = [];
	let empty = true;
	// The retained HUD result: owned copies of every input that decides the
	// draws, and the draws themselves. Cleared by anything that changes them
	// from outside the source (empty, doll use, invalidate, dispose).
	let retained: {
		parts: { model: CharacterModel; images: readonly WorldTexture[]; }[];
		actors: unknown[];
		geometry: GeometryCommands;
		images: ImageCommands;
		draws: readonly GeometryDraw[];
	} | null = null;
	/*
	================
	retainedFor

	The retained draws when every drawing input equals the retained copy.
	================
	*/
	function retainedFor( value: PortraitSource, geometry: GeometryCommands, images: ImageCommands ) {
		if ( !retained || retained.geometry !== geometry || retained.images !== images ) return null;
		const children = value.children ?? [];
		if ( retained.parts.length !== children.length + 1 ) return null;
		for ( let index = 0; index < retained.parts.length; index++ ) {
			const part = index === 0 ? value : children[index - 1]!, kept = retained.parts[index]!;
			if ( part.model !== kept.model || part.images.length !== kept.images.length ) return null;
			for ( let i = 0; i < part.images.length; i++ ) if ( part.images[i] !== kept.images[i] ) return null;
			if ( !sameValue( part.actor, retained.actors[index], index === 0 ? OVERRIDDEN_ROOT : OVERRIDDEN_CHILD ) ) {
				return null;
			}
		}
		return retained.draws;
	}
	/*
	================
	retain

	Keep owned copies; actor snapshots are updated in place by their owner.
	Data that cannot be cloned is simply not retained (the next frame prepares).
	================
	*/
	function retain(
		value: PortraitSource,
		geometry: GeometryCommands,
		images: ImageCommands,
		draws: readonly GeometryDraw[]
	) {
		const parts = [ value, ...(value.children ?? []) ];
		try {
			retained = {
				parts: parts.map( part => ({ model: part.model, images: [ ...part.images ] }) ),
				actors: parts.map( part => structuredClone( part.actor ) ),
				geometry,
				images,
				draws
			};
		} catch {
			retained = null;
		}
	}
	preview.retain( [] );
	return {
		prepare(
			value: PortraitSource | null,
			geometry: GeometryCommands,
			images: ImageCommands,
			frame: {
				readonly yaw?: number;
				readonly seconds?: number;
				readonly aspect?: number;
				readonly camera?: import("@/engine/contracts/scene").WorldCamera;
			} = {}
		) {
			const dollYaw = frame.yaw, seconds = frame.seconds ?? 0;
			if ( !value ) {
				retained = null;
				if ( empty ) return EMPTY_DRAWS;
				preview.actors( [] );
				source = null;
				identity = undefined;
				borrowed = [];
				// This preview is exclusive to this owner. Its synchronous full
				// pass retires the borrowed resources before empty frames can skip.
				// A failed retirement must be retried on the next frame.
				const draws = preview.prepare( geometry, images, 0 );
				empty = true;
				return draws;
			}
			if ( dollYaw === undefined ) {
				const kept = retainedFor( value, geometry, images );
				if ( kept ) return kept;
			} else retained = null;
			empty = false;
			const parts = [ value, ...(value.children ?? []) ];
			const changed = parts.length !== borrowed.length ||
				parts.some( ( part, index ) =>
					part.model !== borrowed[index]?.model || part.actor.model !== borrowed[index]?.actor.model
				);
			// Attached effects come and go every few frames in combat. Only a
			// change of the character's own model resets the borrowed set; a new
			// child borrows just its own model (residency drops the departed).
			const bodyChanged = !source || !isCharacterAnimationExtension( source, value.model ) ||
				value.actor.model !== borrowed[0]?.actor.model || parts.some( part => {
					const previous = borrowed.find( row => row.actor.model === part.actor.model );
					return previous && !isCharacterAnimationExtension( previous.model, part.model );
				} );
			if ( changed && !bodyChanged ) {
				for ( const part of parts ) {
					if ( !preview.hasModel( part.actor.model ) ) {
						preview.borrowModel( part.actor.model, part.model, part.images );
					} else {
						const previous = borrowed.find( row => row.actor.model === part.actor.model );
						if (
							previous && previous.model !== part.model &&
							isCharacterAnimationExtension( previous.model, part.model )
						) {
							preview.extendBorrowedAnimations( part.actor.model, part.model );
						}
					}
				}
				borrowed = parts;
				source = value.model;
			}
			if ( bodyChanged || identity !== value.actor.gid ) {
				drawn = 0;
				drawnAt = undefined;
				glowBirths.clear();
				identity = value.actor.gid;
				if ( bodyChanged ) {
					preview.actors( [] );
					preview.prepare( geometry, images, 0 );
					const models = new Set<string>();
					for ( const part of parts ) {
						if ( models.has( part.actor.model ) ) continue;
						preview.borrowModel( part.actor.model, part.model, part.images );
						models.add( part.actor.model );
					}
					borrowed = parts;
					source = value.model;
				}
			}
			// Only a drawn frame advances the doll clock. A frame without seconds (warm)
			// or after pause() resumes from the time it stopped, so reopening the
			// window never replays the closed interval through the effects.
			if ( dollYaw !== undefined && frame.seconds !== undefined ) {
				if ( drawnAt !== undefined ) drawn += Math.max( 0, seconds - drawnAt );
				drawnAt = seconds;
			}
			// A worn glow is a fresh attachment (ApplyWeaponGlow 8EA960) whose
			// effect starts at its own zero. Handed the doll's age instead, a glow
			// equipped late replayed that whole age in one frame. Wearing an item
			// again gives its glow new gids (model-emission.ts), so a gid that
			// skipped frames is the same glow and keeps its start.
			for ( const child of value.children ?? [] ) {
				if ( !child.effect ) continue;
				const known = glowBirths.get( child.actor.gid );
				if ( known?.model !== child.actor.model ) {
					glowBirths.set( child.actor.gid, { model: child.actor.model, birth: drawn } );
				}
			}
			if ( glowBirths.size > MAX_GLOW_BIRTHS ) {
				const shown = new Set( (value.children ?? []).map( child => child.actor.gid ) );
				for ( const gid of glowBirths.keys() ) if ( !shown.has( gid ) ) glowBirths.delete( gid );
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
				time: dollYaw === undefined ? 0 : drawn,
				loop: true,
				scale: 1
			};
			const actors = [
				actor,
				...(value.children ?? []).map( child => ({
					...child.actor,
					pose: actor.pose,
					time: Math.max( 0, actor.time - (glowBirths.get( child.actor.gid )?.birth ?? 0) ),
					opacity: 1,
					layers: undefined,
					animationLod: undefined,
					modelAnimation: undefined
				}) )
			];
			if ( dollYaw !== undefined ) {
				preview.actors( actors );
				return preview.prepare(
					geometry,
					images,
					0,
					viewProjection(
						frame.camera ?? {
							eye: [ 0, 9, -40 ],
							target: [ 0, 9, 0 ],
							fov: Math.PI / 6,
							near: PREVIEW_NEAR,
							far: PREVIEW_FAR
						},
						frame.aspect ?? DOLL_ASPECT
					),
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
			preview.actors( actors );
			const draws = preview.prepare(
				geometry,
				images,
				0,
				viewProjection( { eye, target, fov: Math.PI / 6, near: PREVIEW_NEAR, far: PREVIEW_FAR }, 1 ),
				true
			);
			retain( value, geometry, images, draws );
			return draws;
		},
		/*
		================
		warm

		Borrow and upload a model before its window first shows it, so opening
		that window (the inventory doll) costs no model preparation. A model
		already borrowed is left alone; the draws are discarded.
		================
		*/
		warm( value: PortraitSource | null, geometry: GeometryCommands, images: ImageCommands ) {
			if ( !value || source === value.model ) return;
			this.prepare( value, geometry, images, { yaw: DEFAULT_PREVIEW_YAW } );
		},
		/*
		================
		pause

		The doll was not drawn this frame. Its clock resumes from where it stopped.
		================
		*/
		pause() {
			drawnAt = undefined;
		},
		/*
		================
		takeDegradations

		The preview snapshot owner's held-clock reports since the last take.
		================
		*/
		takeDegradations() {
			return preview.takeDegradations();
		},
		/*
		================
		invalidate
		================
		*/
		invalidate() {
			retained = null;
			empty = false;
			preview.invalidate();
		},
		/*
		================
		dispose
		================
		*/
		dispose( geometry: GeometryCommands | null, images: ImageCommands | null ) {
			preview.dispose( geometry, images );
			retained = null;
			source = null;
			borrowed = [];
			empty = true;
			drawnAt = undefined;
		}
	};
}
