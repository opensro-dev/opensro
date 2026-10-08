/*
===========================================================================

buildDropModelAssets.mjs - the itemdata ground-drop models as GLB

Mirrors buildNpcModelAssets.mjs: one GLB per distinct AssocFileDrop .bsr and a
manifest keyed by the normalized game path. The ground-item drop renderer
(ciItemDrawVisualSystem.ts) draws these models; the CIItem entity stays the data authority
(position/label/lifecycle), the GLB is twin-side visuals.

Native contract (CIItem_LoadModel 0x86DC10): a dropped item's
world model is its RefItemData+0x138 path - the itemdata AssocFileDrop
column (the LAST .bsr field of the row; equipment rows carry the
world/equip .bsr right before it). Gold heaps are the drop_ch_money_*
tiers, equipment shares drop_ch_equip, etc items drop_ch_bag - so the
whole itemdata resolves to a few dozen distinct models. All are baked;
files missing from Data_extracted are skipped loudly.

The idle clip (stateId-0, the model's single .ban - coin shimmer etc.) is
exported as the "stand" glTF animation, matching the native
CInterfaceModel_PlayAnimation(0, ...) loop and the NPC baker convention.

Output:
.generated/client-public/assets/itemdrop/<native path below res/>.glb
.generated/client-public/assets/itemdrop/manifest.json
(models keyed by "item/etc/drop_ch_bag.bsr"-style normalized paths)

Textures: .ddj under res/item are converted by scripts/convert_images.py;
reuse --skip-textures when they're in.

===========================================================================
*/

import { writeIntoPublicTreeSync } from "../shared/publicWrite.mjs";
import fs from "node:fs";
import path from "node:path";
import { compileBsrVisualToGlb } from "./compileBsrVisual.mjs";
import { dataAssetPath } from "../shared/jmxAssetIO.mjs";
import { convertTextureTrees } from "../shared/convertImagesRunner.mjs";
import { isMainScript } from "../shared/fsUtils.mjs";
import { listTextDataShardNamesSync, readTextDataLinesSync, splitTextDataRow } from "../shared/textDataIo.mjs";

export { assembleStaticBsrModel, compileBsrVisualToGlb } from "./compileBsrVisual.mjs";
import {
	claimResourceOutput,
	finalizeResourceGlbManifest,
	readPreviousResourceGlbPaths,
	resourceGlbOutput
} from "./resourceGlbOutput.mjs";
import { gameRoot, publicAssetsRoot, retailTextdataRoot } from "../world/paths.mjs";

const textdataDir = retailTextdataRoot;
const publicAssets = publicAssetsRoot;
/*
================
collectDropModels

Distinct AssocFileDrop values across itemdata*.txt (utf16le rows; the
drop model is the LAST .bsr field - referenceData.mjs reads the same).
Key = normalized game path ("item/etc/drop_ch_bag.bsr").
================
*/
function collectDropModels() {
	const files = listTextDataShardNamesSync( textdataDir, /^itemdata.*\.txt$/i );
	const models = new Set();
	for ( const file of files ) {
		for ( const line of readTextDataLinesSync( path.join( textdataDir, file ) ) ) {
			const cols = splitTextDataRow( line );
			let drop = "";
			for ( const col of cols ) {
				if ( /\.bsr$/i.test( col ) ) drop = col;
			}
			if ( drop ) models.add( drop.replaceAll( "\\", "/" ).toLowerCase() );
		}
	}
	// data_ccb98c / g_goldDropCoinModelPath: the gold-drop coin fanfare the
	// native CIItem_LoadModel fallback path (StaticInit_DropMoneyModelPath 0xBBCB60) -
	// referenced by address, not by any itemdata column.
	models.add( "item/etc/drop_ch_money_ing.bsr" );
	return [ ...models ].sort();
}

/*
================
buildDropModelAssets
================
*/
export async function buildDropModelAssets( { skipTextures = false } = {} ) {
	// The .bmt sets and .ddj textures live under prim/mtrl/item (the res/item
	// .bsr files are descriptors only).
	if ( !skipTextures ) await convertTextureTrees( "itemdrop", [ "prim/mtrl/item" ] );

	const dropModels = collectDropModels();
	const manifestPath = path.join( publicAssets, "itemdrop", "manifest.json" );
	const previousGlbPaths = readPreviousResourceGlbPaths( manifestPath );
	console.log( `[itemdrop] itemdata references ${dropModels.length} distinct drop models` );
	const models = {};
	const outputOwners = new Map();
	let built = 0;
	let absent = 0;
	for ( const gamePath of dropModels ) {
		const bsrPath = `res/${gamePath}`;
		const output = resourceGlbOutput( bsrPath, {
			namespace: "itemdrop",
			publicAssetsRoot: publicAssets
		} );
		claimResourceOutput( outputOwners, bsrPath, output.publicPath );
		if ( !fs.existsSync( dataAssetPath( bsrPath ) ) ) {
			absent += 1;
			console.warn( `[itemdrop] SKIP ${gamePath} (not in Data_extracted)` );
			continue;
		}
		const { publicPath, diskPath } = output;
		const entry = { bsr: bsrPath, glb: publicPath };
		try {
			// Skinned drop props (equip bundle, coin fanfare) ride the avatar
			// path; the common static bundles ride the one-joint stand-in.
			// The native PlayAnimation(0, ...) starts the DEFAULT-set STATE 0
			// clip; loop-vs-once is the BAN header loopType (0=OneShot plays once
			// and holds, 1=Cyclic loops) - resource-driven, not a caller flag.
			const { glb, clips, clipLoop, modifierSets, particleModifiers, materialModifiers, textureModifiers } =
				await compileBsrVisualToGlb( bsrPath );
			writeIntoPublicTreeSync( diskPath, glb );
			entry.bytes = glb.length;
			entry.clips = clips.map( ( c ) => c.role );
			// The runtime starts the clip with this loop mode (BAN loopType).
			entry.clipLoop = clipLoop;
			entry.modifierSets = modifierSets;
			entry.particleModifiers = particleModifiers;
			entry.materialModifiers = materialModifiers;
			entry.textureModifiers = textureModifiers;
			const stateZero = clips[0]?.clip;
			if ( stateZero ) {
				entry.stateZeroClip = {
					durationMs: stateZero.durationMs,
					fps: stateZero.field1,
					loop: clipLoop
				};
			}
			built += 1;
			console.log(
				`[itemdrop] OK   ${gamePath.padEnd( 44 )} -> ${publicPath} (${glb.length} B, clip=${
					clips.length ? `${clips[0].path.split( "/" ).pop()} ${clipLoop ? "cyclic" : "oneshot"}` : "none"
				})`
			);
		} catch ( error ) {
			entry.error = String( error?.message ?? error );
			console.warn( `[itemdrop] FAIL ${gamePath.padEnd( 44 )} ${entry.error}` );
		}
		models[gamePath] = entry;
	}

	const manifest = {
		format: "sro-mission-itemdrop-models",
		version: 3,
		source:
			"itemdata AssocFileDrop (last .bsr column; native RefItemData+0x138, the CIItem_LoadModel 0x86DC10 ground visual)",
		count: dropModels.length,
		builtCount: built,
		absentCount: absent,
		models
	};
	const removed = await finalizeResourceGlbManifest( {
		manifestPath,
		manifest,
		previousPublicPaths: previousGlbPaths,
		currentPublicPaths: Object.values( models ).map( ( model ) => model.glb ).filter( Boolean ),
		namespace: "itemdrop",
		publicAssetsRoot: publicAssets
	} );
	if ( removed.length > 0 ) {
		console.log( `[itemdrop] removed ${removed.length} superseded GLB output(s)` );
	}
	console.log( `[itemdrop] manifest -> ${manifestPath} (built ${built}/${dropModels.length})` );
	return {
		built,
		modelCount: dropModels.length,
		absent,
		removed: removed.length,
		manifestPath
	};
}

if ( isMainScript( import.meta.url ) ) {
	await buildDropModelAssets( { skipTextures: process.argv.includes( "--skip-textures" ) } );
}
