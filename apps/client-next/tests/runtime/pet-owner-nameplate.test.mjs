/*
===========================================================================

pet-owner-nameplate.test.mjs - companion owner rows and party colors

Exercise the published world glyphs for attack and pickup pets, including
owner colors, roster changes, visibility, and the quick-status stack.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import test from "node:test";
import { fontAtlas, uiFixture } from "../helpers/ui-fixture.mjs";

const { companionOwnerNameplate, overheadLayout } = await import(
	"../../src/engine/foundation/ui/overhead-layout.ts"
);
const { defaultGameOptions } = await import( "../../src/engine/foundation/gameplay/game-options.ts" );
const { emptySocial } = await import( "../../src/engine/foundation/gameplay/social.ts" );

const member = { id: 2, name: "Owner", model: 1907, level: 1, status: 0xaa, region: 1, x: 0, y: 0, z: 0, war: 0 };
const party = { ...emptySocial( "Player" ), leader: 2, members: [ member ] };
const partyColor = [ 98 / 255, 245 / 255, 177 / 255, 1 ];

/*
================
petFixture
================
*/
function petFixture( band ) {
	const fixture = uiFixture();
	const local = { ...fixture.state.entities[0], refObjId: 1907, kind: "local-player" };
	const owner = { ...local, gid: 2, kind: "player", name: "Owner", nameColor: 0xff62f5b1 };
	const pet = {
		...local,
		gid: 3,
		refObjId: 100,
		kind: "cos",
		name: band === 3 ? "Wolf" : "Monkey",
		tidWord: (band << 11) | 0x1c6,
		ownerGid: owner.gid,
		ownerName: owner.name,
		nameColor: 0xffffff55,
		maxHp: 100
	};
	const state = {
		...fixture.state,
		entities: [ local, owner, pet ],
		gameplay: {
			...fixture.state.gameplay,
			revision: 1,
			authoritativePose: null,
			pendingMoves: 0,
			acknowledgedMove: 0,
			targetPending: 0,
			inventoryPending: false,
			casts: [],
			error: null,
			social: party
		}
	};
	return { fixture, state, local, owner, pet };
}

/*
================
petGlyphs
================
*/
function petGlyphs( fixture ) {
	return fixture.scenes.at( -1 ).quads.filter( q => q.characterAnchor === 3 && q.texture === fontAtlas.image );
}

for ( const band of [ 3, 4 ] ) {
	test(`band ${band} draws the owner centered above its own name in the owner's party color`, () => {
		const { fixture, state, pet } = petFixture( band );
		try {
			fixture.ui.step( state, 0 );
			assert.ok( fixture.hasText( "Owner" ) );
			assert.ok( fixture.hasText( pet.name ) );
			const glyphs = petGlyphs( fixture );
			const ownerGlyphs = glyphs.filter( q => q.color.every( ( c, i ) => c === partyColor[i] ) );
			const petNameGlyphs = glyphs.filter( q => q.color[0] === 1 && q.color[1] === 1 && q.color[2] === 85 / 255 );
			assert.equal( ownerGlyphs.length, "Owner".length );
			assert.equal( petNameGlyphs.length, pet.name.length );
			assert.equal( petNameGlyphs[0].rect[1] - ownerGlyphs[0].rect[1], 20 );
			const backgrounds = fixture.scenes.at( -1 ).quads.filter( q =>
				q.characterAnchor === pet.gid && q.texture === "" && q.rect[3] > 1
			);
			assert.equal( backgrounds.length, 2 );
			for ( const board of backgrounds ) {
				assert.ok( Math.abs( board.rect[0] * 2 + board.rect[2] ) <= 1, "each row centers independently" );
			}

			state.entities = state.entities.map( e => e.gid === 2 ? { ...e, nameColor: 0xffffffff } : e );
			state.gameplay = { ...state.gameplay, social: { ...party, leader: 0, members: [] } };
			fixture.ui.step( state, 100 );
			assert.equal( petGlyphs( fixture ).filter( q => q.color.every( c => c === 1 ) ).length, "Owner".length );

			state.entities = state.entities.filter( e => e.gid !== 2 );
			state.gameplay = { ...state.gameplay, social: party };
			fixture.ui.step( state, 200 );
			assert.equal(
				petGlyphs( fixture ).filter( q => q.color.every( c => c === 1 ) ).length,
				"Owner".length,
				"without the owner's visible entity the spawn owner name stays, white even in a party"
			);
		} finally {
			fixture.dispose();
		}
	});
}

test("the owner row follows the pet's range, hover and ridden-name gates", () => {
	const { fixture, state, pet, owner } = petFixture( 3 );
	try {
		state.entities = [ state.entities[0], owner, { ...pet, x: 300 } ];
		fixture.ui.step( state, 0 );
		assert.equal( petGlyphs( fixture ).length, 0 );
		fixture.ui.step( { ...state, hoveredEntity: pet.gid }, 100 );
		assert.equal( petGlyphs( fixture ).length, pet.name.length + "Owner".length );
		state.entities = [ { ...state.entities[0], mountedOn: pet.gid }, owner, pet ];
		fixture.ui.step( state, 200 );
		assert.equal( petGlyphs( fixture ).length, 0 );
	} finally {
		fixture.dispose();
	}
});

test("Monster names off hides the owner row, even on a hovered pet", () => {
	const { fixture, state, pet } = petFixture( 3 );
	try {
		fixture.ui.event( { kind: "preferences", value: { ...defaultGameOptions(), monsterNames: false } } );
		fixture.ui.step( state, 0 );
		assert.equal( petGlyphs( fixture ).length, 0 );
		fixture.ui.step( { ...state, hoveredEntity: pet.gid }, 100 );
		assert.ok( fixture.hasText( pet.name ) );
		assert.equal( petGlyphs( fixture ).length, pet.name.length, "the hovered pet shows only its own name" );
	} finally {
		fixture.dispose();
	}
});

test("owner labels retain live special colors and require a companion with a named owner", () => {
	const { fixture, pet, owner } = petFixture( 4 );
	const options = defaultGameOptions();
	try {
		assert.deepEqual( companionOwnerNameplate( pet, { ...owner, nameColor: 0xffffd87a }, options )?.color, [
			1,
			216 / 255,
			122 / 255,
			1
		] );
		assert.deepEqual( companionOwnerNameplate( pet, undefined, options )?.color, [ 1, 1, 1, 1 ] );
		assert.deepEqual( companionOwnerNameplate( pet, { ...owner, gid: 99 }, options )?.color, [ 1, 1, 1, 1 ] );
		assert.equal( companionOwnerNameplate( pet, owner, { ...options, monsterNames: false } ), null );
		assert.equal( companionOwnerNameplate( { ...pet, ownerName: "" }, owner, options ), null );
		assert.equal( companionOwnerNameplate( { ...pet, kind: "monster" }, owner, options ), null );
	} finally {
		fixture.dispose();
	}
});

test("a guild soldier's owner row takes the owner guild colour and keeps its own when the owner is absent", () => {
	const { fixture, state, pet, owner } = petFixture( 5 );
	const options = defaultGameOptions();
	const label = /** @type { readonly [number, number, number, number] } */ ([ 102 / 255, 185 / 255, 1, 1 ]);
	try {
		// With the owner visible the guild label colour wins over the
		// owner's name colour (854B95..854BC5).
		assert.deepEqual( companionOwnerNameplate( pet, owner, options, label )?.color, label );
		assert.deepEqual( companionOwnerNameplate( pet, owner, options )?.color, partyColor );
		// Without a visible owner the white reset is skipped (854C0C):
		// the soldier keeps its own spawn colour.
		assert.deepEqual( companionOwnerNameplate( pet, undefined, options )?.color, [ 1, 1, 85 / 255, 1 ] );

		// Through the production UI the owner row follows the same two
		// branches (here the owner's label colour is the default guild row
		// colour, since the local player shares no guild with it).
		const state2 = {
			...state,
			entities: state.entities.map( e => e.gid === 2 ? { ...e, guildName: "Empire" } : e )
		};
		fixture.ui.step( state2, 0 );
		// The label colour covers the owner row (and the "[Empire]" guild
		// row); the owner's name colour must not appear anywhere on it.
		assert.ok(
			petGlyphs( fixture ).filter( q => q.color.every( ( c, i ) => c === label[i] ) ).length >= "Owner".length
		);
		assert.equal( petGlyphs( fixture ).filter( q => q.color.every( ( c, i ) => c === partyColor[i] ) ).length, 0 );
		fixture.ui.step( { ...state2, entities: state2.entities.filter( e => e.gid !== 2 ) }, 100 );
		// The own-colour set is now the pet's name plus the whole owner
		// row; no white reset survives.
		assert.equal(
			petGlyphs( fixture ).filter( q => q.color[0] === 1 && q.color[1] === 1 && q.color[2] === 85 / 255 ).length,
			pet.name.length + "Owner".length,
			"owner gone: the soldier's owner row keeps its own colour, not white"
		);
		assert.equal(
			petGlyphs( fixture ).filter( q => q.color.every( ( c, i ) => c === [ 1, 1, 1, 1 ][i] ) ).length,
			0
		);
	} finally {
		fixture.dispose();
	}
});

test("a guild soldier with a guildless visible owner takes the owner's name colour through the production UI", () => {
	const { fixture, state } = petFixture( 5 );
	try {
		// The owner stands visible with no guild name: natively there is
		// no guild record (854BB8), so the row keeps the owner's name
		// colour - the default guild row colour must not leak into it.
		fixture.ui.step( state, 0 );
		assert.equal(
			petGlyphs( fixture ).filter( q => q.color.every( ( c, i ) => c === partyColor[i] ) ).length,
			"Owner".length
		);
		assert.equal(
			petGlyphs( fixture ).filter( q => q.color.every( ( c, i ) => c === [ 102 / 255, 185 / 255, 1, 1 ][i] ) )
				.length,
			0
		);
	} finally {
		fixture.dispose();
	}
});

test("an owned attack pet's quick status stays above its owner row", () => {
	const { fixture, state, local, pet } = petFixture( 3 );
	try {
		const ownPet = { ...pet, ownerGid: local.gid, ownerName: local.name };
		const game = { ...state.gameplay, vitals: [ { gid: pet.gid, hp: 50, maxHp: 100 } ] };
		const layout = overheadLayout( ownPet, local, game, defaultGameOptions() );
		assert.equal( layout.statusY, -45 );
		assert.equal( layout.speechY, -45 );
		assert.equal( overheadLayout( { ...ownPet, ownerName: "" }, local, game, defaultGameOptions() ).statusY, -25 );
		assert.equal(
			overheadLayout( ownPet, local, game, { ...defaultGameOptions(), monsterNames: false } ).statusY,
			-25,
			"855C40 lifts the gauge only when it draws the owner"
		);
	} finally {
		fixture.dispose();
	}
});
