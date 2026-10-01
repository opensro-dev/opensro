import type { WorldCamera, WorldScene } from "@/engine/contracts/scene";
import type { Geometry } from "@/engine/contracts/geometry";
import { characterRadius } from "@/engine/foundation/animation/character-bounds";
import { geometryPickBlocks, geometryVertex, pickGeometry, pickGeometryBlocks, type PickRay } from "./picking";
type Point = readonly [number, number, number];
type SkinEnvelope = {
	joints: readonly { index: number; min: readonly number[]; max: readonly number[]; }[];
	weightMin: number;
	weightMax: number;
};
// blocks: geometryPickBlocks of an unskinned part, built on its first ray
// test. Static world meshes never change, so later frames cull whole index
// blocks instead of transforming and testing every triangle.
export type CameraCollisionPart = {
	object?: string;
	order?: number;
	geometry: Geometry;
	matrix: Float32Array;
	min: number[];
	max: number[];
	skin?: SkinEnvelope;
	sweep?: { min: number[]; max: number[]; };
	boundsDirty?: boolean;
	blocks?: Float64Array;
};
type CollisionNode = {
	min: number[];
	max: number[];
	children?: readonly CollisionNode[];
	indices?: readonly number[];
};
type CollisionProduct = CameraCollisionPart[] & {
	readonly acceleration: {
		readonly root: CollisionNode | null;
		readonly animatedRoot: CollisionNode | null;
		readonly animated: readonly number[];
	};
};
function* collisionTree(
	parts: readonly CameraCollisionPart[],
	indices: number[],
	sweep = false
): Generator<void, CollisionNode | null> {
	if ( !indices.length ) return null;
	const min = [ Infinity, Infinity, Infinity ], max = [ -Infinity, -Infinity, -Infinity ];
	const bounds = ( index: number ) => sweep ? parts[index]!.sweep! : parts[index]!;
	for ( let i = 0; i < indices.length; i++ ) {
		const box = bounds( indices[i]! );
		for ( let axis = 0; axis < 3; axis++ ) {
			min[axis] = Math.min( min[axis]!, box.min[axis]! );
			max[axis] = Math.max( max[axis]!, box.max[axis]! );
		}
		if ( (i & 127) === 127 ) yield;
	}
	if ( indices.length <= 8 ) return { min, max, indices };
	let axis = 0;
	for ( let k = 1; k < 3; k++ ) if ( max[k]! - min[k]! > max[axis]! - min[axis]! ) axis = k;
	indices.sort( ( a, b ) =>
		(bounds( a ).min[axis]! + bounds( a ).max[axis]!) - (bounds( b ).min[axis]! + bounds( b ).max[axis]!)
	);
	const middle = indices.length >>> 1;
	yield;
	return {
		min,
		max,
		children: [
			(yield* collisionTree( parts, indices.slice( 0, middle ), sweep ))!,
			(yield* collisionTree( parts, indices.slice( middle ), sweep ))!
		]
	};
}
// Nonnegative skin weights form a scaled convex combination. Retain bounds of
// each joint's contributing vertices once; pose changes transform these boxes,
// not every triangle vertex in every placement. Exact triangle tests still own hits.
function* skinEnvelope( geometry: Geometry ): Generator<void, SkinEnvelope | undefined> {
	if ( !geometry.bones || !geometry.weights || !geometry.joints ) return undefined;
	const joints = new Map<number, { index: number; min: number[]; max: number[]; }>();
	let weightMin = Infinity, weightMax = 0;
	let work = 0;
	for ( const vertex of new Set( geometry.indices ) ) {
		let sum = 0;
		for ( let lane = 0; lane < 4; lane++ ) {
			const weight = geometry.weights[vertex * 4 + lane]!;
			if ( !Number.isFinite( weight ) || weight < 0 ) return undefined;
			if ( !weight ) continue;
			sum += weight;
			const index = geometry.joints[vertex * 4 + lane]!;
			let box = joints.get( index );
			if ( !box ) {
				box = { index, min: [ Infinity, Infinity, Infinity ], max: [ -Infinity, -Infinity, -Infinity ] };
				joints.set( index, box );
			}
			for ( let axis = 0; axis < 3; axis++ ) {
				const value = geometry.positions[vertex * 3 + axis]!;
				box.min[axis] = Math.min( box.min[axis]!, value );
				box.max[axis] = Math.max( box.max[axis]!, value );
			}
		}
		weightMin = Math.min( weightMin, sum );
		weightMax = Math.max( weightMax, sum );
		if ( ++work === 128 ) {
			work = 0;
			yield;
		}
	}
	return joints.size ? { joints: [ ...joints.values() ], weightMin, weightMax } : undefined;
}
export function refitAnimatedCameraParts(
	parts: readonly CameraCollisionPart[],
	changed?: ReadonlySet<Float32Array>,
	ray?: PickRay
): void {
	const posed = new Map<SkinEnvelope, Float64Array>();
	for ( const part of parts ) {
		const palette = part.geometry.bones;
		if ( !palette ) continue;
		if ( !changed || changed.has( palette ) ) part.boundsDirty = true;
		if ( !part.boundsDirty || ray && part.sweep && !intersects( ray, part.sweep, 1 ) ) continue;
		part.boundsDirty = false;
		part.min.fill( Infinity );
		part.max.fill( -Infinity );
		if ( part.skin ) {
			const m = part.matrix;
			let bounds = posed.get( part.skin );
			if ( !bounds ) {
				bounds = Float64Array.of(
					Infinity,
					Infinity,
					Infinity,
					Infinity,
					-Infinity,
					-Infinity,
					-Infinity,
					-Infinity
				);
				for ( const box of part.skin.joints ) {
					const b = box.index * 16;
					// An affine box extremum chooses each input endpoint independently.
					// This is the same enclosure as eight corners with a quarter of the
					// matrix products. Keep homogeneous W for non-unit skin weight sums.
					for ( let axis = 0; axis < 4; axis++ ) {
						const x = palette[b + axis]!,
							y = palette[b + 4 + axis]!,
							z = palette[b + 8 + axis]!,
							w = palette[b + 12 + axis]!;
						const lo = x * (x < 0 ? box.max[0]! : box.min[0]!) + y * (y < 0 ? box.max[1]! : box.min[1]!) +
							z * (z < 0 ? box.max[2]! : box.min[2]!) + w;
						const hi = x * (x < 0 ? box.min[0]! : box.max[0]!) + y * (y < 0 ? box.min[1]! : box.max[1]!) +
							z * (z < 0 ? box.min[2]! : box.max[2]!) + w;
						bounds[axis] = Math.min( bounds[axis]!, lo );
						bounds[axis + 4] = Math.max( bounds[axis + 4]!, hi );
					}
				}
				for ( let axis = 0; axis < 4; axis++ ) {
					const lo = bounds[axis]!, hi = bounds[axis + 4]!, a = part.skin.weightMin, b = part.skin.weightMax;
					bounds[axis] = Math.min( lo * a, lo * b );
					bounds[axis + 4] = Math.max( hi * a, hi * b );
				}
				posed.set( part.skin, bounds );
			}
			for ( let axis = 0; axis < 3; axis++ ) {
				let low = 0, high = 0, magnitude = 1;
				for ( let k = 0; k < 4; k++ ) {
					const a = m[k * 4 + axis]! * bounds[k]!, b = m[k * 4 + axis]! * bounds[k + 4]!;
					low += Math.min( a, b );
					high += Math.max( a, b );
					magnitude += Math.max( Math.abs( a ), Math.abs( b ) );
				}
				const epsilon = 1e-9 * magnitude;
				part.min[axis] = low - epsilon;
				part.max[axis] = high + epsilon;
			}
			continue;
		}
		for ( const i of part.geometry.indices ) {
			const p = geometryVertex( part.geometry, part.matrix, i, palette );
			for ( let axis = 0; axis < 3; axis++ ) {
				part.min[axis] = Math.min( part.min[axis]!, p[axis]! );
				part.max[axis] = Math.max( part.max[axis]!, p[axis]! );
			}
		}
	}
}

// Immutable collision projection of admitted geometry, independent of draw LOD,
// frustum selection and opacity. Bounds are prepared once, not on each ray.
export function* prepareCameraCollisionParts( scene: WorldScene ): Generator<void, CameraCollisionPart[]> {
	const identity = new Float32Array( [ 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 ] );
	const parts: CameraCollisionPart[] = [];
	const sweepRadius = new Map<Geometry, number>();
	for ( const group of scene.groups ) {
		if ( group.material.sky || group.material.water || group.material.lightmap ) continue;
		// An explicit empty projection means every authored part failed the native
		// eligibility gate. Avoid building bounds or skin envelopes for decoration.
		if ( group.collision?.length === 0 ) continue;
		const source = group.geometry;
		const ranges = group.ranges?.filter( r => r.lod === 0 );
		for ( const range of ranges ?? [ null ] ) {
			const indices = range ?
				source.indices.slice( range.indexStart, range.indexStart + range.indexCount ).map( i =>
					i - range.vertexStart
				) :
				source.indices;
			let positions = source.positions;
			if ( range ) {
				// Render seam stitching mutates positions; retain original full-resolution heights.
				positions = source.positions.slice(
					range.vertexStart * 3,
					(range.vertexStart + range.vertexCount) * 3
				);
				for ( let i = 0; i < range.vertexCount; i++ ) {
					const x = Math.round( (positions[i * 3]! - range.cell[0] * 320) / 20 ),
						z = Math.round( (positions[i * 3 + 2]! - range.cell[1] * 320) / 20 );
					positions[i * 3 + 1] = range.heights[z * 17 + x]!;
				}
			}
			const geometry = { ...source, positions, indices };
			const animation = group.animation,
				model = animation && scene.models?.[animation.model],
				primitive = model && model.primitives[animation!.primitive];
			const affine = ( m: ArrayLike<number>, offset = 0 ) =>
				m[offset + 3] === 0 && m[offset + 7] === 0 && m[offset + 11] === 0 && m[offset + 15] === 1;
			const radius = model && primitive && model.nodes.every( node => !node.matrix || affine( node.matrix ) ) &&
					primitive.joints.every( ( _, i ) => affine( primitive.inverseBind, i * 16 ) ) ?
				characterRadius( { ...model, primitives: [ { ...primitive, geometry } ] } ) :
				undefined;
			if ( radius !== undefined ) sweepRadius.set( geometry, radius );
			const lo = [ Infinity, Infinity, Infinity ], hi = [ -Infinity, -Infinity, -Infinity ];
			let work = 0;
			for ( const i of indices ) {
				for ( let a = 0; a < 3; a++ ) {
					lo[a] = Math.min( lo[a]!, positions[i * 3 + a]! );
					hi[a] = Math.max( hi[a]!, positions[i * 3 + a]! );
				}
				if ( ++work === 128 ) {
					work = 0;
					yield;
				}
			}
			const instances = source.instances ?? identity;
			const collisionGeometries = new Map<string, Geometry>();
			for ( let i = 0; i < instances.length; i += 16 ) {
				const matrix = instances.subarray( i, i + 16 ),
					min = [ Infinity, Infinity, Infinity ],
					max = [ -Infinity, -Infinity, -Infinity ];
				for ( let corner = 0; corner < 8; corner++ ) {
					for ( let a = 0; a < 3; a++ ) {
						const n = matrix[a]! * (corner & 1 ? hi[0]! : lo[0]!) +
							matrix[4 + a]! * (corner & 2 ? hi[1]! : lo[1]!) +
							matrix[8 + a]! * (corner & 4 ? hi[2]! : lo[2]!) + matrix[12 + a]!;
						min[a] = Math.min( min[a]!, n );
						max[a] = Math.max( max[a]!, n );
					}
				}
				if ( group.collision ) {
					for ( const c of group.collision ) {
						if ( c.instance === i / 16 ) {
							const key = c.indexStart + ":" + c.indexCount;
							let shared = collisionGeometries.get( key );
							if ( !shared ) {
								shared = {
									...geometry,
									indices: geometry.indices.slice( c.indexStart, c.indexStart + c.indexCount )
								};
								collisionGeometries.set( key, shared );
								if ( radius !== undefined ) sweepRadius.set( shared, radius );
							}
							parts.push( {
								object: c.object,
								order: c.order,
								geometry: shared,
								matrix,
								min: [ ...min ],
								max: [ ...max ]
							} );
						}
					}
				} else parts.push( { geometry, matrix, min, max } );
				if ( (i / 16 & 31) === 31 ) yield;
			}
		}
	}
	// Native first-part precedence must survive draw/material batching. Unknown
	// synthetic groups remain independent colliders; terrain remains nearest-hit.
	parts.sort( ( a, b ) =>
		a.object && b.object ?
			a.object.localeCompare( b.object ) || (a.order ?? 0) - (b.order ?? 0) :
			a.object ?
			-1 :
			b.object ?
			1 :
			0
	);
	const envelopes = new Map<Geometry, SkinEnvelope | undefined>();
	for ( const part of parts ) {
		if ( !envelopes.has( part.geometry ) ) envelopes.set( part.geometry, yield* skinEnvelope( part.geometry ) );
		part.skin = envelopes.get( part.geometry );
		const radius = sweepRadius.get( part.geometry ), skin = part.skin;
		if ( radius !== undefined && skin ) {
			const min: number[] = [], max: number[] = [], m = part.matrix, r = radius * skin.weightMax;
			for ( let axis = 0; axis < 3; axis++ ) {
				const extent = r * (Math.abs( m[axis]! ) + Math.abs( m[4 + axis]! ) + Math.abs( m[8 + axis]! )),
					a = m[12 + axis]! * skin.weightMin,
					b = m[12 + axis]! * skin.weightMax,
					epsilon = 1e-6 * (1 + extent + Math.max( Math.abs( a ), Math.abs( b ) ));
				min[axis] = Math.min( a, b ) - extent - epsilon;
				max[axis] = Math.max( a, b ) + extent + epsilon;
			}
			part.sweep = { min, max };
		}
	}
	refitAnimatedCameraParts( parts );
	const fixed: number[] = [], bounded: number[] = [], animated: number[] = [];
	for ( let i = 0; i < parts.length; i++ ) {
		if ( parts[i]!.geometry.indices.length ) {
			(parts[i]!.geometry.bones ?
				(parts[i]!.sweep ? bounded : animated) :
				fixed).push( i );
		}
	}
	return Object.assign( parts, {
		acceleration: {
			root: yield* collisionTree( parts, fixed ),
			animatedRoot: yield* collisionTree( parts, bounded, true ),
			animated
		}
	} );
}
export function cameraCollisionParts( scene: WorldScene ): CameraCollisionPart[] {
	const preparation = prepareCameraCollisionParts( scene );
	let result = preparation.next();
	while ( !result.done ) result = preparation.next();
	return result.value;
}
function intersects(
	ray: PickRay,
	part: { readonly min: readonly number[]; readonly max: readonly number[]; },
	nearest: number
) {
	let low = 0, high = nearest;
	for ( let a = 0; a < 3; a++ ) {
		const d = ray.delta[a]!, s = ray.start[a]!;
		if ( Math.abs( d ) < 1e-12 ) {
			if ( s < part.min[a]! || s > part.max[a]! ) return false;
			continue;
		}
		const t0 = (part.min[a]! - s) / d, t1 = (part.max[a]! - s) / d;
		low = Math.max( low, Math.min( t0, t1 ) );
		high = Math.min( high, Math.max( t0, t1 ) );
		if ( low > high ) return false;
	}
	return true;
}
function queryTree( node: CollisionNode | null, ray: PickRay, indices: number[] ): void {
	if ( !node || !intersects( ray, node, 1 ) ) return;
	if ( node.indices ) indices.push( ...node.indices );
	else for ( const child of node.children! ) queryTree( child, ray, indices );
}
export function animatedCameraCandidates(
	parts: readonly CameraCollisionPart[],
	ray: PickRay
): readonly CameraCollisionPart[] {
	const acceleration = (parts as Partial<CollisionProduct>).acceleration;
	if ( !acceleration ) {
		return parts.filter( part => part.geometry.bones && (!part.sweep || intersects( ray, part.sweep, 1 )) );
	}
	const indices = [ ...acceleration.animated ];
	queryTree( acceleration.animatedRoot, ray, indices );
	return indices.map( index => parts[index]! ).filter( part => !part.sweep || intersects( ray, part.sweep, 1 ) );
}
export function cameraSegmentHit( parts: readonly CameraCollisionPart[], ray: PickRay ): number | null {
	let nearest = 1, hit = false;
	const resolved = new Set<string>();
	const acceleration = (parts as Partial<CollisionProduct>).acceleration;
	let candidates: readonly CameraCollisionPart[] = parts;
	if ( acceleration ) {
		const indices = [ ...acceleration.animated ];
		queryTree( acceleration.root, ray, indices );
		queryTree( acceleration.animatedRoot, ray, indices );
		// Preserve first-hit-part precedence within each native object, independent
		// of tree traversal order. Animated bounds are refreshed separately.
		indices.sort( ( a, b ) => a - b );
		candidates = indices.map( index => parts[index]! );
	}
	for ( const part of candidates ) {
		if ( part.object && resolved.has( part.object ) ) continue;
		if ( part.sweep && !intersects( ray, part.sweep, 1 ) ) continue;
		// A farther early part still suppresses later parts of the SAME object.
		if ( !intersects( ray, part, part.object ? 1 : nearest ) ) continue;
		const fraction = part.geometry.bones ?
			pickGeometry( ray, part.geometry, part.matrix, part.geometry.bones ) :
			pickGeometryBlocks( ray, part.geometry, part.matrix, part.blocks ??= geometryPickBlocks( part.geometry ) );
		if ( fraction !== null ) {
			if ( part.object ) resolved.add( part.object );
			if ( fraction < nearest || fraction === nearest && !hit ) {
				nearest = fraction;
				hit = true;
			}
		}
	}
	return hit ? nearest : null;
}
export function cameraCollisionPalettes(
	parts: readonly CameraCollisionPart[],
	ray: PickRay
): ReadonlySet<Float32Array> {
	const palettes = new Set<Float32Array>();
	for ( const part of animatedCameraCandidates( parts, ray ) ) palettes.add( part.geometry.bones! );
	return palettes;
}
export function followDistance( pitch: number, requested: number ): number {
	if ( pitch >= 0 ) return requested;
	const f = Math.fround, n = f( -pitch / 0.8999999761581421 );
	return Math.min( requested, f( (1 - f( Math.sin( f( 1.5707963705062866 * n ) ) )) * 110 + 40 ) );
}
// Native 0x68f830 target-height feedback and 0x68fad0 segment/margin.
// Height is supplied by the character presentation metadata owner.
export function followCameraQuery( camera: WorldCamera, previous: number | null ) {
	const follow = camera.follow!;
	const distance = followDistance( follow.pitch, follow.distance );
	const lift = Math.fround(
		14.199999809265137 +
			(14.199999809265137 -
					Math.fround(
						follow.mounted ? (follow.height ?? 20) * 0.25 : (follow.height ?? 20) - 3.7999999523162842
					)) / 140 * ((previous ?? distance) - 150)
	);
	// 68FA10: add the script offset and lift before base Y; world X/Y,
	// independent of yaw. This target also seeds the collision segment.
	const target: Point = [
		Math.fround( camera.target[0] + (follow.offset?.[0] ?? 0) ),
		Math.fround( (follow.offset?.[1] ?? 0) + lift + camera.target[1] ),
		camera.target[2]
	];
	const direction = [
		Math.sin( follow.yaw ) * Math.cos( follow.pitch ),
		Math.sin( follow.pitch ),
		Math.cos( follow.yaw ) * Math.cos( follow.pitch )
	];
	const ray = {
		start: target.map( ( n, i ) => n + direction[i]! * 5 ),
		delta: direction.map( n => n * (distance - 5) )
	};
	return { ray, target, direction, distance };
}
export function resolveFollowCamera(
	camera: WorldCamera,
	parts: readonly CameraCollisionPart[],
	previous: number | null
) {
	const { ray, target, direction, distance } = followCameraQuery( camera, previous );
	const hit = cameraSegmentHit( parts, ray );
	const resolved = hit === null ?
		distance :
		Math.max( 0.001, Math.fround( Math.fround( 5 + hit * (distance - 5) ) - 5 ) );
	const eye = target.map( ( n, i ) => n + direction[i]! * resolved ) as unknown as Point;
	return { camera: { ...camera, target, eye }, collision: hit === null ? null : resolved };
}
