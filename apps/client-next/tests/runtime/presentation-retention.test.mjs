/*
===========================================================================

presentation-retention.test.mjs - tests for presentation.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";

const { createPresentation } = await import( sourceFileUrl( "src/engine/runtime/presentation/presentation.ts" ).href );
test("unrelated journal events retain entity rows; changed entities invalidate them atomically", () => {
	const p = createPresentation(), entity = { gid: 1, x: 1, y: 0, z: 0, regionId: 1 };
	p.apply( { sequence: 1, events: [ { kind: "reset", epoch: 1 }, { kind: "spawn", entity } ] } );
	const before = p.entities();
	p.apply( { sequence: 2, events: [ { kind: "orb-gauge", value: 3 }, { kind: "synchronized", epoch: 1 } ] } );
	assert.equal( p.entities(), before );
	assert.equal( p.takeFeedback()[0].value, 3 );
	p.apply( { sequence: 3, events: [ { kind: "gameplay", state: { target: 1 } } ] } );
	assert.equal( p.entities(), before );
	assert.throws( () =>
		p.apply( {
			sequence: 4,
			events: [ { kind: "state", entity: { ...entity, x: 2 } }, { kind: "spawn", entity } ]
		} )
	);
	assert.equal( p.entities(), before );
	assert.equal( p.read( 1 ).x, 1 );
	p.apply( { sequence: 4, events: [ { kind: "state", entity: { ...entity, x: 2 } } ] } );
	assert.notEqual( p.entities(), before );
	assert.equal( p.read( 1 ).x, 2 );
	assert.equal( before[0].x, 1 );
	const changed = p.entities();
	p.apply( { sequence: 5, events: [ { kind: "despawn", gid: 999 } ] } );
	assert.equal( p.entities(), changed );
	p.apply( { sequence: 6, events: [ { kind: "reset", epoch: 2 } ] } );
	assert.deepEqual( p.entities(), [] );
	assert.equal( changed[0].x, 2 );
	p.dispose();
});

test("the skill catalog is indexed once and the index follows the catalog across snapshots", () => {
	const p = createPresentation(), catalog = [ { id: 3, group: 1 }, { id: 124, group: 2 } ];
	p.apply( {
		sequence: 1,
		events: [ { kind: "reset", epoch: 1 }, { kind: "gameplay", state: { skillCatalog: catalog } } ]
	} );
	const index = p.gameplay().skillIndex;
	assert.equal( index.get( 124 ), catalog[1] );
	assert.equal( index.get( 999 ), undefined );
	// A later snapshot omits the catalog: both it and its index are retained.
	p.apply( { sequence: 2, events: [ { kind: "gameplay", state: { target: 5 } } ] } );
	assert.equal( p.gameplay().skillCatalog, catalog );
	assert.equal( p.gameplay().skillIndex, index );
	// A replacement catalog gets its own index.
	const replacement = [ { id: 7, group: 3 } ];
	p.apply( { sequence: 3, events: [ { kind: "gameplay", state: { skillCatalog: replacement } } ] } );
	assert.equal( p.gameplay().skillIndex.get( 7 ), replacement[0] );
	assert.equal( p.gameplay().skillIndex.get( 124 ), undefined );
	p.dispose();
});
