/*
===========================================================================

geometry-vertices.ts - a mesh's packed GPU vertex stream, and its identity

packGeometryVertices builds the 14-float vertex the geometry shaders read.
meshArrays names the CPU arrays a mesh's GPU copy is made from: the device
shares one copy between uploads of the same immutable skinned mesh, and the
character budget charges that copy once (device/geometry.ts sharedMesh,
character-budget.ts characterMeshBytes), both by this identity.

===========================================================================
*/
import type { Geometry } from "@/engine/contracts/geometry";

/*
================
packGeometryVertices

Geometry has already passed admission. Do not allocate five typed-array
views per vertex while constructing the retained GPU vertex stream.
================
*/
export function packGeometryVertices( data: Geometry ): Float32Array {
	const { positions, normals, uvs, colors, maskUVs } = data;
	const count = positions.length / 3, packed = new Float32Array( count * 14 );
	for ( let i = 0; i < count; i++ ) {
		const out = i * 14, p = i * 3, uv = i * 2, color = i * 4;
		packed[out] = positions[p]!;
		packed[out + 1] = positions[p + 1]!;
		packed[out + 2] = positions[p + 2]!;
		packed[out + 3] = normals ? normals[p]! : 0;
		packed[out + 4] = normals ? normals[p + 1]! : 1;
		packed[out + 5] = normals ? normals[p + 2]! : 0;
		packed[out + 6] = uvs ? uvs[uv]! : 0;
		packed[out + 7] = uvs ? uvs[uv + 1]! : 0;
		packed[out + 8] = colors ? colors[color]! : 1;
		packed[out + 9] = colors ? colors[color + 1]! : 1;
		packed[out + 10] = colors ? colors[color + 2]! : 1;
		packed[out + 11] = colors ? colors[color + 3]! : 1;
		packed[out + 12] = maskUVs ? maskUVs[uv]! : 0;
		packed[out + 13] = maskUVs ? maskUVs[uv + 1]! : 0;
	}
	return packed;
}

/*
================
meshArrays

The CPU arrays a mesh's GPU copy (vertex stream, indices, skin table) is
made from: two meshes are one when every one of these is the same array.
================
*/
export function meshArrays(
	data: Pick<
		Geometry,
		"positions" | "normals" | "uvs" | "colors" | "maskUVs" | "vertices" | "indices" | "joints" | "weights"
	>
): readonly unknown[] {
	return [
		data.positions,
		data.normals,
		data.uvs,
		data.colors,
		data.maskUVs,
		data.vertices,
		data.indices,
		data.joints,
		data.weights
	];
}

/*
================
sameMesh
================
*/
export function sameMesh( a: readonly unknown[], b: readonly unknown[] ): boolean {
	return a.length === b.length && a.every( ( array, i ) => array === b[i] );
}
