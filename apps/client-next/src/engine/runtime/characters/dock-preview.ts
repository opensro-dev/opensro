/*
===========================================================================

dock-preview.ts - roster and character creation presentation lifetime

Owns committed preview outfits, body shape and deletion transitions. Shared
presentation outputs stay with the frame owner; world actors never enter here.

===========================================================================
*/
import type { CharacterActor } from "@/engine/contracts/character";
import type { CreationSnapshot } from "@/engine/contracts/frontend";
import type { Renderer } from "@/engine/contracts/runtime";
import type { CharacterRecord } from "@/engine/contracts/session";
import {
	assembleEquipmentAppearance,
	wornItemsFromList,
	type DressCatalog
} from "@/engine/foundation/animation/equipment-appearance";
import { defaultWearFrozen } from "@/engine/foundation/animation/default-wear-policy";
import { advanceBodyShape, bodyVolumeIndex, type BodyShapeBlend } from "@/engine/foundation/animation/body-shape";
import { oneShotLayers } from "@/engine/foundation/animation/one-shot-layers";
import { previewIdle } from "@/engine/foundation/animation/preview-idle";
import type { createSceneryEmission } from "@/engine/foundation/animation/scenery-emission";
import { CHARACTER_ACTORS } from "@/engine/foundation/animation/character-budget";
import { radians, previewYaw } from "@/engine/foundation/math/angles";
import { dockSlot } from "@/engine/foundation/rendering/dock-slots";
import { creationLoadout, creationRange } from "@/engine/foundation/ui/character-create";
import type { Resource, PresentationOutput } from "./internal/presentation-contract";

/*
================
DockPreviewOwners
================
*/
interface DockPreviewOwners {
	readonly renderer: Renderer;
	readonly resources: {
		plan( paths: readonly string[] ): void;
		ready( path: string ): boolean;
		duration( path: string, clip: string ): number;
		retainWanted( paths: readonly string[] ): void;
	};
	readonly scenery: ReturnType<typeof createSceneryEmission>;
	readonly lizardGid: number;
}

/*
================
DockPreviewCatalog

Read-only catalog values borrowed from the publication owner each frame.
================
*/
interface DockPreviewCatalog {
	readonly catalog: ReadonlyMap<number, Resource>;
	readonly dress: DressCatalog;
	readonly itemIds: ReadonlyMap<string, number>;
	readonly manifest: number;
}

/*
================
DockPreviewFrame
================
*/
interface DockPreviewFrame {
	readonly seconds: number;
	readonly dock?: readonly CharacterRecord[];
	readonly preview?: CreationSnapshot | null;
	readonly lizard: boolean;
	readonly nativeServerName?: string;
}

/*
================
DockPreviewOutput

The fields this phase borrows from the shared presentation output owner.
================
*/
type DockPreviewOutput = Pick<
	PresentationOutput,
	"previewReady" | "dockReady" | "displayed" | "failure" | "cameraTarget"
>;

/*
================
createDockPreview
================
*/
export function createDockPreview( owners: DockPreviewOwners ) {
	const { renderer, resources, scenery, lizardGid } = owners;
	const previewWear = new Map<number, { resource: Resource; keys: readonly string[]; }>();
	const dockStates = new Map<
		number,
		{ deleted: boolean; started: number; transition: string | null; previous?: CharacterActor; }
	>();
	let lizardStarted: number | null = null;
	let previewShape: BodyShapeBlend | null = null;
	let previewDisplay: { actor: CharacterActor; paths: string[]; } | null = null;
	return {
		/*
		================
		step

		True means the dock owns this frame and world presentation must stop.
		================
		*/
		step( frame: DockPreviewFrame, output: DockPreviewOutput, published: DockPreviewCatalog ) {
			const { seconds, dock, preview, lizard, nativeServerName } = frame;
			output.previewReady = false;
			output.dockReady = false;
			if ( !preview ) {
				previewDisplay = null;
				previewShape = null;
			}
			if ( dock || preview ) {
				if ( previewDisplay ) resources.plan( previewDisplay.paths );
				const actors: CharacterActor[] = [];
				// Dock and creation actors are dressed through the item catalog
				// (roster.json, manifest 0); none exists before it is resident.
				// The gecko needs no catalog and is admitted meanwhile.
				const catalogResident = published.manifest > 0;
				const rows = !catalogResident ? [] : preview ?
					[ {
						id: 0,
						name: preview.selection.name,
						deletePending: false,
						visualLoadout: creationLoadout( preview.selection, published.itemIds )
					} ] :
					dock!.slice( 0, 4 );
				for ( const [index, row] of rows.entries() ) {
					try {
						const resource = [ ...published.catalog.values() ].find( model =>
							model.codename === row.visualLoadout.modelCodename
						);
						if ( !resource ) continue;
						// SCharacterInfo_BuildDisplayActor: the dock is dressed from the row's
						// items through the same slot visuals as the world. Previews are ownerless.
						const oldWear = previewWear.get( row.id ),
							freeze = nativeServerName !== undefined &&
								defaultWearFrozen( published.dress.defaultWearLanguage ?? 4, nativeServerName );
						const assembly = assembleEquipmentAppearance( {
							resource,
							dress: published.dress,
							equipment: wornItemsFromList( row.visualLoadout.items, published.dress ),
							avatars: row.visualLoadout.avatars,
							hwanHair: false,
							mounted: false,
							weaponHidden: false,
							attachmentsHidden: false,
							fortressIndex: -1,
							player: true,
							ownerless: true,
							committedWear: oldWear?.resource === resource ? oldWear.keys : [],
							freezeWear: freeze
						} );
						const parts = assembly.parts, wear = assembly.defaultWear;
						if ( !resource.previewGlb || !resource.previewClips ) {
							throw Error( "Missing native dock preview " + resource.codename );
						}
						const paths = [ resource.previewGlb, ...parts.map( part => part.model ) ];
						// Admit unknown parts incrementally: reserving the maximum
						// for an entire equipped actor at once can never fit.
						if ( !paths.map( path => resources.ready( path ) ).every( Boolean ) ) continue;
						const { heightScale, volumeScale, ...assemblyLoadout } = row.visualLoadout;
						previewWear.set( row.id, { resource, keys: wear } );
						const model = (preview ? "creation:" : "dock:") + row.id + ":" +
							JSON.stringify( assemblyLoadout ) + (wear.length ? ":" + JSON.stringify( wear ) : "");
						renderer.setCharacterAssembly( model, resource.previewGlb, parts );
						let clip = previewIdle(
								resource.previewClips,
								row.visualLoadout.animationSetName,
								row.deletePending
							),
							time = seconds,
							loop = true;
						if ( !preview ) {
							let state = dockStates.get( row.id );
							if ( !state ) {
								state = { deleted: row.deletePending, started: seconds, transition: null };
								dockStates.set( row.id, state );
							} else if ( state.deleted !== row.deletePending ) {
								state = {
									deleted: row.deletePending,
									started: seconds,
									transition: row.deletePending ? "charselect-state13" : "charselect-state15",
									previous: output.displayed.get( row.id )
								};
								dockStates.set( row.id, state );
							}
							if ( state.transition ) {
								const duration = resources.duration( resource.previewGlb, state.transition );
								if ( duration <= 0 ) {
									throw Error( "Missing native deletion transition " + state.transition );
								}
								if ( seconds - state.started < duration ) {
									clip = state.transition;
									time = seconds - state.started;
									loop = false;
								} else state.transition = null;
							}
						}
						const transition = dockStates.get( row.id ),
							elapsed = transition ? Math.max( 0, seconds - transition.started ) : 1;
						const volume = preview ?
							preview.selection.volume :
							bodyVolumeIndex( dock![index]!.bodyShapeByte, dock![index]!.volumeIndex );
						if ( preview ) {
							previewShape = advanceBodyShape(
								previewShape,
								resource.codename,
								row.visualLoadout.heightScale,
								volume,
								seconds
							);
						}
						const opacity = preview ?
							1 :
							transition?.previous ?
							(transition.previous.opacity ?? 1) +
							((row.deletePending ? .8 : 1) - (transition.previous.opacity ?? 1)) *
								Math.min( 1, elapsed ) :
							row.deletePending ?
							.8 :
							1;
						actors.push( {
							gid: row.id,
							model,
							opacity,
							layers: transition?.previous && elapsed < .1 ?
								[ {
									clip: transition.previous.clip,
									time: transition.previous.time + elapsed,
									loop: transition.previous.loop,
									weight: 1 - elapsed / .1,
									lane: "event"
								}, { clip, time, loop, weight: 1, lane: "timed" } ] :
								undefined,
							pose: preview ?
								{ regionId: 0, x: 3, y: .5, z: 0, yaw: previewYaw( preview.yaw ) } :
								dockSlot( index, dock!.length ),
							clip,
							time,
							loop,
							scale: preview ? previewShape!.height : row.visualLoadout.heightScale,
							bodyVolume: {
								index: preview ? previewShape!.volume : volume,
								female: resource.codename.includes( "_WOMAN_" )
							}
						} );
						if ( preview ) previewDisplay = { actor: actors.at( -1 )!, paths };
					} catch ( error ) {
						output.failure = String( error );
					}
				}
				for ( const id of previewWear.keys() ) {
					if ( !rows.some( row => row.id === id ) ) previewWear.delete( id );
				}
				for ( const id of dockStates.keys() ) {
					if ( preview || !rows.some( row => row.id === id ) ) dockStates.delete( id );
				}
				// Count only complete roster assemblies before auxiliary actors enter.
				output.dockReady = catalogResident && !preview && actors.length === rows.length;
				if ( lizard && !preview ) {
					const path = "/assets/character-select/interface_lizard.glb";
					if ( resources.ready( path ) ) {
						if ( lizardStarted === null ) lizardStarted = seconds;
						const elapsed = seconds - lizardStarted, duration = resources.duration( path, "move" );
						// 73a207: PlayAnimation(1,0,200,0,1,1); the animation set
						// has no enter fade and a 200 ms EXIT fade. Root motion stays in the skeleton.
						actors.push( {
							gid: lizardGid,
							model: path,
							pose: {
								regionId: 0x6951,
								x: 155.600006,
								y: -20,
								z: 651.599976,
								yaw: radians( Math.PI - 3 )
							},
							clip: elapsed < duration + .2 ? "move" : "stand",
							time: elapsed,
							loop: elapsed >= duration + .2,
							scale: 1,
							layers: oneShotLayers( "move", "stand", elapsed, duration, .2 )
						} );
					}
				} else lizardStarted = null;
				if ( preview && catalogResident ) {
					// Customization admits the whole selectable wardrobe. Waiting
					// for only the initial outfit makes the first equipment click
					// a network operation after the screen has already been revealed.
					const paths = new Set<string>();
					for ( const gender of [ 0, 1 ] as const ) {
						const selection = { ...preview.selection, gender };
						const [firstFigure, lastFigure] = creationRange( selection, "figure" );
						for ( let figure = firstFigure; figure <= lastFigure; figure++ ) {
							const codename =
								creationLoadout( { ...selection, figure }, published.itemIds ).modelCodename;
							const model = [ ...published.catalog.values() ].find( row => row.codename === codename );
							if ( model?.previewGlb ) paths.add( model.previewGlb );
						}
						const [firstWeapon, lastWeapon] = creationRange( selection, "weapon" );
						for ( let weapon = firstWeapon; weapon <= lastWeapon; weapon++ ) {
							const equipped = { ...selection, weapon };
							const [firstProtector, lastProtector] = creationRange( equipped, "protector" );
							for ( let protector = firstProtector; protector <= lastProtector; protector++ ) {
								const loadout = creationLoadout( { ...equipped, protector }, published.itemIds );
								const body = (selection.race === 0 ? "EU" : "CH") + "_" + (gender === 0 ? "M" : "W");
								for ( const item of loadout.items ) {
									const entry = published.dress.equipment?.[String( item.refObjId )]?.bodies[body];
									if ( entry ) paths.add( entry.glb );
								}
							}
						}
						const prefix = (selection.race === 0 ? "EU" : "CH") + "_" + (gender === 0 ? "M" : "W") + "_";
						for ( const [key, entry] of Object.entries( published.dress.defaultWear ?? {} ) ) {
							if ( key.startsWith( prefix ) ) paths.add( entry.glb );
						}
					}
					output.previewReady = actors.length === 1;
					for ( const path of paths ) {
						// Do not short-circuit: ready owns bounded incremental admission.
						const ready = resources.ready( path );
						output.previewReady = ready && output.previewReady;
					}
				}
				if ( preview && !actors.length && previewDisplay ) {
					actors.push( {
						...previewDisplay.actor,
						pose: { ...previewDisplay.actor.pose, yaw: previewYaw( preview.yaw ) },
						time: seconds
					} );
				}
				output.displayed = new Map( actors.map( actor => [ actor.gid, actor ] ) );
				actors.push(
					...scenery.step(
						renderer.scenery?.() ?? null,
						seconds,
						resources.ready,
						CHARACTER_ACTORS - actors.length
					)
				);
				output.cameraTarget = null;
				renderer.setCharacterActors( actors );
				resources.retainWanted( actors.map( actor => actor.model ) );
				return true;
			}
			return false;
		},
		/*
		================
		reset
		================
		*/
		reset() {
			previewDisplay = null;
			previewShape = null;
			previewWear.clear();
			dockStates.clear();
			lizardStarted = null;
		}
	};
}
