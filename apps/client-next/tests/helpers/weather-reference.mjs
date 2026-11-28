/*
===========================================================================

weather-reference.mjs - a CPU model of the GPU weather pool, for tests

The weather owner (weather.ts) hands its drops to the GPU
(device/weather-particles.ts) and never reads them back. A node test
needs the drops themselves: this model applies the same spawn records
and steps the same way the compute shader does, in float32, so a test
can count live drops, check that no spawn lands on a live slot, and
compare against a real WebGPU pool (tests/browser/live-weather.test.mjs).
It is the shader's rule written once more on purpose: the browser test
pins the two to each other.

===========================================================================
*/
import { CULL_COMMANDS } from "./cull-commands.mjs";

const f = Math.fround;
export const REFERENCE_CAPACITY = [ 8192, 16384, 8192 ];
const KIND = [ 0, 2, 3 ];

/*
================
hash

The shader's integer hash (weather-particles.ts hash).
================
*/
export function hash( value ) {
	let h = (Math.imul( value >>> 0, 747796405 ) + 2891336453) >>> 0;
	h = Math.imul( ((h >>> ((h >>> 28) + 4)) ^ h) >>> 0, 277803737 ) >>> 0;
	return ((h >>> 22) ^ h) >>> 0;
}

/*
================
crossed
================
*/
function crossed( frame, ticks, bit ) {
	for ( let i = 1; i <= Math.min( ticks, 32 ); i++ ) if ( ((frame + i) >>> 0) & bit ) return true;
	return false;
}

/*
================
createWeatherReference

A fake geometry owner whose weather commands run the model. overwrites
counts spawns written over a slot whose drop was alive.
================
*/
export function createWeatherReference() {
	const regions = REFERENCE_CAPACITY.map( capacity => ({
		words: new Uint32Array( capacity * 8 ),
		floats: null,
		visible: []
	}) );
	for ( const region of regions ) region.floats = new Float32Array( region.words.buffer );
	const state = { overwrites: 0, steps: 0, spawns: 0, uploads: [], releases: 0, refuse: false };
	/*
	================
	advance
	================
	*/
	function advance( region, index, slot, step ) {
		const { words, floats } = regions[region], at = slot * 8, kind = KIND[region];
		let flags = words[at + 6];
		if ( !(flags & 1) ) return false;
		const born = (words[at + 7] - step.serial) | 0;
		if ( born > 0 ) return false;
		const dt = f( step.motion[0] );
		const sx = born === 0 ? 0 : f( step.motion[1] ), sz = born === 0 ? 0 : f( step.motion[2] );
		let x = f( floats[at] + sx ), y = floats[at + 1], z = f( floats[at + 2] + sz );
		const age = (words[at + 4] + step.elapsedMs) >>> 0, frame = words[at + 5], ticks = step.ticks;
		const ref = step.reference.map( f );
		if ( kind === 0 ) {
			const t = f( f( age + 10000 ) / 1000 );
			y = f( floats[at + 3] - f( f( t * t ) * f( 4.900000095367432 ) ) );
		} else {
			y = f( y - f( dt * (kind === 2 ? 12 : 10) ) );
			if ( crossed( frame, ticks, 4 ) ) {
				if ( f( x - ref[0] ) > 100 ) x = f( x - 200 );
				else if ( f( x - ref[0] ) < -100 ) x = f( x + 200 );
				if ( f( z - ref[2] ) > 100 ) z = f( z - 200 );
				else if ( f( z - ref[2] ) < -100 ) z = f( z + 200 );
			}
		}
		if ( y < ref[3] ) {
			words[at + 6] = 0;
			return false;
		}
		const m = step.matrix;
		const cw = m[3] * x + m[7] * y + m[11] * z + m[15],
			cx = m[0] * x + m[4] * y + m[8] * z + m[12],
			cy = m[1] * x + m[5] * y + m[9] * z + m[13],
			cz = m[2] * x + m[6] * y + m[10] * z + m[14];
		const visible = cw > 0 && Math.abs( cx ) <= cw && Math.abs( cy ) <= cw && cz >= 0 && cz <= cw;
		flags &= ~8;
		if ( visible ) {
			flags |= 8;
			if ( kind >= 2 ) {
				x = f( x + f( (flags & 2 ? 2 : -2) * dt ) );
				z = f( z + f( (flags & 4 ? 2 : -2) * dt ) );
				if ( crossed( frame, ticks, 16 ) ) {
					const h = hash( (slot ^ hash( (Math.imul( step.seed, 3 ) + region) >>> 0 )) >>> 0 );
					if ( h & 1 ) flags ^= 2;
					if ( h & 2 ) flags ^= 4;
				}
			}
		}
		floats[at] = x;
		floats[at + 1] = y;
		floats[at + 2] = z;
		words[at + 4] = age;
		words[at + 5] = (frame + ticks) >>> 0;
		words[at + 6] = flags;
		return visible;
	}
	const commands = {
		...CULL_COMMANDS,
		upload( mesh, image ) {
			state.uploads.push( { mesh, image } );
			return { id: state.uploads.length, image };
		},
		release() {
			state.releases++;
		},
		weatherSpawn( region, slot, records ) {
			const { words } = regions[region];
			for ( let i = 0; i < records.length / 8; i++ ) {
				if ( words[(slot + i) * 8 + 6] & 1 ) state.overwrites++;
				state.spawns++;
			}
			words.set( records, slot * 8 );
		},
		weatherCorners() {},
		weatherStep( step ) {
			if ( state.refuse ) return false;
			state.steps++;
			for ( let region = 0; region < regions.length; region++ ) {
				const window = step.windows[region], capacity = REFERENCE_CAPACITY[region], visible = [];
				for ( let k = 0; k < window.live; k++ ) {
					const slot = (window.tail + k) % capacity;
					if ( advance( region, k, slot, step ) ) visible.push( slot );
				}
				regions[region].visible = visible;
			}
			return true;
		}
	};
	return {
		commands,
		state,
		/*
		================
		alive

		Live drops of a region (all regions without one).
		================
		*/
		alive( region ) {
			let count = 0;
			for ( const [index, item] of regions.entries() ) {
				if ( region !== undefined && index !== region ) continue;
				for ( let i = 6; i < item.words.length; i += 8 ) if ( item.words[i] & 1 ) count++;
			}
			return count;
		},
		visible( region ) {
			return regions[region].visible;
		},
		drop( region, slot ) {
			const { words, floats } = regions[region], at = slot * 8;
			return {
				x: floats[at],
				y: floats[at + 1],
				z: floats[at + 2],
				age: words[at + 4],
				frame: words[at + 5],
				flags: words[at + 6]
			};
		},
		regions
	};
}
