/*
===========================================================================

correction-carry.test.mjs - a correction carries a predicted cast's statics

BR-261113-1118-7667: the press predicted a cast while the client's walk
had run 21 units ahead of the server. Its static spawned there; the
server's walk stop then corrected the body back and the static stayed
behind. A correction of the local mover now carries the world-placed
visuals of a predicted (or adopted) cast; a cast the server opened
unpredicted and a knockback leave them where they were cast. Drives the
shipped effect owner through the shared native loader.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";

const { createCharacterEffects } = await import(
	sourceFileUrl( "src/engine/runtime/characters/effects/effects.ts" ).href
);
const { createCorrectionCarry, carriedPose } = await import(
	sourceFileUrl( "src/engine/runtime/characters/effects/correction-carry.ts" ).href
);
const { createPresentationRandom } = await import(
	sourceFileUrl( "src/engine/runtime/random/random.ts" ).href
);

const REGION = 26988;
// The report's poses: where the client's walk stood at the press, and
// where the server's walk stop put the body.
const AHEAD = { regionId: REGION, x: 387, y: 234.4, z: 774, angle: 0 };
const SERVER = { regionId: REGION, x: 406.3, y: 232.5, z: 783.3, angle: 0 };
// The server placing the body back (movement.ts beginTransition( "correction" )).
const CORRECTION = { reason: "correction", relocation: 0, eligible: true };
const INPUT = { reason: "input", relocation: 0, eligible: true };
const PREDICTED_TOKEN = -0x10001;
const encode = value => new TextEncoder().encode( JSON.stringify( value ) );
const CATALOG = {
	"1": {
		clips: [ "attack1" ],
		stages: [ {
			resource: "cast.efp",
			startEvent: 1,
			action: "AT_STOP",
			move: "MOV_NONE",
			bone: null,
			offset: [ 0, 0, 0 ],
			life: 5,
			count: 1,
			scripts: []
		} ]
	}
};

/*
================
makeEffects

An effect owner over a stub asset owner serving one decoded catalog and
no stage models.
================
*/
function makeEffects() {
	let id = 0;
	const jobs = new Map();
	return createCharacterEffects(
		{
			available: () => 4,
			request( url, limit, decode ) {
				jobs.set(
					++id,
					decode === "effects" ?
						{ kind: "effects", catalog: CATALOG } :
						{ kind: "bytes", buffer: encode( { format: "sro-skill-stage-models", models: {} } ).buffer }
				);
				return id;
			},
			take( jobId ) {
				const result = jobs.get( jobId );
				jobs.delete( jobId );
				return result;
			},
			cancel( jobId ) {
				jobs.delete( jobId );
			}
		},
		"http://fixture.invalid",
		() => {},
		createPresentationRandom( 1 )
	);
}

/*
================
castThenCorrect

Spawns the cast's static at AHEAD, then publishes the server's
correction to SERVER (the drawn body still at AHEAD, as on the burst
frame), and answers the static's pose before and after.
================
*/
/**
 * @param {{ token: number; predictedToken?: number; movementPath?: { displacement: boolean; }; }} options
 */
function castThenCorrect( { token, predictedToken, movementPath } ) {
	const effects = makeEffects();
	const cast = { token, caster: 1, target: 2, skill: 1, ...(predictedToken ? { predictedToken } : {}) };
	const entities = [
		{ gid: 1, kind: "local-player", ...AHEAD, heading: 0 },
		{ gid: 2, kind: "player", regionId: REGION, x: 360, y: 234, z: 740, heading: 0 }
	];
	const drawnPoses = new Map( [ [ 1, { ...AHEAD } ] ] );
	const step = ( now, gameplay, triggers = [] ) =>
		effects.step(
			entities,
			gameplay,
			now,
			() => true,
			() => 0.5,
			triggers,
			undefined,
			[],
			2,
			true,
			new Map(),
			drawnPoses
		);
	const before = { localGid: 1, pose: { ...AHEAD }, movementRevision: 18, casts: [ cast ] };
	let actors = [];
	for ( const now of [ 0, .1, .2, .3 ] ) {
		// The catalog loads over the first frames; the trigger repeats
		// until it does, and the effect owner fires it once.
		actors = step( now, before, [ { cast, phase: "SHOT", event: 1, at: now } ] );
	}
	const placed = actors[0]?.pose;
	const after = {
		...before,
		pose: { ...SERVER },
		movementRevision: 19,
		movementTransition: CORRECTION,
		...(movementPath ? { movementPath } : {})
	};
	actors = step( .4, after, [ { cast, phase: "SHOT", event: 1, at: .4 } ] );
	const carried = actors[0]?.pose;
	assert.equal( effects.error(), null );
	effects.dispose();
	return { placed, carried };
}

test("a predicted cast's static follows the server's correction", () => {
	const { placed, carried } = castThenCorrect( { token: PREDICTED_TOKEN } );
	assert.ok( placed, "the static spawned" );
	assert.equal( placed.x, AHEAD.x );
	assert.equal( placed.z, AHEAD.z );
	assert.ok( Math.abs( carried.x - SERVER.x ) < 1e-6, `x ${carried.x}` );
	assert.ok( Math.abs( carried.z - SERVER.z ) < 1e-6, `z ${carried.z}` );
	assert.ok( Math.abs( carried.y - (placed.y + SERVER.y - AHEAD.y) ) < 1e-6, `y ${carried.y}` );
});

test("the server cast that adopted a prediction carries it too", () => {
	const { carried } = castThenCorrect( { token: 7, predictedToken: PREDICTED_TOKEN } );
	assert.ok( Math.abs( carried.x - SERVER.x ) < 1e-6 );
});

test("a cast the server opened unpredicted keeps its static where it was cast", () => {
	const { placed, carried } = castThenCorrect( { token: 7 } );
	assert.deepEqual( [ carried.x, carried.z ], [ placed.x, placed.z ] );
});

test("a knockback leaves a predicted cast's static behind, as native does", () => {
	const { placed, carried } = castThenCorrect( {
		token: PREDICTED_TOKEN,
		movementPath: { displacement: true }
	} );
	assert.deepEqual( [ carried.x, carried.z ], [ placed.x, placed.z ] );
});

test("the carry answers a correction's delta once, and nothing for a walk or a teleport", () => {
	const carry = createCorrectionCarry();
	assert.equal( carry.step( { pose: AHEAD, movementRevision: 18 } ), null, "the first frame only remembers" );
	assert.equal(
		carry.step( { pose: { ...AHEAD, x: 390 }, movementRevision: 18 } ),
		null,
		"a walk keeps its revision"
	);
	assert.equal(
		carry.step( { pose: { ...AHEAD, x: 391 }, movementRevision: 19, movementTransition: INPUT } ),
		null,
		"a new click bumps the revision but is no correction"
	);
	const delta = carry.step( { pose: SERVER, movementRevision: 20, movementTransition: CORRECTION } );
	assert.ok( delta );
	assert.ok( Math.abs( delta[0] - (SERVER.x - 391) ) < 1e-9 );
	assert.equal(
		carry.step( { pose: SERVER, movementRevision: 20, movementTransition: CORRECTION } ),
		null,
		"the same correction carries once"
	);
	assert.equal(
		carry.step( { pose: { ...SERVER, x: SERVER.x + 500 }, movementRevision: 21, movementTransition: CORRECTION } ),
		null,
		"a teleport"
	);
});

test("a carried pose crosses a region boundary", () => {
	const pose = carriedPose( { regionId: REGION, x: 1910, y: 0, z: 10 }, [ 20, 0, 0 ] );
	assert.equal( pose.regionId, REGION + 1 );
	assert.ok( Math.abs( pose.x - 10 ) < 1e-9 );
});
