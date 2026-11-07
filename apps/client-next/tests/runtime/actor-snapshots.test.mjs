/*
===========================================================================

actor-snapshots.test.mjs - tests for actor-snapshots.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import fc from "fast-check";
import { defined } from "../helpers/defined.mjs";

const { createActorSnapshots } = await import(
	sourceFileUrl( "src/engine/runtime/renderer/characters/actor-snapshots.ts" ).href
);
const { animationActivation } = await import( "../../src/engine/foundation/animation/animation-activation.ts" );
const json = value => JSON.parse( JSON.stringify( value ) );
const actor = ( gid, n ) => ({
	gid,
	model: "m" + n % 3,
	clip: "run",
	time: n / 10,
	loop: !!(n % 2),
	scale: 1,
	pose: { regionId: 25000, x: n, y: 0, z: 0, yaw: 0 },
	...(n % 2 ?
		{
			opacity: .5,
			layers: [ { clip: "walk", time: 1, loop: true, weight: 1, lane: "timed" } ],
			attachment: { gid: 42, bone: "hand", offset: [ n, 2, 3 ], ...(n % 3 ? { root: true } : {}) },
			effectRotation: { axis: "x", angle: n },
			effectAnchor: { bone: "b", offset: [ n, 0, 0 ] }
		} :
		{})
});

test("retained snapshots update and clear pick ownership and optional layer rates", () => {
	const owner = createActorSnapshots();
	const first = {
		...actor( 1, 0 ),
		pickOwner: 42,
		layers: [ {
			clip: "walk",
			time: 1,
			loop: true,
			weight: 1,
			lane: "timed",
			rate: 2,
			activation: animationActivation( 0 )
		} ]
	};
	const row = owner.update( [ first ] )[0], layer = row.layers[0];
	const next = {
		...first,
		pickOwner: 43,
		layers: [ { clip: "walk", time: 2, loop: true, weight: 1, lane: "timed" } ]
	};
	assert.equal( owner.update( [ next ] )[0], row );
	assert.equal( row.pickOwner, 43 );
	assert.equal( row.layers[0], layer );
	assert.equal( layer.rate, undefined, "an absent rate cannot inherit the preceding installation's rate" );
	assert.equal( layer.activation, undefined );
	const { pickOwner, ...unowned } = next;
	owner.update( [ unowned ] );
	assert.equal( row.pickOwner, undefined, "removed pick ownership cannot survive admission" );
	next.layers[0].time = 99;
	next.pose.x = 99;
	assert.equal( layer.time, 2 );
	assert.equal( row.pose.x, 0 );
});

test("retained attachment copies update required values and clear every omitted option", () => {
	const owner = createActorSnapshots();
	const source = {
		...actor( 1, 0 ),
		attachment: {
			gid: 10,
			bone: "hand",
			offset: [ 1, 2, 3 ],
			basis: "native",
			modelScale: 2,
			root: true,
			rootIfMissing: true,
			keepRotation: false,
			ground: true,
			facing: .5,
			rotation: new Float32Array( 16 )
		}
	};
	const row = owner.update( [ source ] )[0], attachment = row.attachment;
	const next = { ...source, attachment: { gid: 20, bone: "saddle", offset: [ 4, 5, 6 ] } };
	owner.update( [ next ] );
	assert.equal( row.attachment, attachment );
	assert.deepEqual( json( attachment ), next.attachment );
	next.attachment.offset[0] = 99;
	assert.equal( attachment.offset[0], 4 );
});

test("actor snapshots preserve values, isolation, identity and bounded retirement across publications", () => {
	fc.assert(
		fc.property(
			fc.array( fc.array( fc.integer( { min: 0, max: 50 } ), { maxLength: 20 } ), { maxLength: 50 } ),
			events => {
				const owner = createActorSnapshots();
				let old = new Map();
				for ( const numbers of events ) {
					const sources = numbers.map( ( n, gid ) => actor( gid, n ) ),
						expected = json( sources ),
						rows = owner.update( sources );
					assert.deepEqual( json( rows ), expected );
					assert.equal( owner.index.size, sources.length );
					for ( const row of rows ) {
						if ( old.has( row.gid ) ) assert.equal( row, old.get( row.gid ) );
					}
					for ( const source of sources ) {
						source.pose.x = -999;
						if ( source.attachment ) source.attachment.offset[0] = -999;
						if ( source.layers ) source.layers[0].time = -999;
						if ( source.effectRotation ) source.effectRotation.angle = -999;
						if ( source.effectAnchor ) source.effectAnchor.offset[0] = -999;
					}
					assert.deepEqual( json( rows ), expected );
					old = new Map( owner.index );
					assert.throws(
						() => owner.update( [ actor( 1, 1 ), { ...actor( 2, 2 ), opacity: -1 } ] ),
						/opacity/
					);
					assert.deepEqual( json( rows ), expected );
					assert.throws( () => owner.update( [ actor( 1, 1 ), actor( 1, 2 ) ] ), /identity/ );
					assert.deepEqual( json( rows ), expected );
				}
				owner.reset();
				assert.equal( owner.index.size, 0 );
			}
		),
		{ seed: 2402030, numRuns: 1000 }
	);
});

test("effect metadata reuses owned storage without aliasing producer values or retaining removed features", () => {
	const owner = createActorSnapshots();
	let previous;
	for ( let i = 0; i < 80; i++ ) {
		const source = {
			...actor( 1, i * 2 + 1 ),
			effectBasis: [ 1, 0, 0, 0, 1, 0, 0, 0, i ],
			materialTint: [ i / 80, .5, 1 ],
			bloodEffects: [ "blood-" + i, null ],
			pointLight: {
				pose: { regionId: 1, x: i, y: 2, z: 3 },
				ambient: [ .1, .2, .3 ],
				diffuse: [ .4, .5, .6 ],
				range: 50,
				attenuation: i
			}
		};
		const expected = json( source ), row = owner.update( [ source ] )[0];
		assert.deepEqual( json( row ), expected );
		if ( previous ) {
			for (
				const key of [
					"effectBasis",
					"materialTint",
					"bloodEffects",
					"pointLight",
					"effectAnchor",
					"effectRotation"
				]
			) assert.equal( row[key], previous[key], key + " storage retained" );
		}
		previous = { ...row };
		source.effectBasis[0] = 999;
		source.materialTint[0] = 999;
		source.bloodEffects[0] = "changed";
		source.pointLight.pose.x = 999;
		source.pointLight.ambient[0] = 999;
		source.pointLight.diffuse[0] = 999;
		defined( source.effectAnchor ).offset[0] = 999;
		defined( source.effectRotation ).angle = 999;
		assert.deepEqual(
			json( row ),
			expected,
			"caller mutation is isolated on both first admission and repeated publication"
		);
	}
	const bare = actor( 1, 0 );
	assert.deepEqual( json( owner.update( [ bare ] )[0] ), json( bare ) );
	owner.reset();
	assert.equal( owner.index.size, 0 );
});

test("model reference revision ignores pose/order changes and rejected publications", () => {
	const owner = createActorSnapshots(), a = actor( 1, 0 ), b = actor( 2, 1 ), duplicate = actor( 3, 0 );
	owner.update( [ a, b ] );
	const revision = owner.modelRevision();
	owner.update( [ b, { ...a, time: 50 }, duplicate ] );
	assert.equal( owner.modelRevision(), revision );
	owner.update( [ a, b ] );
	assert.equal( owner.modelRevision(), revision );
	assert.throws( () => owner.update( [ { ...a, model: "new" }, { ...b, opacity: -1 } ] ) );
	assert.equal( owner.modelRevision(), revision );
	owner.update( [ a ] );
	assert.ok( owner.modelRevision() > revision );
	const removed = owner.modelRevision();
	owner.update( [ { ...a, model: "new" } ] );
	assert.ok( owner.modelRevision() > removed );
	owner.reset();
	const reset = owner.modelRevision();
	owner.update( [ a ] );
	assert.ok( owner.modelRevision() > reset );
});

test("point light admission tests own pose values only, as Object.values did", () => {
	const owner = createActorSnapshots();
	const light = pose => ({ pose, ambient: [ .1, .2, .3 ], diffuse: [ .4, .5, .6 ], range: 50, attenuation: 1 });
	// An inherited enumerable value is not one of the pose's own values.
	const inherited = Object.assign( Object.create( { stray: NaN } ), { regionId: 1, x: 1, y: 2, z: 3 } );
	assert.equal( owner.update( [ { ...actor( 1, 0 ), pointLight: light( inherited ) } ] ).length, 1 );
	assert.throws(
		() => owner.update( [ { ...actor( 1, 0 ), pointLight: light( { regionId: 1, x: NaN, y: 2, z: 3 } ) } ] ),
		/Invalid character point light/
	);
});

test("an invalid published clock holds the last valid value and reports once per gid", () => {
	const owner = createActorSnapshots(), base = actor( 7, 20 );
	assert.equal( owner.update( [ base ] )[0].time, 2 );
	for ( const bad of [ NaN, -5, Infinity ] ) {
		assert.equal( owner.update( [ { ...base, time: bad } ] )[0].time, 2, `time ${bad} is held` );
	}
	assert.equal( owner.stats().heldTimes, 3 );
	const reports = owner.takeDegradations();
	assert.equal( reports.length, 1 );
	assert.equal( reports[0].code, "character-time-held" );
	assert.match( reports[0].message, /gid=7 model=m2 clip=run time=NaN held=2/ );
	assert.deepEqual( owner.takeDegradations(), [], "the queue drains" );
	assert.equal( owner.update( [ { ...base, time: 3 } ] )[0].time, 3, "a valid clock takes over again" );
});

test("a first invalid clock holds at rest and layer clocks hold per slot", () => {
	const owner = createActorSnapshots();
	const layer = { clip: "walk", time: -1, loop: true, weight: 2, lane: "timed" };
	const row = owner.update( [ { ...actor( 9, 0 ), time: NaN, layers: [ layer ] } ] )[0];
	assert.equal( row.time, 0 );
	assert.equal( row.layers[0].time, 0 );
	assert.equal( row.layers[0].weight, 1 );
	const moved = { clip: "walk", time: 5, loop: true, weight: .5, lane: "timed" };
	owner.update( [ { ...actor( 9, 0 ), time: 1, layers: [ moved ] } ] );
	assert.equal( row.layers[0].time, 5 );
	assert.equal( row.layers[0].weight, .5 );
	owner.update( [ { ...actor( 9, 0 ), time: 1, layers: [ { ...moved, time: NaN, weight: NaN } ] } ] );
	assert.equal( row.layers[0].time, 5, "the layer clock holds its last valid value" );
	assert.equal( row.layers[0].weight, .5, "the layer weight holds its last valid value" );
	assert.equal( owner.stats().heldTimes, 5, "every substitution counts, reported or not" );
	const messages = owner.takeDegradations().map( report => report.message );
	assert.equal( messages.filter( message => message.includes( "clip=run time=NaN" ) ).length, 1 );
	assert.ok( messages.some( message => message.includes( "layers[0].time=-1" ) ) );
	assert.ok( messages.some( message => message.includes( "layers[0].weight=2" ) ) );
	assert.equal(
		messages.filter( message => message.includes( "layers[0].time" ) ).length,
		1,
		"a layer slot reports its first hold only, not the later NaN"
	);
});

test("valid clocks are admitted verbatim and a held clock reports again after the gid returns", () => {
	const owner = createActorSnapshots();
	const source = {
		...actor( 3, 1 ),
		layers: [ { clip: "walk", time: 1.5, loop: true, weight: .25, lane: "timed" } ]
	};
	const row = owner.update( [ source ] )[0];
	assert.equal( row.time, source.time );
	assert.equal( row.layers[0].time, 1.5 );
	assert.equal( row.layers[0].weight, .25 );
	assert.equal( owner.stats().heldTimes, 0 );
	assert.deepEqual( owner.takeDegradations(), [] );
	const stale = actor( 11, 10 );
	owner.update( [ { ...stale, time: NaN } ] );
	assert.equal( owner.takeDegradations().length, 1 );
	owner.update( [] );
	owner.update( [ { ...stale, time: NaN } ] );
	assert.equal( owner.takeDegradations().length, 1, "a returned gid reports its first hold again" );
});
