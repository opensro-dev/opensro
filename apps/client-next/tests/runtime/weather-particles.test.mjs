/*
===========================================================================

weather-particles.test.mjs - the GPU weather pool's CPU contract

The step itself runs on the GPU (weather-particles.ts; its drop-level
parity is tests/browser/live-weather.test.mjs). These tests pin what the
CPU side owes it: dispatches only over live windows, one pair a queued
step, a refused step past the queue, and an empty region's indirect count
cleared in command order whatever the steps before it wrote.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import { GPU_BUFFER_USAGE } from "../helpers/strict-gpu.mjs";
globalThis.GPUBufferUsage = GPU_BUFFER_USAGE;
globalThis.GPUShaderStage ??= { VERTEX: 1, FRAGMENT: 2, COMPUTE: 4 };
const { createWeatherParticles } = await import(
	sourceFileUrl( "src/engine/runtime/renderer/device/weather-particles.ts" ).href
);

/*
================
fakeDevice

Every command an encoder takes, in order.
================
*/
function fakeDevice() {
	const commands = [];
	const device = {
		createBuffer: descriptor => ({ ...descriptor, destroy() {} }),
		createBindGroupLayout: descriptor => descriptor,
		createPipelineLayout: descriptor => descriptor,
		createShaderModule: () => ({}),
		createComputePipelineAsync: async descriptor => ({ label: descriptor.label }),
		createBindGroup: descriptor => descriptor,
		queue: { writeBuffer() {} }
	};
	const encoder = {
		clearBuffer: ( buffer, offset, size ) => commands.push( [ "clear", buffer.label, offset, size ] ),
		beginComputePass() {
			let current;
			return {
				setPipeline: value => current = value.label,
				setBindGroup() {},
				dispatchWorkgroups: count => commands.push( [ current, count ] ),
				end() {}
			};
		}
	};
	return { device, encoder, commands };
}

/*
================
step

A step whose region 1 window holds live slots (0 for none).
================
*/
function step( live, serial ) {
	return {
		matrix: new Float32Array( 16 ),
		reference: [ 0, 0, 0, -500 ],
		motion: [ 0, 0, 0 ],
		elapsedMs: 16,
		ticks: 1,
		seed: serial,
		serial,
		windows: [ { tail: 0, live: 0 }, { tail: 0, live }, { tail: 0, live: 0 } ]
	};
}

test("a step dispatches only its live windows, one step and one compact pass", async () => {
	const { device, encoder, commands } = fakeDevice(), pool = createWeatherParticles( device, () => {} );
	await pool.ready;
	pool.step( step( 130, 1 ) );
	pool.encode( encoder );
	assert.deepEqual( commands, [ [ "weather-particles-main", 3 ], [ "weather-particles-compact", 3 ] ] );
	commands.length = 0;
	pool.encode( encoder );
	assert.deepEqual( commands, [], "an encode without a new step dispatches nothing" );
});

test("an empty region's count is cleared after a queued step that filled it", async () => {
	// Region 1 starts empty, a first queued step fills it (its compaction
	// writes a count), the last step of the same encode empties it again:
	// the count must be cleared after both, though no earlier encode drew it.
	const { device, encoder, commands } = fakeDevice(), pool = createWeatherParticles( device, () => {} );
	await pool.ready;
	assert.equal( pool.step( step( 64, 1 ) ), true );
	assert.equal( pool.step( step( 0, 2 ) ), true );
	pool.encode( encoder );
	assert.deepEqual( commands.at( -1 ), [ "clear", "weather-arguments", 20, 4 ] );
	assert.deepEqual( commands.slice( 0, 2 ), [ [ "weather-particles-main", 1 ], [ "weather-particles-compact", 1 ] ] );
	// Empty and never written since: nothing to clear.
	commands.length = 0;
	pool.step( step( 0, 3 ) );
	pool.encode( encoder );
	assert.deepEqual( commands, [] );
	// Drawn by one encode, emptied by the next.
	pool.step( step( 64, 4 ) );
	pool.encode( encoder );
	commands.length = 0;
	pool.step( step( 0, 5 ) );
	pool.encode( encoder );
	assert.deepEqual( commands, [ [ "clear", "weather-arguments", 20, 4 ] ] );
});

test("steps past the queue are refused, never merged", async () => {
	const { device, encoder, commands } = fakeDevice(), pool = createWeatherParticles( device, () => {} );
	await pool.ready;
	const accepted = [ 1, 2, 3, 4, 5 ].map( serial => pool.step( step( 64, serial ) ) );
	assert.deepEqual( accepted, [ true, true, true, true, false ] );
	pool.encode( encoder );
	assert.equal( commands.length, 8, "each accepted step runs once" );
	assert.equal( pool.stats().droppedSteps, 1 );
});
