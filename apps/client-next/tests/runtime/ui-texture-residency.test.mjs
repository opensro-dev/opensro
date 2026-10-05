/*
===========================================================================

ui-texture-residency.test.mjs - tests for ui-texture-residency.ts

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

const { uiTextureResidency } = await import(
	sourceFileUrl( "src/engine/foundation/rendering/ui-texture-residency.ts" ).href
);
test("preloaded hidden windows never consume GPU descriptors; draw masks do", () => {
	const available = new Set( Array.from( { length: 1000 }, ( _, i ) => String( i ) ) ), dirty = new Set( available );
	const scene = { quads: [ { texture: "1", mask: { texture: "2" } }, { texture: "__portrait" }, { texture: "" } ] };
	const first = uiTextureResidency( scene, available, new Set(), dirty );
	assert.deepEqual( first.upload, [ "1", "2" ] );
	assert.equal( first.needed.size, 2 );
	const same = uiTextureResidency( scene, available, first.needed, new Set() );
	assert.deepEqual( same.upload, [] );
	const next = uiTextureResidency( { quads: [ { texture: "3" } ] }, available, first.needed, new Set() );
	assert.deepEqual( next.release, [ "1", "2" ] );
	assert.deepEqual( next.upload, [ "3" ] );
	available.delete( "3" );
	assert.deepEqual(
		uiTextureResidency( { quads: [ { texture: "3" } ] }, available, next.needed, new Set() ).release,
		[ "3" ]
	);
});
test("a scene's live textures stay resident with the scene, never without one", () => {
	const available = new Set( [ "1", "digit" ] ), scene = { quads: [ { texture: "1" } ] };
	const shown = uiTextureResidency( scene, available, new Set(), new Set(), [ "digit", "missing" ] );
	assert.deepEqual( [ ...shown.needed ].sort(), [ "1", "digit" ] );
	assert.deepEqual( uiTextureResidency( null, available, shown.needed, new Set(), [ "digit" ] ).release.sort(), [
		"1",
		"digit"
	] );
	assert.deepEqual( uiTextureResidency( scene, available, shown.needed, new Set() ).release, [ "digit" ] );
});
test("repeated window transitions, resource replacement and device loss retain exact GPU demand", () => {
	fc.assert(
		fc.property(
			fc.array( fc.tuple( fc.uniqueArray( fc.integer( { min: 0, max: 20 } ) ), fc.boolean() ), {
				maxLength: 100
			} ),
			events => {
				let resident = new Set();
				const available = new Set( Array.from( { length: 21 }, ( _, i ) => String( i ) ) );
				for ( const [ids, loss] of events ) {
					if ( loss ) resident.clear();
					const scene = { quads: ids.map( i => ({ texture: String( i ) }) ) },
						plan = uiTextureResidency( scene, available, resident, new Set() );
					for ( const id of plan.release ) resident.delete( id );
					for ( const id of plan.upload ) resident.add( id );
					assert.deepEqual( [ ...resident ].sort(), ids.map( String ).sort() );
				}
			}
		),
		{ seed: 512, numRuns: 200 }
	);
});
