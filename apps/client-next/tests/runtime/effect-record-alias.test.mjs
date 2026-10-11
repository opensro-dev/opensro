/*
===========================================================================

effect-record-alias.test.mjs - a skill draws through its Basic_Original

GlobalEffectManager_FindSkillMotionMetadata (917240) remaps a skill id to
skilldata column 6 before the record lookup. The published table stores
such a skill as { remapOf }, and the decoder answers it with the
original's record. Without it an attack pet past its first level cast a
skill with no record: no swing, no damage, no death on the client.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
import { readPublishedAssetBytesSync } from "../../../../scripts/lib/publishedAsset.mjs";

const { createEffectDecoder } = await import( "../../src/engine/runtime/assets/worker/effects/effects.ts" );
const { effectRecordAlias, resolveEffectRecordAliases } = await import(
	"../../src/engine/foundation/animation/effect-record-alias.ts"
);

const ORIGINAL_WOLF_ATTACK = 3824; // PSKILL_P_WOLF_01_ATTACK01
const LEVELLED_WOLF_ATTACKS = [
	3825, // PSKILL_P_WOLF_002_ATTACK_01
	3833, // PSKILL_P_WOLF_010_ATTACK_01
	16361 // PSKILL_P_WOLF_002_ATTACK_01_SMALL
];

/*
================
encode
================
*/
function encode( value ) {
	return new TextEncoder().encode( JSON.stringify( value ) );
}

test("an alias answers with its original's record, one remap only", () => {
	assert.equal( effectRecordAlias( { remapOf: 7 } ), 7 );
	assert.equal( effectRecordAlias( { authoredStages: [] } ), undefined );
	for ( const bad of [ { remapOf: 0 }, { remapOf: 1.5 }, { remapOf: 7, authoredStages: [] } ] ) {
		assert.throws( () => effectRecordAlias( bad ), /alias/ );
	}
	const original = { stages: 1 };
	const resolved = resolveEffectRecordAliases( { 1: { stages: 1 }, 2: { remapOf: 1 } }, { 1: original } );
	assert.equal( resolved[2], original );
	assert.throws(
		() => resolveEffectRecordAliases( { 1: {}, 2: { remapOf: 1 }, 3: { remapOf: 2 } }, { 1: {} } ),
		/alias target/
	);
	assert.throws( () => resolveEffectRecordAliases( { 2: { remapOf: 9 } }, {} ), /alias target/ );
});

test("the decoder decodes an original once and lends it to its aliases", () => {
	const catalog = createEffectDecoder().decode( encode( {
		1: { animTable2: [ "ANI_ATTACK1" ], authoredStages: [] },
		2: { remapOf: 1 }
	} ) );
	assert.ok( catalog[1] );
	assert.equal( catalog[2], catalog[1] );
	assert.throws( () => createEffectDecoder().decode( encode( { 2: { remapOf: 1 } } ) ), /alias target/ );
});

test("every levelled wolf attack draws the first wolf attack's swing", () => {
	const raw = readPublishedAssetBytesSync( "/assets/skill/effectRecords.json", CLIENT_PUBLIC_ROOT );
	const published = JSON.parse( new TextDecoder().decode( raw ) );
	for ( const id of LEVELLED_WOLF_ATTACKS ) {
		assert.deepEqual( published[id], { remapOf: ORIGINAL_WOLF_ATTACK }, String( id ) );
	}
	const catalog = createEffectDecoder().decode( raw );
	const original = catalog[ORIGINAL_WOLF_ATTACK];
	// The shot phase names the swing the client plays (a native motion role).
	assert.ok( (original?.phaseClips?.[2]?.length ?? 0) > 0 );
	for ( const id of LEVELLED_WOLF_ATTACKS ) assert.equal( catalog[id], original, String( id ) );
});
