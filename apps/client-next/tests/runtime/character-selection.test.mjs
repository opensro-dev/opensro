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
