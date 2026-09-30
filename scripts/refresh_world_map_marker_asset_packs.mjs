/*
===========================================================================

refresh_world_map_marker_asset_packs.mjs - publish the world-map markers

CIFWorldMap_InitPageResources 576bd0 acquires its five marker sprites by
literal path, so neither resinfo\ifworldmap.txt nor the data-driven
worldmap_*.txt closure (refresh_world_map_asset_packs.mjs) can reach them.
They ride the native-ui group with the rest of the code-selected CIF art.

===========================================================================
*/
import { readFile, writeFile } from "node:fs/promises";
import path from "node:path";
import { refreshPrecompressedSidecars } from "./build/generatedManifestSidecars.mjs";
import { imagePublicPath } from "./build/shared/cifResources.mjs";
import { worldMapMarkerRuntimeImageReferences } from "./build/shared/cifRuntimeImageCatalog.mjs";
import { publishConvertedImage } from "./build/shared/convertedImages.mjs";
import { publishLooseFamily } from "./build/shared/looseFamilyPublication.mjs";
import { publicRoot } from "./build/world/paths.mjs";
import { withGeneratedAssetsLock } from "./rebuildLock.mjs";

// PNG signature plus IHDR: width and height are big-endian at 16 and 20.
const PNG_HEADER_BYTES = 24;
const PNG_WIDTH_OFFSET = 16;
const PNG_HEIGHT_OFFSET = 20;

await withGeneratedAssetsLock( "Native world-map marker texture publication", async () => {
	const files = [];
	for ( const reference of worldMapMarkerRuntimeImageReferences ) {
		files.push( await publishConvertedImage( imagePublicPath( reference ) ) );
	}
	// buildCifResources registers every runtime reference in the shared sprite
	// catalog; a targeted publication has to keep that catalog closed too, or
	// cifSpriteCatalog.test.mjs reports a published sprite with no dimensions.
	const catalogPath = path.join( publicRoot, "assets", "cif", "cif-sprite-catalog.json" );
	const catalog = JSON.parse( await readFile( catalogPath, "utf8" ) );
	for ( const reference of worldMapMarkerRuntimeImageReferences ) {
		const publicPath = imagePublicPath( reference );
		const png = await readFile( path.join( publicRoot, publicPath.slice( 1 ) ) );
		if ( png.length < PNG_HEADER_BYTES || png[0] !== 0x89 || png.subarray( 1, 4 ).toString( "ascii" ) !== "PNG" ) {
			throw new Error( `World-map marker sprite is not a PNG: ${publicPath}` );
		}
		catalog.resourcesByDdjPath[reference] = {
			sourcePath: reference,
			publicPath,
			width: png.readUInt32BE( PNG_WIDTH_OFFSET ),
			height: png.readUInt32BE( PNG_HEIGHT_OFFSET )
		};
	}
	await writeFile( catalogPath, JSON.stringify( catalog ) );
	// The browser is served the precompressed representation, so a rewritten
	// catalog with stale .br/.gz/.zst sidecars would hide the new sprites.
	await refreshPrecompressedSidecars( [ catalogPath ] );
	await publishLooseFamily( { name: "world-map-markers", files, defaultGroup: "native-ui" } );
	console.log( `Published ${files.length} native world-map marker textures.` );
} );
