/*
===========================================================================

chromeTraceCapture.mjs - bounded CDP trace capture for browser diagnostics

Persist large captures directly to disk; parsing is optional for analyzers.

===========================================================================
*/
import { mkdir, open, writeFile } from "node:fs/promises";
import { dirname } from "node:path";
import { promisify } from "node:util";
import { gzip } from "node:zlib";

const gzipAsync = promisify( gzip );

export const DEFAULT_BROWSER_EVENT_LOOP_TRACE_CATEGORIES = [
	"devtools.timeline",
	"disabled-by-default-devtools.timeline",
	"disabled-by-default-devtools.timeline.frame",
	"blink.user_timing",
	"toplevel",
	"v8.execute",
	"v8.gc",
	"cc",
	"gpu"
];

/*
================
startChromeTraceCapture
================
*/
export async function startChromeTraceCapture( page, options = {} ) {
	const session = await page.context().newCDPSession( page );
	const categories = options.categories ?? DEFAULT_BROWSER_EVENT_LOOP_TRACE_CATEGORIES;
	let stopped = false;

	await session.send( "Tracing.start", {
		categories: categories.join( "," ),
		options: options.options ?? "record-as-much-as-possible",
		transferMode: "ReturnAsStream"
	} );

	return {
		categories,
		/*
    ================
    stop

    Raw file mode avoids both the JavaScript string limit and an in-memory
    copy of every event. Analytical callers retain the parsed-events mode.
    ================
    */
		async stop( stopOptions = {} ) {
			if ( stopped ) throw new Error( "Chrome trace capture was already stopped" );
			stopped = true;
			const completed = new Promise( ( resolve ) => {
				session.once( "Tracing.tracingComplete", resolve );
			} );

			await session.send( "Tracing.end" );
			const { stream } = await completed;
			if ( !stream ) throw new Error( "Chrome trace completed without a stream handle" );

			const chunks = [];
			const rawOutput = stopOptions.rawOutputPath;
			if ( rawOutput ) await mkdir( dirname( rawOutput ), { recursive: true } );
			const output = rawOutput ? await open( rawOutput, "w" ) : null;
			let rawBytes = 0;
			try {
				for ( ;; ) {
					const piece = await session.send( "IO.read", {
						handle: stream,
						size: stopOptions.chunkBytes ?? 4 * 1024 * 1024
					} );
					const bytes = Buffer.from( piece.data, piece.base64Encoded ? "base64" : "utf8" );
					rawBytes += bytes.length;
					if ( output ) await output.writeFile( bytes );
					else chunks.push( bytes );
					if ( piece.eof ) break;
				}
			} finally {
				await output?.close();
				await session.send( "IO.close", { handle: stream } ).catch( () => undefined );
				await session.detach().catch( () => undefined );
			}
			if ( rawOutput ) return { categories, rawBytes, outputPath: rawOutput };

			const raw = Buffer.concat( chunks );
			const parsed = JSON.parse( raw.toString( "utf8" ) );
			if ( !Array.isArray( parsed.traceEvents ) ) {
				throw new Error( "Chrome trace did not contain a traceEvents array" );
			}

			if ( stopOptions.outputPath ) {
				await mkdir( dirname( stopOptions.outputPath ), { recursive: true } );
				await writeFile( stopOptions.outputPath, await gzipAsync( raw, { level: 6 } ) );
			}

			return {
				categories,
				rawBytes: raw.byteLength,
				eventCount: parsed.traceEvents.length,
				traceEvents: parsed.traceEvents,
				metadata: parsed.metadata ?? null,
				outputPath: stopOptions.outputPath ?? null
			};
		}
	};
}
