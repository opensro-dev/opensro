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
import fs from "node:fs";
import { GENERATED_ROOT } from "../../lib/generatedRoot.mjs";
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

/*
================
resolveMainCheckout

The main checkout: this one, or the one a linked worktree's .git file names.
================
*/
function resolveMainCheckout() {
	const dotGit = path.join( rebuildRoot, ".git" );
	if ( fs.existsSync( dotGit ) && fs.statSync( dotGit ).isFile() ) {
		const match = /^gitdir:\s*(.+)$/m.exec( fs.readFileSync( dotGit, "utf8" ) );
		if ( !match ) throw new Error( `Unreadable worktree link ${dotGit}` );
		const worktreeGitDir = path.resolve( rebuildRoot, match[1].trim() );
		return path.resolve( worktreeGitDir, "..", "..", ".." );
	}
	return rebuildRoot;
}

export const mainCheckoutRoot = resolveMainCheckout();

export const gameRoot = resolveGameRoot();
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

// The verified server game-data projection lives inside the Go module: its
// tests then read only module files, and `go test` validates cached results
// against the data itself (apps/server/AGENTS.md). Git-ignored there.
export const serverGameDataRoot = path.join(
	rebuildRoot,
	"apps",
	"server",
	".generated",
	"game-data",
	"1.150",
	"server"
);
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
