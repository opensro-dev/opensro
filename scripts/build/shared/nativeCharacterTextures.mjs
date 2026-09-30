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
import { execFile } from "node:child_process";
import { promisify } from "node:util";
import { createHash } from "node:crypto";
import { dataAssetPath, convertedTexturePath, dataRoot } from "./jmxAssetIO.mjs";
import { imageSourceRoot, rebuildRoot } from "../world/paths.mjs";
import { listFiles, isMainScript } from "./fsUtils.mjs";
import { publishFileFromTemp } from "./atomicPublish.mjs";
import { withGeneratedAssetsLock } from "../../rebuildLock.mjs";

const execute = promisify( execFile );
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
const GENERATOR_HASH = createHash( "sha256" )
	.update( fs.readFileSync( path.join( rebuildRoot, "scripts/build/NativeLensResources.cs" ) ) )
	.update( fs.readFileSync( path.join( rebuildRoot, "scripts/build/native_lens_resources.ps1" ) ) )
	.digest( "hex" );

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

Bind generated mip resources to their full source bytes. A changed DDJ cannot
silently reuse an old texture merely because its path or timestamp is stable.
================
*/
function cachePath( bytes ) {
	return path.join(
		cacheRoot,
		createHash( "sha256" ).update( GENERATOR_HASH ).update( bytes ).digest( "hex" ) + ".texture"
	);
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
			const target = cachePath( source );
			if ( !fs.existsSync( target ) ) {
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

Generate missing content-addressed entries in one native device session.
Private staging prevents a failed process from leaving an admissible cache.
================
*/
export async function buildNativeCharacterTextures() {
	await fs.promises.mkdir( cacheRoot, { recursive: true } );
	const staging = await fs.promises.mkdtemp( path.join( cacheRoot, ".build-" ) );
	try {
		const jobs = [], seen = new Set();
		for ( const source of await listFiles( dataRoot, { extensions: [ ".ddj" ] } ) ) {
			if ( !source.toLowerCase().endsWith( ".ddj" ) ) continue;
			const bytes = await fs.promises.readFile( source );
			if ( !compressedSource( bytes ) ) continue;
			const target = cachePath( bytes );
			if ( seen.has( target ) ) continue;
			seen.add( target );
			if ( fs.existsSync( target ) ) {
				validateCharacterTexture( bytes, await fs.promises.readFile( target ) );
				continue;
			}
			jobs.push( { source, target: path.join( staging, path.basename( target ) ), published: target } );
		}
		if ( !jobs.length ) return;
		const manifest = path.join( staging, "jobs.json" );
		await fs.promises.writeFile( manifest, JSON.stringify( jobs ) );
		const powershell = path.join(
			process.env.SystemRoot ?? "C:\\Windows",
			"SysWOW64",
			"WindowsPowerShell",
			"v1.0",
			"powershell.exe"
		);
		await execute( powershell, [
			"-NoProfile",
			"-NonInteractive",
			"-ExecutionPolicy",
			"Bypass",
			"-File",
			path.join( rebuildRoot, "scripts/build/native_lens_resources.ps1" ),
			"-Manifest",
			manifest
		], { windowsHide: true } );
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
