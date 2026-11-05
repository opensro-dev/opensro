/*
===========================================================================

missionDungeonMinimapCoverage.test.mjs - dungeon minimap floors ship
exactly the art the retail archive carries

The published mission dungeon minimap manifest must carry every
dungeoninfo admission row (readDungeonInfoRows appends the six Qin-Shi
Tomb floors when their retail DOFs are present), and a DOF floor label
must resolve to a shipped floor exactly when the retail archive ships
minimap tiles for that label. The manifest's tile inventory is the
build's retail inventory (retailMinimapArt refuses a converted tile
with no retail source and a retail tile left unconverted), so a label
with neither an art set nor a tile in that inventory is absent from
the retail archive itself: the client draws that dungeon without
floor art because v1.150 ships none (the Qin-Shi Tomb, #2572), not
because the build dropped it.

===========================================================================
*/
import assert from "node:assert/strict";
import path from "node:path";
import { test } from "node:test";

import { readDofFloorLabels } from "../../build/world/assets/copyMissionMinimapTileImages.mjs";
import { readDungeonInfoRows } from "../../build/world/assets/dungeonInfo.mjs";
import { dataExtractedRoot } from "../../build/world/paths.mjs";
import { readPublishedAssetJsonSync } from "../../lib/publishedAsset.mjs";

// The Qin-Shi Tomb dungeon sectors (Jangan Cave), region ids 7..2.
const QINSHI_TOMB_SECTOR_IDS = [ 0x8007, 0x8006, 0x8005, 0x8004, 0x8003, 0x8002 ];

/*
================
dofPathForRow

The DOF a dungeoninfo row names, resolved the way the manifest builder
resolves it (either separator, under Data_extracted).
================
*/
function dofPathForRow( dofName ) {
	return path.join( dataExtractedRoot, dofName.replaceAll( "\\", path.sep ).replaceAll( "/", path.sep ) );
}

test("every dungeon row ships, and a floor resolves exactly when retail art exists for its label", async () => {
	const manifest = readPublishedAssetJsonSync( "/assets/data/mission-dungeon-minimap.json" );
	const rows = await readDungeonInfoRows( path.join( dataExtractedRoot, "dungeon", "dungeoninfo.txt" ) );
	assert.ok( rows.length > 0, "dungeoninfo admission rows were read" );
	assert.equal( manifest.dungeons.length, rows.length, "the manifest carries every admission row" );

	const artByPrefix = new Map( manifest.artSets.map( art => [ art.prefix.toLowerCase(), art ] ) );
	const retailTiles = manifest.tilePaths.map( tile => tile.toLowerCase() );

	for ( const tombSector of QINSHI_TOMB_SECTOR_IDS ) {
		assert.ok(
			manifest.dungeons.some( dungeon => dungeon.sectorId === tombSector ),
			`Qin-Shi Tomb sector 0x${tombSector.toString( 16 )} is carried in the manifest`
		);
	}

	for ( const [index, row] of rows.entries() ) {
		const dungeon = manifest.dungeons[index];
		assert.equal( dungeon.regionId, row.regionId, `admission row ${index} region` );
		assert.equal( dungeon.dofName, row.dofName, `admission row ${index} DOF` );

		const labels = await readDofFloorLabels( dofPathForRow( row.dofName ) );
		const floorsByIndex = new Map( dungeon.floors.map( floor => [ floor.floorIndex, floor ] ) );
		let resolved = 0;
		for ( const [floorIndex, label] of labels.entries() ) {
			const prefix = label.trim().toLowerCase();
			const art = artByPrefix.get( prefix );
			const floor = floorsByIndex.get( floorIndex );
			if ( !art ) {
				assert.equal( floor, undefined, `${label} shipped a floor with no retail art set` );
				assert.ok(
					!retailTiles.some( tile =>
						tile.includes( "/minimap_d/" ) && path.posix.basename( tile ).startsWith( `${prefix}_` )
					),
					`${label} has tiles in the retail inventory but shipped no floor`
				);
				continue;
			}
			resolved += 1;
			assert.ok( floor, `${label} has retail art but shipped no floor` );
			assert.equal( floor.floorLabel, label, `${label} floor label` );
			assert.equal( floor.directory, art.directory, `${label} floor directory` );
			assert.equal( floor.prefix, art.prefix, `${label} floor prefix` );
			assert.equal( floor.tileCount, art.tileCount, `${label} floor tile count` );
			assert.deepEqual( floor.tiles, art.tiles, `${label} floor tiles` );
		}
		assert.equal( dungeon.floors.length, resolved, `${row.dofName} shipped floor count` );
	}
});
