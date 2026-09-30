/*
===========================================================================

nativeCharacterTextures.test.mjs - preservation of authored DDS mip blocks

Fixture bytes distinguish authored levels from native-generated suffixes.
Malformed chains must fail before an output can become a published cache.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import {
	compressedSource,
	preserveAuthoredMips,
	validateCharacterTexture
} from "../../build/shared/nativeCharacterTextures.mjs";

const DXT1 = 0x31545844;
const DXT3 = 0x33545844;
const DXT5 = 0x35545844;
const DDJ_DDS_HEADER = 148;
const NTX_HEADER = 20;

/*
================
fixture

Each 4x4 source has one authored block and needs two smaller mip levels.
The generated base deliberately differs to detect accidental recompression.
================
*/
function fixture( format, levels = 1 ) {
	const blockBytes = format === DXT1 ? 8 : 16;
	const source = Buffer.alloc( DDJ_DDS_HEADER + levels * blockBytes, 0 );
	source.write( "JMXVDDJ 1000", 0, "ascii" );
	source.write( "DDS ", 20, "ascii" );
	source.writeUInt32LE( 124, 24 );
	source.writeUInt32LE( 4, 32 );
	source.writeUInt32LE( 4, 36 );
	source.writeUInt32LE( levels, 48 );
	source.writeUInt32LE( format, 104 );
	source.fill( 0x39, DDJ_DDS_HEADER );
	const generated = Buffer.alloc( NTX_HEADER + 3 * blockBytes, 0x72 );
	generated.writeUInt32LE( 0x3158544e, 0 );
	generated.writeUInt32LE( 4, 4 );
	generated.writeUInt32LE( 4, 8 );
	generated.writeUInt32LE( format, 12 );
	generated.writeUInt32LE( 3, 16 );
	return { source, generated, blockBytes };
}

test("authored BC1/BC2/BC3 blocks survive and only missing mips come from the native loader", () => {
	for ( const format of [ DXT1, DXT3, DXT5 ] ) {
		for ( const levels of [ 1, 3 ] ) {
			const { source, generated, blockBytes } = fixture( format, levels );
			const result = preserveAuthoredMips( source, generated );
			assert.deepEqual(
				result.subarray( NTX_HEADER, NTX_HEADER + levels * blockBytes ),
				source.subarray( DDJ_DDS_HEADER )
			);
			assert.deepEqual(
				result.subarray( NTX_HEADER + levels * blockBytes ),
				generated.subarray( NTX_HEADER + levels * blockBytes )
			);
			assert.equal( generated[NTX_HEADER], 0x72 );
			assert.equal( validateCharacterTexture( source, result ).levels, levels );
		}
	}
});

test("native admission rejects changed formats, missing blocks and corrupt authored output", () => {
	const { source, generated } = fixture( DXT1 );
	assert.throws( () => validateCharacterTexture( source, generated ), /authored mip/ );
	assert.throws( () => preserveAuthoredMips( source.subarray( 0, source.length - 1 ), generated ), /Truncated/ );
	assert.throws( () => preserveAuthoredMips( source, generated.subarray( 0, generated.length - 1 ) ), /Truncated/ );
	const changed = Buffer.from( generated );
	changed.writeUInt32LE( DXT3, 12 );
	assert.throws( () => preserveAuthoredMips( source, changed ), /format/ );
	const unsupported = Buffer.from( source );
	unsupported.writeUInt32LE( 0x32545844, 104 );
	assert.equal( compressedSource( unsupported ), null );
});
