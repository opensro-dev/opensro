/*
===========================================================================

cursors.mjs - the native cursors as one action

The cursors lane (plan B5): extract_client_cursors.py decodes the ten
RT_GROUP_CURSOR resources the browser client uses out of SRO_Client.exe
into PNGs. It is one action (graph/action.mjs) keyed on the client's
bytes, the script's code closure, the cursor ids and the Python and
pefile versions, so a build whose client did not change lays the PNGs
down from the store instead of starting Python.

===========================================================================
*/
import path from "node:path";
import { defineAction } from "./graph/action.mjs";
import { pythonToolchain, runPublicActions } from "./shared/publicActions.mjs";
import { runPython } from "./shared/pythonRun.mjs";
import { gameRoot, publicRoot, rebuildRoot } from "./world/paths.mjs";

// The cursor params the browser client draws (extract_client_cursors.py
// names each one).
export const RETAIL_CURSOR_IDS = Object.freeze( [
	"0x95",
	"0x96",
	"0x97",
	"0x98",
	"0x99",
	"0x9a",
	"0xa0",
	"0xa1",
	"0xa3",
	"0xa6"
] );
const CURSOR_SCRIPT = path.join( rebuildRoot, "scripts", "extract_client_cursors.py" );
// The script reads the client from the game root (sro_paths.GAME_ROOT).
const CLIENT_EXE = "SRO_Client.exe";

/*
================
cursorAction

The ten cursors as one action. roots.game holds the client, roots.cursors
receives the PNGs; extract( ids ) writes them (the Python extractor by
default).
================
*/
export function cursorAction( toolchain, roots, extract = extractWithPython ) {
	return defineAction( {
		name: "cursors",
		inputRoot: roots.game,
		inputs: [ CLIENT_EXE ],
		outputRoot: roots.cursors,
		outputs: RETAIL_CURSOR_IDS.map( id => `sro_client_cursor_${id}.png` ),
		code: [ CURSOR_SCRIPT ],
		params: { ids: RETAIL_CURSOR_IDS, toolchain },
		run: () => extract( RETAIL_CURSOR_IDS )
	} );
}

/*
================
extractWithPython

Runs the extractor; it writes into the public cursor folder that
sro_paths.py resolves, the same folder publicRoot names here.
================
*/
async function extractWithPython( ids ) {
	const result = await runPython( [ CURSOR_SCRIPT, ...ids ], {
		task: "[cursors] embedded retail cursor extraction",
		cwd: rebuildRoot,
		context: [
			"Browser cursor paths are client presentation resources and must be published before pack indexing."
		]
	} );
	const output = result.stdout.trim();
	if ( output ) console.log( output );
}

/*
================
extractRetailCursors

The lane. options: store, toolchain and extract, for tests.
================
*/
export async function extractRetailCursors( options = {} ) {
	const toolchain = options.toolchain ?? (await pythonToolchain( [ "pefile" ] ));
	const roots = options.roots ?? { game: gameRoot, cursors: path.join( publicRoot, "assets", "cursors" ) };
	await runPublicActions( "cursor", [ cursorAction( toolchain, roots, options.extract ) ], { store: options.store } );
	return { count: RETAIL_CURSOR_IDS.length };
}
