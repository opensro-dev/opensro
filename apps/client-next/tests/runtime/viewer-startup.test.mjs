/*
===========================================================================

viewer-startup.test.mjs - a still waits for the renderer to start

#4015: the viewer counted settle frames while renderer.frame() still
no-oped in its start-up phase (the device warm-up compiles the pipelines
first; a software adapter such as SwiftShader - the only WebGPU in a
GPU-less VM - needs seconds for that). The readback then refused a doll
target that did not exist yet and the still failed "render". The frame
now waits for phase "running" before anything settles, and a renderer
that never starts fails as the timeout it is.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";

const { createViewer } = await import( "../../src/engine/runtime/viewer/viewer.ts" );
// The page globals the viewer installs (__viewerStill), untyped in this
// test: the fakes stand in for browser objects.
const scope = /** @type {any} */ (globalThis);

/*
================
fakeHost

A viewer host whose renderer starts in the given phase and whose doll
target exists once it runs: readDoll records every call and answers a
small opaque still. The character owner knows the monster and has it
ready and dressed, so a running renderer frames it at once.
================
*/
function fakeHost( phase ) {
	const state = { phase, readbacks: 0 };
	const host = {
		canvas: { width: 0, height: 0 },
		picture: {
			clientWidth: 0,
			clientHeight: 0,
			width: 0,
			height: 0,
			addEventListener() {},
			getContext: () => null
		},
		search: "?monster=1933&still=1&batch=1",
		wake() {},
		renderer: {
			setViewerLight() {},
			phase: () => state.phase,
			setUi() {},
			frame() {},
			characterBounds: () => /** @type {const} */ ([ -1, 0, -1, 1, 2, 1 ]),
			readDoll: () => {
				state.readbacks++;
				const rgba = new Uint8ClampedArray( 4 * 4 * 4 ).fill( 255 );
				return Promise.resolve( { width: 4, height: 4, rgba } );
			}
		},
		characters: {
			receiveLifecycle() {},
			step() {},
			ready: () => true,
			knows: () => true,
			itemType: () => undefined,
			dressed: () => true,
			linkedRide: () => undefined,
			glows: () => false,
			glowing: () => false,
			error: () => null,
			failure: () => null
		}
	};
	return { host: /** @type {any} */ (host), state };
}

/*
================
withBrowserGlobals

window, navigator.gpu and the PNG encoder's canvas as the viewer page
has them, restored after.
================
*/
async function withBrowserGlobals( run ) {
	const saved = new Map();
	for ( const key of [ "window", "navigator", "devicePixelRatio", "OffscreenCanvas", "ImageData" ] ) {
		saved.set( key, Object.getOwnPropertyDescriptor( globalThis, key ) );
	}
	const page = /** @type {any} */ ({ opener: null, addEventListener() {}, removeEventListener() {} });
	page.parent = page;
	Object.defineProperty( globalThis, "window", { value: page, configurable: true, writable: true } );
	Object.defineProperty( globalThis, "navigator", { value: { gpu: {} }, configurable: true } );
	Object.defineProperty( globalThis, "devicePixelRatio", { value: 1, configurable: true } );
	Object.defineProperty( globalThis, "OffscreenCanvas", {
		value: class {
			constructor( width, height ) {
				this.width = width;
				this.height = height;
			}
			getContext() {
				return { putImageData() {} };
			}
			async convertToBlob() {
				return new Blob( [ new Uint8Array( [ 137, 80, 78, 71 ] ) ], { type: "image/png" } );
			}
		},
		configurable: true,
		writable: true
	} );
	Object.defineProperty( globalThis, "ImageData", {
		value: class {
			constructor( data, width, height ) {
				this.data = data;
				this.width = width;
				this.height = height;
			}
		},
		configurable: true,
		writable: true
	} );
	try {
		await run();
	} finally {
		for ( const [key, descriptor] of saved ) {
			if ( descriptor ) Object.defineProperty( globalThis, key, descriptor );
			else delete scope[key];
		}
	}
}

/*
================
tick

Lets the viewer's readback promise chain run between driven frames.
================
*/
function tick() {
	return new Promise( resolve => setImmediate( resolve ) );
}

test("a still waits out the renderer's start-up, then reads the doll", () =>
	withBrowserGlobals( async () => {
		const { host, state } = fakeHost( "starting" );
		const viewer = createViewer( host );
		const still = scope.window.__viewerStill;
		let settled = false;
		still.then( () => settled = true, () => settled = true );
		// Start-up frames draw nothing and settle nothing.
		for ( let i = 0; i < 5; i++ ) assert.equal( viewer.frame( i * 16 ), "draw" );
		await tick();
		assert.equal( state.readbacks, 0 );
		assert.equal( settled, false );

		state.phase = "running";
		let png = /** @type {string | null} */ (null);
		still.then( value => png = value );
		for ( let i = 0; i < 16 && !png; i++ ) {
			viewer.frame( 100 + i * 16 );
			await tick();
		}
		assert.ok( typeof png === "string" && png.startsWith( "data:image/png;base64," ) );
		assert.ok( state.readbacks >= 1 );
	} ));

test("a renderer that never starts fails the still as a timeout, not a render", () =>
	withBrowserGlobals( async () => {
		const { host, state } = fakeHost( "starting" );
		const viewer = createViewer( host );
		const still = scope.window.__viewerStill;
		const rejection = assert.rejects( still, /timeout/ );
		// 30 s of driven frames without the phase ever reaching "running".
		for ( let i = 0; i < 2500; i++ ) viewer.frame( i * 16 );
		await rejection;
		assert.equal( state.readbacks, 0 );
	} ));
