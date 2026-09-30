/*
===========================================================================

convertImagesRunner.mjs - the async, serialized runner for convert_images.py

The old call sites used spawnSync, which blocks the Node event loop and
stalls every other parallel build lane for the whole Python run. This runner
spawns asynchronously so the other lanes keep making progress, but keeps all
convert_images.py invocations in this process strictly serialized:
convert_images.py rewrites the image manifest from scratch on every run, so
two concurrent invocations would truncate or interleave each other's rows.

The contract mirrors spawnSync: it resolves { status }, the exit code, or
null when the process could not be spawned or died from a signal, and it
never rejects. A successful unfiltered pass covers every filtered tree, so
filtered passes after it are skipped.

===========================================================================
*/

import { spawn } from "node:child_process";
import path from "node:path";
import { fileURLToPath } from "node:url";

const rebuildRoot = path.resolve( path.dirname( fileURLToPath( import.meta.url ) ), "..", "..", ".." );
const CONVERT_IMAGES_SCRIPT = path.join( rebuildRoot, "scripts", "convert_images.py" );

/*
================
spawnConvertImages

One convert_images.py process with the given CLI filters.
================
*/
function spawnConvertImages( args ) {
	return new Promise( ( resolve ) => {
		const child = spawn( "py", [ CONVERT_IMAGES_SCRIPT, ...args ], { stdio: "inherit" } );
		let settled = false;
		const settle = ( status ) => {
			if ( settled ) return;
			settled = true;
			resolve( { status } );
		};
		child.on( "error", () => settle( null ) );
		child.on( "close", ( code ) => settle( code ) );
	} );
}

/*
================
createConvertImagesRunner

A runner that serializes every call through spawnConversion and skips
filtered passes once an unfiltered pass has succeeded.
================
*/
export function createConvertImagesRunner( spawnConversion ) {
	// Tail of the serialization chain: each call appends itself, so two runs
	// never overlap even when parallel lanes call in concurrently.
	let queueTail = Promise.resolve();
	let fullConversionComplete = false;
	return function runConvertImages( args ) {
		const run = queueTail.then( async () => {
			// The full resource build runs the unfiltered pass first so compacted
			// workspaces recreate their staging cache; later model builders need
			// not rescan the same extracted corpus.
			if ( fullConversionComplete && args.length > 0 ) return { status: 0 };
			const result = await spawnConversion( args );
			if ( args.length === 0 && result.status === 0 ) fullConversionComplete = true;
			return result;
		} );
		queueTail = run.then( () => undefined, () => undefined );
		return run;
	};
}

/*
================
runConvertImages

The process-wide runner: run convert_images.py with the given CLI filters.
Resolves { status }.
================
*/
export const runConvertImages = createConvertImagesRunner( spawnConvertImages );
