/*
===========================================================================

runtime-own-displacement.test.mjs - the own cast displacement adoption

A published own cast displacement (#1569) is adopted by the runtime's
mover only while the mainThreadOwnWalk row is on and the segment is
live; otherwise the interpolated presenter keeps it. Drives the real
runtime across the synthetic boundary (helpers/runtime-boundary.mjs).

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";
import { frameOwners, startRuntime } from "../helpers/runtime-boundary.mjs";
test("a published own cast displacement is adopted only while the row is on and the segment is live", async t => {
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
	const destination = { ...start, x: 1200 };
	// The worker's publication as its own dash runs: the pose samples the
	// segment on the simulation clock, one revision a step.
	const gameState = /** @type {any} */ ({
		localGid: 1,
		pose: { ...start },
		poseAtMs: 0,
		movementRevision: 0,
		moving: true,
		movementAim: undefined,
		movementDisplacement: undefined,
		movementSpeedFactor: 1,
		walkPredictable: true,
		movementSpeed: 50,
		movementInputSeq: 0,
		navigationRequestId: 1
	});
	const moverFrames = [];
	let simNow = 0;
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
	owners.simulation.session = () => 1;
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
	owners["bug-report"].movementClock = () => {};
	owners.characters.simulationOrigin = () => {};
	// The frame clock is the test's own, with a named time origin: the
	// runtime subtracts performance.timeOrigin from the poll's clock to
	// place simulation time on it (runtime.ts), and a sibling test's fake
	// performance carries no timeOrigin to subtract.
	const t0 = performance.now();
	let fakeNow = t0;
	/** @type {any} */ (globalThis).performance = { now: () => fakeNow, timeOrigin: 0 };
	owners.simulation.poll = () => ({
		timeMs: simNow,
		sequence: 1,
		acceptedInputSequence: 0,
		clock: { originMs: t0 }
	});
	globalThis.__runtimeOwners = owners;
	globalThis.__runtimeCalls = [];
	/** @type {any} */ (globalThis).location = { search: "", origin: "http://localhost" };
	/** @type {any} */ (globalThis).document = { visibilityState: "visible" };
	let callback;
	globalThis.requestAnimationFrame = fn => {
		callback = fn;
		return 1;
	};
	globalThis.cancelAnimationFrame = () => {};
	const runtime = startRuntime( {}, {} );
	try {
		const calls = globalThis.__runtimeCalls ?? [];
		const uiEvent = defined( calls.find( call => call.owner === "platform" ) ).args[5];
		const drive = async now => {
			defined( callback )( now );
			for ( let i = 0; i < 32; i++ ) await Promise.resolve();
		};
		// The worker's pose on the published segment at simNow.
		const publishDash = ( from, to, startedAtMs, durationMs ) => {
			const travelled = durationMs ?
				Math.min( 1, Math.max( 0, (simNow - startedAtMs) / durationMs ) ) :
				1;
			gameState.pose = {
				...from,
				x: from.x + (to.x - from.x) * travelled,
				z: from.z + (to.z - from.z) * travelled
			};
			gameState.poseAtMs = simNow;
			gameState.movementRevision++;
			gameState.moving = durationMs > 0 && travelled < 1;
			gameState.movementDisplacement = { from, to, startedAtMs, durationMs };
		};
		const stepFrames = async ( count, publish ) => {
			for ( let i = 0; i < count; i++ ) {
				simNow += 16.7;
				fakeNow = t0 + simNow;
				if ( publish ) publish();
				await drive( t0 + simNow );
			}
		};
		const walking = () => moverFrames.filter( f => f.kind === "walking" );
		// Row off: the dash publishes and nothing is adopted; the mover
		// reports idle and the presenter keeps the displacement.
		await stepFrames( 5, () => publishDash( start, destination, 0, 400 ) );
		assert.equal( walking().length, 0, "the row-off displacement was adopted" );
		// Row on, finished and zero-duration publications: still nothing.
		uiEvent( { kind: "experimental-preferences", value: { mainThreadOwnWalk: true } } );
		await stepFrames( 5, () => publishDash( start, destination, -2000, 400 ) );
		assert.equal( walking().length, 0, "a finished segment was adopted" );
		await stepFrames( 5, () => publishDash( start, destination, simNow, 0 ) );
		assert.equal( walking().length, 0, "a zero-duration segment was adopted" );
		// Row on, a live segment: the drawn pose advances on the segment's
		// own clock (200 units in 400 ms) until the authored end.
		const dashStart = simNow;
		await stepFrames( 12, () => publishDash( start, destination, dashStart, 400 ) );
		const poses = walking().map( f => f.pose );
		assert.ok( poses.length > 0, "the live segment was never adopted" );
		for ( let i = 1; i < poses.length; i++ ) {
			const step = Math.hypot( poses[i].x - poses[i - 1].x, poses[i].z - poses[i - 1].z );
			assert.ok( step <= 500 * 1.04 * .0167 + .05, `drawn step ${step} outran the authored rate` );
			assert.ok( poses[i].x >= poses[i - 1].x, "the dash stalled or reversed" );
		}
		const mid = defined( poses.at( -1 ) );
		assert.ok( mid.x > start.x + 40 && mid.x < destination.x, `the dash is off its clock at ${mid.x}` );
		// The worker arrives and stands at the destination: the mover
		// hands the destination back and goes idle.
		await stepFrames( 40, () => publishDash( start, destination, dashStart, 400 ) );
		gameState.movementDisplacement = undefined;
		gameState.moving = false;
		gameState.pose = { ...destination };
		await stepFrames( 30, null );
		const ended = moverFrames.filter( f => f.kind === "ended" );
		assert.ok( ended.length > 0, "the mover never handed back" );
		const handback = defined( ended.at( -1 ) );
		assert.ok(
			Math.hypot( handback.pose.x - destination.x, handback.pose.z - destination.z ) < 1,
			"handed back away from the destination"
		);
		const afterEnd = moverFrames.slice( moverFrames.lastIndexOf( handback ) + 1 );
		assert.ok( !afterEnd.some( f => f.kind === "walking" ), "the walk resumed after the handback" );
	} finally {
		runtime.dispose();
	}
});
