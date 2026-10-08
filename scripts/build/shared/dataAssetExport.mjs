import path from "node:path";
import { writeJsonIfChangedSync } from "./jsonOut.mjs";

/** Write one generated JSON data asset below the canonical public data root. */
export function exportDataAsset( { publicRoot, outputFileName, value } ) {
	const outPath = path.join( publicRoot, "assets", "data", outputFileName );
	const changed = writeJsonIfChangedSync( outPath, value );
	return { outPath, changed };
}
