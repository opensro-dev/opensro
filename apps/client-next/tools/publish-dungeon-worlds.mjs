/*
===========================================================================

publish-dungeon-worlds.mjs - the dungeon rendering worlds

Builds the dungeon resource provider and every dungeon world from it, then
packs the provider, the worlds (plain and gzip) and their textures. Files no
group owns yet join the group that owns the provider.

===========================================================================
*/
import fs from "node:fs/promises";
import path from "node:path";
import { refreshPrecompressedSidecars } from "../../../scripts/build/generatedManifestSidecars.mjs";
import { publishLooseFamily } from "../../../scripts/build/shared/looseFamilyPublication.mjs";
import {
	buildDungeonResourceManifest,
	DUNGEON_RESOURCE_PUBLIC_PATH
} from "../../../scripts/build/world/assets/buildDungeonResources.mjs";
import { buildDungeonWorlds } from "../../../scripts/build/world/assets/buildDungeonWorlds.mjs";
import { publicRoot } from "../../../scripts/build/world/paths.mjs";
import { withGeneratedAssetsLock } from "../../../scripts/rebuildLock.mjs";

// Lighter sidecar levels: these files are large and rebuilt often.
const SIDECAR_LEVELS = { brotliQuality: 4, gzipLevel: 3, zstdLevel: 3 };

/*
================
providerGroup
================
*/
function providerGroup( file, previous ) {
	const group = previous.assets.find( row =>
		row.path === DUNGEON_RESOURCE_PUBLIC_PATH || row.path === DUNGEON_RESOURCE_PUBLIC_PATH + ".gz"
	)?.group;
	if ( !group ) throw new Error( "Dungeon provider has no published pack owner" );
	return group;
}

await withGeneratedAssetsLock( "dungeon rendering publication", async () => {
	await buildDungeonResourceManifest();
	const providerFile = path.join( publicRoot, DUNGEON_RESOURCE_PUBLIC_PATH );
	const provider = JSON.parse( await fs.readFile( providerFile, "utf8" ) );
	const result = await buildDungeonWorlds( provider );
	const files = [ providerFile, ...result.files ];
	await refreshPrecompressedSidecars( files, { onlyWhenStale: true, ...SIDECAR_LEVELS } );
	const logical = files.flatMap( file => {
		const name = "/" + path.relative( publicRoot, file ).replaceAll( "\\", "/" );
		return [ name, name + ".gz" ];
	} );
	const updates = await publishLooseFamily( {
		name: "dungeon-world",
		files: [ ...logical, ...result.textures ],
		defaultGroup: providerGroup
	} );
	console.log( JSON.stringify( {
		regions: result.files.length,
		textures: result.textures.length,
		packs: updates.reduce( ( count, update ) => count + update.builtPackCount, 0 )
	} ) );
} );
