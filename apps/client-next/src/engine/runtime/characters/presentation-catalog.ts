/*
===========================================================================

presentation-catalog.ts - the published catalogues character presentation reads

Owns the character, NPC, mission, sound, animation, item-drop and skill-data
manifests once admitted: validated rows projected into the lookups that
assembly, animation and sound selection read every frame. Admission keeps
the original order. A manifest rejected partway can keep its earlier
writes: shadowSizes, bloodEffects and riderModes are written while their
rows validate, and recoveryByCodename and the sound rules are replaced
before the appearance stores are validated. The other lookups are built
aside and swapped in at the end.

Requesting the next manifest stays with the presentation owner, which knows
whether a dock, a creation preview or a world roster needs one.

===========================================================================
*/

import type { TradeSkinPools } from "@/engine/foundation/animation/trade-appearance";
import { validateEquipmentBranches } from "@/engine/foundation/animation/equipment-sockets";
import { validateEquipmentParticles } from "@/engine/foundation/animation/equipment-particles";
import {
	createItemCodenameIndex,
	type DressCatalog,
	type SetEntry
} from "@/engine/foundation/animation/equipment-appearance";
import { modelAnimationParticles } from "@/engine/foundation/animation/animation-emission";
import { modelAnimationBindings, modelModifierSets } from "@/engine/foundation/animation/model-animation";
import { modelAmbientParticles } from "@/engine/foundation/animation/model-emission";
import { readStructureVisuals } from "@/engine/foundation/rendering/structure-stage";
import type { createReferenceAppearances } from "@/engine/foundation/animation/reference-appearance";
import { skillSoundRoots } from "@/engine/foundation/animation/sound-selectors";
import { animationMetadata, type AnimationMetadata } from "@/engine/foundation/animation/animation-metadata";
import type { CharacterActor } from "@/engine/contracts/character";
import type {
	DeathModel,
	DropModel,
	ItemPresentation,
	LinkedRide,
	Resource,
	SoundRule
} from "./internal/presentation-contract";

/*
================
CatalogOwners

The owners whose catalogues ride in these same manifests, as this module
needs them. Sibling owners are passed in by characters.ts, never imported.
================
*/
export interface CatalogOwners {
	// The resource owner's verdict on each admitted manifest.
	readonly resources: {
		accepted( path: string ): void;
		rejected( path: string, error: unknown ): void;
	};
	// The sound owner validates its own rule rows; this catalogue only forwards them.
	readonly sounds: { catalog( rules: readonly SoundRule[] ): void; };
	readonly referenceAppearances: Pick<ReturnType<typeof createReferenceAppearances>, "setReferences">;
}

/*
================
PublishedResult

One fetched manifest, as the resource owner's poll returns it.
================
*/
export interface PublishedResult {
	readonly path: string;
	readonly buffer: ArrayBuffer;
}

/*
================
admitDeathModel

One "death" manifest row: the characterInfo death BSR every requiredBy
codename loads on death (CICharactor_Action_KnockdownDie 8E64F0). Its mesh
projections are the same ones a body row receives.
================
*/
function admitDeathModel( row: Resource & { requiredBy?: unknown; }, deaths: Map<string, DeathModel> ) {
	if (
		typeof row.glb !== "string" || !row.glb.startsWith( "/assets/npc/" ) || row.glb.includes( ".." ) ||
		!Array.isArray( row.clips ) || row.clips.some( clip => typeof clip !== "string" ) ||
		!Array.isArray( row.requiredBy ) || row.requiredBy.some( owner => typeof owner !== "string" )
	) throw new Error( "Invalid death model" );
	const particles = modelAnimationParticles( row.particleModifiers );
	const model: DeathModel = {
		glb: row.glb,
		clips: row.clips,
		animationStates: row.animationStates ? animationMetadata( row.animationStates ) : undefined,
		ambientParticles: modelAmbientParticles( row.particleModifiers ),
		animationParticles: particles,
		animationParticlePaths: [
			...new Set(
				particles.flatMap( set =>
					set.particles.map( p => "/assets/effects/programs.json#" + encodeURIComponent( p.effectPath ) )
				)
			)
		],
		modifierBindings: modelAnimationBindings( row.animationBindings ),
		modifierSelectors: modelModifierSets( row.modifierSets )
	};
	for ( const owner of row.requiredBy as string[] ) deaths.set( owner, model );
}

/*
================
joinModelResources

The NPC manifest stores each baked model once under its BSR and keeps one
slim row per reference (npcManifest.mjs, v9); a row is its resource with
the reference's own fields over it. A row naming a BSR the file does not
hold is a broken publication.
================
*/
function joinModelResources(
	models: Resource[] | Record<string, Resource> | undefined,
	resources: Record<string, Partial<Resource>>
): Record<string, Resource> {
	const joined: Record<string, Resource> = {};
	for ( const [codename, row] of Object.entries( models ?? {} ) ) {
		const bsr = (row as { bsr?: unknown; }).bsr;
		if ( typeof bsr !== "string" ) {
			joined[codename] = row;
			continue;
		}
		const resource = resources[bsr];
		if ( !resource || typeof resource !== "object" ) throw Error( "Missing model resource " + bsr );
		joined[codename] = { ...resource, ...row } as Resource;
	}
	return joined;
}

/*
================
readTradeSkinPools

The China and Europe skin pools: [refObjId, sex] rows, sex 0 or 1.
================
*/
function readTradeSkinPools( value: unknown ): TradeSkinPools {
	const pools = value as { china?: unknown; europe?: unknown; } | null;
	const rows = ( list: unknown ) => {
		if (
			!Array.isArray( list ) || list.length > 0xffff ||
			list.some( row =>
				!Array.isArray( row ) || row.length !== 2 || !Number.isSafeInteger( row[0] ) || row[0] <= 0 ||
				(row[1] !== 0 && row[1] !== 1)
			)
		) throw Error( "Invalid trade skin pool" );
		return list as [number, number][];
	};
	return { china: rows( pools?.china ), europe: rows( pools?.europe ) };
}

/*
================
createPresentationCatalog
================
*/
export function createPresentationCatalog( owners: CatalogOwners ) {
	const { resources, sounds, referenceAppearances } = owners;
	const recoveryByCodename = new Map<string, number>();
	const catalog = new Map<number, Resource>();
	let dress: DressCatalog = {},
		itemIds: ReadonlyMap<string, number> = new Map(),
		// The trade bandits' bodies (missionPresentation.json, 861720).
		tradeSkinPools: TradeSkinPools = { china: [], europe: [] },
		items: Record<string, ItemPresentation> = {};
	let dropModels: Record<string, DropModel> = {};
	const animationStates = new Map<string, Record<string, AnimationMetadata>>(),
		soundProfiles = new Map<string, string>();
	let skillSounds = skillSoundRoots( [] );
	const manifests = [
		"/assets/char/roster.json",
		"/assets/npc/manifest.json",
		"/assets/data/missionPresentation.json",
		"/assets/audio/effectsound.json",
		"/assets/anim/manifest.json",
		"/assets/itemdrop/manifest.json",
		// The skill sound plane and the characterInfo plane, published apart from
		// the 27,835-row skill catalogue the HUD owns (buildSkillDataAsset.mjs).
		"/assets/data/skillAudioData.json",
		"/assets/data/characterActionData.json"
	];
	const shadowSizes = new Map<number, number>();
	const heights = new Map<string, number>(), heightFactors = new Map<string, number>();
	const bloodEffects = new Map<string, readonly [string | null, string | null]>(),
		riderModes = new Map<string, number>();
	const effectAnchors = new Map<string, NonNullable<CharacterActor["effectAnchor"]>>();
	const nativeMotionUrls = new Map<string, ReadonlyMap<string, string>>();
	// characterInfo rides: the rider's codename -> its packetless ride model
	// (CICMonster_DeserializeSpawnPacket).
	const ridesByRider = new Map<string, LinkedRide>();
	// characterInfo death models: the dying codename -> the mesh 8E64F0 loads.
	const deathModels = new Map<string, DeathModel>();
	let manifest = 0;
	return {
		recoveryByCodename,
		catalog,
		animationStates,
		soundProfiles,
		manifests,
		shadowSizes,
		heights,
		heightFactors,
		bloodEffects,
		riderModes,
		effectAnchors,
		nativeMotionUrls,
		ridesByRider,
		deathModels,
		get dress() {
			return dress;
		},
		get tradeSkinPools() {
			return tradeSkinPools;
		},
		get itemIds() {
			return itemIds;
		},
		get items() {
			return items;
		},
		get dropModels() {
			return dropModels;
		},
		get skillSounds() {
			return skillSounds;
		},
		/*
		================
		manifest

		How many of manifests are admitted, in order; manifests[manifest] is next.
		================
		*/
		get manifest() {
			return manifest;
		},
		/*
		================
		admit

		Validate one presentation manifest and project it into the live
		lookups, in the order the file banner describes.
		================
		*/
		admit( result: PublishedResult ) {
			try {
				const decoded = JSON.parse( new TextDecoder( "utf-8", { fatal: true } ).decode( result.buffer ) );
				const value = decoded as {
					effectAppearanceStores?: number[][];
					effectAppearanceReferences?: readonly (readonly [number, number, number])[];
					skillAudioRows?: string[];
					recoveryByCodename?: Record<string, number>;
					tradeSkinPools?: unknown;
					models?: Resource[] | Record<string, Resource>;
					resources?: Record<string, Partial<Resource>>;
					dress?: typeof dress;
					itemsByRefObjId?: typeof items;
					rules?: SoundRule[];
					characterShadowSizes?: readonly (readonly [number, number])[];
					characterActionEffectRows?: {
						codename: string;
						soundProfileName: string;
						bloodEffects?: readonly [string | null, string | null];
						riderTransformMode?: number;
						heightFactor?: number;
						anchorSocketName?: string | null;
						anchorOffset?: { x: number; y: number; z: number; };
					}[];
				};
				if ( value.resources ) value.models = joinModelResources( value.models, value.resources );
				if ( value.characterShadowSizes ) {
					for ( const [id, size] of value.characterShadowSizes ) {
						if (
							!Number.isSafeInteger( id ) || id <= 0 || !Number.isInteger( size ) || size < 0
						) throw Error( "Invalid character shadow reference size" );
						shadowSizes.set( id, size );
					}
				}
				const nextHeights = new Map( heights ),
					nextHeightFactors = new Map( heightFactors ),
					nextEffectAnchors = new Map( effectAnchors );
				const nextSkillSounds = value.skillAudioRows ?
					skillSoundRoots( value.skillAudioRows ) :
					skillSounds;
				const nextCatalog = new Map( catalog ),
					nextAnimations = new Map( animationStates ),
					nextProfiles = new Map( soundProfiles ),
					nextMotionUrls = new Map( nativeMotionUrls );
				const rows = Object.values( value.models ?? {} ).filter( row => row.refObjId !== undefined );
				const nextRides = new Map( ridesByRider ), nextDeaths = new Map( deathModels );
				for (
					const row of Object.values( value.models ?? {} ) as (Resource & {
						kind?: string;
						requiredBy?: unknown;
					})[]
				) {
					if ( row.kind === "death" ) {
						admitDeathModel( row, nextDeaths );
						continue;
					}
					if ( row.kind !== "ride" ) continue;
					if (
						typeof row.glb !== "string" || !row.glb.startsWith( "/assets/npc/" ) ||
						row.glb.includes( ".." ) ||
						!Array.isArray( row.clips ) || row.clips.some( clip => typeof clip !== "string" ) ||
						!Array.isArray( row.requiredBy ) || row.requiredBy.some( rider => typeof rider !== "string" )
					) throw new Error( "Invalid linked ride" );
					for ( const rider of row.requiredBy as string[] ) {
						nextRides.set( rider, { glb: row.glb, clips: row.clips } );
					}
				}
				for ( const row of rows ) {
					if (
						(row.scalePercent !== undefined &&
							(!Number.isFinite( row.scalePercent ) || row.scalePercent <= 0)) ||
						(row.eventRain !== undefined && typeof row.eventRain !== "boolean") ||
						!Number.isInteger( row.refObjId ) || typeof row.codename !== "string" ||
						typeof row.glb !== "string" || !Array.isArray( row.clips ) || row.clips.some( clip =>
							typeof clip !== "string"
						)
					) {
						throw new Error( "Invalid character manifest" );
					}
				}
				for ( const row of rows ) {
					if (
						row.materialKind !== undefined &&
						(!Number.isInteger( row.materialKind ) || row.materialKind < 0 || row.materialKind > 255)
					) throw Error( "Invalid monster material kind" );
					if (
						row.materialVariants !== undefined &&
						(typeof row.materialVariants !== "object" || row.materialVariants === null ||
							Array.isArray( row.materialVariants ) ||
							Object.entries( row.materialVariants ).some( ( [slot, path] ) =>
								!/^([1-4])$/.test( slot ) || typeof path !== "string" ||
								!path.startsWith( "/assets/npc/" ) || path.includes( ".." ) ||
								!path.endsWith( ".glb" )
							))
					) throw Error( "Invalid monster material variants" );
					const staged = readStructureVisuals( row, modelAmbientParticles );
					nextCatalog.set( row.refObjId, {
						...row,
						...(staged ? { structureVisuals: staged } : {}),
						ambientParticles: modelAmbientParticles( row.particleModifiers ),
						animationParticles: modelAnimationParticles( row.particleModifiers ),
						animationParticlePaths: [
							...new Set(
								modelAnimationParticles( row.particleModifiers ).flatMap( s =>
									s.particles.map( p =>
										"/assets/effects/programs.json#" + encodeURIComponent( p.effectPath )
									)
								)
							)
						],
						modifierBindings: modelAnimationBindings( row.animationBindings ),
						modifierSelectors: modelModifierSets( row.modifierSets ),
						animationStates: row.animationStates ? animationMetadata( row.animationStates ) : undefined
					} );
				}

				if ( result.path === "/assets/anim/manifest.json" ) {
					for (
						const [name, entry] of Object.entries(
							(decoded as {
								models: Record<string, Record<string, unknown>>;
							}).models ?? {}
						)
					) {
						const { soundProfileName, animationSets, ...clips } = entry;
						const metadata = animationMetadata( clips );
						if ( metadata.hit && !metadata.hit1 ) metadata.hit1 = metadata.hit;
						if (
							animationSets && typeof animationSets === "object" && !Array.isArray( animationSets )
						) {
							for ( const [set, states] of Object.entries( animationSets ) ) {
								if (
									!states || typeof states !== "object" || Array.isArray( states )
								) throw new Error( "Invalid animation set" );
								const rows = states as Record<string, unknown>;
								const urls = new Map( nextMotionUrls.get( name ) );
								for ( const [state, value] of Object.entries( rows ) ) {
									const role = `native:${set}:${state}`, url = (value as { url?: unknown; }).url;
									if (
										!/^\d+$/.test( state ) || typeof url !== "string" ||
										!url.startsWith( "/assets/anim/" ) || !url.endsWith( ".ban" ) ||
										url.includes( ".." )
									) throw Error( "Invalid native animation publication" );
									Object.assign( metadata, animationMetadata( { [role]: value } ) );
									urls.set( role, url );
								}
								nextMotionUrls.set( name, urls );
								for (
									const [role, id] of Object.entries( {
										attack1: 2,
										attack2: 5,
										attack3: 16,
										attack4: 17
									} )
								) {
									if ( rows[String( id )] ) {
										Object.assign(
											metadata,
											animationMetadata( {
												[`${role}-${set.replaceAll( "_", "-" )}`]: rows[String( id )]
											} )
										);
									}
								}
							}
						}
						nextAnimations.set( name, metadata );
						if ( typeof soundProfileName === "string" ) {
							nextProfiles.set( name, soundProfileName );
						}
					}
				}
				for ( const row of value.characterActionEffectRows ?? [] ) {
					if ( !row || typeof row.codename !== "string" || typeof row.soundProfileName !== "string" ) {
						throw new Error( "Invalid sound profile" );
					}
					nextProfiles.set( row.codename, row.soundProfileName );
					if ( row.bloodEffects ) {
						if (
							row.bloodEffects.length !== 2 ||
							row.bloodEffects.some( p =>
								p !== null && (typeof p !== "string" || !p.endsWith( ".efp" ) || p.includes( ".." ))
							)
						) throw Error( "Invalid character blood resources" );
						bloodEffects.set( row.codename, row.bloodEffects );
					}
					if ( row.riderTransformMode !== undefined ) {
						riderModes.set( row.codename, row.riderTransformMode );
					}
					if ( row.anchorOffset ) {
						const a = row.anchorOffset;
						if ( ![ a.x, a.y, a.z ].every( Number.isFinite ) ) {
							throw Error( "Invalid action effect anchor" );
						}
						nextEffectAnchors.set( row.codename, {
							bone: row.anchorSocketName ?? null,
							offset: [ a.x, a.y, a.z ]
						} );
					}
					if ( row.heightFactor !== undefined ) {
						if ( !Number.isFinite( row.heightFactor ) ) throw new Error( "Invalid character height" );
						nextHeights.set( row.codename, Math.fround( Math.fround( row.heightFactor ) * 20 ) );
						nextHeightFactors.set( row.codename, Math.fround( row.heightFactor ) );
					}
				}
				if ( value.dress ) {
					validateEquipmentParticles( value.dress.specialGlows );
					if (
						value.dress.defaultWearLanguage !== undefined &&
						(!Number.isInteger( value.dress.defaultWearLanguage ) ||
							value.dress.defaultWearLanguage < 0 || value.dress.defaultWearLanguage > 5)
					) throw Error( "Invalid native clothing language" );
					for ( const [id, row] of Object.entries( value.dress.avatarVisualOverrides ?? {} ) ) {
						if (
							!/^\d+$/.test( id ) || !row || typeof row.animation !== "string" ||
							!Number.isInteger( row.priority ) || row.priority < 0 || row.priority > 255 ||
							typeof row.additionalBsr !== "string" ||
							row.additionalBsr !== "" &&
								(!row.additionalBsr.startsWith( "res/" ) || !row.additionalBsr.endsWith( ".bsr" ) ||
									row.additionalBsr.includes( ".." ))
						) throw Error( "Invalid avatar visual override" );
					}
					const equipmentEntries: Record<string, SetEntry> = {};
					for ( const [id, row] of Object.entries( value.dress.equipment ?? {} ) ) {
						if (
							!row || !/^\d+$/.test( id ) ||
							row.slot !== null && (!Number.isInteger( row.slot ) || row.slot < 0 || row.slot > 8) ||
							!row.bodies || typeof row.bodies !== "object"
						) throw Error( "Invalid native equipment reference" );
						for ( const [body, entry] of Object.entries( row.bodies ) ) {
							if ( entry !== null ) equipmentEntries[id + body] = entry;
						}
					}
					for ( const entry of Object.values( value.dress.avatarAuxiliary ?? {} ) ) {
						if (
							!entry || typeof entry.bone !== "string" || !entry.bone ||
							!Array.isArray( entry.clips ) || !entry.clips.includes( "stand" ) ||
							entry.clips.some( clip => typeof clip !== "string" )
						) throw Error( "Invalid auxiliary avatar" );
					}
					for (
						const entries of [
							value.dress.hwan ?? {},
							value.dress.defaultWear ?? {},
							value.dress.fortressWear ?? {},
							value.dress.avatarAuxiliary ?? {},
							equipmentEntries
						]
					) {
						if ( !entries || typeof entries !== "object" || Array.isArray( entries ) ) {
							throw new Error( "Invalid equipment catalog" );
						}
						for ( const entry of Object.values( entries ) ) {
							if (
								!entry || typeof entry.glb !== "string" || !entry.glb.startsWith( "/assets/" ) ||
								entry.glb.includes( ".." ) || !Array.isArray( entry.parts ) ||
								entry.parts.some( part => typeof part !== "string" )
							) throw new Error( "Invalid equipment entry" );
							validateEquipmentBranches( entry.branches );
							if ( entry.covers ) {
								for ( const indices of Object.values( entry.covers ) ) {
									if (
										!Array.isArray( indices ) ||
										indices.some( index => !Number.isInteger( index ) || index < 0 )
									) throw new Error( "Invalid equipment coverage" );
								}
							}
						}
					}
				}
				if ( value.itemsByRefObjId ) {
					for ( const entry of Object.values( value.itemsByRefObjId ) ) {
						if ( !entry || typeof entry.codename !== "string" ) {
							throw new Error( "Invalid item presentation" );
						}
					}
				}
				let nextDrops = dropModels;
				if ( result.path === "/assets/itemdrop/manifest.json" ) {
					if (
						decoded.format !== "sro-mission-itemdrop-models" || !decoded.models ||
						typeof decoded.models !== "object" || Array.isArray( decoded.models )
					) throw new Error( "Invalid drop model catalog" );
					for (
						const row of Object.values( decoded.models ) as {
							glb: string;
							clips: string[];
							clipLoop: boolean;
						}[]
					) {
						if (
							!row || typeof row.glb !== "string" || !row.glb.startsWith( "/assets/itemdrop/" ) ||
							row.glb.includes( ".." ) || !Array.isArray( row.clips ) || row.clips.some( clip =>
								typeof clip !== "string"
							) || typeof row.clipLoop !== "boolean"
						) throw new Error( "Invalid drop model entry" );
					}
					nextDrops = Object.fromEntries(
						Object.entries( decoded.models as typeof dropModels ).map( (
							[key, row]
						) => [ key, { ...row, ambientParticles: modelAmbientParticles( row.particleModifiers ) } ] )
					);
				}
				// No live state changes until every projection has validated.
				if ( value.recoveryByCodename ) {
					for ( const [name, period] of Object.entries( value.recoveryByCodename ) ) {
						if (
							!name || !Number.isInteger( period ) || period < 0 || period > 0x7fffffff - 500
						) {
							throw Error( "Invalid native recovery duration" );
						}
					}
				}
				const nextTradeSkinPools = value.tradeSkinPools === undefined ?
					tradeSkinPools :
					readTradeSkinPools( value.tradeSkinPools );
				if ( value.recoveryByCodename ) {
					recoveryByCodename.clear();
					for ( const [name, period] of Object.entries( value.recoveryByCodename ) ) {
						recoveryByCodename.set( name, period );
					}
				}
				tradeSkinPools = nextTradeSkinPools;
				if ( value.rules ) sounds.catalog( value.rules );
				if ( value.effectAppearanceStores ) {
					const pools = value.effectAppearanceStores;
					if (
						pools.length !== 2 ||
						pools.some( p => !Array.isArray( p ) || p.some( id => !Number.isInteger( id ) || id <= 0 ) )
					) throw Error( "Invalid native appearance stores" );
					// The msch (CSkillData+0x268) references, walked at build time by the
					// client's own decoder (tooltipAppearanceReferences).
					const refs = new Map<number, { type: number; cap: number; }>();
					for ( const row of value.effectAppearanceReferences ?? [] ) {
						if (
							!Array.isArray( row ) || row.length !== 3 || !Number.isSafeInteger( row[0] ) ||
							!Number.isSafeInteger( row[1] ) || !Number.isSafeInteger( row[2] ) || row[0] <= 0 ||
							refs.has( row[0] )
						) throw Error( "Invalid native appearance references" );
						refs.set( row[0], { type: row[1], cap: row[2] } );
					}
					referenceAppearances.setReferences( refs, pools );
				}
				skillSounds = nextSkillSounds;
				effectAnchors.clear();
				for ( const [key, anchor] of nextEffectAnchors ) effectAnchors.set( key, anchor );
				heights.clear();
				for ( const [key, height] of nextHeights ) heights.set( key, height );
				heightFactors.clear();
				for ( const [key, factor] of nextHeightFactors ) heightFactors.set( key, factor );
				catalog.clear();
				for ( const [key, row] of nextCatalog ) catalog.set( key, row );
				ridesByRider.clear();
				for ( const [key, row] of nextRides ) ridesByRider.set( key, row );
				deathModels.clear();
				for ( const [key, row] of nextDeaths ) deathModels.set( key, row );
				animationStates.clear();
				for ( const [key, row] of nextAnimations ) animationStates.set( key, row );
				nativeMotionUrls.clear();
				for ( const [key, row] of nextMotionUrls ) nativeMotionUrls.set( key, row );
				soundProfiles.clear();
				for ( const [key, row] of nextProfiles ) soundProfiles.set( key, row );
				if ( value.dress ) {
					dress = value.dress;
					itemIds = createItemCodenameIndex( dress );
				}
				if ( value.itemsByRefObjId ) items = value.itemsByRefObjId;
				dropModels = nextDrops;
				manifest++;
				resources.accepted( result.path );
			} catch ( error ) {
				resources.rejected( result.path, error );
			}
		},
		/*
		================
		dispose

		Releases the large lookups; the small reference tables stay until reload.
		================
		*/
		dispose() {
			catalog.clear();
			nativeMotionUrls.clear();
			animationStates.clear();
			soundProfiles.clear();
		}
	};
}
