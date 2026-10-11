/*
===========================================================================

effect-link-mode.test.mjs - EFP LinkMode applies as CEFCLinkMode does

Each native branch of B1F6F0 (follow, rotate-about, matrix with its -1
reset, velocity; positive walks, the root stop, negative and zero words;
the ancestor marks) and the shipped effects, whose LinkMode controllers
must resolve to exactly what their element headers already say.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";

const { effectLinks: resolveLinks } = await import( "../../src/engine/foundation/animation/effect-link-mode.ts" );

/*
================
effectLinks

The resolved links, keyed by the fixture elements.
================
*/
function effectLinks( root ) {
	return /** @type {Map<any, any>} */ (resolveLinks( root ));
}

/*
================
node

An element with an optional LinkMode and header fields.
================
*/
/**
 * @param {any[]} [children]
 * @param {number[]} [linkMode]
 * @param {Record<string, number>} [header]
 * @returns {any}
 */
function node( children = [], linkMode, header = {} ) {
	return {
		...header,
		children,
		controllers: linkMode ? [ { name: "NormalTimeLife" }, { name: "LinkMode", value: linkMode } ] : []
	};
}

test("without a LinkMode an element keeps its header links", () => {
	const leaf = node( [], undefined, { int0: 1, int1: -1, int2: 2, int3: 1, byte0: 1, byte1: 0 } );
	const links = effectLinks( node( [ leaf ] ) ).get( leaf );
	assert.deepEqual( links, {
		positionDepth: 1,
		matrixDepth: -1,
		velocityDepth: 2,
		followDepth: 1,
		keepMatrix: true,
		keepOrigin: false
	} );
});

test("a LinkMode overrides the header: follow, rotate-about, matrix, velocity in file order", () => {
	const leaf = node( [], [ 1, 1, 1, 1 ], { int0: 0, int1: 0, int2: 0, int3: 0 } );
	const parent = node( [ leaf ] ), root = node( [ parent ] );
	const links = effectLinks( root );
	assert.deepEqual( links.get( leaf ), {
		positionDepth: 1,
		matrixDepth: 1,
		velocityDepth: 1,
		followDepth: 1,
		keepMatrix: false,
		keepOrigin: false
	} );
	// The follow word marks the reached ancestor's previous position, the
	// other three its turn (B1F724 +0x155, B1F747/773/796 +0x154).
	assert.equal( links.get( parent ).keepOrigin, true );
	assert.equal( links.get( parent ).keepMatrix, true );
	assert.equal( links.get( root ).keepOrigin, false );
	assert.equal( links.get( root ).keepMatrix, false );
});

test("a deeper word marks the ancestor it reaches; a walk past the root stops there and still counts", () => {
	const leaf = node( [], [ 2, 0, 0, 5 ] ), middle = node( [ leaf ] ), root = node( [ middle ] );
	const links = effectLinks( root );
	assert.equal( links.get( leaf ).followDepth, 2 );
	assert.equal( links.get( root ).keepOrigin, true );
	assert.equal( links.get( middle ).keepOrigin, false );
	// Five steps from the leaf: two parents, then the root has none; the
	// native count still reaches three before it stops.
	assert.equal( links.get( leaf ).velocityDepth, 3 );
	assert.equal( links.get( root ).keepMatrix, true );
});

test("a root's own link word marks the root itself", () => {
	const root = node( [], [ 1, 0, 0, 0 ] );
	const links = effectLinks( root ).get( root );
	assert.equal( links.followDepth, 1 );
	assert.equal( links.keepOrigin, true );
});

test("zero links nothing; a negative word marks the element itself at depth 0", () => {
	const leaf = node( [], [ 0, -3, 0, -1 ], { int0: 4, int2: 4 } ), root = node( [ leaf ] );
	const links = effectLinks( root );
	assert.equal( links.get( leaf ).followDepth, 0 );
	assert.equal( links.get( leaf ).positionDepth, 0 );
	assert.equal( links.get( leaf ).velocityDepth, 0 );
	assert.equal( links.get( leaf ).keepMatrix, true );
	assert.equal( links.get( leaf ).keepOrigin, false );
	assert.equal( links.get( root ).keepMatrix, false );
});

test("a matrix word of -1 (written unsigned) keeps no ancestor and drops the rotation", () => {
	const leaf = node( [], [ 0, 0, 0xffffffff, 0 ], { int1: 2 } ), root = node( [ leaf ] );
	const links = effectLinks( root );
	assert.equal( links.get( leaf ).matrixDepth, -1 );
	assert.equal( links.get( root ).keepMatrix, false );
	assert.equal( links.get( leaf ).keepMatrix, false );
});

test("a malformed LinkMode is refused", () => {
	assert.throws( () => effectLinks( node( [], [ 1, 2, 3 ] ) ), /Invalid effect LinkMode/ );
	assert.throws( () => effectLinks( node( [], [ 1, 2, 3, 1.5 ] ) ), /Invalid effect LinkMode/ );
});

test("every shipped effect's LinkMode agrees with its element headers", () => {
	const { effects } = JSON.parse( readFileSync( CLIENT_PUBLIC_ROOT + "/assets/effects/programs.json", "utf8" ) );
	let checked = 0;
	for ( const [path, effect] of Object.entries( effects ) ) {
		if ( !effect?.root ) continue;
		for ( const [element, links] of effectLinks( effect.root ) ) {
			assert.deepEqual( links, {
				positionDepth: element.int0 ?? 0,
				matrixDepth: element.int1 ?? 0,
				velocityDepth: element.int2 ?? 0,
				followDepth: element.int3 ?? 0,
				keepMatrix: !!element.byte0,
				keepOrigin: !!element.byte1
			}, path );
			checked++;
		}
	}
	assert.ok( checked > 7000, String( checked ) );
});
