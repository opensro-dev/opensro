/*
===========================================================================

character-animation-audio.test.mjs - tests for the client modules it imports

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
/*
================
load
================
*/
async function load( file ) {
	return import( sourceFileUrl( file ).href );
}
const { createPresentationRandom } = await load( "src/engine/runtime/random/random.ts" );
const { createCharacterPose } = await load( "src/engine/runtime/renderer/characters/animation/animation.ts" );
const { createCharacterSounds } = await load( "src/engine/runtime/characters/sounds/sounds.ts" );
const { createAudio } = await load( "src/engine/runtime/audio/audio.ts" );
const { createCharacters } = await load( "src/engine/runtime/renderer/characters/characters.ts" );
const identity = () => new Float32Array( [ 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 ] );
const node = ( name, parent = -1, translation = [ 0, 0, 0 ] ) => ({
	name,
	parent,
	translation,
	rotation: [ 0, 0, 0, 1 ],
	scale: [ 1, 1, 1 ]
});
/*
================
model
================
*/
function model( nodes = [ node( "root" ) ], channels = [] ) {
	return {
		nodes,
		images: [],
		clips: [ { name: "stand", duration: 1, channels } ],
		primitives: [ {
			name: "mesh",
			node: 0,
			joints: [ 0 ],
			inverseBind: identity(),
			image: -1,
			geometry: {
				positions: new Float32Array( 9 ),
				indices: new Uint32Array( [ 0, 1, 2 ] ),
				transform: identity()
			}
		} ]
	};
}
test("preview identity zero has no implicit parent and owns an isolated camera transform", () => {
	const c = createCharacters();
	c.model( "preview", model(), [] );
	const uploaded = [], transforms = [];
	const gpu = {
		/*
================
upload
================
		*/
		upload( data ) {
			uploaded.push( data );
			return {};
		},
		/*
================
updateInstances
================
		*/
		updateInstances( draw ) {
			return draw;
		},
		/*
================
updateBones
================
		*/
		updateBones() {},
		/*
================
updateTransform
================
		*/
		updateTransform( draw, matrix ) {
			transforms.push( matrix.slice() );
		},
		/*
================
release
================
		*/
		release() {}
	};
	c.actors( [ {
		gid: 0,
		model: "preview",
		pose: { regionId: 0, x: 0, y: 0, z: 0, yaw: 0 },
		scale: 1,
		clip: "stand",
		time: 0,
		loop: true
	} ] );
	c.prepare( gpu, {}, 0, identity(), true );
	assert.equal( uploaded.length, 1 );
	assert.equal( uploaded[0].world, false );
	assert.equal( uploaded[0].material.fogDisabled, true );
	const changed = identity();
	changed[0] = .5;
	c.prepare( gpu, {}, 0, changed, true );
	assert.equal( uploaded.length, 1 );
	assert.deepEqual( transforms.at( -1 ), changed );
	c.dispose( gpu, null );
});
test("native effect rotation reaches GPU instances and invalidates an otherwise unchanged pose", () => {
	const c = createCharacters();
	c.model( "effect", model(), [] );
	const matrices = [];
	const gpu = {
		/*
================
upload
================
		*/
		upload( data ) {
			matrices.push( data.instances.slice() );
			return {};
		},
		/*
================
updateInstances
================
		*/
		updateInstances( draw, data ) {
			matrices.push( data.slice() );
			return draw;
		},
		/*
================
updateBones
================
		*/
		updateBones() {},
		/*
================
release
================
		*/
		release() {}
	};
	const actor = {
		gid: -1,
		model: "effect",
		pose: { regionId: 257, x: 4, y: 10, z: 0, yaw: 0 },
		effectRotation: { axis: "z", angle: Math.PI / 2 },
		scale: 1,
		clip: "stand",
		time: 0,
		loop: false,
		pickable: false
	};
	c.actors( [ actor ] );
	c.prepare( gpu, {}, 257 );
	assert.ok( Math.abs( matrices.at( -1 )[0] ) < 1e-6 );
	assert.equal( matrices.at( -1 )[1], 1 );
	assert.equal( matrices.at( -1 )[12], 4 );
	assert.equal( matrices.at( -1 )[13], 10 );
	actor.effectRotation = { axis: "x", angle: Math.PI / 2 };
	c.actors( [ actor ] );
	c.prepare( gpu, {}, 257 );
	assert.equal( matrices.length, 2 );
	assert.equal( matrices.at( -1 )[0], 1 );
	assert.equal( matrices.at( -1 )[6], 1 );
	assert.equal( matrices.at( -1 )[9], -1 );
	actor.effectRotation = { axis: "y", angle: Math.PI / 2 };
	c.actors( [ actor ] );
	c.prepare( gpu, {}, 257 );
	assert.equal( matrices.length, 3 );
	assert.ok( Math.abs( matrices.at( -1 )[0] ) < 1e-6 );
	assert.equal( matrices.at( -1 )[2], 1 );
	assert.equal( matrices.at( -1 )[8], -1 );
	c.dispose( gpu, null );
});

test("native root effects use compound orientation, one holder-space offset and no animated root pose", () => {
	const c = createCharacters();
	c.model( "body", model( [ node( "root", -1, [ 500, 600, 700 ] ) ] ), [] );
	const yaw = .7,
		body = {
			gid: 1,
			model: "body",
			pose: { regionId: 257, x: 100, y: 50, z: 200, yaw: Math.PI - yaw },
			scale: 2,
			bodyVolume: { index: 4, female: false },
			clip: "stand",
			time: 0,
			loop: false
		};
	const effect = {
		gid: 2,
		model: "effect",
		pose: { regionId: 257, x: 999, y: 999, z: 999, yaw: 0 },
		scale: 1,
		clip: "",
		time: 0,
		loop: false,
		absoluteEffectScale: true,
		attachment: { gid: 1, root: true, bone: "", basis: "native", offset: [ 2, 10, -13 ] }
	};
	const m = c.matrix( [ body, effect ], 2 ),
		co = Math.cos( yaw ),
		si = Math.sin( yaw ),
		near = ( a, b ) => assert.ok( Math.abs( a - b ) < 1e-4, `${a} != ${b}` );
	near( m[0], co );
	near( m[2], si );
	near( m[8], -si );
	near( m[10], co );
	near( m[12], 100 + 2 * (2 * co + 13 * si) );
	near( m[13], 70 );
	near( m[14], 200 + 2 * (2 * si - 13 * co) );
});

test("body volume changes radial skinning while height remains uniform and cached poses invalidate", () => {
	const c = createCharacters();
	c.model( "body", model( [ node( "Bip01 Spine" ) ] ), [] );
	const matrices = [], bones = [];
	const gpu = {
		/*
================
upload
================
		*/
		upload( data ) {
			matrices.push( data.instances.slice() );
			bones.push( data.bones.slice() );
			return {};
		},
		/*
================
updateInstances
================
		*/
		updateInstances( draw, data ) {
			matrices.push( data.slice() );
			return draw;
		},
		/*
================
updateBones
================
		*/
		updateBones( draw, data ) {
			bones.push( data.slice() );
		},
		/*
================
release
================
		*/
		release() {}
	};
	const actor = {
		gid: 1,
		model: "body",
		pose: { regionId: 257, x: 0, y: 0, z: 0, yaw: 0 },
		scale: 1.06,
		bodyVolume: { index: 0, female: false },
		clip: "stand",
		time: 0,
		loop: true
	};
	c.actors( [ actor ] );
	c.prepare( gpu, {}, 257 );
	for ( const n of [ 0, 5, 10 ] ) assert.ok( Math.abs( matrices.at( -1 )[n] - 1.06 ) < 1e-6 );
	assert.equal( bones.at( -1 )[0], 1 );
	assert.ok( Math.abs( bones.at( -1 )[5] - .88 ) < 1e-6 );
	actor.bodyVolume = { index: 4, female: false };
	c.actors( [ actor ] );
	c.prepare( gpu, {}, 257 );
	assert.ok( Math.abs( bones.at( -1 )[5] - 1.1 ) < 1e-6 );
	assert.throws( () => c.actors( [ { ...actor, bodyVolume: { index: NaN, female: false } } ] ), /volume/ );
	c.dispose( gpu, null );
});

test("projectile sockets sample the requested pose and rotate offsets in the model basis", () => {
	const c = createCharacters(), hand = node( "hand", 0, [ 0, 10, 0 ] );
	hand.rotation = [ 0, 0, Math.SQRT1_2, Math.SQRT1_2 ];
	c.model(
		"body",
		model( [ node( "root" ), hand ], [ {
			node: 0,
			path: "translation",
			interpolation: "LINEAR",
			times: Float32Array.of( 0, 1 ),
			values: Float32Array.of( 0, 0, 0, 20, 0, 0 )
		} ] ),
		[]
	);
	const actor = {
		gid: 1,
		model: "body",
		pose: { regionId: 257, x: 100, y: 20, z: 0, yaw: 0 },
		scale: 1,
		clip: "stand",
		time: .5,
		loop: false
	};
	const anchor = c.socket( [ actor ], 1, "hand", [ 3, 0, 0 ] );
	assert.equal( anchor.x, 113 );
	assert.equal( anchor.y, 30, "authored offset is not rotated by the hand bone" );
	actor.time = 1;
	assert.equal( c.socket( [ actor ], 1, "hand", [ 3, 0, 0 ] ).x, 123 );
	assert.equal( anchor.x, 113, "launch captures a value, not a live socket buffer" );
	assert.equal( c.socket( [], 1, "hand", [ 0, 0, 0 ] ), null );
	assert.equal( c.socket( [ actor ], 1, "missing", [ 0, 0, 0 ] ), null );
	c.dispose( null, null );
});
test("pose sampling respects hierarchy, inverse bind, loop and clamped one-shot end", () => {
	const m = model( [ node( "root" ), node( "saddle", 0, [ 0, 10, 0 ] ) ], [ {
		node: 0,
		path: "translation",
		interpolation: "LINEAR",
		times: Float32Array.of( 0, 1 ),
		values: Float32Array.of( 0, 0, 0, 20, 0, 0 )
	} ] );
	const pose = createCharacterPose( m );
	assert.equal( pose.evaluate( "stand", 0.5 ), true );
	assert.deepEqual( [ ...pose.socket( "saddle" ).slice( 12, 15 ) ], [ 10, 10, 0 ] );
	assert.equal( pose.evaluate( "stand", 0.5 ), false );
	pose.evaluate( "stand", 1.5, false );
	assert.equal( pose.socket( "root" )[12], 20 );
	pose.evaluate( "stand", 1.5, true );
	assert.equal( pose.socket( "root" )[12], 10 );
	const primitive = { ...m.primitives[0], inverseBind: identity() };
	primitive.inverseBind[12] = -10;
	const out = new Float32Array( 16 );
	pose.palette( primitive, out );
	assert.equal( out[12], 0 );
});
test("mounted rider follows animated saddle and unchanged poses issue no uploads", () => {
	const c = createCharacters(), mount = model( [ node( "root" ), node( "saddle", 0, [ 0, 10, 0 ] ) ] );
	c.model( "mount", mount, [] );
	c.model( "rider", model(), [] );
	let uploads = 0;
	const matrices = [];
	const gpu = {
		/*
================
upload
================
		*/
		upload( data ) {
			uploads++;
			matrices.push( data.instances.slice() );
			return {};
		},
		/*
================
updateInstances
================
		*/
		updateInstances( draw, value ) {
			uploads++;
			matrices.push( value.slice() );
			return draw;
		},
		/*
================
updateBones
================
		*/
		updateBones() {
			uploads++;
		},
		/*
================
release
================
		*/
		release() {}
	};
	const actor = ( gid, model, x, mountedOn ) => ({
		gid,
		model,
		mountedOn,
		pose: { regionId: 257, x, y: 0, z: 0, yaw: 0 },
		scale: 1,
		clip: "stand",
		time: 0,
		loop: true
	});
	c.actors( [ actor( 1, "mount", 20 ), actor( 2, "rider", 999, 1 ) ] );
	c.prepare( gpu, {
		/*
================
upload
================
		*/
		upload() {}, /*
================
release
================
		*/
		release() {}
	}, 257 );
	assert.deepEqual( [ ...matrices[1].slice( 12, 15 ) ], [ 20, 10, 0 ] );
	const first = uploads;
	c.prepare( gpu, {
		/*
================
upload
================
		*/
		upload() {}, /*
================
release
================
		*/
		release() {}
	}, 257 );
	assert.equal( uploads, first );
	c.actors( [ actor( 1, "mount", 30 ), actor( 2, "rider", 999, 1 ) ] );
	c.prepare( gpu, {
		/*
================
upload
================
		*/
		upload() {}, /*
================
release
================
		*/
		release() {}
	}, 257 );
	assert.deepEqual( [ ...matrices.at( -1 ).slice( 12, 15 ) ], [ 30, 10, 0 ] );
	c.dispose( gpu, null );
});
test("animation sound cursors dispatch once, wrap correctly, and suppress suspended-tab backlog", () => {
	const heard = [], sounds = createCharacterSounds( event => heard.push( event ) );
	sounds.catalog( [ {
		object: "MODEL",
		handle: "SND_STEP",
		event1: "-",
		publicPath: "/assets/audio/step.wav",
		volume: 80
	} ] );
	const definition = { durationMs: 1000, soundEvents: [ { cursorMs: 100, cue: "snd_step" } ] },
		step = time =>
			sounds.advance(
				1,
				"walk",
				0,
				time,
				true,
				definition,
				time,
				() => ({ profile: "MODEL", position: [ 1, 2, 3 ], surface: undefined, context: { player: false } }),
				"pose"
			);
	step( 0 );
	step( 0.15 );
	step( 0.15 );
	step( 1.15 );
	assert.equal( heard.length, 2 );
	assert.equal( heard[0].gain, 0.8 );
	assert.notEqual( heard[0].id, heard[1].id );
	step( 100.8 );
	assert.equal( heard.length, 2 );
	sounds.reset();
	step( 0.15 );
	assert.equal( heard.length, 3 );
});
test("combat selectors preserve exact native dimensions, ordered fallback and separate action cursors", async () => {
	const { characterSoundKeys, skillSoundRoots } = await load( "src/engine/foundation/animation/sound-selectors.ts" );
	const roots = skillSoundRoots( [ "1\t0\tMOB_SKILL\tPLAYER_SKILL", "2\t1\tCHILD\tCHILD_GROUP" ] );
	assert.deepEqual( roots.get( 2 ), [ "MOB_SKILL", "PLAYER_SKILL" ] );
	assert.throws( () => skillSoundRoots( [ "1\t2\tA\tB", "2\t1\tC\tD" ] ).get( 1 ), /Cyclic/ );
	const p = { player: true, weapon: "SWORD", skill: "PLAYER_SKILL" };
	assert.deepEqual( characterSoundKeys( "BODY", "SND_SWING1", p ), [ "PLAYER:SND_SWING1:-:SWORD:-:-" ] );
	assert.deepEqual( characterSoundKeys( "BODY", "SND_SWING1", { ...p, berserk: true } ), [
		"PLAYER:SND_SWING3:-:HWAN:SWORD:-"
	] );
	assert.deepEqual( characterSoundKeys( "BODY", "SND_DDMG", p ), [
		"PLAYER:SND_DDMG:PLAYER_SKILL:-:-:-",
		"PLAYER:SND_DDMG:-:SWORD:-:NORMAL"
	] );
	assert.deepEqual( characterSoundKeys( "BODY", "VOC_MOAN", { ...p, critical: true } ), [
		"BODY:VOC_MOAN:-:CRITYCAL:-:-"
	] );
	assert.deepEqual( characterSoundKeys( "MOB", "SND_DMG", { player: false, skill: "MOB_SKILL" } ), [
		"MOB:SND_DMG:MOB_SKILL:-:-:-"
	] );
	assert.deepEqual( characterSoundKeys( "BODY", "SND_BLOCKING", { ...p, critical: true } ), [
		"PLAYER:SND_BLOCKING:-:-:CRITYCAL:-"
	] );
	const heard = [], sounds = createCharacterSounds( e => heard.push( e ) );
	sounds.catalog( [
		{
			object: "PLAYER",
			handle: "SND_DMG",
			skillId: "PLAYER_SKILL",
			event1: "-",
			publicPath: "/assets/audio/impact.wav"
		},
		{ object: "PLAYER", handle: "SND_SWING1", event1: "SWORD", publicPath: "/assets/audio/swing.wav" },
		{ object: "BODY", handle: "VOC_MOAN", event1: "NORMAL", publicPath: "/assets/audio/moan.wav" }
	] );
	assert.equal( sounds.emit( "impact", "BODY", [ "SND_CRIDMG", "SND_DMG" ], p, [ 0, 0, 0 ], 1 ), true );
	assert.equal( heard[0].path, "/assets/audio/impact.wav" );
	const swing = { durationMs: 1000, soundEvents: [ { cursorMs: 170, cue: "snd_swing1" } ] },
		hit = { durationMs: 500, soundEvents: [ { cursorMs: 0, cue: "voc_moan" } ] };
	sounds.advance(
		1,
		"attack",
		0,
		.2,
		false,
		swing,
		.2,
		() => ({ profile: "BODY", position: [ 0, 0, 0 ], surface: undefined, context: p }),
		"attack"
	);
	sounds.advance(
		1,
		"hit",
		.2,
		0,
		false,
		hit,
		.2,
		() => ({ profile: "BODY", position: [ 0, 0, 0 ], surface: undefined, context: p }),
		"pose"
	);
	sounds.advance(
		1,
		"hit",
		.2,
		.001,
		false,
		hit,
		.201,
		() => ({ profile: "BODY", position: [ 0, 0, 0 ], surface: undefined, context: p }),
		"pose"
	);
	sounds.advance(
		1,
		"attack",
		0,
		.22,
		false,
		swing,
		.22,
		() => ({ profile: "BODY", position: [ 0, 0, 0 ], surface: undefined, context: p }),
		"attack"
	);
	assert.deepEqual( heard.map( e => e.path ), [
		"/assets/audio/impact.wav",
		"/assets/audio/swing.wav",
		"/assets/audio/moan.wav"
	] );
	sounds.retain( new Set() );
	sounds.advance(
		1,
		"attack",
		0,
		.2,
		false,
		swing,
		.2,
		() => ({ profile: "BODY", position: [ 0, 0, 0 ], surface: undefined, context: p }),
		"attack"
	);
	assert.equal( heard.length, 4 );
	sounds.impact( "timed", 1, "BODY", [ "SND_DMG" ], p, 1 );
	sounds.flush( 1.1, () => [ 3, 4, 5 ] );
	assert.equal( heard.length, 4 );
	sounds.flush( 1.101, () => [ 6, 7, 8 ] );
	assert.equal( heard.length, 5 );
	assert.equal( heard.at( -1 ).x, 6 );
	sounds.flush( 1.2, () => [ 9, 9, 9 ] );
	assert.equal( heard.length, 5 );
	sounds.impact( "stale", 1, "BODY", [ "SND_DMG" ], p, 2 );
	sounds.flush( 3, () => [ 0, 0, 0 ] );
	sounds.impact( "removed", 2, "BODY", [ "SND_DMG" ], p, 3 );
	sounds.flush( 3.2, () => undefined );
	assert.equal( heard.length, 5 );
});

test("audio gesture gate, event deduplication and decoded-buffer reuse have one owner", async t => {
	let started = 0, requested = 0, stopped = 0, closed = 0;
	const nodes = [];
	const param = () => ({ value: 0 });
	const graph = () => ({
		/*
================
connect
================
		*/
		connect() {}, /*
================
disconnect
================
		*/
		disconnect() {}
	});
	class Context {
		state = "running";
		destination = {};
		listener = { positionX: param(), positionY: param(), positionZ: param() };
		/*
================
resume
================
		*/
		resume() {
			return Promise.resolve();
		}
		/*
================
close
================
		*/
		close() {
			closed++;
			return Promise.resolve();
		}
		/*
================
decodeAudioData
================
		*/
		decodeAudioData() {
			return Promise.resolve( { length: 100, numberOfChannels: 1 } );
		}
		/*
================
createBufferSource
================
		*/
		createBufferSource() {
			const source = {
				...graph(),
				/*
================
start
================
				*/
				start() {
					started++;
				},
				/*
================
stop
================
				*/
				stop() {
					stopped++;
					this.onended?.();
				}
			};
			nodes.push( source );
			return source;
		}
		/*
================
createGain
================
		*/
		createGain() {
			return { ...graph(), gain: param() };
		}
		/*
================
createPanner
================
		*/
		createPanner() {
			return { ...graph(), positionX: param(), positionY: param(), positionZ: param() };
		}
	}
	const previous = Object.getOwnPropertyDescriptor( globalThis, "AudioContext" );
	Object.defineProperty( globalThis, "AudioContext", { value: Context, configurable: true } );
	t.after( () => {
		if ( previous ) Object.defineProperty( globalThis, "AudioContext", previous );
		else delete globalThis.AudioContext;
	} );
	const assets = {
		available: () => 4,
		/*
================
request
================
		*/
		request() {
			return ++requested;
		},
		/*
================
take
================
		*/
		take() {
			return { kind: "bytes", buffer: new ArrayBuffer( 1 ) };
		},
		/*
================
cancel
================
		*/
		cancel() {}
	};
	const audio = createAudio( assets, "http://localhost", createPresentationRandom( 1 ) );
	const event = id => ({ id, path: "/assets/audio/test.wav", gain: 1, x: 1, y: 2, z: 3, expires: 10 });
	audio.enqueue( event( "a" ) );
	audio.step( 0, [ 0, 0, 0 ] );
	assert.equal( requested, 0 );
	audio.unlock();
	audio.step( 0, [ 0, 0, 0 ] );
	audio.step( 0, [ 0, 0, 0 ] );
	await Promise.resolve();
	audio.step( 0, [ 0, 0, 0 ] );
	assert.equal( started, 1 );
	audio.enqueue( event( "a" ) );
	audio.enqueue( event( "b" ) );
	audio.step( 0, [ 0, 0, 0 ] );
	assert.equal( started, 2 );
	assert.equal( requested, 1 );
	const rain = { ...event( "weather-loop" ), loop: true, spatial: false };
	audio.enqueue( rain );
	audio.step( 20, [ 0, 0, 0 ] );
	assert.equal( started, 3 );
	assert.equal( nodes.at( -1 ).loop, true );
	audio.enqueue( { ...rain, stop: true } );
	assert.equal( stopped, 1 );
	audio.enqueue( rain );
	audio.step( 21, [ 0, 0, 0 ] );
	assert.equal( started, 4 );
	audio.reset();
	assert.equal( stopped, 4 );
	audio.dispose();
	assert.equal( closed, 1 );
});
test("audio backpressure survives expired events and resets until native decodes settle", async t => {
	const decodes = [];
	let requests = 0, started = 0;
	class Context {
		state = "running";
		listener = { positionX: {}, positionY: {}, positionZ: {} };
		/*
================
resume
================
		*/
		resume() {
			return Promise.resolve();
		}
		/*
================
close
================
		*/
		close() {
			return Promise.resolve();
		}
		/*
================
decodeAudioData
================
		*/
		decodeAudioData() {
			return new Promise( ( resolve, reject ) => decodes.push( { resolve, reject } ) );
		}
		/*
================
createBufferSource
================
		*/
		createBufferSource() {
			started++;
			throw new Error( "Stale decode must not play" );
		}
	}
	const previous = Object.getOwnPropertyDescriptor( globalThis, "AudioContext" );
	Object.defineProperty( globalThis, "AudioContext", { value: Context, configurable: true } );
	t.after( () => {
		if ( previous ) Object.defineProperty( globalThis, "AudioContext", previous );
		else delete globalThis.AudioContext;
	} );
	const audio = createAudio(
		{
			available: () => 4,
			/*
================
request
================
			*/
			request( url, limit ) {
				assert.equal( limit, 4 << 20 );
				return ++requests;
			},
			/*
================
take
================
			*/
			take() {
				return { kind: "bytes", buffer: new ArrayBuffer( 1 ) };
			},
			/*
================
cancel
================
			*/
			cancel() {}
		},
		"http://localhost",
		createPresentationRandom( 1 )
	);
	audio.unlock();
	const enqueue = ( i, now ) =>
		audio.enqueue( {
			id: String( i ),
			path: `/assets/audio/${i}.wav`,
			gain: 1,
			x: 0,
			y: 0,
			z: 0,
			expires: now + 0.25
		} );
	for ( let i = 0; i < 200; i++ ) {
		enqueue( i, i / 60 );
		audio.step( i / 60, [ 0, 0, 0 ] );
	}
	assert.equal( decodes.length, 2 );
	assert.equal( requests, 2 );
	for ( let i = 0; i < 10; i++ ) {
		audio.reset();
		enqueue( 200 + i, 10 );
		audio.step( 10, [ 0, 0, 0 ] );
	}
	assert.equal( requests, 2, "reset cannot release uncancellable native work" );
	decodes[0].resolve( { length: 100, numberOfChannels: 1 } );
	decodes[1].reject( new Error( "old epoch" ) );
	await new Promise( setImmediate );
	assert.equal( audio.error(), null );
	audio.step( 10, [ 0, 0, 0 ] );
	audio.step( 10, [ 0, 0, 0 ] );
	assert.equal( decodes.length, 3 );
	assert.equal( started, 0 );
	audio.dispose();
	decodes[2].resolve( { length: 100, numberOfChannels: 1 } );
	await new Promise( setImmediate );
	assert.equal( started, 0 );
});
test("audio completes bounded expired-cue loads and recovers from synchronous decode failure", t => {
	let decodes = 0, cancelled = 0;
	class Context {
		state = "running";
		listener = { positionX: {}, positionY: {}, positionZ: {} };
		/*
================
resume
================
		*/
		resume() {
			return Promise.resolve();
		}
		/*
================
close
================
		*/
		close() {
			return Promise.resolve();
		}
		/*
================
decodeAudioData
================
		*/
		decodeAudioData() {
			decodes++;
			throw new Error( "decoder unavailable" );
		}
	}
	const previous = Object.getOwnPropertyDescriptor( globalThis, "AudioContext" );
	Object.defineProperty( globalThis, "AudioContext", { value: Context, configurable: true } );
	t.after( () => {
		if ( previous ) Object.defineProperty( globalThis, "AudioContext", previous );
		else delete globalThis.AudioContext;
	} );
	const audio = createAudio(
		{
			available: () => 4,
			request: () => 1,
			take: () => ({ kind: "bytes", buffer: new ArrayBuffer( 1 ) }),
			/*
================
cancel
================
			*/
			cancel() {
				cancelled++;
			}
		},
		"http://localhost",
		createPresentationRandom( 1 )
	);
	audio.unlock();
	const event = ( id, expires ) => ({ id, path: `/assets/audio/${id}.wav`, gain: 1, x: 0, y: 0, z: 0, expires });
	audio.enqueue( event( "expired", 0.1 ) );
	audio.step( 0, [ 0, 0, 0 ] );
	audio.step( 1, [ 0, 0, 0 ] );
	assert.equal( cancelled, 0 );
	assert.equal( decodes, 1 );
	for ( let i = 0; i < 4; i++ ) {
		audio.enqueue( event( String( i ), 10 ) );
		audio.step( 1, [ 0, 0, 0 ] );
		assert.doesNotThrow( () => audio.step( 1, [ 0, 0, 0 ] ) );
	}
	assert.equal( decodes, 5 );
	audio.dispose();
});
test("playing audio buffers stay charged to residency until their voices end", async t => {
	const sources = [];
	let requests = 0, seconds = 0;
	const graph = () => ({
		/*
================
connect
================
		*/
		connect() {}, /*
================
disconnect
================
		*/
		disconnect() {}
	});
	class Context {
		state = "running";
		listener = { positionX: {}, positionY: {}, positionZ: {} };
		/*
================
resume
================
		*/
		resume() {
			return Promise.resolve();
		}
		/*
================
close
================
		*/
		close() {
			return Promise.resolve();
		}
		/*
================
decodeAudioData
================
		*/
		decodeAudioData() {
			return Promise.resolve( { length: 5 << 20, numberOfChannels: 1 } );
		}
		/*
================
createBufferSource
================
		*/
		createBufferSource() {
			const source = {
				...graph(),
				/*
================
start
================
				*/
				start() {
					sources.push( this );
				},
				/*
================
stop
================
				*/
				stop() {
					this.onended?.();
				}
			};
			return source;
		}
		/*
================
createGain
================
		*/
		createGain() {
			return { ...graph(), gain: {} };
		}
		/*
================
createPanner
================
		*/
		createPanner() {
			return { ...graph(), positionX: {}, positionY: {}, positionZ: {} };
		}
	}
	const previous = Object.getOwnPropertyDescriptor( globalThis, "AudioContext" );
	Object.defineProperty( globalThis, "AudioContext", { value: Context, configurable: true } );
	t.after( () => {
		if ( previous ) Object.defineProperty( globalThis, "AudioContext", previous );
		else delete globalThis.AudioContext;
	} );
	const audio = createAudio(
		{
			available: () => 4,
			request: () => ++requests,
			take: () => ({ kind: "bytes", buffer: new ArrayBuffer( 1 ) }),
			/*
================
cancel
================
			*/
			cancel() {}
		},
		"http://localhost",
		createPresentationRandom( 1 )
	);
	audio.unlock();
	/*
================
play
================
	*/
	async function play( id, path ) {
		audio.enqueue( { id, path: `/assets/audio/${path}.wav`, gain: 1, x: 0, y: 0, z: 0, expires: seconds + 10 } );
		audio.step( seconds, [ 0, 0, 0 ] );
		audio.step( seconds, [ 0, 0, 0 ] );
		await new Promise( setImmediate );
		audio.step( seconds, [ 0, 0, 0 ] );
	}
	await play( "one", "a" );
	await play( "two", "b" );
	assert.equal( sources.length, 1 );
	assert.equal( audio.error(), null, "Live residency defers demand without failing the runtime" );
	assert.equal( audio.snapshot().residentBytes, 20 << 20 );
	await play( "reuse", "a" );
	assert.equal( sources.length, 2 );
	assert.equal( requests, 2, "playing cached buffer cannot be evicted" );
	for ( const source of sources ) source.onended();
	seconds = 11; // The blocked cue expired; only fresh demand should play.
	await play( "after-end", "b" );
	assert.equal( sources.length, 3 );
	audio.dispose();
});

const { animationMetadata } = await load( "src/engine/foundation/animation/animation-metadata.ts" );
const { characterRadius } = await load( "src/engine/foundation/animation/character-bounds.ts" );
test("published omitted cue lists normalize at admission and malformed cues fail", () => {
	assert.deepEqual( animationMetadata( { stand: { durationMs: 2333 } } ), {
		stand: { durationMs: 2333, soundEvents: [], trackEvents: [], timeWarpCurve: { scale: 0, records: [] } }
	} );
	assert.throws(
		() => animationMetadata( { walk: { durationMs: 1000, soundEvents: [ { cursorMs: -1, cue: "snd" } ] } } ),
		/Invalid animation sound cue/
	);
});
test("character visibility envelope includes translation and scale animation extrema", () => {
	const m = model( [ node( "root" ) ], [ {
		node: 0,
		path: "translation",
		interpolation: "LINEAR",
		times: Float32Array.of( 0, 1 ),
		values: Float32Array.of( 0, 0, 0, 100, 0, 0 )
	} ] );
	assert.ok( characterRadius( m ) >= 100 );
	const c = createCharacters();
	c.model( "m", m, [] );
	c.actors( [ {
		gid: 1,
		model: "m",
		pose: { regionId: 257, x: 10000, y: 0, z: 0, yaw: 0 },
		clip: "stand",
		time: 0,
		loop: true,
		scale: 1
	} ] );
	let uploaded = 0;
	assert.deepEqual(
		c.prepare(
			{
				/*
================
upload
================
				*/
				upload() {
					uploaded++;
				},
				/*
================
release
================
				*/
				release() {}
			},
			{
				/*
================
upload
================
				*/
				upload() {}, /*
================
release
================
				*/
				release() {}
			},
			257,
			identity()
		),
		[]
	);
	assert.equal( uploaded, 0 );
	c.dispose( null, null );
});

test("native cue records beyond clip end remain unplayed instead of spilling into a new loop", () => {
	const heard = [], sounds = createCharacterSounds( event => heard.push( event ) );
	sounds.catalog( [ { object: "M", handle: "SND_RUN", event1: "-", publicPath: "/assets/audio/run.wav" } ] );
	const clip = { durationMs: 666, soundEvents: [ { cursorMs: 715, cue: "snd_run" } ] };
	sounds.advance(
		1,
		"run",
		0,
		0,
		true,
		clip,
		0,
		() => ({ profile: "M", position: [ 0, 0, 0 ], surface: undefined, context: { player: false } }),
		"pose"
	);
	sounds.advance(
		1,
		"run",
		0,
		0.8,
		true,
		clip,
		0.8,
		() => ({ profile: "M", position: [ 0, 0, 0 ], surface: undefined, context: { player: false } }),
		"pose"
	);
	assert.deepEqual( heard, [] );
});

test("native event pass fills sparse tracks before timed pose and respects weight headroom", () => {
	const channel = ( node, x ) => ({
		node,
		path: "translation",
		interpolation: "LINEAR",
		times: Float32Array.of( 0, 1 ),
		values: Float32Array.of( x, 0, 0, x, 0, 0 )
	});
	const m = model( [ node( "root" ), node( "arm", 0 ) ], [ channel( 0, 10 ), channel( 1, 20 ) ] );
	m.clips.push( { name: "hit", duration: 1, channels: [ channel( 1, 100 ) ] } );
	const pose = createCharacterPose( m ),
		layers = [ { clip: "stand", time: 0, loop: true, weight: 1, lane: "timed" }, {
			clip: "hit",
			time: 0,
			loop: false,
			weight: 0.25,
			lane: "event"
		} ];
	pose.evaluate( "stand", 0, true, layers );
	assert.equal( pose.socket( "root" )[12], 10 );
	assert.equal( pose.socket( "arm" )[12], 50 );
	assert.equal( pose.evaluate( "stand", 0, true, layers ), false );
	layers[1].weight = 1;
	assert.equal( pose.evaluate( "stand", 0, true, layers ), true );
	assert.equal( pose.socket( "arm" )[12], 110 );
	pose.evaluate( "stand", 0, true );
	assert.equal( pose.socket( "arm" )[12], 30 );
	assert.throws( () => pose.evaluate( "stand", 0, true, [ { ...layers[0], weight: NaN } ] ), /Invalid/ );
	pose.evaluate( "stand", 0, true, Array( 9 ).fill( { ...layers[0], weight: 1 / 9 } ) );
	assert.equal( pose.socket( "arm" )[12], 30 );
});

test("bone-attached effects inherit moving mount sockets, invalidate offset changes and reject cycles", () => {
	const c = createCharacters(),
		mount = model( [ node( "root" ), node( "saddle", 0, [ 0, 10, 0 ] ) ] ),
		rider = model( [ node( "root" ), node( "hand", 0, [ 2, 3, 0 ] ) ] );
	c.model( "mount", mount, [] );
	c.model( "rider", rider, [] );
	c.model( "effect", model(), [] );
	const actor = ( gid, model, extra = {} ) => ({
		gid,
		model,
		pose: { regionId: 257, x: 20, y: 0, z: 0, yaw: 0 },
		scale: 1,
		clip: "stand",
		time: 0,
		loop: true,
		...extra
	});
	const rows = [
		actor( 1, "mount" ),
		actor( 2, "rider", { mountedOn: 1 } ),
		actor( 3, "effect", { attachment: { gid: 2, bone: "hand", offset: [ 1, 0, 0 ] } } )
	];
	const matrices = [],
		gpu = {
			/*
================
upload
================
			*/
			upload( data ) {
				matrices.push( data.instances.slice() );
				return {};
			},
			/*
================
updateInstances
================
			*/
			updateInstances( draw, data ) {
				matrices.push( data.slice() );
				return draw;
			},
			/*
================
updateBones
================
			*/
			updateBones() {},
			/*
================
release
================
			*/
			release() {}
		};
	const images = {
		/*
================
upload
================
		*/
		upload() {
			return {};
		},
		/*
================
release
================
		*/
		release() {}
	};
	c.actors( rows );
	c.prepare( gpu, images, 257 );
	assert.deepEqual( [ ...matrices.at( -1 ).slice( 12, 15 ) ], [ 23, 13, 0 ] );
	const count = matrices.length;
	c.prepare( gpu, images, 257 );
	assert.equal( matrices.length, count );
	rows[2].attachment.offset[0] = 5;
	c.actors( rows );
	c.prepare( gpu, images, 257 );
	assert.deepEqual( [ ...matrices.at( -1 ).slice( 12, 15 ) ], [ 27, 13, 0 ] );
	c.actors( [ rows[0], rows[2] ] );
	assert.doesNotThrow( () => c.prepare( gpu, images, 257 ) );
	c.actors( [ actor( 1, "mount", { mountedOn: 2 } ), actor( 2, "rider", { mountedOn: 1 } ) ] );
	assert.throws( () => c.prepare( gpu, images, 257 ), /Cyclic/ );
	c.dispose( gpu, null );
});

test("camera alpha separates the local actor from peers, fades attachments and survives device loss", () => {
	const c = createCharacters();
	c.model( "body", model(), [] );
	const actor = ( gid, opacity ) => ({
		gid,
		opacity,
		model: "body",
		pose: { regionId: 257, x: 0, y: 0, z: 0, yaw: 0 },
		scale: 1,
		clip: "stand",
		time: 0,
		loop: true
	});
	const uploads = [], updates = [];
	const gpu = {
		/*
================
upload
================
		*/
		upload( data ) {
			const draw = { blended: !!data.material?.blend };
			uploads.push( data );
			return draw;
		},
		/*
================
updateInstances
================
		*/
		updateInstances( draw, matrices, alpha ) {
			updates.push( alpha ? [ ...alpha ] : null );
			return draw;
		},
		/*
================
updateBones
================
		*/
		updateBones() {},
		/*
================
release
================
		*/
		release() {}
	};
	c.actors( [ actor( 1, .5 ), actor( 2, 1 ), {
		...actor( 3, 1 ),
		attachment: { gid: 1, bone: "root", offset: [ 0, 0, 0 ] }
	} ] );
	const draws = c.prepare( gpu, {}, 257 );
	assert.equal( draws.length, 2 );
	assert.equal( draws.filter( d => d.blended ).length, 1 );
	assert.deepEqual( updates[0], [ .5, .5 ] );
	assert.equal( uploads.filter( d => d.material?.instanceFade ).length, 1 );
	const count = uploads.length + updates.length;
	c.prepare( gpu, {}, 257 );
	assert.equal( uploads.length + updates.length, count, "stationary fade holds reuse" );
	c.invalidate();
	c.prepare( gpu, {}, 257 );
	assert.deepEqual( updates.at( -1 ), [ .5, .5 ] );
	c.actors( [ actor( 1, 0 ), actor( 2, 1 ) ] );
	assert.equal( c.prepare( gpu, {}, 257 ).length, 1 );
	assert.equal( c.stats().actors, 2, "fade does not remove simulation actors" );
	c.actors( [ actor( 1, 1 ), actor( 2, 1 ) ] );
	const restored = c.prepare( gpu, {}, 257 );
	assert.equal( restored.length, 1 );
	assert.equal( restored[0].blended, false );
	c.dispose( gpu, null );
});
test("UI sounds decode while suspended and play on the first unlocked activation", async t => {
	let started = 0, decoded = 0, requests = 0, contexts = 0;
	const gains = [],
		graph = () => ({
			/*
================
connect
================
			*/
			connect() {}, /*
================
disconnect
================
			*/
			disconnect() {}
		}),
		param = () => ({ value: 0 });
	class Context {
		state = "suspended";
		constructor() {
			contexts++;
		}
		destination = {};
		listener = { positionX: param(), positionY: param(), positionZ: param() };
		/*
================
resume
================
		*/
		resume() {
			this.state = "running";
			return Promise.resolve();
		}
		/*
================
close
================
		*/
		close() {
			return Promise.resolve();
		}
		/*
================
decodeAudioData
================
		*/
		decodeAudioData() {
			decoded++;
			return Promise.resolve( { length: 100, numberOfChannels: 1 } );
		}
		/*
================
createBufferSource
================
		*/
		createBufferSource() {
			return {
				...graph(),
				/*
================
start
================
				*/
				start() {
					started++;
				},
				/*
================
stop
================
				*/
				stop() {}
			};
		}
		/*
================
createGain
================
		*/
		createGain() {
			const gain = { ...graph(), gain: param() };
			gains.push( gain );
			return gain;
		}
		/*
================
createPanner
================
		*/
		createPanner() {
			return { ...graph(), positionX: param(), positionY: param(), positionZ: param() };
		}
	}
	const prior = Object.getOwnPropertyDescriptor( globalThis, "AudioContext" );
	Object.defineProperty( globalThis, "AudioContext", { value: Context, configurable: true } );
	t.after( () => {
		if ( prior ) Object.defineProperty( globalThis, "AudioContext", prior );
		else delete globalThis.AudioContext;
	} );
	const audio = createAudio(
		{
			available: () => 4,
			request: () => ++requests,
			take: () => ({ kind: "bytes", buffer: new ArrayBuffer( 1 ) }),
			/*
================
cancel
================
			*/
			cancel() {}
		},
		"http://localhost",
		createPresentationRandom( 1 )
	);
	const { createUiSoundCatalog } = await load( "src/engine/foundation/ui/sound-catalog.ts" ),
		{ createItemSoundCatalog } = await load( "src/engine/foundation/audio/item-sound-catalog.ts" ),
		catalog = createUiSoundCatalog(),
		paths = new Set(
			[ ...Object.values( catalog ).flat(), ...Object.values( createItemSoundCatalog() ).flat() ].map( row =>
				row.path
			)
		);
	paths.add( "/assets/audio/sfx/prim/snd/ui/buf_disappear.wav" );
	audio.prepareUi( true );
	for ( let i = 0; i < 128; i++ ) {
		audio.step( i / 100, [ 0, 0, 0 ] );
		await Promise.resolve();
	}
	assert.equal( decoded, paths.size );
	assert.equal( started, 0 );
	audio.unlock();
	audio.uiClick();
	audio.uiSound( "open" );
	audio.uiSound( "close" );
	audio.uiSound( "message" );
	audio.step( 1.28, [ 0, 0, 0 ] );
	assert.equal( started, 4 );
	assert.equal( requests, paths.size );
	assert.equal( contexts, 1 );
	for ( const handle of Object.keys( catalog ) ) audio.nativeUi( handle, 1.29, [ 0, 0, 0 ] );
	audio.step( 1.29, [ 0, 0, 0 ] );
	assert.equal( started, 4 + Object.keys( catalog ).length );
	audio.step( 1.30, [ 0, 0, 0 ] );
	assert.equal( started, 4 + Object.keys( catalog ).length, "frame updates never repeat a UI cue" );
	assert.ok(
		gains.every( g => g.gain.value === Math.pow( 10, -.5 ) ),
		"native null-position UI route uses effect master volume without extra row attenuation"
	);
	const count = started;
	audio.options( {
		bgm: 30,
		effects: 100,
		environment: 50,
		muteBgm: false,
		muteEffects: true,
		muteEnvironment: false
	} );
	assert.ok( gains.every( g => g.gain.value === 0 ) );
	audio.options( {
		bgm: 30,
		effects: 100,
		environment: 50,
		muteBgm: false,
		muteEffects: false,
		muteEnvironment: false
	} );
	assert.ok( gains.every( g => g.gain.value === 1 ) );
	assert.equal( started, count, "mix changes do not restart active sources" );
	audio.dispose();
	audio.prepareUi( true );
	assert.equal( contexts, 1 );
});

test("native half-second alpha uses float progress and truncation, with idempotent targets and continuous reversal", async () => {
	const { advanceCharacterFade } = await load( "src/engine/foundation/animation/character-fade.ts" );
	const state = { mode: false, current: 255, start: 255, progress: 1 };
	assert.equal( advanceCharacterFade( state, true, .25 ), 127 / 255 );
	assert.equal( advanceCharacterFade( state, true, .125 ), 63 / 255 );
	assert.equal( advanceCharacterFade( state, false, 0 ), 63 / 255 );
	assert.equal( advanceCharacterFade( state, false, .25 ), 159 / 255 );
	assert.equal( advanceCharacterFade( state, false, .25 ), 1 );
	assert.equal( advanceCharacterFade( state, true, .5 ), 0 );
	assert.throws( () => advanceCharacterFade( state, false, -1 ) );
});

test("low-vital alarm requires an actor anchor, uses positional output and respects effect mute", async t => {
	const gains = [], panners = [], outputs = [];
	let starts = 0;
	const param = () => ({ value: 0 }),
		graph = () => ({
			/*
================
connect
================
			*/
			connect( target ) {
				outputs.push( target );
			},
			/*
================
disconnect
================
			*/
			disconnect() {}
		});
	class Context {
		state = "running";
		destination = { destination: true };
		listener = { positionX: param(), positionY: param(), positionZ: param() };
		/*
================
resume
================
		*/
		resume() {
			return Promise.resolve();
		}
		/*
================
close
================
		*/
		close() {
			return Promise.resolve();
		}
		/*
================
decodeAudioData
================
		*/
		decodeAudioData() {
			return Promise.resolve( { length: 100, numberOfChannels: 1 } );
		}
		/*
================
createBufferSource
================
		*/
		createBufferSource() {
			return {
				...graph(),
				/*
================
start
================
				*/
				start() {
					starts++;
				},
				/*
================
stop
================
				*/
				stop() {}
			};
		}
		/*
================
createGain
================
		*/
		createGain() {
			const g = { ...graph(), gain: param() };
			gains.push( g );
			return g;
		}
		/*
================
createPanner
================
		*/
		createPanner() {
			const p = { ...graph(), positionX: param(), positionY: param(), positionZ: param() };
			panners.push( p );
			return p;
		}
	}
	const prior = Object.getOwnPropertyDescriptor( globalThis, "AudioContext" );
	Object.defineProperty( globalThis, "AudioContext", { value: Context, configurable: true } );
	t.after( () => {
		if ( prior ) Object.defineProperty( globalThis, "AudioContext", prior );
		else delete globalThis.AudioContext;
	} );
	let requests = 0;
	const audio = createAudio(
		{
			available: () => 4,
			request: () => ++requests,
			take: () => ({ kind: "bytes", buffer: new ArrayBuffer( 1 ) }),
			/*
================
cancel
================
			*/
			cancel() {}
		},
		"http://localhost",
		createPresentationRandom( 1 )
	);
	audio.unlock();
	audio.nativeUi( "SND_ALARM", 0 );
	audio.step( 0, [ 0, 0, 0 ] );
	assert.equal( requests, 0, "absent actor cannot become a full-volume UI sound" );
	audio.nativeUi( "SND_ALARM", 0, [ 300, 0, 0 ] );
	audio.step( 0, [ 0, 0, 0 ] );
	assert.equal( requests, 0, "inaudible alarm is rejected before decode" );
	audio.nativeUi( "SND_ALARM", 0, [ 120, 5, 7 ] );
	audio.step( 0, [ 0, 0, 0 ] );
	audio.step( 0, [ 0, 0, 0 ] );
	await new Promise( setImmediate );
	audio.step( 0, [ 0, 0, 0 ] );
	assert.equal( starts, 1 );
	assert.ok( outputs.includes( panners[0] ) );
	assert.deepEqual( [ panners[0].positionX.value, panners[0].positionY.value, panners[0].positionZ.value ], [
		120,
		5,
		7
	] );
	assert.equal( panners[0].distanceModel, "linear" );
	assert.equal( gains[0].gain.value, Math.pow( 10, -.5 ) );
	audio.options( {
		bgm: 30,
		effects: 50,
		environment: 50,
		muteBgm: false,
		muteEffects: true,
		muteEnvironment: false
	} );
	assert.equal( gains[0].gain.value, 0 );
	audio.step( .1, [ 0, 0, 0 ] );
	assert.equal( starts, 1 );
	audio.dispose();
});

test("native sound ranges include the old cursor, exclude the new cursor and never dispatch the duration endpoint", () => {
	const heard = [], sounds = createCharacterSounds( e => heard.push( e ) );
	sounds.catalog( [ { object: "M", handle: "SND_STEP", event1: "-", publicPath: "/assets/audio/step.wav" } ] );
	const definition = {
		durationMs: 1000,
		soundEvents: [ 0, 100, 100, 1000 ].map( cursorMs => ({ cursorMs, cue: "snd_step" }) )
	};
	const step = time =>
		sounds.advance(
			1,
			"walk",
			0,
			time,
			true,
			definition,
			time,
			() => ({ profile: "M", position: [ 0, 0, 0 ], context: { player: false } })
		);
	step( 0 );
	assert.equal( heard.length, 0 );
	step( .0009 );
	assert.equal( heard.length, 0, "sub-millisecond cursor does not advance" );
	step( .001 );
	assert.equal( heard.length, 1 );
	step( .100 );
	assert.equal( heard.length, 1 );
	step( .101 );
	assert.equal( heard.length, 3, "duplicate authored keys retain their order" );
	step( 1 );
	assert.equal( heard.length, 3 );
	step( 1.002 );
	assert.equal( heard.length, 4, "only next-cycle zero fires" );
	step( 1.002 );
	assert.equal( heard.length, 4 );
});

test("sound sources resolve only for due cues while silent, repeated and expired steps still advance cursors", () => {
	const heard = [], sounds = createCharacterSounds( e => heard.push( e ) );
	let resolved = 0, x = 1;
	sounds.catalog( [ { object: "M", handle: "SND_STEP", event1: "-", publicPath: "/assets/audio/step.wav" } ] );
	const definition = {
			durationMs: 1000,
			soundEvents: [ { cursorMs: 100, cue: "snd_step" }, { cursorMs: 150, cue: "snd_step" } ]
		},
		source = () => {
			resolved++;
			return { profile: "M", position: [ x, 0, 0 ], context: { player: false } };
		};
	const step = time => sounds.advance( 1, "walk", 0, time, true, definition, time, source );
	step( 0 );
	step( .05 );
	assert.equal( resolved, 0 );
	x = 9;
	step( .2 );
	assert.equal( resolved, 1 );
	assert.equal( heard.length, 2 );
	assert.ok( heard.every( e => e.x === 9 ) );
	step( .2 );
	step( .8 );
	step( 100.8 );
	assert.equal( resolved, 1, "no stale cue burst after suspension" );
	x = 12;
	step( 101.2 );
	assert.equal( resolved, 2 );
	assert.equal( heard.at( -1 ).x, 12 );
	sounds.retain( new Set() );
	step( .2 );
	assert.equal( resolved, 3, "despawn retires all lane cursors" );
	sounds.reset();
	sounds.advance( 2, "stand", 0, 0, true, undefined, 0, () => {
		throw Error( "silent clip resolved" );
	} );
});

test("production BSR emitter inherits holder alpha exactly once through the renderer", async () => {
	const { createModelEmission, modelAmbientParticles } = await load(
		"src/engine/foundation/animation/model-emission.ts"
	);
	const particles = modelAmbientParticles( [ {
		kind: 2,
		stateId: -1,
		animationSetName: "ambient",
		entries: [ {
			field00: 1,
			effectPath: "system/item_drop_equip.efp",
			boneName: "",
			vector3c: [ 0, 0, 0 ],
			field4c: 0,
			flags50: [ 0, 0, 0 ],
			flag53: 0
		} ]
	} ] );
	const c = createCharacters(), emission = createModelEmission( () => -1 ), updates = [];
	const gpu = {
		/*
================
upload
================
		*/
		upload() {
			return {};
		},
		/*
================
updateInstances
================
		*/
		updateInstances( draw, matrices, alpha ) {
			updates.push( ...(alpha ?? []) );
			return draw;
		},
		/*
================
updateBones
================
		*/
		updateBones() {},
		/*
================
release
================
		*/
		release() {}
	};
	c.model( "body", model(), [] );
	for ( const alpha of [ 1, .75, .5, .25, 0 ] ) {
		const parent = {
			gid: 1,
			model: "body",
			opacity: alpha,
			pose: { regionId: 257, x: 0, y: 0, z: 0, yaw: 0 },
			scale: 1,
			clip: "stand",
			time: 0,
			loop: true
		};
		const child = emission.step( [ { actor: parent, particles } ], 1 - alpha, () => true, 10 )[0];
		const effect = model();
		effect.clips[0].name = "effect";
		c.model( child.model, effect, [] );
		updates.length = 0;
		c.actors( [ parent, child ] );
		const draws = c.prepare( gpu, {}, 257 );
		if ( alpha === 0 ) assert.equal( draws.length, 0 );
		else if ( alpha < 1 ) {
			assert.deepEqual(
				updates,
				[ alpha, alpha ],
				"body and emitted effect receive the same alpha, never its square"
			);
		}
	}
	c.dispose( gpu, null );
});

test("BSR rotation crosses the production emitter and detached renderer snapshot", async () => {
	const { createModelEmission, modelAmbientParticles } = await load(
		"src/engine/foundation/animation/model-emission.ts"
	);
	const particles = modelAmbientParticles( [ {
		kind: 2,
		stateId: -1,
		animationSetName: "ambient",
		entries: [ {
			field00: 1,
			effectPath: "system/item_drop_equip.efp",
			boneName: "",
			vector3c: [ 0, 0, 0 ],
			field4c: 0,
			flags50: [ 0, 0, 0 ],
			flag53: 255,
			vector54: [ Math.PI / 2, 0, 0 ]
		} ]
	} ] );
	const emitter = createModelEmission( () => -1 ),
		parent = {
			gid: 1,
			model: "body",
			pose: { regionId: 257, x: 0, y: 0, z: 0, yaw: Math.PI },
			scale: 1,
			clip: "stand",
			time: 0,
			loop: true
		},
		child = emitter.step( [ { actor: parent, particles } ], 0, () => true, 10 )[0];
	const c = createCharacters(), effect = model();
	effect.clips[0].name = "effect";
	c.model( "body", model(), [] );
	c.model( child.model, effect, [] );
	c.actors( [ parent, child ] );
	child.attachment.rotation[0] = 123;
	const matrices = [],
		gpu = {
			/*
================
upload
================
			*/
			upload( data ) {
				matrices.push( data.instances.slice() );
				return {};
			},
			updateInstances: d => d,
			/*
================
updateBones
================
			*/
			updateBones() {},
			/*
================
release
================
			*/
			release() {}
		};
	c.prepare( gpu, {}, 257 );
	const matrix = matrices[1];
	assert.ok( Math.abs( matrix[0] - 1 ) < 1e-6, JSON.stringify( [ ...matrix ] ) );
	assert.ok( Math.abs( matrix[6] + 1 ) < 1e-6 );
	assert.ok( Math.abs( matrix[9] - 1 ) < 1e-6 );
	c.dispose( gpu, null );
});

test("renderer LOD retains the prior skeletal sample while world placement and animation time advance", () => {
	const c = createCharacters();
	c.model(
		"body",
		model( [ node( "root" ) ], [ {
			node: 0,
			path: "translation",
			interpolation: "LINEAR",
			times: Float32Array.of( 0, 1 ),
			values: Float32Array.of( 0, 0, 0, 10, 0, 0 )
		} ] ),
		[]
	);
	const gpu = {
		/*
================
upload
================
		*/
		upload() {
			return {};
		},
		/*
================
updateInstances
================
		*/
		updateInstances( draw ) {
			return draw;
		},
		/*
================
updateBones
================
		*/
		updateBones() {},
		/*
================
release
================
		*/
		release() {}
	};
	const actor = {
		gid: 1,
		model: "body",
		pose: { regionId: 257, x: 0, y: 0, z: 0, yaw: 0 },
		scale: 1,
		clip: "stand",
		time: .1,
		loop: true,
		animationLod: { fraction: .8, crowded: true }
	};
	const sample = ( time ) => {
		actor.time = time;
		c.actors( [ actor ] );
		c.prepare( gpu, {}, 257, undefined, false, time );
		return c.localMatrix( [ actor ], 1, "root" )[12];
	};
	assert.equal( sample( .1 ), 1 );
	actor.pose.x = 30;
	assert.equal( sample( .2 ), 1 );
	assert.equal( sample( .3 ), 3 );
	actor.animationLod.fraction = .81;
	assert.equal( sample( .4 ), 4 );
	actor.animationLod.crowded = false;
	assert.equal( sample( .5 ), 5 );
	assert.equal( sample( .6 ), 6 );
	c.dispose( gpu, null );
});
test("portraits borrow the character's own parts, never the effects decorating it", () => {
	const c = createCharacters();
	for ( const id of [ "body", "hair", "buff" ] ) c.model( id, model(), [] );
	const at = { regionId: 1, x: 0, y: 0, z: 0, yaw: 0 };
	const actor = ( gid, id, attachment ) => ({
		gid,
		model: id,
		pose: at,
		scale: 1,
		clip: "stand",
		time: 0,
		loop: true,
		...(attachment ? { attachment } : {})
	});
	const hair = actor( 2, "hair", { gid: 1, bone: "root", offset: [ 0, 0, 0 ], basis: "compound" } );
	// A stacked recovery buff: native and BSR effect stages, plus a particle
	// riding one of them. None belongs to the appearance model.
	const effects = [
		actor( 10, "buff", { gid: 1, bone: "root", offset: [ 0, 0, 0 ], basis: "native" } ),
		actor( 11, "buff", { gid: 1, bone: "", root: true, offset: [ 0, 0, 0 ], basis: "native-bsr" } ),
		actor( 12, "buff", { gid: 10, bone: "", root: true, offset: [ 0, 0, 0 ] } )
	];
	c.actors( [ actor( 1, "body" ), hair, ...effects ] );
	const portrait = c.portraitSource( 1 );
	assert.ok( portrait );
	assert.deepEqual( portrait.children?.map( part => part.actor.gid ), [ 2 ] );
	c.dispose( null, null );
});
