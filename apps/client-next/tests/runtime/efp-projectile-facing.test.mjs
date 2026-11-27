/*
===========================================================================

efp-projectile-facing.test.mjs - a flying .efp mover spawns facing its
flight, not the caster's body

Frozen Spear (skill 8635) fires three wizard_cold_bolt_b.efp movers at
SHOT events 2, 4 and 6. Their flight steps are straight and right, but a
mover with no spawn basis is drawn by pose.yaw, which stays the caster's
launch yaw: the followers leave after the lead and freeze whatever yaw
the body has then, lying across the flight line (BR-261127-0018-210B).
Native 8D8E30 writes the travel basis (8D9233) for every mover whose
path has length, whatever its resource; an imported .bsr carries those
rows turned over, an .efp program stands in native rows.

===========================================================================
*/
import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
const { createCharacterEffects } = await import( "../../src/engine/runtime/characters/effects/effects.ts" );
const { createPresentationRandom } = await import( "../../src/engine/runtime/random/random.ts" );
const { createEffectDecoder } = await import( "../../src/engine/runtime/assets/worker/effects/effects.ts" );
const manifest = JSON.parse( readFileSync( CLIENT_PUBLIC_ROOT + "/assets/skillfx/manifest.json", "utf8" ) );
const raw = JSON.parse( readFileSync( CLIENT_PUBLIC_ROOT + "/assets/skill/effectRecords.json", "utf8" ) );
const FROZEN_SPEAR = 8635;
const BOLT = "wizard_cold_bolt_b.efp";
const TARGET = { x: 90, z: 120 };

/*
================
spearCast

Steps one Frozen Spear cast from the origin caster at a monster 150
units away on a diagonal, and returns the bolt actors at each fire
event's first frame: the flight runs 90/120, so a bolt drawn by the
caster's yaw 0 basis (back (0, 0, 1)) sits across the flight line.
================
*/
function spearCast() {
	const catalog = createEffectDecoder().decode(
		new TextEncoder().encode( JSON.stringify( { [FROZEN_SPEAR]: raw[FROZEN_SPEAR] } ) )
	);
	let serial = 0;
	const jobs = new Map();
	// The stub loader covers only what the presenter calls here.
	const owner = createCharacterEffects(
		/** @type {any} */ ({
			available: () => 4,
			request( url, limit, type ) {
				jobs.set(
					++serial,
					type === "effects" ?
						{ kind: "effects", catalog } :
						{ kind: "bytes", buffer: new TextEncoder().encode( JSON.stringify( manifest ) ).buffer }
				);
				return serial;
			},
			take( id ) {
				const job = jobs.get( id );
				jobs.delete( id );
				return job;
			},
			cancel( id ) {
				jobs.delete( id );
			}
		}),
		"http://localhost",
		() => {},
		createPresentationRandom( 1 )
	);
	/** @type {any[]} */
	const entities = [
		{ gid: 1, kind: "local-player", regionId: 257, x: 0, y: 0, z: 0, heading: 0 },
		{ gid: 2, kind: "monster", regionId: 257, x: TARGET.x, y: 0, z: TARGET.z, heading: 0 }
	];
	const cast = {
		token: 1,
		caster: 1,
		target: 2,
		skill: FROZEN_SPEAR,
		receivedAtMs: 0,
		results: [ { target: 2, impacts: [ { damage: 10 } ] } ]
	};
	/** @type {any} Only the fields the presenter reads. */
	const game = { localGid: 1, inventory: [], casts: [ cast ] };
	const step = ( now, events = [] ) =>
		owner.step(
			entities,
			game,
			now,
			() => true,
			() => 10,
			events,
			// Every socket sits at its actor's feet.
			/** @type {any} */ (gid => ({
				regionId: 257,
				x: gid === 1 ? 0 : TARGET.x,
				y: 0,
				z: gid === 1 ? 0 : TARGET.z,
				yaw: 0
			})),
			[],
			2
		);
	step( 0 );
	step( .1 );
	const spawned = [], seen = new Set();
	for ( const [index, event] of [ 2, 4, 6 ].entries() ) {
		const actors = step( .2 + index * .15, [ { cast, phase: "SHOT", event, at: .2 + index * .15 } ] );
		for ( const actor of actors ) {
			if ( !decodeURIComponent( actor.model ?? "" ).endsWith( BOLT ) || seen.has( actor.gid ) ) continue;
			seen.add( actor.gid );
			spawned.push( actor );
		}
	}
	return spawned;
}

test("every Frozen Spear bolt spawns with the travel basis, leader and followers alike", () => {
	const bolts = spearCast();
	assert.equal( bolts.length, 3, "the three bolt stages did not all spawn" );
	// The flight's horizontal direction and the model basis' back column:
	// native rows point the model's back from the destination back to the
	// source, so the back column must oppose the flight direction. The
	// destination sits at the target's chest, so the column may pitch a
	// few degrees; what the sideways bug yields instead is the caster's
	// kept yaw (back (0, 0, 1) here), about 140 off the flight line.
	const length = Math.hypot( TARGET.x, TARGET.z );
	const flightX = TARGET.x / length, flightZ = TARGET.z / length;
	for ( const [index, bolt] of bolts.entries() ) {
		const basis = bolt.effectBasis;
		assert.ok( basis, `bolt ${index + 1} has no spawn basis: it would fly sideways` );
		const backLength = Math.hypot( basis[6], basis[8] );
		assert.ok( backLength > .98, `bolt ${index + 1} back column is not level (${backLength})` );
		const facing = -(basis[6] * flightX + basis[8] * flightZ) / backLength;
		assert.ok(
			facing > .995,
			`bolt ${index + 1} back column sits ${
				(Math.acos( Math.min( 1, facing ) ) * 180 / Math.PI).toFixed( 1 )
			} degrees off the flight line`
		);
	}
});
