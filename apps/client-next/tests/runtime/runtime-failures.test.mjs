/*
===========================================================================

runtime-failures.test.mjs - runtime owner failure and teardown contracts

Injects owners through the synthetic build boundary and verifies failures
reach the player while each owned lifetime is disposed exactly once. The
boundary also records every factory call, so the wiring the composition
hands from one owner to another is itself testable.

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
					contents: `export function ${factories[args.path]}( ...args ) {
( globalThis.__runtimeCalls ??= [] ).push( { owner: ${JSON.stringify( args.path )}, args } );
return globalThis.__runtimeOwners[${JSON.stringify( args.path )}];}`
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
noTextureFeatures

Every fake owner's texture-features hook: the renderer's resolves empty
(no variant), the asset owner's ignores what it is given.
================
*/
function noTextureFeatures() {
	return Promise.resolve( [] );
}

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
			textureFeatures: noTextureFeatures,
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
		setAssetDump() {},
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
	// The main-thread mover's per-frame state (#1440).
	owners.characters.ownMover = () => {};
	owners.characters.drawnPose = () => undefined;
	owners.characters.step = () => {
		throw new Error( "injected presentation failure" );
	};
	Object.assign( owners.characters, { mallOutfit: () => {}, mallPreviewState: () => ({ wearable: [] }) } );
	Object.assign( owners.ui, {
		mallPreview: () => null,
		mallPreviewState: () => {},
		debugClockSeed: seed => seed
	} );
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

test("a hidden tab runs frames from worker deliveries; a visible one waits for its RAF until starved", async t => {
	const keys = [
		"__runtimeOwners",
		"requestAnimationFrame",
		"cancelAnimationFrame",
		"location",
		"document",
		"performance"
	];
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
		// The frame clock and the deliveries are the test's own. The runtime
		// reads performance.now() for every pace and starvation decision
		// (runtime.ts hiddenFrame), so holding `now` holds them all; a
		// delivery settles only when the test settles it. No wall time: a
		// loaded machine used to stretch this test's real 150 ms wait past
		// the 250 ms starvation line and run the visible branch's frame
		// early (issue #1445).
		let now = 1000;
		const deliveries = [];
		globalThis.performance = { now: () => now };
		owners.simulation.delivery = () => new Promise( resolve => deliveries.push( resolve ) );
		let rafs = 0, cancelled = 0;
		globalThis.document = { visibilityState };
		globalThis.location = { search: "", origin: "http://localhost" };
		globalThis.__runtimeOwners = owners;
		globalThis.requestAnimationFrame = () => ++rafs;
		globalThis.cancelAnimationFrame = () => cancelled++;
		const runtime = startRuntime( {}, {} );
		// A failed assertion must not leak a running runtime: it keeps node alive.
		try {
			// Settle the delivery the frame loop is waiting on, then let the
			// loop's promise chain run out (microtasks only, no timers).
			const deliver = async () => {
				defined( deliveries.shift() )();
				for ( let i = 0; i < 32; i++ ) await Promise.resolve();
			};
			await deliver();
			assert.equal( reports.length, 0, `${visibilityState} frames only when its pace is due` );
			// Visible: due only 250 ms after the last frame finished (the
			// starvation line). Hidden: due at the 100 ms pace after the
			// last frame. One millisecond short of either, still no frame.
			now += visibilityState === "visible" ? 249 : 99;
			await deliver();
			assert.equal( reports.length, 0, `${visibilityState} still waits short of its line` );
			// On the line the delivery runs the frame, as a window the
			// browser stopped drawing must.
			now += 1;
			await deliver();
			assert.equal( reports.length, 1, `${visibilityState} frames off a delivery` );
			assert.match( reports[0], /injected presentation failure/ );
			assert.equal( cancelled >= 1, true, "the pending RAF is retired" );
		} finally {
			runtime.dispose();
		}
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
			textureFeatures: noTextureFeatures,
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
	globalThis.__runtimeOwners.platform.setAssetDump = () => {};
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
				textureFeatures: noTextureFeatures,
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
	owners.platform.setAssetDump = () => {};
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

test("a move command starts a mover prediction while the row is on, and only then", async t => {
	const keys = [
		"__runtimeOwners",
		"__runtimeCalls",
		"requestAnimationFrame",
		"cancelAnimationFrame",
		"location",
		"document"
	];
	const previous = new Map( keys.map( key => [ key, Object.getOwnPropertyDescriptor( globalThis, key ) ] ) );
	t.after( () => {
		for ( const [key, value] of previous ) {
			if ( value ) Object.defineProperty( globalThis, key, value );
			else delete globalThis[key];
		}
	} );
	const start = { regionId: 0x61a8, x: 1000, y: 0, z: 1000, angle: 0 };
	const gameState = {
		localGid: 1,
		pose: { ...start },
		poseAtMs: undefined,
		movementRevision: 0,
		moving: false,
		movementAim: undefined,
		movementSpeedFactor: 1,
		walkPredictable: true,
		movementSpeed: 50,
		movementInputSeq: 0,
		navigationRequestId: 1
	};
	const moverFrames = [], sessionCommands = [];
	let lastSeq = 0;
	const owners = frameOwners( "characters", [], [], [] );
	owners.ui.event = () => {};
	owners.characters.step = () => {};
	owners.characters.cameraTarget = () => null;
	owners.characters.drawnPose = () => ({ ...start });
	owners.characters.ownMover = ( gid, frame ) => moverFrames.push( frame );
	owners.presentation.gameplay = () => gameState;
	owners.navigation.view = () => ({
		requestId: 1,
		navigation: {
			available: () => true,
			block: () => 0,
			floor: () => undefined,
			pick: query => ({ ...query.ray.target }),
			clip: ( _from, to, query ) => {
				if ( query ) query.status = 0;
				return to;
			},
			surface: pose => pose
		}
	});
	owners.simulation.session = command => {
		sessionCommands.push( command );
		return ++lastSeq;
	};
	owners.renderer.experimentalVideo = () => {};
	owners.renderer.characterStats = () => ({});
	owners.characters.ready = () => false;
	owners.characters.error = () => null;
	owners.characters.damageText = () => [];
	owners.characters.takeCameraScripts = () => [];
	owners.renderer.worldStats = () => ({ visibleGroups: 0, pendingGroups: 0, pendingTextures: 0 });
	owners.renderer.phase = () => "running";
	owners.renderer.error = () => null;
	owners.renderer.gpuTiming = () => null;
	owners.renderer.frame = () => undefined;
	owners.renderer.audioListener = () => null;
	owners.renderer.setDamageText = () => {};
	owners.renderer.setTeleportGates = () => {};
	owners.renderer.pickEntity = () => null;
	owners.world.ready = () => false;
	owners.world.error = () => null;
	owners.world.reconnecting = () => false;
	owners.world.loadingRegion = () => null;
	owners.world.progress = () => 0;
	owners.world.retryTransient = () => {};
	owners.navigation.phase = () => "pending";
	owners.navigation.error = () => null;
	owners.navigation.reconnecting = () => false;
	owners.navigation.retryTransient = () => {};
	owners.audio.music = () => {};
	owners.audio.prepareUi = () => {};
	owners.audio.prepareCombat = () => {};
	owners.audio.ready = () => false;
	owners.audio.error = () => null;
	owners.audio.musicStatus = () => "idle";
	owners.ui.cursor = () => null;
	owners.ui.stats = () => ({ pending: 0, failed: [] });
	owners.ui.resourceError = () => null;
	owners.platform.readUiViewport = () => ({ width: 1280, height: 720 });
	owners.platform.readViewport = () => ({ width: 1280, height: 720 });
	owners.platform.presentUi = () => {};
	owners.platform.presentLoading = () => {};
	owners.platform.presentWorldCursor = () => {};
	owners.platform.presentTelemetry = () => {};
	owners.platform.diagnosticsActive = () => false;
	owners.presentation.travel = () => null;
	owners.presentation.count = () => 0;
	owners.presentation.read = () => undefined;
	owners["build-info"].readout = () => "";
	owners.input.dropNamesHeld = () => false;
	owners.input.blindHeld = () => false;
	owners.input.altHeld = () => false;
	owners.assets.progress = () => 0;
	owners["bug-report"].movement = () => {};
	globalThis.__runtimeOwners = owners;
	globalThis.__runtimeCalls = [];
	globalThis.location = { search: "", origin: "http://localhost" };
	globalThis.document = { visibilityState: "visible" };
	let callback;
	globalThis.requestAnimationFrame = fn => {
		callback = fn;
		return 1;
	};
	globalThis.cancelAnimationFrame = () => {};
	const runtime = startRuntime( {}, {} );
	try {
		const calls = globalThis.__runtimeCalls ?? [];
		const sessionCommand = defined( calls.find( call => call.owner === "ui" ) ).args[1];
		const uiEvent = defined( calls.find( call => call.owner === "platform" ) ).args[5];
		const drive = async now => {
			defined( callback )( now );
			for ( let i = 0; i < 32; i++ ) await Promise.resolve();
		};
		const t0 = performance.now();
		await drive( t0 );
		const move = {
			kind: "gameplay",
			command: {
				kind: "move",
				destination: { regionId: 0x61a8, x: 1300, y: 0, z: 1000, angle: 0 }
			}
		};
		// Row off: the command reaches the worker and no frame walks.
		sessionCommand( move );
		await drive( t0 + 100 );
		assert.ok(
			sessionCommands.some( c => c.kind === "gameplay" && c.command.kind === "move" ),
			"the move never reached the worker"
		);
		assert.ok( !moverFrames.some( f => f.kind === "walking" ), "the presenter lost the walk with the row off" );
		// Row on but the worker's ground-walk gate closed: still nothing.
		uiEvent( { kind: "experimental-preferences", value: { mainThreadOwnWalk: true } } );
		gameState.walkPredictable = false;
		sessionCommand( move );
		await drive( t0 + 200 );
		assert.ok( !moverFrames.some( f => f.kind === "walking" ), "a gated move was predicted" );
		// Row on, gate open: the body walks toward the destination.
		gameState.walkPredictable = true;
		sessionCommand( move );
		let now = t0 + 200;
		for ( let i = 0; i < 30; i++ ) await drive( now += 16.7 );
		const poses = moverFrames.filter( f => f.kind === "walking" ).map( f => f.pose );
		assert.ok( poses.length > 0, "the move never started a prediction" );
		for ( let i = 1; i < poses.length; i++ ) {
			assert.ok( poses[i].x >= poses[i - 1].x, "the walk stalled or reversed" );
			const step = Math.hypot( poses[i].x - poses[i - 1].x, poses[i].z - poses[i - 1].z );
			assert.ok( step <= 50 * 1.04 * .0167 + .05, `drawn step ${step}` );
		}
		assert.ok( defined( poses.at( -1 ) ).x > start.x + 5, "the body barely moved" );
		// A foreign-region goal starts no prediction of its own: the walk in
		// flight keeps heading for the goal it was predicted with.
		moverFrames.length = 0;
		sessionCommand( {
			kind: "gameplay",
			command: { kind: "move", destination: { regionId: 0x61a9, x: 10, y: 0, z: 10, angle: 0 } }
		} );
		for ( let i = 0; i < 5; i++ ) await drive( now += 16.7 );
		const after = moverFrames.filter( f => f.kind === "walking" ).map( f => f.pose );
		assert.ok( after.length > 0, "the walk in flight stopped on a foreign-region move" );
		for ( const pose of after ) {
			assert.equal( pose.z, start.z, "the walk bent toward the foreign goal" );
			assert.ok( pose.x > start.x && pose.x < 1300, "the walk bent toward the foreign goal" );
		}
	} finally {
		runtime.dispose();
	}
});

test("the runtime hands the UI a field-of-view extension that reaches the world owner", t => {
	const keys = [
		"__runtimeOwners",
		"__runtimeCalls",
		"requestAnimationFrame",
		"cancelAnimationFrame",
		"location"
	];
	const previous = new Map( keys.map( key => [ key, Object.getOwnPropertyDescriptor( globalThis, key ) ] ) );
	t.after( () => {
		for ( const [key, value] of previous ) {
			if ( value ) Object.defineProperty( globalThis, key, value );
			else delete globalThis[key];
		}
	} );
	const fov = [];
	const owners = Object.fromEntries(
		Object.keys( factories ).map( name => [ name, {
			textureFeatures: noTextureFeatures,
			/*
================
dispose
================
			*/
			dispose() {}
		} ] )
	);
	owners.platform.runningEntry = () => null;
	owners.platform.setMovementDump = () => {};
	owners.platform.setAssetDump = () => {};
	owners.simulation.delivery = () => new Promise( () => {} );
	owners.world.setFieldOfView = degrees => fov.push( degrees );
	globalThis.__runtimeOwners = owners;
	globalThis.__runtimeCalls = [];
	globalThis.location = { search: "", origin: "http://localhost" };
	globalThis.requestAnimationFrame = () => 1;
	globalThis.cancelAnimationFrame = () => {};
	const runtime = startRuntime( {}, {} );
	try {
		const call = (globalThis.__runtimeCalls ?? []).find( recorded => recorded.owner === "ui" );
		const extensions = defined( call ).args.at( -1 );
		assert.equal(
			typeof extensions.setWorldFieldOfView,
			"function",
			"the UI never received the field-of-view extension"
		);
		extensions.setWorldFieldOfView( 90 );
		extensions.setWorldFieldOfView( 45 );
		assert.deepEqual( fov, [ 90, 45 ], "the degrees never reached the world owner" );
	} finally {
		runtime.dispose();
	}
});
