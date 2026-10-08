/*
===========================================================================

refresh_asset_family.mjs - publish one focused asset family into the packs

	node scripts/refresh_asset_family.mjs <family> [--flags]

Runs one row of build/families/looseFamilies.mjs under the generated-assets
lock: the row produces its public files, and the one pack owner
(build/shared/looseFamilyPublication.mjs) packs exactly those, leaving
every unrelated member alone. `pnpm task list --kind assets` lists the
families as assets:refresh:<family> and assets:publish:<family>.

===========================================================================
*/
// First: it sizes libuv's thread pool before anything starts it.
import "./build/shared/buildParallelism.mjs";
import { LOOSE_FAMILIES } from "./build/families/looseFamilies.mjs";
import { readFile } from "node:fs/promises";
import { publishLooseFamily } from "./build/shared/looseFamilyPublication.mjs";
import { PACK_INDEX_PATH } from "./build/shared/packGroupRefresh.mjs";
import { withGeneratedAssetsLock } from "./rebuildLock.mjs";

const [name, ...rest] = process.argv.slice( 2 );
const family = LOOSE_FAMILIES[name];
if ( !family ) {
	console.error( `Unknown asset family "${name ?? ""}". Known: ${Object.keys( LOOSE_FAMILIES ).join( ", " )}` );
	process.exit( 2 );
}

await withGeneratedAssetsLock( `Asset family ${name}`, async () => {
	const output = await family.produce( new Set( rest ) );
	// A focused republish repacks the representations the index already holds.
	const files = family.packFiles ?
		family.packFiles( output, JSON.parse( await readFile( PACK_INDEX_PATH, "utf8" ) ) ) :
		output.files;
	const updates = await publishLooseFamily( {
		name: family.packFolder,
		owner: name,
		files,
		defaultGroup: output.defaultGroup ?? family.defaultGroup
	} );
	const built = updates.reduce( ( count, update ) => count + update.builtPackCount, 0 );
	console.log(
		`Published ${files.length} ${family.label}${output.note ? ` ${output.note}` : ""}; ` +
			`${built} pack(s) rebuilt.`
	);
} );
