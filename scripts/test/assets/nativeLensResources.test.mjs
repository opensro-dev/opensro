/*
===========================================================================
nativeLensResources.test.mjs - fresh generation and failed-publication contracts

Synthetic compiler output exercises orchestration without licensed assets.
The Windows integration test uses synthetic DDS pixels and the real D3DX API.
===========================================================================
*/
import assert from "node:assert/strict";
import { existsSync } from "node:fs";
import { mkdtemp, mkdir, readFile, readdir, rm, stat, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { generateNativeLensResources, validateLensResource } from "../../build/shared/nativeLensResources.mjs";

const LENS_COUNT = 8;
const NTX_MAGIC = 0x3158544e;
const NTX_HEADER_BYTES = 20;
const D3DFMT_A8R8G8B8 = 21;
// Original 32-bit native-lens-resources.cpp output for the synthetic DXT3
// block below. This pins mip compression without storing any retail pixels.
const COMPRESSED_REFERENCE = Buffer.from(
	"4e54583104000000040000004458543303000000" +
		"ffffffffffffffff00f8e007e4e4e4e4" +
		"ffffffffffffffff0084007ceeeeeeee" +
		"ffffffffffffffff0084008400000000",
	"hex"
);

/*
================
solidTexture
================
*/
function solidTexture() {
	const bytes = Buffer.alloc( NTX_HEADER_BYTES + 4 );
	[ NTX_MAGIC, 1, 1, D3DFMT_A8R8G8B8, 1 ].forEach( ( value, index ) => bytes.writeUInt32LE( value, index * 4 ) );
	bytes.writeUInt32LE( 0xff332211, NTX_HEADER_BYTES );
	return bytes;
}

/*
================
workspace
================
*/
async function workspace( context ) {
	const root = await mkdtemp( path.join( os.tmpdir(), "sro-lens-test-" ) );
	context.after( () => rm( root, { recursive: true, force: true } ) );
	return { sourceRoot: path.join( root, "input" ), outputRoot: path.join( root, "output" ) };
}

/*
================
writeCompilerOutput
================
*/
async function writeCompilerOutput( sourceRoot, outputRoot ) {
	assert.ok( sourceRoot );
	for ( let index = 1; index <= LENS_COUNT; index++ ) {
		await writeFile( path.join( outputRoot, `lens${index}.texture` ), solidTexture() );
	}
}

test("fresh lens build creates all outputs and preserves unchanged file timestamps", async ( context ) => {
	const options = { ...await workspace( context ), compile: writeCompilerOutput };
	await generateNativeLensResources( options );
	const target = path.join( options.outputRoot, "lens2.texture" );
	const before = await stat( target );
	await generateNativeLensResources( options );
	assert.equal( (await stat( target )).mtimeMs, before.mtimeMs );
	assert.deepEqual( await readFile( target ), solidTexture() );
	assert.equal( (await readdir( options.outputRoot )).length, LENS_COUNT );
});

test("missing or truncated compiler output cannot replace existing resources", async ( context ) => {
	const options = { ...await workspace( context ), compile: writeCompilerOutput };
	await generateNativeLensResources( options );
	for ( const failure of [ "missing", "truncated", "compiler" ] ) {
		await assert.rejects( generateNativeLensResources( {
			...options,
			compile: async ( sourceRoot, outputRoot ) => {
				await writeCompilerOutput( sourceRoot, outputRoot );
				const changed = solidTexture();
				changed.writeUInt32LE( 0xffffffff, NTX_HEADER_BYTES );
				await writeFile( path.join( outputRoot, "lens1.texture" ), changed );
				if ( failure === "missing" ) await rm( path.join( outputRoot, "lens8.texture" ) );
				if ( failure === "truncated" ) {
					await writeFile( path.join( outputRoot, "lens8.texture" ), solidTexture().subarray( 0, 23 ) );
				}
				if ( failure === "compiler" ) throw new Error( "compiler failed" );
			}
		} ) );
		assert.deepEqual( await readFile( path.join( options.outputRoot, "lens1.texture" ) ), solidTexture() );
		assert.equal( (await readdir( options.outputRoot )).length, LENS_COUNT );
	}
});

test("lens validation rejects absent mip levels and truncated payloads", () => {
	const missingMip = solidTexture();
	missingMip.writeUInt32LE( 2, 4 );
	assert.throws( () => validateLensResource( missingMip ), /mip count/ );
	assert.throws( () => validateLensResource( solidTexture().subarray( 0, 23 ) ), /Incomplete/ );
});

/*
================
solidDdj - one uncompressed 4x4 DDS level inside the retail DDJ wrapper
================
*/
function solidDdj() {
	const wrapperBytes = 20;
	const ddsHeaderBytes = 128;
	const pixels = 16;
	const bytes = Buffer.alloc( wrapperBytes + ddsHeaderBytes + pixels * 4 );
	bytes.write( "JMXVDDJ 1000" );
	bytes.writeUInt32LE( bytes.length - wrapperBytes, 12 );
	bytes.write( "DDS ", wrapperBytes );
	for (
		const [offset, value] of [
			[ 4, 124 ],
			[ 8, 0x100f ],
			[ 12, 4 ],
			[ 16, 4 ],
			[ 20, 16 ],
			[ 76, 32 ],
			[ 80, 0x41 ],
			[ 88, 32 ],
			[ 92, 0xff0000 ],
			[ 96, 0xff00 ],
			[ 100, 0xff ],
			[ 104, 0xff000000 ],
			[ 108, 0x1000 ]
		]
	) {
		bytes.writeUInt32LE( value, wrapperBytes + offset );
	}
	for ( let pixel = 0; pixel < pixels; pixel++ ) {
		bytes.writeUInt32LE( 0xff332211, wrapperBytes + ddsHeaderBytes + pixel * 4 );
	}
	return bytes;
}

const hasRuntime = process.platform === "win32" && existsSync(
	path.join( process.env.SystemRoot ?? "C:\\Windows", "SysWOW64", "d3dx9_39.dll" )
);
/*
================
compressedDdj - synthetic red/green DXT3 block with four selector values
================
*/
function compressedDdj() {
	const wrapperBytes = 20;
	const ddsHeaderBytes = 128;
	const blockBytes = 16;
	const bytes = Buffer.from( solidDdj().subarray( 0, wrapperBytes + ddsHeaderBytes + blockBytes ) );
	bytes.writeUInt32LE( bytes.length - wrapperBytes, 12 );
	for (
		const [offset, value] of [
			[ 8, 0x81007 ],
			[ 80, 4 ],
			[ 84, 0x33545844 ],
			[ 88, 0 ],
			[ 92, 0 ],
			[ 96, 0 ],
			[ 100, 0 ],
			[ 104, 0 ]
		]
	) {
		bytes.writeUInt32LE( value, wrapperBytes + offset );
	}
	Buffer.from( "ffffffffffffffff00f8e007e4e4e4e4", "hex" ).copy( bytes, wrapperBytes + ddsHeaderBytes );
	return bytes;
}

test( "real compiler generates every mip from fresh synthetic DDJ inputs", { skip: !hasRuntime }, async ( context ) => {
	const options = await workspace( context );
	await mkdir( options.sourceRoot );
	for ( let index = 1; index <= LENS_COUNT; index++ ) {
		await writeFile(
			path.join( options.sourceRoot, `lens${index}.ddj` ),
			index > 4 ? compressedDdj() : solidDdj()
		);
	}
	await generateNativeLensResources( options );
	for ( let index = 1; index <= LENS_COUNT; index++ ) {
		const bytes = await readFile( path.join( options.outputRoot, `lens${index}.texture` ) );
		if ( index > 4 ) {
			assert.deepEqual( bytes, COMPRESSED_REFERENCE );
			continue;
		}
		assert.equal( bytes.readUInt32LE( 16 ), 3 );
		assert.equal( bytes.length, NTX_HEADER_BYTES + (16 + 4 + 1) * 4 );
		for ( let offset = NTX_HEADER_BYTES; offset < bytes.length; offset += 4 ) {
			assert.equal( bytes.readUInt32LE( offset ), 0xff332211 );
		}
	}
} );
