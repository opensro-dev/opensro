/*
===========================================================================

packPublicTree.mjs - the ordered pack tail of every full pack build

The full resource build and the standalone repack both end the same way,
and each step reads what the one before it published:

  1. refresh the JSON sidecars: packs hold the .json.gz bytes, so a stale
     sidecar packed now would hide a newer loose file;
  2. collect the pack groups (assetPackGroups.mjs owns the one group list);
  3. build the packs;
  4. verify every pack the index names exists (identity or zstd-only, the
     compact profile keeps only the latter);
  5. retire precompressed sidecars nothing produces - the full build only:
     a compacted tree drops loose bases on purpose, and retiring there would
     delete sidecars the release still serves;
  6. build the web manifest;
  7. refresh the sidecars the regenerated manifests just aged out of.

The steps are a plain struct so tests run this exact sequence with
recording stubs instead of reading an entry script's source.

===========================================================================
*/
import { existsSync } from "node:fs";
import path from "node:path";
import { collectAssetPackGroups } from "./assetPackGroups.mjs";
import { buildAssetPacks, DEFAULT_ASSET_PACK_TARGET_BYTES } from "./assetPacks.mjs";
import { optimizePublicJsonAssets } from "./jsonAssetCompression.mjs";
import { retireUnownedSidecars } from "./precompressedSidecars.mjs";
import { auditClaims } from "./shared/publicationLedger.mjs";
import { buildWebAssetManifest } from "./webManifest.mjs";
import { publicRoot as defaultPublicRoot } from "./world/paths.mjs";

export const PACK_TREE_STEPS = Object.freeze( {
	optimizeJson: () => optimizePublicJsonAssets(),
	collectGroups: ( inputs ) => collectAssetPackGroups( inputs ),
	buildPacks: ( request ) => buildAssetPacks( request ),
	packFileExists: ( file ) => existsSync( file ),
	retireSidecars: ( request ) => retireUnownedSidecars( request ),
	auditClaims: ( groups ) => auditClaims( groups ),
	buildWebManifest: () => buildWebAssetManifest()
} );

/*
================
untimed

The default step timer: runs the step without recording anything.
================
*/
function untimed( label, task ) {
	return task();
}

/*
================
missingPackFiles

The pack paths an index names that exist neither as the identity file nor
as its zstd sidecar under publicRoot.
================
*/
export function missingPackFiles( assetPacks, publicRoot, exists ) {
	const missing = [];
	for ( const group of assetPacks.groups ) {
		for ( const pack of group.packs ) {
			const identityPath = path.join( publicRoot, pack.path.replace( /^\/+/, "" ) );
			const zstdPath = path.join( publicRoot, (pack.zstdPath ?? `${pack.path}.zst`).replace( /^\/+/, "" ) );
			if ( !exists( identityPath ) && !exists( zstdPath ) ) missing.push( pack.path );
		}
	}
	return missing;
}

/*
================
packPublicTree

request.groupInputs feeds collectAssetPackGroups (the caller's ui-preload
and minimap lists, and whether an outdoor lane ran). request.retireSidecars
is true only for a full build. request.timed wraps each step for the
caller's timing log. Returns every step's result.
================
*/
export async function packPublicTree( request, steps = PACK_TREE_STEPS ) {
	const timed = request.timed ?? untimed;
	const publicRoot = request.publicRoot ?? defaultPublicRoot;
	const jsonOptimization = await timed( "jsonOptimization", () => steps.optimizeJson() );
	const packGroups = await timed( "packListings", () => steps.collectGroups( request.groupInputs ) );
	// Report-only for now: which swept files no build owner claimed (publicationLedger.mjs).
	const claimAudit = request.auditClaims ?
		await timed( "claimAudit", () => steps.auditClaims( packGroups.groups ) ) :
		null;
	const assetPacks = await timed(
		"assetPacks",
		() => steps.buildPacks( { targetBytes: DEFAULT_ASSET_PACK_TARGET_BYTES, groups: packGroups.groups } )
	);
	const missing = missingPackFiles( assetPacks, publicRoot, steps.packFileExists );
	if ( missing.length > 0 ) {
		throw new Error( `Asset pack manifest references missing pack file(s): ${missing.join( ", " )}` );
	}
	const sidecarRetirement = request.retireSidecars ?
		await timed( "sidecarRetirement", () => steps.retireSidecars( { publicRoot, apply: true } ) ) :
		null;
	const manifest = await timed( "webManifest", () => steps.buildWebManifest() );
	const finalJsonOptimization = await timed( "finalJsonOptimization", () => steps.optimizeJson() );
	return {
		jsonOptimization,
		packGroups,
		claimAudit,
		assetPacks,
		sidecarRetirement,
		manifest,
		finalJsonOptimization
	};
}
