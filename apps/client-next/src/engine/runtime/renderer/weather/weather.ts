import { initialThunder, advanceThunder } from "@/engine/foundation/rendering/thunder";
import type { SoundEvent } from "@/engine/contracts/audio";
import type { WeatherOptions } from "@/engine/foundation/gameplay/weather";
import { advanceWeatherAmount, initialWeatherAmount } from "@/engine/foundation/gameplay/weather";
import type { PresentationRandom } from "@/engine/contracts/presentation-random";
import type { GeometryCommands, GeometryDraw, ImageDraw } from "../internal/gpu-contract";
import type { WorldCamera } from "@/engine/contracts/scene";
type Particle = {
	kind: number;
	x: number;
	y: number;
	z: number;
	start: number;
	ground: number | null;
	age: number;
	frame: number;
	dx: boolean;
	dz: boolean;
};
type Batch = { draw: GeometryDraw; capacity: number; positions: Float32Array; colors: Float32Array; image: ImageDraw; };
// The weather owner retains particles and GPU batches. The manager's shared
// remainder, frame-counter bits and draw-gated RNG follow 8CE9C0/8D1910.
export function createWeather( random?: PresentationRandom, sound?: ( event: SoundEvent ) => void ) {
	let options: WeatherOptions | null = null,
		amount = initialWeatherAmount(),
		remainder = 0,
		last: number | null = null,
		origin: number | null = null;
	let thunder = initialThunder(), soundSequence = 0, overlayVisible = false, drawDelta = 0;
	let particles: Particle[] = [];
	const batches = new Map<number, Batch>();
	const names = [ "rain1", "rain2", "snow1", "snow2" ] as const;
	const path = ( kind: number ) => "/assets/images/Map_extracted/weather/" + names[kind] + ".png";
	function eventSound( active: boolean ) {
		sound?.( {
			id: "weather-event-rain",
			path: "/assets/audio/sfx/prim/snd/etc/rain1.wav",
			gain: 1,
			x: 0,
			y: 0,
			z: 0,
			expires: (last ?? 0) + 5,
			spatial: false,
			loop: true,
			stop: !active
		} );
	}
	function clear() {
		overlayVisible = false;
		if ( options?.eventRain ) eventSound( false );
		thunder = initialThunder();
		particles = [];
		amount = initialWeatherAmount();
		remainder = 0;
		last = null;
		origin = null;
	}
	return {
		set( value: WeatherOptions | null ) {
			if (
				value &&
				(![ 1, 2, 3 ].includes( value.mode ) || !Number.isInteger( value.amount ) || value.amount < 0 ||
					value.amount > 255)
			) throw Error( "Invalid weather options" );
			if ( !value && options ) clear();
			else if ( !!value?.eventRain !== !!options?.eventRain ) eventSound( !!value?.eventRain );
			options = value ? { ...value } : null;
		},
		overlay() {
			return !overlayVisible || thunder.complete ? null : thunder.color.map( v => Math.trunc( v ) / 255 );
		},
		paths() {
			return options && (options.mode !== 1 || options.eventRain) || particles.length ?
				names.map( ( _, kind ) => path( kind ) ) :
				[];
		},
		update(
			camera: WorldCamera,
			seconds: number,
			region: number,
			enabled: boolean,
			ground: ( x: number, y: number, z: number ) => number | null,
			reference: readonly [number, number, number] = camera.target
		) {
			if ( !Number.isFinite( seconds ) ) throw Error( "Invalid weather time" );
			const elapsedMs = last === null ?
				0 :
				Math.min( 3000, Math.max( 0, Math.floor( seconds * 1000 ) - Math.floor( last * 1000 ) ) );
			const dt = last === null ? 0 : Math.fround( Math.min( 3, Math.max( 0, seconds - last ) ) );
			last = seconds;
			drawDelta = dt;
			if ( origin !== null && origin !== region ) {
				const dx = ((origin & 255) - (region & 255)) * 1920, dz = ((origin >>> 8) - (region >>> 8)) * 1920;
				for ( const p of particles ) {
					p.x += dx;
					p.z += dz;
				}
			}
			origin = region;
			overlayVisible = enabled && !!options;
			if ( !overlayVisible || !options ) return;
			if ( !random && (options.mode !== 1 || options.eventRain) ) {
				throw Error( "Weather requires shared presentation RNG" );
			}
			const flash = advanceThunder( thunder, seconds, dt, options.mode === 2, ( a, b ) => random!.range( a, b ) );
			thunder = flash.state;
			if ( flash.sound !== null ) {
				sound?.( {
					id: "weather:" + ++soundSequence,
					path: "/assets/audio/sfx/prim/snd/etc/lightning" + (flash.sound + 1) + ".wav",
					gain: 1,
					x: 0,
					y: 0,
					z: 0,
					expires: seconds + 5,
					spatial: false
				} );
			}
			amount = advanceWeatherAmount( amount, options.eventRain ? 10 : options.amount, dt );
			for ( const mode of [ 2, 3 ] ) {
				if ( !(mode === 2 ? (options.mode === 2 || options.eventRain) : options.mode === 3) ) continue;
				const emission = Math.fround( remainder + amount.value * dt * (mode === 2 ? 2 : .5) ),
					count = Math.trunc( emission );
				remainder = Math.fround( emission - count );
				if ( count > 32768 ) throw Error( "Weather frame exceeds emission budget" );
				for ( let i = 0; i < count; i++ ) {
					// 8CE9C0 uses the player position, not the camera eye. CWSnow2 (10/s)
					// is the one-in-four factory F0886C; CWSnow (12/s) uses F0877C.
					const kind = mode === 2 ? 0 : random!.range( 0, 4 ) === 0 ? 3 : 2;
					const y = Math.fround( reference[1] + random!.range( 500, 1500 ) / 10 ),
						extent = kind === 0 ? 600 : 1000;
					const x = Math.fround( reference[0] + random!.range( -extent, extent ) / 10 ),
						z = Math.fround( reference[2] + random!.range( -extent, extent ) / 10 );
					particles.push( {
						kind,
						x,
						y,
						z,
						start: kind === 0 ? Math.fround( y + 100 * 4.900000095367432 ) : y,
						ground: kind === 0 ? ground( x, y, z ) : null,
						age: 0,
						frame: 0,
						dx: false,
						dz: false
					} );
				}
			}
			const next: Particle[] = [];
			for ( const p of particles ) {
				p.age = (p.age + elapsedMs) >>> 0;
				p.frame = (p.frame + 1) >>> 0;
				if ( p.kind === 1 ) {
					if ( p.age <= 1200 ) next.push( p );
					continue;
				}
				if ( p.kind === 0 ) {
					const t = Math.fround( (p.age + 10000) / 1000 );
					p.y = Math.fround( p.start - t * t * 4.900000095367432 );
				} else {
					p.y = Math.fround( p.y - dt * (p.kind === 2 ? 12 : 10) );
					if ( p.frame & 4 ) {
						if ( p.x - reference[0] > 100 ) p.x = Math.fround( p.x - 200 );
						else if ( p.x - reference[0] < -100 ) p.x = Math.fround( p.x + 200 );
						if ( p.z - reference[2] > 100 ) p.z = Math.fround( p.z - 200 );
						else if ( p.z - reference[2] < -100 ) p.z = Math.fround( p.z + 200 );
					}
				}
				if ( p.y < camera.eye[1] - 500 ) continue;
				if ( p.kind === 0 && p.ground !== null && p.y < p.ground ) {
					next.push( { ...p, kind: 1, y: Math.fround( p.ground + .5 ), age: 0, frame: 0 } );
					continue;
				}
				next.push( p );
			}
			particles = next;
		},
		prepare(
			gpu: GeometryCommands,
			images: ReadonlyMap<string, ImageDraw>,
			camera: WorldCamera,
			matrix: Float32Array
		) {
			if ( !overlayVisible ) return [];
			const dt = drawDelta;
			const fx = camera.target[0] - camera.eye[0],
				fy = camera.target[1] - camera.eye[1],
				fz = camera.target[2] - camera.eye[2],
				length = Math.hypot( fx, fy, fz ) || 1;
			const cameraUp = camera.up ?? [ 0, 1, 0 ],
				rx = cameraUp[1] * fz - cameraUp[2] * fy,
				ry = cameraUp[2] * fx - cameraUp[0] * fz,
				rz = cameraUp[0] * fy - cameraUp[1] * fx,
				rl = Math.hypot( rx, ry, rz ) || 1;
			const heading = Math.hypot( fx, fz ) || 1, rainRight = [ fz / heading, 0, -fx / heading ];
			const right = [ rx / rl, ry / rl, rz / rl ],
				up = [
					(fy * right[2]! - fz * right[1]!) / length,
					(fz * right[0]! - fx * right[2]!) / length,
					(fx * right[1]! - fy * right[0]!) / length
				];
			const visible = ( p: Particle ) => {
				const w = matrix[3]! * p.x + matrix[7]! * p.y + matrix[11]! * p.z + matrix[15]!,
					x = matrix[0]! * p.x + matrix[4]! * p.y + matrix[8]! * p.z + matrix[12]!,
					y = matrix[1]! * p.x + matrix[5]! * p.y + matrix[9]! * p.z + matrix[13]!,
					z = matrix[2]! * p.x + matrix[6]! * p.y + matrix[10]! * p.z + matrix[14]!;
				return w > 0 && Math.abs( x ) <= w && Math.abs( y ) <= w && z >= 0 && z <= w;
			};
			const output: GeometryDraw[] = [];
			for ( let kind = 0; kind < 4; kind++ ) {
				const rows = particles.filter( p => p.kind === kind && visible( p ) );
				// Native 8CF730 traverses typed render lists; texture availability must
				// not suppress the corresponding visible particle callbacks.
				if ( kind >= 2 ) {
					for ( const p of rows ) {
						p.x = Math.fround( p.x + (p.dx ? 2 : -2) * dt );
						p.z = Math.fround( p.z + (p.dz ? 2 : -2) * dt );
						if ( p.frame & 16 ) {
							if ( random!.range( 0, 2 ) ) p.dx = !p.dx;
							if ( random!.range( 0, 2 ) ) p.dz = !p.dz;
						}
					}
				}
				const image = images.get( path( kind ) );
				if ( !rows.length || !image ) continue;
				const vertices = kind === 1 ? 6 : 3;
				let batch = batches.get( kind );
				if ( !batch || batch.capacity < rows.length || batch.image !== image ) {
					if ( batch ) gpu.release( batch.draw );
					const capacity = 2 ** Math.ceil( Math.log2( Math.max( 16, rows.length ) ) ),
						positions = new Float32Array( capacity * vertices * 3 ),
						colors = new Float32Array( capacity * vertices * 4 ).fill( 1 ),
						uvs = new Float32Array( capacity * vertices * 2 );
					const uv = kind === 1 ?
						[ 0, 0, 1, 0, 1, 1, 0, 0, 1, 1, 0, 1 ] :
						kind === 0 ?
						[ .5, 0, .7, 1, .3, 1 ] :
						[ .5, 0, 0, 1, 1, 1 ];
					for ( let i = 0; i < capacity; i++ ) uvs.set( uv, i * vertices * 2 );
					const transform = Float32Array.from( [ 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 ] ),
						instances = new Float32Array( (kind === 1 ? 1 : 9) * 16 );
					for ( let i = 0; i < instances.length / 16; i++ ) {
						instances.set( transform, i * 16 );
						if ( kind !== 1 ) {
							const shifts = [
								[ 0, 0 ],
								[ 50, 0 ],
								[ 50, -50 ],
								[ 0, -50 ],
								[ -50, -50 ],
								[ -50, 0 ],
								[ -50, 50 ],
								[ 0, 50 ],
								[ 50, 50 ]
							];
							instances[i * 16 + 12] = shifts[i]![0]!;
							instances[i * 16 + 14] = shifts[i]![1]!;
						}
					}
					const draw = gpu.upload( {
						world: true,
						dynamicVertices: true,
						positions,
						colors,
						uvs,
						indices: Uint32Array.from( { length: capacity * vertices }, ( _, i ) => i ),
						transform,
						instances,
						material: {
							color: [ 1, 1, 1, 1 ],
							alphaCutoff: 0,
							blend: true,
							doubleSided: true,
							unlit: true,
							fogDisabled: true
						}
					}, image );
					batch = { draw, capacity, positions, colors, image };
					batches.set( kind, batch );
				}
				batch.positions.fill( 0 );
				batch.colors.fill( 1 );
				for ( let i = 0; i < rows.length; i++ ) {
					const p = rows[i]!;
					const scale = Math.fround( p.age / 300 ),
						corners = kind === 1 ?
							[ [ -scale, 0, scale ], [ scale, 0, scale ], [ scale, 0, -scale ], [ -scale, 0, scale ], [
								scale,
								0,
								-scale
							], [ -scale, 0, -scale ] ] :
							kind === 0 ?
							[ [ 0, 10, 0 ], [ rainRight[0]! * .3, 0, rainRight[2]! * .3 ], [
								-rainRight[0]! * .3,
								0,
								-rainRight[2]! * .3
							] ] :
							[
								up.map( v => v * .5 ),
								right.map( ( v, k ) => (v - up[k]!) * .5 ),
								right.map( ( v, k ) => (-v - up[k]!) * .5 )
							];
					for ( let j = 0; j < vertices; j++ ) {
						const c = corners[j]!;
						batch.positions.set( [ p.x + c[0]!, p.y + c[1]!, p.z + c[2]! ], (i * vertices + j) * 3 );
						if ( kind === 1 ) {
							batch.colors[(i * vertices + j) * 4 + 3] = (255 - Math.trunc( scale * 63 )) / 255;
						}
					}
				}
				gpu.updatePositions( batch.draw, batch.positions, batch.colors );
				output.push( batch.draw );
			}
			return output;
		},
		stats() {
			return { particles: particles.length, amount: amount.value, remainder };
		},
		invalidate() {
			batches.clear();
		},
		dispose( gpu: GeometryCommands | null ) {
			if ( gpu ) { for ( const batch of batches.values() ) gpu.release( batch.draw ); }
			batches.clear();
			clear();
			options = null;
		}
	};
}
