import { readFile } from "node:fs/promises";
import path from "node:path";
import { refreshPrecompressedSidecars } from "../generatedManifestSidecars.mjs";
import { writeJson } from "./io.mjs";
import { normalizeRegionId, publicRoot } from "./paths.mjs";
import { compareText } from "../shared/textOrder.mjs";

export const WORLD_REGION_CATALOG_PUBLIC_PATH = "/assets/world/world-region-catalog.json";
export const OUTDOOR_WORLD_REGION_CATALOG_PUBLIC_PATH = "/assets/world/outdoor/world-region-catalog.json";

export async function buildWorldRegionCatalog( resourceGroups ) {
	const catalog = buildWorldRegionCatalogDescriptor( resourceGroups );
	return writeWorldRegionCatalogDescriptor( catalog );
}

export async function writeWorldRegionCatalogDescriptor( catalog, options = {} ) {
	const publicPath = options.publicPath ?? WORLD_REGION_CATALOG_PUBLIC_PATH;
	const outputPath = path.join( publicRoot, publicPath.replace( /^\/+/, "" ) );
	const normalizedCatalog = normalizeCatalogDescriptor( catalog );
	await writeJson( outputPath, normalizedCatalog );
	await refreshPrecompressedSidecars( [ outputPath ], { onlyWhenStale: true } );

	return {
		publicPath,
		outputPath,
		regionCount: Object.keys( normalizedCatalog.regionsById ).length,
		entryCount: Object.values( normalizedCatalog.regionsById ).reduce(
			( count, entries ) => count + entries.length,
			0
		)
	};
}

export function buildWorldRegionCatalogDescriptor( resourceGroups ) {
	const regionsById = {};

	for ( const group of resourceGroups ) {
		addRegionResourceGroup( regionsById, group );
	}

	return {
		format: "sro-world-region-catalog",
		version: 1,
		generatedAt: new Date( 0 ).toISOString(),
		regionsById: Object.fromEntries(
			Object.entries( regionsById ).sort( ( [left], [right] ) => compareText( left, right ) )
		)
	};
}

/**
 * Replace one or more independently generated source families without
 * discarding fixed CPS/title entries already present in the global catalog.
 */
export function overlayWorldRegionCatalogDescriptor( baseCatalog, resourceGroups, options = {} ) {
	const replaceSources = new Set(
		(options.replaceSources ?? resourceGroups.map( ( group ) => group?.sourceName ).filter( Boolean )).map( String )
	);
	const regionsById = {};

	for ( const [rawId, entries] of Object.entries( baseCatalog?.regionsById ?? {} ) ) {
		const id = normalizeRegionId( rawId );
		const retained = entries.filter( ( entry ) => !replaceSources.has( String( entry.source ?? "" ) ) );
		if ( retained.length > 0 ) {
			regionsById[id] = retained.map( normalizeCatalogEntry ).sort( compareCatalogEntries );
		}
	}

	for ( const group of resourceGroups ) {
		addRegionResourceGroup( regionsById, group );
	}

	return normalizeCatalogDescriptor( {
		format: "sro-world-region-catalog",
		version: 1,
		generatedAt: new Date( 0 ).toISOString(),
		regionsById
	} );
}

export async function overlayWorldRegionCatalog( resourceGroups, options = {} ) {
	const outputPath = path.join( publicRoot, WORLD_REGION_CATALOG_PUBLIC_PATH.replace( /^\/+/, "" ) );
	let baseCatalog;
	try {
		baseCatalog = JSON.parse( await readFile( outputPath, "utf8" ) );
	} catch ( error ) {
		if ( error?.code !== "ENOENT" ) {
			throw error;
		}
		baseCatalog = buildWorldRegionCatalogDescriptor( [] );
	}

	const catalog = overlayWorldRegionCatalogDescriptor( baseCatalog, resourceGroups, options );
	const primary = await writeWorldRegionCatalogDescriptor( catalog );
	const mirrors = [];
	for ( const publicPath of options.mirrorPublicPaths ?? [] ) {
		if ( publicPath !== WORLD_REGION_CATALOG_PUBLIC_PATH ) {
			mirrors.push( await writeWorldRegionCatalogDescriptor( catalog, { publicPath } ) );
		}
	}
	return { ...primary, mirrors };
}

function addRegionResourceGroup( regionsById, resourceGroup ) {
	const descriptor = resourceGroup?.regionIndexDescriptor;
	if ( !descriptor ) {
		return;
	}

	for ( const region of descriptor.regions ) {
		const id = normalizeRegionId( region.id );
		const seedRegionId = normalizeRegionId( region.seedRegionId );
		const entry = {
			id,
			area: descriptor.area,
			seedRegionId,
			seedSector: seedRegionId === id ?
				{ sectorX: region.sectorX, sectorY: region.sectorY } :
				descriptor.seedSector,
			sectorX: region.sectorX,
			sectorY: region.sectorY,
			worldRegionsPublicPath: resourceGroup.worldRegionsPublicPath,
			bundlePublicPath: region.bundlePublicPath,
			source: resourceGroup.sourceName
		};

		const entries = regionsById[id] ?? [];
		entries.push( entry );
		regionsById[id] = entries.sort( compareCatalogEntries );
	}
}

function compareCatalogEntries( left, right ) {
	const leftExact = left.id === left.seedRegionId ? 0 : 1;
	const rightExact = right.id === right.seedRegionId ? 0 : 1;
	if ( leftExact !== rightExact ) {
		return leftExact - rightExact;
	}
	return compareText(
		`${left.area}:${left.seedRegionId}:${left.bundlePublicPath}`,
		`${right.area}:${right.seedRegionId}:${right.bundlePublicPath}`
	);
}

function normalizeCatalogDescriptor( catalog ) {
	if (
		catalog?.format !== "sro-world-region-catalog" ||
		catalog.version !== 1 ||
		!catalog.regionsById ||
		typeof catalog.regionsById !== "object" ||
		Array.isArray( catalog.regionsById )
	) {
		throw new Error( "World region catalog must satisfy the current version 1 contract" );
	}
	return {
		format: "sro-world-region-catalog",
		version: 1,
		generatedAt: catalog?.generatedAt ?? new Date( 0 ).toISOString(),
		regionsById: Object.fromEntries(
			Object.entries( catalog?.regionsById ?? {} )
				.map( ( [id, entries] ) => [
					normalizeRegionId( id ),
					entries.map( normalizeCatalogEntry ).sort( compareCatalogEntries )
				] )
				.sort( ( [left], [right] ) => compareText( left, right ) )
		)
	};
}

function normalizeCatalogEntry( entry ) {
	return {
		...entry,
		id: normalizeRegionId( entry.id ),
		seedRegionId: normalizeRegionId( entry.seedRegionId )
	};
}
