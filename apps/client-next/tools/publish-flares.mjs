/*
===========================================================================

publish-flares.mjs - the live sky flare and weather textures

Copies the sky textures the retail sky references and points every published
world's sky at the current flare set and star primitive. The world files
are validated as a set before any is replaced, and each is repacked with the
representations (plain, gzip) its group already holds; the textures join the
group that owns the sun texture.

===========================================================================
*/
import fs from "node:fs/promises";
import path from "node:path";
import { refreshPrecompressedSidecars } from "../../../scripts/build/generatedManifestSidecars.mjs";
import { publishBytesAtomically } from "../../../scripts/build/shared/atomicPublish.mjs";
import { publishLooseFamily } from "../../../scripts/build/shared/looseFamilyPublication.mjs";
import { copyReferencedSkyImages, resolveSkyTextures } from "../../../scripts/build/world/assets/copySkyImages.mjs";
import { publicRoot } from "../../../scripts/build/world/paths.mjs";
import { withGeneratedAssetsLock } from "../../../scripts/rebuildLock.mjs";
import { packedWorldFiles, SIDECAR_LEVELS, WORLD_ROOT } from "./worldSkyPublication.mjs";

await withGeneratedAssetsLock( "live flare texture publication", async () => {
	const sky = resolveSkyTextures();
	await copyReferencedSkyImages( sky );
	console.log( "Flare sources copied; checking published world references." );
	const changes = [], published = [];
	for ( const name of await fs.readdir( WORLD_ROOT, { recursive: true } ) ) {
		if ( !name.endsWith( ".json" ) ) continue;
		const file = path.join( WORLD_ROOT, name );
		const value = JSON.parse( await fs.readFile( file, "utf8" ) );
		const current = value.sky;
		if ( !current ) continue;
		published.push( file );
		if (
			JSON.stringify( current.flareTexturePublicPaths ) === JSON.stringify( sky.flareTexturePublicPaths ) &&
			JSON.stringify( current.starPrimitive ) === JSON.stringify( sky.starPrimitive )
		) continue;
		current.flareTexturePublicPaths = sky.flareTexturePublicPaths;
		current.starPrimitive = sky.starPrimitive;
		changes.push( [ file, JSON.stringify( value ) ] );
	}
	// Validate the entire publication set before replacing any asset.
	for ( const [file, json] of changes ) await publishBytesAtomically( file, Buffer.from( json ) );
	// Include unchanged logical files: this also repairs an interrupted publication
	// between JSON replacement and sidecar/pack commit without rewriting geometry.
	console.log( JSON.stringify( { phase: "world-sidecars", changed: changes.length, verified: published.length } ) );
	await refreshPrecompressedSidecars( published, { onlyWhenStale: true, ...SIDECAR_LEVELS } );
	const index = JSON.parse(
		await fs.readFile( path.join( publicRoot, "assets", "packs", "manifest.json" ), "utf8" )
	);
	const sunGroup = index.assets.find( row => row.path === sky.sunTexturePublicPath )?.group;
	if ( !sunGroup ) throw new Error( "Sun texture has no authoritative pack group" );
	const textures = [
		...sky.flareTexturePublicPaths,
		...sky.textures.filter( row => row.role === "weather" ).map( row => row.publicPath )
	];
	const updates = await publishLooseFamily( {
		name: "live-flares",
		files: [ ...packedWorldFiles( index, published ), ...textures ],
		defaultGroup: sunGroup
	} );
	console.log( JSON.stringify( {
		updated: changes.length,
		verified: published.length,
		packsBuilt: updates.reduce( ( count, update ) => count + update.builtPackCount, 0 ),
		textures: sky.flareTexturePublicPaths
	} ) );
} );
