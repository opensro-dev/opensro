import { CLIENT_PUBLIC_ROOT } from "../lib/generatedRoot.mjs";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { formatOptimizationSummary, optimizeJsonAssets } from "./jsonAssetCompression.mjs";
import { withGeneratedAssetsLock } from "../rebuildLock.mjs";

const scriptDir = path.dirname( fileURLToPath( import.meta.url ) );
const rebuildRoot = path.resolve( scriptDir, "..", ".." );
const publicRoot = CLIENT_PUBLIC_ROOT;
const layoutsRoot = path.join( publicRoot, "assets", "cif", "layouts" );

await withGeneratedAssetsLock( "CIF layout JSON optimization", async () => {
	const summary = await optimizeJsonAssets( {
		root: layoutsRoot,
		publicRoot,
		force: process.argv.includes( "--force" )
	} );
	console.log( formatOptimizationSummary( summary ) );
} );
