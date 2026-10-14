/*
===========================================================================

local-life-authority.test.mjs - LIFE alone kills the local player on screen

BR-261014-1051-49A0: the death clip (and its cry) played, then the player
stood back up. The local latch spawned without an appearance row, so
presentation read its death from the predicted vitals HP, which a hit
result subtracts from; a hit whose B070 arrived after an absolute 0x33A6
that already held it drove that prediction to 0. Natively only LIFE
(0x3122 channel 0, 777B60) and the fatal impact enter death (8E64F0).

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";
const { createEntities } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/entities/entities.ts"
);
const { createPresentationState } = await import( "../../src/engine/runtime/characters/presentation-state.ts" );

const LOCAL_GID = 7;
const MODEL = 1933;

/*
================
lifeFrame

0x3122 channel 0: [u32 gid][u8 0][u8 life], 1 alive and 2 dead.
================
*/
function lifeFrame( gid, life ) {
	const payload = Buffer.alloc( 6 );
	payload.writeUInt32LE( gid );
	payload[4] = 0;
	payload[5] = life;
	return { opcode: 0x3122, payload };
}

/*
================
harness

The worker's entity owner with a latched local player, stepped through
presentation-state as characters.ts does. predictedHpZero puts the local
gid in deadGids, the set characters.ts builds from vitals with hp 0.
================
*/
function harness() {
	const owner = createEntities();
	owner.bootstrap( {
		protocolVersion: 2,
		nativeResult: 1,
		refObjSnapshot: [],
		localPlayerEntry: { modelRef: MODEL, startProfile: { regionId: 257, x: 1, y: 2, z: 3, angle: 0 } }
	} );
	owner.receive( { opcode: 0x32a6, payload: Buffer.from( [ LOCAL_GID, 0, 0, 0, 0, 0, 0, 0 ] ) }, 0 );
	const state = createPresentationState();
	const published = {
		catalog: new Map( [ [ MODEL, {
			codename: "CHAR_FIXTURE",
			refObjId: MODEL,
			glb: "/assets/char/fixture.glb",
			clips: [ "stand", "death", "deathloop" ]
		} ] ] ),
		recoveryByCodename: new Map( [ [ "CHAR_FIXTURE", 2000 ] ] ),
		animationStates: new Map(),
		deathModels: new Map()
	};
	let seconds = 0;
	return {
		owner,
		/*
		================
		step
		================
		*/
		/** @param {{ predictedHpZero?: boolean; }} [frame] */
		step( { predictedHpZero = false } = {} ) {
			seconds += .1;
			const entity = defined( owner.read( LOCAL_GID ) );
			state.step(
				{
					entities: [ entity ],
					seconds,
					simulationMs: seconds * 1000,
					logicalPose: e => ({ regionId: e.regionId, x: e.x, y: e.y, z: e.z, angle: 0 }),
					appearanceRef: e => e.refObjId,
					states: new Map(),
					health: undefined,
					deadGids: new Set( predictedHpZero ? [ LOCAL_GID ] : [] ),
					hitByActor: new Map(),
					castByActor: new Map(),
					resources: { duration: () => 1 },
					random: { range: () => 0 },
					active: new Set( [ LOCAL_GID ] ),
					pendingDeaths: new Set(),
					uncensored: false
				},
				{ failure: null },
				published
			);
			return defined( state.idleStates.get( LOCAL_GID ) );
		}
	};
}

test("a predicted HP of 0 never shows the local player's death", () => {
	const h = harness();
	h.step();
	const drifted = h.step( { predictedHpZero: true } );
	assert.ok( !drifted.deathEntered, "no death clip from a predicted HP of 0" );
	const restored = h.step();
	assert.ok( !restored.deathEntered );
	h.owner.dispose();
});

test("LIFE dead still shows the local player's death, and LIFE alive ends it", () => {
	const h = harness();
	h.step();
	h.owner.receive( lifeFrame( LOCAL_GID, 2 ), 1 );
	assert.ok( h.step().deathEntered, "LIFE dead enters the death" );
	h.owner.receive( lifeFrame( LOCAL_GID, 1 ), 2 );
	assert.ok( !h.step().deathEntered, "LIFE alive (rebirth) ends it" );
	h.owner.dispose();
});
