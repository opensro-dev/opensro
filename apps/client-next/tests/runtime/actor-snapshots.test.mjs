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
const { createCharacters } = await import( "../../src/engine/runtime/renderer/characters/characters.ts" );
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

/*
================
spineRig

A root, a Spine_Base one unit up and a tip one unit above it: each joint's
palette matrix is its model-space matrix (identity inverse binds).
================
*/
/** @returns {import("../../src/engine/contracts/character.ts").CharacterModel} */
function spineRig() {
	const identity = Float32Array.of( 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 );
	const node = ( name, parent, y ) => ({
		name,
		parent,
		translation: [ 0, y, 0 ],
		rotation: [ 0, 0, 0, 1 ],
		scale: [ 1, 1, 1 ]
	});
	return {
		nodes: [ node( "root", -1, 0 ), node( "Spine_Base", 0, 1 ), node( "tip", 1, 1 ) ],
		images: [],
		clips: [ {
			name: "stand",
			duration: 1,
			channels: [ {
				node: 0,
				path: "translation",
				interpolation: "LINEAR",
				times: Float32Array.of( 0, 1 ),
				values: Float32Array.of( 0, 0, 0, 0, 0, 0 )
			} ]
		} ],
		primitives: [ {
			name: "body",
			node: 0,
			joints: [ 0, 1, 2 ],
			inverseBind: new Float32Array( 48 ).map( ( _, i ) => identity[i % 16] ),
			image: -1,
			geometry: {
				positions: Float32Array.of( -.25, -.25, 0, .25, -.25, 0, 0, .25, 0 ),
				joints: new Uint32Array( 12 ),
				weights: Float32Array.of( 1, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0 ),
				indices: Uint32Array.of( 0, 1, 2 ),
				transform: identity
			}
		} ]
	};
}

test("a bone rotation published after a row's first admission follows the source until it is dropped", () => {
	const owner = createActorSnapshots(), base = actor( 5, 0 );
	const quarter = { bone: "Spine_Base", rotation: [ 0, 0, Math.SQRT1_2, Math.SQRT1_2 ] };
	const row = owner.update( [ base ] )[0];
	assert.equal( row.boneRotation, undefined );
	owner.update( [ { ...base, boneRotation: quarter } ] );
	assert.deepEqual( json( row.boneRotation ), quarter, "an aim armed after first admission reaches the row" );
	const owned = row.boneRotation;
	quarter.rotation[0] = 99;
	assert.equal( owned.rotation[0], 0, "the row owns its rotation" );
	const turned = { bone: "Spine_Base", rotation: [ 0, 0, .5, Math.sqrt( .75 ) ] };
	owner.update( [ { ...base, boneRotation: turned } ] );
	assert.deepEqual( json( row.boneRotation ), turned, "a turning aim updates the row" );
	assert.equal( row.boneRotation, owned, "and reuses its storage" );
	owner.update( [ base ] );
	assert.equal( row.boneRotation, undefined, "a released aim leaves the row" );
	const fresh = owner.update( [ { ...actor( 6, 0 ), boneRotation: turned } ] )[0];
	turned.rotation[3] = 99;
	assert.notEqual( fresh.boneRotation.rotation[3], 99, "a first admission does not alias the producer's rotation" );
});

test("the drawn palette follows a spine aim armed, turned and released after the actor's first frame", () => {
	const c = createCharacters();
	c.model( "body", spineRig(), [] );
	/** @type {Float32Array | null} */
	let bones = null;
	/** @type {any} */
	const gpu = {
		upload( data ) {
			bones = data.bones.slice();
			return {};
		},
		updateInstances: draw => draw,
		updateBones( _draw, value ) {
			bones = value.slice();
			return 0;
		},
		release() {}
	};
	const base = {
		gid: 1,
		model: "body",
		pose: { regionId: 257, x: 0, y: 0, z: .5, yaw: 0 },
		scale: 1,
		clip: "stand",
		time: 0,
		loop: true
	};
	const view = Float32Array.of( 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 );
	// The tip joint's model-space position in the palette the frame drew.
	const tip = ( actor, time ) => {
		bones = null;
		c.actors( [ actor ] );
		c.prepare( gpu, gpu, 257, view, false, time );
		assert.ok( bones, "the frame drew the actor's palette" );
		return [ bones[32 + 12], bones[32 + 13] ].map( v => Math.round( v * 1e4 ) / 1e4 );
	};
	assert.deepEqual( tip( base, 0 ), [ 0, 2 ], "an unaimed spine stands straight" );
	const quarter = { bone: "Spine_Base", rotation: [ 0, 0, Math.SQRT1_2, Math.SQRT1_2 ] };
	assert.deepEqual(
		tip( { ...base, time: .1, boneRotation: quarter }, .1 ),
		[ -1, 1 ],
		"an aim armed later is drawn"
	);
	const sixth = { bone: "Spine_Base", rotation: [ 0, 0, .5, Math.sqrt( .75 ) ] };
	assert.deepEqual(
		tip( { ...base, time: .2, boneRotation: sixth }, .2 ),
		[ -.866, 1.5 ],
		"a turned aim is drawn"
	);
	assert.deepEqual( tip( { ...base, time: .3 }, .3 ), [ 0, 2 ], "a released aim straightens the spine" );
	c.dispose( gpu, null );
});

test("rows past the actor cap are never admitted, and a row the cap pushes out is retired", () => {
	const owner = createActorSnapshots(), cap = 640;
	const gids = ( first, end ) => Array.from( { length: end - first }, ( _, i ) => actor( first + i, i ) );
	const keys = () => [ ...owner.index.keys() ].sort( ( a, b ) => a - b );
	const range = ( first, end ) => Array.from( { length: end - first }, ( _, i ) => first + i );
	assert.equal( owner.update( gids( 0, cap + 2 ) ).length, cap );
	assert.deepEqual( keys(), range( 0, cap ) );
	// Five leave the front: the next five, two of them over the cap before, come in.
	const shifted = gids( 5, cap + 6 );
	assert.equal( owner.update( shifted ).length, cap );
	assert.deepEqual( keys(), range( 5, cap + 5 ) );
	// The same publication again changes nothing.
	owner.update( shifted );
	assert.deepEqual( keys(), range( 5, cap + 5 ) );
	// An admitted row the publication moves past the cap is retired.
	owner.update( [ ...gids( 6, cap + 6 ), actor( 5, 0 ) ] );
	assert.deepEqual( keys(), range( 6, cap + 6 ) );
	// Within the cap, the leavers alone are retired.
	owner.update( gids( 100, 110 ) );
	assert.deepEqual( keys(), range( 100, 110 ) );
});

test("a frozen actor republished with a clock entry takes the entry's clocks, as the full copy does", () => {
	const reused = createActorSnapshots(), full = createActorSnapshots( false );
	const shape = Object.freeze( {
		...actor( 4, 1 ),
		layers: [ { clip: "walk", time: 1, loop: true, weight: 1, lane: "timed" } ]
	} );
	for ( const owner of [ reused, full ] ) owner.update( [ shape ] );
	const entries = new Map( [ [ shape, {
		time: 2.5,
		layers: [ { clip: "walk", time: 3, loop: true, weight: .5, lane: "timed" } ]
	} ] ] );
	const rows = reused.update( [ shape ], entries ), copied = full.update( [ shape ], entries );
	assert.equal( rows[0].time, 2.5 );
	assert.equal( rows[0].layers[0].time, 3 );
	assert.equal( rows[0].layers[0].weight, .5 );
	assert.deepEqual( json( rows ), json( copied ), "the full copy reads the same entry" );
	assert.equal( reused.stats().clockCopies, 1, "the republished actor copied only its clocks" );
	// An actor with no entry carries its own clocks.
	assert.equal( reused.update( [ shape ] )[0].time, shape.time );
});

test("an invalid clock in an entry holds and reports as a published one does", () => {
	const owner = createActorSnapshots(), shape = Object.freeze( actor( 7, 20 ) );
	assert.equal( owner.update( [ shape ] )[0].time, 2 );
	assert.equal( owner.update( [ shape ], new Map( [ [ shape, { time: NaN } ] ] ) )[0].time, 2 );
	assert.match( owner.takeDegradations()[0].message, /gid=7 model=m2 clip=run time=NaN held=2/ );
});

test("an entry for an actor that is not the row's last source does not skip its comparison", () => {
	const owner = createActorSnapshots(), shape = Object.freeze( actor( 5, 2 ) );
	owner.update( [ shape ] );
	const moved = Object.freeze( { ...shape, pose: { ...shape.pose, x: 40 } } );
	const row = owner.update( [ moved ], new Map( [ [ moved, { time: 9 } ] ] ) )[0];
	assert.equal( row.pose.x, 40, "a new actor object is compared and copied" );
	assert.equal( row.time, 9 );
});

/*
================
everyField

Two frozen actors that differ in every field. Typed Required, so the test
types gate fails when CharacterActor gains a field this list lacks; the
clock fields are the entry's and are left out.
================
*/
/** @returns {[ Required<Omit<import("../../src/engine/contracts/character").CharacterActor, "layers" | "modelAnimation">>, any ] } */
function everyField() {
	/** @type {Required<Omit<import("../../src/engine/contracts/character").CharacterActor, "layers" | "modelAnimation">>} */
	const a = {
		shadowSize: 1,
		shadowAttachment: false,
		modifierId: 1,
		animationLod: { fraction: 1, crowded: false, optional: false },
		holderVisible: true,
		deferredParticle: { offset: 0, nightOnly: false, lodHidden: false },
		blindable: false,
		blindableCompanion: false,
		groundItem: false,
		pointLight: {
			pose: { regionId: 25000, x: 0, y: 0, z: 0 },
			ambient: [ 0, 0, 0 ],
			diffuse: [ 1, 1, 1 ],
			attenuation: 1,
			attenuationConstant: 0,
			range: 10
		},
		bloodEffects: [ "a", null ],
		previewClip: "stand",
		effectBasis: [ 1, 0, 0, 0, 1, 0, 0, 0, 1 ],
		drawGeometry: true,
		materialTint: [ 1, 1, 1 ],
		heightFactor: 1,
		effectBaseScale: 1,
		absoluteEffectScale: false,
		emissionEnd: 5,
		effectRotation: { axis: "x", angle: 0 },
		effectAnchor: { bone: "b", offset: [ 0, 0, 0 ] },
		height: 2,
		pickable: true,
		pickWhenFiltered: false,
		pickOwner: 7,
		dungeonBlock: 1,
		bodyVolume: { index: 0, female: false },
		opacity: 1,
		effectEntity: false,
		boneRotation: { bone: "spine", rotation: [ 0, 0, 0, 1 ] },
		attachment: { gid: 42, bone: "hand", offset: [ 0, 0, 0 ] },
		mountedOn: 9,
		gid: 3,
		model: "m0",
		pose: /** @type {any} */ ({ regionId: 25000, x: 0, y: 0, z: 0, yaw: 0 }),
		clip: "run",
		time: 1,
		loop: true,
		scale: 1
	};
	const b = {
		shadowSize: 2,
		shadowAttachment: true,
		modifierId: 2,
		animationLod: { fraction: .5, crowded: true, optional: true },
		holderVisible: false,
		deferredParticle: { offset: 1, nightOnly: true, lodHidden: true },
		blindable: true,
		blindableCompanion: true,
		groundItem: true,
		pointLight: { ...a.pointLight, range: 20 },
		bloodEffects: [ "b", "c" ],
		previewClip: "run",
		effectBasis: [ 0, 1, 0, 1, 0, 0, 0, 0, 1 ],
		drawGeometry: false,
		materialTint: [ 1, .5, .5 ],
		heightFactor: 2,
		effectBaseScale: 2,
		absoluteEffectScale: true,
		emissionEnd: 6,
		effectRotation: { axis: "y", angle: 1 },
		effectAnchor: { bone: "c", offset: [ 1, 0, 0 ] },
		height: 3,
		pickable: false,
		pickWhenFiltered: true,
		pickOwner: 8,
		dungeonBlock: 2,
		bodyVolume: { index: 1, female: true },
		opacity: .5,
		effectEntity: true,
		boneRotation: { bone: "spine", rotation: [ 0, .1, 0, .99 ] },
		attachment: { gid: 42, bone: "foot", offset: [ 1, 0, 0 ] },
		mountedOn: 10,
		model: "m1",
		pose: { regionId: 25000, x: 1, y: 0, z: 0, yaw: 0 },
		clip: "walk",
		loop: false,
		scale: 2
	};
	return [ a, b ];
}

test("a frozen republication that changes any one non-clock field is copied in full", () => {
	const [a, b] = everyField();
	for ( const key of Object.keys( b ) ) {
		const owner = createActorSnapshots(), first = Object.freeze( { ...a } );
		owner.update( [ first ] );
		const changed = Object.freeze( { ...a, [key]: b[key], time: 2 } );
		const row = owner.update( [ changed ], new Map( [ [ first, { time: 2 } ] ] ) )[0];
		assert.deepEqual( json( row[key] ), json( b[key] ), key );
	}
});

test("a spine aim held by value keeps the clock path; one turned or released reaches the row", () => {
	const owner = createActorSnapshots(), base = Object.freeze( actor( 6, 2 ) );
	const aim = rotation => Object.freeze( { ...base, boneRotation: { bone: "spine", rotation } } );
	owner.update( [ aim( [ 0, 0, 0, 1 ] ) ] );
	owner.update( [ aim( [ 0, 0, 0, 1 ] ) ] );
	assert.equal( owner.stats().clockCopies, 1, "an equal aim in a new object is clocks only" );
	assert.deepEqual( owner.update( [ aim( [ 0, .1, 0, .99 ] ) ] )[0].boneRotation.rotation, [ 0, .1, 0, .99 ] );
	assert.equal( owner.update( [ base ] )[0].boneRotation, undefined, "a released aim clears the row" );
});
