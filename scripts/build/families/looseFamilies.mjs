/*
===========================================================================

looseFamilies.mjs - the focused asset families and how each is produced

A loose family is a handful of public files outside the main builders
(code-selected images, catalogs rebuilt by one step, patches over the world
and NPC manifests) that must still be published through the packs. Each row
says how its files are produced and which pack group a new file joins.

produce() only writes files: it never reads the published pack index, so it
runs on a fresh tree. packFiles( output, index ), when a row has it, picks
which representations a focused republish repacks; the full build packs by
sweep and does not need it. scripts/refresh_asset_family.mjs runs one row
under the generated-assets lock and hands the result to the one pack owner
(shared/looseFamilyPublication.mjs). kind names the row's task:
assets:refresh:<name> or assets:publish:<name>.

Rows keep the native provenance that explains why the family exists. A new
family is a new row, never a new script.

===========================================================================
*/
import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { readdir, readFile } from "node:fs/promises";
import path from "node:path";
import { buildSkillStageModelAssets } from "../char/buildSkillStageModelAssets.mjs";
import { publishEntityBsrModifiers } from "../char/publishEntityBsrModifiers.mjs";
import { buildQuestDataAsset } from "../data/buildQuestDataAsset.mjs";
import { buildSkillDataAsset } from "../data/buildSkillDataAsset.mjs";
import { parseWeatherEvents } from "../char/weatherEvents.mjs";
import { buildEffectProgramsAsset } from "../effects/buildEffectPrograms.mjs";
import { refreshPrecompressedSidecars } from "../generatedManifestSidecars.mjs";
import { publishBytesAtomically } from "../shared/atomicPublish.mjs";
import {
	buildAlarmSoundResource,
	buildNativeDirectSoundResources,
	buildWeatherSoundResources
} from "../shared/audioResources.mjs";
import { buildGuideImageResources, imagePublicPath, registerSpriteResource } from "../shared/cifResources.mjs";
import {
	itemMallRuntimeImageReferences,
	quickslotRuntimeImageReferences,
	returnScrollRuntimeImageReferences,
	runtimeCifImageReferences,
	slotEffectRuntimeImageReferences,
	worldMapMarkerRuntimeImageReferences
} from "../shared/cifRuntimeImageCatalog.mjs";
import { convertedImageFolder, publishConvertedImage } from "../shared/convertedImages.mjs";
import { writeJsonIfChanged } from "../shared/jsonOut.mjs";
import { pythonExecutable, runPython } from "../shared/pythonRun.mjs";
import { buildTextResources, completeRestrictionText } from "../shared/textResources.mjs";
import { buildDungeonResourceManifest, DUNGEON_RESOURCE_PUBLIC_PATH } from "../world/assets/buildDungeonResources.mjs";
import { buildDungeonWorlds } from "../world/assets/buildDungeonWorlds.mjs";
import { copyMissionMinimapTileImages } from "../world/assets/copyMissionMinimapTileImages.mjs";
import {
	buildNativeSkyStarPrimitive,
	copyReferencedSkyImages,
	resolveSkyTextures
} from "../world/assets/copySkyImages.mjs";
import { publicRoot, retailTextdataRoot } from "../world/paths.mjs";
import { claimPublicPaths, withPublication } from "../shared/publicationLedger.mjs";

// The CIFButton state family (sub_5419c0) for the quickslot and return-scroll
// buttons; none of these ships a _disable.
const BUTTON_STATES = [ "", "_focus", "_press" ];
const SPRITE_CATALOG = "/assets/cif/cif-sprite-catalog.json";
const NATIVE_WINDOW_SCRIPT = path.join( import.meta.dirname, "..", "..", "tools", "refresh_native_window_images.py" );
const FOOTPRINT_DDJ = /^effect\/footstep_(sand|snow)\.ddj$/;
const WORLD_ROOT = path.join( publicRoot, "assets", "world" );
// Lighter sidecar levels for the large world files these families rewrite.
const WORLD_SIDECAR_LEVELS = { gzipLevel: 3 };
const SKILL_UI_SCRIPT = path.join(
	import.meta.dirname,
	"..",
	"..",
	"..",
	"apps",
	"client-next",
	"tools",
	"build-skill-ui.py"
);
const SKILL_MASTERY_DATA = "/assets/data/skillmasterydata.json.gz";

/**
 * @typedef {string | ((file: string, index: object) => string | undefined)} DefaultGroup
 * @typedef {{ files: string[], note?: string, defaultGroup?: DefaultGroup, [extra: string]: unknown }} FamilyOutput
 * @typedef {{
 *   kind: "refresh" | "publish",
 *   label: string,
 *   packFolder: string,
 *   defaultGroup?: DefaultGroup,
 *   produce: ( flags: Set<string> ) => Promise<FamilyOutput>,
 *   packFiles?: ( output: FamilyOutput, index: object ) => string[]
 * }} LooseFamily
 */

/*
================
publicFile
================
*/
function publicFile( publicPath ) {
	return path.join( publicRoot, publicPath.slice( 1 ) );
}

/*
================
readPublicJson
================
*/
async function readPublicJson( publicPath ) {
	return JSON.parse( await readFile( publicFile( publicPath ), "utf8" ) );
}

/*
================
buttonStates

Every state DDJ of one CIFButton stem.
================
*/
function buttonStates( stem ) {
	return BUTTON_STATES.map( state => `${stem}${state}.ddj` );
}

/*
================
publishImages

Publishes each referenced DDJ's converted image and returns the public paths.
================
*/
async function publishImages( references ) {
	const files = [];
	for ( const reference of references ) files.push( await publishConvertedImage( imagePublicPath( reference ) ) );
	return files;
}

/*
================
registerSprites

Code-selected CIF art must stay in the shared sprite catalog, or the client
draws a published sprite with no dimensions (cifSpriteCatalog.test.mjs). The
browser is served the catalog's precompressed sidecars, so they follow a
rewrite.
================
*/
async function registerSprites( references ) {
	const catalog = await readPublicJson( SPRITE_CATALOG );
	for ( const reference of references ) await registerSpriteResource( catalog, reference );
	if ( await writeJsonIfChanged( publicFile( SPRITE_CATALOG ), catalog ) ) {
		await refreshPrecompressedSidecars( [ publicFile( SPRITE_CATALOG ) ] );
	}
}

/*
================
packedJson

Refreshes stale sidecars of loose JSON records and returns the .json.gz
members the packs hold for them.
================
*/
async function packedJson( publicPaths ) {
	await refreshPrecompressedSidecars( publicPaths.map( publicFile ), { onlyWhenStale: true } );
	return publicPaths.map( publicPath => publicPath + ".gz" );
}

/*
================
toPublic

The /assets/... path of a file under the public root.
================
*/
function toPublic( file ) {
	return "/" + path.relative( publicRoot, file ).replaceAll( "\\", "/" );
}

/*
================
packedRepresentations

For a focused republish: each representation (plain or .gz) of the given
files that the index already packs. One no group holds stays loose, as the
full build left it.
================
*/
function packedRepresentations( index, publicPaths ) {
	const packed = new Set( index.assets.map( row => row.path ) );
	return publicPaths.flatMap( logical => [ logical, logical + ".gz" ].filter( path => packed.has( path ) ) );
}

/*
================
groupOf

The pack group that holds a public path in the index, for a default group.
================
*/
function groupOf( index, publicPath ) {
	return index.assets.find( row => row.path.toLowerCase() === publicPath.toLowerCase() )?.group;
}

/*
================
rewriteWorldSkies

Rewrites the sky block of every published world file through update( sky,
world, file ), which returns true when it changed it. The whole set is
validated (update may throw) before any file is replaced. Returns the world
files carrying a sky, changed or not, so an interrupted run is repaired by
refreshing their sidecars too.
================
*/
async function rewriteWorldSkies( update ) {
	const changes = [], published = [];
	for ( const name of await readdir( WORLD_ROOT, { recursive: true } ) ) {
		if ( !name.endsWith( ".json" ) ) continue;
		const file = path.join( WORLD_ROOT, name );
		const value = JSON.parse( await readFile( file, "utf8" ) );
		if ( !value.sky ) continue;
		published.push( file );
		if ( update( value.sky, value, file ) ) changes.push( [ file, JSON.stringify( value ) ] );
	}
	for ( const [file, json] of changes ) await publishBytesAtomically( file, Buffer.from( json ) );
	await refreshPrecompressedSidecars( published, { onlyWhenStale: true, ...WORLD_SIDECAR_LEVELS } );
	return { changed: changes.length, published: published.map( toPublic ) };
}

/*
================
entityBsrGroup
================
*/
function entityBsrGroup( file ) {
	if ( file.endsWith( ".gz" ) ) return "game-data";
	if ( file.endsWith( ".glb" ) || file.endsWith( ".vat.bin" ) ) return "game-models";
	return "game-images";
}

/*
================
produceEntityBsr

Republishes the entity BSR modifier manifests and the effect programs they
reference. --skillfx adds the skill stage models; --rebuilt-npc adds the NPC
models, material variants and VAT payloads a mesh rebuild produced.
================
*/
async function produceEntityBsr( flags ) {
	const manifests = await publishEntityBsrModifiers();
	if ( flags.has( "--skillfx" ) ) {
		await buildSkillStageModelAssets();
		const stage = await readPublicJson( "/assets/skillfx/manifest.json" );
		manifests.push( "/assets/skillfx/manifest.json", ...Object.values( stage.models ).map( row => row.glb ) );
	}
	await buildEffectProgramsAsset();
	if ( flags.has( "--rebuilt-npc" ) ) {
		const npc = await readPublicJson( "/assets/npc/manifest.json" );
		for ( const row of Object.values( npc.models ) ) {
			manifests.push(
				row.glb,
				...Object.values( row.materialVariants ?? {} ),
				...(row.vat ? [ row.vat.manifest, row.vat.bin ] : [])
			);
		}
		manifests.push( "/assets/npc/animation-catalog.json" );
	}
	const dependencies = [ ...new Set( manifests ) ];
	await refreshPrecompressedSidecars( dependencies.map( publicFile ), { onlyWhenStale: true } );
	const programs = await readPublicJson( "/assets/effects/programs.json" );
	const json = [ ...dependencies.filter( url => url.endsWith( ".json" ) ), "/assets/effects/programs.json" ];
	const other = [
		...new Set( [
			...dependencies.filter( url => !url.endsWith( ".json" ) ),
			...Object.values( programs.textures )
		] )
	];
	return {
		files: [ ...other, ...json.map( url => url + ".gz" ) ],
		json,
		other,
		note: flags.has( "--rebuilt-npc" ) ?
			"including rebuilt NPC model/VAT references" :
			"without rebuilding mesh/VAT payloads"
	};
}

/*
================
entityBsrPackFiles

Preserve and refresh every existing logical representation. VAT JSON was
originally packed without gzip; refreshing only its sidecar would leave a
client requesting the plain path on the old bytes.
================
*/
function entityBsrPackFiles( output, index ) {
	const existing = new Set( index.assets.map( row => row.path ) );
	return [
		...new Set( [
			...output.other,
			...output.json.flatMap( url => existing.has( url ) ? [ url, url + ".gz" ] : [ url + ".gz" ] )
		] )
	];
}

/*
================
produceSlotEffects

The CIFSlotWithHelp overlay sheets are code-selected, so a new one reaches
the packs and the CIF sprite catalog only through this family or a full
asset build. A loose catalog cannot replace an older packed representation:
whichever identity/gzip members the index owns are republished, so the
publication owner keeps their original groups.
================
*/
async function produceSlotEffects() {
	const files = await publishImages( slotEffectRuntimeImageReferences );
	await registerSprites( slotEffectRuntimeImageReferences );
	return { files: [ ...files, SPRITE_CATALOG ], sheets: files };
}

/*
================
slotEffectPackFiles

Republish whichever catalog representations the index owns, both when both
are packed, so the publication owner keeps their original groups.
================
*/
function slotEffectPackFiles( output, index ) {
	const catalog = index.assets.filter( row => row.path === SPRITE_CATALOG || row.path === SPRITE_CATALOG + ".gz" )
		.map( row => row.path );
	return [ ...output.sheets, ...(catalog.length ? catalog : [ SPRITE_CATALOG ]) ];
}

/*
================
produceOverlays

Each folder contributes only the files its selector keeps. A <stem>.ddj.png
collision is published under its plain name.
================
*/
async function produceOverlays() {
	const folders = [
		{ folder: "/assets/images/Media_extracted/icon/", keep: file => file === "buf_effect.png" },
		{ folder: "/assets/images/Media_extracted/icon/stateodd/", keep: () => true },
		{
			folder: "/assets/images/Media_extracted/icon/etc/",
			keep: file => file.startsWith( "mark_" ) || file === "fort_jangan.png"
		},
		{
			folder: "/assets/images/Media_extracted/interface/ifcommon/",
			keep: file => file.startsWith( "quickparty_move_" ) || file.startsWith( "com_kindred_" )
		}
	];
	const files = [];
	for ( const { folder, keep } of folders ) {
		for ( const file of await readdir( convertedImageFolder( folder ) ) ) {
			if ( !file.endsWith( ".png" ) || file.endsWith( ".ddj.png" ) || !keep( file ) ) continue;
			files.push( await publishConvertedImage( folder + file ) );
		}
	}
	return { files };
}

/** @type {Record<string, LooseFamily>} */
export const LOOSE_FAMILIES = {
	// The characterInfo plane and the skill catalogue are published by one build
	// step as three files; files new to the index join game-data, the startup
	// group the full build puts them in.
	"character-info": {
		kind: "refresh",
		label: "native characterInfo files",
		packFolder: "character-info",
		defaultGroup: "game-data",
		async produce() {
			await buildSkillDataAsset();
			return {
				files: [
					"/assets/data/skillData.json.gz",
					"/assets/data/skillAudioData.json.gz",
					"/assets/data/characterActionData.json.gz"
				]
			};
		}
	},
	// The complete generated EFP dependency closure: updating a loose JSON file
	// alone leaves the packed copy stale.
	"effect": {
		kind: "refresh",
		label: "effect resources",
		packFolder: "effects",
		defaultGroup: file => file.endsWith( ".gz" ) ? "game-data" : "game-images",
		async produce() {
			const catalog = await readPublicJson( "/assets/effects/programs.json" );
			const records = await packedJson( [
				"/assets/skill/effectRecords.json",
				"/assets/skill/namedEffectRecords.json",
				"/assets/effects/programs.json"
			] );
			return { files: [ ...records, ...new Set( Object.values( catalog.textures ) ) ] };
		}
	},
	"entity-bsr": {
		kind: "refresh",
		label: "entity BSR dependencies",
		packFolder: "entity-bsr",
		defaultGroup: entityBsrGroup,
		produce: produceEntityBsr,
		packFiles: entityBsrPackFiles
	},
	// The renderer's terrain dependency must be published, not merely present in
	// the converted-image tree: only the sand and snow footstep decals.
	"footprint": {
		kind: "refresh",
		label: "terrain footprint textures",
		packFolder: "footprints",
		defaultGroup: "game-images",
		async produce() {
			const references = runtimeCifImageReferences.filter( file => FOOTPRINT_DDJ.test( file ) );
			if ( new Set( references ).size !== 2 ) throw new Error( "Footprint catalog must contain sand and snow" );
			return { files: await publishImages( references ) };
		}
	},
	// The guide reads both catalogs and the localized menu dictionary; publishing
	// only the catalogs leaves Help labels and item descriptions on an old
	// revision, so all four data files ride with the inline images.
	"guide": {
		kind: "refresh",
		label: "guide data files and inline images",
		packFolder: "guide",
		async produce() {
			await buildTextResources();
			buildQuestDataAsset();
			const images = await buildGuideImageResources();
			const data = await packedJson( [
				"/assets/data/event-guide-catalog.json",
				"/assets/data/questData.json",
				"/assets/text/texthelp.en.json",
				"/assets/text/textdataname.en.json"
			] );
			return {
				files: [ ...data, ...images.copiedImages ],
				defaultGroup: file => data.includes( file ) ? "game-data" : "native-ui"
			};
		}
	},
	// The executable creates the mall category controls outside resinfo.
	"item-mall": {
		kind: "refresh",
		label: "native Item Mall textures",
		packFolder: "item-mall",
		defaultGroup: "native-ui",
		async produce() {
			return { files: await publishImages( itemMallRuntimeImageReferences ) };
		}
	},
	"native-audio": {
		kind: "refresh",
		label: "native direct sounds",
		packFolder: "native-audio",
		defaultGroup: "game-audio",
		async produce() {
			const catalog = await buildNativeDirectSoundResources();
			return { files: [ ...new Set( catalog.rows.map( row => row.publicPath ) ) ] };
		}
	},
	// scripts/tools/refresh_native_window_images.py re-decodes the window DDJ
	// families whose RGB16 payloads the generic converter cannot express and
	// prints the public paths it wrote.
	"native-window": {
		kind: "refresh",
		label: "native RGB16 window textures",
		packFolder: "native-window",
		defaultGroup: "native-ui",
		async produce() {
			const output = execFileSync( pythonExecutable(), [ NATIVE_WINDOW_SCRIPT ], {
				encoding: "utf8",
				env: process.env
			} );
			return { files: JSON.parse( output ) };
		}
	},
	// The party member status icons, the fortress markers and the party control
	// buttons are chosen by code, not by any resinfo layout.
	"overlay": {
		kind: "refresh",
		label: "party status, fortress and party control images",
		packFolder: "overlays",
		defaultGroup: "native-ui",
		produce: produceOverlays
	},
	// The quick HP/MP gauges and the low-health alarm are loaded by code.
	"quick-status": {
		kind: "refresh",
		label: "quick status images and the native alarm sound",
		packFolder: "quick-status",
		async produce() {
			const files = [];
			for ( const kind of [ "hp", "mp" ] ) {
				files.push(
					await publishConvertedImage( `/assets/images/Media_extracted/interface/ifcommon/quick_${kind}.png` )
				);
			}
			const sound = await buildAlarmSoundResource();
			return {
				files: [ ...files, sound ],
				defaultGroup: file => file === sound ? "game-audio" : "native-ui"
			};
		}
	},
	// The quickslot bar, its skill-page button and the close buttons of both bar
	// orientations, in every button state.
	"quickslot": {
		kind: "refresh",
		label: "native quickslot textures",
		packFolder: "quickslots",
		defaultGroup: "native-ui",
		async produce() {
			return {
				files: await publishImages( [
					...quickslotRuntimeImageReferences,
					...buttonStates( "interface/skill/skl_button_up" ),
					...buttonStates( "interface/quick_slot/qsl_hclose_button" ),
					...buttonStates( "interface/quick_slot/qsl_vclose_button" )
				] )
			};
		}
	},
	// The English restriction notices completed in the UI system catalog, repacked
	// in the group that already owns it.
	"restriction-text": {
		kind: "refresh",
		label: "restriction notice catalog",
		packFolder: "restriction-text",
		async produce() {
			const catalog = await readPublicJson( "/assets/text/textuisystem.en.json" );
			completeRestrictionText( catalog.entries );
			await writeJsonIfChanged( publicFile( "/assets/text/textuisystem.en.json" ), catalog );
			return { files: await packedJson( [ "/assets/text/textuisystem.en.json" ] ) };
		}
	},
	// The return-scroll casting gauge and its cancel button in every state.
	"return-scroll": {
		kind: "refresh",
		label: "native return-scroll textures",
		packFolder: "return-scrolls",
		defaultGroup: "native-ui",
		async produce() {
			return {
				files: await publishImages( [
					...returnScrollRuntimeImageReferences,
					...buttonStates( "interface/ifcommon/com_casting_cancel" )
				] )
			};
		}
	},
	"slot-effect": {
		kind: "refresh",
		label: "item-slot effect sheets",
		packFolder: "slot-effects",
		defaultGroup: file => file === SPRITE_CATALOG ? "game-data" : "game-images",
		produce: produceSlotEffects,
		packFiles: slotEffectPackFiles
	},
	// CIFWorldMap_InitPageResources 576bd0 acquires its five marker sprites by
	// literal path, so neither resinfo\ifworldmap.txt nor the data-driven
	// worldmap_*.txt closure (refresh_world_map_asset_packs.mjs) reaches them.
	"world-map-markers": {
		kind: "refresh",
		label: "native world-map marker textures",
		packFolder: "world-map-markers",
		defaultGroup: "native-ui",
		async produce() {
			const files = await publishImages( worldMapMarkerRuntimeImageReferences );
			await registerSprites( worldMapMarkerRuntimeImageReferences );
			return { files };
		}
	},
	// The dungeon resource provider and every dungeon world built from it, packed
	// with their textures; files no group owns yet join the provider's group.
	"dungeon-worlds": {
		kind: "publish",
		label: "dungeon world files and textures",
		packFolder: "dungeon-world",
		defaultGroup: ( file, index ) => {
			const group = groupOf( index, DUNGEON_RESOURCE_PUBLIC_PATH ) ??
				groupOf( index, DUNGEON_RESOURCE_PUBLIC_PATH + ".gz" );
			if ( !group ) throw new Error( "Dungeon provider has no published pack owner" );
			return group;
		},
		async produce() {
			await buildDungeonResourceManifest();
			const providerFile = publicFile( DUNGEON_RESOURCE_PUBLIC_PATH );
			const provider = JSON.parse( await readFile( providerFile, "utf8" ) );
			const result = await buildDungeonWorlds( provider );
			const worlds = [ providerFile, ...result.files ];
			await refreshPrecompressedSidecars( worlds, { onlyWhenStale: true, ...WORLD_SIDECAR_LEVELS } );
			return {
				files: [ ...worlds.map( toPublic ).flatMap( name => [ name, name + ".gz" ] ), ...result.textures ],
				note: `(${result.files.length} regions, ${result.textures.length} textures)`
			};
		}
	},
	// The sky textures the retail sky references, and every published world's sky
	// pointed at the current flare set and star primitive; the textures join the
	// group that owns the sun texture.
	"flares": {
		kind: "publish",
		label: "flare and weather textures and world skies",
		packFolder: "live-flares",
		async produce() {
			const sky = resolveSkyTextures();
			await copyReferencedSkyImages( sky );
			const worlds = await rewriteWorldSkies( current => {
				if (
					JSON.stringify( current.flareTexturePublicPaths ) ===
						JSON.stringify( sky.flareTexturePublicPaths ) &&
					JSON.stringify( current.starPrimitive ) === JSON.stringify( sky.starPrimitive )
				) return false;
				current.flareTexturePublicPaths = sky.flareTexturePublicPaths;
				current.starPrimitive = sky.starPrimitive;
				return true;
			} );
			const textures = [
				...sky.flareTexturePublicPaths,
				...sky.textures.filter( row => row.role === "weather" ).map( row => row.publicPath )
			];
			return {
				files: [ ...worlds.published, ...textures ],
				worlds: worlds.published,
				textures,
				defaultGroup: ( file, index ) => {
					const group = groupOf( index, sky.sunTexturePublicPath );
					if ( !group ) throw new Error( "Sun texture has no authoritative pack group" );
					return group;
				},
				note: `(${worlds.changed} world skies updated)`
			};
		},
		packFiles: ( output, index ) => [ ...packedRepresentations( index, output.worlds ), ...output.textures ]
	},
	// The retail minimap tiles the mission dungeons use, and their coverage catalog.
	"minimap-coverage": {
		kind: "publish",
		label: "retail mission minimap coverage catalog",
		packFolder: "minimap-coverage",
		defaultGroup: "game-data",
		async produce() {
			await copyMissionMinimapTileImages();
			return { files: [ "/assets/data/mission-dungeon-minimap.json" ] };
		},
		// Every tile the catalog names must already be packed, or the minimap would
		// request a missing file.
		packFiles( output, index ) {
			const catalog = JSON.parse(
				readFileSync( publicFile( "/assets/data/mission-dungeon-minimap.json" ), "utf8" )
			);
			const packed = new Set( index.assets.map( row => row.path.toLowerCase() ) );
			const missing = catalog.tilePaths.filter( tile => !packed.has( tile.toLowerCase() ) );
			if ( missing.length > 0 ) throw new Error( `Retail minimap tile missing from publication: ${missing[0]}` );
			return output.files;
		}
	},
	// build-skill-ui.py projects the skill window data into assets/data/skillUi.json,
	// packed beside the skill mastery data it is read with.
	"skill-ui": {
		kind: "publish",
		label: "native skill window projection",
		packFolder: "skill-ui",
		defaultGroup: ( file, index ) => groupOf( index, SKILL_MASTERY_DATA ),
		async produce() {
			await runPython( [ SKILL_UI_SCRIPT ], { task: "Native skill UI projection" } );
			return { files: await packedJson( [ "/assets/data/skillUi.json" ] ) };
		}
	},
	// The native RNG state after the sky stars are constructed, so later draws
	// continue the retail sequence. The geometry must already match the producer.
	"star-rng": {
		kind: "publish",
		label: "star RNG continuation state",
		packFolder: "star-rng",
		async produce() {
			const primitive = buildNativeSkyStarPrimitive();
			const vertices = JSON.stringify( primitive.vertices );
			const worlds = await rewriteWorldSkies( ( sky, world, file ) => {
				const stars = sky.starPrimitive;
				if ( !stars ) return false;
				if (
					JSON.stringify( stars.vertices ) !== vertices ||
					stars.nativeRand?.seed !== primitive.nativeRand.seed
				) {
					throw new Error( `Star geometry does not match the continuation producer: ${file}` );
				}
				if (
					stars.nativeRand.stateAfterConstruction === primitive.nativeRand.stateAfterConstruction &&
					stars.nativeRand.calls === primitive.nativeRand.calls
				) return false;
				stars.nativeRand = {
					...stars.nativeRand,
					stateAfterConstruction: primitive.nativeRand.stateAfterConstruction,
					calls: primitive.nativeRand.calls
				};
				return true;
			} );
			return { files: worlds.published, note: `(${worlds.changed} world skies updated)` };
		},
		packFiles: ( output, index ) => packedRepresentations( index, output.files )
	},
	// The NPC models that play the rain event (skilleffect.txt) flagged in the NPC
	// manifest, and the weather sounds; a new weather sound joins game-audio.
	"weather-assets": {
		kind: "publish",
		label: "NPC rain-event flags and weather sounds",
		packFolder: "weather",
		defaultGroup: file => file.endsWith( ".wav" ) ? "game-audio" : undefined,
		async produce() {
			const flags = parseWeatherEvents(
				await readFile( path.join( retailTextdataRoot, "skilleffect.txt" ), "utf16le" )
			);
			const npcPath = publicFile( "/assets/npc/manifest.json" );
			const npc = JSON.parse( await readFile( npcPath, "utf8" ) );
			for ( const row of Object.values( npc.models ) ) row.eventRain = flags.get( row.codename ) ?? false;
			await publishBytesAtomically( npcPath, Buffer.from( JSON.stringify( npc ) ), {
				logLabel: "weather event metadata"
			} );
			await refreshPrecompressedSidecars( [ npcPath ], { onlyWhenStale: true } );
			const sounds = await buildWeatherSoundResources();
			return { files: [ "/assets/npc/manifest.json.gz", ...sounds ] };
		}
	}
};

/*
================
produceAllFamilies

The full build's family pass: every row's produce() in table order, each
recorded as its own ledger owner (family-<name>), after the builders whose
outputs the families extend and before the pack tail, which packs their
files with everything else. A fresh tree therefore needs no separate
`pnpm assets publish` and no second build.
================
*/
export async function produceAllFamilies( log = console.log ) {
	const results = [];
	for ( const [name, family] of Object.entries( LOOSE_FAMILIES ) ) {
		const startedAt = performance.now();
		const output = await withPublication( `family-${name}`, async () => {
			const produced = await family.produce( new Set() );
			claimPublicPaths( produced.files );
			return produced;
		} );
		const seconds = ((performance.now() - startedAt) / 1000).toFixed( 1 );
		log( `[families] ${name}: ${output.files.length} file(s) in ${seconds}s` );
		results.push( { name, files: output.files.length } );
	}
	return results;
}
