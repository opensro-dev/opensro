/*
===========================================================================

runtime-failures.test.mjs - runtime owner failure and teardown contracts

Injects owners through the synthetic build boundary and verifies failures
reach the player while each owned lifetime is disposed exactly once.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { build } from "esbuild";
import { defined } from "../helpers/defined.mjs";
const factories = {
	frontend: "createFrontend",
	navigation: "createNavigationStream",
	ui: "createUi",
	audio: "createAudio",
	characters: "createCharacterPresentation",
	world: "createWorldStream",
	presentation: "createPresentation",
	assets: "createAssets",
	input: "createInput",
	platform: "createPlatform",
	renderer: "createRenderer",
	simulation: "createSimulationHost",
	release: "createReleaseWatch",
	"bug-report": "createBugReport",
	"build-info": "createBuildInfo"
};
const compiled = await build( {
	entryPoints: [ "src/engine/runtime/runtime.ts" ],
	bundle: true,
	platform: "node",
	format: "esm",
	write: false,
	define: {
		"import.meta.env.DEV": "false",
		// A real build always defines MODE (frame-probes.ts reads it).
		"import.meta.env.MODE": JSON.stringify( "production" ),
		"import.meta.env.SRO_CLIENT_REVISION": "undefined",
		"import.meta.env.SRO_CLIENT_SUBJECT": "undefined",
		"import.meta.env.VITE_AGENT_API_BASE": "undefined",
		// tools/build-metadata.mjs stamps these in a real build; the fixture has none.
		"import.meta.env.SRO_CLIENT_REVISION": "undefined",
		"import.meta.env.SRO_CLIENT_SUBJECT": "undefined",
		"import.meta.url": JSON.stringify( "http://localhost/runtime.ts" )
	},
	plugins: [ {
		name: "runtime-owners",
		/*
================
setup
================
		*/
		setup( build ) {
			build.onResolve( { filter: /^\.\// }, args => {
				const owner = args.path.split( "/" )[1];
				if ( factories[owner] ) return { path: owner, namespace: "owner" };
			} );
			build.onLoad(
				{ filter: /.*/, namespace: "owner" },
				args => ({
					contents: `export function ${factories[args.path]}(){return globalThis.__runtimeOwners[${
						JSON.stringify( args.path )
					}];}`
				})
			);
		}
	} ]
} );
const { startRuntime } = await import(
	"data:text/javascript;base64," + Buffer.from( compiled.outputFiles[0].contents ).toString( "base64" )
);
/*
================
frameOwners

Owners whose first frame reaches characters.step; failure selects which
owner fails it ("assets" dies idle, "characters" throws from its step).
================
*/
function frameOwners( failure, closed, reports, sounds ) {
	const owners = Object.fromEntries(
		Object.keys( factories ).map( key => [ key, {
			/*
================
step
================
			*/
			step() {},
			/*
================
dispose
================
			*/
			dispose() {
				closed.push( key );
			}
		} ] )
	);
	Object.assign( owners.assets, {
		health: () => failure === "assets" ? { phase: "failed", error: "worker died" } : { phase: "running" }
	} );
	owners.renderer.setCharacterPreview = () => {};
	owners.renderer.setSelectionDecal = () => {};
	Object.assign( owners.characters, { frameWork() {} } );
	Object.assign( owners.platform, {
		report: value => reports.push( value ),
		runningEntry: () => null,
		/*
================
setMovementDump
================
		*/
		setMovementDump() {},
		visibilityReturned: () => false,
		connectionReturned: () => false,
		/*
================
presentUpdate
================
		*/
		presentUpdate() {}
	} );
	owners.release.newerAvailable = () => false;
	Object.assign( owners.input, {
		drain: () => null,
		error: () => null,
		camera: () => ({ yaw: 0, pitch: 0, distance: 80 })
	} );
	Object.assign( owners.simulation, {
		// Never settles: these frames are driven by the fake RAF alone.
		delivery: () => new Promise( () => {} ),
		pollSession: () => null,
		pollWorld: () => null,
		poll: () => ({ timeMs: 1000, sequence: 1, acceptedInputSequence: 0 }),
		error: () => null,
		camera: () => null
	} );
	owners.audio.world = () => {};
	owners.audio.nativeUi = ( handle, at ) => sounds.push( { handle, at } );
	owners.audio.nativeItem = ( cue, at ) => sounds.push( { ...cue, at } );
	Object.assign( owners.presentation, {
		gameplay: () => null,
		entities: () => [],
		takeFeedback: () => [],
		takeSounds: () => [ { kind: "ui-sound", handle: "SND_POTION", at: 950 }, {
			kind: "item-sound",
			cue: { handle: "SND_EQUIP", typeFlags: 0x132c },
			at: 900
		} ]
	} );
	owners.world.step = () => {};
	owners.renderer.setWorldClock = () => {};
	owners.renderer.setWeather = () => {};
	owners.world.pumpCameraScripts = () => {};
	owners.frontend.step = () => ({ phase: "loading-title" });
	owners.navigation.step = () => {};
	owners.characters.previewReady = () => false;
	owners.characters.dockReady = () => false;
	owners.characters.entryReady = () => false;
	owners.ui.entryReady = () => false;
	owners.characters.profile = () => {};
	owners.characters.eventRain = () => false;
	owners.characters.orbGauge = () => ({ authoritative: 0, displayed: 0, pending: 0 });
	owners.characters.receiveFeedback = () => {};
	owners.characters.step = () => {
		throw new Error( "injected presentation failure" );
	};
	Object.assign( owners.characters, { mallOutfit: () => {}, mallPreviewState: () => ({ wearable: [] }) } );
	Object.assign( owners.ui, { mallPreview: () => null, mallPreviewState: () => {} } );
	return owners;
}

test("runtime reports idle asset death and frame exceptions, then disposes every owner once", t => {
	const previous = new Map(
		[ "__runtimeOwners", "requestAnimationFrame", "cancelAnimationFrame", "location" ].map(
			key => [ key, Object.getOwnPropertyDescriptor( globalThis, key ) ]
		)
	);
	t.after( () => {
		for ( const [key, value] of previous ) {
			if ( value ) Object.defineProperty( globalThis, key, value );
			else delete globalThis[key];
		}
	} );
	for ( const failure of [ "assets", "characters" ] ) {
		const closed = [], reports = [], sounds = [];
		let callback, frames = 0;
		const owners = frameOwners( failure, closed, reports, sounds );
		globalThis.location = { search: "", origin: "http://localhost" };
		globalThis.__runtimeOwners = owners;
		globalThis.requestAnimationFrame = fn => {
			callback = fn;
			return ++frames;
		};
		globalThis.cancelAnimationFrame = () => {};
		const runtime = startRuntime( {}, {} );
		assert.doesNotThrow( () => callback( 100000 ) );
		assert.equal( frames, 1 );
		assert.equal( reports.length, 1 );
		assert.match( reports[0], failure === "assets" ? /worker died.*Reload/ : /injected presentation failure/ );
		assert.deepEqual(
			sounds,
			failure === "assets" ?
				[] :
				[ { handle: "SND_POTION", at: 99.95 }, { handle: "SND_EQUIP", typeFlags: 0x132c, at: 99.9 } ],
			"simulation age is translated into the independent RAF clock for both sound routes"
		);
		assert.deepEqual( [ ...closed ].sort(), Object.keys( factories ).filter( key => key !== "input" ).sort() );
		runtime.dispose();
		defined( callback )( 2 );
		assert.equal( closed.length, Object.keys( factories ).length - 1 );
	}
});

test("a hidden tab runs frames from worker deliveries; a visible one waits for its RAF", async t => {
	const keys = [ "__runtimeOwners", "requestAnimationFrame", "cancelAnimationFrame", "location", "document" ];
	const previous = new Map( keys.map( key => [ key, Object.getOwnPropertyDescriptor( globalThis, key ) ] ) );
	t.after( () => {
		for ( const [key, value] of previous ) {
			if ( value ) Object.defineProperty( globalThis, key, value );
			else delete globalThis[key];
		}
	} );
	for ( const visibilityState of [ "visible", "hidden" ] ) {
		const closed = [], reports = [], sounds = [];
		const owners = frameOwners( "characters", closed, reports, sounds );
		// Deliveries arrive on the worker's own clock, never from a RAF.
		owners.simulation.delivery = () => new Promise( resolve => setTimeout( resolve, 20 ) );
		let rafs = 0, cancelled = 0;
		globalThis.document = { visibilityState };
		globalThis.location = { search: "", origin: "http://localhost" };
		globalThis.__runtimeOwners = owners;
		globalThis.requestAnimationFrame = () => ++rafs;
		globalThis.cancelAnimationFrame = () => cancelled++;
		const runtime = startRuntime( {}, {} );
		await new Promise( resolve => setTimeout( resolve, 400 ) );
		if ( visibilityState === "hidden" ) {
			// The frame ran (and hit the injected failure) with no RAF callback.
			assert.equal( reports.length, 1 );
			assert.match( reports[0], /injected presentation failure/ );
			assert.equal( cancelled >= 1, true, "the pending RAF is retired" );
		} else assert.equal( reports.length, 0, "a visible tab never frames off a delivery" );
		runtime.dispose();
	}
});

test("startup rolls back every acquired owner in reverse order at every constructor boundary", t => {
	const keys = [ "__runtimeOwners", "requestAnimationFrame", "cancelAnimationFrame", "location" ];
	const previous = new Map( keys.map( key => [ key, Object.getOwnPropertyDescriptor( globalThis, key ) ] ) );
	t.after( () => {
		for ( const [key, value] of previous ) {
			if ( value ) Object.defineProperty( globalThis, key, value );
			else delete globalThis[key];
		}
	} );
	globalThis.location = { search: "", origin: "http://localhost" };
	globalThis.cancelAnimationFrame = () => {};
	// Discover acquisition order from successful startup, then fail each boundary.
	const order = [], closed = [];
	let failure = null;
	globalThis.__runtimeOwners = Object.fromEntries(
		Object.keys( factories ).map( name => [ name, {
			/*
================
dispose
================
			*/
			dispose() {
				closed.push( name );
			}
		} ] )
	);
	globalThis.__runtimeOwners.platform.runningEntry = () => null;
	globalThis.__runtimeOwners.platform.setMovementDump = () => {};
	globalThis.__runtimeOwners.simulation.delivery = () => new Promise( () => {} );
	for ( const name of Object.keys( factories ) ) {
		const owner = globalThis.__runtimeOwners[name];
		Object.defineProperty( globalThis.__runtimeOwners, name, {
			/*
================
get
================
			*/
			get() {
				if ( name === failure ) throw Error( "constructor " + name );
				order.push( name );
				return owner;
			}
		} );
	}
	globalThis.requestAnimationFrame = () => 1;
	const runtime = startRuntime( {}, {} ), acquired = [ ...order ];
	runtime.dispose();
	runtime.dispose();
	assert.deepEqual( closed, acquired.filter( name => name !== "input" ).reverse() );
	for ( let index = 0; index < acquired.length; index++ ) {
		failure = acquired[index];
		closed.length = 0;
		order.length = 0;
		assert.throws( () => startRuntime( {}, {} ), new RegExp( "constructor " + failure ) );
		assert.deepEqual( closed, acquired.slice( 0, index ).filter( name => name !== "input" ).reverse() );
	}
	failure = null;
	closed.length = 0;
	globalThis.requestAnimationFrame = () => {
		throw Error( "schedule failure" );
	};
	assert.throws( () => startRuntime( {}, {} ), /schedule failure/ );
	assert.deepEqual( closed, acquired.filter( name => name !== "input" ).reverse() );
});

test("cleanup drains remaining owners when a disposer throws and never runs twice", t => {
	const previous = new Map(
		[ "__runtimeOwners", "requestAnimationFrame", "cancelAnimationFrame", "location" ].map(
			key => [ key, Object.getOwnPropertyDescriptor( globalThis, key ) ]
		)
	);
	t.after( () => {
		for ( const [key, value] of previous ) {
			if ( value ) Object.defineProperty( globalThis, key, value );
			else delete globalThis[key];
		}
	} );
	const closed = [],
		owners = Object.fromEntries(
			Object.keys( factories ).map( name => [ name, {
				/*
================
dispose
================
				*/
				dispose() {
					closed.push( name );
					if ( name === "characters" ) throw Error( "cleanup failure" );
				}
			} ] )
		);
	owners.platform.runningEntry = () => null;
	owners.platform.setMovementDump = () => {};
	owners.simulation.delivery = () => new Promise( () => {} );
	globalThis.__runtimeOwners = owners;
	globalThis.location = { search: "", origin: "http://localhost" };
	globalThis.requestAnimationFrame = () => 1;
	globalThis.cancelAnimationFrame = () => {};
	const runtime = startRuntime( {}, {} );
	assert.throws( () => runtime.dispose(), /Runtime cleanup failed/ );
	assert.deepEqual( [ ...closed ].sort(), Object.keys( factories ).filter( name => name !== "input" ).sort() );
	const count = closed.length;
	runtime.dispose();
	assert.equal( closed.length, count );
});
