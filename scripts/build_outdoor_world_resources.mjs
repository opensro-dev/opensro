/*
===========================================================================

build_outdoor_world_resources.mjs - every outdoor region as a streamable bundle

`pnpm assets build world-outdoor` (and the first half of `assets build full`):
builds the extracted outdoor world region by region and publishes the
outdoor index once every discovered bundle exists. The ordinary resource
build reuses what this writes.

===========================================================================
*/
// First: it sizes libuv's thread pool before anything starts it.
import "./build/shared/buildParallelism.mjs";
import {
	OUTDOOR_WORLD_INDEX_PUBLIC_PATH,
	buildOutdoorWorldRegionResources
} from "./build/world/buildOutdoorWorldRegionResources.mjs";
import { runConvertImages } from "./build/shared/convertImagesRunner.mjs";
import { beginPublication, commitPublication } from "./build/shared/publicationLedger.mjs";
import { withGeneratedAssetsLock } from "./rebuildLock.mjs";
import { assertClientInputs } from "./build/shared/clientInputs.mjs";

const options = parseArguments( process.argv.slice( 2 ) );

if ( options.help ) {
	console.log( `Build the complete extracted outdoor world as independent streamable regions.

Usage:
  node scripts/build_outdoor_world_resources.mjs [options]

Options:
  --plan                 Discover and report without writing files.
  --force                Rebuild selected region files and shared resources.
  --force-shared         Rebuild only the shared render/object resource stores.
  --jobs=N               Concurrent region builders (default SRO_BUILD_JOBS, else cores - 1).
  --region=HEX[,HEX...]  Incrementally build specific valid region ids.
  --no-catalog           Do not update the global world-region catalog.
  --help                 Show this help.

Without --region, all valid MAPM+MAPT+MAPO2+JMXVNVM sectors are emitted. The
global index is published only when every discovered bundle exists.` );
	process.exit( 0 );
}

/*
================
runBuild

Convert the source images the outdoor writers read, then build the regions.
================
*/
const runBuild = async () => {
	// `pnpm assets compact` deliberately removes rebuild/assets,
	// including the converted DDJ/TGA staging images consumed by the outdoor
	// sky, water, terrain and object-resource writers. Keep this standalone
	// entry point self-sufficient just like build_sro_resources.mjs: a clean
	// compact checkout plus the extracted PK2 inputs must be enough to rebuild
	// the complete shippable outdoor plane.
	if ( !options.planOnly ) {
		const sourceImages = await runConvertImages( [] );
		if ( sourceImages.status !== 0 ) {
			throw new Error( `Source image conversion failed with exit status ${sourceImages.status}.` );
		}
	}
	// A --region run claims only its regions and merges into the full record.
	if ( !options.planOnly ) beginPublication( "outdoor-world", { complete: options.regionIds.length === 0 } );
	const result = await buildOutdoorWorldRegionResources( options );
	if ( !options.planOnly ) await commitPublication();
	if ( result.planOnly ) {
		console.log(
			`Outdoor build plan: ${result.sectorCount} valid sectors, ${result.selectedSectorCount} selected.`
		);
		return;
	}

	console.log(
		`Outdoor regions: ${result.built} built, ${result.reused} reused, ` +
			`${result.sectorCount} globally addressable; shared objects ` +
			`${result.sharedObjectIndex.bsrCount} BSR / ${result.sharedObjectIndex.meshCount} BMS.`
	);
	if ( result.published ) {
		console.log( `Published ${OUTDOOR_WORLD_INDEX_PUBLIC_PATH} and global catalog coverage.` );
	} else {
		console.warn(
			`Routing was not republished because ${result.missingBundlePaths.length} global bundle(s) remain missing.`
		);
	}
};

assertClientInputs( "outdoor world build" );

if ( options.planOnly ) {
	await runBuild();
} else {
	await withGeneratedAssetsLock( "outdoor world resource build", runBuild );
}

/*
================
parseArguments
================
*/
function parseArguments( args ) {
	const parsed = {
		planOnly: false,
		force: false,
		forceShared: false,
		updateCatalog: true,
		jobs: undefined,
		regionIds: [],
		help: false
	};

	for ( const argument of args ) {
		if ( argument === "--plan" ) {
			parsed.planOnly = true;
		} else if ( argument === "--force" ) {
			parsed.force = true;
		} else if ( argument === "--force-shared" ) {
			parsed.forceShared = true;
		} else if ( argument === "--no-catalog" ) {
			parsed.updateCatalog = false;
		} else if ( argument === "--help" || argument === "-h" ) {
			parsed.help = true;
		} else if ( argument.startsWith( "--jobs=" ) ) {
			parsed.jobs = Number.parseInt( argument.slice( "--jobs=".length ), 10 );
		} else if ( argument.startsWith( "--region=" ) ) {
			parsed.regionIds.push(
				...argument
					.slice( "--region=".length )
					.split( "," )
					.map( ( value ) => value.trim() )
					.filter( Boolean )
			);
		} else {
			throw new Error( `Unknown outdoor build option ${argument}` );
		}
	}

	return parsed;
}
