/*
===========================================================================

navigation-fixture.mjs - a one-cell navigation product for movement tests

A flat, fully covered region (optionally all blocked) with one placed
object, and a pose inside it. Shared by the navigation and movement
receipt tests so the fixture has one definition.

===========================================================================
*/
/*
================
mesh
================
*/
export function mesh( y = 10 ) {
	return {
		vertices: Float32Array.from( [ 0, y, 0, 100, y, 0, 100, y, 100, 0, y, 100 ] ),
		cells: Uint16Array.from( [ 0, 1, 2, 0, 2, 3 ] ),
		edges: Uint32Array.from( [ 1, 2, 0, 65535, 3, 0, 0, 2, 0, 1, 4, 1 ] ),
		bounds: [ 0, y, 0, 100, y, 100 ],
		passThrough: false
	};
}
/*
================
product
================
*/
export function product( regionId = 257, blocked = false ) {
	return {
		regionId,
		complete: true,
		objects: [ { x: 0, y: 0, z: 0, yaw: 0, mesh: mesh() } ],
		navmesh: {
			regionSize: 1920,
			tileSize: 20,
			tilesPerAxis: 96,
			regions: [ {
				dx: 0,
				dz: 0,
				blockedTiles: Buffer.alloc( 9216, blocked ? 1 : 0 ).toString( "base64" ),
				tileCellIds: Buffer.alloc( 36864 ).toString( "base64" ),
				heightMap: Buffer.alloc( 97 * 97 * 4 ).toString( "base64" ),
				cells: { count: 1 }
			} ]
		}
	};
}
export const pose = { regionId: 257, x: 10, y: 10, z: 50, angle: 0 };
