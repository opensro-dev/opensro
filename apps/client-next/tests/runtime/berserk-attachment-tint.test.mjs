/*
===========================================================================

berserk-attachment-tint.test.mjs - a berserk body's attachments wear its
material register

The native character is one CCompChar: SetMaterial (vtable +0x48) writes one
register on it, and the draw (sub_A91970 through sub_A5EE80) applies that
register to every part of the compound - body, dress and wings alike. In the
port the register reaches appearance() as the owner's materialTint, but the
auxiliary children (wings) and the Hwan hair were built without it, so they
stayed white while the body turned Hwan red (#3543). Drives the shipped
auxiliary presenter; the bindings are fixtures, no GPU is needed.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";
const { createAuxiliaryPresentation } = await import(
	"../../src/engine/runtime/characters/presentation-auxiliary.ts"
);
const { radians } = await import( "../../src/engine/foundation/math/angles.ts" );

// The Hwan register's red and an absorb shield's blue: any two tints prove
// the child tracks the owner's current register rather than a fixed colour.
const HWAN_RED = /** @type {const} */ ([ .85, .16, .02 ]);
const SHIELD_BLUE = /** @type {const} */ ([ .9, .98, 1 ]);
const WING_GLB = "/assets/wings.glb";
const HAIR_GLB = "/assets/hair.glb";

/*
================
fixture

One player, its body actor already in the frame's actor map the way
actor-advance publishes it (with the register tint when one is active), and
one committed auxiliary wing.
================
*/
function fixture() {
	let nextId = -1;
	const auxiliary = createAuxiliaryPresentation( () => nextId-- );
	const entity = {
		gid: 2,
		kind: /** @type {const} */ ("player"),
		appearanceState: [ 1, 0, 0 ],
		moving: false
	};
	const wing = {
		id: 11,
		entry: { glb: WING_GLB, parts: [], bone: "Bip01 Spine2", clips: [ "stand" ], particleModifiers: [] }
	};
	const bindings = {
		resourceFor: () => undefined,
		dress: {},
		resources: { ready: () => true },
		ridesByRider: new Map(),
		riderModes: new Map(),
		committedAuxiliary: new Map( [ [ entity.gid, [ wing ] ] ] )
	};
	/*
	================
	step
	================
	*/
	/**
	 * @param {readonly [number, number, number] | undefined} materialTint
	 * @param {import('../../src/engine/contracts/world.ts').EntityState} [who]
	 * @param {Parameters<typeof auxiliary.step>[1]} [bound]
	 */
	function step( materialTint, who = /** @type {any} */ (entity), bound = /** @type {any} */ (bindings) ) {
		const owner = {
			gid: who.gid,
			model: "/assets/body.glb",
			pose: { regionId: 257, x: 0, y: 0, z: 0, yaw: radians( 0 ) },
			clip: "stand",
			time: 0,
			loop: true,
			scale: 1,
			materialTint
		};
		/** @type {Parameters<typeof auxiliary.step>[0]} */
		const frame = {
			entities: [ who ],
			seconds: 1,
			next: new Map( [ [ owner.gid, owner ] ] ),
			clocks: new Map(),
			gameplay: null,
			localMover: () => false,
			castByActor: new Map(),
			actionLayersByActor: new Map(),
			particleHolders: []
		};
		auxiliary.step( frame, bound );
		return [ ...frame.next.values() ].filter( actor => actor.gid !== who.gid );
	}
	return { auxiliary, entity, wing, bindings, step };
}

test("an auxiliary wing wears its owner's current material tint, frame by frame", () => {
	const f = fixture();
	const red = defined( f.step( HWAN_RED ).find( actor => actor.model === WING_GLB ) );
	assert.deepEqual( red.materialTint, HWAN_RED, "the wing stayed white under the owner's Hwan register" );
	const blue = defined( f.step( SHIELD_BLUE ).find( actor => actor.model === WING_GLB ) );
	assert.deepEqual( blue.materialTint, SHIELD_BLUE, "the wing kept a stale tint instead of the owner's current one" );
});

test("an auxiliary wing is untinted when its owner is", () => {
	const f = fixture();
	const child = defined( f.step( undefined ).find( actor => actor.model === WING_GLB ) );
	assert.equal( child.materialTint, undefined, "a tint was fabricated for a calm owner" );
});

test("the Hwan hair wears the berserk register like every other part", () => {
	const f = fixture();
	const berserk = { ...f.entity, appearanceState: [ 1, 0, 1 ] },
		bound = {
			...f.bindings,
			committedAuxiliary: new Map(),
			resourceFor: () => ({ codename: "CHAR_CH_MAN_FIXTURE" }),
			dress: { hwan: { CH_M: { glb: HAIR_GLB, parts: [], clip: "stand", bone: "Bip01 Head" } } }
		};
	const hair = defined(
		f.step( HWAN_RED, /** @type {any} */ (berserk), /** @type {any} */ (bound) ).find( actor =>
			actor.model === HAIR_GLB
		)
	);
	assert.deepEqual( hair.materialTint, HWAN_RED, "the Hwan hair stayed white on a berserk body" );
});
