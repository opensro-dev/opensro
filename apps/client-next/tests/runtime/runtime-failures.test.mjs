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
	release: "createReleaseWatch"
};
const compiled = await build( {
	entryPoints: [ "src/engine/runtime/runtime.ts" ],
	bundle: true,
	platform: "node",
	format: "esm",
	write: false,
	define: {
		"import.meta.env.DEV": "false",
		"import.meta.env.VITE_AGENT_API_BASE": "undefined",
		"import.meta.url": JSON.stringify( "http://localhost/runtime.ts" )
	},
	plugins: [ {
		name: "runtime-owners",
		setup( build ) {
			build.onResolve( { filter: /^\.\// }, args => {
				const owner = args.path.split( "/" )[1];
				if ( factories[owner] ) return { path: owner, namespace: "owner" };
			} );
			build.onLoad(
				{ filter: /.*/, namespace: "owner" },
				args => ({
					contents: `export function ${
						factories[args.path]
					}(){return globalThis.__runtimeOwners.${args.path};}`
				})
			);
		}
	} ]
} );
const { startRuntime } = await import(
	"data:text/javascript;base64," + Buffer.from( compiled.outputFiles[0].contents ).toString( "base64" )
);
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
		const owners = Object.fromEntries(
			Object.keys( factories ).map( key => [ key, {
				step() {},
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
		Object.assign( owners.platform, {
			report: value => reports.push( value ),
			runningEntry: () => null,
			visibilityReturned: () => false,
			presentUpdate() {}
		} );
		owners.release.newerAvailable = () => false;
		Object.assign( owners.input, {
			drain: () => null,
			error: () => null,
			camera: () => ({ yaw: 0, pitch: 0, distance: 80 })
		} );
		Object.assign( owners.simulation, {
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
		owners.characters.eventRain = () => false;
		owners.characters.orbGauge = () => ({ authoritative: 0, displayed: 0, pending: 0 });
		owners.characters.receiveFeedback = () => {};
		owners.characters.step = () => {
			throw new Error( "injected presentation failure" );
		};
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
			dispose() {
				closed.push( name );
			}
		} ] )
	);
	globalThis.__runtimeOwners.platform.runningEntry = () => null;
	for ( const name of Object.keys( factories ) ) {
		const owner = globalThis.__runtimeOwners[name];
		Object.defineProperty( globalThis.__runtimeOwners, name, {
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
				dispose() {
					closed.push( name );
					if ( name === "characters" ) throw Error( "cleanup failure" );
				}
			} ] )
		);
	owners.platform.runningEntry = () => null;
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
