/*
===========================================================================

id-plates.test.mjs - the GM console's /id byte and its three readers

Case 137 (691671) flips the byte at 0xCCB5D8, which ships standing. Its
readers draw only while it stands:

- 85F5DC: CICharactor_RenderOverheadNameAndIndicators skips the name board
  (+0x124) and the kind-4 overhead pass (guild row, marks, companion owner,
  quick-status gauge) while it is clear; the +0x464 board (stall title,
  speech) at 85F609 still draws, from the unlifted anchor.
- 8D5856: CIDecoDamageEffect_Initialize spawns a hit's damage number (and
  8D4DD0's blocking sound) only while it stands, after the 200-unit or
  local-attacker gate.

The tests drive the production HUD's console and the character presenter.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { goldDropModels } from "../helpers/gold-drop-models.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { fontAtlas, uiFixture } from "../helpers/ui-fixture.mjs";

const { overheadLayout, withoutBoardPass } = await import( "../../src/engine/foundation/ui/overhead-layout.ts" );
const { defaultGameOptions } = await import( "../../src/engine/foundation/gameplay/game-options.ts" );
const { STALL_TITLE_MODE } = await import( "../../src/engine/foundation/gameplay/interaction-approach.ts" );

const GUILD_GID = 2, STALL_GID = 3;

/*
================
consoleFixture

The production HUD with the GM console open, a guild player and a stall
holder in name range, and the damage-number sink recorded.
================
*/
function consoleFixture() {
	const shown = [];
	const f = uiFixture(
		() => {},
		undefined,
		undefined,
		undefined,
		undefined,
		undefined,
		undefined,
		{ setDamageNumbers: value => shown.push( value ) }
	);
	const local = f.state.entities[0];
	f.state.entities = [
		local,
		{ ...local, gid: GUILD_GID, name: "Lacrimosa", guildName: "Keepers", x: 30 },
		{
			...local,
			gid: STALL_GID,
			name: "Merchant",
			x: -30,
			appearanceState: [ 1, 0, 0, 0, 0, 0, STALL_TITLE_MODE ],
			titleText: "Bargains"
		}
	];
	f.state.gameplay.eligibility = { gm: true };
	let now = 0;
	const step = () => {
		for ( let i = 0; i < 16; i++ ) f.ui.step( f.state, now += 100 );
	};
	const type = line => {
		f.ui.event( {
			kind: "edit",
			id: "gm-input",
			value: line,
			start: line.length,
			end: line.length,
			composing: false
		} );
		f.ui.event( { kind: "activate", id: "submit" } );
		step();
	};
	step();
	f.ui.event( { kind: "key", code: "Backquote", shift: true } );
	step();
	return { f, shown, type };
}

/*
================
anchoredGlyphs

The glyph quads drawn over one character in the last published scene.
================
*/
function anchoredGlyphs( f, gid ) {
	return f.scenes.at( -1 ).quads.filter( q => q.characterAnchor === gid && q.texture === fontAtlas.image );
}

test("/id clears the name board and the kind-4 rows; the stall title stays (85F5DC)", () => {
	const { f, shown, type } = consoleFixture();
	try {
		const standing = "Lacrimosa".length + "[Keepers]".length;
		assert.equal( anchoredGlyphs( f, GUILD_GID ).length, standing, "the byte ships standing: name and guild row" );
		assert.equal( anchoredGlyphs( f, STALL_GID ).length, "Merchant".length + "Bargains".length );
		type( "/id" );
		assert.equal( anchoredGlyphs( f, GUILD_GID ).length, 0, "neither the name board nor the guild row draws" );
		assert.equal(
			anchoredGlyphs( f, STALL_GID ).length,
			"Bargains".length,
			"the +0x464 board draws past the branch, without its name"
		);
		assert.deepEqual( shown, [ false ], "the character owner receives the cleared byte" );
		type( "/id" );
		assert.equal( anchoredGlyphs( f, GUILD_GID ).length, standing );
		assert.equal( anchoredGlyphs( f, STALL_GID ).length, "Merchant".length + "Bargains".length );
		assert.deepEqual( shown, [ false, true ] );
	} finally {
		f.dispose();
	}
});

test("without the kind-4 pass no row lifts the stall title or speech", () => {
	const local = {
		gid: 1,
		refObjId: 1907,
		kind: "local-player",
		name: "Me",
		heading: 0,
		regionId: 257,
		x: 0,
		y: 0,
		z: 0
	};
	// Only the fields overheadLayout reads; the rest of the state is absent.
	const game =
		/** @type {import("../../src/engine/contracts/gameplay.ts").GameplayState} */ (/** @type {unknown} */ ({
			localGid: 1,
			vitals: [ { gid: 2, hp: 50, maxHp: 100, mp: 50, maxMp: 100 } ],
			social: { members: [] }
		}));
	const options = defaultGameOptions();
	const stall = {
		...local,
		gid: 2,
		kind: "player",
		name: "Merchant",
		guildName: "Keepers",
		appearanceState: [ 1, 0, 0, 0, 0, 0, STALL_TITLE_MODE ],
		titleText: "Bargains"
	};
	const standing = overheadLayout( stall, local, game, options );
	assert.equal( standing.guildText, "[Keepers]" );
	assert.ok( standing.stallY < -20, "standing, the guild row lifts the stall title" );
	const cleared = withoutBoardPass( standing );
	assert.equal( cleared.guildText, "" );
	assert.equal( cleared.status, null );
	assert.equal( cleared.fortressMark, null );
	assert.equal( cleared.fortressCrest, null );
	assert.equal( cleared.stallText, "Bargains" );
	assert.equal( cleared.stallY, -20 );
	assert.equal( cleared.speechY, -20 );
	const plain = withoutBoardPass(
		overheadLayout( { ...stall, appearanceState: [ 1 ], titleText: "" }, local, game, options )
	);
	assert.equal( plain.speechY, 0, "speech rises from the bare anchor" );
});

/*
================
presenterFixture

The character presenter with one authored damage event (combat-presentation
fixture shape): the local player (gid 1) and a monster (gid 2) trade a hit.
================
*/
async function presenterFixture() {
	const { createCharacterPresentation } = await import( "../../src/engine/runtime/characters/characters.ts" );
	const { createPresentationRandom } = await import( "../../src/engine/runtime/random/random.ts" );
	const clips = [ "stand", "attack1", "hit1", "death" ],
		metadata = Object.fromEntries(
			clips.map(
				name => [ name, {
					durationMs: 1000,
					soundEvents: [],
					trackEvents: name === "attack1" ? [ { cursorMs: 100, eventCode: 1, param0: 0, param1: 0 } ] : []
				} ]
			)
		);
	const catalog = {
		7: {
			phaseClips: [ [], [], [ "attack1" ] ],
			clips: [ "attack1" ],
			stages: [ { phase: "SHOT", startEvent: 1, damageEvent: true } ]
		}
	};
	const pending = new Map();
	let serial = 0;
	const bytes = value => ({ kind: "bytes", buffer: new TextEncoder().encode( JSON.stringify( value ) ).buffer });
	const manifest = refObjId => ({
		refObjId,
		codename: "NPC_" + refObjId,
		glb: `/assets/${refObjId}.glb`,
		clips,
		animationStates: metadata
	});
	const assets = {
		available: () => 4,
		request( url, limit, decode ) {
			pending.set( ++serial, { url, decode } );
			return serial;
		},
		cancel( id ) {
			pending.delete( id );
		},
		take( id ) {
			const job = pending.get( id );
			if ( !job ) return null;
			pending.delete( id );
			if ( job.decode === "effects" ) return { kind: "effects", catalog };
			if ( job.url.endsWith( "/skillfx/manifest.json" ) ) {
				return bytes( { format: "sro-skill-stage-models", models: {} } );
			}
			if ( job.url.endsWith( "/itemdrop/manifest.json" ) ) {
				return bytes( { format: "sro-mission-itemdrop-models", models: goldDropModels() } );
			}
			if ( job.decode === "character" ) {
				return {
					kind: "character",
					images: [],
					model: {
						nodes: [ {
							name: "root",
							parent: -1,
							translation: [ 0, 0, 0 ],
							rotation: [ 0, 0, 0, 1 ],
							scale: [ 1, 1, 1 ]
						} ],
						primitives: [],
						images: [],
						clips: clips.map( name => ({ name, duration: 1, channels: [] }) )
					}
				};
			}
			if ( job.url.endsWith( "/npc/manifest.json" ) ) return bytes( { models: [ 1, 2 ].map( manifest ) } );
			return bytes( { models: {} } );
		}
	};
	const renderer = {
		setCharacterModel() {},
		setCharacterAssembly() {},
		retainCharacterModels() {},
		characterSocket: () => null,
		setCharacterActors() {}
	};
	const presenter = createCharacterPresentation(
		/** @type {Parameters<typeof createCharacterPresentation>[0]} */ (/** @type {unknown} */ (assets)),
		/** @type {Parameters<typeof createCharacterPresentation>[1]} */ (/** @type {unknown} */ (renderer)),
		"http://localhost",
		() => {},
		createPresentationRandom( 1 )
	);
	const entities =
		/** @type {import("../../src/engine/contracts/world.ts").EntityState[]} */ (/** @type {unknown} */ ([ 1, 2 ]
			.map( gid => ({
				gid,
				refObjId: gid,
				kind: gid === 1 ? "local-player" : "monster",
				appearanceState: [ 1, 0, 0 ],
				regionId: 257,
				x: gid * 10,
				y: 0,
				z: 0,
				heading: 0,
				movementMode: 0
			}) )));
	// Only the fields the presenter reads. exchange() replaces casts per hit
	// on this plain object; the presenter sees it through game.
	const state = {
		localGid: 1,
		pose: { regionId: 257, x: 10, y: 0, z: 0, angle: 0 },
		inventory: [],
		vitals: [],
		casts: /** @type {unknown[]} */ ([])
	};
	const game = /** @type {import("../../src/engine/contracts/gameplay.ts").GameplayState} */ (
		/** @type {unknown} */ (state)
	);
	for ( let i = 0; i < 30; i++ ) presenter.step( entities, game, i * .01, i * 10 );
	assert.equal( presenter.error(), null );
	/*
	================
	exchange

	Both bodies hit each other at the given second; returns the numbers
	spawned by it.
	================
	*/
	function exchange( at ) {
		state.casts = [ 1, 2 ].map( caster => ({
			token: caster + at * 10,
			caster,
			target: 3 - caster,
			skill: 7,
			damage: 9,
			fatal: false,
			receivedAtMs: at * 1000,
			impacts: [ { damage: 9, fatal: false, type: 0, flags: 0, secondaryAmount: 0 } ]
		}) );
		presenter.step( entities, game, at, at * 1000 );
		presenter.step( entities, game, at + .31, at * 1000 + 310 );
		return presenter.damageText().filter( row => row.started >= at ).length;
	}
	return { presenter, exchange };
}

test("a cleared /id byte spawns no hit damage number, even for the local attacker (8D5856)", async () => {
	const { presenter, exchange } = await presenterFixture();
	try {
		assert.equal( exchange( 1 ), 2, "standing, both hits spawn a number" );
		presenter.setDamageNumbers( false );
		assert.equal( exchange( 5 ), 0, "cleared, neither hit spawns a number" );
		presenter.setDamageNumbers( true );
		assert.equal( exchange( 9 ), 2 );
	} finally {
		presenter.dispose();
	}
});
