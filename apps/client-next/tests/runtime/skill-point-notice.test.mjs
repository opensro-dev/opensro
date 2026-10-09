/*
===========================================================================

skill-point-notice.test.mjs - the 0x30B3 type 2 notify byte

CPSMission_OnPointUpdate30B3 (779DD9) prints a murderer's skill-point loss
on death, or a gain, only when the server sets the notify byte; a silent
update (training, withdrawal) prints nothing.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import path from "node:path";
import { root } from "../../tools/project.mjs";
const { skillPointNotice } = await import(
	sourceFileUrl( path.join( root, "src/engine/foundation/gameplay/progression.ts" ) ).href
);

/*
================
skillPoints

[u8 2][u32 skill points][u8 notify]
================
*/
function skillPoints( value, notify ) {
	const p = new Uint8Array( 6 );
	p[0] = 2;
	new DataView( p.buffer ).setUint32( 1, value, true );
	p[5] = notify;
	return p;
}

test("a notified loss prints the murderer skill-point message with the amount", () => {
	assert.deepEqual( skillPointNotice( 5000, skillPoints( 3800, 1 ) ), {
		key: "UIIT_MSG_JSERR_SINCE_YOU_DIE_IN_MURDERER_SP_DEPRIVED_BY_SERVER",
		value: 1200,
		nativeType: 0,
		banner: true
	} );
});

test("a notified gain or no change prints the recovery message", () => {
	assert.equal( skillPointNotice( 100, skillPoints( 150, 1 ) ).key, "UIIT_STT_SKILL_POINT_RECOVER_RESULT" );
	assert.equal( skillPointNotice( 100, skillPoints( 150, 1 ) ).value, 50 );
	assert.equal( skillPointNotice( 100, skillPoints( 100, 1 ) ).value, 0 );
});

test("a silent update, another type or an unknown balance prints nothing", () => {
	assert.equal( skillPointNotice( 5000, skillPoints( 3800, 0 ) ), null );
	assert.equal( skillPointNotice( undefined, skillPoints( 3800, 1 ) ), null );
	const gold = new Uint8Array( 10 );
	gold[0] = 1;
	gold[9] = 1;
	assert.equal( skillPointNotice( 5000, gold ), null );
});
