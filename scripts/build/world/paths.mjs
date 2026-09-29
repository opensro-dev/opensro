/*
===========================================================================

paths.mjs - the one owner of the pipeline's filesystem roots

Every asset builder, check and test resolves the repository, the game data
and the generated output through these exports; none derives them from its
own location. The repository root is this checkout. Generated output lives
in this checkout's .generated, so worktrees build in isolation. The game
data (extracted/) is shared and lives beside the MAIN checkout, which a
linked git worktree names in its .git file.

===========================================================================
*/
import fs from "node:fs";
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
	const dotGit = path.join( rebuildRoot, ".git" );
	if ( fs.existsSync( dotGit ) && fs.statSync( dotGit ).isFile() ) {
		const match = /^gitdir:\s*(.+)$/m.exec( fs.readFileSync( dotGit, "utf8" ) );
		if ( !match ) throw new Error( `Unreadable worktree link ${dotGit}` );
		const worktreeGitDir = path.resolve( rebuildRoot, match[1].trim() );
		const mainCheckout = path.resolve( worktreeGitDir, "..", "..", ".." );
		return path.resolve( mainCheckout, ".." );
	}
	return path.resolve( rebuildRoot, ".." );
}

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

export const generatedRoot = path.join( rebuildRoot, ".generated" );
export const publicRoot = path.join( generatedRoot, "client-public" );
export const publicAssetsRoot = path.join( publicRoot, "assets" );
export const imageSourceRoot = path.join( generatedRoot, "intermediate", "images" );
export const imagePublicRoot = path.join( publicAssetsRoot, "images" );

/*
================
toGameRelative
================
*/
export function toGameRelative( value, sourceGameRoot = gameRoot ) {
	return path.relative( sourceGameRoot, value ).replaceAll( "\\", "/" );
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
