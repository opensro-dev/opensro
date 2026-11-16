/*
===========================================================================

cloth-vertices.ts - one mesh instance's retained dynamic vertex stream

===========================================================================
*/
import type { Geometry } from "@/engine/contracts/geometry";
import type { ClothData } from "./cloth";
import { createCloth } from "@/engine/foundation/animation/cloth";
import { packGeometryVertices } from "@/engine/foundation/rendering/geometry-vertices";
import { skinClothVertex } from "./cloth-skin";

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
	// One skinned vertex, [x, y, z, nx, ny, nz] (cloth-skin.ts).
	const skin = new Float64Array( 6 );
	let lastTime: number | undefined, lastEnabled = false;
	return {
		/*
		================
		hold

		True when update would return exactly the last vertices, given that
		the palette has not changed since, so the caller may skip it. Enabled,
		no step may fall due and the skipped time stays owed: deltaMs is
		measured from the last real update, so the accumulator sees the same
		sum. Disabled, an update resets the accumulator, so the frame's time
		passes here instead.
		================
		*/
		hold( seconds: number, enabled: boolean ): boolean {
			if ( lastTime === undefined || enabled !== lastEnabled ) return false;
			if ( !enabled ) {
				lastTime = seconds;
				return true;
			}
			return !simulation.due( Math.trunc( seconds * 1000 ) - Math.trunc( lastTime * 1000 ), anchors );
		},

		/*
		================
		pause

		Out of view the frame's time passes without being owed: the original
		adds a frame's time to the cloth accumulator (+0x194) only on its
		compound's visible path (A9EFB0), so the next update steps only for
		the time after this frame.
		================
		*/
		pause( seconds: number ) {
			if ( lastTime !== undefined ) lastTime = seconds;
		},

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
				skinClothVertex( mesh, palette, i, skin );
				const at = i * 3;
				anchors[at] = skin[0]!;
				anchors[at + 1] = skin[1]!;
				anchors[at + 2] = skin[2]!;
				if ( !enabled || data.pins[i] !== 0 || lastTime === undefined ) {
					vertices[i * 14 + 3] = skin[3]!;
					vertices[i * 14 + 4] = skin[4]!;
					vertices[i * 14 + 5] = skin[5]!;
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
			lastEnabled = enabled;
			for ( let i = 0; i < positions.length / 3; i++ ) {
				vertices[i * 14] = positions[i * 3]!;
				vertices[i * 14 + 1] = positions[i * 3 + 1]!;
				vertices[i * 14 + 2] = positions[i * 3 + 2]!;
			}
			return vertices;
		}
	};
}
