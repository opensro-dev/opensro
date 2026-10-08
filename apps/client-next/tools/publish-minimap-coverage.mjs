/*
===========================================================================

publish-minimap-coverage.mjs - the retail mission-dungeon minimap coverage

Copies the retail minimap tiles the mission dungeons use and publishes their
coverage catalog in game-data. Every tile the catalog names must already be
published, or the minimap would request a missing file.

===========================================================================
*/
import { readFile } from "node:fs/promises";
import path from "node:path";
import { publishLooseFamily } from "../../../scripts/build/shared/looseFamilyPublication.mjs";
import { copyMissionMinimapTileImages } from "../../../scripts/build/world/assets/copyMissionMinimapTileImages.mjs";
import { publicRoot } from "../../../scripts/build/world/paths.mjs";
import { withGeneratedAssetsLock } from "../../../scripts/rebuildLock.mjs";

await withGeneratedAssetsLock( "retail minimap coverage publication", async () => {
	await copyMissionMinimapTileImages();
	const index = JSON.parse( await readFile( path.join( publicRoot, "assets", "packs", "manifest.json" ), "utf8" ) );
	const catalog = JSON.parse(
		await readFile( path.join( publicRoot, "assets", "data", "mission-dungeon-minimap.json" ), "utf8" )
	);
	const published = new Set( index.assets.map( row => row.path.toLowerCase() ) );
	for ( const tile of catalog.tilePaths ) {
		if ( !published.has( tile.toLowerCase() ) ) {
			throw new Error( `Retail minimap tile missing from publication: ${tile}` );
		}
	}
	await publishLooseFamily( {
		name: "minimap-coverage",
		owner: "minimap-coverage",
		files: [ "/assets/data/mission-dungeon-minimap.json" ],
		defaultGroup: "game-data"
	} );
	console.log( "Retail minimap coverage published through the asset pack authority." );
} );
