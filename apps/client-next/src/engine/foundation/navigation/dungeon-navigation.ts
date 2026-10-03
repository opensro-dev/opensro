/*
===========================================================================

dungeon-navigation.ts - a dungeon's object navmesh product

Reads the dungeon manifest's raw navigation resource for one sector:
placed object meshes, their links and the dungeon block topology.

===========================================================================
*/

import { dungeonLinks } from "./topology";
import { objectNavigation } from "@/engine/foundation/navigation/object-navigation";
import type {
	NavigationProduct,
	NavPlacement,
	NavMesh,
	DungeonManifest,
	ObjectNavWire,
	NavBsr
} from "@/engine/contracts/navigation";
import { base64Bytes } from "@/engine/foundation/assets/base64";
/*
================
dungeonNavigation
================
*/
// DOF block grammar: native dungeon resident loading; server dungeonspawn.go.
export function dungeonNavigation( manifest: DungeonManifest, regionId: number ): NavigationProduct {
	if ( manifest.format !== "sro-dungeon-resources" || manifest.version !== 3 ) {
		throw new Error( "Unsupported dungeon manifest" );
	}
	const normalize = ( s: string ) => s.trim().replaceAll( "\\", "/" ).replace( /^\/+/, "" ).toLowerCase();
	const entry = manifest.entries?.find( e => e.sectorId === regionId ),
		resource = manifest.resources?.find( r =>
			normalize( r.normalizedName ) === normalize( entry?.normalizedName ?? "" )
		);
	if ( !resource || resource.byteLength > 64 * 1024 * 1024 ) throw new Error( "Missing dungeon navigation" );
	const raw = base64Bytes( resource.rawBase64 );
	if ( raw.length !== resource.byteLength ) throw new Error( "Dungeon byte length" );
	const v = new DataView( raw.buffer );
	let o = 0;
	/*
	================
	take
	================
	*/
	function take( n: number ) {
		if ( !Number.isSafeInteger( n ) || n < 0 || o + n > raw.length ) throw new Error( "Truncated dungeon" );
		const at = o;
		o += n;
		return at;
	}
	const u8 = () => v.getUint8( take( 1 ) ),
		u32 = () => v.getUint32( take( 4 ), true ),
		count = ( max: number ) => {
			const n = u32();
			if ( n > max ) throw new Error( "Dungeon count budget" );
			return n;
		},
		f32 = () => {
			const n = v.getFloat32( take( 4 ), true );
			if ( !Number.isFinite( n ) ) throw new Error( "Dungeon coordinate" );
			return n;
		},
		str = () => {
			const n = count( 65536 ), at = take( n );
			return new TextDecoder().decode( raw.subarray( at, at + n ) );
		};
	if ( new TextDecoder().decode( raw.subarray( take( 12 ), 12 ) ) !== "JMXVDOF 0101" ) {
		throw new Error( "Dungeon signature" );
	}
	const offsets = Array.from( { length: 8 }, () => u32() );
	u32();
	str();
	take( 10 );
	/*
	================
	seek
	================
	*/
	function seek( n: number ) {
		if ( n < o || n > raw.length ) throw new Error( "Dungeon offset" );
		o = n;
	}
	seek( offsets[7]! );
	take( 48 );
	seek( offsets[0]! );
	const meshIndex = new Map<string, ObjectNavWire>(
		manifest.navResources.meshes.map( r => [ normalize( r.sourcePath ), r ] )
	);
	const bsr = new Map<string, NavBsr>( manifest.navResources.bsr.map( r => [ normalize( r.sourcePath ), r ] ) );
	const decoded = new Map<string, NavMesh[]>(), objects: NavPlacement[] = [], neighbors: number[][] = [];
	const nb = count( 4096 );
	for ( let i = 0; i < nb; i++ ) {
		const path = normalize( str() );
		str();
		take( 4 );
		const x = f32(), y = f32(), z = f32(), yaw = f32();
		take( 32 );
		take( 16 );
		if ( u8() ) take( 16 );
		if ( u8() ) take( 28 );
		str();
		u32();
		const floor = u32();
		const adjacent = Array.from( { length: count( 4096 ) }, () => u32() );
		if ( adjacent.some( n => n >= nb ) ) throw new Error( "Invalid dungeon adjacency" );
		neighbors.push( adjacent );
		const visible = Array.from( { length: count( 4096 ) }, () => u32() );
		if ( visible.some( n => n >= nb ) ) throw new Error( "Invalid dungeon visibility" );
		const obstacles: import("@/engine/contracts/navigation").NavObstacle[] = [];
		const no = count( 1048576 );
		u32();
		for ( let j = 0; j < no; j++ ) {
			str();
			str();
			const x = f32(), y = f32(), z = f32();
			take( 24 );
			const flags = u32();
			u32();
			const radiusSquared = f32();
			if ( flags & 2 ) {
				if ( radiusSquared < 0 ) throw new Error( "Invalid dungeon collision radius" );
				obstacles.push( { x, y, z, radiusSquared } );
			}
			if ( flags & 4 ) take( 4 );
		}
		const nl = count( 1048576 );
		for ( let j = 0; j < nl; j++ ) {
			str();
			take( 60 );
		}
		const r = bsr.get( path );
		if ( !r ) throw new Error( "Missing dungeon resident " + path );
		for ( const name of r.renderMeshSection?.paths ?? r.meshPaths ?? [] ) {
			const key = normalize( name );
			let meshes = decoded.get( key );
			if ( !meshes ) {
				const row = meshIndex.get( key ); // The v3 builder omits BMS rows without native payloads; a missing row is decorative.
				if ( !row ) meshes = [];
				else meshes = objectNavigation( row );
				decoded.set( key, meshes );
			}
			for ( const mesh of meshes ) objects.push( { x, y, z, yaw, mesh, block: i, floor, obstacles } );
		}
	}
	if ( o !== offsets[2] ) throw new Error( "Dungeon block section length" );
	return { regionId, objects: dungeonLinks( objects, neighbors ), complete: true };
}
