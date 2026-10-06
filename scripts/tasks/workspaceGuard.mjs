/*
===========================================================================

workspaceGuard.mjs - a task never runs over a linked node_modules

pnpm resolves a node_modules that is a junction or symlink to the directory
it points at. A task run in a worktree whose node_modules linked to the
main checkout's let pnpm's dependency check relink the main checkout's
packages into the worktree; deleting the worktree then broke dprint,
typescript and vite for every agent (2026-10-04, 10-05 and 10-06).

A worktree installs its own instead: `pnpm install --frozen-lockfile
--offline` hard-links from the shared store in seconds and downloads
nothing. So a linked node_modules is refused before any task starts.

===========================================================================
*/
import { lstatSync, readdirSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const repositoryRoot = fileURLToPath( new URL( "../../", import.meta.url ) );

/*
================
workspaceNodeModules

The root node_modules and each workspace package's (pnpm-workspace.yaml:
apps/*).
================
*/
function workspaceNodeModules( root ) {
	const apps = path.join( root, "apps" );
	let packages = [];
	try {
		packages = readdirSync( apps, { withFileTypes: true } ).filter( entry => entry.isDirectory() );
	} catch {
		packages = [];
	}
	return [
		path.join( root, "node_modules" ),
		...packages.map( entry => path.join( apps, entry.name, "node_modules" ) )
	];
}

/*
================
linkedNodeModules

Every workspace node_modules that is a link (Node reports a Windows
junction as a symbolic link). A missing one is not a link.
================
*/
export function linkedNodeModules( root = repositoryRoot ) {
	return workspaceNodeModules( root ).filter( candidate => {
		try {
			return lstatSync( candidate ).isSymbolicLink();
		} catch {
			return false;
		}
	} );
}

/*
================
assertRealNodeModules
================
*/
export function assertRealNodeModules( root = repositoryRoot ) {
	const linked = linkedNodeModules( root );
	if ( linked.length === 0 ) return;
	throw Error(
		"node_modules must be a real directory, not a link:\n  " + linked.join( "\n  " ) +
			"\nRemove the link (rmdir, never a recursive delete) and run `pnpm install --frozen-lockfile --offline` " +
			"in this checkout: the shared store makes it a few seconds and leaves every other checkout alone."
	);
}
