import { createHash } from "node:crypto";
import { createWriteStream } from "node:fs";
import { mkdir, readFile, readdir, rename, rm, stat } from "node:fs/promises";
import path from "node:path";
import { once } from "node:events";
import { finished } from "node:stream/promises";
import { constants, createGzip, gunzipSync } from "node:zlib";
import { compareText } from "../shared/textOrder.mjs";

const ARCHIVE_MAGIC = Buffer.from( "SROGDB1\n", "ascii" );
const MAX_ARCHIVE_FILES = 100_000;

export async function buildServerGameDataArchive( bundleRoot, archivePath = `${bundleRoot}.srogz` ) {
	const files = await listRelativeFiles( bundleRoot );
	if ( files.length === 0 || files.length > MAX_ARCHIVE_FILES ) {
		throw new Error( `Server game-data archive file count ${files.length} is invalid` );
	}

	const temporaryPath = `${archivePath}.tmp-${process.pid}`;
	await mkdir( path.dirname( archivePath ), { recursive: true } );
	await rm( temporaryPath, { force: true } );
	const output = createWriteStream( temporaryPath, { flags: "wx" } );
	const gzip = createGzip( {
		level: constants.Z_BEST_COMPRESSION,
		mtime: 0
	} );
	gzip.pipe( output );

	try {
		await writeChunk( gzip, ARCHIVE_MAGIC );
		const count = Buffer.alloc( 4 );
		count.writeUInt32LE( files.length );
		await writeChunk( gzip, count );

		for ( const relativePath of files ) {
			const pathBytes = Buffer.from( relativePath, "utf8" );
			const contents = await readFile( path.join( bundleRoot, ...relativePath.split( "/" ) ) );
			const record = Buffer.alloc( 12 );
			record.writeUInt32LE( pathBytes.length, 0 );
			record.writeBigUInt64LE( BigInt( contents.length ), 4 );
			await writeChunk( gzip, record );
			await writeChunk( gzip, pathBytes );
			await writeChunk( gzip, contents );
		}
		gzip.end();
		await finished( output );
		await validateServerGameDataArchive( temporaryPath );
		await rm( archivePath, { force: true } );
		await rename( temporaryPath, archivePath );
	} catch ( error ) {
		gzip.destroy();
		output.destroy();
		await rm( temporaryPath, { force: true } );
		throw error;
	}

	return {
		archivePath,
		fileCount: files.length,
		bytes: (await stat( archivePath )).size
	};
}

export async function validateServerGameDataArchive( archivePath ) {
	const compressed = await readFile( archivePath );
	const raw = gunzipSync( compressed );
	let offset = 0;
	take( raw, offset, ARCHIVE_MAGIC.length, "magic" ).equals( ARCHIVE_MAGIC ) || fail( "bad archive magic" );
	offset += ARCHIVE_MAGIC.length;
	const count = take( raw, offset, 4, "file count" ).readUInt32LE();
	offset += 4;
	if ( count === 0 || count > MAX_ARCHIVE_FILES ) {
		fail( `invalid archive file count ${count}` );
	}

	const records = new Map();
	for ( let index = 0; index < count; index += 1 ) {
		const header = take( raw, offset, 12, `record ${index} header` );
		offset += 12;
		const pathLength = header.readUInt32LE( 0 );
		const size = Number( header.readBigUInt64LE( 4 ) );
		if ( pathLength === 0 || pathLength > 4096 || !Number.isSafeInteger( size ) ) {
			fail( `invalid archive record ${index}` );
		}
		const relativePath = take( raw, offset, pathLength, `record ${index} path` ).toString( "utf8" );
		offset += pathLength;
		if ( !safeRelativePath( relativePath ) || records.has( relativePath.toLowerCase() ) ) {
			fail( `unsafe or duplicate archive path ${JSON.stringify( relativePath )}` );
		}
		const contents = take( raw, offset, size, `record ${index} contents` );
		offset += size;
		records.set( relativePath.toLowerCase(), { relativePath, contents } );
	}
	if ( offset !== raw.length ) {
		fail( `archive has ${raw.length - offset} trailing byte(s)` );
	}

	const manifestRecord = records.get( "manifest.json" );
	if ( !manifestRecord ) {
		fail( "archive has no manifest.json" );
	}
	const manifest = JSON.parse( manifestRecord.contents.toString( "utf8" ) );
	if ( manifest.format !== "sro-game-data-bundle" || manifest.files.length + 1 !== records.size ) {
		fail( "archive manifest identity/file count is invalid" );
	}
	for ( const descriptor of manifest.files ) {
		const record = records.get( descriptor.path.toLowerCase() );
		if ( !record || record.relativePath !== descriptor.path || record.contents.length !== descriptor.size ) {
			fail( `archive descriptor mismatch for ${descriptor.path}` );
		}
		const digest = `sha256:${createHash( "sha256" ).update( record.contents ).digest( "hex" )}`;
		if ( digest !== descriptor.digest ) {
			fail( `archive digest mismatch for ${descriptor.path}` );
		}
	}
	return { fileCount: count, expandedBytes: raw.length };
}

async function listRelativeFiles( root ) {
	const files = [];
	const walk = async ( directory ) => {
		const entries = await readdir( directory, { withFileTypes: true } );
		entries.sort( ( left, right ) => compareText( left.name, right.name ) );
		for ( const entry of entries ) {
			const absolutePath = path.join( directory, entry.name );
			if ( entry.isSymbolicLink() ) {
				throw new Error( `Server game-data archive source contains a symlink: ${absolutePath}` );
			}
			if ( entry.isDirectory() ) {
				await walk( absolutePath );
			} else if ( entry.isFile() ) {
				files.push( path.relative( root, absolutePath ).replaceAll( "\\", "/" ) );
			}
		}
	};
	await walk( root );
	files.sort();
	return files;
}

async function writeChunk( stream, chunk ) {
	if ( !stream.write( chunk ) ) {
		await once( stream, "drain" );
	}
}

function take( buffer, offset, length, label ) {
	if ( !Number.isSafeInteger( length ) || length < 0 || offset < 0 || length > buffer.length - offset ) {
		fail( `truncated ${label}` );
	}
	return buffer.subarray( offset, offset + length );
}

function safeRelativePath( value ) {
	return value.length > 0 &&
		value.length <= 4096 &&
		!value.includes( "\\" ) &&
		!value.startsWith( "/" ) &&
		!value.split( "/" ).some( ( part ) => part === "" || part === "." || part === ".." );
}

function fail( message ) {
	throw new Error( message );
}
