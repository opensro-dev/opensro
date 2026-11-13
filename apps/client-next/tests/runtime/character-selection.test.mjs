/*
===========================================================================

character-selection.test.mjs - tests for character-selection.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import fc from "fast-check";

const { createCharacterSelection } = await import(
	sourceFileUrl( "src/engine/foundation/animation/character-selection.ts" ).href
);

test("arena reservation retains all fifty enemies under a saturated spell-effect budget", () => {
	const entities = Array.from(
		{ length: 51 },
		( _, i ) => ({ gid: i + 1, kind: i ? "monster" : "local-player", regionId: 17989, x: i * 4, y: 31, z: 914 })
	);
	const owner = createCharacterSelection( 512, () => Array( 512 ).fill( 0 ) );
	assert.equal(
		owner.select( entities, entities[0], 1, undefined ).length,
		1,
		"shared effects reproduce invisible enemies"
	);
	assert.equal(
		owner.select( entities, entities[0], 1, undefined, undefined, true ).length,
		51,
		"arena reserves visible, pickable enemy bodies first"
	);
	assert.equal(
		createCharacterSelection( 12 ).select( entities, entities[0], 1, undefined, undefined, true ).length,
		12,
		"renderer cap remains enforced"
	);
});

test("arena enemies and power eggs remain visible before dense tenfold loot", () => {
	const entities = Array.from(
		{ length: 51 },
		( _, i ) => ({ gid: i + 1, kind: i ? "monster" : "local-player", regionId: 17989, x: 100 + i, y: 31, z: 914 })
	);
	const loot = Array.from(
		{ length: 600 },
		( _, i ) => ({
			gid: 1000 + i,
			refObjId: 99,
			kind: "npc",
			groundItem: {},
			regionId: 17989,
			x: 101,
			y: 31,
			z: 914
		})
	);
	const egg = { ...loot[0], gid: 2000, refObjId: 9272 };
	const selected = createCharacterSelection( 512, () => Array( 512 ).fill( 0 ) ).select(
		[ ...entities, ...loot, egg ],
		entities[0],
		1,
		undefined,
		undefined,
		true
	);
	assert.equal( selected.length, 512 );
	for ( const e of [ ...entities, egg ] ) {
		assert.ok( selected.includes( e ), `live encounter/power ${e.gid} was evicted by loot` );
	}
});
function oracle( entities, anchor, local, mount, limit ) {
	const priority = e => e.gid === local ? 0 : e.gid === mount ? 1 : 2;
	const distance = e =>
		anchor ?
			(e.x + ((e.regionId & 255) - (anchor.regionId & 255)) * 1920 - anchor.x) ** 2 +
			(e.z + ((e.regionId >>> 8) - (anchor.regionId >>> 8)) * 1920 - anchor.z) ** 2 :
			0;
	return entities.map( entity => ({ entity, p: priority( entity ), d: distance( entity ) }) ).sort( ( a, b ) =>
		a.p - b.p || a.d - b.d || a.entity.gid - b.entity.gid
	).slice( 0, limit ).map( r => r.entity );
}
test("retained selection matches admission after movement, topology, mount and non-pose updates", () => {
	const row = fc.record( {
		gid: fc.integer( { min: 1, max: 40 } ),
		regionId: fc.integer( { min: 24000, max: 25000 } ),
		x: fc.integer( { min: 0, max: 1920 } ),
		z: fc.integer( { min: 0, max: 1920 } )
	} );
	fc.assert(
		fc.property(
			fc.array( fc.uniqueArray( row, { selector: r => r.gid, maxLength: 40 } ), { minLength: 1, maxLength: 20 } ),
			frames => {
				const owner = createCharacterSelection( 12 );
				for ( let i = 0; i < frames.length; i++ ) {
					const entities = frames[i],
						anchor = i % 3 ? entities[0] : undefined,
						local = entities.at( -1 )?.gid,
						mount = entities[1]?.gid;
					assert.deepEqual(
						owner.select( entities, anchor, local, mount ),
						oracle( entities, anchor, local, mount, 12 )
					);
					const replaced = entities.map( e => ({ ...e, health: i }) );
					const result = owner.select( replaced, anchor, local, mount );
					assert.deepEqual( result, oracle( replaced, anchor, local, mount, 12 ) );
					for ( const entity of result ) {
						assert.ok( replaced.includes( entity ) );
					}
					if ( entities[0] ) entities[0].x += 300;
					assert.deepEqual(
						owner.select( entities, anchor, local, mount ),
						oracle( entities, anchor, local, mount, 12 )
					);
					const admit = e => e.gid % 2 === 1;
					assert.deepEqual(
						owner.select( entities, anchor, local, mount, admit ),
						oracle( entities.filter( admit ), anchor, local, mount, 12 )
					);
				}
				owner.reset();
				assert.deepEqual( owner.select( [], undefined, undefined, undefined ), [] );
			}
		),
		{ seed: 2402026, numRuns: 1000 }
	);
});
