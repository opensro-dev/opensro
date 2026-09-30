/*
===========================================================================

publish-star-rng.mjs - the native star RNG continuation state

Every published world's sky carries the star primitive; this records the
native RNG state after the stars are constructed, so later draws continue
the retail sequence. Geometry must already match the producer. The world
files are validated as a set before any is replaced, and each is repacked
with the representations its group already holds.

===========================================================================
*/
import fs from "node:fs/promises";
import path from "node:path";
import { refreshPrecompressedSidecars } from "../../../scripts/build/generatedManifestSidecars.mjs";
import { publishBytesAtomically } from "../../../scripts/build/shared/atomicPublish.mjs";
import { publishLooseFamily } from "../../../scripts/build/shared/looseFamilyPublication.mjs";
import { buildNativeSkyStarPrimitive } from "../../../scripts/build/world/assets/copySkyImages.mjs";
import { publicRoot } from "../../../scripts/build/world/paths.mjs";
import { withGeneratedAssetsLock } from "../../../scripts/rebuildLock.mjs";
import { packedWorldFiles, SIDECAR_LEVELS, WORLD_ROOT } from "./worldSkyPublication.mjs";

await withGeneratedAssetsLock( "star RNG continuation publication", async () => {
	const primitive = buildNativeSkyStarPrimitive();
	const vertices = JSON.stringify( primitive.vertices );
	const changes = [], published = [];
	for ( const name of await fs.readdir( WORLD_ROOT, { recursive: true } ) ) {
		if ( !name.endsWith( ".json" ) ) continue;
		const file = path.join( WORLD_ROOT, name );
		const value = JSON.parse( await fs.readFile( file, "utf8" ) );
		const stars = value.sky?.starPrimitive;
		if ( !stars ) continue;
		if ( JSON.stringify( stars.vertices ) !== vertices || stars.nativeRand?.seed !== primitive.nativeRand.seed ) {
			throw new Error( `Star geometry does not match the continuation producer: ${file}` );
		}
		published.push( file );
		if (
			stars.nativeRand.stateAfterConstruction === primitive.nativeRand.stateAfterConstruction &&
			stars.nativeRand.calls === primitive.nativeRand.calls
		) continue;
		stars.nativeRand = {
			...stars.nativeRand,
			stateAfterConstruction: primitive.nativeRand.stateAfterConstruction,
			calls: primitive.nativeRand.calls
		};
		changes.push( [ file, JSON.stringify( value ) ] );
	}
	// Validate the entire publication set before replacing any asset.
	for ( const [file, json] of changes ) await publishBytesAtomically( file, Buffer.from( json ) );
	// Include unchanged logical files: this also repairs an interrupted publication
	// between JSON replacement and sidecar/pack commit without rewriting geometry.
	await refreshPrecompressedSidecars( published, { onlyWhenStale: true, ...SIDECAR_LEVELS } );
	const index = JSON.parse(
		await fs.readFile( path.join( publicRoot, "assets", "packs", "manifest.json" ), "utf8" )
	);
	const updates = await publishLooseFamily( { name: "star-rng", files: packedWorldFiles( index, published ) } );
	console.log( JSON.stringify( {
		updated: changes.length,
		verified: published.length,
		packsBuilt: updates.reduce( ( count, update ) => count + update.builtPackCount, 0 ),
		random: primitive.nativeRand
	} ) );
} );
