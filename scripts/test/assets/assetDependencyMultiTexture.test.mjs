/*
===========================================================================

assetDependencyMultiTexture.test.mjs - a BSR's multi-texture modifier joins the closure

The later-source importers (Sky Temple, Arabia, the cap-110 equipment)
copy a resource and everything it names through
analysis/cap110_asset_dependencies.mjs. A multi-texture modifier names an
extra texture straight from the BSR, outside every material set (the
flame spirits' prim/mtrl/etc/flame_spirit_t.ddj); the closure once missed
it, and the NPC model build then failed on the absent file.

===========================================================================
*/
import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { test } from "node:test";
import { fileURLToPath } from "node:url";

const TEXTURE = "prim\mtrl\etc\fixture_t.ddj";

// ModDataMultiTexRev: modifier class 1, kind 2 (bsrModifiers.mjs).
const MULTI_TEXTURE_REVERSED = 0x10002;

// Where the fixture's empty lists and its modifier section start.
const EMPTY_LIST = 0x70;
const MODIFIER_SECTION = 0x80;

/*
================
multiTextureBsr

A minimal character BSR: no mesh, no material, one modifier set holding
one reversed multi-texture modifier, then an empty second list and a
part-link tail.
================
*/
function multiTextureBsr() {
	const bytes = Buffer.alloc( 1024 );
	bytes.write( "JMXVRES 0109" );
	// Header pointers: the material set (0) and the mesh list (1) read an
	// empty count; the modifier section (6) follows.
	bytes.writeUInt32LE( EMPTY_LIST, 0x0c );
	bytes.writeUInt32LE( EMPTY_LIST, 0x0c + 4 );
	let p = MODIFIER_SECTION;
	bytes.writeUInt32LE( p, 0x0c + 6 * 4 );
	const u32 = ( value ) => {
		bytes.writeUInt32LE( value >>> 0, p );
		p += 4;
	};
	const str = ( value ) => {
		u32( value.length );
		p += bytes.write( value, p, "latin1" );
	};
	u32( 1 ); // One modifier set.
	u32( 2 );
	u32( -1 );
	str( "ambient" );
	u32( 1 );
	u32( MULTI_TEXTURE_REVERSED );
	for ( let i = 1; i <= 6; i++ ) u32( i );
	bytes.set( [ 0, 0, 0, 0 ], p );
	p += 4;
	u32( 1 ); // Stage mode.
	str( TEXTURE );
	u32( 0x66000000 ); // Texture factor.
	u32( 0 ); // Second list.
	u32( 1 );
	u32( 4 );
	u32( 2 );
	u32( 0 );
	return bytes.subarray( 0, p );
}

/*
================
dependenciesOf

Runs the dependency audit on one resource and returns its list.
================
*/
function dependenciesOf( resource ) {
	const script = fileURLToPath( new URL( "../../analysis/cap110_asset_dependencies.mjs", import.meta.url ) );
	const run = spawnSync( process.execPath, [ script ], { input: JSON.stringify( [ resource ] ), encoding: "utf8" } );
	assert.equal( run.status, 0, run.stderr );
	return JSON.parse( run.stdout )[0];
}

test("a later BSR's multi-texture modifier names its texture as a dependency", () => {
	const bytes = multiTextureBsr().toString( "base64" );
	assert.ok( dependenciesOf( { path: "res/mob/fixture.bsr", bytes, native: false } ).includes( TEXTURE ) );
	// A native resource's texture is native: the closure leaves it alone.
	assert.ok( !dependenciesOf( { path: "res/mob/fixture.bsr", bytes, native: true } ).includes( TEXTURE ) );
});
