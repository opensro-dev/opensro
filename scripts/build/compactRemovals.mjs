/*
===========================================================================

compactRemovals.mjs - what `pnpm assets compact` may delete, decided first

The compact release deletes three trees besides loose public duplicates:
the generated image staging cache, the loose server game-data projection
and its extraction cache. A linked worktree resolves all of them to the
MAIN checkout's trees (scripts/lib/generatedRoot.mjs, paths.mjs), so they
are never inside the checkout that runs compact; bounding them by that
checkout refused every worktree run - and only after compact had already
deleted public files and rewritten manifests.

compactRemovals bounds each tree by the root that owns it instead, and the
compact script calls it before its first mutation, so a refusal leaves the
tree untouched.

===========================================================================
*/
import { mkdir, writeFile } from "node:fs/promises";
import path from "node:path";

/*
================
assertInsideRoot

Refuse a target outside root, or root itself.
================
*/
function assertInsideRoot( root, target, label ) {
	const relative = path.relative( path.resolve( root ), path.resolve( target ) );
	if ( relative === "" || relative.startsWith( ".." ) || path.isAbsolute( relative ) ) {
		throw new Error( `${label} must stay below ${root}, got ${target}` );
	}
}

/*
================
assertBelowGeneratedFolder

The server projection may live in another checkout's Go module
(SRO_SERVER_GAME_DATA_ROOT, or a worktree's main checkout), so what every
valid location shares is that it sits below a .generated folder.
================
*/
function assertBelowGeneratedFolder( target, label ) {
	const parts = path.resolve( target ).split( path.sep );
	const marker = parts.lastIndexOf( ".generated" );
	if ( marker === -1 || marker === parts.length - 1 ) {
		throw new Error( `${label} must stay below a .generated folder, got ${target}` );
	}
}

/*
================
compactRemovals

The trees compact removes, each checked against its owner: the image
staging cache below the generated root, the server projection and its
extraction cache below a .generated folder. Throws before anything is
deleted. dropGeneratedCache adds the staging cache.
================
*/
export function compactRemovals( { generatedRoot, serverGameDataRoot, dropGeneratedCache } ) {
	const generatedAssetsRoot = path.join( generatedRoot, "intermediate" );
	const serverGameDataCacheRoot = path.join( path.dirname( serverGameDataRoot ), ".game-data-cache" );
	const removals = [];
	if ( dropGeneratedCache ) {
		assertInsideRoot( generatedRoot, generatedAssetsRoot, "generated image staging cache" );
		removals.push( { label: "generated image staging cache", target: generatedAssetsRoot } );
	}
	assertBelowGeneratedFolder( serverGameDataRoot, "loose server game-data projection" );
	assertBelowGeneratedFolder( serverGameDataCacheRoot, "server game-data extraction cache" );
	removals.push(
		{ label: "loose server game-data projection", target: serverGameDataRoot },
		{ label: "server game-data extraction cache", target: serverGameDataCacheRoot }
	);
	return removals;
}

/*
================
compactStatePath

The compact marker describes the generated tree, so it lives beside it,
not in the checkout that ran compact: a worktree compacts the shared tree,
and check_compact_assets reads the marker from any checkout.
================
*/
export function compactStatePath( generatedRoot ) {
	return path.join( generatedRoot, "compact-assets.json" );
}

/*
================
writeCompactState

Persist the completed compaction record at the same shared-tree location
the release checker reads, including when invoked from another worktree.
================
*/
export async function writeCompactState( generatedRoot, state ) {
	const statePath = compactStatePath( generatedRoot );
	await mkdir( path.dirname( statePath ), { recursive: true } );
	await writeFile( statePath, `${JSON.stringify( state, null, 2 )}\n`, "utf8" );
}
