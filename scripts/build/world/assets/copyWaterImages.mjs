/*
===========================================================================

copyWaterImages.mjs - native asset compilation

===========================================================================
*/
import { claimPublicFile } from "../../shared/publicationLedger.mjs";
import { copyIntoPublicTree } from "../../shared/publicWrite.mjs";
import { runPython } from "../../shared/pythonRun.mjs";
import { rebuildRoot } from "../paths.mjs";
import { copyFile, mkdir } from "node:fs/promises";
import path from "node:path";
import { toPublicImagePath } from "../../shared/assetPaths.mjs";
import { WATER_NORMAL_FRAME_COUNT } from "../constants.mjs";
import { exists } from "../io.mjs";
import { imagePublicRoot, imageSourceRoot } from "../paths.mjs";

/*
================
resolveWaterTextures
================
*/
export function resolveWaterTextures() {
	return {
		reflectionBumpPublicPath: waterImagePublicPath( "reflection-bump.ddj" ),
		normalFramePublicPaths: Array.from(
			{ length: WATER_NORMAL_FRAME_COUNT },
			( _, index ) => waterImagePublicPath( `water1${String( index + 1 ).padStart( 2, "0" )}.ddj` )
		),
		specialTexturePublicPath: waterImagePublicPath( "water201.ddj" ),
		waveTexturePublicPaths: [ 1, 2, 3 ].map( ( index ) => waterImagePublicPath( `wave${index}.ddj` ) )
	};
}

/*
================
copyReferencedWaterImages
================
*/
export async function copyReferencedWaterImages( waterTextures ) {
	const bumpTarget = path.join( imagePublicRoot, "Map_extracted", "water", "reflection-bump.png" );
	await mkdir( path.dirname( bumpTarget ), { recursive: true } );
	await runPython( [
		path.join( rebuildRoot, "scripts/build/world/assets/water_bump.py" ),
		path.join( imageSourceRoot, "Map_extracted", "skybox", "waterbump3.png" ),
		bumpTarget
	], { task: "native water bump conversion" } );
	// The Python step writes the bump map; this step owns it.
	claimPublicFile( bumpTarget );
	const publicPaths = [
		...waterTextures.normalFramePublicPaths,
		waterTextures.specialTexturePublicPath,
		...waterTextures.waveTexturePublicPaths
	];

	for ( const publicPath of publicPaths ) {
		const pngFileName = publicPath.split( "/" ).at( -1 );
		const source = path.join( imageSourceRoot, "Map_extracted", "water", pngFileName );
		const target = path.join( imagePublicRoot, "Map_extracted", "water", pngFileName );

		if ( !(await exists( source )) ) {
			throw new Error( `Missing converted water texture ${source}; run the DDJ image conversion first.` );
		}

		await copyIntoPublicTree( source, target );
	}
}

/*
================
waterImagePublicPath
================
*/
function waterImagePublicPath( ddjFileName ) {
	return toPublicImagePath( "Map_extracted/water", ddjFileName, { basename: true } );
}
