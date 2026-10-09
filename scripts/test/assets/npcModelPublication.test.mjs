import { CLIENT_PUBLIC_ROOT } from "../../lib/generatedRoot.mjs";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { test } from "node:test";
import { readPublishedAssetJson } from "../../lib/publishedAsset.mjs";
import { npcManifestModels } from "../../build/shared/npcManifest.mjs";

test("every NPC model and authored material variant has exactly one published pack owner", async () => {
	const catalog = await readPublishedAssetJson( "/assets/npc/manifest.json" );
	const index = JSON.parse( await readFile( CLIENT_PUBLIC_ROOT + "/assets/packs/manifest.json", "utf8" ) );
	const owners = new Map();
	for ( const entry of index.assets ) {
		const key = entry.path.toLowerCase();
		const rows = owners.get( key ) ?? [];
		rows.push( entry );
		owners.set( key, rows );
	}
	const packs = new Set( index.groups.flatMap( group => group.packs.map( pack => pack.path ) ) );
	const failures = [];
	for ( const [model, row] of Object.entries( npcManifestModels( catalog ) ) ) {
		if ( row.error ) continue;
		for ( const file of [ row.glb, ...Object.values( row.materialVariants ?? {} ) ] ) {
			const entries = owners.get( file?.toLowerCase() ) ?? [];
			if ( entries.length !== 1 || !packs.has( entries[0]?.packPath ) ) {
				failures.push( { model, file, owners: entries.length } );
			}
		}
	}
	assert.deepEqual( failures, [], "NPC catalogue references must be published before world admission" );
});

test("quick status fills and the directly requested alarm sound are published", async () => {
	const index = JSON.parse( await readFile( CLIENT_PUBLIC_ROOT + "/assets/packs/manifest.json", "utf8" ) );
	for (
		const file of [
			"/assets/images/Media_extracted/interface/ifcommon/quick_hp.png",
			"/assets/images/Media_extracted/interface/ifcommon/quick_mp.png",
			"/assets/audio/sfx/prim/snd/ui/alarm_sound.wav"
		]
	) {
		assert.equal( index.assets.filter( row => row.path === file ).length, 1, file );
	}
});
