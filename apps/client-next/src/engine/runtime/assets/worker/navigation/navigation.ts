/*
===========================================================================

navigation.ts - navigation asset worker decoder

Decodes published outdoor navigation bundles (including 3x3 neighborhood
coverage across city and field sectors) and dungeon navigation manifests
into admitted NavigationProduct instances.

===========================================================================
*/

import type {
	DungeonManifest,
	NavBsr,
	NavBundle,
	NavigationProduct,
	NavMesh,
	NavPlacement,
	NavResources,
	ObjectNavWire
} from "@/engine/contracts/navigation";
import { dungeonNavigation } from "@/engine/foundation/navigation/dungeon-navigation";
import { objectNavigation } from "@/engine/foundation/navigation/object-navigation";
import { terrainObjectCells } from "@/engine/foundation/navigation/terrain-object-cells";
import { objectLinks, validateLinks } from "@/engine/foundation/navigation/topology";
import { featuresAt } from "@/engine/foundation/world/plane";
import { REGION_SIZE, sectorX, sectorY } from "@/engine/foundation/world/world-space";

/*
================
createNavigationResources
================
*/
export function createNavigationResources() {
	return {
		async resolve(
			bytes: Uint8Array,
			regionId: number,
			read: ( path: string ) => Promise<Uint8Array>
		): Promise<NavigationProduct> {
			const parse = <T>( bytes: Uint8Array ): T =>
				JSON.parse( new TextDecoder( "utf-8", { fatal: true } ).decode( bytes ) );
			if ( !featuresAt( regionId ).terrainGrid ) {
				return dungeonNavigation( parse<DungeonManifest>( bytes ), regionId );
			}
			const bundle = parse<NavBundle>( bytes );
			if ( !bundle.navmesh?.regions || !bundle.source ) {
				throw new Error( "Missing native navigation regions" );
			}
			// Published city bundles may span more than the resident 3x3 neighborhood.
			// Rebase to the requested sector before producing collision placements.
			const sources = [ bundle ];
			const seed = regionId,
				sx = sectorX( regionId ),
				sz = sectorY( regionId );
			const rows = bundle.navmesh.regions
				.map( r => ({
					...r,
					dx: r.dx + bundle.source.sectorX - sx,
					dz: r.dz + bundle.source.sectorY - sz
				}) )
				.filter( r => Math.abs( r.dx ) <= 1 && Math.abs( r.dz ) <= 1 );
			if ( !rows.some( r => r.dx === 0 && r.dz === 0 ) ) {
				throw Error( "Navigation bundle does not cover requested region " + regionId );
			}
			const catalog = parse<{ regionsById: Record<string, { area: string; bundlePublicPath: string; }[]>; }>(
				await read( "/assets/world/world-region-catalog.json" )
			);
			for ( let dz = -1; dz <= 1; dz++ ) {
				for ( let dx = -1; dx <= 1; dx++ ) {
					const x = sx + dx,
						z = sz + dz;
					if ( x < 0 || x > 255 || z < 0 || z > 127 || rows.some( r => r.dx === dx && r.dz === dz ) ) {
						continue;
					}
					const entry = catalog.regionsById[`0x${(x | (z << 8)).toString( 16 ).padStart( 4, "0" )}`]?.find(
						r => r.area === "outdoor"
					);
					if ( entry ) {
						const neighbor = parse<NavBundle>( await read( entry.bundlePublicPath ) );
						const row = neighbor.navmesh.regions.find(
							r => neighbor.source.sectorX + r.dx === x && neighbor.source.sectorY + r.dz === z
						);
						if ( !row ) throw Error( "Neighbor navigation coverage missing" );
						rows.push( { ...row, dx, dz } );
						sources.push( neighbor );
					}
				}
			}
			bundle.navmesh = { ...bundle.navmesh, regions: rows };
			const placements = bundle.navmesh.regions.flatMap( r => {
				const cells = terrainObjectCells( r );
				return (r.objects ?? []).map( ( p, ordinal ) => ({
					...p,
					terrainCells: cells?.[ordinal],
					ordinal,
					region: r.dx + "," + r.dz,
					x: p.x + r.dx * REGION_SIZE,
					z: p.z + r.dz * REGION_SIZE
				}) );
			} );
			// A city index is scoped to its bundle. Neighbor sectors own their resource
			// indices too; do not resolve their object IDs through the city-only index.
			const ids = new Set( placements.map( p => p.assetId ) ),
				bsr = new Map<number, NavBsr>(),
				indices: NavResources[] = [],
				loaded = new Set<string>();
			for ( const source of sources ) {
				const path = source.objects.resourceIndexPublicPath;
				if ( !source.objects.resources && !path ) {
					if ( source.navmesh.regions.every( r => !r.objects?.length ) ) continue;
					throw Error( "Missing navigation resource index" );
				}
				if ( !source.objects.resources && loaded.has( path ) ) continue;
				const index = source.objects.resources ?? parse<NavResources>( await read( path ) );
				loaded.add( path );
				indices.push( index );
				for ( const row of index.bsr ) {
					if ( ids.has( row.objectId ) ) bsr.set( row.objectId, row );
				}
			}
			// 441E40 loads an object's navigation from the one mesh its BSR header
			// names (offset slot 7, primaryMeshPath); its render meshes carry none.
			const navMeshPaths = ( r: NavBsr ) => r.primaryMeshPath ? [ r.primaryMeshPath ] : [];
			const wanted = new Set<string>(
				[ ...bsr.values() ].flatMap( r => navMeshPaths( r ).map( ( s: string ) => s.toLowerCase() ) )
			);
			const meshes = new Map<string, NavMesh[]>();
			for ( const index of indices ) {
				if ( index.meshes ) {
					for ( const mesh of index.meshes ) {
						if (
							wanted.has( mesh.sourcePath.toLowerCase() ) &&
							!meshes.has( mesh.sourcePath.toLowerCase() )
						) {
							meshes.set( mesh.sourcePath.toLowerCase(), objectNavigation( mesh ) );
						}
					}
				} else {
					for ( const file of index.meshFiles ?? [] ) {
						if (
							wanted.has( file.sourcePath.toLowerCase() ) &&
							!meshes.has( file.sourcePath.toLowerCase() )
						) {
							const value = parse<{ mesh: ObjectNavWire; }>( await read( file.publicPath ) );
							meshes.set( file.sourcePath.toLowerCase(), objectNavigation( value.mesh ) );
						}
					}
				}
			}
			const objects: NavPlacement[] = [],
				sourcePlacements: number[][] = [];
			for ( const p of placements ) {
				sourcePlacements.push( [] );
				const r = bsr.get( p.assetId );
				if ( !r ) throw new Error( "Missing navigation BSR " + p.assetId );
				for ( const path of navMeshPaths( r ) ) {
					const rows = meshes.get( path.toLowerCase() );
					if ( !rows ) throw new Error( "Missing navigation mesh " + path );
					for ( const mesh of rows ) {
						sourcePlacements[sourcePlacements.length - 1]!.push( objects.length );
						objects.push( { x: p.x, y: p.y, z: p.z, yaw: p.yaw, mesh, terrainCells: p.terrainCells } );
					}
				}
			}
			const identity = new Map( placements.map( ( p, i ) => [ p.region + ":" + p.ordinal, i ] ) );
			for ( let i = 0; i < placements.length; i++ ) {
				const p = placements[i]!,
					links = objectLinks( p.linkEdgeCount, p.linkEdges );
				for ( const at of sourcePlacements[i]! ) {
					const linked = [];
					for ( const link of links ) {
						if ( link.target === 65535 ) continue;
						const target = identity.get( p.region + ":" + link.target );
						if ( target === undefined ) throw new Error( "Missing object link placement" );
						const targets = sourcePlacements[target]!;
						if ( targets.length !== 1 || sourcePlacements[i]!.length !== 1 ) {
							throw new Error( "Ambiguous object link mesh" );
						}
						linked.push( { ...link, target: targets[0]! } );
					}
					objects[at] = { ...objects[at]!, links: linked };
				}
			}
			validateLinks( objects );
			return {
				regionId: seed,
				navmesh: {
					regionSize: REGION_SIZE,
					tileSize: 20,
					tilesPerAxis: 96,
					regions: bundle.navmesh.regions.map( r => ({
						dx: r.dx,
						dz: r.dz,
						blockedTiles: r.blockedTiles,
						tileCellIds: r.tileCellIds,
						heightMap: r.heightMap,
						planeType: r.planeType,
						planeHeight: r.planeHeight,
						cells: { count: r.cells.count },
						objects: []
					}) )
				},
				objects,
				complete: true
			};
		}
	};
}
