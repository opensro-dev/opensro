/*
===========================================================================

looseFamilies.mjs - the focused asset families and how each is produced

A loose family is a handful of public files the full asset build does not
reach (code-selected images, catalogs rebuilt by one step) that must still
be published through the packs. Each row here says how its files are
produced and which pack group a new file joins; scripts/refresh_asset_family.mjs
runs one row under the generated-assets lock and hands the result to the
one pack owner (shared/looseFamilyPublication.mjs).

Rows keep the native provenance that explains why the family exists. A new
family is a new row, never a new script.

===========================================================================
*/
import { execFileSync } from "node:child_process";
import { readdir, readFile } from "node:fs/promises";
import path from "node:path";
import { buildSkillStageModelAssets } from "../char/buildSkillStageModelAssets.mjs";
import { publishEntityBsrModifiers } from "../char/publishEntityBsrModifiers.mjs";
import { buildQuestDataAsset } from "../data/buildQuestDataAsset.mjs";
import { buildSkillDataAsset } from "../data/buildSkillDataAsset.mjs";
import { buildEffectProgramsAsset } from "../effects/buildEffectPrograms.mjs";
import { refreshPrecompressedSidecars } from "../generatedManifestSidecars.mjs";
import { buildAlarmSoundResource, buildNativeDirectSoundResources } from "../shared/audioResources.mjs";
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
import { pythonExecutable } from "../shared/pythonRun.mjs";
import { buildTextResources, completeRestrictionText } from "../shared/textResources.mjs";
import { publicRoot } from "../world/paths.mjs";

// The CIFButton state family (sub_5419c0) for the quickslot and return-scroll
// buttons; none of these ships a _disable.
const BUTTON_STATES = [ "", "_focus", "_press" ];
const SPRITE_CATALOG = "/assets/cif/cif-sprite-catalog.json";
const NATIVE_WINDOW_SCRIPT = path.join( import.meta.dirname, "..", "..", "tools", "refresh_native_window_images.py" );
const FOOTPRINT_DDJ = /^effect\/footstep_(sand|snow)\.ddj$/;

/**
 * @typedef {string | ((file: string) => string)} DefaultGroup
 * @typedef {{ files: string[], note?: string, defaultGroup?: DefaultGroup }} FamilyOutput
 * @typedef {{
 *   label: string,
 *   packFolder: string,
 *   defaultGroup?: DefaultGroup,
 *   produce: ( flags: Set<string> ) => Promise<FamilyOutput>
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
	const previous = await readPublicJson( "/assets/packs/manifest.json" );
	// Preserve and refresh every existing logical representation. VAT JSON was
	// originally packed without gzip; refreshing only its sidecar leaves clients
	// which request the plain logical path on the old source identity.
	const existingPaths = new Set( previous.assets.map( row => row.path ) );
	const jsonPaths = [ ...dependencies.filter( url => url.endsWith( ".json" ) ), "/assets/effects/programs.json" ];
	const files = [
		...new Set( [
			...dependencies.filter( url => !url.endsWith( ".json" ) ),
			...jsonPaths.flatMap( url => existingPaths.has( url ) ? [ url, url + ".gz" ] : [ url + ".gz" ] ),
			...Object.values( programs.textures )
		] )
	];
	return {
		files,
		note: flags.has( "--rebuilt-npc" ) ?
			"including rebuilt NPC model/VAT references" :
			"without rebuilding mesh/VAT payloads"
	};
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
	const previous = await readPublicJson( "/assets/packs/manifest.json" );
	const catalogFiles = previous.assets.filter( row =>
		row.path === SPRITE_CATALOG || row.path === SPRITE_CATALOG + ".gz"
	).map( row => row.path );
	return { files: [ ...files, ...(catalogFiles.length ? catalogFiles : [ SPRITE_CATALOG ]) ] };
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
		label: "entity BSR dependencies",
		packFolder: "entity-bsr",
		defaultGroup: entityBsrGroup,
		produce: produceEntityBsr
	},
	// The renderer's terrain dependency must be published, not merely present in
	// the converted-image tree: only the sand and snow footstep decals.
	"footprint": {
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
		label: "native Item Mall textures",
		packFolder: "item-mall",
		defaultGroup: "native-ui",
		async produce() {
			return { files: await publishImages( itemMallRuntimeImageReferences ) };
		}
	},
	"native-audio": {
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
		label: "party status, fortress and party control images",
		packFolder: "overlays",
		defaultGroup: "native-ui",
		produce: produceOverlays
	},
	// The quick HP/MP gauges and the low-health alarm are loaded by code.
	"quick-status": {
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
		label: "item-slot effect sheets",
		packFolder: "slot-effects",
		defaultGroup: file => file === SPRITE_CATALOG ? "game-data" : "game-images",
		produce: produceSlotEffects
	},
	// CIFWorldMap_InitPageResources 576bd0 acquires its five marker sprites by
	// literal path, so neither resinfo\ifworldmap.txt nor the data-driven
	// worldmap_*.txt closure (refresh_world_map_asset_packs.mjs) reaches them.
	"world-map-markers": {
		label: "native world-map marker textures",
		packFolder: "world-map-markers",
		defaultGroup: "native-ui",
		async produce() {
			const files = await publishImages( worldMapMarkerRuntimeImageReferences );
			await registerSprites( worldMapMarkerRuntimeImageReferences );
			return { files };
		}
	}
};
