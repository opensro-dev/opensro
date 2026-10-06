/*
===========================================================================

cloth-vertex-cases.mjs - deterministic weighted cloth streams and animation

Exercises all four skin influences, authored and default normals, pin kinds,
option changes and frame debt with the solver's existing varied fixture.

===========================================================================
*/
import { clothCase } from "./cloth-cases.mjs";

/*
================
clothVertexCase
================
*/
export function clothVertexCase( skinned, normals ) {
	const fixture = clothCase( 7 ), count = fixture.rest.length / 3;
	const geometry = {
		positions: fixture.rest,
		indices: Uint32Array.of( 0, 1, 2 ),
		transform: Float32Array.of( 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 ),
		normals: normals ? Float32Array.from( { length: count * 3 }, ( _, i ) => Math.cos( i * .13 ) ) : undefined,
		joints: skinned ? Uint32Array.from( { length: count * 4 }, ( _, i ) => i % 8 ) : undefined,
		weights: skinned ?
			Float32Array.from( { length: count * 4 }, ( _, i ) => i % 7 === 0 ? 0 : (i % 4 + 1) / 10 ) :
			undefined
	};
	let seconds = 0;
	return {
		primitive: { geometry, cloth: fixture.data },
		frames: fixture.frames,
		random: fixture.input( 0 ).random,
		calls: fixture.calls,
		/*
		================
		input
		================
		*/
		input( frame ) {
			const source = fixture.input( frame );
			seconds += source.deltaMs / 1000;
			return {
				palette: Float32Array.from( { length: 128 }, ( _, i ) => Math.sin( i * .17 + frame * .031 ) ),
				seconds,
				enabled: source.enabled,
				motion: { direction: source.direction, speed: source.speed }
			};
		}
	};
}
