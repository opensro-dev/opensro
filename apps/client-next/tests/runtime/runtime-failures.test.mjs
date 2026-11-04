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
import { defined } from "../helpers/defined.mjs";
import { factories, frameOwners, noTextureFeatures, startRuntime } from "../helpers/runtime-boundary.mjs";
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

test("an entity click predicts its approach leg only through the worker's own gates (#1567)", async t => {
	const keys = [
		"__runtimeOwners",
		"__runtimeCalls",
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
	const start = { regionId: 0x61a8, x: 1000, y: 0, z: 1000, angle: 0 };
	const entities = new Map( [
		[ 1, { gid: 1, kind: "player", ...start, appearanceState: [ 1, 0, 0 ] } ],
		[ 41, { gid: 41, kind: "npc", regionId: 0x61a8, x: 1300, y: 0, z: 1000, angle: 0 } ],
		[ 42, { gid: 42, kind: "ground-item", regionId: 0x61a8, x: 1100, y: 0, z: 1000, angle: 0 } ],
		[ 43, { gid: 43, kind: "ground-item", regionId: 0x61a8, x: 1005, y: 0, z: 1000, angle: 0 } ]
	] );
	/*
================
attempt

One booted runtime, one click on the picked entity under the given
published gates: the mover frames the click produced and the commands
that reached the worker.
================
	*/
	async function attempt( picked, gates ) {
		const gameState = {
			localGid: 1,
			pose: { ...start },
			poseAtMs: undefined,
			movementRevision: 0,
			moving: false,
			movementAim: undefined,
			movementSpeedFactor: 1,
			walkPredictable: gates.walk,
			pickupPredictable: gates.pickup,
			movementSpeed: 50,
			movementInputSeq: 0,
			navigationRequestId: 1,
			targetPending: false
		};
		const moverFrames = [], sessionCommands = [];
		let lastSeq = 0, callback;
		const owners = frameOwners( "characters", [], [], [] );
		owners.ui.event = () => {};
		owners.characters.step = () => {};
		owners.characters.cameraTarget = () => null;
		owners.characters.drawnPose = () => ({ ...start });
		owners.characters.ownMover = ( gid, frame ) => moverFrames.push( frame );
		owners.characters.ready = () => false;
		owners.characters.error = () => null;
		owners.characters.damageText = () => [];
		owners.characters.takeCameraScripts = () => [];
		owners.presentation.gameplay = () => gameState;
		owners.presentation.read = gid => entities.get( gid );
		owners.frontend.snapshot = () => ({ phase: "world" });
		owners.frontend.isRace = () => false;
		owners.frontend.isDock = () => false;
		owners.frontend.pointer = () => {};
		owners.frontend.select = () => {};
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
		owners.simulation.pollSession = () => ({ phase: "world", characters: [] });
		owners.renderer.experimentalVideo = () => {};
		owners.renderer.characterStats = () => ({});
		owners.renderer.worldStats = () => ({ visibleGroups: 0, pendingGroups: 0, pendingTextures: 0 });
		owners.renderer.phase = () => "running";
		owners.renderer.error = () => null;
		owners.renderer.gpuTiming = () => null;
		owners.renderer.frame = () => undefined;
		owners.renderer.audioListener = () => null;
		owners.renderer.setDamageText = () => {};
		owners.renderer.setTeleportGates = () => {};
		owners.renderer.pickEntity = () => picked;
		owners.renderer.pickGround = () => null;
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
		owners.ui.skinPreview = () => null;
		owners.platform.readUiViewport = () => ({ width: 1280, height: 720 });
		owners.platform.readViewport = () => ({ width: 1280, height: 720 });
		owners.platform.presentUi = () => {};
		owners.platform.presentLoading = () => {};
		owners.platform.presentWorldCursor = () => {};
		owners.platform.presentTelemetry = () => {};
		// #1593: the frame keeps a world-session tab awake.
		owners.platform.setKeepAwake = () => {};
		owners.platform.diagnosticsActive = () => false;
		owners.presentation.travel = () => null;
		owners.presentation.count = () => 0;
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
		globalThis.requestAnimationFrame = fn => {
			callback = fn;
			return 1;
		};
		globalThis.cancelAnimationFrame = () => {};
		const runtime = startRuntime( {}, {} );
		try {
			const calls = globalThis.__runtimeCalls ?? [];
			const platformArgs = defined( calls.find( call => call.owner === "platform" ) ).args;
			const uiEvent = defined( platformArgs[5] ), worldClick = defined( platformArgs[7] );
			const drive = async now => {
				defined( callback )( now );
				for ( let i = 0; i < 32; i++ ) await Promise.resolve();
			};
			let now = performance.now();
			await drive( now );
			uiEvent( { kind: "experimental-preferences", value: { mainThreadOwnWalk: true } } );
			worldClick( 10, 10 );
			for ( let i = 0; i < 30; i++ ) await drive( now += 16.7 );
			return { moverFrames, sessionCommands };
		} finally {
			runtime.dispose();
		}
	}
	const walked = frames => frames.filter( f => f.kind === "walking" ).map( f => f.pose );

	// A far select predicts its approach leg.
	{
		const { moverFrames, sessionCommands } = await attempt( 41, { walk: true, pickup: true } );
		assert.ok(
			sessionCommands.some( c => c.command?.kind === "select" && c.command.gid === 41 ),
			"the select never reached the worker"
		);
		const poses = walked( moverFrames );
		assert.ok( poses.length > 0, "the far select started no prediction" );
		assert.ok( defined( poses.at( -1 ) ).x > start.x + 5, "the body barely moved" );
		assert.ok( poses.every( p => p.x <= 1060 ), "the walk ran past the NPC's reach point" );
	}
	// A far pickup predicts its run-up under the pickup gate alone.
	for ( const gates of [ { walk: true, pickup: true }, { walk: false, pickup: true } ] ) {
		const { moverFrames, sessionCommands } = await attempt( 42, gates );
		assert.ok(
			sessionCommands.some( c => c.command?.kind === "pickup" && c.command.gid === 42 ),
			"the pickup never reached the worker"
		);
		const poses = walked( moverFrames );
		assert.ok( poses.length > 0, `the far pickup started no prediction (gates ${JSON.stringify( gates )})` );
		assert.ok( defined( poses.at( -1 ) ).x > start.x + 5, "the body barely moved" );
	}
	// An in-reach pickup is granted in place: no leg to predict.
	{
		const { moverFrames } = await attempt( 43, { walk: true, pickup: true } );
		assert.equal( walked( moverFrames ).length, 0, "an in-reach pickup was predicted" );
	}
	// The pickup gate closed (a held cast, any mount): the ground gate
	// being open must not predict a run-up the worker never makes.
	{
		const { moverFrames } = await attempt( 42, { walk: true, pickup: false } );
		assert.equal( walked( moverFrames ).length, 0, "a gated pickup was predicted" );
	}
	// The ground gate closed: a select predicts nothing.
	{
		const { moverFrames } = await attempt( 41, { walk: false, pickup: true } );
		assert.equal( walked( moverFrames ).length, 0, "a gated select was predicted" );
	}
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
	// #1593: the frame keeps a world-session tab awake.
	owners.platform.setKeepAwake = () => {};
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

test("terminal asset failures reach the session as asset incidents, once per episode", async t => {
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
	const closed = [], reports = [], sounds = [], commands = [];
	const owners = frameOwners( "characters", closed, reports, sounds );
	// The frame clock and the deliveries are the test's own (the hidden-tab
	// test's pattern): holding `now` holds every pace decision the runtime
	// makes, and a frame runs when a delivery settles past the starve line.
	// The loop reaches each delivery wait on a microtask chain, so frame()
	// lets it run until a wait is pending before releasing it.
	let now = 1000;
	const deliveries = [];
	globalThis.performance = { now: () => now };
	globalThis.document = { visibilityState: "visible" };
	globalThis.location = { search: "", origin: "http://localhost" };
	owners.simulation.delivery = () => new Promise( resolve => deliveries.push( resolve ) );
	globalThis.__runtimeOwners = owners;
	globalThis.requestAnimationFrame = () => 1;
	globalThis.cancelAnimationFrame = () => {};
	// The owner surface the frame reads on its way to the report block,
	// plus the failure handouts this test steers. The handout queues are
	// the owners' take-once contract: an empty queue hands out nothing.
	const worldHandouts = [], navigationHandouts = [];
	let characterError = null, rendererError = null;
	owners.simulation.session = command => {
		commands.push( command );
	};
	Object.assign( owners.world, {
		error: () => null,
		reconnecting: () => false,
		ready: () => false,
		progress: () => 0,
		loadingRegion: () => undefined,
		terminalFailure: () => worldHandouts.shift()
	} );
	Object.assign( owners.navigation, {
		error: () => null,
		reconnecting: () => false,
		phase: () => "idle",
		terminalFailure: () => navigationHandouts.shift()
	} );
	owners.characters.step = () => {};
	owners.characters.cameraTarget = () => null;
	owners.characters.drawnPose = () => null;
	owners.characters.ownMover = () => {};
	owners.characters.ready = () => false;
	owners.characters.damageText = () => [];
	owners.characters.takeCameraScripts = () => [];
	owners.characters.error = () => characterError;
	owners.ui.event = () => {};
	owners.ui.cursor = () => null;
	owners.renderer.experimentalVideo = () => {};
	owners.renderer.setDamageText = () => {};
	owners.renderer.setTeleportGates = () => {};
	owners.renderer.pickEntity = () => null;
	owners.renderer.audioListener = () => null;
	owners.world.retryTransient = () => {};
	owners.navigation.retryTransient = () => {};
	owners.platform.readViewport = () => ({ width: 800, height: 600 });
	owners.platform.presentWorldCursor = () => {};
	owners.platform.presentTelemetry = () => {};
	owners.platform.setKeepAwake = () => {};
	owners.presentation.travel = () => null;
	owners.presentation.read = () => undefined;
	owners.input.altHeld = () => false;
	Object.assign( owners.renderer, {
		worldStats: () => ({ visibleGroups: 0, pendingGroups: 0, pendingTextures: 0, residentGroups: 0 }),
		phase: () => rendererError ? "failed" : "ready",
		error: () => rendererError,
		adapterFacts: () => ({
			vendor: "nvidia",
			architecture: "ampere",
			features: [ "texture-compression-bc" ],
			limits: { maxBufferSize: 0, maxStorageBufferBindingSize: 0, maxTextureDimension2D: 0 }
		}),
		characterStats: () => ({ actors: 0, draws: 0 }),
		gpuTiming: () => undefined
	} );
	Object.assign( owners.platform, {
		presentUi() {},
		presentLoading() {},
		readUiViewport: () => ({ width: 800, height: 600 }),
		diagnosticsActive: () => false
	} );
	owners.assets.progress = () => null;
	Object.assign( owners.audio, {
		enqueue() {},
		ready: () => true,
		error: () => null,
		music() {},
		musicStatus: () => "stopped",
		prepareUi() {},
		prepareCombat() {}
	} );
	Object.assign( owners.ui, {
		resourceError: () => null,
		stats: () => ({ pending: 0, failed: [], error: undefined }),
		runtimeError() {}
	} );
	owners["bug-report"].note = () => {};
	owners["build-info"].readout = () => "";
	owners.input.dropNamesHeld = () => false;
	owners.input.blindHeld = () => false;
	owners.presentation.count = () => 0;
	const runtime = startRuntime( {}, {} );
	// A failed assertion must not leak a running runtime: it keeps node alive.
	try {
		const frame = async () => {
			now += 1000;
			for ( let i = 0; i < 64 && deliveries.length === 0; i++ ) await Promise.resolve();
			defined( deliveries.shift() )();
			for ( let i = 0; i < 32; i++ ) await Promise.resolve();
		};
		const assetReports = () => commands.filter( c => c.kind === "client-report" );
		await frame();
		assert.deepEqual( assetReports(), [], "no failure, no incident" );
		worldHandouts.push( {
			error: "Error: /assets/a.json: Load failed",
			path: "/assets/a.json",
			transient: true,
			attempts: 8
		} );
		await frame();
		assert.deepEqual( assetReports(), [ {
			kind: "client-report",
			incidentKind: "asset",
			code: "world_asset_failed",
			message: "asset=/assets/a.json transient=true attempts=8 error=Error: /assets/a.json: Load failed"
		} ] );
		await frame();
		assert.equal( assetReports().length, 1, "the same episode is not filed twice" );
		characterError = "Missing height";
		await frame();
		assert.equal( assetReports().length, 2, "a fatal resource failure files beside the stream lanes" );
		assert.deepEqual( assetReports()[1], {
			kind: "client-report",
			incidentKind: "asset",
			code: "fatal_asset_failed",
			message: "asset=- transient=false attempts=0 error=Characters: Missing height"
		} );
		navigationHandouts.push( {
			error: "Error: Asset HTTP 404",
			path: "/assets/world/world-region-catalog.json",
			transient: false,
			attempts: 0
		} );
		worldHandouts.push( {
			error: "Error: /assets/b.json: Load failed",
			path: "/assets/b.json",
			transient: true,
			attempts: 8
		} );
		await frame();
		assert.equal( assetReports().length, 4, "a later episode files again under its own facts" );
		assert.equal( assetReports()[2].code, "world_asset_failed" );
		assert.equal( assetReports()[3].code, "navigation_asset_failed" );
		rendererError = "D3D12 device removed (DXGI_ERROR_DEVICE_HUNG): the GPU device instance has been suspended";
		await frame();
		assert.equal( assetReports().length, 5, "the renderer failure files before dispose" );
		assert.deepEqual( assetReports()[4], {
			kind: "client-report",
			incidentKind: "asset",
			code: "renderer_failed",
			message: "asset=- transient=false attempts=0 error=D3D12 device removed (DXGI_ERROR_DEVICE_HUNG): " +
				"the GPU device instance has been suspended | adapter: nvidia/ampere features=texture-compression-bc"
		} );
		assert.ok( closed.length > 0, "the runtime disposed after filing" );
	} finally {
		runtime.dispose();
	}
});
