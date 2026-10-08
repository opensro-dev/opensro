import { CLIENT_PUBLIC_ROOT } from "../lib/generatedRoot.mjs";
import { readFileSync, writeFileSync } from "node:fs";
import { mkdir, readFile, stat, writeFile } from "node:fs/promises";
import { availableParallelism } from "node:os";
import { buildJobs } from "./shared/buildParallelism.mjs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { isMainThread, parentPort, Worker, workerData } from "node:worker_threads";
import { withGeneratedAssetsLock } from "../rebuildLock.mjs";
import { mapWithConcurrency } from "./shared/asyncUtils.mjs";
import {
	compressionAvailable,
	compressBrotliSync,
	compressGzipSync,
	compressZstdSync,
	DEFAULT_BROTLI_QUALITY,
	DEFAULT_GZIP_LEVEL,
	DEFAULT_ZSTD_LEVEL,
	DEFAULT_ZSTD_WINDOW_LOG,
	PRECOMPRESSED_ASSET_SUFFIXES
} from "./shared/compressionUtils.mjs";
import { fileHashCacheDisabled } from "./shared/fileHashCache.mjs";
import { isMainScript, listFiles } from "./shared/fsUtils.mjs";
import { readJsonOrUndefined } from "./shared/jsonOut.mjs";

const scriptDir = path.dirname( fileURLToPath( import.meta.url ) );
const rebuildRoot = path.resolve( scriptDir, "..", ".." );
const publicRoot = CLIENT_PUBLIC_ROOT;
const assetsRoot = path.join( publicRoot, "assets" );
const minifyCachePath = path.join( rebuildRoot, ".state", "json-minify-cache.json" );

const MINIFY_CACHE_FORMAT = "sro-json-minify-cache";
const MINIFY_CACHE_VERSION = 1;
/** Files processed in parallel; bounded because cache misses read whole (up to ~80 MiB) JSONs. */
const FILE_PROCESS_CONCURRENCY = 8;

export { PRECOMPRESSED_ASSET_SUFFIXES } from "./shared/compressionUtils.mjs";

/**
 * @typedef {{ path: string, rawBeforeBytes: number, rawAfterBytes: number, encodings: Record<string, number> }} JsonCompressionFileRecord
 * @typedef {{ encoding: string, filePath: string, sidecarPath: string, fileRecord: JsonCompressionFileRecord }} JsonCompressionJob
 */

// Only the .json.gz the packs hold has a reader (PUBLISHED_SIDECAR_SUFFIXES).
const DEFAULT_ENCODINGS = [ "gzip" ];
const DEFAULT_COMPRESS_MIN_BYTES = 1024;

const ENCODING_DESCRIPTORS = {
	br: {
		suffix: ".br",
		label: "Brotli 11",
		available: () => compressionAvailable( "br" ),
		compressSync: ( bytes ) =>
			compressBrotliSync( bytes, {
				quality: numberFromEnv( "SRO_BROTLI_QUALITY", DEFAULT_BROTLI_QUALITY )
			} )
	},
	gzip: {
		suffix: ".gz",
		label: "gzip 9",
		available: () => compressionAvailable( "gzip" ),
		compressSync: ( bytes ) =>
			compressGzipSync( bytes, { level: numberFromEnv( "SRO_GZIP_LEVEL", DEFAULT_GZIP_LEVEL ) } )
	},
	zstd: {
		suffix: ".zst",
		label: "Zstandard 19",
		available: () => compressionAvailable( "zstd" ),
		compressSync: ( bytes ) =>
			compressZstdSync( bytes, {
				level: numberFromEnv( "SRO_ZSTD_LEVEL", DEFAULT_ZSTD_LEVEL ),
				windowLog: Math.min(
					numberFromEnv( "SRO_ZSTD_WINDOW_LOG", DEFAULT_ZSTD_WINDOW_LOG ),
					DEFAULT_ZSTD_WINDOW_LOG
				)
			} )
	}
};

export async function optimizePublicJsonAssets( options = {} ) {
	return optimizeJsonAssets( {
		root: assetsRoot,
		publicRoot,
		exclude: [ /[/\\]char[/\\]vat[/\\].*\.vat\.json$/i ],
		...options
	} );
}

export async function optimizeJsonAssets( options ) {
	const root = path.resolve( options.root );
	const rootPublic = path.resolve( options.publicRoot ?? root );
	const encodings = normalizeEncodings( options.encodings ?? encodingsFromEnv() );
	const compressMinBytes = options.compressMinBytes ??
		numberFromEnv( "SRO_ASSET_COMPRESS_MIN_BYTES", DEFAULT_COMPRESS_MIN_BYTES );
	const compressionJobs = [];
	const compressionConcurrency = normalizeConcurrency( options.compressionConcurrency );
	const force = Boolean( options.force );
	const summary = createSummary( rootPublic, encodings, compressMinBytes, compressionConcurrency );
	const excludes = options.exclude ?? [];
	const files = (await listFiles( root, { extensions: [ ".json" ] } )).filter(
		( filePath ) => !matchesExcludedPath( filePath, excludes )
	);

	// Minify-state cache: a file whose (size, mtimeMs) matches its record from the last pass
	// is already minified and is not re-read. Re-reading + byte-scanning every JSON (~2.7 GB)
	// on every pass is what made this step dominate the resource build.
	const minifyCache = force || fileHashCacheDisabled() ? Object.create( null ) : await readMinifyCache();
	const nextMinifyCache = Object.create( null );

	const fileResults = await mapWithConcurrency( files, FILE_PROCESS_CONCURRENCY, async ( filePath ) => {
		const cacheKey = path.resolve( filePath ).toLowerCase();
		const cached = minifyCache[cacheKey];
		let rawStat = await stat( filePath );
		let rawBeforeBytes = rawStat.size;
		let minifiedWritten = false;

		if ( !cached || cached.size !== rawStat.size || cached.mtimeMs !== rawStat.mtimeMs ) {
			const before = await readFile( filePath );
			const minified = minifyJsonBytes( before );
			rawBeforeBytes = before.byteLength;
			if ( !bufferEquals( before, minified ) ) {
				await mkdir( path.dirname( filePath ), { recursive: true } );
				await writeFile( filePath, minified );
				minifiedWritten = true;
				rawStat = await stat( filePath );
			}
		}
		nextMinifyCache[cacheKey] = { size: rawStat.size, mtimeMs: rawStat.mtimeMs };

		const rawAfterBytes = rawStat.size;
		/** @type {{ rawBeforeBytes: number, rawAfterBytes: number, minifiedWritten: boolean, fileRecord: JsonCompressionFileRecord | null, reusedSidecars: { encoding: string, bytes: number }[], jobs: JsonCompressionJob[], skippedEncodings: string[] }} */
		const result = {
			rawBeforeBytes,
			rawAfterBytes,
			minifiedWritten,
			fileRecord: null,
			reusedSidecars: [],
			jobs: [],
			skippedEncodings: []
		};
		if ( rawAfterBytes < compressMinBytes ) {
			return result;
		}

		const fileRecord = {
			path: toPublicPath( filePath, rootPublic ),
			rawBeforeBytes,
			rawAfterBytes,
			encodings: {}
		};
		result.fileRecord = fileRecord;

		for ( const encoding of encodings ) {
			const descriptor = ENCODING_DESCRIPTORS[encoding];
			if ( !descriptor?.available() ) {
				result.skippedEncodings.push( encoding );
				continue;
			}

			const sidecarPath = `${filePath}${descriptor.suffix}`;
			const sidecarStat = await safeStat( sidecarPath );
			if ( !force && sidecarStat && sidecarStat.mtimeMs >= rawStat.mtimeMs && sidecarStat.size > 0 ) {
				result.reusedSidecars.push( { encoding, bytes: sidecarStat.size } );
				continue;
			}

			result.jobs.push( { encoding, filePath, sidecarPath, fileRecord } );
		}
		return result;
	} );

	for ( const result of fileResults ) {
		summary.jsonFiles += 1;
		summary.rawBeforeBytes += result.rawBeforeBytes;
		summary.rawAfterBytes += result.rawAfterBytes;
		if ( result.minifiedWritten ) {
			summary.minifiedFiles += 1;
		}
		for ( const encoding of result.skippedEncodings ) {
			summary.skippedEncodings.add( encoding );
		}
		if ( !result.fileRecord ) {
			continue;
		}
		for ( const reused of result.reusedSidecars ) {
			recordCompressed( summary, result.fileRecord, reused.encoding, reused.bytes, false );
		}
		compressionJobs.push( ...result.jobs );
		summary.files.push( result.fileRecord );
	}

	summary.compressionJobs = compressionJobs.length;
	await runCompressionJobs( compressionJobs, compressionConcurrency, ( job, result ) => {
		recordCompressed( summary, job.fileRecord, job.encoding, result.bytes, true );
	} );

	if ( !fileHashCacheDisabled() ) {
		await writeMinifyCache( nextMinifyCache );
	}

	summary.files.sort( ( left, right ) => right.rawAfterBytes - left.rawAfterBytes );
	return freezeSummary( summary );
}

async function readMinifyCache() {
	const parsed = await readJsonOrUndefined( minifyCachePath );
	if ( parsed?.format === MINIFY_CACHE_FORMAT && parsed?.version === MINIFY_CACHE_VERSION && parsed.entries ) {
		return parsed.entries;
	}
	return Object.create( null );
}

async function writeMinifyCache( entries ) {
	// Merge over the on-disk cache: runs against other roots (e.g. tests) must not drop
	// entries belonging to the main public tree.
	const merged = { ...(await readMinifyCache()), ...entries };
	await mkdir( path.dirname( minifyCachePath ), { recursive: true } );
	await writeFile(
		minifyCachePath,
		JSON.stringify( { format: MINIFY_CACHE_FORMAT, version: MINIFY_CACHE_VERSION, entries: merged } ),
		"utf8"
	);
}

function matchesExcludedPath( filePath, excludes ) {
	return excludes.some( ( exclude ) => {
		if ( exclude instanceof RegExp ) {
			return exclude.test( filePath );
		}
		return path.resolve( filePath ) === path.resolve( String( exclude ) );
	} );
}

export function minifyJsonBytes( bytes ) {
	const output = Buffer.allocUnsafe( bytes.byteLength );
	let writeIndex = 0;
	let inString = false;
	let escaped = false;

	for ( let readIndex = 0; readIndex < bytes.byteLength; readIndex += 1 ) {
		const byte = bytes[readIndex];

		if ( inString ) {
			output[writeIndex] = byte;
			writeIndex += 1;

			if ( escaped ) {
				escaped = false;
			} else if ( byte === 0x5c ) {
				escaped = true;
			} else if ( byte === 0x22 ) {
				inString = false;
			}
			continue;
		}

		if ( byte === 0x22 ) {
			inString = true;
			output[writeIndex] = byte;
			writeIndex += 1;
			continue;
		}

		if ( byte === 0x20 || byte === 0x0a || byte === 0x0d || byte === 0x09 ) {
			continue;
		}

		output[writeIndex] = byte;
		writeIndex += 1;
	}

	return output.subarray( 0, writeIndex );
}

export function isPrecompressedAssetPath( filePath ) {
	return PRECOMPRESSED_ASSET_SUFFIXES.some( ( suffix ) => filePath.endsWith( suffix ) );
}

export function formatOptimizationSummary( summary ) {
	const rawSavings = summary.rawBeforeBytes - summary.rawAfterBytes;
	const lines = [
		`Optimized ${summary.jsonFiles} JSON assets: ${formatBytes( summary.rawBeforeBytes )} -> ${
			formatBytes(
				summary.rawAfterBytes
			)
		} (${formatPercent( rawSavings, summary.rawBeforeBytes )} saved by minification).`
	];

	if ( summary.compressionJobs > 0 ) {
		lines.push( `  compression workers: ${summary.compressionConcurrency}, jobs run: ${summary.compressionJobs}` );
	}

	for ( const encoding of summary.encodings ) {
		const encodingSummary = summary.byEncoding[encoding];
		if ( !encodingSummary || encodingSummary.files === 0 ) {
			continue;
		}

		lines.push(
			`  ${encoding}: ${encodingSummary.files} sidecars, ${formatBytes( encodingSummary.bytes )} total ` +
				`(${
					formatPercent( encodingSummary.sourceBytes - encodingSummary.bytes, encodingSummary.sourceBytes )
				} below minified JSON; ` +
				`${encodingSummary.written} written).`
		);
	}

	if ( summary.skippedEncodings.length > 0 ) {
		lines.push( `  skipped unavailable encodings: ${summary.skippedEncodings.join( ", " )}` );
	}

	const largest = summary.files.slice( 0, 5 );
	if ( largest.length > 0 ) {
		lines.push( "  largest JSON assets after optimization:" );
		for ( const file of largest ) {
			const encodings = Object.entries( file.encodings )
				.map( ( [encoding, bytes] ) => `${encoding}=${formatBytes( bytes )}` )
				.join( ", " );
			lines.push( `    ${file.path}: ${formatBytes( file.rawAfterBytes )}${encodings ? ` (${encodings})` : ""}` );
		}
	}

	return lines.join( "\n" );
}

/**
 * @returns {{ root: string, encodings: string[], compressMinBytes: number, compressionConcurrency: number, compressionJobs: number, jsonFiles: number, minifiedFiles: number, rawBeforeBytes: number, rawAfterBytes: number, byEncoding: Record<string, { files: number, sourceBytes: number, bytes: number, written: number }>, skippedEncodings: Set<string>, files: JsonCompressionFileRecord[] }}
 */
function createSummary( root, encodings, compressMinBytes, compressionConcurrency ) {
	/** @type {Record<string, { files: number, sourceBytes: number, bytes: number, written: number }>} */
	const byEncoding = {};
	for ( const encoding of encodings ) {
		byEncoding[encoding] = {
			files: 0,
			sourceBytes: 0,
			bytes: 0,
			written: 0
		};
	}

	return {
		root,
		encodings,
		compressMinBytes,
		compressionConcurrency,
		compressionJobs: 0,
		jsonFiles: 0,
		minifiedFiles: 0,
		rawBeforeBytes: 0,
		rawAfterBytes: 0,
		byEncoding,
		skippedEncodings: new Set(),
		files: []
	};
}

function freezeSummary( summary ) {
	return {
		...summary,
		skippedEncodings: [ ...summary.skippedEncodings ].sort()
	};
}

function recordCompressed( summary, fileRecord, encoding, bytes, written ) {
	const encodingSummary = summary.byEncoding[encoding];
	encodingSummary.files += 1;
	encodingSummary.sourceBytes += fileRecord.rawAfterBytes;
	encodingSummary.bytes += bytes;
	if ( written ) encodingSummary.written += 1;
	fileRecord.encodings[encoding] = bytes;
}

function normalizeEncodings( encodings ) {
	const normalized = [];
	for ( const encoding of encodings ) {
		const lower = String( encoding ).trim().toLowerCase();
		if ( lower && ENCODING_DESCRIPTORS[lower] && !normalized.includes( lower ) ) {
			normalized.push( lower );
		}
	}

	return normalized;
}

function encodingsFromEnv() {
	const raw = process.env.SRO_ASSET_ENCODINGS;
	if ( !raw ) return DEFAULT_ENCODINGS;
	return raw.split( "," );
}

function normalizeConcurrency( value ) {
	const parsed = Number( value );
	const concurrency = value !== undefined && Number.isFinite( parsed ) ? parsed : buildJobs();
	return Math.max( 1, Math.min( Math.floor( concurrency ), availableParallelism() ) );
}

function numberFromEnv( name, fallback ) {
	const value = Number( process.env[name] );
	return Number.isFinite( value ) ? value : fallback;
}

async function runCompressionJobs( jobs, concurrency, onResult ) {
	if ( jobs.length === 0 ) {
		return;
	}

	let nextJobIndex = 0;
	const workerCount = Math.min( concurrency, jobs.length );

	async function runQueue() {
		while ( nextJobIndex < jobs.length ) {
			const job = jobs[nextJobIndex];
			nextJobIndex += 1;
			const result = await runCompressionWorker( job );
			onResult( job, result );
		}
	}

	await Promise.all( Array.from( { length: workerCount }, runQueue ) );
}

function runCompressionWorker( job ) {
	return new Promise( ( resolve, reject ) => {
		let settled = false;
		const worker = new Worker( new URL( import.meta.url ), {
			workerData: {
				encoding: job.encoding,
				filePath: job.filePath,
				sidecarPath: job.sidecarPath
			}
		} );

		worker.once( "message", ( message ) => {
			settled = true;
			if ( message?.error ) {
				reject( new Error( message.error ) );
				return;
			}
			resolve( message );
		} );
		worker.once( "error", ( error ) => {
			settled = true;
			reject( error );
		} );
		worker.once( "exit", ( code ) => {
			if ( !settled && code !== 0 ) {
				reject( new Error( `compression worker exited with code ${code}` ) );
			}
		} );
	} );
}

function runCompressionWorkerThread() {
	try {
		const descriptor = ENCODING_DESCRIPTORS[workerData.encoding];
		if ( !descriptor?.compressSync ) {
			throw new Error( `Unsupported compression encoding in worker: ${workerData.encoding}` );
		}

		const source = readFileSync( workerData.filePath );
		const compressed = descriptor.compressSync( source );
		writeFileSync( workerData.sidecarPath, compressed );
		parentPort?.postMessage( { bytes: compressed.byteLength } );
	} catch ( error ) {
		parentPort?.postMessage( { error: error instanceof Error ? error.message : String( error ) } );
	}
}

function bufferEquals( left, right ) {
	return left.byteLength === right.byteLength && Buffer.compare( left, right ) === 0;
}

async function safeStat( filePath ) {
	try {
		return await stat( filePath );
	} catch ( error ) {
		if ( error?.code === "ENOENT" ) return undefined;
		throw error;
	}
}

function toPublicPath( filePath, root ) {
	return path.relative( root, filePath ).replaceAll( "\\", "/" );
}

function formatBytes( bytes ) {
	if ( bytes >= 1024 * 1024 ) {
		return `${(bytes / 1024 / 1024).toFixed( 2 )} MiB`;
	}
	if ( bytes >= 1024 ) {
		return `${(bytes / 1024).toFixed( 1 )} KiB`;
	}
	return `${bytes} B`;
}

function formatPercent( savedBytes, baseBytes ) {
	if ( baseBytes <= 0 ) return "0.00%";
	return `${((savedBytes / baseBytes) * 100).toFixed( 2 )}%`;
}

if ( !isMainThread ) {
	runCompressionWorkerThread();
} else if ( isMainScript( import.meta.url ) ) {
	await withGeneratedAssetsLock( "JSON asset optimization", async () => {
		const summary = await optimizePublicJsonAssets( {
			force: process.argv.includes( "--force" ),
			compressionConcurrency: cliNumberOption( "--jobs" )
		} );
		console.log( formatOptimizationSummary( summary ) );
	} );
}

function cliNumberOption( name ) {
	const equalsArg = process.argv.find( ( arg ) => arg.startsWith( `${name}=` ) );
	if ( equalsArg ) {
		return Number( equalsArg.slice( name.length + 1 ) );
	}

	const index = process.argv.indexOf( name );
	if ( index >= 0 ) {
		return Number( process.argv[index + 1] );
	}

	return undefined;
}
