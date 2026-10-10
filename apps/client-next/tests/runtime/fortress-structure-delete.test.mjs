/*
===========================================================================

fortress-structure-delete.test.mjs - the structure target window's delete

516BC0's eligibility (fortressDeleteAction), 517750's question and
517CA0's 0x71E1 {target, 0x16 or 0x17, fortress}, through the production
HUD: a barricade in a running war, held by the local guild.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { uiFixture } from "../helpers/ui-fixture.mjs";
const fortress = await import( "../../src/engine/foundation/gameplay/fortress.ts" );
const services = await import( "../../src/engine/foundation/gameplay/fortress-services.ts" );
const { emptySocial } = await import( "../../src/engine/foundation/gameplay/social.ts" );

const BARRICADE = 31;
const SELF = 5;
// TypeID 1/2/5/6: TID1..TID3 in the low word, TID4 from bit 11.
const BARRICADE_TID = 0x2c4 | (6 << 11);

test("516BC0 shows delete by window kind, war, holding and fortress role", () => {
	const allow = ( kind, context ) =>
		fortress.fortressDeleteAction( kind, { war: false, holder: true, role: 1, ownObject: false, ...context } );
	// A guard is dismissed outside the war by role 1 or 4.
	assert.deepEqual( [ 1, 2, 4, 8 ].map( role => allow( 1, { role } ) ), [ 0x16, 0, 0x16, 0 ] );
	assert.equal( allow( 1, { war: true } ), 0 );
	assert.equal( allow( 1, { holder: false } ), 0 );
	// A barricade is demolished during the war by role 1, 2 or 4.
	assert.deepEqual( [ 1, 2, 4, 8, 16 ].map( role => allow( 2, { war: true, role } ) ), [ 0x17, 0x17, 0x17, 0, 0 ] );
	assert.equal( allow( 2, { war: false } ), 0 );
	assert.equal( allow( 2, { war: true, holder: false } ), 0 );
	// A war object is dismissed during the war by the commander of its guild.
	assert.equal( allow( 3, { war: true, ownObject: true } ), 0x16 );
	assert.equal( allow( 3, { war: true, ownObject: false } ), 0 );
	assert.equal( allow( 3, { war: true, ownObject: true, role: 2 } ), 0 );
	assert.equal( allow( 0, { war: true } ), 0 );
	// 517CA0's request: target, action, the player's fortress.
	assert.deepEqual(
		[ ...services.fortressServiceRequest( { target: BARRICADE, action: 0x17, fortress: 1 } ).payload ],
		[ BARRICADE, 0, 0, 0, 0x17, 1, 0, 0, 0 ]
	);
});

/*
================
barricadeFixture

The local guild holds Jangan; its member has the given fortress role and
has selected a barricade. warFlags 1 is a running war.
================
*/
function barricadeFixture( t, role, warFlags ) {
	const sent = [];
	const f = uiFixture( message => {
		if ( message.kind === "gameplay" ) sent.push( message.command );
	} );
	t.after( () => f.dispose() );
	f.state.entities.push( {
		...f.state.entities[0],
		gid: BARRICADE,
		refObjId: 19565,
		kind: "structure",
		tidWord: BARRICADE_TID,
		name: "Barricade",
		maxHp: 80000
	} );
	Object.assign( f.state.gameplay, {
		target: BARRICADE,
		fortress: {
			...fortress.fortressBootstrap( {
				gameWorldData: [ { gameWorldId: 7, warName: "FORTRESS_JANGAN" } ],
				siegeFortressData: [ { fortressId: 1, codeName: "FORTRESS_JANGAN", nameStrId: "SN_FORTRESS_JANGAN" } ]
			} ),
			worldId: 7,
			wars: [ { id: 1, name: "Holders", flags: warFlags } ]
		},
		social: {
			...emptySocial( "Player" ),
			self: SELF,
			guild: {
				id: 9,
				name: "Holders",
				level: 3,
				gp: 0,
				subject: "",
				contents: "",
				crest: 0,
				members: [ { id: SELF, name: "Player", grade: 0, level: 60, permissions: 31, role } ]
			}
		}
	} );
	let now = 0, last = null;
	const step = () => {
		for ( let i = 0; i < 16; i++ ) last = f.ui.step( f.state, now += 100 ) ?? last;
		return last;
	};
	return { f, sent, step };
}

test("a barricade's delete asks, then demolishes it for the player's fortress", t => {
	const { f, sent, step } = barricadeFixture( t, 2, 1 );
	assert.ok( step().controls.some( c => c.id === "target-structure-remove" ), "no delete on a held barricade" );
	f.ui.event( { kind: "activate", id: "target-structure-remove" } );
	const asked = step();
	assert.ok( asked.controls.some( c => c.id === "structure-remove-confirm" ), "delete did not ask" );
	assert.equal( sent.filter( c => c.kind === "fortress-dismantle" ).length, 0, "delete sent before the answer" );
	f.ui.event( { kind: "activate", id: "structure-remove-confirm" } );
	assert.deepEqual( sent.at( -1 ), { kind: "fortress-dismantle", gid: BARRICADE, action: 0x17, fortress: 1 } );
});

test("the delete stays hidden outside the war and for other roles", t => {
	for ( const [role, flags] of [ [ 2, 0 ], [ 8, 1 ] ] ) {
		const { step } = barricadeFixture( t, role, flags );
		assert.ok(
			!step().controls.some( c => c.id === "target-structure-remove" ),
			`role ${role} with war flags ${flags} showed delete`
		);
	}
});
