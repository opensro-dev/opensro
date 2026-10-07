/*
===========================================================================

combat-presentation.test.mjs - tests for locomotion-blend.ts,
damage-text.ts, ui.ts, character-labels.ts, ...

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { goldDropModels } from "../helpers/gold-drop-models.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";
const { changeLocomotion, locomotionLayers } = await import(
	"../../src/engine/foundation/animation/locomotion-blend.ts"
);
const { damageText, damageTextQuads, appendDamageText, damageTextRise } = await import(
	"../../src/engine/foundation/ui/damage-text.ts"
);
const { copyUi } = await import( "../../src/engine/foundation/ui/ui.ts" );
const { projectCharacterLabels } = await import( "../../src/engine/foundation/ui/character-labels.ts" );
const { worldCursor } = await import( "../../src/engine/foundation/ui/world-cursor.ts" );
const { disappearActor } = await import( "../../src/engine/foundation/animation/disappear.ts" );
test("idle/run transitions retain the outgoing phase, blend over native intervals and remain continuous on reversal", () => {
	let state = changeLocomotion( undefined, "stand", true, 0, "stand" );
	state = changeLocomotion( state, "run", true, 1, "run" );
	assert.deepEqual( locomotionLayers( state, 1 ), [ {
		clip: "stand",
		time: 1,
		loop: true,
		weight: 1,
		lane: "timed",
		activation: state.outgoing[0].layer.activation
	} ] );
	let layers = locomotionLayers( state, 1.05 );
	assert.ok( Math.abs( layers[0].weight - .75 ) < 1e-8 );
	assert.ok( Math.abs( layers[1].weight - .5 ) < 1e-8 );
	const before = layers;
	state = changeLocomotion( state, "stand", true, 1.05, "stand" );
	assert.deepEqual( locomotionLayers( state, 1.05 ), before );
	assert.deepEqual( locomotionLayers( state, 1.3 ).map( l => [ l.clip, l.weight ] ), [ [ "stand", 1 ] ] );
	assert.equal( state.outgoing.length, 0 );
});
test("repeated movement reversals expire old exits instead of overflowing the eight-layer pose mixer", () => {
	let state;
	for ( let i = 0; i < 1000; i++ ) {
		const now = i * .05, clip = i % 2 ? "stand" : "run";
		state = changeLocomotion( state, clip, true, now, clip );
		const layers = locomotionLayers( state, now + .01 );
		assert.ok( layers.length <= 6, `unretired exits at transition ${i}` );
	}
	assert.equal( locomotionLayers( state, 51 ).length, 1 );
});
test("damage feedback covers normal, critical, block, resist, local-player tint and the native rise/alpha", () => {
	const target = { kind: "monster", regionId: 257, x: 0, y: 0, z: 0 },
		impact = { type: 0, damage: 123, flags: 0, secondaryAmount: 0, fatal: false };
	const row = damageText( target, impact, 0 );
	target.y = 99;
	assert.equal( row.anchor.y, 20 );
	const quads = damageTextQuads( [ row ], .75, 1600, 1200 );
	assert.equal( quads.length, 6 );
	assert.deepEqual( quads[0].rect, [ -34, -57.5, 24, 40 ] );
	assert.equal( quads[0].color[3], 127 / 255 );
	assert.deepEqual( damageTextQuads( [ row ], 1, 1600, 1200 ), [] );
	assert.equal( damageText( target, { ...impact, flags: 2 }, 0 ).kind, 1 );
	assert.equal( damageText( target, { ...impact, type: 2 }, 0 ).kind, 2 );
	assert.equal( damageText( target, { ...impact, flags: 16 }, 0 ).kind, 3 );
	assert.deepEqual( damageText( { ...target, kind: "local-player" }, impact, 0 ).color, [ 1, 58 / 255, 58 / 255 ] );
	const block = damageTextQuads( [ damageText( target, { ...impact, type: 2 }, 0 ) ], .5, 1600, 1200 );
	assert.deepEqual( block[0].rect, [ -60, -53, 120, 56 ], "rise is not multiplied by the block glyph scale" );
});
test("damage world anchors are copied, region-correct, survive target removal and reject points behind the camera", () => {
	const row = damageText( { kind: "monster", regionId: 258, x: -1920, y: -20, z: 0 }, {
		damage: 1,
		flags: 0,
		secondaryAmount: 0
	}, 0 );
	const scene = copyUi( {
		revision: 1,
		width: 1600,
		height: 1200,
		quads: damageTextQuads( [ row ], 0, 1600, 1200 )
	} );
	row.anchor.x = 99;
	const matrix = Float32Array.of( 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 );
	const projected = projectCharacterLabels( scene, new Map(), { origin: 257, matrix } );
	assert.deepEqual( projected.quads[0].rect, [ 788, 580, 24, 40 ] );
	assert.equal( projected.quads[0].worldAnchor, undefined );
	matrix[15] = -1;
	assert.equal( projectCharacterLabels( scene, new Map(), { origin: 257, matrix } ).quads.length, 0 );
	assert.throws(
		() =>
			copyUi( { ...scene, quads: [ { ...scene.quads[0], worldAnchor: { regionId: 1, x: NaN, y: 0, z: 0 } } ] } ),
		/anchor/
	);
});
test("frame damage text follows the scene's labels and windows, and leaves with the world", () => {
	const row = damageText( { kind: "monster", regionId: 257, x: 0, y: -20, z: 0 }, {
		damage: 7,
		flags: 0,
		secondaryAmount: 0
	}, 0 );
	const label = {
		rect: [ -5, -5, 10, 10 ],
		clip: [ 0, 0, 1600, 1200 ],
		uv: [ 0, 0, 1, 1 ],
		texture: "name",
		color: [ 1, 1, 1, 1 ],
		characterAnchor: 9
	};
	const window = {
		rect: [ 0, 0, 50, 50 ],
		clip: [ 0, 0, 1600, 1200 ],
		uv: [ 0, 0, 1, 1 ],
		texture: "window",
		color: [ 1, 1, 1, 1 ]
	};
	const scene = { revision: 1, width: 1600, height: 1200, quads: [ label, window ], damageText: true };
	const live = damageTextQuads( [ row ], 0, 1600, 1200 ),
		matrix = Float32Array.of( 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 );
	const projected = projectCharacterLabels( scene, new Map( [ [ 9, [ 100, 100, .5 ] ] ] ), {
		origin: 257,
		matrix
	}, live );
	assert.deepEqual( projected.quads.map( q => q.texture ), [ "name", "window", ...live.map( q => q.texture ) ] );
	assert.ok( projected.quads.slice( 2 ).every( q => q.depth !== undefined && !q.worldAnchor ) );
	assert.equal(
		projectCharacterLabels( scene, new Map( [ [ 9, [ 100, 100, .5 ] ] ] ), undefined, live ).quads.length,
		2
	);
	assert.equal( copyUi( scene ).damageText, true );
	assert.equal( copyUi( { ...scene, damageText: false } ).damageText, undefined );
});
test("monster hover honors server attack flags and native sibling pickup/talk/gate cursors", () => {
	const local = { kind: "local-player" };
	assert.equal( worldCursor( { kind: "monster" }, local ), 0x97 );
	assert.equal( worldCursor( { kind: "monster", attackFlags: 0 }, local ), 0x95 );
	assert.equal( worldCursor( { kind: "ground-item" }, local ), 0x99 );
	assert.equal( worldCursor( { kind: "ground-item" }, local, true ), 0x9a );
	assert.equal( worldCursor( { kind: "npc" }, local ), 0x98 );
	assert.equal( worldCursor( { kind: "teleport" }, local ), 0xa1 );
});
test("despawn keeps only an unpickable fading model, advances its final animation and retires at 1.5 seconds", () => {
	const row = {
		started: 2,
		actor: {
			gid: -1073741824,
			clip: "death",
			time: .3,
			loop: false,
			opacity: 1,
			layers: [ { clip: "death", time: .3, weight: 1, lane: "timed", loop: false } ]
		}
	};
	const actor = disappearActor( row, 2.75 );
	assert.equal( defined( actor ).pickable, false );
	assert.equal( defined( actor ).opacity, .5 );
	assert.equal( defined( actor ).time, 1.05 );
	assert.equal( defined( defined( actor ).layers )[0].time, 1.05 );
	assert.equal( row.actor.time, .3 );
	assert.equal( disappearActor( row, 3.5 ), null );
});
test("authored impact events produce damage text and sparse head reactions while the victim is attacking", async () => {
	const { createCharacterPresentation } = await import( "../../src/engine/runtime/characters/characters.ts" );
	const { createPresentationRandom } = await import( "../../src/engine/runtime/random/random.ts" );
	const clips = [ "stand", "attack1", "hit1", "death" ],
		metadata = Object.fromEntries(
			clips.map(
				name => [ name, {
					durationMs: 1000,
					soundEvents: [],
					trackEvents: name === "attack1" ? [ { cursorMs: 100, eventCode: 1, param0: 0, param1: 0 } ] : []
				} ]
			)
		);
	const catalog = {
		7: {
			phaseClips: [ [], [], [ "attack1" ] ],
			clips: [ "attack1" ],
			stages: [ { phase: "SHOT", startEvent: 1, damageEvent: true } ]
		}
	};
	const pending = new Map();
	let serial = 0, actors = [];
	const bytes = value => ({ kind: "bytes", buffer: new TextEncoder().encode( JSON.stringify( value ) ).buffer });
	const assets = {
		available: () => 4,
		request( url, limit, decode ) {
			pending.set( ++serial, { url, decode } );
			return serial;
		},
		cancel( id ) {
			pending.delete( id );
		},
		take( id ) {
			const job = pending.get( id );
			if ( !job ) return null;
			pending.delete( id );
			if ( job.decode === "effects" ) return { kind: "effects", catalog };
			if ( job.url.endsWith( "/skillfx/manifest.json" ) ) {
				return bytes( {
					format: "sro-skill-stage-models",
					models: {}
				} );
			}
			if ( job.url.endsWith( "/itemdrop/manifest.json" ) ) {
				return bytes( {
					format: "sro-mission-itemdrop-models",
					models: goldDropModels()
				} );
			}
			if ( job.decode === "character" ) {
				return {
					kind: "character",
					images: [],
					model: {
						nodes: [ {
							name: "root",
							parent: -1,
							translation: [ 0, 0, 0 ],
							rotation: [ 0, 0, 0, 1 ],
							scale: [ 1, 1, 1 ]
						} ],
						primitives: [],
						images: [],
						clips: clips.map( name => ({ name, duration: 1, channels: [] }) )
					}
				};
			}
			if ( job.url.endsWith( "/npc/manifest.json" ) ) {
				return bytes( {
					models: [ 1, 2 ].map( refObjId => ({
						refObjId,
						codename: "NPC_" + refObjId,
						glb: `/assets/${refObjId}.glb`,
						clips,
						animationStates: metadata
					}) )
				} );
			}
			return bytes( { models: {} } );
		}
	};
	const renderer = {
		setCharacterModel() {},
		setCharacterAssembly() {},
		retainCharacterModels() {},
		characterSocket: () => null,
		setCharacterActors( value ) {
			actors = value;
		}
	};
	const presenter = createCharacterPresentation(
		assets,
		renderer,
		"http://localhost",
		() => {},
		createPresentationRandom( 1 )
	);
	const entities = [ 1, 2 ].map( gid => ({
		gid,
		refObjId: gid,
		kind: gid === 1 ? "local-player" : "monster",
		appearanceState: [ 1, 0, 0 ],
		regionId: 257,
		x: gid * 10,
		y: 0,
		z: 0,
		heading: 0,
		movementMode: 0
	}) );
	const game = {
		localGid: 1,
		pose: { regionId: 257, x: 10, y: 0, z: 0, angle: 0 },
		inventory: [],
		vitals: [],
		casts: []
	};
	for ( let i = 0; i < 30; i++ ) presenter.step( entities, game, i * .01, i * 10 );
	assert.equal( actors.length, 2 );
	assert.equal( presenter.error(), null );
	game.casts = [ 1, 2 ].map( caster => ({
		token: caster,
		caster,
		target: 3 - caster,
		skill: 7,
		damage: 9,
		fatal: false,
		receivedAtMs: 1000,
		impacts: [ { damage: 9, fatal: false, type: 0, flags: 0, secondaryAmount: 0 } ]
	}) );
	presenter.step( entities, game, 1, 1000 );
	assert.equal( presenter.damageText().length, 0, "packet receipt is not a hit marker" );
	presenter.step( entities, game, 1.31, 1310 );
	assert.equal( presenter.damageText().length, 2 );
	assert.deepEqual(
		actors.find( a => a.gid === 1 ).layers.slice( 0, 2 ).map( l => l.clip ),
		[ "hit1", "attack1" ],
		"attack must not suppress sparse hit reaction"
	);
	presenter.step( entities, game, 1.4, 1400 );
	assert.equal( presenter.damageText().length, 2, "same marker cannot publish twice" );
	assert.deepEqual( presenter.damageText().map( row => row.color ), [ [ 1, 1, 1 ], [ 1, 58 / 255, 58 / 255 ] ] );
	game.casts = [ {
		token: 3,
		caster: 2,
		target: 1,
		skill: 7,
		damage: 9,
		fatal: false,
		receivedAtMs: 3000,
		impacts: [ { damage: 9, fatal: false, type: 7, flags: 0, secondaryAmount: 0 } ]
	} ];
	presenter.step( entities, game, 3, 3000 );
	presenter.step( entities, game, 3.31, 3310 );
	assert.equal(
		actors.find( a => a.gid === 1 ).layers?.some( l => l.clip === "hit1" ) ?? false,
		false,
		"native subtract-2 then subtract-5 excludes type 7"
	);
	entities.push( { ...entities[1], gid: 3, x: 211 } );
	game.casts = [ {
		token: 4,
		caster: 2,
		target: 3,
		skill: 7,
		damage: 9,
		fatal: false,
		receivedAtMs: 5000,
		impacts: [ { damage: 9, fatal: false, type: 0, flags: 0, secondaryAmount: 0 } ]
	} ];
	presenter.step( entities, game, 5, 5000 );
	presenter.step( entities, game, 5.31, 5310 );
	assert.equal(
		presenter.damageText().some( row => row.anchor.x === 211 ),
		false,
		"distant third-party fight does not allocate feedback"
	);
	game.casts = [ { ...game.casts[0], token: 5, caster: 1, receivedAtMs: 6000 } ];
	presenter.step( entities, game, 6, 6000 );
	presenter.step( entities, game, 6.31, 6310 );
	assert.equal(
		presenter.damageText().some( row => row.anchor.x === 211 ),
		true,
		"local attacker keeps feedback beyond the near-player gate"
	);
	game.casts = [];
	game.environmentalDamage = [ { sequence: 1, gid: 1, damage: 17, atMs: 10000 } ];
	presenter.step( entities, game, 10, 10000 );
	assert.equal( presenter.damageText().length, 1, "environmental wire feedback reaches the presenter" );
	presenter.step( entities, game, 10.01, 10010 );
	assert.equal( presenter.damageText().length, 1, "snapshot replay cannot duplicate environmental feedback" );
	game.environmentalDamage = [];
	game.casts = [ {
		token: 6,
		caster: 2,
		target: 1,
		skill: 7,
		damage: 9,
		fatal: false,
		receivedAtMs: 14000,
		cancelledAtMs: 14001,
		impacts: [ { damage: 9, fatal: false, type: 0, flags: 0, secondaryAmount: 0 } ]
	} ];
	presenter.step( entities, game, 14.01, 14010 );
	assert.equal(
		presenter.damageText().length,
		1,
		"cancellation flush reaches presentation before any animation marker"
	);
	presenter.step( entities, game, 14.02, 14020 );
	assert.equal( presenter.damageText().length, 1 );
	// Local movement publishes pose independently of the spawn/entity row.
	// Aggro must show incoming damage before the player has attacked at all.
	presenter.reset();
	game.casts = [];
	game.pose = { regionId: 258, x: 400, y: 30, z: 500, angle: 0 };
	entities[1] = { ...entities[1], regionId: 258, x: 410, y: 30, z: 500 };
	for ( let i = 0; i < 30; i++ ) presenter.step( entities, game, 18 + i * .01, 18000 + i * 10 );
	game.casts = [ {
		token: 7,
		caster: 2,
		target: 1,
		skill: 7,
		damage: 9,
		fatal: false,
		receivedAtMs: 19000,
		impacts: [ { damage: 9, fatal: false, type: 0, flags: 0, secondaryAmount: 0 } ]
	} ];
	presenter.step( entities, game, 19, 19000 );
	assert.equal( presenter.damageText().length, 0 );
	presenter.step( entities, game, 19.31, 19310 );
	assert.equal(
		presenter.damageText().length,
		1,
		"incoming monster hit uses live local pose for distance admission"
	);
	assert.deepEqual( presenter.damageText()[0].anchor, { regionId: 258, x: 400, y: 50, z: 500 } );
	assert.deepEqual( presenter.damageText()[0].color, [ 1, 58 / 255, 58 / 255 ] );
	presenter.step( entities, game, 19.2, 19200 );
	assert.equal( presenter.damageText().length, 1, "incoming marker is consumed once" );
	presenter.dispose();
});

test("every combat glyph and shadow retains explicit non-occluding world presentation", () => {
	for (
		const impact of [ { damage: 123, flags: 0 }, { damage: 123, flags: 2 }, { damage: 0, type: 2, flags: 0 }, {
			damage: 0,
			flags: 16,
			secondaryAmount: 0
		} ]
	) {
		const rows = damageTextQuads(
			[ damageText( { kind: "monster", regionId: 257, x: 0, y: -20, z: .75 }, impact, 0 ) ],
			.2,
			1600,
			1200
		);
		assert.ok( rows.length >= 2 );
		const scene = copyUi( { revision: 1, width: 1600, height: 1200, quads: rows } );
		const matrix = Float32Array.of( 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 );
		const projected = projectCharacterLabels( scene, new Map(), { origin: 257, matrix } );
		assert.equal( projected.quads.length, rows.length );
		assert.ok( projected.quads.every( q => q.depth === .75 && q.occlusion === "none" ) );
		matrix[14] = 1;
		assert.equal( projectCharacterLabels( scene, new Map(), { origin: 257, matrix } ).quads.length, 0 );
	}
	const q = {
		texture: "",
		rect: [ 0, 0, 1, 1 ],
		clip: [ 0, 0, 10, 10 ],
		color: [ 1, 1, 1, 1 ],
		uv: [ 0, 0, 1, 1 ],
		occlusion: "typo"
	};
	assert.throws( () => copyUi( { revision: 1, width: 10, height: 10, quads: [ q ] } ), /occlusion/ );
});

test("native rapid-hit text insertion shifts older target texts and restarts rise without restarting alpha", () => {
	const target = { gid: 1, kind: "monster", regionId: 257, x: 0, y: 0, z: 0 };
	const make = ( at, flags = 0, type = 0, gid = 1 ) =>
		damageText( { ...target, gid }, { damage: 12, flags, type, secondaryAmount: 0 }, at );
	const original = make( 0 ), other = make( 0, 0, 0, 2 );
	let rows = appendDamageText( [ original, other ], make( .1 ), .1 );
	assert.equal( damageTextRise( rows[0], .1 ), 45 );
	assert.equal( damageTextRise( rows[0], .6 ), 67.5, "remaining rise now takes a full second" );
	assert.equal( rows[1], other, "another target is untouched" );
	assert.equal( original.rise, undefined, "published snapshots remain immutable" );
	rows = appendDamageText( rows, make( .1 ), .1 );
	assert.deepEqual(
		rows.map( row => damageTextRise( row, .1 ) ),
		[ 85, 5, 40, 0 ],
		"simultaneous hits stack in publication order"
	);
	assert.equal( rows[0].started, 0 );
	assert.equal( damageTextQuads( [ rows[0] ], .75, 1600, 1200 )[0].color[3], 127 / 255 );
	assert.deepEqual( damageTextQuads( [ rows[0] ], 1, 1600, 1200 ), [], "new hits cannot prolong opacity" );
	for ( const [flags, type, gap] of [ [ 2, 0, 80 ], [ 0, 2, 50 ], [ 16, 0, 0 ] ] ) {
		const shifted = appendDamageText( [ make( 0, flags, type ) ], make( .1 ), .1 );
		assert.equal( damageTextRise( shifted[0], .1 ), 5 + gap, "native spacing uses existing text kind" );
	}
	const expired = make( -4 );
	assert.equal( appendDamageText( [ expired ], make( 0 ), 0 )[0], expired );
});
