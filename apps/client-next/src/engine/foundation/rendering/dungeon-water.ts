/*
===========================================================================

dungeon-water.ts - the render group of one dungeon water surface

Builds the blended, animated sheet a .dof water object draws. Its fog stays
the linear table fog 8AEEAC turns on for this pass, unlike the block shader
fog of the rooms around it (dungeon-fog.ts).

===========================================================================
*/
import { identity } from "./world-math";
import type { WorldGroup } from "@/engine/contracts/scene";
export interface DungeonWaterSurface {
	readonly blockIndex: number;
	readonly vertices: readonly number[];
	readonly color: number;
	readonly fog: {
		readonly color: number;
		readonly nearPlane: number;
		readonly farPlane: number;
		readonly intensity: number;
	};
}
/*
================
dungeonWaterGroup

AD5730's provider is already translated. 8AE2D0 draws only its first four
vertices as a fan, with UV extents (max-min)/20/10*4 (8AF37F..8AF40B).
================
*/
export function dungeonWaterGroup( surface: DungeonWaterSurface, frames: readonly string[] ): WorldGroup {
	if (
		!Number.isInteger( surface.blockIndex ) || surface.blockIndex < 0 || surface.vertices.length < 12 ||
		surface.vertices.length % 3 || !surface.vertices.every( Number.isFinite ) ||
		!Number.isInteger( surface.color ) || surface.color < 0 || surface.color > 0xffffffff || frames.length !== 30
	) throw Error( "Invalid dungeon water provider" );
	const p = surface.vertices.slice( 0, 12 ),
		xs = [ p[0]!, p[3]!, p[6]!, p[9]! ],
		zs = [ p[2]!, p[5]!, p[8]!, p[11]! ];
	const minX = Math.min( ...xs ), maxX = Math.max( ...xs ), minZ = Math.min( ...zs ), maxZ = Math.max( ...zs );
	const u = Math.fround( Math.fround( (maxX - minX) / 20 / 10 ) * 4 ),
		v = Math.fround( Math.fround( (maxZ - minZ) / 10 / 20 ) * 4 );
	const color = surface.color,
		center: [number, number, number] = [
			(minX + maxX) / 2,
			(p[1]! + p[4]! + p[7]! + p[10]!) / 4,
			(minZ + maxZ) / 2
		];
	return {
		id: `dungeon-water:${surface.blockIndex}`,
		dungeonBlock: surface.blockIndex,
		center,
		radius: Math.max(
			...[ 0, 1, 2, 3 ].map( i =>
				Math.hypot( p[i * 3]! - center[0], p[i * 3 + 1]! - center[1], p[i * 3 + 2]! - center[2] )
			)
		),
		material: {
			color: [ ((color >>> 16) & 255) / 255, ((color >>> 8) & 255) / 255, (color & 255) / 255, 180 / 255 ],
			textureAlpha: false,
			texture: frames[0]!,
			frames: [ ...frames ],
			fog: { ...surface.fog, linear: true },
			alphaCutoff: 0,
			blend: true,
			doubleSided: true,
			unlit: true
		},
		geometry: {
			world: true,
			positions: new Float32Array( p ),
			normals: new Float32Array( [ 0, 1, 0, 0, 1, 0, 0, 1, 0, 0, 1, 0 ] ),
			uvs: new Float32Array( [ 0, 0, 0, v, u, v, u, 0 ] ),
			indices: new Uint32Array( [ 0, 1, 2, 0, 2, 3 ] ),
			instances: identity(),
			transform: identity()
		}
	};
}
