/*
===========================================================================

objectTextureLevels.test.mjs - level overrides touch only their own blocks

===========================================================================
*/
import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import test from "node:test";
import { applyObjectTextureLevels, blockBytes } from "../../build/world/assets/objectTextureLevels.mjs";

const NTX_MAGIC = 0x3158544e;
const DXT1_FOURCC = 0x31545844;
const DXT5_FOURCC = 0x35545844;
const LISTED = "prim/mtrl/bldg/china/jangan_enter/cj_east_door.ddj";
// sha256 of the listed level of container() after the table is applied.
const LISTED_LEVEL_SHA256 = "89d387ef6f46dc0e5259081070916ba86243a31ffc4b0ec6286de817bba01ca7";

/*
================
container

A filled NTX1 container: 512x512 with its full chain, every block 0xAA.
================
*/
function container( fourcc = DXT1_FOURCC ) {
	const blockBytes = fourcc === DXT1_FOURCC ? 8 : 16;
	let size = 20, side = 512, levels = 0;
	for ( ; side >= 1; side >>= 1, levels++ ) size += Math.ceil( side / 4 ) ** 2 * blockBytes;
	const bytes = Buffer.alloc( size, 0xaa );
	bytes.writeUInt32LE( NTX_MAGIC, 0 );
	bytes.writeUInt32LE( 512, 4 );
	bytes.writeUInt32LE( 512, 8 );
	bytes.writeUInt32LE( fourcc, 12 );
	bytes.writeUInt32LE( levels, 16 );
	return bytes;
}

test("a listed texture changes only blocks inside its listed level", () => {
	const source = container();
	const out = applyObjectTextureLevels( source, "D:/extracted/Data/" + LISTED.toUpperCase() );
	const levelZero = 20 + 128 * 128 * 8, levelOne = levelZero + 64 * 64 * 8;
	assert.ok( !out.equals( source ) );
	assert.ok( out.subarray( 0, levelZero ).equals( source.subarray( 0, levelZero ) ) );
	assert.ok( out.subarray( levelOne ).equals( source.subarray( levelOne ) ) );
	assert.equal( source[levelZero], 0xaa, "the input buffer is never written" );
	// The table's own words land on their own blocks: the first and
	// last listed blocks carry their listed bytes at their listed
	// offsets, and the unlisted 3948 between them stays filled - an
	// index or hex typo in the table fails here, not in the world.
	const at = index => levelZero + index * 8;
	assert.equal( out.subarray( at( 3818 ), at( 3818 ) + 8 ).toString( "hex" ), "0a638431fb8b5037" );
	assert.equal( out[at( 3948 )], 0xaa );
	assert.equal( out.subarray( at( 3956 ), at( 3956 ) + 8 ).toString( "hex" ), "6b738531a1820800" );
	// Every listed block at once: the patched level's digest moves on
	// any index or hex change anywhere in the table.
	assert.equal(
		createHash( "sha256" ).update( out.subarray( levelZero, levelOne ) ).digest( "hex" ),
		LISTED_LEVEL_SHA256
	);
});

test("an unlisted texture is returned as is", () => {
	const source = container();
	assert.equal( applyObjectTextureLevels( source, "prim/mtrl/bldg/china/jangan_enter/cj_wall01.ddj" ), source );
});

test("a table row that is not one whole DXT1 block fails the build", () => {
	assert.equal( blockBytes( LISTED, 1, "0a638431fb8b5037" ).toString( "hex" ), "0a638431fb8b5037" );
	// Short, odd-length and non-hex rows would otherwise copy short.
	for ( const hex of [ "0a638431fb8b50", "0a638431fb8b503", "0a638431fb8b50zz", "0a638431fb8b503700" ] ) {
		assert.throws( () => blockBytes( LISTED, 1, hex ), /not 8 bytes of hex/, hex );
	}
});

test("a listed texture in another block format fails the build", () => {
	assert.throws( () => applyObjectTextureLevels( container( DXT5_FOURCC ), LISTED ), /DXT1/ );
});
