/*
===========================================================================

profile.mjs - where a CPU or allocation profile spends its weight

Usage:
  node tools/perf/analyze/profile.mjs FILE [more...] [options]

Options:
  --top N          rows per table (default 30)
  --seconds S      the profiled duration: report per second (heap
                   profiles carry none; CPU profiles default to their own)
  --children FN    weight under each direct callee of functions whose key
                   starts with FN, and their own
  --callers FN     weight by the three frames calling functions whose key
                   starts with FN (a built-in's real allocators)
  --lines FN       CPU only: weight by source line inside FN
  --maps DIR       production source maps (beta package private/maps)
  --dev-root DIR   client root a dev server served (default: this client)

FILE is a .cpuprofile (time) or a sampled .heapprofile (bytes allocated;
tools/perf/bench --heap records one per scenario). Several files of one
kind are summed. Frames map back to client source (core/symbols.mjs).

===========================================================================
*/
import path from "node:path";
import { fileURLToPath } from "node:url";
import { createSymbolizer } from "../core/symbols.mjs";
import { readProfile, urlsOf, forEachStack, forEachLine } from "../core/profile.mjs";
import { parseOptions, table, add, createStackTotals } from "../core/report.mjs";

const CLIENT_ROOT = path.resolve( path.dirname( fileURLToPath( import.meta.url ) ), "../../.." );
const USAGE = "profile.mjs FILE [more...] [--top N] [--seconds S] [--children FN] [--callers FN] [--lines FN]";

/*
================
report
================
*/
async function report( options ) {
	if ( !options.files.length ) throw Error( `usage: ${USAGE}` );
	const symbolizer = await createSymbolizer( { maps: options.maps || undefined, root: options.devRoot } );
	const totals = createStackTotals( options.children || null, options.callers || null ), lines = new Map();
	let kind = null;
	for ( const file of options.files ) {
		const profile = readProfile( file );
		if ( kind && kind !== profile.kind ) throw Error( "profiles of different kinds cannot be summed" );
		kind = profile.kind;
		await symbolizer.prepare( urlsOf( profile ) );
		forEachStack( profile, symbolizer, ( stack, weight ) => totals.add( stack, weight ) );
		if ( options.lines ) {
			forEachLine( profile, symbolizer, options.lines, ( key, weight ) => add( lines, key, weight ) );
		}
	}
	// CPU weights are microseconds, heap weights bytes.
	const seconds = options.seconds || (kind === "cpu" ? totals.weight() / 1e6 : 0);
	const scale = kind === "cpu" ? 1000 * seconds : seconds ? 1048576 * seconds : 1048576;
	const unit = kind === "cpu" ? "ms/s" : seconds ? "MB/s" : "MB";
	const format = { scale, top: options.top, unit };
	const total = kind === "cpu" ?
		`${seconds.toFixed( 2 )} s sampled` :
		`${(totals.weight() / 1048576).toFixed( 1 )} MB allocated${
			seconds ? `, ${(totals.weight() / 1048576 / seconds).toFixed( 1 )} MB/s` : ""
		}`;
	const out = [ `# ${options.files.map( f => path.basename( f ) ).join( ", " )} (${kind}: ${total})` ];
	out.push( "\n## self\n" + table( totals.self, format ) );
	out.push( "\n## inclusive\n" + table( totals.total, format ) );
	if ( options.children ) out.push( `\n## children of ${options.children}\n` + table( totals.children, format ) );
	if ( options.callers ) out.push( `\n## callers of ${options.callers}\n` + table( totals.callers, format ) );
	if ( options.lines ) out.push( `\n## lines of ${options.lines}\n` + table( lines, format ) );
	console.log( out.join( "\n" ) );
}

await report( parseOptions(
	process.argv.slice( 2 ),
	{ top: 30, seconds: 0, children: "", callers: "", lines: "", maps: "", devRoot: CLIENT_ROOT },
	USAGE
) );
