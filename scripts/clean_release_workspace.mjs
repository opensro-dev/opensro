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

const scriptDirectory = path.dirname( fileURLToPath( import.meta.url ) );
const rebuildRoot = path.resolve( scriptDirectory, ".." );
// The build's reuse caches are not here: they live beside the shared
// generated tree (generatedRoot.mjs buildStatePath), not in this checkout,
// and the next build on any worktree reuses them.
const removablePaths = [
	".state/archive-dogfood-cache",
	".state/probe-gm.log",
	".state/chat-probe-085806432.err.log",
	".state/chat-probe-085806432.out.log",
	".state/check-pipeline.log",
	".state/check-dogfood",
	".state/codex-validation",
	".state/locks",
	"temp/runlogs",
	"temp/callgraph-out",
	"scripts/__pycache__"
];

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
