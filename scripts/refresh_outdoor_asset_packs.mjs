/*
===========================================================================

refresh_outdoor_asset_packs.mjs - republish the outdoor world packs

Repacks the outdoor-world group through its content-addressed pack cache
(a full rebuild when the loose projection is complete, else a sparse patch)
and archives the packs it superseded.

===========================================================================
*/
// First: it sizes libuv's thread pool before anything starts it.
import "./build/shared/buildParallelism.mjs";
import { mkdir, readFile } from "node:fs/promises";
import path from "node:path";
import { collectOutdoorWorldFiles } from "./build/assetPackGroups.mjs";
import { buildAssetPacks } from "./build/assetPacks.mjs";
import { optimizeJsonAssets } from "./build/jsonAssetCompression.mjs";
import { publishBytesAtomically } from "./build/shared/atomicPublish.mjs";
import {
	PACK_INDEX_PATH,
	PACKS_ROOT,
	refreshPackGroups,
	requireGroup,
	timedStep
} from "./build/shared/packGroupRefresh.mjs";
import { patchAssetPackGroupFromLooseFiles } from "./build/sparseAssetPackGroupRefresh.mjs";
import { publicRoot } from "./build/world/paths.mjs";
import { withGeneratedAssetsLock } from "./rebuildLock.mjs";

// This sub-index is a real content-addressed build cache. It deliberately
// survives refreshes and compact releases; unchanged packs may point either
// at the ordinary pack root or at this cache directory.
const OUTDOOR_PACK_CACHE_ROOT = path.join( PACKS_ROOT, "outdoor" );
const OUTDOOR_PACK_CACHE_INDEX = path.join( OUTDOOR_PACK_CACHE_ROOT, "manifest.json" );
const OUTDOOR_GROUP = "outdoor-world";
const DEFAULT_OUTDOOR_TARGET_BYTES = 8 * 1024 * 1024;
const timed = timedStep( "outdoor-refresh" );

await withGeneratedAssetsLock( "outdoor asset-pack refresh", async () => {
	// Only gzip is consumed by fetchJson's pack path. Keep this focused refresh
	// cheap; a full production resource build still emits every encoding.
	const compression = await timed( "JSON sidecar freshness", () =>
		optimizeJsonAssets( {
			root: path.join( publicRoot, "assets", "world", "outdoor" ),
			publicRoot,
			encodings: [ "gzip" ]
		} ) );
	const previous = JSON.parse( await readFile( PACK_INDEX_PATH, "utf8" ) );
	await timed( "pack-cache seed", () => seedOutdoorPackCache( previous ) );
	const outdoor = await timed( "outdoor file enumeration", () => collectOutdoorWorldFiles( publicRoot ) );
	const outdoorFiles = [ ...new Set( [ ...outdoor.compressedJson, ...outdoor.rawJson, ...outdoor.images ] ) ].sort();
	const { updates, webManifest } = await refreshPackGroups( {
		name: "outdoor",
		timed,
		deltas: [ {
			groupName: OUTDOOR_GROUP,
			files: outdoorFiles,
			rebuild: previousIndex => rebuildOutdoorGroup( previousIndex, outdoorFiles )
		} ]
	} );
	const refreshed = updates[0];
	console.log(
		`Refreshed ${outdoorFiles.length} outdoor files: ` +
			`${refreshed.builtPackCount} pack(s) rebuilt, ${refreshed.reusedPackCount} reused; ` +
			`${refreshed.changedAssetCount ?? 0} sparse delta(s), ${refreshed.hydratedAssetCount ?? 0} hydrated; ` +
			`${compression.compressionJobs} gzip job(s), ${webManifest.files.length} web assets.`
	);
} );

/*
================
rebuildOutdoorGroup

A complete loose projection rebuilds the group through the pack cache (an
unchanged pack keeps its bytes); a compacted tree that dropped some loose
members patches only what is present.
================
*/
function rebuildOutdoorGroup( previous, outdoorFiles ) {
	const group = requireGroup( previous, OUTDOOR_GROUP );
	const present = new Set( outdoorFiles );
	const complete = previous.assets.filter( asset => asset.group === OUTDOOR_GROUP ).every( asset =>
		present.has( asset.path )
	);
	if ( !complete ) {
		return patchAssetPackGroupFromLooseFiles( {
			publicRoot,
			outputRoot: OUTDOOR_PACK_CACHE_ROOT,
			previousIndex: previous,
			groupName: OUTDOOR_GROUP,
			looseFiles: outdoorFiles
		} );
	}
	return buildAssetPacks( {
		publicRoot,
		outputRoot: OUTDOOR_PACK_CACHE_ROOT,
		targetBytes: previous.targetPackBytes,
		groups: [ {
			name: OUTDOOR_GROUP,
			load: group.load ?? "manual",
			targetBytes: group.targetBytes ?? DEFAULT_OUTDOOR_TARGET_BYTES,
			files: outdoorFiles
		} ]
	} );
}

/*
================
seedOutdoorPackCache

The first refresh seeds the cache index from the main index's group, so
the cache knows which packs it may reuse.
================
*/
async function seedOutdoorPackCache( previous ) {
	const existing = await readFile( OUTDOOR_PACK_CACHE_INDEX, "utf8" ).then( JSON.parse ).catch( () => undefined );
	if ( existing?.format === "sro-asset-pack-index" && existing.version === 1 ) return;
	const group = requireGroup( previous, OUTDOOR_GROUP );
	const seed = {
		format: "sro-asset-pack-index",
		version: 1,
		generatedAt: previous.generatedAt,
		targetPackBytes: previous.targetPackBytes,
		groups: [ structuredClone( group ) ],
		assets: previous.assets
			.filter( asset => asset.group === OUTDOOR_GROUP )
			.map( asset => structuredClone( asset ) )
			.sort( ( left, right ) => left.path.localeCompare( right.path ) )
	};
	await mkdir( OUTDOOR_PACK_CACHE_ROOT, { recursive: true } );
	await publishBytesAtomically( OUTDOOR_PACK_CACHE_INDEX, Buffer.from( JSON.stringify( seed ), "utf8" ), {
		logLabel: "outdoor-pack-cache-seed"
	} );
}
