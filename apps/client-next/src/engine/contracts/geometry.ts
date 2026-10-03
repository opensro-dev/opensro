/*
===========================================================================

geometry.ts - the mesh a caller hands to the renderer

Caller transfers logical ownership to the renderer; the renderer takes a
private copy of what it uploads.

===========================================================================
*/
export interface Geometry {
	readonly joints?: Uint32Array;
	readonly weights?: Float32Array;
	readonly bones?: Float32Array;
	readonly world?: boolean;
	readonly material?: import("./scene").WorldMaterial;
	readonly normals?: Float32Array;
	readonly uvs?: Float32Array;
	readonly colors?: Float32Array;
	readonly maskUVs?: Float32Array;
	readonly positions: Float32Array;
	readonly indices: Uint32Array;
	readonly transform: Float32Array;
	readonly instances?: Float32Array;
	// The caller will rewrite positions, colors or UVs after upload
	// (updatePositions). Only such meshes keep a CPU mirror of their packed
	// vertices; every other mesh drops it once the GPU holds the bytes.
	readonly dynamicVertices?: boolean;
	// The packed vertex stream (14 floats a vertex, packGeometryVertices) of a
	// dynamicVertices mesh, already built by the asset worker. The renderer
	// uploads it and keeps it as the mirror instead of packing on its thread.
	readonly vertices?: Float32Array;
}
