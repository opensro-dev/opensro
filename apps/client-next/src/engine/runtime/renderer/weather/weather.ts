/*
===========================================================================

weather.ts - the native weather manager: emission, thunder, and the drops

Emits rain and snow the way the native manager does (8CE9C0/8D1910: the
shared remainder, the player-relative spawn, the shared presentation RNG
draws in their native order) and hands every new drop to the resident GPU
simulation (device/weather-particles.ts), which steps, culls, drifts and
draws them by the native rules. Port-only, not native (#4692): the drops'
per-frame state lives on the GPU, so the CPU's work a frame is constant
plus one record a new drop, never a pass over the live drops. What the GPU
changes (drift flips from a per-drop hash, frame bits on fixed ticks) is
described there.

Native rain never grounds its drops (#4108, the 8CE9C0 census on the
v1.150 client: no weather function calls any height or raycast
function; SampleHeight serves nav movement only). A drop spawns at
the player position plus a random offset and dies by the below-eye
cull, the way the spawned native effect object dies by its own
lifetime.

Slots: each kind's region is a ring the CPU fills in spawn order. The CPU
never reads a drop back; it keeps, per region, the spawn batches (one a
step) whose drops may still be alive and an upper bound on each batch's
height: a drop only falls (snow by its kind's speed times the summed dt,
rain by the native law of its age), and nothing raises it. A batch whose
bound is below the death height is dead on the GPU by the step that
proves it, so its slots leave the live window and may be reused. A spawn
that finds its region full of possibly live drops is dropped and counted
(stats().overflow), never written over a live one.

===========================================================================
*/
import { initialThunder, advanceThunder } from "@/engine/foundation/rendering/thunder";
import type { SoundEvent } from "@/engine/contracts/audio";
import type { WeatherOptions } from "@/engine/foundation/gameplay/weather";
import { advanceWeatherAmount, initialWeatherAmount } from "@/engine/foundation/gameplay/weather";
import type { PresentationRandom } from "@/engine/contracts/presentation-random";
import type { GeometryCommands, GeometryDraw, ImageDraw, WeatherStep } from "../internal/gpu-contract";
import type { WorldCamera } from "@/engine/contracts/scene";
import { hypot2, hypot3 } from "@/engine/foundation/math/hypot";
import { regionOffsetXZ } from "@/engine/foundation/world/world-space";
import {
	PARTICLE_ALIVE,
	PARTICLE_BIRTH_WORD,
	PARTICLE_WORDS,
	WEATHER_CAPACITY,
	WEATHER_REGIONS
} from "../device/weather-particles";

// The particle kinds (native factories) and each one's region: rain
// (CWRain), snow (CWSnow) and rare snow (CWSnow2); kind 1 has no factory.
const KIND_REGION: readonly number[] = [ 0, -1, 1, 2 ];
const REGION_TEXTURE = [ "rain1", "snow1", "snow2" ] as const;
// Snow's fall speed a region (units a second); rain falls by its age.
const REGION_FALL = [ 0, 12, 10 ];
// Each region's corner uvs (three u, v pairs): the rain streak's, the flake's.
const REGION_UVS: readonly (readonly number[])[] = [
	[ .5, 0, .7, 1, .3, 1 ],
	[ .5, 0, 0, 1, 1, 1 ],
	[ .5, 0, 0, 1, 1, 1 ]
];
const RAIN_GRAVITY = 4.900000095367432;
// World units a batch's height bound keeps above its drops: snow
// subtracts each step's fall in float32 on the GPU and in float64 here, so
// the rounding can add up; rain's height is a closed form of its age.
const SNOW_BOUND_MARGIN = 32;
const RAIN_BOUND_MARGIN = 1;
// The rate the drops' frame counters tick (port-only, inferred): the
// native bits were tuned for a 60 Hz frame (vsync), so a counter ticks
// at that rate whatever the render rate.
export const WEATHER_TICK_HZ = 60;
// The nine tiles every drop draws at (native weather instances, 50 apart).
const TILE_SHIFTS = [
	[ 0, 0 ],
	[ 50, 0 ],
	[ 50, -50 ],
	[ 0, -50 ],
	[ -50, -50 ],
	[ -50, 0 ],
	[ -50, 50 ],
	[ 0, 50 ],
	[ 50, 50 ]
] as const;
// The most drops one frame may emit (a stalled frame emits at most three
// seconds' worth, far below this).
const MAX_FRAME_EMISSION = 32768;
// Steps that may wait for one prepare (update without prepare).
const MAX_QUEUED_STEPS = 4;

/*
================
Region

One kind's ring: spawn and death counts since the last clear (the live
window is [dead, spawned)), the batches that may be alive (a ring of
their end counts, height bounds and the clock at their birth), the
records waiting for the GPU, and the draw once its texture is resident.
================
*/
type Region = {
	spawned: number;
	dead: number;
	overflow: number;
	batchEnd: Float64Array;
	batchTop: Float64Array;
	batchClock: Float64Array;
	batchFirst: number;
	batchCount: number;
	pending: Uint32Array;
	pendingCount: number;
	// The pending records start at this spawn count.
	pendingFrom: number;
	draw: GeometryDraw | null;
	image: ImageDraw | null;
};

/*
================
createRegion
================
*/
function createRegion( capacity: number ): Region {
	return {
		spawned: 0,
		dead: 0,
		overflow: 0,
		batchEnd: new Float64Array( capacity ),
		batchTop: new Float64Array( capacity ),
		batchClock: new Float64Array( capacity ),
		batchFirst: 0,
		batchCount: 0,
		pending: new Uint32Array( 64 * PARTICLE_WORDS ),
		pendingCount: 0,
		pendingFrom: 0,
		draw: null,
		image: null
	};
}

/*
================
weatherDrawGeometry

A region's draw: a placeholder stream (the vertex stage reads the pool),
an index a corner for the whole region, and the nine tiles.
================
*/
function weatherDrawGeometry( region: number ) {
	const instances = new Float32Array( TILE_SHIFTS.length * 16 );
	for ( let i = 0; i < TILE_SHIFTS.length; i++ ) {
		instances.set( [ 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, TILE_SHIFTS[i]![0], 0, TILE_SHIFTS[i]![1], 1 ], i * 16 );
	}
	return {
		world: true,
		positions: new Float32Array( 3 ),
		indices: Uint32Array.from( { length: WEATHER_CAPACITY[region]! * 3 }, ( _, i ) => i ),
		transform: Float32Array.from( [ 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 ] ),
		instances,
		weather: { region, uvs: REGION_UVS[region]! },
		material: {
			color: [ 1, 1, 1, 1 ] as [number, number, number, number],
			alphaCutoff: 0,
			blend: true,
			doubleSided: true,
			unlit: true,
			fogDisabled: true
		}
	};
}

// The weather owner retains emission state and the GPU regions. The manager's
// shared remainder and spawn RNG follow 8CE9C0/8D1910.
export function createWeather( random?: PresentationRandom, sound?: ( event: SoundEvent ) => void ) {
	let options: WeatherOptions | null = null,
		amount = initialWeatherAmount(),
		remainder = 0,
		last: number | null = null,
		origin: number | null = null;
	let thunder = initialThunder(), soundSequence = 0, overlayVisible = false;
	// The summed step time the drops have fallen by (seconds, and whole ms
	// for rain's age), the fixed ticks, and the step serials.
	let fallClock = 0, ageClock = 0, tickTime = 0, ticksDone = 0, serial = 1, seed = 0;
	// Region rebases not yet stepped: a paused frame still crosses regions,
	// and the next step moves the drops by every crossing since the last.
	let heldShiftX = 0, heldShiftZ = 0, refused = 0, lastDeath: number | null = null;
	const regions: Region[] = WEATHER_CAPACITY.map( createRegion );
	// Steps made by update and sent by the next prepare, in order.
	const queued: Omit<WeatherStep, "matrix">[] = [];
	const record = new Uint32Array( PARTICLE_WORDS ), recordFloats = new Float32Array( record.buffer );
	const corners = new Float32Array( 12 );
	const path = ( region: number ) => "/assets/images/Map_extracted/weather/" + REGION_TEXTURE[region] + ".png";
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
	/*
	================
	queueSpawn

	One drop record into its region's ring, or a counted overflow when
	every slot may hold a live drop.
	================
	*/
	function queueSpawn( region: Region, capacity: number, top: number ) {
		if ( region.spawned - region.dead >= capacity ) {
			region.overflow++;
			return;
		}
		if ( region.pendingCount === 0 ) region.pendingFrom = region.spawned;
		const need = (region.pendingCount + 1) * PARTICLE_WORDS;
		if ( need > region.pending.length ) {
			const grown = new Uint32Array( Math.max( need, region.pending.length * 2 ) );
			grown.set( region.pending );
			region.pending = grown;
		}
		region.pending.set( record, region.pendingCount * PARTICLE_WORDS );
		region.pendingCount++;
		region.spawned++;
		// One batch a step: its end, its highest drop, the clock at birth.
		const lastBatch = (region.batchFirst + region.batchCount - 1) % capacity;
		if ( region.batchCount && region.batchClock[lastBatch] === (region === regions[0] ? ageClock : fallClock) ) {
			region.batchEnd[lastBatch] = region.spawned;
			region.batchTop[lastBatch] = Math.max( region.batchTop[lastBatch]!, top );
			return;
		}
		const at = (region.batchFirst + region.batchCount) % capacity;
		region.batchEnd[at] = region.spawned;
		region.batchTop[at] = top;
		region.batchClock[at] = region === regions[0] ? ageClock : fallClock;
		region.batchCount++;
	}
	/*
	================
	retire

	Drops the batches the last step killed: their highest drop is below
	its death height. Batches die oldest first or not at all here; a
	younger batch dead before an older one waits (the window stays a
	superset of the live drops).
	================
	*/
	function retire( index: number, deathHeight: number ) {
		const region = regions[index]!, capacity = WEATHER_CAPACITY[index]!;
		while ( region.batchCount ) {
			const at = region.batchFirst, top = region.batchTop[at]!;
			let bound: number;
			if ( index === 0 ) {
				const t = (ageClock - region.batchClock[at]! + 10000) / 1000;
				bound = top - t * t * RAIN_GRAVITY;
			} else {
				bound = top - REGION_FALL[index]! * (fallClock - region.batchClock[at]!);
			}
			if ( bound + (index === 0 ? RAIN_BOUND_MARGIN : SNOW_BOUND_MARGIN) >= deathHeight ) return;
			region.dead = region.batchEnd[at]!;
			region.batchFirst = (at + 1) % capacity;
			region.batchCount--;
		}
	}
	/*
	================
	flushRegion

	A region's queued spawns into its ring slots, split where it wraps.
	================
	*/
	function flushRegion( gpu: GeometryCommands, index: number, region: Region ) {
		if ( !region.pendingCount ) return;
		const capacity = WEATHER_CAPACITY[index]!, slot = region.pendingFrom % capacity;
		const first = Math.min( region.pendingCount, capacity - slot );
		gpu.weatherSpawn( index, slot, region.pending.subarray( 0, first * PARTICLE_WORDS ) );
		if ( first < region.pendingCount ) {
			gpu.weatherSpawn(
				index,
				0,
				region.pending.subarray( first * PARTICLE_WORDS, region.pendingCount * PARTICLE_WORDS )
			);
		}
		region.pendingCount = 0;
	}
	/*
	================
	resetRegions

	Every region empty: a clear (the drops end with the weather) or a lost
	device (they ended with its pool). Slots are not zeroed: nothing past
	an empty window is stepped or drawn, and a reused slot gets a record.
	================
	*/
	function resetRegions() {
		lastDeath = null;
		for ( const region of regions ) {
			region.spawned = region.dead = 0;
			region.batchFirst = region.batchCount = 0;
			region.pendingCount = 0;
		}
		queued.length = 0;
	}
	function clear() {
		overlayVisible = false;
		if ( options?.eventRain ) eventSound( false );
		thunder = initialThunder();
		resetRegions();
		amount = initialWeatherAmount();
		remainder = 0;
		last = null;
		origin = null;
		heldShiftX = heldShiftZ = 0;
	}
	const live = () => regions.some( region => region.spawned > region.dead );
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
			return options && (options.mode !== 1 || options.eventRain) || live() ?
				regions.map( ( _, region ) => path( region ) ) :
				[];
		},
		update(
			camera: WorldCamera,
			seconds: number,
			region: number,
			enabled: boolean,
			reference: readonly [number, number, number] = camera.target
		) {
			if ( !Number.isFinite( seconds ) ) throw Error( "Invalid weather time" );
			const elapsedMs = last === null ?
				0 :
				Math.min( 3000, Math.max( 0, Math.floor( seconds * 1000 ) - Math.floor( last * 1000 ) ) );
			const dt = last === null ? 0 : Math.fround( Math.min( 3, Math.max( 0, seconds - last ) ) );
			last = seconds;
			if ( origin !== null && origin !== region ) {
				const [dx, dz] = regionOffsetXZ( region, origin );
				heldShiftX += dx;
				heldShiftZ += dz;
			}
			origin = region;
			overlayVisible = enabled && !!options;
			// Paused (no weather, indoors, the frontend): the drops neither
			// step nor draw, as the native update returned before them.
			if ( !overlayVisible || !options ) return;
			if ( !random && (options.mode !== 1 || options.eventRain) ) {
				throw Error( "Weather requires shared presentation RNG" );
			}
			// Batches the last step killed free their slots before this frame
			// spawns: their bound fell below that step's death height while
			// they were in its window, so the GPU has already cleared them.
			if ( lastDeath !== null ) {
				for ( let index = 0; index < WEATHER_REGIONS; index++ ) retire( index, lastDeath );
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
				if ( count > MAX_FRAME_EMISSION ) throw Error( "Weather frame exceeds emission budget" );
				for ( let i = 0; i < count; i++ ) {
					// 8CE9C0 uses the player position, not the camera eye. CWSnow2 (10/s)
					// is the one-in-four factory F0886C; CWSnow (12/s) uses F0877C.
					const kind = mode === 2 ? 0 : random!.range( 0, 4 ) === 0 ? 3 : 2;
					const y = Math.fround( reference[1] + random!.range( 500, 1500 ) / 10 ),
						extent = kind === 0 ? 600 : 1000;
					const x = Math.fround( reference[0] + random!.range( -extent, extent ) / 10 ),
						z = Math.fround( reference[2] + random!.range( -extent, extent ) / 10 );
					const start = kind === 0 ? Math.fround( y + 100 * RAIN_GRAVITY ) : y;
					recordFloats[0] = x;
					recordFloats[1] = y;
					recordFloats[2] = z;
					recordFloats[3] = start;
					record[4] = 0;
					record[5] = 0;
					record[6] = PARTICLE_ALIVE;
					record[PARTICLE_BIRTH_WORD] = serial;
					const index = KIND_REGION[kind]!;
					queueSpawn( regions[index]!, WEATHER_CAPACITY[index]!, start );
				}
			}
			// The step every drop takes this frame, newborn ones included
			// (the native update stepped the drops it had just spawned).
			fallClock += dt;
			ageClock += elapsedMs;
			tickTime += dt;
			const ticks = Math.floor( tickTime * WEATHER_TICK_HZ ) - ticksDone;
			ticksDone += ticks;
			const deathHeight = camera.eye[1] - 500;
			const shiftX = heldShiftX, shiftZ = heldShiftZ;
			heldShiftX = heldShiftZ = 0;
			lastDeath = deathHeight;
			// Updates without a prepare between them: the oldest step cannot be
			// sent, so the drops it should have moved are forgotten too.
			if ( queued.length === MAX_QUEUED_STEPS ) {
				resetRegions();
				refused++;
			}
			queued.push( {
				reference: [ reference[0], reference[1], reference[2], deathHeight ],
				motion: [ dt, shiftX, shiftZ ],
				elapsedMs,
				ticks,
				seed: seed++,
				serial: serial++,
				windows: regions.map( ( item, index ) => ({
					tail: item.dead % WEATHER_CAPACITY[index]!,
					live: item.spawned - item.dead
				}) )
			} );
		},
		prepare(
			gpu: GeometryCommands,
			images: ReadonlyMap<string, ImageDraw>,
			camera: WorldCamera,
			matrix: Float32Array
		) {
			for ( let index = 0; index < WEATHER_REGIONS; index++ ) flushRegion( gpu, index, regions[index]! );
			if ( !overlayVisible || !live() ) {
				queued.length = 0;
				return [];
			}
			const fx = camera.target[0] - camera.eye[0],
				fy = camera.target[1] - camera.eye[1],
				fz = camera.target[2] - camera.eye[2],
				length = hypot3( fx, fy, fz ) || 1;
			const cameraUp = camera.up ?? [ 0, 1, 0 ],
				rx = cameraUp[1] * fz - cameraUp[2] * fy,
				ry = cameraUp[2] * fx - cameraUp[0] * fz,
				rz = cameraUp[0] * fy - cameraUp[1] * fx,
				rl = hypot3( rx, ry, rz ) || 1;
			const heading = hypot2( fx, fz ) || 1, rainRight = [ fz / heading, 0, -fx / heading ];
			const right = [ rx / rl, ry / rl, rz / rl ],
				up = [
					(fy * right[2]! - fz * right[1]!) / length,
					(fz * right[0]! - fx * right[2]!) / length,
					(fx * right[1]! - fy * right[0]!) / length
				];
			// The corners are the same for every drop of a kind: the camera
			// axes and the rain's right are this frame's constants.
			corners.fill( 0 );
			corners[1] = 10;
			corners[4] = rainRight[0]! * .3;
			corners[6] = rainRight[2]! * .3;
			corners[8] = -rainRight[0]! * .3;
			corners[10] = -rainRight[2]! * .3;
			gpu.weatherCorners( 0, corners );
			for ( let axis = 0; axis < 3; axis++ ) {
				corners[axis] = up[axis]! * .5;
				corners[4 + axis] = (right[axis]! - up[axis]!) * .5;
				corners[8 + axis] = (-right[axis]! - up[axis]!) * .5;
			}
			corners[3] = corners[7] = corners[11] = 0;
			gpu.weatherCorners( 1, corners );
			gpu.weatherCorners( 2, corners );
			// Each update's step once, whichever passes then draw the drops
			// (the main view, the water mirror): they read the same pool.
			// A refused step means the pool missed time the CPU clock counted:
			// forget every drop (their slots leave the windows, so nothing
			// steps or draws them) rather than reuse a slot early.
			for ( const step of queued ) {
				if ( !gpu.weatherStep( { ...step, matrix } ) ) {
					resetRegions();
					refused++;
					break;
				}
			}
			queued.length = 0;
			const output: GeometryDraw[] = [];
			for ( let index = 0; index < WEATHER_REGIONS; index++ ) {
				const region = regions[index]!, image = images.get( path( index ) );
				// Native 8CF730 traverses typed render lists; texture availability
				// must not suppress the step, only the drawing.
				if ( region.spawned === region.dead || !image ) continue;
				if ( !region.draw || region.image !== image ) {
					if ( region.draw ) gpu.release( region.draw );
					region.draw = gpu.upload( weatherDrawGeometry( index ), image );
					region.image = image;
				}
				output.push( region.draw );
			}
			return output;
		},
		/*
		================
		stats

		The CPU's view of the drops: each region's live window (an upper
		bound on its live drops), spawns dropped for a full ring, and the
		emission state.
		================
		*/
		stats() {
			return {
				windows: regions.map( region => region.spawned - region.dead ),
				spawned: regions.map( region => region.spawned ),
				overflow: regions.reduce( ( sum, region ) => sum + region.overflow, 0 ),
				refused,
				amount: amount.value,
				remainder
			};
		},
		/*
		================
		invalidate

		A lost device: the drops died with its pool, so the regions start
		empty and their draws are made again on the new device.
		================
		*/
		invalidate() {
			resetRegions();
			for ( const region of regions ) {
				region.draw = null;
				region.image = null;
			}
		},
		dispose( gpu: GeometryCommands | null ) {
			if ( gpu ) {
				for ( const region of regions ) if ( region.draw ) gpu.release( region.draw );
			}
			clear();
			for ( const region of regions ) {
				region.draw = null;
				region.image = null;
			}
			options = null;
		}
	};
}
