/*
===========================================================================

navigation-memory-bench.mjs - what one region's navigation costs in memory

Usage:
  node --expose-gc tools/perf/bench/navigation-memory-bench.mjs
       [--region 0x61a8,0x6b4f] [--json FILE]

Builds each region's NavigationProduct the way the asset worker does
(createNavigationResources), then installs it into the simulation
worker's navigation owner (createNavigation). Prints, per region:

	bundle   bytes read from the published tree (JSON and meshes)
	typed    typed-array bytes the product holds (unique ArrayBuffers)
	accounted  the owner's own residency accounting (its 64 MiB budget)
	product  heap retained by the product alone
	owner    heap retained by an installed navigation owner, product
	         included (what a second copy on the main thread would cost)

This decides how the main thread gets a navigation view for the
render-thread mover (GH #1439): a parity-tested copy if `owner` is small
next to the tab's budget, a shared buffer otherwise.

Heap numbers are process.memoryUsage() deltas after forced collections,
so run with --expose-gc. They are Node's V8, not a browser tab's.

===========================================================================
*/
import "../../../tests/helpers/native-source-loader.mjs";
import { writeFile } from "node:fs/promises";
import { CLIENT_PUBLIC_ROOT } from "../../../../../scripts/lib/generatedRoot.mjs";
import { readPublishedAssetBytesSync } from "../../../../../scripts/lib/publishedAsset.mjs";
import { parseOptions } from "../core/report.mjs";

const { createNavigationResources } = await import(
	"../../../src/engine/runtime/assets/worker/navigation/navigation.ts"
);
const { admitObjectProduct } = await import(
	"../../../src/engine/runtime/simulation/worker/session/world/gameplay/movement/navigation/admission.ts"
);
const { createNavigationIndex } = await import( "../../../src/engine/foundation/navigation/spatial-index.ts" );
const { isDungeonRegion } = await import( "../../../src/engine/foundation/world/world-space.ts" );
const { createNavigation } = await import(
	"../../../src/engine/runtime/simulation/worker/session/world/gameplay/movement/navigation/navigation.ts"
);

const USAGE = "navigation-memory-bench.mjs [--region 0x61a8,0x6b4f] [--json FILE]";
const MIB = 1 << 20;
// Copies kept alive per measurement (see measure).
const COPIES = 4;
// Jangan field; the largest Constantinople, China city and wharf bundles;
// the largest outdoor region; the largest dungeon manifest.
const DEFAULT_REGIONS = [ "0x61a8", "0x6b4f", "0x63a9", "0x60bd", "0x674b", "0x8005" ];

/*
================
settledHeap

Heap in use after collecting garbage twice (weak references and
finalizers can need the second pass).
================
*/
function settledHeap() {
	globalThis.gc();
	globalThis.gc();
	const usage = process.memoryUsage();
	return usage.heapUsed + usage.arrayBuffers;
}

/*
================
typedBytes

Bytes of every distinct ArrayBuffer reachable from value. A buffer shared
by several views counts once.
================
*/
function typedBytes( value ) {
	const seen = new Set(), buffers = new Set(), stack = [ value ];
	let total = 0;
	while ( stack.length ) {
		const item = stack.pop();
		if ( item === null || typeof item !== "object" || seen.has( item ) ) continue;
		seen.add( item );
		if ( ArrayBuffer.isView( item ) ) {
			if ( !buffers.has( item.buffer ) ) {
				buffers.add( item.buffer );
				total += item.buffer.byteLength;
			}
			continue;
		}
		for ( const child of Object.values( item ) ) stack.push( child );
	}
	return total;
}

/*
================
measure

Builds the region's product and owner COPIES times, keeping every copy
alive, and divides the heap growth: one copy's retained size is within
collection noise of a few hundred KiB. A warm-up build first pays for
module caches, so they are not charged to the region.
================
*/
async function measure( regionText ) {
	const regionId = Number( regionText );
	const catalog = JSON.parse(
		new TextDecoder().decode(
			readPublishedAssetBytesSync( "/assets/world/world-region-catalog.json", CLIENT_PUBLIC_ROOT )
		)
	);
	const path = isDungeonRegion( regionId ) ?
		"/assets/world/dungeon/dungeon-resources.json" :
		catalog.regionsById[regionText]?.[0]?.bundlePublicPath;
	if ( !path ) throw new Error( "No navigation bundle for " + regionText );
	let bundleBytes = 0;
	const read = async file => {
		const bytes = readPublishedAssetBytesSync( file, CLIENT_PUBLIC_ROOT );
		bundleBytes += bytes.byteLength;
		return bytes;
	};
	const build = async () => createNavigationResources().resolve( await read( path ), regionId, read );
	const warm = await build();
	const typed = typedBytes( warm );
	// The owner's own residency accounting, as install charges it against
	// its budget (admitObjectProduct plus the spatial index).
	const accounted = (warm.complete ? admitObjectProduct( warm, regionId ) : 0) +
		createNavigationIndex( warm.objects ).bytes;
	createNavigation().install( regionId, warm );
	bundleBytes = 0;
	const products = [], owners = [];
	let before = settledHeap();
	for ( let i = 0; i < COPIES; i++ ) products.push( await build() );
	const productHeap = (settledHeap() - before) / COPIES;
	const bundlePerCopy = bundleBytes / COPIES;
	products.length = 0;
	before = settledHeap();
	for ( let i = 0; i < COPIES; i++ ) {
		const owner = createNavigation();
		owner.install( regionId, await build() );
		owners.push( owner );
	}
	const indexHeap = (settledHeap() - before) / COPIES;
	if ( owners.length !== COPIES ) throw new Error( "unreachable" );
	return { region: regionText, bundleBytes: bundlePerCopy, typed, accounted, productHeap, indexHeap };
}

/*
================
main
================
*/
async function main() {
	if ( typeof globalThis.gc !== "function" ) throw new Error( "Run with node --expose-gc. " + USAGE );
	const options = parseOptions( process.argv.slice( 2 ), { region: [], json: "" }, USAGE );
	const regions = options.region.length ? options.region : DEFAULT_REGIONS;
	const rows = [];
	for ( const region of regions ) rows.push( await measure( region ) );
	const mib = n => (n / MIB).toFixed( 1 );
	console.log( "region   bundle MiB  typed MiB  accounted MiB  product MiB  owner MiB" );
	for ( const row of rows ) {
		console.log(
			row.region.padEnd( 8 ),
			mib( row.bundleBytes ).padStart( 10 ),
			mib( row.typed ).padStart( 10 ),
			mib( row.accounted ).padStart( 14 ),
			mib( row.productHeap ).padStart( 12 ),
			mib( row.indexHeap ).padStart( 10 )
		);
	}
	if ( options.json ) await writeFile( options.json, JSON.stringify( rows, null, "\t" ) );
}

await main();
