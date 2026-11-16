import path from "node:path";
import { refreshPrecompressedSidecars } from "../generatedManifestSidecars.mjs";
import { REGION_SIZE } from "./constants.mjs";
import { writeJson } from "./io.mjs";
import { publicRoot, regionIdFromSectorCoordinates } from "./paths.mjs";
import { compareText } from "../shared/textOrder.mjs";

// Phase 4a build artifact: the per-seed region index the live WorldRegionManager (CGWorld
// analog) streams against. `regions` enumerates EVERY region baked into the merged seed bundle
// (the full camera-path coverage, derived from bundle.terrain.sectors - 28 for the title, 9 for
// char-select). Until the build emits true one-file-per-region bundles, every baked entry points
// at the existing merged bundle so the runtime never advertises non-existent region JSON files.

export function buildWorldRegionIndexDescriptor( bundle, seedBundlePublicPath ) {
	const area = bundle.source.area;
	const seedRegionId = regionIdFromSectorCoordinates( bundle.source.sectorX, bundle.source.sectorY );
	const coverageSectors = bundle.terrain.sectors ?? [
		{ sectorX: bundle.source.sectorX, sectorY: bundle.source.sectorY }
	];

	const regions = coverageSectors
		.map( ( sector ) => {
			const id = regionIdFromSectorCoordinates( sector.sectorX, sector.sectorY );
			return {
				id,
				sectorX: sector.sectorX,
				sectorY: sector.sectorY,
				seedRegionId,
				bundlePublicPath: seedBundlePublicPath
			};
		} )
		.sort( ( left, right ) => compareText( left.id, right.id ) );

	return {
		format: "sro-world-region-index",
		version: 2,
		area,
		regionSize: REGION_SIZE,
		seedRegionId,
		seedSector: { sectorX: bundle.source.sectorX, sectorY: bundle.source.sectorY },
		bundleLayout: "merged-coverage",
		deliveryMode: "prebuilt",
		regions
	};
}

export async function buildWorldRegionIndex( bundle, seedBundlePublicPath ) {
	const descriptor = buildWorldRegionIndexDescriptor( bundle, seedBundlePublicPath );
	// One index per SEED region, not per area: CPSTitle (0x694e) and CPSCharacterSelect (0x6951)
	// are distinct stages of the same "constantinople" area, so a single per-area file would
	// collide. Name it world-regions-<seedhex>.json to keep both stages' halos.
	const seedHexDigits = descriptor.seedRegionId.replace( /^0x/i, "" );
	const fileName = `world-regions-${seedHexDigits}.json`;
	const targetPath = path.join( publicRoot, "assets", "world", descriptor.area, fileName );
	await writeJson( targetPath, descriptor );
	await refreshPrecompressedSidecars( [ targetPath ], { onlyWhenStale: true } );
	return {
		publicPath: `/assets/world/${descriptor.area}/${fileName}`,
		outputPath: targetPath,
		seedRegionId: descriptor.seedRegionId,
		regionCount: descriptor.regions.length,
		descriptor
	};
}
