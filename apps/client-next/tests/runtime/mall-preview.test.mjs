/*
===========================================================================

mall-preview.test.mjs - try-on isolation, complete assembly and preview lifetime

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import test from "node:test";
const { createMallPreview, MALL_PREVIEW_GID } = await import(
	"../../src/engine/runtime/characters/mall-preview.ts"
);
const { createPortrait } = await import( "../../src/engine/runtime/renderer/characters/portrait.ts" );
const { radians } = await import( "../../src/engine/foundation/math/angles.ts" );
const { createModelEmission } = await import(
	"../../src/engine/foundation/animation/model-emission.ts"
);
const { defined } = await import( "../helpers/defined.mjs" );
const { equipmentSocket } = await import(
	"../../src/engine/foundation/animation/equipment-sockets.ts"
);

/*
================
fixture
================
*/
function fixture() {
	const garment = id => ({ glb: "garment-" + id, parts: [ "cloth" ], covers: { cloth: [ 1 ] } });
	const record = ( id, slot, gender = "CH_M" ) => ({
		slot: null,
		avatarSlot: slot,
		armorClass: 0,
		thiefSuit: false,
		visualMask: 0,
		visualPriority: 70,
		model: null,
		source: null,
		bodies: { [gender]: garment( id ) }
	});
	const dress = {
		defaultWear: { CH_M_clothes_BA: garment( "body" ), CH_M_clothes_LA: garment( "legs" ) },
		equipment: { 1: record( 1, 1 ), 2: record( 2, 1 ), 3: record( 3, 0 ), 4: record( 4, 1, "CH_W" ) },
		avatarVisualOverrides: { 2: { animation: "stand", priority: 70, additionalBsr: "wings" } },
		avatarAuxiliary: { 2: { glb: "wings", parts: [ "wings" ], bone: "Bip01 Spine", clips: [ "stand" ] } }
	};
	const frame = {
		resource: { codename: "CHAR_CH_MAN_ADVENTURER", glb: "body", cover: { 1: 7 } },
		dress,
		equipment: [],
		avatars: [ { refObjId: 1 } ],
		seconds: 10
	};
	const blocked = new Set(), admitted = [], retained = [];
	const resources = {
		/*
		================
		ready
		================
		*/
		ready( path ) {
			admitted.push( path );
			return !blocked.has( path );
		},
		/*
		================
		plan
		================
		*/
		plan( paths ) {
			retained.push( ...paths );
			return true;
		}
	};
	const assemblies = [];
	const renderer = {
		/*
		================
		setCharacterAssembly
		================
		*/
		setCharacterAssembly( name, base, parts ) {
			assemblies.push( { name, base, parts } );
		}
	};
	return { owner: createMallPreview(), frame, resources, renderer, assemblies, blocked, admitted, retained };
}

test("mannequin replaces one avatar family without mutating equipment and retains auxiliary skeletons", () => {
	const f = fixture(), before = structuredClone( f.frame );
	f.owner.request( [] );
	f.owner.step( f.frame, f.resources, f.renderer );
	assert.deepEqual( f.owner.state().wearable, [ 1, 2, 3 ] );
	f.owner.request( [ 2, 3 ] );
	const actors = f.owner.step( f.frame, f.resources, f.renderer );
	const parts = f.assemblies.at( -1 ).parts;
	assert.ok( parts.some( p => p.model === "garment-2" && p.covers.includes( 7 ) ) );
	assert.ok( parts.some( p => p.model === "garment-3" ) );
	assert.ok( !parts.some( p => p.model === "garment-1" ) );
	assert.equal( actors.length, 2 );
	assert.equal( actors[0].gid, MALL_PREVIEW_GID );
	assert.equal( actors[1].model, "wings" );
	assert.equal( actors[1].attachment?.gid, actors[0].gid );
	assert.equal( actors[1].attachment?.basis, "compound" );
	assert.deepEqual( f.frame, before );
	f.owner.request( [] );
	assert.equal( f.owner.step( f.frame, f.resources, f.renderer ).length, 1 );
	assert.ok( f.assemblies.at( -1 ).parts.some( p => p.model === "garment-1" ) );
});

test("cold try-on keeps the last complete body and admits every required source", () => {
	const f = fixture();
	f.owner.request( [] );
	const old = f.owner.step( f.frame, f.resources, f.renderer );
	f.blocked.add( "wings" );
	f.owner.request( [ 2 ] );
	assert.equal( f.owner.step( f.frame, f.resources, f.renderer ), old );
	assert.ok( f.admitted.includes( "garment-2" ) && f.admitted.includes( "wings" ) );
	assert.ok( f.retained.includes( "garment-1" ) );
	f.blocked.clear();
	assert.equal( f.owner.step( f.frame, f.resources, f.renderer ).length, 2 );
	f.owner.request( null );
	assert.deepEqual( f.owner.step( f.frame, f.resources, f.renderer ), [] );
	assert.equal( f.owner.state().gid, undefined );
	f.owner.request( [ 4 ] );
	assert.throws( () => f.owner.step( f.frame, f.resources, f.renderer ), /Invalid mannequin garment/ );
});

test("borrowed portrait keeps socket children, retires old models and never closes shared images", () => {
	const models = new Map();
	let actors = [], projection, bodyBorrows = 0;
	const preview = createPortrait( {
		retain() {},
		/*
		================
		actors
		================
		*/
		actors( rows ) {
			actors = [ ...rows ];
		},
		/*
		================
		prepare
		================
		*/
		prepare( geometry, images, origin, view ) {
			projection = view;
			for ( const key of models.keys() ) if ( !actors.some( row => row.model === key ) ) models.delete( key );
			return [];
		},
		/*
		================
		borrowModel
		================
		*/
		borrowModel( key, model ) {
			assert.ok( !models.has( key ) );
			models.set( key, model );
			if ( key === "body" ) bodyBorrows++;
		},
		/*
		================
		hasModel
		================
		*/
		hasModel( key ) {
			return models.has( key );
		},
		/*
		================
		extendBorrowedAnimations
		================
		*/
		extendBorrowedAnimations( key, model ) {
			assert.ok( models.has( key ) );
			models.set( key, model );
		},
		/*
		================
		socket
		================
		*/
		socket() {
			return { x: 0, y: 10, z: 0 };
		},
		invalidate() {},
		takeDegradations: () => [],
		/*
		================
		dispose
		================
		*/
		dispose() {
			models.clear();
		}
	} );
	const model = { nodes: [], primitives: [], clips: [], images: [] };
	const actor = {
		gid: 1,
		model: "body",
		pose: { regionId: 0, x: 0, y: 0, z: 0, yaw: radians( 0 ) },
		clip: "stand",
		time: 0,
		loop: true,
		scale: 1
	};
	const source = {
		actor,
		model,
		images: [],
		children: [ {
			actor: {
				...actor,
				gid: 2,
				model: "wings",
				attachment: { gid: 1, bone: "spine", offset: /** @type {[number, number, number]} */ ([ 0, 0, 0 ]) }
			},
			model,
			images: []
		} ]
	};
	/** @type {import("../../src/engine/runtime/renderer/internal/gpu-contract.ts").GeometryCommands} */
	const geometry = /** @type {any} */ ({});
	/** @type {import("../../src/engine/runtime/renderer/internal/gpu-contract.ts").ImageCommands} */
	const images = /** @type {any} */ ({});
	preview.prepare( source, geometry, images, { yaw: 0, seconds: 10, aspect: 88 / 168 } );
	assert.ok( projection );
	assert.ok( Math.abs( Math.abs( projection[0] / projection[5] ) - 168 / 88 ) < 1e-6 );
	preview.prepare( source, geometry, images, { yaw: 0, seconds: 11 } );
	assert.equal( actors.length, 2 );
	assert.equal( actors[1].attachment.gid, actors[0].gid );
	assert.equal( actors[1].time, 1 );
	assert.deepEqual( [ ...models.keys() ], [ "body", "wings" ] );
	preview.prepare( { ...source, children: [] }, geometry, images, { yaw: 0, seconds: 12 } );
	assert.deepEqual( [ ...models.keys() ], [ "body" ] );
	// Effects attach and detach every few frames in combat: the body stays
	// borrowed (its render and GPU plans are not rebuilt) and keeps its clock.
	for ( let n = 0; n < 4; n++ ) {
		preview.prepare( source, geometry, images, { yaw: 0, seconds: 13 + n } );
		preview.prepare( { ...source, children: [] }, geometry, images, { yaw: 0, seconds: 13.5 + n } );
	}
	preview.prepare( source, geometry, images, { yaw: 0, seconds: 18 } );
	assert.deepEqual( [ ...models.keys() ], [ "body", "wings" ] );
	assert.equal( bodyBorrows, 1, "a child change re-borrowed the body" );
	assert.equal( actors[0].time, 8, "a child change restarted the body's clock" );
	preview.prepare( null, geometry, images );
	assert.equal( models.size, 0 );
	preview.prepare( source, geometry, images, { yaw: 0, seconds: 20 } );
	assert.equal( actors[1].time, 0 );
	preview.dispose( geometry, images );
	assert.equal( models.size, 0 );
});

/*
================
glowFixture

A mannequin wearing swords as the build publishes them for a Chinese man:
two rare swords naming their effect in specialGlows, one plain. The
emission owner is the real one; allocate hands out descending ids.
================
*/
function glowFixture() {
	const f = fixture();
	const sword = () => ({
		slot: 6,
		armorClass: 0,
		thiefSuit: false,
		visualMask: 1 << 6,
		visualPriority: 0,
		model: "res/item/china/weapon/sword_01.bsr",
		source: null,
		bodies: {
			CH_M: {
				glb: "rare-sword",
				parts: [ "EQ0" ],
				branches: [ {
					part: "EQ0",
					attachBone: "hand",
					nodes: [ {
						name: "ai_end",
						parent: -1,
						translation: [ 0, 0, 0 ],
						rotation: [ 0, 0, 0, 1 ],
						scale: [ 1, 1, 1 ]
					} ]
				} ]
			}
		}
	});
	const glow = effectPath => [ {
		effectPath,
		bone: "ai_end",
		scale: 1.3,
		offset: /** @type {[ number, number, number ]} */ ([ 0, 0, 0 ]),
		root: false
	} ];
	const dress = {
		...f.frame.dress,
		equipment: { ...f.frame.dress.equipment, 9000: sword(), 9001: sword(), 9002: sword() },
		specialGlows: {
			9001: glow( "system/system_raretype_a_step1.efp" ),
			9002: glow( "system/system_raretype_b_step1.efp" )
		}
	};
	const frame = { ...f.frame, dress, avatars: [], equipment: [ { slot: 6, refObjId: 9001, plus: 0 } ] };
	let nextId = -1;
	const emission = createModelEmission( () => nextId-- );
	return { ...f, frame, emission };
}

test("mannequin keeps its rare weapon's glow on the portrait pass with the outfit's particle identity", () => {
	const f = glowFixture();
	f.owner.request( [] );
	f.owner.step( f.frame, f.resources, f.renderer );
	const holder = defined( f.owner.glow(), "the committed mannequin offers its glow holder" );
	assert.equal( holder.actor.gid, MALL_PREVIEW_GID );
	assert.equal( holder.particles.length, 1 );
	const emitted = f.emission.step( [ holder ], 10, () => true, 8 );
	assert.equal( emitted.length, 1 );
	assert.equal( defined( defined( emitted[0] ).attachment ).gid, MALL_PREVIEW_GID );
	assert.equal( defined( defined( emitted[0] ).attachment ).bone, equipmentSocket( 6, "EQ0", "ai_end" ) );
	// An unchanged outfit keeps its particle array, so the emitter and its
	// clock continue instead of restarting (the #2416 identity rule).
	f.owner.step( f.frame, f.resources, f.renderer );
	assert.equal( defined( f.owner.glow() ).particles, holder.particles );
	const continued = f.emission.step( [ defined( f.owner.glow() ) ], 11, () => true, 8 );
	assert.equal( defined( continued[0] ).gid, defined( emitted[0] ).gid );
	assert.equal( defined( continued[0] ).time, 1 );
	// A different rare sword is a new outfit: new array, new emitter.
	const swapped = { ...f.frame, equipment: [ { slot: 6, refObjId: 9002, plus: 0 } ] };
	f.owner.step( swapped, f.resources, f.renderer );
	assert.notEqual( defined( f.owner.glow() ).particles, holder.particles );
	const restarted = f.emission.step( [ defined( f.owner.glow() ) ], 12, () => true, 8 );
	assert.notEqual( defined( restarted[0] ).gid, defined( emitted[0] ).gid );
	// A plain sword offers no glow at all.
	const plain = { ...f.frame, equipment: [ { slot: 6, refObjId: 9000, plus: 0 } ] };
	f.owner.step( plain, f.resources, f.renderer );
	assert.equal( defined( f.owner.glow() ).particles.length, 0 );
	assert.deepEqual( f.emission.step( [ defined( f.owner.glow() ) ], 13, () => true, 8 ), [] );
});

test("body replacement clears incompatible local try-on before the next UI publication", () => {
	const f = fixture();
	f.owner.request( [ 2, 3 ] );
	f.owner.step( f.frame, f.resources, f.renderer );
	const frame = {
		...f.frame,
		avatars: [],
		resource: { ...f.frame.resource, codename: "CHAR_CH_WOMAN_ADVENTURER" },
		dress: {
			...f.frame.dress,
			defaultWear: {
				...f.frame.dress.defaultWear,
				CH_W_clothes_BA: f.frame.dress.defaultWear.CH_M_clothes_BA,
				CH_W_clothes_LA: f.frame.dress.defaultWear.CH_M_clothes_LA
			}
		}
	};
	assert.equal( f.owner.step( frame, f.resources, f.renderer ).length, 1 );
	assert.deepEqual( f.owner.state().wearable, [ 4 ] );
	assert.ok( f.assemblies.at( -1 ).parts.every( part => ![ "garment-2", "garment-3" ].includes( part.model ) ) );
});
