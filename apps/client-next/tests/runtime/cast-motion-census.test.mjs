/*
===========================================================================

cast-motion-census.test.mjs - authored player skill phase coverage

Walks every Chinese and European skill motion and matching player model.
The native clock rules apply to all resolved clips, not a list of examples.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { readPublishedAssetBytesSync } from "../../../../scripts/lib/publishedAsset.mjs";
const { skillMotionRole } = await import( "../../src/engine/foundation/animation/skill-motion.ts" );
const { advanceAction, actionLayers } = await import( "../../src/engine/foundation/animation/action-schedule.ts" );

/*
================
asset
================
*/
function asset( name ) {
	return JSON.parse(
		new TextDecoder().decode( readPublishedAssetBytesSync( name, "../../.generated/client-public" ) )
	);
}

test("every authored CH/EU player phase resolves and follows its native installation kind", () => {
	const models = asset( "/assets/anim/manifest.json" ).models;
	const records = asset( "/assets/skill/effectRecords.json" );
	const unique = [ new Map(), new Map(), new Map() ];
	for ( const [id, record] of Object.entries( records ) ) {
		const race = /^SKILL_(CH|EU)_/.exec( record.animBaseName ?? "" )?.[1];
		if ( !race ) continue;
		for ( const [name, model] of Object.entries( models ) ) {
			if ( !name.startsWith( `CHAR_${race}_` ) ) continue;
			for ( let phase = 0; phase < 3; phase++ ) {
				for ( const motion of record[`animTable${phase}`] ?? [] ) {
					const [, set, state] = skillMotionRole( record.animSlotKey, motion ).split( ":" );
					const entry = model.animationSets?.[set]?.[state] ?? model.animationSets?.default?.[state];
					assert.ok( entry, `${id} ${name} ${motion} has no native animation` );
					assert.equal( entry.looping, phase === 1 ? 1 : 0, `${id} ${name} ${motion} installation kind` );
					unique[phase].set( entry.path, entry );
				}
			}
		}
	}
	assert.deepEqual( unique.map( rows => rows.size ), [ 22, 24, 231 ], "review new authored phase families" );
	for ( const [stage, rows] of unique.entries() ) {
		for ( const [clip, entry] of rows ) {
			for ( const rate of [ .5, 1, 2 ] ) {
				/** @type {import("../../src/engine/foundation/animation/action-schedule.ts").ActionPhase[]} */
				const phases = [];
				phases.length = 3;
				phases[stage] = { clip, definition: { ...entry, loop: entry.looping === 1 } };
				const clock = { phases, phase: 0, started: 0, previous: 0, entered: false, animationRate: rate };
				advanceAction( clock, 0 );
				const entering = actionLayers( clock, .1 );
				assert.equal( entering.length, 1, clip );
				assert.equal( entering[0].time, stage === 1 ? .1 * rate : 0, clip );
				assert.equal( entering[0].weight, stage === 1 ? Math.min( 1, .5 * rate ) : .5, clip );
				if ( stage === 1 ) {
					advanceAction( clock, 10, 10 );
					assert.equal( actionLayers( clock, 10 + .201 / rate ).length, 0, clip );
				} else {
					const end = .2 + entry.durationMs / rate / 1000;
					advanceAction( clock, end + .001 );
					assert.equal( clock.phase, 3, clip );
					assert.equal( actionLayers( clock, end + .201 ).length, 0, clip );
				}
			}
		}
	}
});
