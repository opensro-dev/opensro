/*
===========================================================================

frame-water-order.test.mjs - the water refraction stage's pass order

Port-only, not native (device/water-refraction.ts). The command fake
records every pass, the draws it executes and when the stage's copies
are encoded. Armed with blended water in view, the frame draws the
opaque world without that water, copies the frame, then draws the water
and the transparent tail in their order; otherwise it keeps the native
single pass.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { createFrame } = await import( "../../src/engine/runtime/renderer/frame/frame.ts" );

/*
================
fixture
================
*/
function fixture() {
	const view = { name: "frame" }, depth = { name: "depth" };
	const events = [];
	const commands = {
		createBundleEncoder() {
			let pipeline;
			const draws = [];
			return {
				setPipeline( value ) {
					pipeline = value;
				},
				setBindGroup() {},
				setVertexBuffer() {},
				setIndexBuffer() {},
				draw() {
					draws.push( pipeline.name );
				},
				drawIndexed() {
					draws.push( pipeline.name );
				},
				finish() {
					return { draws };
				}
			};
		},
		createEncoder() {
			return {
				beginRenderPass( descriptor ) {
					const pass = { label: descriptor.label, descriptor, draws: /** @type {string[]} */ ([]) };
					events.push( pass );
					return {
						setBlendConstant() {},
						executeBundles( bundles ) {
							for ( const bundle of bundles ) pass.draws.push( ...bundle.draws );
						},
						end() {}
					};
				},
				finish() {
					return 1;
				}
			};
		},
		submit() {}
	};
	const frame = createFrame( /** @type {any} */ (commands) );
	const stage = {
		encode( _encoder, colour, frameDepth ) {
			events.push( { label: "copies", colour, depth: frameDepth } );
		}
	};
	const ui = [ { pipeline: { name: "label" }, layer: "world", binding: {}, first: 0, count: 1 } ];
	/*
	================
	draw

	world is a list of [name, flags]; transparentStart splits it as the
	renderer does. armed passes the stage.
	================
	*/
	function draw( world, transparentStart, armed, extra = {} ) {
		const draws = world.map( ( [name, flags = {}] ) => ({
			pipeline: { name },
			binding: {},
			indexCount: 3,
			instanceCount: 1,
			...flags
		}) );
		frame.draw(
			/** @type {any} */ (view),
			undefined,
			undefined,
			/** @type {any} */ (depth),
			/** @type {any} */ (draws),
			/** @type {any} */ (ui),
			[],
			undefined,
			undefined,
			undefined,
			undefined,
			[],
			undefined,
			undefined,
			undefined,
			undefined,
			/** @type {any} */ ({ ...(armed ? { water: { stage, transparentStart } } : {}), ...extra })
		);
		return events.map( event => event.label === "copies" ? event : [ event.label, event.draws ] );
	}
	/*
	================
	msaa

	A 4x stage fake: its views, and each depth resolve in the event list.
	================
	*/
	const msaa = {
		view: { name: "msaa-colour" },
		depth: { name: "msaa-depth" },
		resolveDepth( _encoder, target ) {
			events.push( { label: "resolve-depth", target } );
		}
	};
	return { draw, events, view, depth, msaa };
}

const WATER = { water: true, blended: true };
const ICE = { water: true, blended: false };
const BLENDED = { blended: true };

test("armed with blended water in view: opaque frame, copies, then the water and the transparent tail", () => {
	const f = fixture();
	const world = [ [ "terrain" ], [ "lake", WATER ], [ "house" ], [ "smoke", BLENDED ], [ "rain", BLENDED ] ];
	const order = f.draw( world, 3, true );
	assert.deepEqual( order[0], [ "main-pass", [ "terrain", "house" ] ] );
	assert.equal( order[1].label, "copies" );
	assert.equal( order[1].colour, f.view, "the copies read the scene target" );
	assert.equal( order[1].depth, f.depth, "the copies read the scene depth" );
	// The smoke in front of the lake still covers it: the tail keeps its
	// native order after the water. The labels close the last scene pass.
	assert.deepEqual( order[2], [ "water-refraction", [ "lake", "smoke", "rain", "label" ] ] );
	assert.equal( order.length, 3 );
	const [main, , overlay] = f.events;
	assert.equal( main.descriptor.depthStencilAttachment.depthStoreOp, "store", "the overlay loads the main depth" );
	assert.equal( overlay.descriptor.depthStencilAttachment.depthLoadOp, "load" );
	assert.equal( overlay.descriptor.colorAttachments[0].loadOp, "load" );
});

test("blended water inside the transparent tail keeps its sorted place", () => {
	const f = fixture();
	const order = f.draw(
		[ [ "terrain" ], [ "far-smoke", BLENDED ], [ "pond", WATER ], [ "near-smoke", BLENDED ] ],
		1,
		true
	);
	assert.deepEqual( order[0], [ "main-pass", [ "terrain" ] ] );
	assert.deepEqual( order[2], [ "water-refraction", [ "far-smoke", "pond", "near-smoke", "label" ] ] );
});

test("opaque water (the ice) stays in the main pass and alone never arms the copies", () => {
	const f = fixture();
	const order = f.draw( [ [ "terrain" ], [ "ice", ICE ], [ "smoke", BLENDED ] ], 2, true );
	assert.deepEqual( order, [ [ "main-pass", [ "terrain", "ice", "smoke", "label" ] ] ] );
});

test("off, the frame is the native single pass", () => {
	const world = [ [ "terrain" ], [ "lake", WATER ], [ "house" ], [ "smoke", BLENDED ] ];
	const f = fixture();
	assert.deepEqual( f.draw( world, 3, false ), [ [ "main-pass", [
		"terrain",
		"lake",
		"house",
		"smoke",
		"label"
	] ] ] );
	assert.equal( f.events[0].descriptor.depthStencilAttachment.depthStoreOp, "discard" );
});

test("under MSAA the water draws on into the 4x buffers and resolves again", () => {
	const f = fixture();
	const world = [ [ "terrain" ], [ "lake", WATER ], [ "smoke", BLENDED ] ];
	const order = f.draw( world, 2, true, { msaa: f.msaa } );
	// The scene pass resolves, its depth resolves for the copies, the water
	// pass draws the retained 4x bundles, and the depth resolves once more
	// for the label passes that read it after the separate scene.
	assert.deepEqual( order.slice( 0, 5 ).map( event => event.label ?? event[0] ), [
		"main-pass",
		"resolve-depth",
		"copies",
		"water-refraction",
		"resolve-depth"
	] );
	const [main] = f.events, overlay = f.events.find( event => event.label === "water-refraction" );
	assert.equal( main.descriptor.colorAttachments[0].storeOp, "store", "the 4x colour is kept for the water pass" );
	assert.equal( overlay.descriptor.colorAttachments[0].view, f.msaa.view );
	assert.equal( overlay.descriptor.colorAttachments[0].resolveTarget, f.view );
	assert.equal( overlay.descriptor.depthStencilAttachment.view, f.msaa.depth );
	assert.equal( f.events.filter( event => event.label === "resolve-depth" ).length, 2 );
	assert.ok( f.events.filter( event => event.label === "resolve-depth" ).every( event => event.target === f.depth ) );
});

test("under MSAA without water the scene keeps one resolve and discards its 4x colour", () => {
	const f = fixture();
	f.draw( [ [ "terrain" ], [ "lake", WATER ] ], 2, false, { msaa: f.msaa } );
	assert.equal( f.events[0].descriptor.colorAttachments[0].storeOp, "discard" );
	assert.equal( f.events.filter( event => event.label === "resolve-depth" ).length, 1 );
	assert.ok( !f.events.some( event => event.label === "water-refraction" ) );
});
