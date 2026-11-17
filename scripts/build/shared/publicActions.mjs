/*
===========================================================================

publicActions.mjs - build lanes as actions that write the public tree

A lane leaving the lane memo (plan B5) becomes one or more actions
(graph/action.mjs) whose outputs are files players download. Two things
every such lane needs, and this module owns:

	runPublicActions  runs the actions from the store and claims every
	                  declared output in the open publication: a hit is
	                  laid down by materialize, which does not claim, and a
	                  file nobody claimed is refused as a local leftover
	                  (publicationLedger.mjs)
	pythonToolchain   the interpreter and module versions a Python step
	                  writes with, as one key param: a new Pillow or
	                  fontTools may write other bytes, so it misses every key

===========================================================================
*/
import { outputPath } from "../graph/action.mjs";
import { runGraph } from "../graph/graph.mjs";
import { openStore } from "../graph/store.mjs";
import { claimPublicFile } from "./publicationLedger.mjs";
import { runPython } from "./pythonRun.mjs";
import { rebuildRoot } from "../world/paths.mjs";

// Prints "python <version>" and "<module> <version>" for each named module,
// one per line; a module without __version__ prints its file's absence as "-".
const TOOLCHAIN_SCRIPT = [
	"import importlib, sys",
	"print('python', sys.version.split()[0])",
	"for name in sys.argv[1:]:",
	"    module = importlib.import_module(name)",
	"    print(name, getattr(module, '__version__', getattr(module, 'version', '-')))"
].join( "\n" );

const toolchains = new Map();

/*
================
pythonToolchain

The Python and module versions as one string, probed once per process
for each module list.
================
*/
export function pythonToolchain( modules ) {
	const key = modules.join( "," );
	if ( !toolchains.has( key ) ) {
		toolchains.set(
			key,
			runPython( [ "-c", TOOLCHAIN_SCRIPT, ...modules ], {
				task: `Read the ${[ "python", ...modules ].join( ", " )} versions for the build's action keys`,
				cwd: rebuildRoot
			} ).then( ( { stdout } ) => stdout.trim().split( /\r?\n/ ).join( "; " ) )
		);
	}
	return toolchains.get( key );
}

/*
================
runPublicActions

Runs actions from the store (options.store, else the default one, shut
down after) and claims every declared output. label names the lane in
its summary line. Returns runGraph's results by action name.
================
*/
export async function runPublicActions( label, actions, options = {} ) {
	const store = options.store ?? (await openStore());
	try {
		const results = await runGraph( actions, { store, env: {} } );
		for ( const action of actions ) {
			for ( const name of action.outputs ) claimPublicFile( outputPath( action, name ) );
		}
		const hits = Object.values( results ).filter( result => result.hit ).length;
		console.log(
			`[${label}-actions] ${actions.length} actions: ${hits} from the store, ${actions.length - hits} run`
		);
		return results;
	} finally {
		if ( !options.store ) await store.shutdown();
	}
}
