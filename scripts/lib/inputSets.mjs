/*
===========================================================================

inputSets.mjs - the named lists of what a gate or build step reads

scripts/data/input-sets.json names each set once: the client check's inputs,
the Go server gate's inputs. run_if_changed.mjs fingerprints a set to skip
a gate whose inputs did not change, and the release graph (opensro-ops
release_data.py) keys its steps on the same sets read from the freeze, so
a release reruns only the steps whose inputs moved. A path is a file or
directory of the checkout; `@cmd,arg` is a command whose output counts (a
toolchain version); `!path` is a git-ignored file that still counts (a
generated manifest).

===========================================================================
*/

import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

export const INPUT_SETS_FORMAT = "opensro-input-sets-v1";
export const INPUT_SETS_PATH = path.resolve(
	path.dirname( fileURLToPath( import.meta.url ) ),
	"..",
	"data",
	"input-sets.json"
);

/*
================
inputSet

The inputs of one named set. An unknown name or a file in another format
is refused: a gate fingerprinting nothing would always be "up to date".
================
*/
export function inputSet( name, file = INPUT_SETS_PATH ) {
	const document = JSON.parse( readFileSync( file, "utf8" ) );
	if ( document.format !== INPUT_SETS_FORMAT ) {
		throw new Error( `${file} is not a ${INPUT_SETS_FORMAT} document` );
	}
	const inputs = document.sets?.[name];
	if ( !Array.isArray( inputs ) || inputs.length === 0 ) {
		throw new Error( `${file} names no input set "${name}"` );
	}
	return inputs;
}
