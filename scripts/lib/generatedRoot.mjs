/*
===========================================================================

generatedRoot.mjs - where the built asset tree lives

Every build product outside the Go module (client-public, the intermediate
images, the observatory catalog) lives under one generated root: the MAIN
checkout's .generated. A linked git worktree names the main checkout in its
.git file, so an agent worktree reads (and builds into) the one shared tree
with no junction, no copy and no environment. SRO_GENERATED_ROOT, an
absolute directory, overrides it.

Readers and writers resolve through here and nowhere else (the
check:generated-root gate enforces it), so moving or sharing the tree is
one environment variable. A build run with the variable set writes into
the shared tree, which is the canonical tree, not a private copy.

The server's game-data projection is a separate, module-local tree with
its own override (SRO_SERVER_GAME_DATA_ROOT, apps/server/AGENTS.md).

===========================================================================
*/
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

export const GENERATED_ROOT_ENV = "SRO_GENERATED_ROOT";

const repositoryRoot = fileURLToPath( new URL( "../../", import.meta.url ) );

/*
================
resolveMainCheckout

The main checkout: checkout itself, or the one a linked worktree's .git file
names ("gitdir: <main>/.git/worktrees/<name>"; a main checkout's .git is a
directory). The Python twin is scripts/sro_paths.py, the Go one
config.MainCheckoutRoot.
================
*/
export function resolveMainCheckout( checkout ) {
	const dotGit = path.join( checkout, ".git" );
	if ( !fs.existsSync( dotGit ) || !fs.statSync( dotGit ).isFile() ) return path.resolve( checkout );
	const match = /^gitdir:\s*(.+)$/m.exec( fs.readFileSync( dotGit, "utf8" ) );
	if ( !match ) throw new Error( `Unreadable worktree link ${dotGit}` );
	const worktreeGitDir = path.resolve( checkout, match[1].trim() );
	return path.resolve( worktreeGitDir, "..", "..", ".." );
}

export const MAIN_CHECKOUT_ROOT = resolveMainCheckout( repositoryRoot );

/*
================
resolveGeneratedRoot

The override when set (it must be absolute: a relative one would follow
whichever directory a tool happened to start in), else the main checkout's.
================
*/
export function resolveGeneratedRoot( env = process.env, mainCheckout = MAIN_CHECKOUT_ROOT ) {
	const override = env[GENERATED_ROOT_ENV];
	if ( override ) {
		if ( !path.isAbsolute( override ) ) {
			throw Error( GENERATED_ROOT_ENV + " must be an absolute path, not " + JSON.stringify( override ) );
		}
		return path.resolve( override );
	}
	return path.join( mainCheckout, ".generated" );
}

export const GENERATED_ROOT = resolveGeneratedRoot();
export const CLIENT_PUBLIC_ROOT = path.join( GENERATED_ROOT, "client-public" );
export const INTERMEDIATE_ROOT = path.join( GENERATED_ROOT, "intermediate" );

/*
================
generatedPath

A path inside the generated root.
================
*/
export function generatedPath( ...parts ) {
	return path.join( GENERATED_ROOT, ...parts );
}

/*
================
clientPublicPath

A path inside the published client tree (client-public).
================
*/
export function clientPublicPath( ...parts ) {
	return path.join( CLIENT_PUBLIC_ROOT, ...parts );
}
