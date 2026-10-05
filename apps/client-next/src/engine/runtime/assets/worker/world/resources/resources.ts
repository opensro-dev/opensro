/*
===========================================================================

resources.ts - resolve a published region bundle and what it references

An outdoor scene is the centre region plus its eight neighbours, the mesh
files their placements name, and the area's shared sky and water. Region
crossings revisit the same bundles, so parsed documents are kept: bundles
in a small LRU, catalogues and indexes for the worker's lifetime. Parsed
documents are never mutated; callers build new objects around them.

===========================================================================
*/
import type { Bundle } from "../internal/resource-contract";

type Region = Bundle & {
	sharedRenderResourcesPublicPath?: string;
	objects: Bundle["objects"] & { resourceIndexPublicPath?: string; };
};
type ResourceIndex = Omit<Bundle["objects"]["resources"], "meshes"> & {
	meshFiles: { sourcePath: string; publicPath: string; }[];
};
type Catalog = { regionsById: Record<string, { area: string; bundlePublicPath: string; }[]>; };
type Read = ( path: string ) => Promise<Uint8Array>;

// Parsed region bundles kept: a 3x3 scene and the row or column a crossing adds.
const PARSED_REGION_LIMIT = 12;
// Parsed mesh files kept: a town scene places a few hundred distinct meshes.
const PARSED_MESH_LIMIT = 1024;
// Parallel mesh file reads while resolving one scene.
const MESH_READS = 3;

/*
================
parse
================
*/
function parse<T>( bytes: Uint8Array ): T {
	return JSON.parse( new TextDecoder( "utf-8", { fatal: true } ).decode( bytes ) ) as T;
}

/*
================
createWorldResources
================
*/
export function createWorldResources() {
	const regions = new Map<string, Region>();
	const meshes = new Map<string, Bundle["objects"]["resources"]["meshes"][number]>();
	const documents = new Map<string, unknown>();

	/*
	================
	remember

	Inserts into a most-recently-used-last map and drops the oldest entry
	past limit.
	================
	*/
	function remember<T>( map: Map<string, T>, key: string, value: T, limit: number ): T {
		map.delete( key );
		map.set( key, value );
		if ( map.size > limit ) map.delete( map.keys().next().value! );
		return value;
	}

	/*
	================
	regionAt

	A parsed region bundle by path, most recently used last.
	================
	*/
	async function regionAt( path: string, read: Read ): Promise<Region> {
		return remember(
			regions,
			path,
			regions.get( path ) ?? parse<Region>( await read( path ) ),
			PARSED_REGION_LIMIT
		);
	}

	/*
	================
	document

	A catalogue, resource index or shared resource file: a handful of
	documents that do not change while the worker lives.
	================
	*/
	async function document<T>( path: string, read: Read ): Promise<T> {
		if ( !documents.has( path ) ) documents.set( path, parse<T>( await read( path ) ) );
		return documents.get( path ) as T;
	}

	/*
	================
	centre

	The requested bundle. A self-contained bundle (frontend stages,
	dungeons) carries its resources; a split outdoor bundle names them.
	================
	*/
	async function centre( path: string, bytes: Uint8Array, read: Read ): Promise<Region> {
		if ( path && regions.has( path ) ) return regionAt( path, read );
		const region = parse<Region>( bytes );
		if ( region.objects?.resources || !path ) return region;
		return remember( regions, path, region, PARSED_REGION_LIMIT );
	}

	/*
	================
	meshAt
	================
	*/
	async function meshAt( path: string, read: Read ) {
		const mesh = meshes.get( path ) ??
			parse<{ mesh: Bundle["objects"]["resources"]["meshes"][number]; }>( await read( path ) ).mesh;
		return remember( meshes, path, mesh, PARSED_MESH_LIMIT );
	}

	return {
		/*
		================
		resolve

		The full scene bundle: centre, eight neighbours, the placed meshes
		and the shared sky and water.
		================
		*/
		async resolve( bytes: Uint8Array, read: Read, path = "" ): Promise<Bundle> {
			const center = await centre( path, bytes, read );
			if ( center.objects?.resources ) return center;
			if ( !center.objects?.resourceIndexPublicPath || !center.sharedRenderResourcesPublicPath ) {
				throw new Error( "World resource references are missing" );
			}
			const catalog = await document<Catalog>( "/assets/world/world-region-catalog.json", read );
			const around: Region[] = [ center ];
			for ( let dz = -1; dz <= 1; dz++ ) {
				for ( let dx = -1; dx <= 1; dx++ ) {
					if ( !dx && !dz ) continue;
					const x = center.source.sectorX + dx, z = center.source.sectorY + dz;
					if ( x < 0 || x > 255 || z < 0 || z > 255 ) continue;
					const neighbour = catalog.regionsById[`0x${(x | (z << 8)).toString( 16 ).padStart( 4, "0" )}`]
						?.find( row => row.area === "outdoor" )?.bundlePublicPath;
					if ( neighbour ) around.push( await regionAt( neighbour, read ) );
				}
			}
			const index = await document<ResourceIndex>( center.objects.resourceIndexPublicPath, read );
			const shared = await document<Pick<Bundle, "sky" | "water">>(
				center.sharedRenderResourcesPublicPath,
				read
			);
			const placements = around.flatMap( region => region.objects.placements ),
				ids = new Set( placements.map( p => p.objectId ) );
			const bsr = index.bsr.filter( row => ids.has( row.objectId ) );
			const materialPaths = new Set( bsr.flatMap( row => row.materialPaths.map( p => p.toLowerCase() ) ) );
			const paths = new Set(
				bsr.flatMap( row => (row.renderMeshSection?.paths ?? row.meshPaths).map( p => p.toLowerCase() ) )
			);
			const files = index.meshFiles.filter( row => paths.has( row.sourcePath.toLowerCase() ) );
			const placed: Bundle["objects"]["resources"]["meshes"] = [];
			let next = 0;
			await Promise.all( Array.from( { length: Math.min( MESH_READS, files.length ) }, async () => {
				while ( next < files.length ) {
					const file = files[next++]!;
					placed.push( await meshAt( file.publicPath, read ) );
				}
			} ) );
			placed.sort( ( a, b ) => a.sourcePath.localeCompare( b.sourcePath ) );
			const tiles = new Map(
				around.flatMap( region => region.terrainTextures.tileCatalog.referencedTiles ).map( tile => [
					tile.textureId,
					tile
				] )
			);
			return {
				...center,
				...shared,
				terrain: {
					...center.terrain,
					sectors: around.map( region => ({
						...region.source,
						blocks: region.terrain.blocks,
						lightmapPublicPath: region.terrainTextures.lightmapPublicPath
					}) )
				},
				terrainTextures: { ...center.terrainTextures, tileCatalog: { referencedTiles: [ ...tiles.values() ] } },
				objects: {
					...center.objects,
					placements,
					resources: {
						bsr,
						meshes: placed,
						materialSets: index.materialSets.filter( row =>
							materialPaths.has( row.sourcePath.toLowerCase() )
						)
					}
				}
			};
		},
		/*
		================
		resolveTerrain

		One region's own terrain: its bundle and the area's shared water and
		sky. No neighbours, placements or meshes.
		================
		*/
		async resolveTerrain( bytes: Uint8Array, read: Read, path = "" ): Promise<Bundle> {
			const region = await centre( path, bytes, read );
			if ( !region.sharedRenderResourcesPublicPath ) throw new Error( "World resource references are missing" );
			const shared = await document<Pick<Bundle, "sky" | "water">>(
				region.sharedRenderResourcesPublicPath,
				read
			);
			return {
				...region,
				...shared,
				terrain: {
					...region.terrain,
					sectors: [ {
						...region.source,
						blocks: region.terrain.blocks,
						lightmapPublicPath: region.terrainTextures.lightmapPublicPath
					} ]
				},
				objects: {
					...region.objects,
					placements: [],
					resources: { bsr: [], meshes: [], materialSets: [] }
				}
			};
		}
	};
}
