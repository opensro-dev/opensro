/*
===========================================================================

arrow-trail.test.mjs - tests for effects.ts, random.ts, effects.ts,
moving-stage.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { defined } from "../helpers/defined.mjs";
const { createCharacterEffects } = await import( "../../src/engine/runtime/characters/effects/effects.ts" );
const { createPresentationRandom } = await import( "../../src/engine/runtime/random/random.ts" );
const { createEffectDecoder } = await import( "../../src/engine/runtime/assets/worker/effects/effects.ts" );
const { projectileBasis } = await import( "../../src/engine/foundation/animation/moving-stage.ts" );
const reference = JSON.parse( readFileSync( "tests/fixtures/native/arrow-reference.json", "utf8" ) );
const manifest = JSON.parse( readFileSync( "../../.generated/client-public/assets/skillfx/manifest.json", "utf8" ) );
const raw = JSON.parse( readFileSync( "../../.generated/client-public/assets/skill/effectRecords.json", "utf8" ) );
const catalog = createEffectDecoder().decode( new TextEncoder().encode( JSON.stringify( raw ) ) );
const stage = {
	resource: "arrow.bsr",
	phase: "SHOT",
	startEvent: 1,
	damageEvent: true,
	action: "AT_MOV_1TAR",
	move: "MOV_STRAIGHT",
	movement: { delayMs: 0, startSpeed: 10, endSpeed: 10 },
	bone: null,
	targetBone: null,
	offset: [ 0, 0, 0 ],
	targetOffset: [ 0, 0, 0 ],
	life: 0,
	count: 1,
	scripts: [ "SCT_ARROW" ]
};
function fixture( stages = [ stage ], record = {}, detail = 2 ) {
	let serial = 0, ready = true;
	const jobs = new Map(),
		requested = new Set(),
		data = { 1: { clips: [], arrowEffects: [ "trail.efp", "force.efp" ], stages, ...record } };
	const models = {
		...manifest.models,
		"arrow.bsr": { glb: "/assets/arrow.glb", clips: [ "stand" ], clipLoop: true }
	};
	const owner = createCharacterEffects(
		{
			available: () => 4,
			request( url, limit, type ) {
				jobs.set(
					++serial,
					type === "effects" ?
						{ kind: "effects", catalog: data } :
						{
							kind: "bytes",
							buffer: new TextEncoder().encode( JSON.stringify( { ...manifest, models } ) ).buffer
						}
				);
				return serial;
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
	const entities = [ { gid: 1, kind: "local-player", regionId: 257, x: 0, y: 0, z: 0, heading: 0 }, {
		gid: 2,
		kind: "monster",
		regionId: 257,
		x: 10,
		y: 0,
		z: 0,
		heading: 0
	} ];
	const cast = { token: 1, caster: 1, target: 2, skill: 1, results: [ { target: 2, impacts: [ { damage: 10 } ] } ] },
		game = { localGid: 1, inventory: [ { slot: 6, refObjId: 143 } ], casts: [ cast ] };
	const step = ( now, events = [] ) =>
		owner.step(
			entities,
			game,
			now,
			path => {
				requested.add( path );
				return ready;
			},
			() => 10,
			events,
			( gid ) => ({ regionId: 257, x: gid === 1 ? 0 : 10, y: 0, z: 0, yaw: 0 }),
			[],
			detail
		);
	const trigger = ( event = 1, at = .2 ) => ({ cast, phase: "SHOT", event, at });
	step( 0 );
	step( .1 );
	const initial = step( .2, [ trigger() ] );
	return {
		owner,
		entities,
		cast,
		game,
		step,
		trigger,
		initial,
		requested,
		setReady( v ) {
			ready = v;
		}
	};
}
test("arrow catalog exposes the two retail resources and never uses the trail as a projectile fallback", () => {
	let arrows = 0, weapons = 0;
	for ( const [id, r] of Object.entries( catalog ) ) {
		assert.deepEqual( r.arrowEffects, [ raw[id].arrowTrailEffectPath, raw[id].arrowForceEffectPath ] );
		for ( const s of r.stages ) {
			if ( s.scripts[0] === "SCT_ARROW" ) {
				assert.equal( defined( s.script ).kind, "arrow" );
				arrows++;
			}
			if ( s.resource === "weapon" ) weapons++;
		}
	}
	assert.equal( arrows, 666 );
	assert.equal( weapons, 26 );
	const decoded = createEffectDecoder().decode(
		new TextEncoder().encode(
			JSON.stringify( {
				1: {
					arrowTrailEffectPath: "trail.efp",
					authoredStages: [ {
						actionType: "AT_MOV_1TAR",
						startEvent: 1,
						startOffset: [ 0, 0, 0 ],
						startKeepRotation: true,
						startAddHeight: false,
						targetKeepRotation: true,
						targetAddHeight: false
					} ]
				}
			} )
		)
	);
	assert.equal( decoded[1].stages[0].resource, null );
});
test("all 666 published arrow stages admit, including held arrows, splash and equipped weapon copies", () => {
	let count = 0;
	for ( const record of Object.values( catalog ) ) {
		for ( const s of record.stages ) {
			if ( defined( s.script ).kind === "arrow" ) {
				const f = fixture( [ { ...s, phase: "SHOT", startEvent: 1 } ], {
					...record,
					stages: [ { ...s, phase: "SHOT", startEvent: 1 } ]
				} );
				assert.equal( f.owner.error(), null, JSON.stringify( s ) );
				f.owner.dispose();
				count++;
			}
		}
	}
	assert.equal( count, 666 );
});
test("auxiliary creation order, detail gate and held-arrow fade match original helper execution", () => {
	assert.equal( reference.binarySha256, "375e868234437e815af8ce9289ddea7ec9144430f4ea24e32988a6d6c9dd108a" );
	for ( const sample of reference.cases ) {
		const row = {
			...stage,
			action: sample.slow ? "AT_LOOP" : "AT_MOV_1TAR",
			move: sample.slow ? "MOV_NONE" : "MOV_STRAIGHT",
			movement: { delayMs: 0, startSpeed: 1, endSpeed: 1 },
			damageEvent: false
		};
		const f = fixture( [ row ], {
			arrowEffects: [ sample.mask & 1 ? "trail.efp" : null, sample.mask & 2 ? "force.efp" : null ]
		}, sample.detail );
		const created = sample.initial.filter( c => c[0] === "create" ).map( c =>
			c[1] === 11 ? "trail.efp" : "force.efp"
		);
		assert.deepEqual( f.initial.slice( 1 ).map( a => decodeURIComponent( a.model.split( "#" )[1] ) ), created );
		let now = .2;
		for ( const tick of sample.ticks ) {
			now += tick.delta;
			const rows = f.step( now );
			for ( const call of tick.calls ) {
				if ( call[0] === "alpha" ) {
					const path = created[call[1]], actor = rows.find( a => a.model.endsWith( path ) );
					if ( tick.calls.some( c => c[0] === "release" && c[1] === call[1] ) ) {
						assert.equal( actor, undefined );
					} else assert.equal( defined( actor ).opacity, call[2] / 255 );
				}
			}
		}
		f.owner.dispose();
	}
});
test("launch transfers existing child identities and clocks, then adds the moving-stage arrow effects", () => {
	const f = fixture( [ { ...stage, action: "AT_LOOP", move: "MOV_NONE", damageEvent: false, native: { slot: 7 } }, {
		...stage,
		startEvent: 2,
		native: { trade: 7 }
	} ] );
	const held = f.initial, launched = f.step( .4, [ f.trigger( 2, .4 ) ] );
	assert.equal( launched[0].gid, held[0].gid );
	assert.equal( launched.length, 5 );
	assert.deepEqual( launched.slice( 1, 3 ).map( a => a.gid ), held.slice( 1 ).map( a => a.gid ) );
	assert.ok( launched[1].time > 0 );
	assert.equal( launched[3].time, 0 );
	assert.equal( f.owner.error(), null );
	f.owner.dispose();
});
test("arrival hides projectile geometry but drains trail particles at its retained socket; reset removes all", () => {
	const f = fixture();
	assert.equal( f.initial.length, 3 );
	const alive = f.step( .7 );
	assert.equal( defined( alive[1].attachment ).gid, alive[0].gid );
	// The trail and force programs ride the arrow's Bone01 (socket desc
	// 0xCCC8B8) in 8D6880 native space, like every .efp on a named bone:
	// without it their Z axis reverses and they point away from the arrow.
	assert.deepEqual(
		alive.slice( 1 ).map( a => [ defined( a.attachment ).bone, defined( a.attachment ).basis ] ),
		[ [ "Bone01", "native" ], [ "Bone01", "native" ] ]
	);
	const landed = f.step( 1.3 );
	assert.equal( landed.length, 3 );
	assert.equal( landed[0].drawGeometry, false );
	assert.ok( landed.slice( 1 ).every( a => a.emissionEnd !== undefined && !a.loop ) );
	assert.equal( landed[0].pose.x, 10 );
	assert.equal( f.owner.takeImpacts().filter( e => e.kind === "arrival" ).length, 1 );
	assert.deepEqual( f.step( 12 ), [] );
	f.owner.reset();
	assert.deepEqual( f.step( 13 ), [] );
	f.owner.dispose();
});
test("kill and cancel dispose held model and auxiliaries; launched arrows keep independent ownership", () => {
	for ( const cancel of [ true, false ] ) {
		const f = fixture( [
			{ ...stage, action: "AT_LOOP", move: "MOV_NONE", damageEvent: false, native: { slot: 7 } },
			{ ...stage, startEvent: 2, resource: null, native: { kill: 7 } }
		] );
		if ( cancel ) f.cast.cancelledAtMs = 300;
		assert.deepEqual( f.step( .3, cancel ? [] : [ f.trigger( 2, .3 ) ] ), [] );
		f.owner.dispose();
	}
	const f = fixture();
	f.cast.cancelledAtMs = 300;
	f.entities.shift();
	f.game.casts = [];
	assert.equal( f.step( .3 ).length, 3 );
	f.owner.dispose();
});
test("weapon copies use current local/peer inventory, admit bind-pose models, and restore source on cancel", () => {
	const f = fixture( [ { ...stage, resource: "weapon" } ] );
	assert.equal( f.initial[0].model, manifest.models[manifest.weapons["143"]].glb );
	assert.equal( f.initial[0].clip, "" );
	assert.equal( f.owner.appearance( 1 ).weaponHidden, true );
	f.cast.cancelledAtMs = 300;
	assert.equal( f.step( .3 ).length, 3 );
	assert.equal( f.owner.appearance( 1 ).weaponHidden, false );
	f.owner.reset();
	f.cast.cancelledAtMs = undefined;
	f.game.localGid = 99;
	f.entities[0].equipment = [ { slot: 6, refObjId: 144 } ];
	f.step( .4 );
	f.step( .5 );
	const rows = f.step( .6, [ f.trigger( 1, .6 ) ] );
	assert.equal( rows[0].model, manifest.models[manifest.weapons["144"]].glb );
	f.owner.dispose();
});
test("empty weapon slots create no substituted geometry; cold auxiliary resources are requested independently", () => {
	const f = fixture( [ { ...stage, resource: "weapon" } ] );
	f.owner.reset();
	f.game.inventory = [];
	f.step( .4 );
	f.step( .5 );
	assert.deepEqual( f.step( .6, [ f.trigger( 1, .6 ) ] ), [] );
	assert.equal( f.owner.appearance( 1 ).weaponHidden, false );
	f.owner.dispose();
	const cold = fixture();
	cold.setReady( false );
	cold.step( .3 );
	assert.ok( cold.requested.has( "/assets/effects/programs.json#trail.efp" ) );
	assert.ok( cold.requested.has( "/assets/effects/programs.json#force.efp" ) );
	cold.setReady( true );
	assert.equal( cold.step( .4 ).length, 3 );
	cold.owner.dispose();
});
test("native model basis preserves pitch, backward direction and near-vertical fallback", () => {
	for ( const sample of reference.basis ) {
		const [x, y, z] = sample.direction,
			actual = projectileBasis( { regionId: 257, x, y, z, yaw: 0 }, { regionId: 257, x: 0, y: 0, z: 0, yaw: 0 } );
		assert.equal( defined( actual ).length, 9 );
		for ( let i = 0; i < 9; i++ ) {
			assert.ok(
				Math.abs( defined( actual )[i] - sample.basis[i] ) < 2e-7,
				JSON.stringify( { sample, actual } )
			);
		}
	}
	assert.equal(
		projectileBasis( { regionId: 257, x: 0, y: 0, z: 0 }, { regionId: 257, x: 0, y: 0, z: 0 } ),
		undefined
	);
});

test("late launch callbacks preserve flight without re-hiding equipment after cast cancellation", () => {
	const f = fixture( [ { ...stage, startEvent: 2, resource: "weapon" } ] );
	assert.equal( f.initial.length, 0 );
	const historical = { ...f.cast };
	f.cast.cancelledAtMs = 300;
	const rows = f.step( .4, [ { cast: historical, phase: "SHOT", event: 2, at: .2 } ] );
	assert.equal( rows.length, 3 );
	assert.equal( f.owner.appearance( 1 ).weaponHidden, false );
	f.owner.dispose();
});

test("arrival EFP owns a new actor and does not inherit the flying BSR basis or hidden geometry", () => {
	const f = fixture( [ { ...stage, arrivalResource: "arrival.efp" } ] );
	const rows = f.step( 1.3 ), arrival = rows.find( a => a.model.endsWith( "arrival.efp" ) );
	assert.ok( arrival );
	assert.notEqual( arrival.gid, f.initial[0].gid );
	assert.equal( arrival.effectBasis, undefined );
	assert.equal( arrival.drawGeometry, undefined );
	assert.equal( arrival.scale, 1 );
	assert.equal( defined( rows.find( a => a.gid === f.initial[0].gid ) ).drawGeometry, false );
	f.owner.dispose();
});
test("speed-0 straight movers admit: a zero-distance one arrives, the rest stand at their start", () => {
	// Navigation_StepTowards (879650) arrives once distance squared <= step
	// squared, so speed 0 is a legal native mover, not an unsupported stage.
	let count = 0;
	for ( const id of [ "198", "216", "287", "3494" ] ) {
		const record = defined( catalog[id] );
		const stages = record.stages.filter( s =>
			s.move === "MOV_STRAIGHT" && s.movement?.startSpeed === 0 && s.movement.endSpeed === 0
		).map( s => ({ ...s, phase: "SHOT", startEvent: 1 }) );
		assert.ok( stages.length, id );
		const f = fixture( stages, { ...record, stages } );
		for ( let t = .3; t < 1; t += .1 ) f.step( t );
		assert.equal( f.owner.error(), null, id );
		f.owner.dispose();
		count += stages.length;
	}
	assert.equal( count, 7 );
});
