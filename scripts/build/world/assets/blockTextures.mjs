/*
===========================================================================

blockTextures.mjs - the shared block-texture probe and NTX1 publisher

Every lane that ships authored DXT blocks as a .texture container goes
through here. The terrain lanes (ground tiles, MAPT-embedded lightmaps)
publish the AUTHORED levels only, as a pure byte remap - retail loaded the
terrain through CD3DTexture_CreateFromDDJArchiveHandle (0x9f8ea0) with the
file's own level count, so single-level sources ship single-level and no
generator runs. The world object lane generates its mip suffix through the
python encoder (review-approved in #195). The probe admits exactly what the
client's NTX route admits (power-of-two DXT1/DXT3/DXT5); everything else
stays on its converted-PNG path.

Encodes are content-addressed - the authored source bytes AND the encoder's
own bytes name the cached container, so a generator change can never serve
stale output - and coalesced: producers enqueue one file at a time while the
drain window is open, and one python invocation encodes the whole batch into
staged temporaries that are validated and atomically published (the
nativeCharacterTextures.mjs cache pattern). The character pipeline proved
the cache first.
===========================================================================
*/
import { randomUUID } from "node:crypto";
import { mkdir, open, readFile, rm, writeFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { publishFileFromTemp } from "../../shared/atomicPublish.mjs";
import { claimPublicFile } from "../../shared/publicationLedger.mjs";
import { exists } from "../io.mjs";
import { generatedRoot } from "../paths.mjs";
import { sha256Hex } from "../../shared/hash.mjs";
import { runPython } from "../../shared/pythonRun.mjs";

const DDJ_HEADER_SIZE = 20;
const DDS_HEADER_BYTES = 148;
// Offsets inside a bare DDS header; a DDJ wrapper shifts them by 20.
const DDS_MAGIC = "DDS ";
const DDS_STRUCTURE_OFFSET = 4;
const DDS_HEIGHT_OFFSET = 12;
const DDS_WIDTH_OFFSET = 16;
const DDS_PIXEL_FORMAT_OFFSET = 76;
const DDS_MIP_COUNT_OFFSET = 28;
// Where a DDS header ends and the level payloads begin (the probe buffer is
// DDJ-wrapped and therefore larger - do not conflate the two).
const DDS_PAYLOAD_OFFSET = 128;
const DDS_PIXEL_FORMAT_BYTES = 32;
const DDPF_FLAGS_OFFSET = DDS_PIXEL_FORMAT_OFFSET + 4;
const FOURCC_CODE_OFFSET = DDPF_FLAGS_OFFSET + 4;
const DDPF_FOURCC = 0x4;
const BLOCK_FOURCCS = new Map( [
	[ 0x31545844, "dxt1" ],
	[ 0x33545844, "dxt3" ],
	[ 0x35545844, "dxt5" ]
] );
const NTX_MAGIC = 0x3158544e;
const NTX_HEADER_BYTES = 20;
const MAX_TEXTURE_DIMENSION = 8192;
const ENCODER_PATH = fileURLToPath( new URL( "../../native_texture_mips.py", import.meta.url ) );
const BLOCK_TEXTURE_CACHE_DIR = path.join( generatedRoot, "intermediate", "block-texture-cache" );
// One python process per batch beats one per file; the window keeps a
// steady producer stream batching while never delaying a lone file long.
const DRAIN_DELAY_MS = 40;
const DRAIN_BATCH = 64;

/*
================
blockFormatOfDdsHeader

The block format the client's NTX route admits, or null when the surface is
not a power-of-two DXT texture (native-texture.ts rejects those). The header
slice starts at the DDS magic; dwMipMapCount 0 means one authored level and
the encoder clamps the same way.
================
*/
function blockFormatOfDdsHeader( dds ) {
	if ( dds.byteLength < FOURCC_CODE_OFFSET + 4 ) return null;
	if ( dds.toString( "latin1", 0, 4 ) !== DDS_MAGIC ) return null;
	if ( dds.readUInt32LE( DDS_STRUCTURE_OFFSET ) !== 124 ) return null;
	if ( dds.readUInt32LE( DDS_PIXEL_FORMAT_OFFSET ) !== DDS_PIXEL_FORMAT_BYTES ) return null;
	if ( (dds.readUInt32LE( DDPF_FLAGS_OFFSET ) & DDPF_FOURCC) === 0 ) return null;
	const height = dds.readUInt32LE( DDS_HEIGHT_OFFSET );
	const width = dds.readUInt32LE( DDS_WIDTH_OFFSET );
	if (
		width < 1 || height < 1 || width > MAX_TEXTURE_DIMENSION || height > MAX_TEXTURE_DIMENSION ||
		width & (width - 1) || height & (height - 1)
	) return null;
	return BLOCK_FOURCCS.get( dds.readUInt32LE( FOURCC_CODE_OFFSET ) ) ?? null;
}

// Probe results per source path: a build process reads each DDJ once.
const probedFiles = new Map();

/*
================
probeBlockTextureFile

Read the on-disk shape ("JMXVDDJ " + DDS) and report the admitted block
format, or null. Missing files return null (the caller owns that error).
================
*/
export async function probeBlockTextureFile( absolutePath ) {
	const cached = probedFiles.get( absolutePath );
	if ( cached !== undefined ) return cached;
	let format = null;
	if ( await exists( absolutePath ) ) {
		const handle = await open( absolutePath, "r" );
		try {
			const header = Buffer.alloc( DDS_HEADER_BYTES );
			const { bytesRead } = await handle.read( header, 0, DDS_HEADER_BYTES, 0 );
			// The slice guards below reject anything shorter than the fourCC
			// field, so a short read is simply a non-block file.
			if ( bytesRead >= FOURCC_CODE_OFFSET + 4 ) {
				format = header.toString( "latin1", 0, 8 ) === "JMXVDDJ " ?
					blockFormatOfDdsHeader( header.subarray( DDJ_HEADER_SIZE ) ) :
					blockFormatOfDdsHeader( header );
			}
		} finally {
			await handle.close();
		}
	}
	probedFiles.set( absolutePath, format );
	return format;
}

/*
================
probeBlockDdsPayload

The bare DDS payload a MAPT terrain sector embeds (no DDJ wrapper).
================
*/
export function probeBlockDdsPayload( bytes ) {
	return bytes.byteLength >= FOURCC_CODE_OFFSET + 4 ?
		blockFormatOfDdsHeader( Buffer.from( bytes.buffer, bytes.byteOffset, bytes.byteLength ) ) :
		null;
}

/*
================
authoredLevelCount

The file's own level count (DDS dwMipMapCount; 0 means one), clamped to
the full chain: a lying header cannot claim more levels than the
dimensions describe.
================
*/
function authoredLevelCount( dds, width, height ) {
	const full = 1 + Math.floor( Math.log2( Math.max( width, height ) ) );
	return Math.max( 1, Math.min( full, dds.readUInt32LE( DDS_MIP_COUNT_OFFSET ) || 1 ) );
}

/*
================
authoredBlockContainer

The terrain container: the authored DDJ/DDS bytes remapped into an NTX1
header, nothing generated, nothing re-encoded. Every level is copied
verbatim; a truncated or malformed source fails loudly instead of shipping.
================
*/
export function authoredBlockContainer( bytes, origin ) {
	const fail = why => {
		throw Error( `Invalid block texture source ${origin}: ${why}` );
	};
	const isDdj = bytes.length >= 8 && bytes.subarray( 0, 8 ).toString( "latin1" ) === "JMXVDDJ ";
	const dds = isDdj ? bytes.subarray( DDJ_HEADER_SIZE ) : bytes;
	const format = blockFormatOfDdsHeader( dds );
	if ( !format ) fail( "not a power-of-two DXT1/DXT3/DXT5 surface" );
	const width = dds.readUInt32LE( DDS_WIDTH_OFFSET ), height = dds.readUInt32LE( DDS_HEIGHT_OFFSET );
	const blockBytes = nativeTextureBlockBytesOf( dds.readUInt32LE( FOURCC_CODE_OFFSET ) );
	const count = authoredLevelCount( dds, width, height );
	const out = Buffer.alloc( NTX_HEADER_BYTES );
	out.writeUInt32LE( NTX_MAGIC, 0 );
	out.writeUInt32LE( width, 4 );
	out.writeUInt32LE( height, 8 );
	out.writeUInt32LE( dds.readUInt32LE( FOURCC_CODE_OFFSET ), 12 );
	out.writeUInt32LE( count, 16 );
	const levels = [];
	let offset = DDS_PAYLOAD_OFFSET, levelWidth = width, levelHeight = height;
	for ( let level = 0; level < count; level++ ) {
		const size = Math.ceil( levelWidth / 4 ) * Math.ceil( levelHeight / 4 ) * blockBytes;
		if ( offset + size > dds.length ) fail( `truncated authored level ${level}` );
		levels.push( dds.subarray( offset, offset + size ) );
		offset += size;
		levelWidth = Math.max( 1, levelWidth >> 1 );
		levelHeight = Math.max( 1, levelHeight >> 1 );
	}
	return Buffer.concat( [ out, ...levels ] );
}

/*
================
writeAuthoredBlockContainer

Validate the remapped container before it publishes, so the write can
never ship a malformed artifact even if the source parse drifted.
================
*/
export async function writeAuthoredBlockContainer( bytes, origin, target ) {
	const container = authoredBlockContainer( bytes, origin );
	validateAuthoredContainer( container, origin );
	// Staged and renamed into place: a crash mid-write never leaves a
	// truncated container in the published tree.
	await mkdir( path.dirname( target ), { recursive: true } );
	const staged = `${target}.${randomUUID()}.tmp`;
	await writeFile( staged, container );
	await publishFileFromTemp( staged, target );
}

/*
================
nativeTextureBlockBytesOf
================
*/
function nativeTextureBlockBytesOf( fourcc ) {
	return fourcc === 0x31545844 ? 8 : 16;
}

/*
================
validateAuthoredContainer
================
*/
function validateAuthoredContainer( bytes, origin ) {
	const fail = why => {
		throw Error( `Invalid authored container for ${origin}: ${why}` );
	};
	if ( bytes.length < NTX_HEADER_BYTES || bytes.readUInt32LE( 0 ) !== NTX_MAGIC ) fail( "not an NTX1 header" );
	const width = bytes.readUInt32LE( 4 ), height = bytes.readUInt32LE( 8 ), fourcc = bytes.readUInt32LE( 12 );
	const blockBytes = nativeTextureBlockBytesOf( fourcc );
	if (
		!BLOCK_FOURCCS.has( fourcc ) || width < 1 || height < 1 ||
		width > MAX_TEXTURE_DIMENSION || height > MAX_TEXTURE_DIMENSION ||
		width & (width - 1) || height & (height - 1)
	) fail( "unsupported format or dimensions" );
	const count = bytes.readUInt32LE( 16 );
	const full = 1 + Math.floor( Math.log2( Math.max( width, height ) ) );
	if ( count < 1 || count > full ) fail( "mip count" );
	let size = NTX_HEADER_BYTES, levelWidth = width, levelHeight = height;
	for ( let level = 0; level < count; level++ ) {
		size += Math.ceil( levelWidth / 4 ) * Math.ceil( levelHeight / 4 ) * blockBytes;
		levelWidth = Math.max( 1, levelWidth >> 1 );
		levelHeight = Math.max( 1, levelHeight >> 1 );
	}
	if ( bytes.length !== size ) fail( `byte extent ${bytes.length} of ${size}` );
}

/*
================
publishBlockTextureFile

Resolve when the NTX1 container of the authored source exists at target
(the world object lane: authored levels plus the generated suffix).
================
*/
export async function publishBlockTextureFile( source, target ) {
	const result = await enqueueBlockTexture( { source, target } );
	// A cache hit resolves without writing; the existing container is still this run's.
	claimPublicFile( target );
	return result;
}

const pendingJobs = [];
const hashBySource = new Map();
let drainTimer = null, encoderDigest = null;

/*
================
generatorHash

The encoder's own bytes fold into every cache key: a change to
native_texture_mips.py can never serve an artifact the old code produced
(nativeCharacterTextures.mjs's GENERATOR_HASH rule).
================
*/
async function generatorHash() {
	encoderDigest ??= sha256Hex( await readFile( ENCODER_PATH ) );
	return encoderDigest;
}

/*
================
enqueueBlockTexture
================
*/
function enqueueBlockTexture( job ) {
	return new Promise( ( resolve, reject ) => {
		pendingJobs.push( { job, resolve, reject } );
		if ( pendingJobs.length >= DRAIN_BATCH ) {
			if ( drainTimer !== null ) clearTimeout( drainTimer );
			drainTimer = null;
			void drainPendingJobs();
		} else if ( drainTimer === null ) {
			drainTimer = setTimeout( () => {
				drainTimer = null;
				void drainPendingJobs();
			}, DRAIN_DELAY_MS );
		}
	} );
}

/*
================
validateBlockContainer

Self-consistency of an encoded NTX1 before it may enter the cache: magic,
power-of-two DXT format, the full mip count and the exact byte extent. A
truncated or foreign artifact fails the build instead of publishing.
================
*/
function validateBlockContainer( bytes, origin ) {
	const fail = why => {
		throw Error( `Invalid block container for ${origin}: ${why}` );
	};
	if ( bytes.length < NTX_HEADER_BYTES || bytes.readUInt32LE( 0 ) !== NTX_MAGIC ) fail( "not an NTX1 header" );
	const width = bytes.readUInt32LE( 4 ), height = bytes.readUInt32LE( 8 ), fourcc = bytes.readUInt32LE( 12 );
	const blockBytes = fourcc === 0x31545844 ? 8 : fourcc === 0x33545844 || fourcc === 0x35545844 ? 16 : 0;
	if (
		!blockBytes || width < 1 || height < 1 ||
		width > MAX_TEXTURE_DIMENSION || height > MAX_TEXTURE_DIMENSION ||
		width & (width - 1) || height & (height - 1)
	) {
		fail( "unsupported format or dimensions" );
	}
	const count = bytes.readUInt32LE( 16 );
	if ( count !== 1 + Math.floor( Math.log2( Math.max( width, height ) ) ) ) fail( "mip count" );
	let size = NTX_HEADER_BYTES, levelWidth = width, levelHeight = height;
	for ( let level = 0; level < count; level++ ) {
		size += Math.ceil( levelWidth / 4 ) * Math.ceil( levelHeight / 4 ) * blockBytes;
		levelWidth = Math.max( 1, levelWidth >> 1 );
		levelHeight = Math.max( 1, levelHeight >> 1 );
	}
	if ( bytes.length !== size ) fail( `byte extent ${bytes.length} of ${size}` );
}

/*
================
drainPendingJobs

Group the batch by content key, serve cache hits immediately, encode the
rest through one python manifest into staged temporaries, validate each and
atomically publish it into the cache, then copy to every target. A failed
encode is a build-time defect (the sources were probed admitted): the whole
batch fails the build, exactly like the object lane's one-shot manifest.
================
*/
async function drainPendingJobs() {
	const batch = pendingJobs.splice( 0 );
	// content key -> { cachedTexture, staged, manifestSource, entries }
	const groups = new Map();
	try {
		const generator = await generatorHash();
		for ( const entry of batch ) {
			const { job } = entry;
			const sourceHash = await hashFile( job.source );
			const key = sha256Hex( generator + sourceHash );
			let group = groups.get( key );
			if ( !group ) {
				const cachedTexture = path.join( BLOCK_TEXTURE_CACHE_DIR, `${key}.texture` );
				group = {
					cachedTexture,
					staged: `${cachedTexture}.${randomUUID()}.tmp`,
					manifestSource: job.source,
					entries: []
				};
				groups.set( key, group );
			}
			group.entries.push( entry );
		}
		const manifest = [];
		const stagedDestinations = new Map();
		await mkdir( BLOCK_TEXTURE_CACHE_DIR, { recursive: true } );
		for ( const group of groups.values() ) {
			if ( await exists( group.cachedTexture ) ) continue;
			manifest.push( { source: group.manifestSource, target: group.staged } );
			stagedDestinations.set( group.staged, group.cachedTexture );
		}
		if ( manifest.length ) {
			const manifestPath = path.join(
				BLOCK_TEXTURE_CACHE_DIR,
				`encode-jobs-${randomUUID()}.json`
			);
			await writeFile( manifestPath, JSON.stringify( manifest ), "utf8" );
			try {
				await runPython( [ ENCODER_PATH, "-Manifest", manifestPath ], {
					task: "Encode block textures to NTX1 containers",
					context: [
						"Sources are authored DXT power-of-two DDJ/DDS files; targets are staged cache temporaries."
					]
				} );
			} finally {
				await rm( manifestPath, { force: true } );
			}
			for ( const item of manifest ) {
				validateBlockContainer( await readFile( item.target ), item.source );
				await publishFileFromTemp( item.target, stagedDestinations.get( item.target ) );
			}
		}
		for ( const group of groups.values() ) {
			for ( const entry of group.entries ) {
				await copyCached( group.cachedTexture, entry.job.target );
				entry.resolve();
			}
		}
	} catch ( error ) {
		for ( const entry of batch ) entry.reject( error );
	}
}

/*
================
hashFile

Source files do not change during one build process: hash each once.
================
*/
async function hashFile( source ) {
	const memo = hashBySource.get( source );
	if ( memo ) return memo;
	const digest = sha256Hex( await readFile( source ) );
	hashBySource.set( source, digest );
	return digest;
}

/*
================
copyCached

A cache entry is re-validated on its way out: whatever put a partial or
foreign artifact there (a crashed old writer, an editor, a bad restore)
fails the build here instead of shipping.
================
*/
async function copyCached( cachedTexture, target ) {
	const bytes = await readFile( cachedTexture );
	validateBlockContainer( bytes, cachedTexture );
	await mkdir( path.dirname( target ), { recursive: true } );
	const staged = `${target}.${randomUUID()}.tmp`;
	try {
		await writeFile( staged, bytes );
		await publishFileFromTemp( staged, target );
	} finally {
		await rm( staged, { force: true } );
	}
}
