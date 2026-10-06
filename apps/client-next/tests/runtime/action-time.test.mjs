/*
===========================================================================

action-time.test.mjs - tests for action-time.ts, animation-metadata.ts,
random.ts, projectile-time.ts, ...

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { actionCursor, animationWarp, actionMotionWeight, actionStageEvents } = await import(
	"../../src/engine/foundation/animation/action-time.ts"
);
const { animationMetadata } = await import( "../../src/engine/foundation/animation/animation-metadata.ts" );
const { createPresentationRandom } = await import( "../../src/engine/runtime/random/random.ts" );
const definition = animationMetadata( {
	attack: {
		durationMs: 1000,
		trackEvents: [ { cursorMs: 200, eventCode: 1, param0: 42, param1: 7 }, {
			cursorMs: 400,
			eventCode: 2,
			param0: 0,
			param1: 0
		}, { cursorMs: 600, eventCode: 1, param0: 0, param1: 0 } ],
		timeWarpCurve: { scale: 80, records: [ { phase: 0.25, value: 0.5 }, { phase: 0.75, value: 0.8 } ] }
	}
} ).attack;
test("native action callback intervals are half-open and preserve distinct impact stages", () => {
	assert.deepEqual( actionStageEvents( definition, 0, 200, true ), [ 0 ] );
	assert.deepEqual( actionStageEvents( definition, 200, 201, false ), [ 1 ] );
	assert.deepEqual( actionStageEvents( definition, 201, 600, false ), [] );
	assert.deepEqual( actionStageEvents( definition, 600, 601, false ), [ 2 ] );
	assert.deepEqual( actionStageEvents( definition, 601, 601, false ), [] );
	assert.deepEqual( actionStageEvents( definition, 0, 1000, true ), [ 0, 1, 2 ] );
});
test("duration override scales the clip cursor while motion curves preserve implicit endpoints", () => {
	assert.equal( actionCursor( 200, 1000, 500 ), 400 );
	assert.equal( actionCursor( 900, 1000, 500 ), 1000 );
	assert.equal( actionCursor( -20, 1000 ), 0 );
	assert.equal( animationWarp( definition.timeWarpCurve, -1 ), 0 );
	assert.equal( animationWarp( definition.timeWarpCurve, 1.1 ), 1 );
	assert.equal( animationWarp( definition.timeWarpCurve, 0.125 ), 0.25 );
	assert.ok( Math.abs( animationWarp( definition.timeWarpCurve, 0.875 ) - 0.9 ) < 1e-6 );
	assert.equal( actionMotionWeight( definition, 0, 250 ), 0.5 );
	assert.equal( actionMotionWeight( definition, 250, 250 ), 0 );
	assert.equal( actionCursor( 250, 1000 ), 250, "motion integral never replaces the pose cursor" );
});
test("malformed curves and event records fail metadata admission", () => {
	assert.throws(
		() =>
			animationMetadata( {
				a: {
					durationMs: 1000,
					timeWarpCurve: { scale: 1, records: [ { phase: 0.5, value: 0 }, { phase: 0.5, value: 1 } ] }
				}
			} ),
		/curve/
	);
	assert.throws(
		() =>
			animationMetadata( {
				a: { durationMs: 1000, trackEvents: [ { cursorMs: 1.5, eventCode: 1, param0: 0, param1: 0 } ] }
			} ),
		/event map/
	);
});

const { sampleProjectile } = await import( "../../src/engine/foundation/animation/projectile-time.ts" );
const { crtRandomRange } = await import( "../../src/engine/foundation/math/crt-random.ts" );
test("retail speed ranges are upper-exclusive and fixed/reversed ranges do not advance RNG", () => {
	assert.deepEqual( crtRandomRange( 1, 100, 110 ), { state: 2745024, value: 101 } );
	assert.deepEqual( crtRandomRange( 1, 110, 110 ), { state: 1, value: 110 } );
	assert.deepEqual( crtRandomRange( 1, 110, 100 ), { state: 1, value: 110 } );
	let state = 1;
	for ( let i = 0; i < 1000; i++ ) {
		const next = crtRandomRange( state, -10, 10 );
		state = next.state;
		assert.ok( next.value >= -10 && next.value < 10 );
	}
});
test("rotated arc is a rendering offset and never extends mechanical arrival time", () => {
	const start = { regionId: 257, x: 100, y: 0, z: 100, yaw: 0 }, end = { ...start, x: 300 };
	const vertical = sampleProjectile( start, end, 100, 0, 1, { amplitudePermille: 500, rotationRadians: 0 } );
	assert.equal( vertical.phase, "travel" );
	assert.ok( Math.abs( vertical.pose.y - 100 ) < 1e-4 );
	assert.equal( vertical.pose.x, 200 );
	const side = sampleProjectile( start, end, 100, 0, 1, { amplitudePermille: 500, rotationRadians: Math.PI / 2 } );
	assert.ok( Math.abs( side.pose.x - 100 ) < 1e-4 );
	assert.ok( Math.abs( side.pose.y ) < 1e-4 );
	assert.deepEqual( sampleProjectile( start, end, 100, .2, 3, { amplitudePermille: 5000, rotationRadians: 1 } ), {
		phase: "arrived",
		at: 2.2,
		pose: end
	} );
});
test("projectile delay and 3D arrival are independent of render cadence and cross region boundaries", () => {
	const start = { regionId: 257, x: 1910, y: 0, z: 30, yaw: 0 }, end = { regionId: 258, x: 20, y: 40, z: 30, yaw: 0 };
	assert.deepEqual( sampleProjectile( start, end, 100, 0.2, 0.1 ), { phase: "delay" } );
	const middle = sampleProjectile( start, end, 100, 0.2, 0.45 );
	assert.equal( middle.phase, "travel" );
	assert.equal( middle.pose.regionId, 258 );
	assert.ok( Math.abs( middle.pose.x - 5 ) < 1e-6 );
	assert.equal( middle.pose.y, 20 );
	const arrived = sampleProjectile( start, end, 100, 0.2, 2 );
	assert.equal( arrived.phase, "arrived" );
	assert.equal( arrived.at, 0.7 );
	assert.deepEqual( arrived.pose, end );
	assert.throws( () => sampleProjectile( { ...start, regionId: 0x8001 }, end, 100, 0, 1 ), /dungeon/ );
});

test("a speed-0 projectile arrives at once when its start is its end and otherwise never moves", () => {
	const start = { regionId: 257, x: 10, y: 0, z: 30, yaw: 0 };
	assert.deepEqual( sampleProjectile( start, { ...start }, 0, 0.2, 0.2 ), {
		phase: "arrived",
		at: 0.2,
		pose: start
	} );
	assert.deepEqual( sampleProjectile( start, { ...start, x: 20 }, 0, 0, 50 ), { phase: "travel", pose: start } );
	assert.throws( () => sampleProjectile( start, start, -1, 0, 0 ), /Invalid projectile clock/ );
});

test("dungeon projectiles preserve dungeon identity and unbounded local coordinates", () => {
	const start = { regionId: 0x8001, x: -2000, y: 10, z: 5000, yaw: 0 }, end = { ...start, x: 2000, y: 3010 };
	const middle = sampleProjectile( start, end, 1000, 0.2, 2.7 );
	assert.equal( middle.phase, "travel" );
	assert.equal( middle.pose.regionId, 0x8001 );
	assert.equal( middle.pose.x, 0 );
	assert.equal( middle.pose.z, 5000 );
	assert.equal( middle.pose.y, 1510 );
	assert.deepEqual( sampleProjectile( start, end, 1000, .2, 10 ), { phase: "arrived", at: 5.2, pose: end } );
	assert.throws( () => sampleProjectile( start, { ...end, regionId: 0x8002 }, 1000, 0, 1 ), /linked dungeon/ );
});

const { createCharacterPresentation } = await import( "../../src/engine/runtime/characters/characters.ts" );

test("live presenter holds the authored WAIT clip and consumes the authoritative SHOT release", () => {
	const names = [ "stand", "ready01", "wait01", "attack1" ], pending = new Map();
	let id = 0, actors = [];
	const encode = value => new TextEncoder().encode( JSON.stringify( value ) ).buffer;
	const assets = {
		available: () => 4,
		request( url, limit, decode ) {
			pending.set( ++id, { url, decode } );
			return id;
		},
		cancel( id ) {
			pending.delete( id );
		},
		take( id ) {
			const job = pending.get( id );
			if ( !job ) return null;
			pending.delete( id );
			if ( job.decode === "effects" ) {
				return {
					kind: "effects",
					catalog: {
						"1": {
							clips: [ "attack1" ],
							phaseClips: [ [ "ready01" ], [ "wait01" ], [ "attack1" ] ],
							stages: []
						}
					}
				};
			}
			if ( job.decode === "character" ) {
				return {
					kind: "character",
					model: {
						nodes: [],
						primitives: [],
						images: [],
						clips: names.map( name => ({ name, duration: 1, channels: [] }) )
					},
					images: []
				};
			}
			let value = {};
			if ( job.url.endsWith( "/roster.json" ) ) {
				value = {
					models: [ {
						refObjId: 1,
						codename: "rider",
						glb: "/assets/rider.glb",
						clips: names,
						animationStates: Object.fromEntries( names.map( name => [ name, { durationMs: 1000 } ] ) )
					} ]
				};
			}
			if ( job.url.endsWith( "/skillfx/manifest.json" ) ) {
				value = { format: "sro-skill-stage-models", models: {} };
			}
			if ( job.url.endsWith( "/itemdrop/manifest.json" ) ) {
				value = {
					format: "sro-mission-itemdrop-models",
					models: {}
				};
			}
			return { kind: "bytes", buffer: encode( value ) };
		}
	};
	const p = createCharacterPresentation(
		assets,
		{
			setCharacterModel() {},
			setCharacterAssembly() {},
			retainCharacterModels() {},
			setCharacterActors( value ) {
				actors = value;
			}
		},
		"http://localhost",
		() => {},
		createPresentationRandom( 1 )
	);
	const entity = { gid: 1, refObjId: 1, regionId: 257, x: 0, y: 0, z: 0, heading: 0 },
		gameplay = { localGid: 1, inventory: [], casts: [], vitals: [] };
	const step = t => p.step( [ entity ], gameplay, t, t * 1000 );
	for ( let i = 0; i < 30; i++ ) step( i / 30 );
	gameplay.casts = [ { token: 1, caster: 1, target: 1, skill: 1, receivedAtMs: 2000, damage: 0, fatal: false } ];
	step( 2 );
	assert.equal( actors[0].layers, undefined );
	step( 2.1 );
	assert.equal( actors[0].layers[0].clip, "ready01" );
	assert.ok( Math.abs( actors[0].layers[0].weight - .5 ) < 1e-9 );
	step( 3 );
	assert.equal( actors[0].layers[0].clip, "ready01" );
	step( 3.3 );
	assert.equal( actors[0].layers[0].clip, "wait01" );
	step( 5 );
	assert.equal( actors[0].layers[0].loop, true );
	gameplay.casts = [ { ...gameplay.casts[0], shotAtMs: 5000 } ];
	step( 5.25 );
	assert.equal( actors[0].layers[0].clip, "attack1" );
	assert.equal( actors[0].layers[0].time, .05 );
	gameplay.casts = [];
	step( 5.3 );
	assert.equal( actors[0].layers, undefined );
	assert.equal( p.error(), null );
	p.dispose();
});
test("live presenter uses admitted BAN callbacks for multi-hit effects and resets both clocks together", () => {
	const encode = value => new TextEncoder().encode( JSON.stringify( value ) ).buffer;
	const pending = new Map();
	let id = 0, actors = [];
	const animationStates = {
		stand: { durationMs: 1000 },
		attack1: { durationMs: 1000, trackEvents: definition.trackEvents },
		hit1: { durationMs: 500 }
	};
	const row = refObjId => ({
		refObjId,
		codename: "NPC_" + refObjId,
		glb: "/assets/" + refObjId + ".glb",
		clips: [ "stand", "attack1", "hit1" ],
		animationStates
	});
	const stages = [ 1, 2 ].map( startEvent => ({
		resource: "hit.efp",
		phase: "SHOT",
		startEvent,
		damageEvent: true,
		action: "AT_DMG_POS",
		move: "MOV_NONE",
		bone: null,
		offset: [ 0, 0, 0 ],
		life: 0.25,
		sound: null,
		count: 1,
		scripts: []
	}) );
	const assets = {
		available: () => 4,
		request( url, limit, decode ) {
			pending.set( ++id, { url, decode } );
			return id;
		},
		cancel( id ) {
			pending.delete( id );
		},
		take( id ) {
			const job = pending.get( id );
			if ( !job ) return null;
			pending.delete( id );
			if ( job.decode === "effects" ) {
				return { kind: "effects", catalog: { "1": { clips: [ "attack1" ], stages } } };
			}
			if ( job.decode === "character" || job.decode === "effect" ) {
				return {
					kind: "character",
					model: {
						nodes: [],
						primitives: [],
						images: [],
						clips: [
							{ name: "stand", duration: 1, channels: [] },
							{ name: "attack1", duration: 1, channels: [] },
							{ name: "hit1", duration: 0.5, channels: [] },
							{ name: "effect", duration: 0.25, channels: [] }
						]
					},
					images: []
				};
			}
			let value = {};
			if ( job.url.endsWith( "/roster.json" ) ) value = { models: [ row( 1 ), row( 2 ) ] };
			if ( job.url.endsWith( "/skillfx/manifest.json" ) ) {
				value = { format: "sro-skill-stage-models", models: {} };
			}
			if ( job.url.endsWith( "/itemdrop/manifest.json" ) ) {
				value = {
					format: "sro-mission-itemdrop-models",
					models: {}
				};
			}
			return { kind: "bytes", buffer: encode( value ) };
		}
	};
	const presenter = createCharacterPresentation(
		assets,
		{
			setCharacterModel() {},
			setCharacterAssembly() {},
			retainCharacterModels() {},
			setCharacterActors( value ) {
				actors = value;
			}
		},
		"http://localhost",
		() => {},
		createPresentationRandom( 1 )
	);
	const entities = [ 1, 2 ].map( gid => ({
		gid,
		refObjId: gid,
		regionId: 257,
		x: gid * 10,
		y: 0,
		z: 0,
		heading: 0
	}) );
	const gameplay = { localGid: 1, inventory: [], casts: [], vitals: [] };
	const step = time => presenter.step( entities, gameplay, time, time * 1000 );
	for ( let i = 0; i < 20; i++ ) step( i / 20 );
	gameplay.casts = [ {
		token: 1,
		caster: 1,
		target: 2,
		skill: 1,
		damage: 30,
		fatal: false,
		receivedAtMs: 2000,
		impacts: [ { damage: 10, fatal: false }, { damage: 20, fatal: false } ]
	} ];
	step( 2 );
	assert.equal( actors.filter( a => a.gid < 0 ).length, 0 );
	assert.equal( actors.find( a => a.gid === 1 ).layers, undefined );
	step( 2.1 );
	assert.equal( actors.find( a => a.gid === 1 ).layers[0].clip, "attack1" );
	step( 2.2 );
	assert.equal( actors.filter( a => a.gid < 0 ).length, 0 );
	step( 2.402 );
	step( 2.41 );
	assert.equal( actors.filter( a => a.gid < 0 ).length, 1 );
	const first = actors.find( a => a.gid < 0 ).gid;
	step( 2.802 );
	assert.equal( actors.filter( a => a.gid < 0 ).length, 1 );
	assert.notEqual( actors.find( a => a.gid < 0 ).gid, first );
	assert.ok( actors.find( a => a.gid === 2 ).layers[0].time < 0.01, "second hit restarts the reaction" );
	gameplay.casts = [];
	step( 2.85 );
	assert.equal(
		actors.filter( a => a.gid < 0 ).length,
		1,
		"a released one-shot finishes independently of cast finalization"
	);
	step( 3.06 );
	assert.equal(
		actors.filter( a => a.gid < 0 ).length,
		0,
		"the admitted one-shot expires at its own visual lifetime"
	);
	step( 3.2 );
	presenter.step( entities, gameplay, 3.2, 3200, -1 );
	presenter.step( entities, gameplay, 3.45, 3450, -1 );
	assert.equal( actors.find( a => a.gid === 1 ).opacity, 127 / 255 );
	assert.equal( actors.find( a => a.gid === 2 ).opacity, undefined );
	presenter.step( entities, gameplay, 3.45, 3450, -0.8999999761581421 );
	presenter.step( entities, gameplay, 3.7, 3700, -0.8999999761581421 );
	assert.equal( actors.find( a => a.gid === 1 ).opacity, 191 / 255, "boundary restores from current alpha" );
	presenter.reset();
	assert.deepEqual( actors, [] );
	presenter.dispose();
});
