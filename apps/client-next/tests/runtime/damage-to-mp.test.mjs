/*
===========================================================================
damage-to-mp.test.mjs - combat MP publication leaves HP feedback independent
Snow Shield uses ordinary timed effects and an MP-only combat refresh.
===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { createCombat } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/combat/combat.ts"
);
/*
================
mpRefresh
================
*/
function mpRefresh( mp ) {
	const packet = new Uint8Array( 11 ), view = new DataView( packet.buffer );
	view.setUint32( 0, 1, true );
	view.setUint16( 4, 4, true );
	packet[6] = 2;
	view.setUint32( 7, mp, true );
	return packet;
}
test("combat MP debit and depletion never create an HP event or damage popup", () => {
	const combat = createCombat();
	combat.seed( 1, { hp: 80, mp: 100, maxHp: 200, maxMp: 200 } );
	for ( const mp of [ 70, 1, 0, 0 ] ) {
		assert.equal( combat.receive( 0x33a6, mpRefresh( mp ), 100 ), true );
		const state = combat.state();
		assert.equal( state.vitals[0].hp, 80 );
		assert.equal( state.vitals[0].mp, mp );
		assert.deepEqual( state.environmentalDamage, [] );
	}
});
