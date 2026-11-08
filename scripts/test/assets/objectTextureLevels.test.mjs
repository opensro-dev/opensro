/*
===========================================================================

objectTextureLevels.test.mjs - level overrides touch only their own blocks

===========================================================================
*/
import assert from "node:assert/strict";
import test from "node:test";
import { applyObjectTextureLevels } from "../../build/world/assets/objectTextureLevels.mjs";

const NTX_MAGIC = 0x3158544e;
const DXT1_FOURCC = 0x31545844;
const DXT5_FOURCC = 0x35545844;
const LISTED = "prim/mtrl/bldg/china/jangan_enter/cj_east_door.ddj";

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
});

test("an unlisted texture is returned as is", () => {
	const source = container();
	assert.equal( applyObjectTextureLevels( source, "prim/mtrl/bldg/china/jangan_enter/cj_wall01.ddj" ), source );
});

test("a listed texture in another block format fails the build", () => {
	assert.throws( () => applyObjectTextureLevels( container( DXT5_FOURCC ), LISTED ), /DXT1/ );
});
