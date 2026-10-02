/*
===========================================================================

effect-socket-fallback.test.mjs - native effect endpoints when a marker is absent

Exercise the renderer's real socket lookup and the moving effect owner together.
A supported projectile must survive a missing named marker without weakening
strict socket queries used by contact effects.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import { test } from "node:test";
const { createCharacters } = await import( "../../src/engine/runtime/renderer/characters/characters.ts" );
const { createCharacterEffects } = await import( "../../src/engine/runtime/characters/effects/effects.ts" );
const { createEffectDecoder } = await import( "../../src/engine/runtime/assets/worker/effects/effects.ts" );
const { createPresentationRandom } = await import( "../../src/engine/runtime/random/random.ts" );
const { radians } = await import( "../../src/engine/foundation/math/angles.ts" );

/*
================
model

Keep a nonzero marker position so a root fallback cannot pass as a socket hit.
================
*/
function model( name = "root" ) {
	return {
		nodes: [ { name, parent: -1, translation: [ 2, 3, 4 ], rotation: [ 0, 0, 0, 1 ], scale: [ 1, 1, 1 ] } ],
		primitives: [],
		images: [],
		clips: []
	};
}

/*
================
actor
================
*/
function actor( gid, x = 0 ) {
	return {
		gid,
		model: "body",
		pose: { regionId: 257, x, y: 0, z: 0, yaw: radians( 0 ) },
		scale: 1,
		clip: "",
		time: 0,
		loop: true
	};
}

/*
================
encode
================
*/
function encode( value ) {
	return new TextEncoder().encode( JSON.stringify( value ) );
}

test("effect sockets fall back to the root while strict contact queries still miss", () => {
	const renderer = createCharacters(), row = actor( 1, 10 );
	renderer.model( "body", model(), [] );
	assert.equal( renderer.socket( [ row ], 1, "ai_end", [ 0, 0, 0 ] ), null );
	assert.deepEqual( renderer.socket( [ row ], 1, { name: "ai_end", fallback: "mount-root" }, [ 1, 2, 3 ] ), {
		...row.pose,
		x: 11,
		y: 2,
		z: 3
	} );
	assert.deepEqual( renderer.socket( [ row ], 1, { name: "root", fallback: "mount-root" }, [ 1, 2, 3 ] ), {
		...row.pose,
		x: 13,
		y: 5,
		z: 7
	} );
	row.pose.yaw = radians( Math.PI / 2 );
	const rotated = renderer.socket( [ row ], 1, { name: "ai_end", fallback: "mount-root" }, [ 1, 2, 3 ] );
	assert.ok( rotated );
	assert.ok( Math.abs( rotated.x - 13 ) < 1e-5 );
	assert.ok( Math.abs( rotated.z + 1 ) < 1e-5 );
});

test("effect sockets retry mounts and use the last mount root when all markers miss", () => {
	const renderer = createCharacters(),
		rider = { ...actor( 1 ), mountedOn: 2 },
		mount = { ...actor( 2, 20 ), model: "mount" };
	renderer.model( "body", model(), [] );
	renderer.model( "mount", model( "ai_end" ), [] );
	const query = { name: "ai_end", fallback: /** @type {const} */ ("mount-root") };
	assert.equal( renderer.socket( [ rider, mount ], 1, query, [ 0, 0, 0 ] )?.x, 22 );
	assert.equal( renderer.socket( [ rider, mount ], 1, { ...query, name: "absent" }, [ 0, 0, 0 ] )?.x, 20 );
	assert.equal( renderer.socket( [ rider ], 1, query, [ 0, 0, 0 ] ), null );
	assert.equal( renderer.socket( [ { ...rider, model: "cold" }, mount ], 1, query, [ 0, 0, 0 ] ), null );
	assert.equal( renderer.socket( [ mount ], 1, query, [ 0, 0, 0 ] ), null );
	assert.throws(
		() => renderer.socket( [ rider, { ...mount, mountedOn: 1 } ], 1, { ...query, name: "absent" }, [ 0, 0, 0 ] ),
		/Cyclic/
	);
});

/*
================
adapted

A model as character.ts imports it: every root sits under the
__gltf_left_handed__ node, whose Sx(-1) every pose global inherits.
================
*/
function adapted( name, translation ) {
	const node = ( n, parent, t, scale = [ 1, 1, 1 ] ) => ({
		name: n,
		parent,
		translation: t,
		rotation: [ 0, 0, 0, 1 ],
		scale
	});
	return {
		nodes: [ node( "__gltf_left_handed__", -1, [ 0, 0, 0 ], [ -1, 1, 1 ] ), node( name, 0, translation ) ],
		primitives: [],
		images: [],
		clips: []
	};
}

/*
================
determinant
================
*/
function determinant( m ) {
	return m[0] * (m[5] * m[10] - m[9] * m[6]) - m[4] * (m[1] * m[10] - m[9] * m[2]) +
		m[8] * (m[1] * m[6] - m[5] * m[2]);
}

test("a mounted rider keeps its on-foot handedness and facing on the saddle", () => {
	const renderer = createCharacters();
	renderer.model( "rider", adapted( "Bip01 R Hand", [ 1, 0, 0 ] ), [] );
	renderer.model( "horse", adapted( "saddle", [ 0, 10, 0 ] ), [] );
	renderer.model( "bare", adapted( "tail", [ 0, 0, -5 ] ), [] );
	const yaw = radians( 0.5 );
	const pose = { regionId: 257, x: 0, y: 0, z: 0, yaw };
	const rider = { gid: 1, model: "rider", pose, scale: 1, clip: "", time: 0, loop: true };
	const horse = { gid: 2, model: "horse", pose, scale: 1, clip: "", time: 0, loop: true };
	const foot = renderer.matrix( [ rider ], 1 );
	const mounted = renderer.matrix( [ { ...rider, mountedOn: 2 }, horse ], 1 );
	assert.ok( foot && mounted );
	// The saddle's Sx(-1) is cancelled once: same rotation, raised by the saddle.
	assert.ok( determinant( mounted ) > 0, "a mirrored rider is culled inside out" );
	for ( let i = 0; i < 12; i++ ) assert.ok( Math.abs( mounted[i] - foot[i] ) < 1e-5, `column entry ${i}` );
	assert.ok( Math.abs( mounted[13] - foot[13] - 10 ) < 1e-5 );
	// Effect sockets sampled through the mount see the same hand as on foot.
	const query = { name: "Bip01 R Hand", fallback: /** @type {const} */ ("mount-root") };
	const hand = renderer.socket( [ rider ], 1, query, [ 0, 0, 0 ] );
	const riding = renderer.socket( [ { ...rider, mountedOn: 2 }, horse ], 1, query, [ 0, 0, 0 ] );
	assert.ok( hand && riding );
	assert.ok( Math.abs( riding.x - hand.x ) < 1e-5 && Math.abs( riding.z - hand.z ) < 1e-5 );
	assert.ok( Math.abs( riding.y - hand.y - 10 ) < 1e-5 );
	// A mount without a saddle keeps the plain root: nothing to cancel.
	const rooted = renderer.matrix( [ { ...rider, mountedOn: 3 }, { ...horse, gid: 3, model: "bare" } ], 1 );
	assert.ok( rooted );
	for ( let i = 0; i < 16; i++ ) assert.ok( Math.abs( rooted[i] - foot[i] ) < 1e-5, `root entry ${i}` );
});

for ( const targetBone of [ null, "missing-target-marker" ] ) {
	test(`Power Shot projectile survives missing source and ${targetBone ?? "unnamed target"} sockets`, () => {
		const resource = "res/item/china/weapon/cha_arrow_normal.bsr";
		const catalog = createEffectDecoder().decode( encode( {
			8074: {
				authoredShotAnimationNames: [ "ANI_SKILL_1" ],
				authoredStages: [ {
					animationPhase: "SHOT",
					startEvent: 1,
					actionType: "AT_MOV_1TAR",
					objectResourcePath: resource,
					startBone: "ai_end",
					startOffset: [ 0, 0, 0 ],
					targetBone,
					targetOffset: [ 0, 11, -2 ],
					move: { kind: "MOV_STRAIGHT", delay: 0, startSpeed: 250, endSpeed: 250 },
					createCount: 1,
					scripts: [ "SCT_ARROW" ]
				} ]
			}
		} ) );
		let serial = 0;
		const jobs = new Map();
		const effects = createCharacterEffects(
			{
				available: () => 4,
				progress: () => null,
				health: () => ({ phase: "running" }),
				install: () => {},
				dispose: () => jobs.clear(),
				/*
			================
			request
			================
			*/
				request( url, limit, kind ) {
					jobs.set(
						++serial,
						kind === "effects" ? { kind: "effects", catalog } : {
							kind: "bytes",
							buffer: encode( {
								format: "sro-skill-stage-models",
								models: {
									[resource]: { glb: "/assets/arrow.glb", clips: [], clipLoop: false }
								}
							} ).buffer
						}
					);
					return serial;
				},
				/*
			================
			take
			================
			*/
				take( id ) {
					const result = jobs.get( id );
					jobs.delete( id );
					return result;
				},
				/*
			================
			cancel
			================
			*/
				cancel( id ) {
					jobs.delete( id );
				}
			},
			"http://localhost",
			() => {},
			createPresentationRandom( 1 )
		);
		const renderer = createCharacters(), actors = [ actor( 1 ), actor( 2, 100 ) ];
		renderer.model( "body", model(), [] );
		/** @type {import('../../src/engine/contracts/world.ts').EntityState[]} */
		const entities = actors.map( row => ({
			...row.pose,
			gid: row.gid,
			heading: 0,
			refObjId: 1,
			kind: "player",
			name: `Player ${row.gid}`
		}) );
		const cast = { token: 1, caster: 1, target: 2, skill: 8074, damage: 0, fatal: false };
		/** @type {import('../../src/engine/contracts/gameplay.ts').GameplayState} */
		const gameplay = {
			revision: 0,
			localGid: 3,
			pose: null,
			authoritativePose: null,
			pendingMoves: 0,
			acknowledgedMove: 0,
			target: 0,
			targetPending: 0,
			inventory: [],
			inventoryPending: false,
			vitals: [],
			casts: [ cast ],
			error: null
		};
		/*
		================
		step
		================
		*/
		function step( now, triggers = [], state = gameplay ) {
			return effects.step(
				entities,
				state,
				now,
				() => true,
				() => 10,
				triggers,
				( gid, bone, offset ) => renderer.socket( actors, gid, { name: bone, fallback: "mount-root" }, offset )
			);
		}
		step( 0 );
		step( .1 );
		step( .2 );
		const launched = step( 1, [ { cast, phase: "SHOT", event: 1, at: 1 } ] );
		assert.equal( effects.error(), null );
		assert.equal( launched.length, 1 );
		assert.equal( launched[0].pose.x, 0 );
		assert.ok( step( 1.1 )[0].pose.x > 0 );
		assert.deepEqual( step( 2 ), [] );
		// A caster whose model is still loading holds the callback while its
		// cast lives (native compounds load synchronously), then launches from
		// the posed marker; only a model still cold when the cast ends reports.
		actors[0].model = "cold";
		cast.token = 2;
		assert.deepEqual( step( 3, [ { cast, phase: "SHOT", event: 1, at: 3 } ] ), [] );
		assert.equal( effects.error(), null );
		actors[0].model = "body";
		const held = step( 3.1 );
		assert.equal( effects.error(), null );
		assert.equal( held.length, 1 );
		// The held mover keeps its native clock: 0.1 s at 250 units/s.
		assert.ok( Math.abs( held[0].pose.x - 25 ) < 1 );
		actors[0].model = "cold";
		cast.token = 3;
		assert.deepEqual( step( 5, [ { cast, phase: "SHOT", event: 1, at: 5 } ] ), [] );
		step( 5.1, [], { ...gameplay, casts: [] } );
		const error = effects.error();
		assert.ok( error );
		assert.match( error, /source-socket:1\/ai_end/ );
		effects.dispose();
	});
}
