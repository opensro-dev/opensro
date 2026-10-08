/*
===========================================================================

paths.mjs - the one owner of the pipeline's filesystem roots

Every asset builder, check and test resolves the repository, the game data
and the generated output through these exports; none derives them from its
own location. The repository root is this checkout. Generated output lives
in this checkout's .generated unless SRO_GENERATED_ROOT names another tree
(scripts/lib/generatedRoot.mjs owns that rule), so a worktree can read the
main checkout's build instead of linking or copying it. The game
data (extracted/) is shared and lives beside the MAIN checkout, which a
linked git worktree names in its .git file.

===========================================================================
*/
import { GENERATED_ROOT, MAIN_CHECKOUT_ROOT } from "../../lib/generatedRoot.mjs";
import path from "node:path";
import { fileURLToPath } from "node:url";

export { normalizeAssetPath } from "../shared/assetPaths.mjs";

const scriptDir = path.dirname( fileURLToPath( import.meta.url ) );

export const rebuildRoot = path.resolve( scriptDir, "..", "..", ".." );

/*
================
resolveGameRoot

The directory holding extracted/: SRO_GAME_ROOT when the operator sets it,
else the parent of the main checkout. A linked worktree's .git is a file
"gitdir: <main>/.git/worktrees/<name>"; a main checkout's .git is a directory.
================
*/
function resolveGameRoot() {
	const configured = process.env.SRO_GAME_ROOT?.trim();
	if ( configured ) return path.resolve( configured );
	return path.resolve( mainCheckoutRoot, ".." );
}

// The main checkout (scripts/lib/generatedRoot.mjs owns the rule).
export const mainCheckoutRoot = MAIN_CHECKOUT_ROOT;

export const gameRoot = resolveGameRoot();
// Where gameRoot came from, for reports: the variable or the checkout layout.
export const gameRootSource = process.env.SRO_GAME_ROOT?.trim() ? "SRO_GAME_ROOT" : "beside the main checkout";
export const extractedRoot = path.join( gameRoot, "extracted" );
export const dataExtractedRoot = path.join( extractedRoot, "Data_extracted" );
export const mapExtractedRoot = path.join( extractedRoot, "Map_extracted" );
export const mediaExtractedRoot = path.join( extractedRoot, "Media_extracted" );

// Native client-owned table root loaded through `%stextdata\\...` format
// strings. Keep this distinct from `Media_extracted/resinfo`: resinfo may
// contain older UI-facing mirrors and is not a substitute for retail table
// ownership.
export const retailTextdataRoot = path.join( mediaExtractedRoot, "server_dep", "silkroad", "textdata" );

// The supplied v1.150 client archive's matching skilleffect mirror. It is
// corroborating evidence for newer server rows, not a replacement for the
// complete server textdata corpus.
export const clientV150ResinfoRoot = path.join( mediaExtractedRoot, "resinfo" );

// The generated tree has one owner (scripts/lib/generatedRoot.mjs), which
// honours SRO_GENERATED_ROOT.
export const generatedRoot = GENERATED_ROOT;

// gamedata.EnvRoot (apps/server/internal/gamedata/resolve.go): the Go server
// and its tests read the projection from here when it is set.
export const SERVER_GAME_DATA_ROOT_ENV = "SRO_SERVER_GAME_DATA_ROOT";

/*
================
resolveServerGameDataRoot

The verified server game-data projection lives inside the Go module: its
tests then read only module files, and `go test` validates cached results
against the data itself (apps/server/AGENTS.md). Git-ignored there. It is
the main checkout's projection, so a linked worktree reads the shared one
with no environment; SRO_SERVER_GAME_DATA_ROOT, the variable the Go server
reads, overrides it. Only an absolute override is accepted, as for
SRO_GENERATED_ROOT, so the answer never depends on the working directory.
================
*/
export function resolveServerGameDataRoot( env = process.env, mainCheckout = MAIN_CHECKOUT_ROOT ) {
	const override = env[SERVER_GAME_DATA_ROOT_ENV]?.trim();
	if ( override ) {
		if ( !path.isAbsolute( override ) ) {
			throw Error( `${SERVER_GAME_DATA_ROOT_ENV} must be an absolute path, not ${JSON.stringify( override )}` );
		}
		return path.resolve( override );
	}
	return path.join( mainCheckout, "apps", "server", ".generated", "game-data", "1.150", "server" );
}

export const serverGameDataRoot = resolveServerGameDataRoot();
export const publicRoot = path.join( generatedRoot, "client-public" );
export const publicAssetsRoot = path.join( publicRoot, "assets" );
export const imageSourceRoot = path.join( generatedRoot, "intermediate", "images" );
export const imagePublicRoot = path.join( publicAssetsRoot, "images" );

/*
================
gameRelativePath

value relative to the game root, with a file inside this checkout named as
the main checkout's file. Built assets record these paths, so a linked
worktree - even one on another drive, where no relative path exists - builds
the same bytes the main checkout does.
================
*/
export function gameRelativePath( value, roots ) {
	const inCheckout = path.relative( roots.checkout, value );
	const named = inCheckout.startsWith( ".." ) || path.isAbsolute( inCheckout ) ?
		value :
		path.join( roots.mainCheckout, inCheckout );
	return path.relative( roots.game, named ).replaceAll( "\\", "/" );
}

/*
================
toGameRelative
================
*/
export function toGameRelative( value, sourceGameRoot = gameRoot ) {
	return gameRelativePath( value, { checkout: rebuildRoot, mainCheckout: mainCheckoutRoot, game: sourceGameRoot } );
}

/*
================
toHex16
================
*/
export function toHex16( value ) {
	return `0x${value.toString( 16 ).padStart( 4, "0" )}`;
}

/*
================
normalizeRegionId

A numeric or hexadecimal text region id in the 0xXXXX form.
================
*/
export function normalizeRegionId( value ) {
	if ( typeof value === "number" ) {
		return toHex16( value & 0xffff );
	}
	const text = String( value ).trim().replace( /^0x/i, "" );
	if ( !/^[0-9a-f]{1,4}$/i.test( text ) ) {
		throw new Error( `Invalid 16-bit region id ${JSON.stringify( value )}` );
	}
	return `0x${text.toLowerCase().padStart( 4, "0" )}`;
}

/*
================
regionIdFromSectorCoordinates

The retail region id from its unsigned sector bytes.
================
*/
export function regionIdFromSectorCoordinates( sectorX, sectorY ) {
	return toHex16( ((sectorY & 0xff) << 8) | (sectorX & 0xff) );
}
