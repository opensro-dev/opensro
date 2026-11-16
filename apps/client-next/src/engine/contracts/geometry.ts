/*
===========================================================================

geometry.ts - the mesh a caller hands to the renderer

Caller transfers logical ownership to the renderer; the renderer takes a
private copy of what it uploads.

===========================================================================
*/
/*
================
Geometry
================
*/
export interface Geometry {
	readonly cloth?: import("@/engine/foundation/animation/cloth").ClothData;
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
	// The caller rewrites the material uniform every frame (a material clock:
	// equipment glow, colour timeline, texture motion, TEXTUREFACTOR pulse).
	// Such materials share staged slabs that reach the GPU in one write a
	// frame; any other material is written once (its rare updates still work).
	readonly dynamicMaterial?: boolean;
	// Each instance row carries its own equipment glow colour and UV in a
	// row-state stream beside the instances (updateRowStates), which the
	// draw's shader variant reads in place of the material's (every other
	// draw keeps the plain shader and no row-state binding): rows of several actors share
	// one draw while each keeps its own glow phase. Gain, alpha test and the
	// enabled bit stay in the material uniform (updateEquipmentGlow).
	readonly rowStates?: boolean;
	// A skinned cloth mesh's stream slots: the vertex each entry of its cloth
	// stream holds, in index order (cloth-stream.ts clothStreamSlots). Its
	// skin table carries a slot map of one entry a vertex and the stream,
	// which the owner rewrites with writeClothStream. The GPU skins a vertex
	// unless it has a slot whose entry says the solver owns its position or
	// normal.
	readonly clothStream?: Int32Array;
	// The packed vertex stream (14 floats a vertex, packGeometryVertices) of a
	// dynamicVertices mesh, already built by the asset worker. The renderer
	// uploads it and keeps it as the mirror instead of packing on its thread.
	readonly vertices?: Float32Array;
}
