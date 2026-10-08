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

The converter keeps a PNG newer than its source, which says nothing about
the code that wrote it. Its code stamp (codeStamp.mjs, over convert_images.py
and the modules beside it that it imports) forces every pass to reconvert
until an unfiltered pass under the current code succeeds.

===========================================================================
*/

import { spawn } from "node:child_process";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { codeHash, stampIsCurrent, writeStamp } from "./codeStamp.mjs";
import { pythonAttempts } from "./pythonRun.mjs";

const rebuildRoot = path.resolve( path.dirname( fileURLToPath( import.meta.url ) ), "..", "..", ".." );
const CONVERT_IMAGES_SCRIPT = path.join( rebuildRoot, "scripts", "convert_images.py" );
const CONVERTER_CODE_STAMP = "image-conversion";

/*
================
spawnConvertImages

One convert_images.py process with the given CLI filters, forced to
reconvert while the converter's code stamp is stale.
================
*/
async function spawnConvertImages( args ) {
	const converterHash = await codeHash( CONVERT_IMAGES_SCRIPT );
	const codeCurrent = await stampIsCurrent( CONVERTER_CODE_STAMP, converterHash );
	const env = codeCurrent ? process.env : { ...process.env, SRO_FORCE_IMAGE_CONVERT: "1" };
	if ( !codeCurrent ) console.log( "[convert_images] converter code changed since the last full pass; reconverting" );
	const result = await spawnPython( args, env );
	if ( !codeCurrent && args.length === 0 && result.status === 0 ) {
		await writeStamp( CONVERTER_CODE_STAMP, converterHash );
	}
	return result;
}

/*
================
spawnPython
================
*/
function spawnPython( args, env ) {
	return new Promise( ( resolve ) => {
		const [{ command, args: prefix }] = pythonAttempts( [] );
		const child = spawn( command, [ ...prefix, CONVERT_IMAGES_SCRIPT, ...args ], { stdio: "inherit", env } );
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

/*
================
textureConversionSkipped

SRO_SKIP_TEXTURE_CONVERT=1 reuses the converted images already on disk (a
fingerprinted build knob for iterating on a model builder).
================
*/
export function textureConversionSkipped() {
	return process.env.SRO_SKIP_TEXTURE_CONVERT === "1";
}

/*
================
convertTextureTrees

Converts every source texture under the given extracted subtrees before a
model builder reads them. A failed conversion fails the builder: carrying
on would publish models textured from whatever an earlier run left on this
machine, and untextured ones on a fresh clone.
================
*/
export async function convertTextureTrees( label, trees ) {
	if ( textureConversionSkipped() ) {
		console.log( `[${label}] skipping texture conversion (SRO_SKIP_TEXTURE_CONVERT=1)` );
		return;
	}
	const result = await runConvertImages( trees );
	if ( result.status !== 0 ) {
		throw new Error(
			`[${label}] texture conversion of ${trees.join( ", " )} exited ${result.status}; ` +
				"intermediate/image-conversion-failures.txt lists the files"
		);
	}
}
