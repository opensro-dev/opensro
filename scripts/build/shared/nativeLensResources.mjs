/*
===========================================================================
nativeLensResources.mjs - own lens mip generation and atomic publication

Full and standalone world builds share this prerequisite. A process generates
the eight resources once, in a private staging directory, before any sky copy.
The build entry point owns the generated-asset lock.
===========================================================================
*/
import { execFile } from "node:child_process";
import { mkdir, mkdtemp, readFile, rm } from "node:fs/promises";
import path from "node:path";
import { promisify } from "node:util";
import { extractedRoot, imageSourceRoot, rebuildRoot } from "../world/paths.mjs";
import { publishFileFromTemp } from "./atomicPublish.mjs";
import { isMainScript } from "./fsUtils.mjs";
import { withGeneratedAssetsLock } from "../../rebuildLock.mjs";

const executeFile = promisify( execFile );
const LENS_COUNT = 8;
const NTX_MAGIC = 0x3158544e;
const NTX_HEADER_BYTES = 20;
const D3DFMT_A8R8G8B8 = 21;
const D3DFMT_DXT3 = 0x33545844;
let buildPromise;

/*
================
compileLensResources
================
*/
async function compileLensResources( sourceRoot, outputRoot ) {
	if ( process.platform !== "win32" ) {
		throw new Error( "Native lens generation requires Windows and the DirectX End-User Runtime." );
	}
	const windowsRoot = process.env.SystemRoot ?? "C:\\Windows";
	const powershell = path.join( windowsRoot, "SysWOW64", "WindowsPowerShell", "v1.0", "powershell.exe" );
	try {
		await executeFile( powershell, [
			"-NoProfile",
			"-NonInteractive",
			"-ExecutionPolicy",
			"Bypass",
			"-File",
			path.join( rebuildRoot, "scripts", "build", "native_lens_resources.ps1" ),
			"-SourceRoot",
			sourceRoot,
			"-OutputRoot",
			outputRoot
		], { windowsHide: true } );
	} catch ( error ) {
		throw new Error( `Native lens generation failed: ${error.stderr || error.message}`, { cause: error } );
	}
}

/*
================
validateLensResource - reject incomplete output before publishing any file
================
*/
export function validateLensResource( data ) {
	if ( data.length < NTX_HEADER_BYTES || data.readUInt32LE( 0 ) !== NTX_MAGIC ) {
		throw new Error( "Invalid native lens resource header" );
	}
	let width = data.readUInt32LE( 4 );
	let height = data.readUInt32LE( 8 );
	const format = data.readUInt32LE( 12 );
	const levels = data.readUInt32LE( 16 );
	if (
		!width || !height || ![ D3DFMT_A8R8G8B8, D3DFMT_DXT3 ].includes( format ) ||
		levels !== Math.floor( Math.log2( Math.max( width, height ) ) ) + 1
	) {
		throw new Error( "Invalid native lens dimensions, format or mip count" );
	}
	let expectedBytes = NTX_HEADER_BYTES;
	for ( let level = 0; level < levels; level++ ) {
		expectedBytes += format === D3DFMT_DXT3 ?
			Math.ceil( width / 4 ) * Math.ceil( height / 4 ) * 16 :
			width * height * 4;
		width = Math.max( 1, Math.floor( width / 2 ) );
		height = Math.max( 1, Math.floor( height / 2 ) );
	}
	if ( data.length !== expectedBytes ) {
		throw new Error( `Incomplete native lens mip chain: expected ${expectedBytes} bytes, got ${data.length}` );
	}
}

/*
================
generateNativeLensResources - explicit paths and compiler permit isolated tests
================
*/
export async function generateNativeLensResources( options = {} ) {
	const sourceRoot = options.sourceRoot ?? path.join( extractedRoot, "Map_extracted", "sun" );
	const outputRoot = options.outputRoot ?? path.join( imageSourceRoot, "Map_extracted", "sun" );
	const compile = options.compile ?? compileLensResources;
	await mkdir( outputRoot, { recursive: true } );
	const stagingRoot = await mkdtemp( path.join( outputRoot, ".lens-build-" ) );
	try {
		await compile( sourceRoot, stagingRoot );
		const names = Array.from( { length: LENS_COUNT }, ( _, index ) => `lens${index + 1}.texture` );
		for ( const name of names ) {
			validateLensResource( await readFile( path.join( stagingRoot, name ) ) );
		}
		for ( const name of names ) {
			await publishFileFromTemp( path.join( stagingRoot, name ), path.join( outputRoot, name ) );
		}
	} finally {
		await rm( stagingRoot, { recursive: true, force: true } );
	}
}

/*
================
buildNativeLensResources - concurrent world consumers share one prerequisite
================
*/
export function buildNativeLensResources() {
	buildPromise ??= generateNativeLensResources().catch( ( error ) => {
		buildPromise = undefined;
		throw error;
	} );
	return buildPromise;
}

if ( isMainScript( import.meta.url ) ) {
	await withGeneratedAssetsLock( "native lens resources", buildNativeLensResources );
}
