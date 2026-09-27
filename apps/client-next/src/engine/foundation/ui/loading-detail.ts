/*
===========================================================================

loading-detail.ts - the one-line account of what a loading screen is doing

The boot and loading screens carry a single short detail line under the
progress bar. It names the current step, what is being fetched in plain
words, and the live transfer rate, so a slow connection reads as slow rather
than hung. When transfers are open but no bytes have arrived for a while, it
says it is waiting for the server instead of showing a stale rate.

Pure functions of the asset progress; the platform owns the clock and the
DOM line.

===========================================================================
*/

import type { AssetProgress } from "@/engine/contracts/assets";

// No bytes for this long while transfers are open reads as "waiting".
export const LOADING_STALL_MS = 3000;

const BYTES_PER_KB = 1000;
const BYTES_PER_MB = 1000000;

/*
================
packGroupName

An asset pack group (assetPackOwnership.mjs) in player words.
================
*/
function packGroupName( group: string ): string {
	switch ( group ) {
		case "cosmetic-models":
			return "costumes";
		case "developer-labs":
			return "test content";
		case "equipment-models":
			return "equipment";
		case "game-audio":
			return "sounds";
		case "game-data":
			return "game data";
		case "game-images":
			return "interface images";
		case "game-models":
			return "character models";
		case "hwan-models":
			return "effects";
		case "mission-cos-models":
			return "pets and mounts";
		case "mission-minimap":
			return "minimap";
		case "mission-npc-vat":
			return "creatures";
		case "native-ui":
			return "interface";
		case "outdoor-world":
			return "world terrain";
		case "title-crowd-vat":
			return "crowds";
		default:
			return group.replaceAll( "-", " " );
	}
}

/*
================
assetFolderName

A top-level folder under /assets/ in player words, for files outside the
packs. Empty for a folder with no player-facing name.
================
*/
function assetFolderName( folder: string ): string {
	switch ( folder ) {
		case "audio":
			return "sounds";
		case "char":
			return "characters";
		case "effects":
			return "effects";
		case "fonts":
			return "fonts";
		case "images":
			return "images";
		case "interface":
		case "ui":
			return "interface";
		case "npc":
			return "creatures";
		case "text":
		case "textdata":
			return "text";
		case "world":
			return "world terrain";
		default:
			return "";
	}
}

/*
================
loadingFileLabel

Plain words for the file being fetched: its pack group for a pack
(/assets/packs/game-images-004-<hash>.bin -> "interface images"), else its
top-level asset folder. Empty when nothing names it.
================
*/
export function loadingFileLabel( file: string ): string {
	if ( !file ) return "";
	let path = file;
	try {
		path = decodeURIComponent( new URL( file, "http://local" ).pathname );
	} catch {
		// Not a URL; use it as a path.
	}
	const pack = /\/packs\/(?:.*\/)?([a-z-]+?)-\d{3}-[0-9a-f]+\.bin(?:\.zst)?$/i.exec( path );
	if ( pack ) return packGroupName( pack[1]!.toLowerCase() );
	const folder = /^\/assets\/([^/]+)\//i.exec( path );
	if ( folder ) return assetFolderName( folder[1]!.toLowerCase() );
	return "";
}

/*
================
transferRateText

"640 KB/s" below one megabyte per second, "1.8 MB/s" above.
================
*/
export function transferRateText( bytesPerSecond: number ): string {
	if ( bytesPerSecond >= BYTES_PER_MB ) return `${(bytesPerSecond / BYTES_PER_MB).toFixed( 1 )} MB/s`;
	return `${Math.max( 1, Math.round( bytesPerSecond / BYTES_PER_KB ) )} KB/s`;
}

/*
================
loadingDetailText

The detail line: the step, then what is being fetched and how fast.
`quietMs` is how long ago the byte count last grew.

	Loading models · interface images · 1.2 MB/s
	Loading models · interface images · waiting for server
	Loading models
================
*/
export function loadingDetailText( step: string, progress: AssetProgress | null | undefined, quietMs: number ): string {
	if ( !progress || progress.filesActive <= 0 ) return step;
	// "Loading characters..." reads badly with more after it.
	const parts = [ step.replace( /\s*(?:\.{3}|…)$/u, "" ) ];
	const what = loadingFileLabel( progress.currentFile );
	if ( what ) parts.push( what );
	if ( quietMs >= LOADING_STALL_MS ) parts.push( "waiting for server" );
	else if ( progress.bytesPerSecond > 0 ) parts.push( transferRateText( progress.bytesPerSecond ) );
	return parts.join( " · " );
}
