/*
===========================================================================

character-bounds.ts - a conservative radius around every pose of a model

The radius encloses the model in any admitted clip: rotations preserve
norms, parent scales and translations compose the envelope, and skinned
vertices are measured in bind space from each influencing joint.

The per-vertex skin pass depends only on a primitive's geometry and inverse
binds, never on which nodes its joint slots name, so it is kept per
geometry: an assembled character (body plus equipment, a new model every
assembly) reuses its parts' passes. Measuring them again was 8 ms on the
main thread when a character came into view.

===========================================================================
*/
import type { CharacterModel, CharacterPrimitive } from "@/engine/contracts/character";

/*
================
CharacterBoundsCache

Per-geometry results of characterRadius, owned by the character renderer:
bind-space joint distances of a skinned geometry (valid for one
inverse-bind array) and the longest vertex of an unskinned one.
================
*/
export interface CharacterBoundsCache {
	readonly skin: WeakMap<object, { inverseBind: Float32Array; distances: Float64Array; }>;
	readonly vertices: WeakMap<Float32Array, number>;
}

/*
================
createCharacterBoundsCache
================
*/
export function createCharacterBoundsCache(): CharacterBoundsCache {
	return { skin: new WeakMap(), vertices: new WeakMap() };
}

/*
================
stretch

||M||2 <= sqrt(||transpose(M) M||infinity). Unlike the Frobenius bound, an
identity or rotation does not spuriously enlarge every skeleton level by
sqrt(3). Absolute Gram row sums also enclose shear and nonuniform scale.
================
*/
function stretch( m: ArrayLike<number> ): number {
	let maximum = 0;
	for ( let i = 0; i < 3; i++ ) {
		let sum = 0;
		for ( let j = 0; j < 3; j++ ) {
			sum += Math.abs( m[i * 4]! * m[j * 4]! + m[i * 4 + 1]! * m[j * 4 + 1]! + m[i * 4 + 2]! * m[j * 4 + 2]! );
		}
		maximum = Math.max( maximum, sum );
	}
	// Include headroom for float32 matrix/palette products, not just the
	// double-precision bound calculation (unit rotations are the tight case).
	return Math.sqrt( maximum ) * (1 + 8 * 2 ** -23);
}

/*
================
jointDistances

Per joint slot, the farthest bind-space distance of a vertex it influences
(-1 for a slot no vertex uses). Bind-space distance must be measured from
each influencing joint, not from the model origin plus the inverse-bind
translation: the latter counts a limb's offset twice and admits hidden rigs.
================
*/
function jointDistances( primitive: CharacterPrimitive, cache: CharacterBoundsCache | undefined ): Float64Array {
	const geometry = primitive.geometry, m = primitive.inverseBind;
	const cached = cache?.skin.get( geometry );
	if ( cached && cached.inverseBind === m && cached.distances.length === primitive.joints.length ) {
		return cached.distances;
	}
	const joints = geometry.joints!, weights = geometry.weights!, positions = geometry.positions;
	const distances = new Float64Array( primitive.joints.length ).fill( -1 );
	for ( let v = 0; v < positions.length / 3; v++ ) {
		const x = positions[v * 3]!, y = positions[v * 3 + 1]!, z = positions[v * 3 + 2]!;
		for ( let influence = 0; influence < 4; influence++ ) {
			if ( weights[v * 4 + influence] === 0 ) continue;
			const joint = joints[v * 4 + influence]!, offset = joint * 16;
			const a = m[offset]! * x + m[offset + 4]! * y + m[offset + 8]! * z + m[offset + 12]!,
				b = m[offset + 1]! * x + m[offset + 5]! * y + m[offset + 9]! * z + m[offset + 13]!,
				c = m[offset + 2]! * x + m[offset + 6]! * y + m[offset + 10]! * z + m[offset + 14]!;
			let magnitude = 1;
			for ( let row = 0; row < 3; row++ ) {
				magnitude += Math.abs( m[offset + row]! * x ) + Math.abs( m[offset + 4 + row]! * y ) +
					Math.abs( m[offset + 8 + row]! * z ) + Math.abs( m[offset + 12 + row]! );
			}
			distances[joint] = Math.max( distances[joint]!, Math.hypot( a, b, c ) + magnitude * 16 * 2 ** -23 );
		}
	}
	cache?.skin.set( geometry, { inverseBind: m, distances } );
	return distances;
}

/*
================
vertexLength

The longest vertex of positions, measured once per array.
================
*/
function vertexLength( positions: Float32Array, cache: CharacterBoundsCache | undefined ): number {
	let vertex = cache?.vertices.get( positions );
	if ( vertex === undefined ) {
		vertex = 0;
		for ( let i = 0; i < positions.length; i += 3 ) {
			vertex = Math.max( vertex, Math.hypot( positions[i]!, positions[i + 1]!, positions[i + 2]! ) );
		}
		cache?.vertices.set( positions, vertex );
	}
	return vertex;
}

/*
================
characterRadius

Conservative envelope over every admitted clip, including cubic tangent
bounds.
================
*/
export function characterRadius( model: CharacterModel, cache?: CharacterBoundsCache ): number {
	const local = model.nodes.map( node => Math.hypot( ...node.translation ) ),
		scale = model.nodes.map( node => Math.max( ...node.scale.map( Math.abs ) ) );
	for ( const clip of model.clips ) {
		for ( const channel of clip.channels ) {
			if ( channel.path === "rotation" ) continue;
			let maximum = 0;
			for ( const value of channel.values ) maximum = Math.max( maximum, Math.abs( value ) );
			if ( channel.interpolation === "CUBICSPLINE" ) maximum *= 4 + 2 * clip.duration;
			if ( channel.path === "translation" ) {
				local[channel.node] = Math.max( local[channel.node]!, maximum * Math.sqrt( 3 ) );
			} else scale[channel.node] = Math.max( scale[channel.node]!, maximum );
		}
	}
	const translation: number[] = [], globalScale: number[] = [], visiting = new Set<number>();
	/*
	================
	resolve

	A node's farthest translation and largest scale from the model origin.
	================
	*/
	function resolve( index: number ) {
		if ( translation[index] !== undefined ) return;
		if ( visiting.has( index ) ) throw new Error( "Cyclic character bounds" );
		visiting.add( index );
		const node = model.nodes[index]!;
		if ( node.matrix ) {
			local[index] = Math.hypot( node.matrix[12]!, node.matrix[13]!, node.matrix[14]! );
			scale[index] = stretch( node.matrix );
		}
		if ( node.parent >= 0 ) {
			resolve( node.parent );
			translation[index] = translation[node.parent]! + globalScale[node.parent]! * local[index]!;
			globalScale[index] = globalScale[node.parent]! * scale[index]!;
		} else {
			translation[index] = local[index]!;
			globalScale[index] = scale[index]!;
		}
		visiting.delete( index );
	}
	for ( let n = 0; n < model.nodes.length; n++ ) resolve( n );
	let radius = Math.max( 0, ...translation );
	for ( const primitive of model.primitives ) {
		const geometry = primitive.geometry;
		if ( geometry.joints && geometry.weights && !primitive.emission ) {
			// Skin weights are admitted nonnegative and normalized. Every
			// output is in the convex hull of these all-clip joint envelopes.
			const distances = jointDistances( primitive, cache );
			for ( let i = 0; i < distances.length; i++ ) {
				if ( distances[i]! < 0 ) continue;
				const joint = primitive.joints[i]!;
				radius = Math.max( radius, translation[joint]! + globalScale[joint]! * distances[i]! );
			}
			continue;
		}
		const vertex = vertexLength( geometry.positions, cache );
		for ( let i = 0; i < primitive.joints.length; i++ ) {
			const joint = primitive.joints[i]!, m = primitive.inverseBind.subarray( i * 16, i * 16 + 16 );
			radius = Math.max(
				radius,
				translation[joint]! +
					globalScale[joint]! * (stretch( m ) * vertex + Math.hypot( m[12]!, m[13]!, m[14]! ))
			);
		}
	}
	if ( !Number.isFinite( radius ) ) throw new Error( "Invalid character bounds" );
	// Include palette composition and normalized float32 weight roundoff.
	return radius * (1 + 16 * 2 ** -23) + 16 * 2 ** -23;
}
