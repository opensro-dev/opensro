/*
===========================================================================

sound-presentation.test.mjs - tests for the client modules it imports

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { defined } from "../helpers/defined.mjs";
async function load( file ) {
	return import( sourceFileUrl( "src/engine/" + file + ".ts" ).href );
}
const { decodeSoundTerrain, sampleSoundTerrain } = await load( "foundation/audio/terrain-sounds" );
const { createCharacterSounds } = await load( "runtime/characters/sounds/sounds" );
test("retail pickup cue selects ITEM once for any picker, without positional attenuation", () => {
	const heard = [], sounds = createCharacterSounds( e => heard.push( e ) );
	const catalog = JSON.parse(
		readFileSync( CLIENT_PUBLIC_ROOT + "/assets/audio/effectsound.json", "utf8" )
	);
	const manifest = JSON.parse( readFileSync( CLIENT_PUBLIC_ROOT + "/assets/anim/manifest.json", "utf8" ) );
	sounds.catalog( catalog.rules );
	const pick = manifest.models.CHAR_CH_MAN_ADVENTURER.pick;
	sounds.advance(
		1,
		"pick",
		10,
		0,
		false,
		pick,
		10,
		() => ({
			profile: "PCM_ADVENTURER",
			position: [ 9000, 100, 8000 ],
			surface: undefined,
			context: { player: true, skill: "FIRE", critical: true }
		}),
		"pose"
	);
	sounds.advance(
		1,
		"pick",
		10,
		.2,
		false,
		pick,
		10.2,
		() => ({
			profile: "PCM_ADVENTURER",
			position: [ 9000, 100, 8000 ],
			surface: undefined,
			context: { player: false }
		}),
		"pose"
	);
	assert.equal( heard.length, 1 );
	assert.match( heard[0].path, /\/itpickup\.wav$/ );
	assert.equal( heard[0].spatial, false );
	assert.equal( heard[0].gain, 1 );
});
test("Manyang authored attack cues select only their physical attack rows", () => {
	const heard = [],
		sounds = createCharacterSounds( e => heard.push( e ) ),
		catalog = JSON.parse( readFileSync( CLIENT_PUBLIC_ROOT + "/assets/audio/effectsound.json", "utf8" ) );
	sounds.catalog( catalog.rules );
	for ( const [n, expected] of [ [ 1, "battswordhit2n.wav" ], [ 2, "bataxehit2n.wav" ] ] ) {
		const skill = "MSKILL_CH_MANGNYANG_ATTACK0" + n;
		assert.equal(
			sounds.emit(
				"hit" + n,
				"MOB_MANGNYANG",
				[ "SND_CRIDMG", "SND_DMG" ],
				{ player: false, skill, critical: true },
				[ 0, 0, 0 ],
				0
			),
			true
		);
		assert.ok( heard.at( -1 ).path.endsWith( "/" + expected ) );
	}
	const count = heard.length;
	assert.equal(
		sounds.emit( "missing", "MOB_MANGNYANG", [ "SND_DMG" ], { player: false, skill: "UNRELATED_FIRE_SKILL" }, [
			0,
			0,
			0
		], 0 ),
		false
	);
	assert.equal( heard.length, count );
});
const { createAudio } = await load( "runtime/audio/audio" );
const { createPresentationRandom } = await load( "runtime/random/random" );

test("buff retirement uses the published positional cue without a variant RNG draw", async t => {
	const paths = [], panners = [], started = [], gains = [];
	const node = () => ({ connect() {}, disconnect() {} });
	class Context {
		state = "running";
		destination = {};
		listener = { positionX: {}, positionY: {}, positionZ: {} };
		resume() {
			return Promise.resolve();
		}
		close() {
			return Promise.resolve();
		}
		decodeAudioData() {
			return Promise.resolve( { length: 100, numberOfChannels: 1 } );
		}
		createBufferSource() {
			return {
				...node(),
				start() {
					started.push( this );
				},
				stop() {
					this.onended?.();
				}
			};
		}
		createGain() {
			const n = { ...node(), gain: {} };
			gains.push( n );
			return n;
		}
		createPanner() {
			const n = { ...node(), positionX: {}, positionY: {}, positionZ: {} };
			panners.push( n );
			return n;
		}
	}
	const old = globalThis.AudioContext;
	globalThis.AudioContext = Context;
	t.after( () => {
		if ( old ) globalThis.AudioContext = old;
		else delete globalThis.AudioContext;
	} );
	const audio = createAudio(
		{
			available: () => 4,
			request: path => {
				paths.push( path );
				return paths.length;
			},
			take: () => ({ kind: "bytes", buffer: new ArrayBuffer( 1 ) }),
			cancel() {}
		},
		"http://fixture.invalid",
		{
			range() {
				throw Error( "Literal buff sound must not consume RNG" );
			}
		}
	);
	audio.unlock();
	audio.buffEnded( 0, [ 100, 20, 200 ] );
	audio.step( 0, [ 100, 20, 200 ] );
	audio.step( .01, [ 100, 20, 200 ] );
	await new Promise( setImmediate );
	audio.step( .02, [ 100, 20, 200 ] );
	assert.equal( started.length, 1 );
	assert.ok( paths[0].endsWith( "/sfx/prim/snd/ui/buf_disappear.wav" ) );
	// Web Audio is right-handed: the left-handed world reaches it with Z mirrored (space.ts).
	assert.deepEqual( [ panners[0].positionX.value, panners[0].positionY.value, panners[0].positionZ.value ], [
		100,
		20,
		-200
	] );
	assert.equal(
		gains[0].gain.value,
		Math.pow( 10, -.5 ),
		"unit cue gain still obeys the native default effects slider"
	);
	audio.dispose();
});
test("published Jangan surface and player animation select authored movement sounds", () => {
	const bundle = JSON.parse(
			readFileSync( CLIENT_PUBLIC_ROOT + "/assets/world/outdoor/regions/region-61a8.json", "utf8" )
		),
		terrain = decodeSoundTerrain( bundle );
	assert.ok( terrain.length > 0 );
	const row = terrain.find( row => row.regionId === 0x61a8 ), index = row.types.indexOf( 3 );
	assert.ok( index >= 0, "Jangan contains stone ground" );
	const surface = sampleSoundTerrain( terrain, {
		regionId: row.regionId,
		x: (index % 96) * 20 + 1,
		z: Math.floor( index / 96 ) * 20 + 1
	} );
	assert.equal( surface, "STONE" );
	assert.equal( sampleSoundTerrain( [], {} ), undefined );
	const heard = [], sounds = createCharacterSounds( e => heard.push( e ), () => 0 );
	sounds.catalog(
		JSON.parse( readFileSync( CLIENT_PUBLIC_ROOT + "/assets/audio/effectsound.json", "utf8" ) ).rules
	);
	const run = JSON.parse( readFileSync( CLIENT_PUBLIC_ROOT + "/assets/anim/manifest.json", "utf8" ) ).models
		.CHAR_CH_MAN_ADVENTURER.run;
	sounds.advance(
		1,
		"run",
		0,
		0,
		true,
		run,
		0,
		() => ({ profile: "CH_MAN", position: [ 0, 0, 0 ], surface: surface, context: { player: true } }),
		"pose"
	);
	sounds.advance(
		1,
		"run",
		0,
		.31,
		true,
		run,
		.31,
		() => ({ profile: "CH_MAN", position: [ 0, 0, 0 ], surface: surface, context: { player: true } }),
		"pose"
	);
	assert.equal( heard.length, 1 );
	assert.match( heard[0].path, /mvwalkhground/i );
});
test("footstep variants match the complete surface selector instead of being dropped or mixed", () => {
	const heard = [], sounds = createCharacterSounds( e => heard.push( e ), ( min, max ) => max - 1 );
	const rule = ( event2, file ) => ({
		object: "PLAYER",
		handle: "SND_WALK1",
		skillId: "-",
		event1: "FIELD",
		event2,
		event3: "-",
		publicPath: "/assets/audio/" + file
	});
	sounds.catalog( [ rule( "WOOD", "a.wav" ), rule( "WOOD", "b.wav" ), rule( "STONE", "stone.wav" ), {
		...rule( "WOOD", "skill.wav" ),
		skillId: "123"
	} ] );
	sounds.advance(
		1,
		"walk",
		0,
		.1,
		true,
		{ durationMs: 1000, soundEvents: [ { cursorMs: 50, cue: "snd_walk1" } ] },
		.1,
		() => ({ profile: "CHAR", position: [ 0, 0, 0 ], surface: "WOOD", context: { player: true } }),
		"pose"
	);
	assert.equal( heard.length, 1 );
	assert.ok( heard[0].path.endsWith( "/b.wav" ) );
});
test("distant cues allocate nothing; cold expired cues warm the cache without late playback", async t => {
	let requested = 0, cancelled = 0, started = 0, finish;
	const panners = [];
	const node = () => ({ connect() {}, disconnect() {} });
	class Context {
		state = "running";
		destination = {};
		listener = { positionX: {}, positionY: {}, positionZ: {} };
		resume() {
			return Promise.resolve();
		}
		close() {
			return Promise.resolve();
		}
		decodeAudioData() {
			return new Promise( resolve => finish = resolve );
		}
		createBufferSource() {
			return {
				...node(),
				start() {
					started++;
				},
				stop() {
					this.onended?.();
				}
			};
		}
		createGain() {
			return { ...node(), gain: {} };
		}
		createPanner() {
			const p = { ...node(), positionX: {}, positionY: {}, positionZ: {} };
			panners.push( p );
			return p;
		}
	}
	const old = globalThis.AudioContext;
	globalThis.AudioContext = Context;
	t.after( () => {
		if ( old ) globalThis.AudioContext = old;
		else delete globalThis.AudioContext;
	} );
	const audio = createAudio(
		{
			available: () => 4,
			request: () => ++requested,
			take: () => ({ kind: "bytes", buffer: new ArrayBuffer( 1 ) }),
			cancel() {
				cancelled++;
			}
		},
		"http://fixture.invalid",
		createPresentationRandom( 1 )
	);
	audio.unlock();
	const cue = ( id, x, expires ) => ({ id, path: "/assets/audio/step.wav", gain: 1, x, y: 0, z: 0, expires });
	audio.enqueue( cue( "far", 300, 10 ) );
	audio.step( 0, [ 0, 0, 0 ] );
	assert.equal( requested, 0 );
	audio.enqueue( cue( "cold", 0, .25 ) );
	audio.step( 0, [ 0, 0, 0 ] );
	audio.step( 1, [ 0, 0, 0 ] );
	defined( finish )( { length: 100, numberOfChannels: 1 } );
	await new Promise( setImmediate );
	audio.step( 1, [ 0, 0, 0 ] );
	assert.equal( started, 0 );
	assert.equal( cancelled, 0 );
	audio.enqueue( cue( "next", 0, 2 ) );
	audio.step( 1, [ 0, 0, 0 ] );
	assert.equal( requested, 1 );
	assert.equal( started, 1 );
	assert.equal( panners[0].distanceModel, "linear" );
	assert.equal( panners[0].maxDistance, 300 );
	assert.equal( panners[0].refDistance, 100 );
	assert.equal( panners[0].panningModel, "equalpower" );
	audio.dispose();
});
test("a listener without position parameters (Firefox) is placed through setPosition", t => {
	const placed = [], turned = [];
	const node = () => ({ connect() {}, disconnect() {} });
	class Context {
		state = "running";
		destination = {};
		listener = {
			setPosition( ...xyz ) {
				placed.push( xyz );
			},
			setOrientation( ...v ) {
				turned.push( v );
			}
		};
		resume() {
			return Promise.resolve();
		}
		close() {
			return Promise.resolve();
		}
		createGain() {
			return { ...node(), gain: {} };
		}
	}
	const old = globalThis.AudioContext;
	globalThis.AudioContext = Context;
	t.after( () => {
		if ( old ) globalThis.AudioContext = old;
		else delete globalThis.AudioContext;
	} );
	const audio = createAudio(
		{ available: () => 4, request: () => 1, take: () => undefined, cancel() {} },
		"http://fixture.invalid",
		{ range: () => 0 }
	);
	audio.unlock();
	audio.step( 0, [ 1, 2, 3 ], { forward: [ 0, 0, -1 ], up: [ 0, 1, 0 ] } );
	// World space reaches Web Audio with Z mirrored (space.ts).
	assert.deepEqual( placed.at( -1 ), [ 1, 2, -3 ] );
	assert.deepEqual( turned.at( -1 ), [ 0, 0, 1, 0, 1, 0 ] );
	audio.dispose();
});
