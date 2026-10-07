/*
===========================================================================

presentation-finalize.ts - camera follow and final character visibility

Reads the completed actor map, including linked rides, before publication.
The shared output owner retains the camera target and fade between frames.

===========================================================================
*/
import type { CharacterActor } from "@/engine/contracts/character";
import type { EntityState } from "@/engine/contracts/world";
import type { GameplayState } from "@/engine/contracts/gameplay";
import type { Renderer, FrameWork } from "@/engine/contracts/runtime";
import type { DressCatalog, WornItem } from "@/engine/foundation/animation/equipment-appearance";
import type { SkillLookup } from "@/engine/foundation/ui/buff-viewer";
import type { Resource, PresentationOutput, PresentationDisappear } from "./internal/presentation-contract";
import { advanceCharacterFade } from "@/engine/foundation/animation/character-fade";
import { concealmentState, concealmentAlpha, seenAlpha } from "@/engine/foundation/gameplay/concealment";
import { partyMembers, partyPortraitGid } from "@/engine/foundation/ui/party-overlay";
import { radians } from "@/engine/foundation/math/angles";
import { CHARACTER_ACTORS } from "@/engine/foundation/animation/character-budget";
import { disappearActor } from "@/engine/foundation/animation/disappear";
import { hiddenSilkCos } from "@/engine/foundation/ui/name-visibility";
import type { ModelParticle } from "@/engine/foundation/animation/model-particles";
import type { AnimationParticleSet, createAnimationEmission } from "@/engine/foundation/animation/animation-emission";
import type { createModelEmission } from "@/engine/foundation/animation/model-emission";
import type { createSceneryEmission } from "@/engine/foundation/animation/scenery-emission";

/*
================
CameraOutput
================
*/
type CameraOutput = Pick<PresentationOutput, "cameraTarget" | "cameraFade">;

/*
================
CameraFrame
================
*/
interface CameraFrame {
	readonly local: EntityState | undefined;
	readonly gameplay: GameplayState | null;
	readonly next: Map<number, CharacterActor>;
	readonly entitiesByGid: ReadonlyMap<number, EntityState>;
}

/*
================
CameraCatalog
================
*/
interface CameraCatalog {
	readonly heights: ReadonlyMap<string, number>;
	readonly catalog: ReadonlyMap<number, Resource>;
}

/*
================
selectCameraTarget
================
*/
export function selectCameraTarget( frame: CameraFrame, output: CameraOutput, published: CameraCatalog ) {
	const { local, gameplay, next, entitiesByGid } = frame;
	const { heights, catalog } = published;
	output.cameraTarget = null;
	if ( local && gameplay?.pose ) {
		const height = heights.get( catalog.get( local.refObjId )?.codename ?? "" );
		const mount = local.mountedOn ? entitiesByGid.get( local.mountedOn ) : undefined;
		const mountHeight = mount ? heights.get( catalog.get( mount.refObjId )?.codename ?? "" ) : undefined;
		// A rider whose mount has not spawned (yet) is drawn alone; follow it.
		const riding = mount && mountHeight !== undefined && next.has( mount.gid ) ? mount : undefined;
		const rendered = next.get( riding?.gid ?? local.gid )?.pose;
		if ( rendered && height !== undefined ) {
			output.cameraTarget = {
				height,
				mounted: !!riding,
				// Actor yaw is pi minus the native yaw (characterHeadingYaw).
				yaw: Math.PI - rendered.yaw,
				pose: {
					regionId: rendered.regionId,
					x: rendered.x,
					y: riding ? Math.fround( Math.fround( rendered.y + mountHeight! ) - 13 ) : rendered.y,
					z: rendered.z,
					// The local mover drives the mount, so its heading is the mount's.
					angle: gameplay.pose.angle
				}
			};
		}
	}
}

/*
================
VisibilityFrame
================
*/
interface VisibilityFrame {
	readonly entities: readonly EntityState[];
	readonly next: Map<number, CharacterActor>;
	readonly local: EntityState | undefined;
	readonly gameplay: GameplayState | null;
	readonly seconds: number;
	readonly cameraPitch: number;
	readonly concealmentSkills: ( catalog: GameplayState["skillCatalog"] ) => SkillLookup;
	readonly entityLod: { distance( gid: number ): number; };
}

/*
================
applyCharacterVisibility

Keep body status, party concealment and camera fade in their original order.
================
*/
export function applyCharacterVisibility( frame: VisibilityFrame, output: CameraOutput ) {
	const { entities, next, local, gameplay, seconds, cameraPitch, concealmentSkills, entityLod } = frame;
	// 85EC00 sets body state 4's alpha 0x50 on the model and its mount,
	// but 85D890 runs on every update of every character except the
	// local player (CICUser/CICCos_OnUpdate) and restores 0xFF unless
	// the concealment rule hides it. Body 4 therefore stays only on the
	// local player's own model, and 6/7 follow concealment.ts.
	for ( const entity of entities ) {
		const actor = next.get( entity.gid );
		if ( actor?.opacity !== undefined ) next.set( entity.gid, { ...actor, opacity: undefined } );
	}
	if ( local?.appearanceState?.[2] === 4 ) {
		const actor = next.get( local.gid );
		if ( actor ) next.set( local.gid, { ...actor, opacity: seenAlpha() } );
	}
	if (
		entities.some( e => e.gid !== local?.gid && (e.appearanceState?.[2] === 6 || e.appearanceState?.[2] === 7) )
	) {
		const lookup = concealmentSkills( gameplay?.skillCatalog ),
			byGid = new Map<number, import("@/engine/foundation/gameplay/attached-effects").AttachedEffect[]>();
		for ( const effect of gameplay?.attachedEffects ?? [] ) {
			let list = byGid.get( effect.gid );
			if ( !list ) byGid.set( effect.gid, list = [] );
			list.push( effect );
		}
		const viewer = local ? byGid.get( local.gid ) ?? [] : [],
			party = new Set( gameplay?.social?.members.map( m => m.name ) ?? [] );
		for ( const entity of entities ) {
			const body = entity.appearanceState?.[2] ?? 0;
			if ( entity.gid === local?.gid || body !== 6 && body !== 7 ) continue;
			const state = concealmentState(
				body,
				byGid.get( entity.gid ) ?? [],
				viewer,
				entityLod.distance( entity.gid ),
				lookup
			);
			const alpha = concealmentAlpha( state, party.has( entity.name ) );
			const actor = next.get( entity.gid );
			if ( actor && alpha !== undefined ) next.set( entity.gid, { ...actor, opacity: alpha } );
		}
	}
	if ( local ) {
		if ( output.cameraFade?.gid !== local.gid ) {
			output.cameraFade = { gid: local.gid, time: seconds, mode: false, current: 255, start: 255, progress: 1 };
		}
		const hidden = cameraPitch < -0.8999999761581421,
			transition = output.cameraFade.mode !== hidden || output.cameraFade.progress < 1;
		const alpha = advanceCharacterFade(
			output.cameraFade,
			hidden,
			Math.max( 0, seconds - output.cameraFade.time )
		);
		output.cameraFade.time = seconds;
		// 866B90 applies camera interpolation first while it is live;
		// after it finishes, the body-4 branch restores alpha 0x50.
		const actor = next.get( local.gid );
		if ( actor && (transition || local.appearanceState?.[2] !== 4) ) {
			next.set( local.gid, { ...actor, opacity: alpha } );
		}
	} else output.cameraFade = null;
}

/*
================
DisappearingFrame
================
*/
interface DisappearingFrame {
	readonly disappearing: ReadonlyMap<number, PresentationDisappear>;
	readonly seconds: number;
	readonly next: Map<number, CharacterActor>;
	readonly animationDeltaMs: number;
	readonly resources: { duration( path: string, clip: string ): number; };
	readonly animationHolders: { actor: CharacterActor; sets: readonly AnimationParticleSet[]; }[];
	readonly particleHolders: { actor: CharacterActor; particles: readonly ModelParticle[]; }[];
}

/*
================
presentDisappearing
================
*/
export function presentDisappearing( frame: DisappearingFrame ) {
	const { disappearing, seconds, next, animationDeltaMs, resources, animationHolders, particleHolders } = frame;
	for ( const row of disappearing.values() ) {
		let actor = disappearActor( row, seconds );
		if ( actor && next.size < CHARACTER_ACTORS ) {
			if ( row.animation && actor.layers ) {
				const { resource, dispatch, selection } = row.animation;
				const ranges = dispatch.step(
					actor.layers,
					animationDeltaMs,
					name =>
						resource.animationStates?.[name]?.durationMs ??
							Math.round( resources.duration( resource.glb, name ) * 1000 )
				);
				actor = {
					...actor,
					modelAnimation: selection.step(
						ranges,
						resource.modifierBindings ?? [],
						resource.modifierSelectors ?? []
					)
				};
				if ( resource.animationParticles?.length ) {
					animationHolders.push( { actor, sets: resource.animationParticles } );
				}
			}
			next.set( actor.gid, actor );
			for ( const child of row.children ?? [] ) {
				if ( next.size >= CHARACTER_ACTORS ) break;
				const age = seconds - row.started;
				next.set( child.gid, {
					...child,
					time: child.time + age,
					layers: child.layers?.map( layer => ({ ...layer, time: layer.time + age }) )
				} );
			}
			if ( row.particles.length ) particleHolders.push( { actor, particles: row.particles } );
		}
	}
}

/*
================
EmissionFrame
================
*/
interface EmissionFrame {
	readonly entities: readonly EntityState[];
	readonly next: Map<number, CharacterActor>;
	readonly seconds: number;
	readonly hideSilkCos: boolean;
	readonly particleHolders: { actor: CharacterActor; particles: readonly ModelParticle[]; }[];
	readonly animationHolders: { actor: CharacterActor; sets: readonly AnimationParticleSet[]; }[];
	readonly frameWork: FrameWork | undefined;
}

/*
================
EmissionBindings
================
*/
interface EmissionBindings {
	readonly resources: { ready( path: string ): boolean; duration( path: string, clip: string ): number; };
	readonly renderer: Renderer;
	readonly modelEmission: ReturnType<typeof createModelEmission>;
	readonly animationEmission: ReturnType<typeof createAnimationEmission>;
	readonly scenery: ReturnType<typeof createSceneryEmission>;
	readonly entityLod: { fraction( gid: number ): number; };
	readonly applySpawnFades: (
		entities: readonly EntityState[],
		next: Map<number, CharacterActor>,
		seconds: number
	) => void;
}

/*
================
presentEmission

Opacity and holder replacement precede particle publication, as in the parent.
================
*/
export function presentEmission( frame: EmissionFrame, bindings: EmissionBindings ) {
	const { entities, next, seconds, hideSilkCos, particleHolders, animationHolders, frameWork } = frame;
	const { resources, renderer, modelEmission, animationEmission, scenery, entityLod, applySpawnFades } = bindings;
	for ( const entity of entities ) {
		if ( hiddenSilkCos( entity, hideSilkCos ) ) {
			const actor = next.get( entity.gid );
			if ( actor ) next.set( entity.gid, { ...actor, opacity: 0 } );
		}
	}
	applySpawnFades( entities, next, seconds );
	for ( const holder of particleHolders ) holder.actor = next.get( holder.actor.gid )!;
	for (
		const actor of modelEmission.step(
			particleHolders,
			seconds,
			resources.ready,
			CHARACTER_ACTORS - next.size,
			gid => frameWork?.level() && next.get( gid )?.animationLod?.optional ? 1 : entityLod.fraction( gid )
		)
	) next.set( actor.gid, actor );
	for ( const holder of animationHolders ) holder.actor = next.get( holder.actor.gid )!;
	for (
		const actor of animationEmission.step(
			animationHolders,
			seconds,
			resources.ready,
			CHARACTER_ACTORS - next.size,
			renderer.presentationNight?.() ?? true,
			( gid, actor ) => {
				if ( !actor ) return renderer.characterParticleSnapshot( gid );
				const matrix = renderer.characterMatrix( [ ...next.values(), actor ], gid );
				return matrix ? { matrix, regionId: actor.pose.regionId } : null;
			},
			gid => renderer.characterParticleTime?.( gid ),
			path => resources.duration( path, "effect" )
		)
	) next.set( actor.gid, actor );
	for (
		const actor of scenery.step(
			renderer.scenery?.() ?? null,
			seconds,
			resources.ready,
			CHARACTER_ACTORS - next.size
		)
	) next.set( actor.gid, actor );
}

/*
================
PortraitFrame
================
*/
interface PortraitFrame {
	readonly local: EntityState | undefined;
	readonly gameplay: GameplayState | null;
	readonly next: Map<number, CharacterActor>;
	readonly seconds: number;
	readonly blindHeld: boolean;
}

/*
================
PortraitCatalog
================
*/
interface PortraitCatalog {
	readonly catalog: ReadonlyMap<number, Resource>;
	readonly dress: DressCatalog;
	readonly manifest: number;
}

/*
================
PortraitResources
================
*/
interface PortraitResources {
	ready( path: string ): boolean;
	plan( paths: readonly string[] ): boolean;
	retainWanted( paths: readonly string[] ): void;
}

/*
================
PortraitBindings

Borrow only the mall mannequin operations this publication phase consumes.
================
*/
interface PortraitBindings {
	readonly renderer: Pick<Renderer, "setCharacterAssembly" | "setCharacterActors">;
	readonly resources: PortraitResources;
	readonly mallPreview: {
		skin(): { model: number; shape: number; } | null;
		step(
			frame: {
				resource: Resource;
				dress: DressCatalog;
				equipment: readonly WornItem[];
				avatars: readonly { readonly refObjId: number; }[];
				seconds: number;
				source?: CharacterActor;
				shape?: number;
			},
			resources: Pick<PortraitResources, "ready" | "plan">,
			renderer: Pick<Renderer, "setCharacterAssembly">
		): readonly CharacterActor[];
	};
}

/*
================
publishCharacters

Portraits share admission but do not enter the world actor map.
================
*/
export function publishCharacters(
	frame: PortraitFrame,
	output: Pick<PresentationOutput, "displayed">,
	published: PortraitCatalog,
	bindings: PortraitBindings
) {
	const { local, gameplay, next, seconds, blindHeld } = frame;
	const { catalog, dress, manifest } = published;
	const { renderer, resources, mallPreview } = bindings;
	// 5BAF70 -> 5B9DF0 builds a slot-owned preview from the roster model.
	// It remains admitted even when no world entity exists for that member.
	const portraits: CharacterActor[] = [];
	const skin = mallPreview.skin(), localResource = local && catalog.get( local.refObjId );
	const mallResource = local && catalog.get( skin?.model ?? local.refObjId );
	if ( mallResource && localResource && local && gameplay && manifest >= 3 ) {
		// A body of the other sex cannot wear the worn set; 4EFE50 refuses
		// that change until the armour and avatars are off anyway.
		const worn = mallResource.codename.includes( "_WOMAN_" ) ===
			localResource.codename.includes( "_WOMAN_" );
		portraits.push( ...mallPreview.step(
			{
				resource: mallResource,
				dress,
				equipment: worn ? gameplay.inventory : [],
				avatars: worn ? local.avatars ?? [] : [],
				seconds,
				source: next.get( local.gid ),
				shape: skin?.shape
			},
			resources,
			renderer
		) );
	}
	for ( const member of gameplay ? partyMembers( gameplay ) : [] ) {
		const resource = catalog.get( member.model );
		if ( !resource || !resources.ready( resource.glb ) ) continue;
		portraits.push( {
			gid: partyPortraitGid( member.id ),
			model: resource.glb,
			pose: { regionId: 0, x: 0, y: 0, z: 0, yaw: radians( 0 ) },
			clip: "stand",
			time: 0,
			loop: true,
			scale: 1
		} );
	}
	output.displayed = next;
	// One pass builds both the published actors and the wanted models, in the
	// same order the spread-and-map version produced, without temporaries.
	const presentedActors: CharacterActor[] = [], wanted: string[] = [];
	for ( const actor of next.values() ) {
		presentedActors.push(
			blindHeld && actor.blindable ? { ...actor, opacity: 0, pickable: false } : actor
		);
		wanted.push( actor.model );
	}
	for ( const actor of portraits ) wanted.push( actor.model );
	renderer.setCharacterActors( presentedActors, portraits );
	resources.retainWanted( wanted );
}
