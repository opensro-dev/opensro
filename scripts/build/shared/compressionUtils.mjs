import * as zlib from "node:zlib";

export const DEFAULT_GZIP_LEVEL = 9;
// Every suffix a precompressed sidecar has ever had: what the retirement and
// freshness passes recognize as a sidecar of the file beside it, so an older
// tree's .br and .zst files are archived instead of shipped.
export const PRECOMPRESSED_ASSET_SUFFIXES = [ ".br", ".gz", ".zst" ];
// The sidecars the build publishes: only the .json.gz the packs hold. The
// client reads JSON through the packs, the dev middleware serves gzip only
// (apps/client-next/tools/published-assets.mjs) and the release packager
// compresses its own routes.
export const PUBLISHED_SIDECAR_SUFFIXES = [ ".gz" ];

export function compressionAvailable( encoding ) {
	return encoding === "gzip" && typeof zlib.gzipSync === "function";
}

export function compressGzipSync( bytes, { level = DEFAULT_GZIP_LEVEL } = {} ) {
	return zlib.gzipSync( bytes, { level } );
}
