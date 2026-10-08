import * as zlib from "node:zlib";

export const DEFAULT_BROTLI_QUALITY = 11;
export const DEFAULT_GZIP_LEVEL = 9;
export const DEFAULT_ZSTD_LEVEL = 19;
// 8 MiB: RFC 9659 maximum for HTTP Content-Encoding: zstd.
export const DEFAULT_ZSTD_WINDOW_LOG = 23;
// Every suffix a precompressed sidecar has ever had: what the retirement and
// freshness passes recognize as a sidecar of the file beside it.
export const PRECOMPRESSED_ASSET_SUFFIXES = [ ".br", ".gz", ".zst" ];
// The sidecars the build publishes: only the .json.gz the packs hold. The
// client reads JSON through the packs, the dev middleware serves gzip only
// (apps/client-next/tools/published-assets.mjs) and the release packager
// compresses its own routes, so Brotli and zstd sidecars had no reader and
// cost most of a clean build (476 s of 1,020 on 2026-10-08).
export const PUBLISHED_SIDECAR_SUFFIXES = [ ".gz" ];

export function compressionAvailable( encoding ) {
	if ( encoding === "br" ) return typeof zlib.brotliCompressSync === "function";
	if ( encoding === "gzip" ) return typeof zlib.gzipSync === "function";
	if ( encoding === "zstd" ) return typeof zlib.zstdCompressSync === "function";
	return false;
}

export function compressBrotliSync( bytes, { quality = DEFAULT_BROTLI_QUALITY } = {} ) {
	return zlib.brotliCompressSync( bytes, {
		params: {
			[zlib.constants.BROTLI_PARAM_QUALITY]: quality,
			[zlib.constants.BROTLI_PARAM_SIZE_HINT]: bytes.byteLength
		}
	} );
}

export function compressGzipSync( bytes, { level = DEFAULT_GZIP_LEVEL } = {} ) {
	return zlib.gzipSync( bytes, { level } );
}

export function compressZstdSync(
	bytes,
	{ level = DEFAULT_ZSTD_LEVEL, windowLog = DEFAULT_ZSTD_WINDOW_LOG } = {}
) {
	if ( typeof zlib.zstdCompressSync !== "function" ) {
		throw new Error( "Zstandard compression requires Node.js zlib Zstandard support." );
	}
	return zlib.zstdCompressSync( bytes, {
		params: {
			[zlib.constants.ZSTD_c_compressionLevel]: level,
			[zlib.constants.ZSTD_c_windowLog]: windowLog
		}
	} );
}

export function compressZstd(
	bytes,
	{ level = DEFAULT_ZSTD_LEVEL, windowLog = DEFAULT_ZSTD_WINDOW_LOG } = {}
) {
	if ( typeof zlib.zstdCompress !== "function" ) {
		return Promise.reject( new Error( "Zstandard compression requires Node.js zlib Zstandard support." ) );
	}
	return new Promise( ( resolve, reject ) => {
		zlib.zstdCompress(
			bytes,
			{
				params: {
					[zlib.constants.ZSTD_c_compressionLevel]: level,
					[zlib.constants.ZSTD_c_windowLog]: windowLog
				}
			},
			( error, compressed ) => (error ? reject( error ) : resolve( compressed ))
		);
	} );
}
