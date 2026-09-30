/*
===========================================================================

refresh_entity_bsr_asset_packs.mjs - publish entity BSR dependencies

Republishes the entity BSR modifier manifests and the effect programs they
reference. --skillfx adds the skill stage models; --rebuilt-npc adds the NPC
models, material variants and VAT payloads a mesh rebuild produced.

===========================================================================
*/
import { readFile } from "node:fs/promises";
import path from "node:path";
import { buildSkillStageModelAssets } from "./build/char/buildSkillStageModelAssets.mjs";
import { publishEntityBsrModifiers } from "./build/char/publishEntityBsrModifiers.mjs";
import { buildEffectProgramsAsset } from "./build/effects/buildEffectPrograms.mjs";
import { refreshPrecompressedSidecars } from "./build/generatedManifestSidecars.mjs";
import { publishLooseFamily } from "./build/shared/looseFamilyPublication.mjs";
import { publicRoot } from "./build/world/paths.mjs";
import { withGeneratedAssetsLock } from "./rebuildLock.mjs";

const WITH_SKILLFX = process.argv.includes( "--skillfx" );
const WITH_REBUILT_NPC = process.argv.includes( "--rebuilt-npc" );

/*
================
readPublicJson
================
*/
async function readPublicJson( publicPath ) {
	return JSON.parse( await readFile( path.join( publicRoot, publicPath ), "utf8" ) );
}

/*
================
defaultGroup
================
*/
function defaultGroup( file ) {
	if ( file.endsWith( ".gz" ) ) return "game-data";
	if ( file.endsWith( ".glb" ) || file.endsWith( ".vat.bin" ) ) return "game-models";
	return "game-images";
}

await withGeneratedAssetsLock( "Entity BSR dependency publication", async () => {
	const manifests = await publishEntityBsrModifiers();
	if ( WITH_SKILLFX ) {
		await buildSkillStageModelAssets();
		const stage = await readPublicJson( "assets/skillfx/manifest.json" );
		manifests.push( "/assets/skillfx/manifest.json", ...Object.values( stage.models ).map( row => row.glb ) );
	}
	await buildEffectProgramsAsset();
	if ( WITH_REBUILT_NPC ) {
		const npc = await readPublicJson( "assets/npc/manifest.json" );
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
	console.log( `[entity-bsr-packs] Refreshing compressed sidecars for ${dependencies.length} dependencies.` );
	await refreshPrecompressedSidecars( dependencies.map( url => path.join( publicRoot, url ) ), {
		onlyWhenStale: true
	} );
	const programs = await readPublicJson( "assets/effects/programs.json" );
	const previous = await readPublicJson( "assets/packs/manifest.json" );
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
	await publishLooseFamily( { name: "entity-bsr", files, defaultGroup } );
	console.log(
		`Published ${files.length} entity BSR dependencies` +
			(WITH_REBUILT_NPC ?
				" including rebuilt NPC model/VAT references." :
				" without rebuilding mesh/VAT payloads.")
	);
} );
