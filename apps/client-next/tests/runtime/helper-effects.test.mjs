/*
===========================================================================

helper-effects.test.mjs - tests for the client modules it imports

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFile, mkdir } from "node:fs/promises";
import { pathToFileURL } from "node:url";
import { resolve } from "node:path";
await mkdir( "temp/artifacts/helper-tests", { recursive: true } );
async function load( file, name ) {
	return import( sourceFileUrl( file ).href );
}
const { createEffectPrograms } = await load( "src/engine/runtime/assets/worker/effects/program/program.ts", "program" );
const { createEffectDecoder } = await load( "src/engine/runtime/assets/worker/effects/effects.ts", "decoder" );
const { createCharacterEffects } = await load( "src/engine/runtime/characters/effects/effects.ts", "owner" );
const { createPresentationRandom } = await load( "src/engine/runtime/random/random.ts", "random" );
const encode = v => new TextEncoder().encode( JSON.stringify( v ) );
const programs = await readFile( "../../.generated/client-public/assets/effects/programs.json" );
const records = await readFile( "../../.generated/client-public/assets/skill/effectRecords.json" );
const manifest = await readFile( "../../.generated/client-public/assets/skillfx/manifest.json" );

test("retail helper EFP compiles all three plates and its rotation without a substitute icon", () => {
	const { model, imagePaths } = createEffectPrograms().decode( programs, "system/system_helpermark.efp" );
	assert.equal( model.primitives.length, 3 );
	assert.equal( model.primitives.filter( p => p.billboard === "camera" ).length, 3 );
	assert.equal( model.clips[0].duration, 1.5 );
	assert.ok( imagePaths.some( p => p.includes( "icon_cha_helper" ) ) );
	const rotation = model.nodes.find( n => n.name === "helpermark:rotation" );
	assert.deepEqual( rotation.matrix, [ 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 ] );
});

test("helper lifetime survives casts, slow assets and movement; clears on flag, despawn and reset", () => {
	let id = 0, admitted = false;
	const jobs = new Map();
	const catalog = createEffectDecoder().decode( records );
	const owner = createCharacterEffects(
		{
			available: () => 4,
			request( url, limit, kind ) {
				jobs.set(
					++id,
					kind === "effects" ?
						{ kind: "effects", catalog } :
						{ kind: "bytes", buffer: Uint8Array.from( manifest ).buffer }
				);
				return id;
			},
			take( id ) {
				const r = jobs.get( id );
				jobs.delete( id );
				return r;
			},
			cancel( id ) {
				jobs.delete( id );
			}
		},
		"http://localhost",
		() => {},
		createPresentationRandom( 1 )
	);
	let entities = [ { gid: 10, visualFlags: 2, refObjId: 1907, regionId: 257, x: 100, y: 0, z: 100, heading: 0 } ];
	let body = {
		gid: 10,
		height: 20,
		model: "/assets/body.glb",
		pose: { regionId: 257, x: 100, y: 0, z: 100, yaw: 0 },
		clip: "stand",
		time: 0,
		loop: true,
		scale: 1
	};
	const gameplay = { casts: [] };
	const wanted = [];
	const step = t =>
		owner.step(
			entities,
			gameplay,
			t,
			path => {
				wanted.push( path );
				return admitted;
			},
			() => 1.5,
			[],
			undefined,
			[ body ]
		);
	step( 0 );
	step( .1 );
	assert.deepEqual( step( .2 ), [] );
	assert.deepEqual( step( 5 ), [] );
	admitted = true;
	const first = step( 6 );
	assert.equal( first.length, 2 );
	assert.equal( owner.error(), null );
	const parent = first.find( a => a.model.endsWith( ".glb" ) ),
		particle = first.find( a => a.model.includes( "programs.json" ) );
	assert.equal( parent.attachment.gid, 10 );
	assert.equal( parent.attachment.root, true );
	assert.deepEqual( parent.attachment.offset, [ 0, 33, 0 ] );
	assert.equal( particle.attachment.gid, parent.gid );
	assert.deepEqual( particle.attachment.offset, [ 0, 3, -0 ] );
	assert.equal( particle.time, 0 );
	assert.equal( particle.loop, true );
	gameplay.casts = [ { token: 7, caster: 10, target: 11, skill: 1 } ];
	body = { ...body, pose: { ...body.pose, x: 400, y: 25 } };
	assert.equal( step( 10 ).find( a => a.gid === particle.gid ).time, 4 );
	gameplay.casts = [];
	wanted.length = 0;
	assert.equal( step( 11 ).length, 2 );
	assert.ok(
		wanted.includes( parent.model ) && wanted.includes( particle.model ),
		"persistent models must renew asset residency every frame"
	);
	entities = [ { ...entities[0], visualFlags: 1 } ];
	assert.deepEqual( step( 12 ), [] );
	entities = [ { ...entities[0], visualFlags: 3 } ];
	assert.equal( step( 13 ).length, 2 );
	entities = [];
	assert.deepEqual( step( 14 ), [] );
	entities = [ { gid: 10, visualFlags: 2, refObjId: 1907 } ];
	assert.equal( step( 15 ).length, 2 );
	owner.reset();
	assert.equal( step( 16 )[0].time, 0 );
	owner.dispose();
	assert.deepEqual( step( 17 ), [] );
});

test("static rotation coexists with animated scale and unsupported timing stays explicit", () => {
	const catalog = JSON.parse( programs ), root = catalog.effects["system/system_helpermark.efp"].root;
	// RotVector.left is authored degrees; right is native conversion scratch.
	root.renderProgram[0].parameter.left = [ 0, 0, 90 ];
	root.renderProgram[0].parameter.right = Array( 16 ).fill( 123 );
	const { model } = createEffectPrograms().decode( encode( catalog ), "system/system_helpermark.efp" );
	const matrix = model.nodes.find( n => n.name === "helpermark:rotation" ).matrix,
		expected = [ 0, 1, 0, 0, -1, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 ];
	for ( let i = 0; i < 16; i++ ) assert.ok( Math.abs( matrix[i] - expected[i] ) < 1e-6, `rotation component ${i}` );
	assert.ok( model.clips[0].channels.every( c => !model.nodes[c.node].matrix ) );
	root.renderProgram[0].step = 1;
	assert.throws(
		() => createEffectPrograms().decode( encode( catalog ), "system/system_helpermark.efp" ),
		/rotation assignment/
	);
});

test("skeleton-only GLB admission does not permit dangling mesh references", async () => {
	const { createCharacterDecoder } = await load(
		"src/engine/runtime/assets/worker/model/character/character.ts",
		"character"
	);
	const document = { json: { nodes: [ { name: "root" } ] }, binary: new ArrayBuffer( 0 ) };
	assert.equal( createCharacterDecoder().decode( document ).primitives.length, 0 );
	assert.throws(
		() => createCharacterDecoder().decode( { ...document, json: { nodes: [ { name: "root", mesh: 0 } ] } } ),
		/mesh/i
	);
});

test("quest marker registry renders every authored family and retires replacements without stale actors", () => {
	let id = 0;
	const jobs = new Map(), catalog = createEffectDecoder().decode( records );
	const owner = createCharacterEffects(
		{
			available: () => 4,
			request( url, limit, kind ) {
				jobs.set(
					++id,
					kind === "effects" ?
						{ kind: "effects", catalog } :
						{ kind: "bytes", buffer: Uint8Array.from( manifest ).buffer }
				);
				return id;
			},
			take( id ) {
				const r = jobs.get( id );
				jobs.delete( id );
				return r;
			},
			cancel( id ) {
				jobs.delete( id );
			}
		},
		"http://localhost",
		() => {},
		createPresentationRandom( 1 )
	);
	const entities = [ { gid: 10, refObjId: 1907, kind: "npc" } ],
		body = {
			gid: 10,
			height: 20,
			model: "/assets/body.glb",
			pose: { regionId: 257, x: 100, y: 0, z: 100, yaw: 0 },
			clip: "stand",
			time: 0,
			loop: true,
			scale: 1
		};
	const gameplay = { casts: [], questMarkers: [] };
	let now = 0;
	const step = () => owner.step( entities, gameplay, now += .1, () => true, () => 1.5, [], undefined, [ body ] );
	step();
	step();
	let old = [];
	for (
		const [state, name] of [ [ 1, "ex_mark_start" ], [ 2, "ex_mark_going" ], [ 3, "ex_mark_end" ], [
			4,
			"ex_mark_start_r"
		] ]
	) {
		gameplay.questMarkers = [ { refId: 1, flags: 2, valueA: state, optional: 10 } ];
		const actors = step();
		assert.ok( actors.some( a => a.model.includes( name ) ), JSON.stringify( actors ) );
		assert.ok( actors.every( a => !old.includes( a.gid ) ), "state replacement retires the preceding family" );
		old = actors.map( a => a.gid );
		const parent = actors.find( a => a.model.includes( name ) );
		assert.deepEqual( parent.attachment.offset, [ 0, 25, 0 ] );
	}
	gameplay.questMarkers = [ { refId: 2, flags: 2, valueA: 3, optional: 10 }, {
		refId: 1,
		flags: 2,
		valueA: 2,
		optional: 10
	} ];
	assert.ok( step().some( a => a.model.includes( "ex_mark_going" ) ), "native first ascending key wins for one NPC" );
	gameplay.questMarkers = [];
	assert.deepEqual( step(), [] );
	owner.dispose();
});

test("missing marker metadata reports its identity without suppressing another actors fire and sound; recovery admits marker", () => {
	let id = 0;
	const jobs = new Map(), catalog = createEffectDecoder().decode( records ), sounds = [];
	const owner = createCharacterEffects(
		{
			available: () => 4,
			request( url, limit, kind ) {
				jobs.set(
					++id,
					kind === "effects" ?
						{ kind: "effects", catalog } :
						{ kind: "bytes", buffer: Uint8Array.from( manifest ).buffer }
				);
				return id;
			},
			take( id ) {
				const r = jobs.get( id );
				jobs.delete( id );
				return r;
			},
			cancel( id ) {
				jobs.delete( id );
			}
		},
		"http://localhost",
		sound => sounds.push( sound ),
		createPresentationRandom( 1 )
	);
	const entities = [ { gid: 10, refObjId: 2005, kind: "npc" }, { gid: 11, refObjId: 1911, kind: "local-player" } ];
	const body = {
		model: "/assets/body.glb",
		pose: { regionId: 257, x: 100, y: 0, z: 100, yaw: 0 },
		clip: "stand",
		time: 0,
		loop: true,
		scale: 1
	};
	let npc = { ...body, gid: 10 };
	const player = { ...body, gid: 11, height: 20, heightFactor: 1 };
	const game = {
		casts: [],
		questMarkers: [ { refId: 1, flags: 2, valueA: 1, optional: 10 } ],
		attachedEffects: [ { gid: 11, skill: 124, token: 99, phase: 2 } ]
	};
	const step = t => owner.step( entities, game, t, () => true, () => 1.5, [], undefined, [ npc, player ] );
	step( 0 );
	step( .1 );
	const actors = step( .2 );
	assert.match( owner.error(), /actor 10, reference 2005.*Missing native effect anchor height/ );
	assert.ok( actors.some( a => a.model.includes( "fire_gigongta_keep" ) ) );
	assert.ok( sounds.some( s => s.path.endsWith( "csk_fire_gigong_hand.wav" ) ) );
	npc = { ...npc, height: 16 };
	assert.ok( step( .3 ).some( a => a.model.includes( "ex_mark_start" ) ) );
	assert.equal( owner.error(), null );
	owner.dispose();
});
