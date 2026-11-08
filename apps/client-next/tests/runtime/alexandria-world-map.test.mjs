/*
===========================================================================

alexandria-world-map.test.mjs - the server's SRO_ALEXANDRIA owns the M window's wider map

Port-only, not native. The wider overview and the Alexandria town pages a
variant build publishes are used only while the shard runs Alexandria: the
world-entry blob's "alexandria" switch reaches the gameplay snapshot, clears
on a native reentry, and is the only input that turns the wider map on.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";

const { createGameplay } = await import(
	sourceFileUrl( "src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts" ).href
);
const { expandedWorldMapOn } = await import( sourceFileUrl( "src/engine/foundation/ui/world-map.ts" ).href );

const POSE = {
	gid: 7,
	refObjId: 1907,
	kind: "player",
	name: "Tester",
	regionId: 0x6b4f,
	x: 60,
	y: 10,
	z: 100,
	heading: 0
};

/*
================
snapshotAfter

The gameplay snapshot after a world entry with the given blob.
================
*/
function snapshotAfter( game, blob ) {
	game.bootstrap( { simulationProtocolVersion: 1, ...blob } );
	game.seed( POSE );
	return game.take();
}

test("the server's Alexandria switch reaches the snapshot and clears on a native reentry", () => {
	const game = createGameplay( () => {} );
	assert.equal( snapshotAfter( game, { alexandria: true } )?.alexandria, true );
	assert.equal( snapshotAfter( game, {} )?.alexandria, undefined );
	// Only a real true turns it on.
	assert.equal( snapshotAfter( game, { alexandria: "true" } )?.alexandria, undefined );
	game.dispose();
});

test("the wider world map follows the server's switch and nothing else", () => {
	assert.equal( expandedWorldMapOn( { alexandria: true } ), true );
	for ( const game of [ undefined, null, {}, { alexandria: false }, { alexandria: 1 } ] ) {
		assert.equal( expandedWorldMapOn( game ), false, JSON.stringify( game ) );
	}
	const game = createGameplay( () => {} );
	assert.equal( expandedWorldMapOn( snapshotAfter( game, { alexandria: true } ) ), true );
	assert.equal( expandedWorldMapOn( snapshotAfter( game, {} ) ), false );
	game.dispose();
});
