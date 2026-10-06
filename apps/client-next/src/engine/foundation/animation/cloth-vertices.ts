/*
===========================================================================

cloth-vertices.ts - one mesh instance's retained dynamic vertex stream

===========================================================================
*/
import type { Geometry } from "@/engine/contracts/geometry";
import type { ClothData } from "./cloth";
import { createCloth } from "@/engine/foundation/animation/cloth";
import { packGeometryVertices } from "@/engine/foundation/rendering/geometry-vertices";

/*
================
createClothVertices

Keep separate state per actor, even when actors share the same asset.
================
*/
export function createClothVertices( primitive: { geometry: Geometry; cloth?: ClothData; }, random: () => number ) {
	const mesh = primitive.geometry, data = primitive.cloth!;
	const simulation = createCloth( data, mesh.positions );
	const anchors = new Float32Array( mesh.positions.length );
	const vertices = packGeometryVertices( mesh );
	const skinned = !!mesh.joints && !!mesh.weights;
	let lastTime: number | undefined;
	return {
		/*
		================
		update

		AEADE0 refreshes pinned vertices from the skeleton before simulation.
		================
		*/
		update(
			palette: Float32Array,
			seconds: number,
			enabled: boolean,
			motion: { direction: readonly number[]; speed: number; }
		) {
			// Unskinned meshes have immutable model-space anchors. Option changes
			// still reset the simulation below, using those original anchors.
			for ( let i = 0; (skinned || lastTime === undefined) && i < mesh.positions.length / 3; i++ ) {
				// Once initialized, free vertices belong to the simulation. Their
				// skeleton anchors and normals are unused until animation is disabled.
				if ( enabled && lastTime !== undefined && data.pins[i] === 0 ) continue;
				for ( let axis = 0; axis < 3; axis++ ) {
					let position = 0, normal = 0;
					if ( mesh.joints && mesh.weights ) {
						for ( let joint = 0; joint < 4; joint++ ) {
							const weight = mesh.weights[i * 4 + joint]!;
							if ( !weight ) continue;
							const base = mesh.joints[i * 4 + joint]! * 16;
							let p = palette[base + 12 + axis]!, n = 0;
							for ( let c = 0; c < 3; c++ ) {
								p += palette[base + c * 4 + axis]! * mesh.positions[i * 3 + c]!;
								n += palette[base + c * 4 + axis]! * (mesh.normals?.[i * 3 + c] ?? (c === 1 ? 1 : 0));
							}
							position += p * weight;
							normal += n * weight;
						}
					} else {
						position = mesh.positions[i * 3 + axis]!;
						normal = mesh.normals?.[i * 3 + axis] ?? (axis === 1 ? 1 : 0);
					}
					anchors[i * 3 + axis] = position;
					if ( !enabled || data.pins[i] !== 0 || lastTime === undefined ) {
						vertices[i * 14 + 3 + axis] = normal;
					}
				}
			}
			const positions = simulation.advance( {
				anchors,
				deltaMs: lastTime === undefined ? 0 : Math.trunc( seconds * 1000 ) - Math.trunc( lastTime * 1000 ),
				enabled,
				direction: motion.direction,
				speed: motion.speed,
				random
			} );
			lastTime = seconds;
			for ( let i = 0; i < positions.length / 3; i++ ) {
				for ( let axis = 0; axis < 3; axis++ ) vertices[i * 14 + axis] = positions[i * 3 + axis]!;
			}
			return vertices;
		}
	};
}
