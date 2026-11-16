/*
===========================================================================

replay.mjs - replay one presentation session through one tree

Usage: node replay.mjs ROOT SESSION OUT

ROOT is a repository checkout (any worktree or copy whose generated root
resolves); SESSION a session file from scenarios.mjs (.json or .json.gz);
OUT the NDJSON the frames are written to, one line per frame as its change
from the frame before (frame-delta.mjs), gzipped when it ends in .gz.
Compare two outputs with diff.mjs.

The replay runs ROOT's character presentation (harness.mjs) and prints
the frame count, the run time and any renderer port member the
presentation read that the harness does not provide.

===========================================================================
*/
import { createWriteStream, readFileSync } from "node:fs";
import { once } from "node:events";
import path from "node:path";
import { gunzipSync, createGzip } from "node:zlib";
import { createPresentationHarness } from "./harness.mjs";
import { frameDelta } from "./frame-delta.mjs";

const USAGE = "usage: node replay.mjs ROOT SESSION OUT";
const PROGRESS_FRAMES = 600;

/*
================
readSession
================
*/
export function readSession( file ) {
	const bytes = readFileSync( file );
	return JSON.parse( (file.endsWith( ".gz" ) ? gunzipSync( bytes ) : bytes).toString( "utf8" ) );
}

/*
================
openOutput

A line sink with backpressure; gzip when the name says so.
================
*/
function openOutput( file ) {
	const sink = createWriteStream( file );
	const stream = file.endsWith( ".gz" ) ? createGzip() : sink;
	if ( stream !== sink ) stream.pipe( sink );
	return {
		/*
		================
		write
		================
		*/
		async write( line ) {
			if ( !stream.write( line + "\n" ) ) await once( stream, "drain" );
		},
		/*
		================
		close
		================
		*/
		async close() {
			stream.end();
			await once( sink, "close" );
		}
	};
}

/*
================
main
================
*/
async function main( argv ) {
	if ( argv.length !== 3 ) {
		console.error( USAGE );
		process.exit( 2 );
	}
	const [root, sessionFile, outFile] = argv.map( value => path.resolve( value ) );
	const session = readSession( sessionFile );
	const started = process.hrtime.bigint();
	const harness = await createPresentationHarness( root, session );
	const output = openOutput( outFile );
	try {
		let previous = null;
		for ( let index = 0; index < harness.frames; index++ ) {
			const frame = harness.step( index );
			await output.write( JSON.stringify( frameDelta( previous, frame ) ) );
			previous = frame;
			if ( index % PROGRESS_FRAMES === PROGRESS_FRAMES - 1 ) console.error( `${index + 1} / ${harness.frames}` );
		}
	} finally {
		harness.dispose();
		await output.close();
	}
	const seconds = Number( process.hrtime.bigint() - started ) / 1e9;
	console.log( `${session.name}: ${harness.frames} frames in ${seconds.toFixed( 1 )} s -> ${outFile}` );
	if ( harness.missing.size ) {
		console.error( `renderer members not provided: ${[ ...harness.missing ].join( ", " )}` );
	}
}

await main( process.argv.slice( 2 ) );
