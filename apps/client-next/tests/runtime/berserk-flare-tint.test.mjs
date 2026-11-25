/*
===========================================================================

berserk-flare-tint.test.mjs - a berserk body keeps its Hwan red through a
port-only flare

The client's 85C590 makes no material call while the berserk byte (+0x2B6)
is 1, so the Hwan register's SCT_MAT is the only tint a berserk body wears.
The port's wolf signals (0x324B on the owner) flare a near-white tint on
their target; on a berserk owner that flare replaced the red for its length
(#3543). Drives the shipped effects owner with the real effect catalogs.

===========================================================================
*/
import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
const { createCharacterEffects } = await import( "../../src/engine/runtime/characters/effects/effects.ts" );
const { createEffectDecoder } = await import( "../../src/engine/runtime/assets/worker/effects/effects.ts" );
const { createCombat } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/combat/combat.ts"
);
const { createPresentationRandom } = await import( "../../src/engine/runtime/random/random.ts" );
const { radians } = await import( "../../src/engine/foundation/math/angles.ts" );

// 0xF1: the Execute III sweep, a wolf signal that lands on the owner.
const WOLF_SWEEP = 0xf1;
const FRAME_SECONDS = 1 / 30;

/*
================
createOwner

The effects owner over the generated catalogs, with every model resident.
================
*/
function createOwner() {
	const decode = createEffectDecoder(),
		named = decode.decode( readFileSync( CLIENT_PUBLIC_ROOT + "/assets/skill/namedEffectRecords.json" ) ),
		catalog = decode.decode( readFileSync( CLIENT_PUBLIC_ROOT + "/assets/skill/effectRecords.json" ) );
	let serial = 0;
	const jobs = new Map();
	/** @type {import("../../src/engine/contracts/assets.ts").AssetOwner} */
	const assets = {
		progress: () => null,
		health: () => ({ phase: "running" }),
		available: () => 4,
		request( url, limit, type ) {
			jobs.set(
				++serial,
				type === "effects" ?
					{ kind: "effects", catalog: url.includes( "namedEffect" ) ? named : catalog } :
					{
						kind: "bytes",
						buffer: new TextEncoder().encode(
							JSON.stringify( { format: "sro-skill-stage-models", models: {} } )
						).buffer
					}
			);
			return serial;
		},
		take( id ) {
			const r = jobs.get( id );
			jobs.delete( id );
			return r;
		},
		textureFeatures() {},
		hdTextures() {},
		cancel() {},
		install() {},
		dispose() {}
	};
	return createCharacterEffects(
		assets,
		"http://localhost",
		() => {},
		createPresentationRandom( 7 )
	);
}

/*
================
runFlare

Steps one player through a wolf sweep and returns every tint it wore.
================
*/
function runFlare( berserk ) {
	const owner = createOwner();
	/** @type {import("../../src/engine/contracts/world.ts").EntityState} */
	const entity = {
			gid: 2,
			kind: "player",
			refObjId: 1907,
			regionId: 257,
			x: 0,
			y: 0,
			z: 0,
			heading: 0,
			name: "flare-owner",
			appearanceState: [ 1, 0, berserk ? 1 : 0 ]
		},
		entities = [ entity ],
		body = {
			gid: 2,
			model: "/assets/body.glb",
			clip: "stand",
			time: 0,
			loop: true,
			pose: { regionId: 257, x: 0, y: 0, z: 0, yaw: radians( 0 ) },
			scale: 1,
			height: 40,
			heightFactor: 1
		};
	const combat = createCombat( gid => entities.find( e => e.gid === gid ) );
	const step = at =>
		owner.step(
			entities,
			{ ...combat.state(), localGid: 1, pose: null, inventory: [] },
			at,
			() => true,
			() => 1,
			[],
			undefined,
			[ body ]
		);
	// Settle the body first: the Hwan state effect and its material register.
	let at = 0;
	for ( ; at < 2; at += FRAME_SECONDS ) step( at );
	const settled = owner.appearance( 2 ).materialTint;
	entities[0] = { ...entity, emote: { action: WOLF_SWEEP, revision: 1, atMs: at * 1000 } };
	const tints = [];
	for ( const end = at + 1.5; at < end; at += FRAME_SECONDS ) {
		step( at );
		tints.push( owner.appearance( 2 ).materialTint );
	}
	assert.equal( owner.error(), null );
	owner.dispose();
	return { settled, tints };
}

const isFlare = tint => !!tint && tint[0] === 1 && tint[1] < 1 && tint[1] === tint[2];

test("a wolf flare tints a calm owner: the control for the berserk case", () => {
	const { settled, tints } = runFlare( false );
	assert.equal( settled, undefined );
	assert.ok( tints.some( isFlare ), "the flare never tinted the owner; the berserk case below proves nothing" );
});

test("a berserk owner keeps the Hwan red through the whole flare (#3543)", () => {
	const { settled, tints } = runFlare( true );
	assert.ok( settled, "the Hwan register never painted the berserk body" );
	assert.ok( settled[0] > settled[1], "the Hwan register is red" );
	for ( const [index, tint] of tints.entries() ) {
		assert.ok( tint && !isFlare( tint ), `frame ${index}: the flare replaced the Hwan red (${tint})` );
		assert.ok( tint[0] > tint[1], `frame ${index}: not the Hwan red (${tint})` );
	}
});
