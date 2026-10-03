/*
===========================================================================
mesh-admission.ts - static resource admission before world instancing
Separates optional texture coordinates from strict geometry and index checks.
===========================================================================
*/
import {
	finiteNumbers,
	integerIndicesInRange,
	textureCoordinate
} from "@/engine/foundation/rendering/geometry-validation";
import type { Mesh } from "../internal/resource-contract";

/*
================
admitMesh

The caller caches this result per source mesh. Conversion owns a new UV array;
shared published resources stay immutable across neighboring region loads.
================
*/
export function admitMesh( mesh: Mesh ) {
	const uvs = Float32Array.from( mesh.uvs, textureCoordinate );
	let extent = 0;
	for ( const value of mesh.bounds.min ) extent = Math.max( extent, Math.abs( value ) );
	for ( const value of mesh.bounds.max ) extent = Math.max( extent, Math.abs( value ) );
	return {
		valid: !(!mesh.positions.length || mesh.positions.length % 3 || mesh.normals.length !== mesh.positions.length ||
			mesh.uvs.length !== mesh.positions.length / 3 * 2 || !finiteNumbers( mesh.positions ) ||
			!finiteNumbers( mesh.normals ) || !finiteNumbers( uvs ) || mesh.indices.length % 3 ||
			!integerIndicesInRange( mesh.indices, mesh.positions.length / 3 )),
		radius: extent * Math.sqrt( 3 ),
		material: mesh.metadata.materialName.toLowerCase(),
		uvs
	};
}
