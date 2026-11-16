/*
===========================================================================

cloth-vertex-cases.mjs - deterministic weighted cloth streams and animation

Exercises all four skin influences, authored and default normals, pin kinds,
option changes and frame debt with the solver's existing varied fixture,
pins no solver step reads, and long off stretches with frames out of view.

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

// A joint only the unread pins of withUnreadPins hang from.
export const UNREAD_JOINT = 9;
/*
================
withUnreadPins

The case with every twelfth vertex (a pin 1) taken off every constraint and
hung from UNREAD_JOINT alone, so no solver step reads it; the palette grows
to that joint.
================
*/
export function withUnreadPins( base ) {
	const { geometry, cloth } = base.primitive, count = geometry.positions.length / 3;
	const unread = i => i % 12 === 0;
	const kept = cloth.constraints.map( ( constraint, index ) => [ constraint, index ] ).filter( ( [constraint] ) =>
		!unread( constraint[0] ) && !unread( constraint[1] )
	);
	const renumbered = new Map( kept.map( ( [, index], at ) => [ index, at ] ) );
	const data = {
		...cloth,
		constraints: kept.map( ( [constraint] ) => constraint ),
		order: cloth.order.filter( index => renumbered.has( index ) ).map( index => renumbered.get( index ) )
	};
	const joints = geometry.joints.slice(), weights = geometry.weights.slice();
	for ( let i = 0; i < count; i++ ) {
		if ( !unread( i ) ) continue;
		joints.set( [ UNREAD_JOINT, 0, 0, 0 ], i * 4 );
		weights.set( [ 1, 0, 0, 0 ], i * 4 );
	}
	return {
		...base,
		primitive: { geometry: { ...geometry, joints, weights }, cloth: data },
		input( frame ) {
			const source = base.input( frame );
			const palette = Float32Array.from(
				{ length: (UNREAD_JOINT + 1) * 16 },
				( _, i ) => Math.sin( i * .17 + frame * .031 )
			);
			return { ...source, palette };
		}
	};
}

/*
================
withOffStretches

The case with longer off stretches than its one-frame gaps: off for 30
frames of which it is out of view (absent: not drawn) for eight, for four, for five and
for two, so it restarts after its clock's 300, 100 and 16 ms frames, on a
step and between steps.
================
*/
export function withOffStretches( base ) {
	return {
		...base,
		input( frame ) {
			const source = base.input( frame );
			const off = frame >= 40 && frame < 70 || frame >= 80 && frame < 84 || frame >= 100 && frame < 105 ||
				frame >= 110 && frame < 112;
			return { ...source, enabled: source.enabled && !off, absent: frame >= 50 && frame < 58 };
		}
	};
}
