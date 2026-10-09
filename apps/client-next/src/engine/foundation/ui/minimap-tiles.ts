import type { Pose } from "@/engine/contracts/gameplay";
export interface MinimapFloor {
	readonly floorIndex: number;
	readonly floorLabel: string;
	readonly directory: string;
	readonly prefix: string;
	readonly tiles: readonly string[];
}
export function minimapDungeons( value: unknown ): ReadonlyMap<number, readonly MinimapFloor[]> {
	const data = value as {
		format: string;
		version: number;
		dungeons: { sectorId: number; floors: MinimapFloor[]; }[];
	};
	if (
		data?.format !== "sro-mission-dungeon-minimap-manifest" || data.version !== 1 ||
		!Array.isArray( data.dungeons ) || data.dungeons.length > 32768
	) throw Error( "Invalid dungeon minimap catalog" );
	const result = new Map<number, readonly MinimapFloor[]>();
	for ( const d of data.dungeons ) {
		if (
			!Number.isInteger( d.sectorId ) || d.sectorId < 32768 || d.sectorId > 65535 || !Array.isArray( d.floors ) ||
			d.floors.length > 256 || result.has( d.sectorId )
		) throw Error( "Invalid dungeon minimap region" );
		const ids = new Set<number>();
		for ( const f of d.floors ) {
			if (
				!Number.isInteger( f.floorIndex ) || f.floorIndex < 0 || ids.has( f.floorIndex ) ||
				typeof f.floorLabel !== "string" || f.floorLabel.length > 256 || ![ f.directory, f.prefix ].every( s =>
					typeof s === "string" && /^[a-z0-9_]+$/i.test( s )
				) || !Array.isArray( f.tiles ) || f.tiles.length > 65536 || f.tiles.some( t =>
					typeof t !== "string" || !/^\d+x\d+$/.test( t )
				)
			) throw Error( "Invalid dungeon minimap floor" );
			ids.add( f.floorIndex );
		}
		result.set( d.sectorId, d.floors );
	}
	return result;
}
// The retail art list names each tile's published file: a .texture where the
// retail DDJ is block compressed, a .png otherwise. Keyed by the path without
// its extension, lowercase, so a tile resolves whatever it shipped as.
export function minimapArt( value: unknown ): ReadonlyMap<string, string> {
	const data = value as { format?: unknown; version?: unknown; tilePaths?: unknown; };
	if (
		data?.format !== "sro-mission-dungeon-minimap-manifest" || data.version !== 1 ||
		!Array.isArray( data.tilePaths ) || data.tilePaths.length === 0 || data.tilePaths.length > 65536
	) throw Error( "Invalid retail minimap artwork catalog" );
	const paths = new Map<string, string>();
	for ( const path of data.tilePaths ) {
		if (
			typeof path !== "string" ||
			!/^\/assets\/images\/Media_extracted\/(?:minimap\/\d+x\d+|minimap_d\/[a-z0-9_]+\/[a-z0-9_]+_-?\d+x-?\d+)\.(?:png|texture)$/i
				.test( path )
		) throw Error( "Invalid retail minimap artwork path" );
		const key = path.replace( /\.[a-z]+$/i, "" ).toLowerCase();
		if ( paths.has( key ) ) throw Error( "Invalid retail minimap artwork path" );
		paths.set( key, path );
	}
	return paths;
}
export function minimapTiles( pose: Pose, zoom: number, floor?: MinimapFloor, art?: ReadonlyMap<string, string> ) {
	if ( !art ) return [];
	const dungeon = !!(pose.regionId & 0x8000);
	if ( dungeon && !floor ) return [];
	const gx = dungeon ? Math.floor( pose.x / 1920 ) : 0,
		gz = dungeon ? Math.floor( pose.z / 1920 ) : 0,
		rx = dungeon ? gx + 128 : pose.regionId & 255,
		rz = dungeon ? gz + 128 : pose.regionId >>> 8,
		x = pose.x - gx * 1920,
		z = pose.z - gz * 1920,
		scale = zoom / 1920,
		result = [];
	for ( let dz = -1; dz <= 1; dz++ ) {
		for ( let dx = -1; dx <= 1; dx++ ) {
			const key = (rx + dx) + "x" + (rz + dz);
			if ( dungeon && !floor!.tiles.includes( key ) ) continue;
			const path = art.get(
				("/assets/images/Media_extracted/" +
					(dungeon ? "minimap_d/" + floor!.directory + "/" + floor!.prefix + "_" : "minimap/") + key)
					.toLowerCase()
			);
			if ( !path ) continue;
			result.push( { x: (dx * 1920 - x) * scale, y: -((dz + 1) * 1920 - z) * scale, path } );
		}
	}
	return result;
}
