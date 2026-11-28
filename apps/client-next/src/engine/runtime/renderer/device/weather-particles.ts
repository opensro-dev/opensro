/*
===========================================================================

weather-particles.ts - resident GPU rain and snow simulation

Port-only, not native (#4692): the native weather manager (8CE9C0/8D1910)
steps every drop on the CPU, and the port did the same, rebuilding three
vertices a visible drop and uploading them every frame - the busy crowd's
largest world cost. Here the drops live in one resident storage buffer.
The CPU keeps only what is shared (emission and its shared-RNG spawn
draws, each step's view, reference, dt and region rebase, each region's
live window, weather.ts) and writes each new drop's 32-byte record once.
Two dispatches a step do the rest, over the live windows only:

- step: one thread a drop advances it by the native law (region rebase,
  age, rain's start - ((age + 10000) / 1000)^2 * 4.9 or snow's dt * 12 / 10
  fall, the wrap around the reference, the below-eye death), tests it
  against the view and moves the visible snow by its drift; each
  workgroup counts its visible drops;
- compact: each workgroup sums the counts of the blocks before it and
  writes its visible drops' slots into the region's list in order; the
  region's last block writes the list length into the draw's indirect
  arguments.

Order: thread k steps the k-th oldest live slot (the window starts at the
ring's tail), so the list keeps the CPU path's draw order, oldest drop
first, and blended drops overlap as before. The arguments are rewritten
whole by every step (no counter to reset), and the list is bounded by the
region's capacity. The weather draws read the list and the pool in place
(pipelines.ts vsWeather): no CPU geometry, no per-drop upload, no
readback, no copy.

Steps: every step weather.ts asks for runs exactly once, at the next
encode, however many passes then draw its lists. Steps queue
(MAX_PENDING_STEPS, one uniform slot each) when a frame prepares without
submitting; each new drop's record carries the serial of the step it is
born in, and a step skips drops born after it, so a drop never takes time
from before its birth however the steps and spawn writes interleave.

Frame bits: the native rules gate the wrap on frame bit 4 and the drift
flips on bit 16 of a per-drop frame counter, tuned for the native frame
rate. Port-only: the counter advances by fixed TICK_HZ ticks (weather.ts),
not by rendered frames, so a flake wraps and wanders the same at 30 or
500 fps. A step that crosses a tick with the bit set applies the rule
once; several fair flips compose to one fair flip.

The drift flips draw from a per-drop hash instead of the shared
presentation RNG: the native draws ran once per visible flake, a count
only the GPU knows. The odds stay native (one in two an axis). The shared
RNG is a local cosmetic generator (curves, orbs, star flicker); its other
consumers no longer depend on how many flakes the camera sees.

Buffer: three regions (rain, snow, rare snow), each REGION_ALIGN-aligned
so a draw binds its own (binding 17):
- header (WEATHER_HEADER_WORDS): three billboard corners (vec4 each),
  three corner uvs, the capacity and the draw's baseVertex;
- drops (PARTICLE_WORDS each): x, y, z, start (f32), age ms, frame
  count, flags (alive, drift x/z, visible), birth step serial;
- the visible list (one slot a drop), then the block counts.

===========================================================================
*/
import { DeviceDraw } from "./device-draw";
import type { Retire } from "./retirement";
import type { GpuTimingFrame, WeatherStep } from "../internal/gpu-contract";

// The regions' capacities: rain (CWRain), snow (CWSnow) and rare snow
// (CWSnow2). weather.ts never lets a spawn take a slot whose drop may be
// alive; these hold several times the steady state of the strongest
// native emission (live-weather tests). Multiples of WORKGROUP, at most
// WORKGROUP * MAX_REGION_BLOCKS.
export const WEATHER_REGIONS = 3;
export const WEATHER_CAPACITY: readonly number[] = [ 8192, 16384, 8192 ];
// The kind code each region steps (weather.ts particle kinds).
const REGION_KIND = [ 0, 2, 3 ];
export const PARTICLE_WORDS = 8;
export const PARTICLE_ALIVE = 1;
const PARTICLE_DRIFT_X = 2;
const PARTICLE_DRIFT_Z = 4;
const PARTICLE_VISIBLE = 8;
export const PARTICLE_BIRTH_WORD = 7;
export const WEATHER_HEADER_WORDS = 32;
// The header's capacity word; the draw's baseVertex follows it.
export const WEATHER_CAPACITY_WORD = 28;
const CORNER_FLOATS = 12;
const UV_FLOATS = 6;
const REGION_ALIGN = 256;
// drawIndexedIndirect's five words a region.
const ARG_WORDS = 5;
const ARG_BYTES = ARG_WORDS * 4;
const WORKGROUP = 64;
// The block counts one compact workgroup sums, WORKGROUP at a time.
const MAX_REGION_BLOCKS = 256;
// The frame bits (native 8D1910): wrap on bit 4, drift flips on bit 16.
const WRAP_BIT = 4;
const FLIP_BIT = 16;
// One step's uniform: matrix, reference + death height, motion, counts,
// a row a region, the windows and the workgroups a region.
const STEP_WORDS = 48;
const STEP_SLOT_BYTES = 256;
const MAX_PENDING_STEPS = 4;

for ( const capacity of WEATHER_CAPACITY ) {
	if ( capacity % WORKGROUP || capacity / WORKGROUP > MAX_REGION_BLOCKS ) throw Error( "Invalid weather capacity" );
}

const shader = /* wgsl */ `
struct Step {
	matrix: mat4x4f, reference: vec4f, motion: vec4f,
	counts: vec4u, regions: array<vec4u, ${WEATHER_REGIONS}>, windows: vec4u, groups: vec4u
}
@group(0) @binding(0) var<uniform> step: Step;
@group(0) @binding(1) var<storage, read_write> words: array<u32>;
@group(0) @binding(2) var<storage, read_write> args: array<u32>;
var<workgroup> counted: atomic<u32>;
var<workgroup> marks: array<u32, ${WORKGROUP}>;

struct Lane { region: u32, base: u32, capacity: u32, slot: u32, block: u32, live: bool, last: bool }

// The region a workgroup works in (a uniform answer: windows are whole
// workgroups), and the lane's drop: the age-th oldest live slot.
fn laneOf(group: u32, lane: u32) -> Lane {
	var first = group;
	var region = 0u;
	for (var r = 0u; r < ${WEATHER_REGIONS - 1}u; r++) {
		if (region == r && first >= step.groups[r]) { first -= step.groups[r]; region = r + 1u; }
	}
	let row = step.regions[region];
	let age = first * ${WORKGROUP}u + lane;
	return Lane(region, row.x, row.y, (row.w + age) % row.y, first, age < step.windows[region], first + 1u == step.groups[region]);
}

fn hash(value: u32) -> u32 {
	var h = value * 747796405u + 2891336453u;
	h = ((h >> ((h >> 28u) + 4u)) ^ h) * 277803737u;
	return (h >> 22u) ^ h;
}

// Whether a tick in (frame, frame + ticks] has bit set.
fn crossed(frame: u32, ticks: u32, bit: u32) -> bool {
	var hit = false;
	for (var i = 1u; i <= ${FLIP_BIT * 2}u; i++) {
		if (i <= ticks && ((frame + i) & bit) != 0u) { hit = true; }
	}
	return hit;
}

// One drop's step; true when it is alive and visible.
fn advance(at: u32, kind: u32, slot: u32, region: u32) -> bool {
	var flags = words[at + 6u];
	if ((flags & ${PARTICLE_ALIVE}u) == 0u) { return false; }
	// Born in a later step (its record was written before this one ran).
	let born = i32(words[at + ${PARTICLE_BIRTH_WORD}u] - step.counts.w);
	if (born > 0) { return false; }
	let dt = step.motion.x;
	// The region rebase moves the drops that existed before it; a drop born
	// in this step spawned in the new region's coordinates (native order).
	let shift = select(step.motion.yz, vec2f(0.0), born == 0);
	var x = bitcast<f32>(words[at]) + shift.x;
	var y = bitcast<f32>(words[at + 1u]);
	var z = bitcast<f32>(words[at + 2u]) + shift.y;
	let age = words[at + 4u] + step.counts.x;
	let frame = words[at + 5u];
	let ticks = step.counts.z;
	if (kind == 0u) {
		let t = f32(age + 10000u) / 1000.0;
		y = bitcast<f32>(words[at + 3u]) - t * t * 4.900000095367432;
	} else {
		y = y - dt * select(10.0, 12.0, kind == 2u);
		if (crossed(frame, ticks, ${WRAP_BIT}u)) {
			if (x - step.reference.x > 100.0) { x = x - 200.0; } else if (x - step.reference.x < -100.0) { x = x + 200.0; }
			if (z - step.reference.z > 100.0) { z = z - 200.0; } else if (z - step.reference.z < -100.0) { z = z + 200.0; }
		}
	}
	if (y < step.reference.w) {
		words[at + 6u] = 0u;
		return false;
	}
	let clip = step.matrix * vec4f(x, y, z, 1.0);
	let visible = clip.w > 0.0 && abs(clip.x) <= clip.w && abs(clip.y) <= clip.w && clip.z >= 0.0 && clip.z <= clip.w;
	flags = flags & ~${PARTICLE_VISIBLE}u;
	if (visible) {
		flags = flags | ${PARTICLE_VISIBLE}u;
		if (kind >= 2u) {
			x = x + select(-2.0, 2.0, (flags & ${PARTICLE_DRIFT_X}u) != 0u) * dt;
			z = z + select(-2.0, 2.0, (flags & ${PARTICLE_DRIFT_Z}u) != 0u) * dt;
			if (crossed(frame, ticks, ${FLIP_BIT}u)) {
				let h = hash(slot ^ hash(step.counts.y * 3u + region));
				if ((h & 1u) != 0u) { flags = flags ^ ${PARTICLE_DRIFT_X}u; }
				if ((h & 2u) != 0u) { flags = flags ^ ${PARTICLE_DRIFT_Z}u; }
			}
		}
	}
	words[at] = bitcast<u32>(x);
	words[at + 1u] = bitcast<u32>(y);
	words[at + 2u] = bitcast<u32>(z);
	words[at + 4u] = age;
	words[at + 5u] = frame + ticks;
	words[at + 6u] = flags;
	return visible;
}

@compute @workgroup_size(${WORKGROUP})
fn main(@builtin(workgroup_id) group: vec3u, @builtin(local_invocation_index) lane: u32) {
	let at = laneOf(group.x, lane);
	var visible = false;
	if (at.live) {
		visible = advance(at.base + ${WEATHER_HEADER_WORDS}u + at.slot * ${PARTICLE_WORDS}u, step.regions[at.region].z, at.slot, at.region);
	}
	if (visible) { atomicAdd(&counted, 1u); }
	workgroupBarrier();
	if (lane == 0u) {
		words[at.base + ${WEATHER_HEADER_WORDS}u + at.capacity * ${
	PARTICLE_WORDS + 1
}u + at.block] = atomicLoad(&counted);
	}
}

@compute @workgroup_size(${WORKGROUP})
fn compact(@builtin(workgroup_id) group: vec3u, @builtin(local_invocation_index) lane: u32) {
	let at = laneOf(group.x, lane);
	let blocks = at.base + ${WEATHER_HEADER_WORDS}u + at.capacity * ${PARTICLE_WORDS + 1}u;
	var before = 0u;
	for (var r = 0u; r < ${MAX_REGION_BLOCKS / WORKGROUP}u; r++) {
		let b = lane + r * ${WORKGROUP}u;
		if (b < at.block) { before += words[blocks + b]; }
	}
	atomicAdd(&counted, before);
	var mark = 0u;
	if (at.live) {
		let flags = words[at.base + ${WEATHER_HEADER_WORDS}u + at.slot * ${PARTICLE_WORDS}u + 6u];
		mark = select(0u, 1u, (flags & ${PARTICLE_VISIBLE | PARTICLE_ALIVE}u) == ${PARTICLE_VISIBLE | PARTICLE_ALIVE}u);
	}
	marks[lane] = mark;
	workgroupBarrier();
	var local = 0u;
	for (var i = 0u; i < ${WORKGROUP}u; i++) {
		if (i < lane) { local += marks[i]; }
	}
	let start = atomicLoad(&counted);
	if (mark != 0u) {
		words[at.base + ${WEATHER_HEADER_WORDS}u + at.capacity * ${PARTICLE_WORDS}u + start + local] = at.slot;
	}
	if (lane == ${WORKGROUP - 1}u && at.last) {
		args[at.region * ${ARG_WORDS}u] = (start + local + mark) * 3u;
	}
}
`;

/*
================
regionWords

A region's words: header, drops, list and block counts, rounded up to
REGION_ALIGN.
================
*/
function regionWords( capacity: number ) {
	const words = WEATHER_HEADER_WORDS + capacity * (PARTICLE_WORDS + 1) + capacity / WORKGROUP;
	return Math.ceil( words * 4 / REGION_ALIGN ) * REGION_ALIGN / 4;
}

// Where each region starts, in words, and the pool's total.
const REGION_OFFSETS = WEATHER_CAPACITY.map( ( _, index ) =>
	WEATHER_CAPACITY.slice( 0, index ).reduce( ( sum, capacity ) => sum + regionWords( capacity ), 0 )
);
const POOL_WORDS = REGION_OFFSETS[WEATHER_REGIONS - 1]! + regionWords( WEATHER_CAPACITY[WEATHER_REGIONS - 1]! );

/*
================
createWeatherParticles

One owner per device generation: a lost device takes the pool with it,
and weather.ts starts its regions empty on the next.
================
*/
export function createWeatherParticles( device: GPUDevice, retire: Retire ) {
	let failure: string | null = null, disposed = false;
	let stepPipeline: GPUComputePipeline | undefined, compactPipeline: GPUComputePipeline | undefined;
	let pool: GPUBuffer | null = null, args: GPUBuffer | null = null, stepBuffer: GPUBuffer | null = null;
	let binding: GPUBindGroup | null = null;
	let layout: GPUBindGroupLayout | undefined;
	const argWords = new Uint32Array( WEATHER_REGIONS * ARG_WORDS );
	const steps = new Uint32Array( MAX_PENDING_STEPS * STEP_SLOT_BYTES / 4 );
	const stepFloats = new Float32Array( steps.buffer );
	// Workgroups each pending step dispatches.
	const stepGroups = new Array<number>( MAX_PENDING_STEPS ).fill( 0 );
	// Each pending step's workgroups a region, and the regions whose
	// arguments the last encoded step left non-empty: a region whose window
	// empties has its count cleared once, in command order after the
	// steps, since no workgroup writes it.
	const regionGroups = new Uint32Array( MAX_PENDING_STEPS * WEATHER_REGIONS );
	const drawn = new Array<boolean>( WEATHER_REGIONS ).fill( false );
	// The corners each region's header holds, to skip an unchanged write.
	const corners = WEATHER_CAPACITY.map( () => new Float32Array( CORNER_FLOATS ).fill( Number.NaN ) );
	const attached = new Map<DeviceDraw, number>();
	let pending = 0, dispatches = 0, spawned = 0, uploadBytes = 0, steppedSlots = 0, droppedSteps = 0;
	const ready = Promise.resolve()
		.then( () => {
			// One explicit layout both pipelines share: the step leaves binding
			// 2 unused, and the steps' uniform slots take a dynamic offset.
			const created = device.createBindGroupLayout( {
				label: "weather-particles",
				entries: [
					{
						binding: 0,
						visibility: GPUShaderStage.COMPUTE,
						buffer: { type: "uniform", hasDynamicOffset: true }
					},
					{ binding: 1, visibility: GPUShaderStage.COMPUTE, buffer: { type: "storage" } },
					{ binding: 2, visibility: GPUShaderStage.COMPUTE, buffer: { type: "storage" } }
				]
			} );
			layout = created;
			const module = device.createShaderModule( { label: "weather-particles", code: shader } );
			const pipelineLayout = device.createPipelineLayout( {
				label: "weather-particles",
				bindGroupLayouts: [ created ]
			} );
			return Promise.all( [ "main", "compact" ].map( entryPoint =>
				device.createComputePipelineAsync( {
					label: "weather-particles-" + entryPoint,
					layout: pipelineLayout,
					compute: { module, entryPoint }
				} )
			) );
		} )
		.then( ( [main, compact] ) => {
			if ( disposed ) return;
			stepPipeline = main;
			compactPipeline = compact;
		} )
		.catch( error => {
			if ( !disposed ) failure = String( error );
		} );

	/*
	================
	write
	================
	*/
	function write( buffer: GPUBuffer, offset: number, data: ArrayBufferView ) {
		device.queue.writeBuffer( buffer, offset, data.buffer as ArrayBuffer, data.byteOffset, data.byteLength );
		uploadBytes += data.byteLength;
	}

	/*
	================
	ensurePool

	The pool, created zeroed (every drop dead), and each region's capacity
	word, on first use.
	================
	*/
	function ensurePool() {
		if ( disposed ) throw Error( "Disposed weather particles" );
		if ( pool ) return pool;
		pool = device.createBuffer( {
			label: "weather-particles",
			size: POOL_WORDS * 4,
			// COPY_SRC: the WebGPU parity test reads the pool back; the client never does.
			usage: GPUBufferUsage.STORAGE | GPUBufferUsage.COPY_DST | GPUBufferUsage.COPY_SRC
		} );
		args = device.createBuffer( {
			label: "weather-arguments",
			size: WEATHER_REGIONS * ARG_BYTES,
			usage: GPUBufferUsage.INDIRECT | GPUBufferUsage.STORAGE | GPUBufferUsage.COPY_DST | GPUBufferUsage.COPY_SRC
		} );
		for ( let region = 0; region < WEATHER_REGIONS; region++ ) {
			write(
				pool,
				(REGION_OFFSETS[region]! + WEATHER_CAPACITY_WORD) * 4,
				Uint32Array.of( WEATHER_CAPACITY[region]! )
			);
		}
		return pool;
	}

	/*
	================
	checkRegion
	================
	*/
	function checkRegion( region: number ) {
		if ( !Number.isInteger( region ) || region < 0 || region >= WEATHER_REGIONS ) {
			throw Error( "Invalid weather region" );
		}
	}

	return {
		ready,
		/*
		================
		binding

		The region a weather draw's vertex stage reads (binding 17).
		================
		*/
		binding( region: number ): GPUBufferBinding {
			checkRegion( region );
			return {
				buffer: ensurePool(),
				offset: REGION_OFFSETS[region]! * 4,
				size: regionWords( WEATHER_CAPACITY[region]! ) * 4
			};
		},
		/*
		================
		attach

		Makes draw its region's draw: its counts come from the region's
		arguments (empty until the next step), its corner uvs and baseVertex
		go to the header. A later draw of the region takes it over.
		================
		*/
		attach( region: number, draw: DeviceDraw, uvs: Float32Array ) {
			checkRegion( region );
			if ( uvs.length !== UV_FLOATS ) throw Error( "Invalid weather uvs" );
			if ( draw.count < WEATHER_CAPACITY[region]! * 3 ) throw Error( "Weather draw is smaller than its region" );
			const buffer = ensurePool();
			for ( const [other, at] of attached ) if ( at === region ) attached.delete( other );
			attached.set( draw, region );
			DeviceDraw.drawIndirect( draw, args!, region * ARG_BYTES );
			const at = region * ARG_WORDS;
			argWords.set( [ 0, draw.instanceCount, draw.firstIndex, draw.baseVertex, 0 ], at );
			write( args!, at * 4, argWords.subarray( at, at + ARG_WORDS ) );
			drawn[region] = false;
			const header = REGION_OFFSETS[region]! * 4;
			write( buffer, header + CORNER_FLOATS * 4, uvs );
			write( buffer, header + (WEATHER_CAPACITY_WORD + 1) * 4, Uint32Array.of( draw.baseVertex ) );
		},
		/*
		================
		detach
		================
		*/
		detach( draw: DeviceDraw ) {
			attached.delete( draw );
		},
		/*
		================
		spawn

		Writes drop records (PARTICLE_WORDS each, weather.ts layout, the
		birth serial in PARTICLE_BIRTH_WORD) into region's slots from slot on.
		================
		*/
		spawn( region: number, slot: number, records: Uint32Array ) {
			checkRegion( region );
			const count = records.length / PARTICLE_WORDS;
			if (
				!Number.isInteger( count ) || !Number.isInteger( slot ) || slot < 0 ||
				slot + count > WEATHER_CAPACITY[region]!
			) throw Error( "Invalid weather spawn" );
			const offset = (REGION_OFFSETS[region]! + WEATHER_HEADER_WORDS + slot * PARTICLE_WORDS) * 4;
			write( ensurePool(), offset, records );
			spawned += count;
		},
		/*
		================
		corners

		The region's billboard corners (three xyz offsets, padded to vec4),
		written only when they changed.
		================
		*/
		corners( region: number, value: Float32Array ) {
			checkRegion( region );
			if ( value.length !== CORNER_FLOATS ) throw Error( "Invalid weather corners" );
			const last = corners[region]!;
			let changed = false;
			for ( let i = 0; i < CORNER_FLOATS; i++ ) {
				if ( last[i] !== value[i] ) changed = true;
			}
			if ( !changed ) return;
			last.set( value );
			write( ensurePool(), REGION_OFFSETS[region]! * 4, value );
		},
		/*
		================
		step

		Queues one simulation step for the next encode, or refuses it (false)
		when MAX_PENDING_STEPS already wait (frames prepared and never
		submitted): the caller then forgets its drops (weather.ts), so its
		lifetime clock never runs ahead of a pool that missed a step.
		================
		*/
		step( input: WeatherStep ) {
			ensurePool();
			if ( input.windows.length !== WEATHER_REGIONS ) throw Error( "Invalid weather windows" );
			if ( pending === MAX_PENDING_STEPS ) {
				droppedSteps++;
				return false;
			}
			const at = pending * STEP_SLOT_BYTES / 4;
			steps.fill( 0, at, at + STEP_WORDS );
			stepFloats.set( input.matrix, at );
			stepFloats.set( input.reference, at + 16 );
			stepFloats.set( input.motion, at + 20 );
			steps[at + 24] = input.elapsedMs >>> 0;
			steps[at + 25] = input.seed >>> 0;
			steps[at + 26] = input.ticks >>> 0;
			steps[at + 27] = input.serial >>> 0;
			let groups = 0;
			for ( let region = 0; region < WEATHER_REGIONS; region++ ) {
				const window = input.windows[region]!, capacity = WEATHER_CAPACITY[region]!;
				if ( !Number.isInteger( window.live ) || window.live < 0 || window.live > capacity ) {
					throw Error( "Invalid weather window" );
				}
				const row = at + 28 + region * 4;
				steps[row] = REGION_OFFSETS[region]!;
				steps[row + 1] = capacity;
				steps[row + 2] = REGION_KIND[region]!;
				steps[row + 3] = window.tail % capacity;
				steps[at + 40 + region] = window.live;
				const count = Math.ceil( window.live / WORKGROUP );
				steps[at + 44 + region] = count;
				regionGroups[pending * WEATHER_REGIONS + region] = count;
				groups += count;
				steppedSlots += count * WORKGROUP;
			}
			steps[at + 47] = groups;
			stepGroups[pending] = groups;
			pending++;
			return true;
		},
		/*
		================
		encode

		Every queued step, in order: one dispatch steps the live windows'
		drops, one compacts their visible ones into the lists and arguments.
		However many passes then draw the lists, each step ran once.
		================
		*/
		encode( encoder: GPUCommandEncoder, timing?: GpuTimingFrame ) {
			if ( disposed || !pending || !pool ) return;
			if ( failure ) throw Error( `Weather particle pipeline failed: ${failure}` );
			if ( !stepPipeline || !compactPipeline || !layout ) throw Error( "Weather particle pipeline is not ready" );
			stepBuffer ??= device.createBuffer( {
				label: "weather-step",
				size: MAX_PENDING_STEPS * STEP_SLOT_BYTES,
				usage: GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST
			} );
			write( stepBuffer, 0, steps.subarray( 0, pending * STEP_SLOT_BYTES / 4 ) );
			binding ??= device.createBindGroup( {
				label: "weather-particles",
				layout,
				entries: [
					{ binding: 0, resource: { buffer: stepBuffer, size: STEP_WORDS * 4 } },
					{ binding: 1, resource: { buffer: pool } },
					{ binding: 2, resource: { buffer: args! } }
				]
			} );
			const pass = encoder.beginComputePass( {
				label: "weather-particles",
				timestampWrites: timing?.pass( "weather-particles" )
			} );
			for ( let index = 0; index < pending; index++ ) {
				const groups = stepGroups[index]!;
				if ( !groups ) continue;
				pass.setBindGroup( 0, binding, [ index * STEP_SLOT_BYTES ] );
				pass.setPipeline( stepPipeline );
				pass.dispatchWorkgroups( groups );
				pass.setPipeline( compactPipeline );
				pass.dispatchWorkgroups( groups );
				dispatches += 2;
			}
			pass.end();
			// A region the last step left empty keeps no count from an earlier
			// encode or from an earlier step of this one.
			const lastStep = (pending - 1) * WEATHER_REGIONS;
			for ( let region = 0; region < WEATHER_REGIONS; region++ ) {
				let written = drawn[region]!;
				for ( let index = 0; index < pending; index++ ) {
					if ( regionGroups[index * WEATHER_REGIONS + region] ) written = true;
				}
				drawn[region] = !!regionGroups[lastStep + region];
				if ( written && !drawn[region] ) encoder.clearBuffer( args!, region * ARG_BYTES, 4 );
			}
			pending = 0;
		},
		stats() {
			return { dispatches, spawned, uploadBytes, steppedSlots, droppedSteps, attached: attached.size };
		},
		/*
		================
		buffers

		The pool and the arguments with each region's word offset, for the
		WebGPU parity test's readback (tests/browser/live-weather.test.mjs).
		================
		*/
		buffers() {
			return { pool, args, offsets: REGION_OFFSETS };
		},
		dispose() {
			disposed = true;
			attached.clear();
			for ( const buffer of [ pool, args, stepBuffer ] ) if ( buffer ) retire( buffer );
			pool = args = stepBuffer = null;
			binding = null;
			pending = 0;
		}
	};
}
