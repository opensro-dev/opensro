/*
===========================================================================

nativeCharacterTextures.mjs - shared native mip publication for character images

Material owners resolve retail paths before calling this module. DXT1/3/5
sources use the existing D3DX generator, while other authored image formats
keep the PNG route. Every authored compressed mip is checked byte-for-byte;
only the absent suffix may be generated. The outer build owns the asset lock.

===========================================================================
*/
import fs from "node:fs";
import path from "node:path";
import { createHash } from "node:crypto";
import { dataAssetPath, convertedTexturePath, dataRoot } from "./jmxAssetIO.mjs";
import { imageSourceRoot } from "../world/paths.mjs";
import { listFiles, isMainScript } from "./fsUtils.mjs";
import { publishFileFromTemp } from "./atomicPublish.mjs";
import { withGeneratedAssetsLock } from "../../rebuildLock.mjs";
import { candidateGenerators, generatorHash, runMipGenerator, selectMipGenerator } from "./nativeMipGenerator.mjs";

const DDJ_HEADER = 20;
const DDS_HEADER = 128;
const NTX_HEADER = 20;
const DXT1 = 0x31545844;
const DXT3 = 0x33545844;
const DXT5 = 0x35545844;
const NTX_MAGIC = 0x3158544e;
const MAX_DIMENSION = 8192;
const NATIVE_MIME = "application/x-sro-texture";
const cacheRoot = path.join( imageSourceRoot, "native-character" );
// Source bytes and generator code both govern generated mip cache identity.
// D3DX and the portable native_texture_mips.py never share entries
// (nativeMipGenerator.mjs); a reader takes the first candidate it finds.
const GENERATOR_HASHES = Object.fromEntries( candidateGenerators().map( kind => [ kind, generatorHash( kind ) ] ) );

/*
================
compressedSource

Only authored BC formats enter this route. DXT2 lightmaps have premultiplied
alpha and belong to the existing world publisher, not the character contract.
================
*/
export function compressedSource( bytes ) {
	if (
		bytes.length < DDJ_HEADER + DDS_HEADER ||
		bytes.toString( "ascii", 0, 12 ) !== "JMXVDDJ 1000" ||
		bytes.toString( "ascii", DDJ_HEADER, DDJ_HEADER + 4 ) !== "DDS "
	) return null;
	const format = bytes.readUInt32LE( DDJ_HEADER + 84 );
	if ( ![ DXT1, DXT3, DXT5 ].includes( format ) ) return null;
	const width = bytes.readUInt32LE( DDJ_HEADER + 16 );
	const height = bytes.readUInt32LE( DDJ_HEADER + 12 );
	const levels = Math.max( 1, bytes.readUInt32LE( DDJ_HEADER + 28 ) );
	const fullLevels = 1 + Math.floor( Math.log2( Math.max( width, height ) ) );
	if (
		!width || !height || width > MAX_DIMENSION || height > MAX_DIMENSION ||
		levels > fullLevels ||
		bytes.readUInt32LE( DDJ_HEADER + 112 ) !== 0
	) {
		throw Error( "Unsupported compressed character dimensions, mip count or surface kind" );
	}
	// The existing world PNG path owns non-power-of-two building textures.
	// D3DX would resize these; they cannot claim byte-preserving block upload.
	if ( (width & (width - 1)) || (height & (height - 1)) ) return null;
	return { width, height, format, levels, fullLevels, blockBytes: format === DXT1 ? 8 : 16 };
}

/*
================
validateCharacterTexture

The generator may extend a chain, but it must not reinterpret or recompress
the source levels. Validate the complete output before publishing its cache.
================
*/
export function validateCharacterTexture( source, output ) {
	const info = compressedSource( source );
	if (
		!info || output.length < NTX_HEADER || output.readUInt32LE( 0 ) !== NTX_MAGIC ||
		output.readUInt32LE( 4 ) !== info.width || output.readUInt32LE( 8 ) !== info.height ||
		output.readUInt32LE( 12 ) !== info.format || output.readUInt32LE( 16 ) !== info.fullLevels
	) {
		throw Error( "Native character texture changed its source contract" );
	}
	let targetOffset = NTX_HEADER, sourceOffset = DDJ_HEADER + DDS_HEADER;
	for ( let level = 0; level < info.fullLevels; level++ ) {
		const width = Math.max( 1, info.width >> level ), height = Math.max( 1, info.height >> level );
		const size = Math.ceil( width / 4 ) * Math.ceil( height / 4 ) * info.blockBytes;
		if ( targetOffset + size > output.length ) throw Error( "Truncated native character mip chain" );
		if ( level < info.levels ) {
			if (
				sourceOffset + size > source.length ||
				!source.subarray( sourceOffset, sourceOffset + size ).equals(
					output.subarray( targetOffset, targetOffset + size )
				)
			) {
				throw Error(
					"Native character generator changed authored mip " + level + " (" + width + "x" + height + ")"
				);
			}
			sourceOffset += size;
		}
		targetOffset += size;
	}
	if ( targetOffset !== output.length ) throw Error( "Trailing native character mip bytes" );
	return info;
}

/*
================
preserveAuthoredMips

D3DX can normalize padding selectors in a mip smaller than one block.
Take authored blocks directly from DDS, including their padding, and use
the native loader only for the missing suffix. Validate the assembled
resource before the caller publishes it.
================
*/
export function preserveAuthoredMips( source, generated ) {
	const info = compressedSource( source );
	if (
		!info || generated.length < NTX_HEADER ||
		generated.readUInt32LE( 0 ) !== NTX_MAGIC ||
		generated.readUInt32LE( 4 ) !== info.width ||
		generated.readUInt32LE( 8 ) !== info.height ||
		generated.readUInt32LE( 12 ) !== info.format ||
		generated.readUInt32LE( 16 ) !== info.fullLevels
	) {
		throw Error( "Native generator changed source dimensions or format" );
	}
	const output = Buffer.from( generated );
	let sourceOffset = DDJ_HEADER + DDS_HEADER, targetOffset = NTX_HEADER;
	for ( let level = 0; level < info.levels; level++ ) {
		const width = Math.max( 1, info.width >> level ), height = Math.max( 1, info.height >> level );
		const size = Math.ceil( width / 4 ) * Math.ceil( height / 4 ) * info.blockBytes;
		if ( sourceOffset + size > source.length || targetOffset + size > output.length ) {
			throw Error( "Truncated authored native mip" );
		}
		source.copy( output, targetOffset, sourceOffset, sourceOffset + size );
		sourceOffset += size;
		targetOffset += size;
	}
	validateCharacterTexture( source, output );
	return output;
}

/*
================
cachePath

Bind generated mip resources to their full source bytes and the generator
that wrote them. A changed DDJ cannot silently reuse an old texture merely
because its path or timestamp is stable.
================
*/
function cachePath( bytes, kind ) {
	return path.join(
		cacheRoot,
		createHash( "sha256" ).update( GENERATOR_HASHES[kind] ).update( bytes ).digest( "hex" ) + ".texture"
	);
}

/*
================
cachedPath

The entry an earlier build published for these bytes, preferring D3DX's.
================
*/
function cachedPath( bytes ) {
	for ( const kind of candidateGenerators() ) {
		const target = cachePath( bytes, kind );
		if ( fs.existsSync( target ) ) return target;
	}
	return null;
}

/*
================
readCharacterTexture

All base, reflection and equipment-glow publishers use the same selection.
A missing native cache fails the build instead of silently publishing PNGs.
================
*/
export function readCharacterTexture( gamePath, pngPath ) {
	if ( gamePath ) {
		const source = fs.readFileSync( dataAssetPath( gamePath ) );
		if ( compressedSource( source ) ) {
			const target = cachedPath( source );
			if ( !target ) {
				throw Error(
					"Missing native character texture; run the native character texture prerequisite: " + gamePath
				);
			}
			const bytes = fs.readFileSync( target );
			validateCharacterTexture( source, bytes );
			return { bytes, mime: NATIVE_MIME };
		}
	}
	return { bytes: fs.readFileSync( pngPath ?? convertedTexturePath( gamePath ) ), mime: "image/png" };
}

/*
================
buildNativeCharacterTextures

Generate missing content-addressed entries in one generator session (D3DX
or the portable generator, nativeMipGenerator.mjs), probed only when an
entry is missing. Private staging prevents a failed process from leaving an
admissible cache.
================
*/
export async function buildNativeCharacterTextures() {
	await fs.promises.mkdir( cacheRoot, { recursive: true } );
	const staging = await fs.promises.mkdtemp( path.join( cacheRoot, ".build-" ) );
	try {
		const missing = [], seen = new Set();
		for ( const source of await listFiles( dataRoot, { extensions: [ ".ddj" ] } ) ) {
			if ( !source.toLowerCase().endsWith( ".ddj" ) ) continue;
			const bytes = await fs.promises.readFile( source );
			if ( !compressedSource( bytes ) ) continue;
			const digest = createHash( "sha256" ).update( bytes ).digest( "hex" );
			if ( seen.has( digest ) ) continue;
			seen.add( digest );
			const cached = cachedPath( bytes );
			if ( cached ) {
				validateCharacterTexture( bytes, await fs.promises.readFile( cached ) );
				continue;
			}
			missing.push( { source, bytes } );
		}
		if ( !missing.length ) return;
		const kind = await selectMipGenerator();
		const jobs = missing.map( ( { source, bytes } ) => {
			const target = cachePath( bytes, kind );
			return { source, target: path.join( staging, path.basename( target ) ), published: target };
		} );
		const manifest = path.join( staging, "jobs.json" );
		await fs.promises.writeFile( manifest, JSON.stringify( jobs ) );
		await runMipGenerator( kind, { manifest }, "native character texture mip generation" );
		for ( const job of jobs ) {
			const source = await fs.promises.readFile( job.source );
			const output = preserveAuthoredMips( source, await fs.promises.readFile( job.target ) );
			await fs.promises.writeFile( job.target, output );
		}
		for ( const job of jobs ) await publishFileFromTemp( job.target, job.published );
		console.log( "Native character textures: published " + jobs.length + " verified mip resources" );
	} finally {
		await fs.promises.rm( staging, { recursive: true, force: true } );
	}
}

if ( isMainScript( import.meta.url ) ) {
	await withGeneratedAssetsLock( "native character textures", buildNativeCharacterTextures );
}
