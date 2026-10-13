/*
===========================================================================

attached-effect-one-shot.test.mjs - tests for combat.ts and
attached-effects.ts: token-0 adds are one-shots, not held instances

The server sends a token-0 0xB419 for every heal, cure and resurrection
that lands (593F50 -> 59AF00). No B6A0 ever ends one, so the session must
let them go: a healer's adds may not fill the 2048-row list.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";
const { createCombat } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/combat/combat.ts"
);
const { MAX_ATTACHED_EFFECTS, ONE_SHOT_DELIVERY_MS } = await import(
	"../../src/engine/foundation/gameplay/attached-effects.ts"
);
const u32 = n => [ n & 255, n >>> 8 & 255, n >>> 16 & 255, n >>> 24 ];
const HEAL = 296;
const BUFF = 297;
/*
================
fixture
================
*/
function fixture() {
	const c = createCombat( () => undefined, () => {} );
	c.references(
		[ HEAL, BUFF ].map( id => ({
			id,
			status: false,
			effectRider: false,
			effectDurationMs: id === BUFF ? 60000 : 0,
			zeroEffectDuration: id === HEAL
		}) )
	);
	return {
		c,
		add( gid, skill, token, now ) {
			c.receive( 0xb419, Uint8Array.from( [ ...u32( gid ), ...u32( skill ), ...u32( token ) ] ), now );
		},
		rows() {
			return c.state().attachedEffects;
		}
	};
}
test("a healer's token-0 adds past the 2048-row bound never throw", () => {
	const f = fixture();
	f.add( 10, BUFF, 77, 0 );
	for ( let i = 0; i < MAX_ATTACHED_EFFECTS + 1000; i++ ) f.add( 11, HEAL, 0, i );
	const rows = f.rows();
	assert.ok( rows.length <= MAX_ATTACHED_EFFECTS );
	assert.ok( rows.some( row => row.token === 77 ), "the live buff instance was evicted" );
	assert.equal( defined( rows.at( -1 ) ).receivedAtMs, MAX_ATTACHED_EFFECTS + 999 );
});
test("a one-shot leaves after its delivery window, live instances stay", () => {
	const f = fixture();
	f.add( 10, BUFF, 77, 0 );
	f.add( 11, HEAL, 0, 0 );
	f.add( 12, HEAL, 0, ONE_SHOT_DELIVERY_MS - 1 );
	assert.equal( f.rows().filter( row => row.token === 0 ).length, 2 );
	f.add( 13, HEAL, 0, ONE_SHOT_DELIVERY_MS );
	assert.deepEqual(
		f.rows().map( row => [ row.gid, row.token ] ),
		[ [ 10, 77 ], [ 12, 0 ], [ 13, 0 ] ]
	);
});
test("a list full of live instances still refuses one more", () => {
	const f = fixture();
	for ( let token = 1; token <= MAX_ATTACHED_EFFECTS; token++ ) f.add( 10, BUFF, token, 0 );
	assert.throws( () => f.add( 11, HEAL, 0, 1 ), /Attached effect capacity exceeded/ );
});
