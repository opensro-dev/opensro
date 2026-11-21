/*
===========================================================================

skill-recall.test.mjs - the skill reset scroll's client half

Port-only: the server's control 21 carries the kept skills and the reset
masteries after a skill reset scroll's use; the world gate admits it,
the decoder holds it to its shape, and gameplay applies it at once:
masteries at 0, the forgotten skills gone with their quick slots. Both
reset scrolls (3/3/13/13, CanUse 129) ask the native mall-use question
before their use.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { skillRecallFrame } = await import( "../../src/engine/foundation/gameplay/skill-recall.ts" );
const { isWorldControl, SKILL_RECALL_CONTROL } = await import(
	"../../src/engine/foundation/gameplay/commerce-controls.ts"
);
const { isMallUseConfirmItem } = await import( "../../src/engine/foundation/gameplay/mall-use.ts" );
const { createGameplay } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts"
);
const { defined } = await import( "../helpers/defined.mjs" );

/*
================
body
================
*/
function body( value ) {
	return new TextEncoder().encode( JSON.stringify( value ) );
}

test("the world gate admits control 21", () => {
	assert.equal( SKILL_RECALL_CONTROL, 21 );
	assert.equal( isWorldControl( 21 ), true );
});

test("the decoder holds the control to its shape", () => {
	const ok = skillRecallFrame( body( { version: 1, skills: [ 1 ], masteries: [ { id: 257, level: 0 } ] } ) );
	assert.deepEqual( ok, { skills: [ 1 ], masteries: [ { id: 257, level: 0 } ] } );
	assert.deepEqual( skillRecallFrame( body( { version: 1, skills: [], masteries: [] } ) ), {
		skills: [],
		masteries: []
	} );
	for (
		const bad of [
			{ version: 2, skills: [], masteries: [] },
			{ version: 1, skills: [ 1.5 ], masteries: [] },
			{ version: 1, skills: [ 0 ], masteries: [] },
			{ version: 1, skills: [ 1, 1 ], masteries: [] },
			{ version: 1, skills: [], masteries: [ { id: 257, level: -1 } ] },
			{ version: 1, skills: [], masteries: [ { id: 0, level: 0 } ] },
			{ version: 1, skills: [], masteries: Array.from( { length: 257 }, ( _, i ) => ({ id: i + 1, level: 0 }) ) },
			{ version: 1, masteries: [] }
		]
	) assert.throws( () => skillRecallFrame( body( bad ) ), undefined, JSON.stringify( bad ) );
	assert.throws( () => skillRecallFrame( Uint8Array.of( 0x7b ) ) );
});

test("both reset scrolls ask the native mall-use question first", () => {
	const type = (13 << 11) | (13 << 7) | (3 << 5) | (3 << 2);
	assert.equal( isMallUseConfirmItem( { typeFlags: type, nativeFields: { canUse: 129 } } ), true );
});

test("gameplay applies a reset: masteries at 0, forgotten skills and their slots gone", () => {
	const g = createGameplay( () => {} );
	g.bootstrap( {
		character: {
			name: "Author",
			skills: [ 12, 20, 1 ],
			quickSlots: [ { slot: 0, kind: 0x49, payload: 12 }, { slot: 1, kind: 0x49, payload: 1 }, {
				slot: 2,
				kind: 0x49,
				payload: 20
			} ],
			masteries: [ { id: 257, level: 6 }, { id: 258, level: 0 } ]
		}
	} );
	g.skillRecall( { skills: [ 1 ], masteries: [ { id: 257, level: 0 }, { id: 258, level: 0 } ] } );
	const state = defined( g.take() );
	assert.deepEqual( state.skills, [ 1 ] );
	assert.deepEqual( state.quickSlots, [ { slot: 1, kind: 0x49, payload: 1 } ] );
	assert.deepEqual( state.progression.masteries, [ { id: 257, level: 0 }, { id: 258, level: 0 } ] );
	g.dispose();
});
