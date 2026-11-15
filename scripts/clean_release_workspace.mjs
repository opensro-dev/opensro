/*
===========================================================================

Release Workspace Cleanup

Removes only enumerated, reproducible outputs left by release verification.
Authored source, reconstruction evidence, compact release assets, and compact
state are deliberately outside this allow-list.

===========================================================================
*/

import { rm, stat } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { BUILD_STATE_ROOT } from "./lib/generatedRoot.mjs";

const scriptDirectory = path.dirname( fileURLToPath( import.meta.url ) );
const rebuildRoot = path.resolve( scriptDirectory, ".." );
const alwaysRemovable = [
	".state/archive-dogfood-cache",
	".state/probe-gm.log",
	".state/chat-probe-085806432.err.log",
	".state/chat-probe-085806432.out.log",
	".state/check-pipeline.log",
	".state/check-dogfood",
	".state/codex-validation",
	"temp/runlogs",
	"temp/callgraph-out",
	"scripts/__pycache__"
];
// The build's reuse caches live in the build state beside the shared
// generated tree (generatedRoot.mjs buildStatePath). A worktree's own
// .state copies of them were written by builds before that move and no
// build reads them now. In the checkout whose .state is the build state
// (the main checkout), the same names are the live caches, and its locks
// are the shared generated-assets lock other worktrees' builds hold, so
// there neither group is touched.
const supersededCaches = [
	".state/file-hash-cache.json",
	".state/json-minify-cache.json",
	".state/outdoorPayloadVerdicts.json",
	".state/asset-integrity-hash-cache.json",
	".state/resource-build-fingerprint.json",
	".state/lane-memo",
	".state/pack-member-gzip",
	".state/catalogue-webp-cache"
];
const checkoutLocks = [ ".state/locks" ];
const holdsBuildState = path.relative( path.resolve( rebuildRoot, ".state" ), path.resolve( BUILD_STATE_ROOT ) ) === "";
const removablePaths = holdsBuildState ?
	alwaysRemovable :
	[ ...alwaysRemovable, ...supersededCaches, ...checkoutLocks ];

let removedBytes = 0;
let removedTargets = 0;
for ( const relativePath of removablePaths ) {
	const targetPath = path.resolve( rebuildRoot, relativePath );
	assertInsideRebuild( targetPath );
	const targetStats = await stat( targetPath ).catch( () => undefined );
	if ( !targetStats ) {
		continue;
	}
	removedBytes += await measurePath( targetPath );
	await rm( targetPath, { recursive: targetStats.isDirectory(), force: true } );
	removedTargets += 1;
	console.log( `removed ${relativePath}` );
}

console.log(
	`Release workspace cleanup removed ${removedTargets} target(s), ` +
		`${(removedBytes / (1024 ** 2)).toFixed( 1 )} MiB.`
);

/*
================
assertInsideRebuild
================
*/
function assertInsideRebuild( targetPath ) {
	const relativePath = path.relative( rebuildRoot, targetPath );
	if ( relativePath === "" || relativePath.startsWith( ".." ) || path.isAbsolute( relativePath ) ) {
		throw new Error( `Cleanup target escapes the rebuild root: ${targetPath}` );
	}
}

/*
================
measurePath
================
*/
async function measurePath( targetPath ) {
	const targetStats = await stat( targetPath );
	if ( targetStats.isFile() ) {
		return targetStats.size;
	}
	const { readdir } = await import( "node:fs/promises" );
	const entries = await readdir( targetPath, { withFileTypes: true } );
	let bytes = 0;
	for ( const entry of entries ) {
		bytes += await measurePath( path.join( targetPath, entry.name ) );
	}
	return bytes;
}
