/*
===========================================================================

buildUniqueNestsAsset.mjs - the world map's unique nests (port-only)

Port-only, not native: the v1.150 world map draws no unique nest. This step
publishes the nest table the server's own population template names
(sro-evidence unique-nests: monster.Template.UniqueNests), so the M window
can show every candidate spawn of each unique as one sign.

The rule lives in Go only (monster.NestIsUnique, the same rule the unique
tracker reads); the build checks the table's shape and writes it, and
decides nothing of its own, so a server-side nest change cannot drift
from the map (the creatable-monster roster's posture, npcModelRoster.mjs).

Output: /assets/data/uniqueNests.json, { format, version, nests }, each nest
{ refObjId, regionId, x, z } in npcpos order.

===========================================================================
*/
import { spawnSync } from "node:child_process";
import path from "node:path";
import { sharedCacheEnvironment } from "../../lib/goEnvironment.mjs";
import { refreshPrecompressedSidecars } from "../generatedManifestSidecars.mjs";
import { exportDataAsset } from "../shared/dataAssetExport.mjs";
import { isMainScript } from "../shared/fsUtils.mjs";
import { publicRoot, rebuildRoot, retailTextdataRoot } from "../world/paths.mjs";

const FORMAT = "sro-unique-nests";
const VERSION = 1;
export const UNIQUE_NESTS_FILE = "uniqueNests.json";
const EXPORT_MAX_BYTES = 4 * 1024 * 1024;
const REGION_MAX = 0xffff;

/*
================
checkUniqueNestTable

The server's table, refused whole when its shape is not the client's
contract (foundation/ui/unique-nests.ts) or it names no nest: shipped
textdata always holds uniques, so an empty table is a missing input.
================
*/
export function checkUniqueNestTable( table ) {
	if ( table?.format !== FORMAT || table.version !== VERSION || !Array.isArray( table.nests ) ) {
		throw Error( `[unique-nests] the server export is not a ${FORMAT} v${VERSION} table` );
	}
	if ( table.nests.length === 0 ) throw Error( "[unique-nests] the server export names no nest" );
	for ( const nest of table.nests ) {
		const valid = Number.isSafeInteger( nest?.refObjId ) && nest.refObjId > 0 &&
			Number.isInteger( nest.regionId ) && nest.regionId >= 0 && nest.regionId <= REGION_MAX &&
			Number.isFinite( nest.x ) && Number.isFinite( nest.z );
		if ( !valid ) {
			throw Error( `[unique-nests] the server export holds a malformed nest: ${JSON.stringify( nest )}` );
		}
	}
	return { format: FORMAT, version: VERSION, nests: table.nests };
}

/*
================
exportServerUniqueNests

Asks the server's classifier for the table over the build's textdata.
================
*/
export function exportServerUniqueNests( textdataDir = retailTextdataRoot, env = process.env ) {
	const serverRoot = path.resolve( env.SRO_SERVER_SOURCE_ROOT ?? path.join( rebuildRoot, "apps", "server" ) );
	const result = spawnSync(
		"go",
		[ "run", "./cmd/tools/sro-evidence", "unique-nests", "-textdata-dir", textdataDir ],
		{
			cwd: serverRoot,
			// Compiled once for every checkout (goEnvironment.mjs).
			env: sharedCacheEnvironment( env ),
			encoding: "utf8",
			maxBuffer: EXPORT_MAX_BYTES
		}
	);
	if ( result.error || result.status !== 0 ) {
		throw Error(
			`[unique-nests] export failed (go run ./cmd/tools/sro-evidence unique-nests in ${serverRoot}): ` +
				`${result.error ?? ""} ${result.stderr ?? ""}`.trim()
		);
	}
	return JSON.parse( result.stdout );
}

/*
================
buildUniqueNestsAsset
================
*/
export async function buildUniqueNestsAsset( options = {} ) {
	const table = checkUniqueNestTable( options.table ?? exportServerUniqueNests() );
	const { outPath } = exportDataAsset( {
		publicRoot: options.publicRoot ?? publicRoot,
		outputFileName: UNIQUE_NESTS_FILE,
		value: table
	} );
	await refreshPrecompressedSidecars( [ outPath ], { onlyWhenStale: true } );
	console.log( `[unique-nests] wrote ${table.nests.length} nest(s)` );
	return { outPath, nests: table.nests.length };
}

if ( isMainScript( import.meta.url ) ) await buildUniqueNestsAsset();
