/*
===========================================================================

generatedRoot.mjs - where the built asset tree lives

Every build product outside the Go module (client-public, the intermediate
images, the observatory catalog) lives under one generated root. It is
this checkout's own .generated, unless SRO_GENERATED_ROOT names another
absolute directory: an agent worktree points it at the main checkout's
built tree and reads the published assets with no junction and no copy.

Readers and writers resolve through here and nowhere else (the
check:generated-root gate enforces it), so moving or sharing the tree is
one environment variable. A build run with the variable set writes into
the shared tree, which is the canonical tree, not a private copy.

The server's game-data projection is a separate, module-local tree with
its own override (SRO_SERVER_GAME_DATA_ROOT, apps/server/AGENTS.md).

===========================================================================
*/
import path from "node:path";
import { fileURLToPath } from "node:url";

export const GENERATED_ROOT_ENV = "SRO_GENERATED_ROOT";

const repositoryRoot = fileURLToPath( new URL( "../../", import.meta.url ) );

/*
================
resolveGeneratedRoot

The override when set (it must be absolute: a relative one would follow
whichever directory a tool happened to start in), else the checkout's own.
================
*/
export function resolveGeneratedRoot( env = process.env ) {
	const override = env[GENERATED_ROOT_ENV];
	if ( override ) {
		if ( !path.isAbsolute( override ) ) {
			throw Error( GENERATED_ROOT_ENV + " must be an absolute path, not " + JSON.stringify( override ) );
		}
		return path.resolve( override );
	}
	return path.join( repositoryRoot, ".generated" );
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
