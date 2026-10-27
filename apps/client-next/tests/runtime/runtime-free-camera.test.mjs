/*
===========================================================================

runtime-free-camera.test.mjs - the runtime's free-camera wiring (#1787)

The world stream owns the free-camera byte (CGInterface +0x3CE); the
runtime reads it where native code tests it outside the fly: the
selection decal 68FE62 zeroes every frame, the arrow-key presses
Game_OnKeyDown skips (680ACF, 680B27, 680B5F), and the third-person
yaw lock of 68F830, which the byte skips. It also installs the fly's
poll: the input owner's held keys and its yaw/pitch write-back. Drives
the real runtime across the synthetic boundary
(helpers/runtime-boundary.mjs).

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";
import { frameOwners, startRuntime } from "../helpers/runtime-boundary.mjs";

const POSE = { regionId: 0x61a8, x: 1000, y: 0, z: 1000, angle: 0 };
const VK_W = 0x57;

/*
================
freeCameraRuntime

The runtime in the world phase with a ground selection decal, recording
the decals, the camera reads and the arrow commands it sends.
================
*/
function freeCameraRuntime( t ) {
	const keys = [
		"__runtimeOwners",
		"__runtimeCalls",
		"requestAnimationFrame",
		"cancelAnimationFrame",
		"location",
		"document",
		"performance",
		"HTMLInputElement",
		"HTMLTextAreaElement"
	];
	const previous = new Map( keys.map( key => [ key, Object.getOwnPropertyDescriptor( globalThis, key ) ] ) );
	const gameState = /** @type {any} */ ({
		localGid: 1,
		pose: { ...POSE },
		poseAtMs: 0,
		movementRevision: 0,
		selectionDecal: { kind: "ground", pose: { ...POSE } }
	});
	const record = { free: false, decals: [], playerYaws: [], sent: [], angles: [], flyInput: null };
	const owners = frameOwners( "characters", [], [], [] );
	owners.ui.event = () => {};
	owners.characters.step = () => {};
	owners.characters.cameraTarget = () => ({ pose: { ...POSE }, height: 20, mounted: false, yaw: 1.25 });
	owners.characters.drawnPose = () => ({ ...POSE });
	owners.presentation.gameplay = () => gameState;
	owners.navigation.view = () => null;
	owners.simulation.session = command => {
		record.sent.push( command );
		return undefined;
	};
	let session = /** @type {any} */ ({ phase: "world" });
	owners.simulation.pollSession = () => {
		const state = session;
		session = null;
		return state;
	};
	owners.frontend.step = () => ({ phase: "world" });
	owners.frontend.snapshot = () => ({ phase: "world" });
	owners.renderer.experimentalVideo = () => {};
	owners.renderer.characterStats = () => ({});
	owners.renderer.setSelectionDecal = value => record.decals.push( value );
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
	owners.world.freeCamera = () => record.free;
	owners.world.setFreeCameraInput = value => {
		record.flyInput = value;
	};
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
	owners.input.keyHeld = vk => vk === VK_W;
	owners.input.setCameraAngles = ( yaw, pitch ) => record.angles.push( [ yaw, pitch ] );
	owners.input.camera = playerYaw => {
		record.playerYaws.push( playerYaw );
		return { yaw: 0, pitch: 0, distance: 80 };
	};
	owners.assets.progress = () => 0;
	owners["bug-report"].movement = () => {};
	owners["bug-report"].movementClock = () => {};
	owners.characters.simulationOrigin = () => {};
	let now = 1000;
	/** @type {any} */ (globalThis).performance = { now: () => now, timeOrigin: 0 };
	owners.simulation.poll = () => ({ timeMs: now, sequence: 1, acceptedInputSequence: 0, clock: { originMs: 0 } });
	// The world phase reaches members the shared fakes do not carry:
	// each answers nothing, which the world-phase frame tolerates.
	for ( const name of Object.keys( owners ) ) {
		owners[name] = new Proxy( owners[name], {
			/*
			================
			get
			================
			*/
			get( target, key ) {
				if ( key in target || typeof key === "symbol" || key === "then" ) return target[key];
				return () => undefined;
			}
		} );
	}
	globalThis.__runtimeOwners = owners;
	globalThis.__runtimeCalls = [];
	/** @type {any} */ (globalThis).location = { search: "", origin: "http://localhost" };
	// The text-field classes the focus test reads; the chat edit is one.
	class TextInput {}
	/** @type {any} */ (globalThis).HTMLInputElement = TextInput;
	/** @type {any} */ (globalThis).HTMLTextAreaElement = class {};
	const document = { visibilityState: "visible", activeElement: /** @type {any} */ (null) };
	/** @type {any} */ (globalThis).document = document;
	let callback;
	globalThis.requestAnimationFrame = fn => {
		callback = fn;
		return 1;
	};
	globalThis.cancelAnimationFrame = () => {};
	const runtime = startRuntime( {}, {} );
	// Dispose before the globals it cancels its frame through go back.
	t.after( () => {
		runtime.dispose();
		for ( const [key, value] of previous ) {
			if ( value ) Object.defineProperty( globalThis, key, value );
			else delete globalThis[key];
		}
	} );
	const calls = globalThis.__runtimeCalls ?? [];
	const onKey = defined( calls.find( call => call.owner === "input" ) ).args[0];
	/*
	================
	frame
	================
	*/
	const frame = async () => {
		now += 16;
		defined( callback )( now );
		for ( let i = 0; i < 32; i++ ) await Promise.resolve();
	};
	return { record, frame, onKey, document, TextInput };
}

/*
================
test("68FE62: the selection decal is dropped while the free camera stands")
================
*/
test("68FE62: the selection decal is dropped while the free camera stands", async t => {
	const f = freeCameraRuntime( t );
	await f.frame();
	assert.ok( f.record.decals.at( -1 ), "the ground decal shows with the byte clear" );
	f.record.free = true;
	await f.frame();
	assert.equal( f.record.decals.at( -1 ), null, "the decal still shows under the free camera" );
	f.record.free = false;
	await f.frame();
	assert.ok( f.record.decals.at( -1 ), "the decal returns once the byte clears" );
});

/*
================
test("68F830: the third-person yaw lock is skipped while the free camera stands")
================
*/
test("68F830: the third-person yaw lock is skipped while the free camera stands", async t => {
	const f = freeCameraRuntime( t );
	await f.frame();
	assert.equal( f.record.playerYaws.at( -1 ), 1.25, "the follow passes the drawn body's yaw" );
	f.record.free = true;
	await f.frame();
	assert.equal( f.record.playerYaws.at( -1 ), undefined, "the free camera passed a player yaw" );
});

/*
================
test("680ACF/680B27/680B5F: arrow presses do not walk while the free camera stands")
================
*/
test("680ACF/680B27/680B5F: arrow presses do not walk while the free camera stands", async t => {
	const f = freeCameraRuntime( t );
	await f.frame();
	const arrows = () => f.record.sent.filter( command => command.command?.kind === "arrow-key" );
	f.record.free = true;
	for ( const code of [ "ArrowUp", "ArrowLeft", "ArrowRight" ] ) {
		f.onKey( { kind: "key", code, down: true, timeMs: 1 } );
	}
	assert.equal( arrows().length, 0, "an arrow press walked under the free camera" );
	f.record.free = false;
	f.onKey( { kind: "key", code: "ArrowUp", down: true, timeMs: 2 } );
	assert.deepEqual( arrows().map( command => [ command.command.key, command.command.down ] ), [ [ "up", true ] ] );
});

/*
================
test("68FE50: the fly polls the input owner's keys and writes its angles back")
================
*/
test("68FE50: the fly polls the input owner's keys and writes its angles back", async t => {
	const f = freeCameraRuntime( t );
	const input = defined( f.record.flyInput );
	assert.equal( input.held( VK_W ), true );
	assert.equal( input.held( 0x53 ), false );
	input.turn( -.04, .3 );
	assert.deepEqual( f.record.angles, [ [ -.04, .3 ] ] );
	assert.equal( input.typing(), false, "no text field has focus" );
	f.document.activeElement = new f.TextInput();
	assert.equal( input.typing(), true, "the chat edit's focus skips the fly" );
});
