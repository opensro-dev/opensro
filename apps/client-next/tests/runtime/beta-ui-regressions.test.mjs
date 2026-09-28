/*
===========================================================================

beta-ui-regressions.test.mjs - exercise window admission and party commands

Drive the retained UI through its public events and published snapshots.
Asset requests read the generated retail catalog, so these cases cover the
same window definitions and localized strings used by the running client.

===========================================================================
*/

import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import test from "node:test";
import { readFileSync } from "node:fs";

const { createUi } = await import( "../../src/engine/runtime/ui/ui.ts" );
const { emptySocial } = await import( "../../src/engine/foundation/gameplay/social.ts" );
const WARM_FRAMES = 24;
const FRAME_MS = 100;
const fontAtlas = JSON.parse(
	readFileSync( "../../.generated/client-public/assets/fonts/native-ui-font-atlas.json", "utf8" )
);

/*
================
hasText

Inspect the published glyph run rather than private retained text state.
================
*/
/** @param {import('../../src/engine/contracts/ui').UiScene | null} scene @param {string} value */
function hasText( scene, value ) {
	const font = fontAtlas.fonts["0"];
	const pattern = Array.from( value, character => {
		const glyph = font.glyphs[character.codePointAt( 0 ) ?? 63];
		return [
			glyph.x / fontAtlas.atlasWidth,
			glyph.y / fontAtlas.atlasHeight,
			glyph.width / fontAtlas.atlasWidth,
			glyph.height / fontAtlas.atlasHeight
		].join( "," );
	} );
	const actual = scene?.quads.filter( q => q.texture === fontAtlas.image ).map( q => q.uv.join( "," ) ) ?? [];
	return actual.some( ( _, start ) => pattern.every( ( uv, offset ) => actual[start + offset] === uv ) );
}

/*
================
createFixture

Keep resource delivery synchronous while preserving request/take ownership.
The UI still advances its own loading and publication lifecycles normally.
================
*/
function createFixture() {
	/** @type {Map<number, import('../../src/engine/contracts/assets').AssetResult>} */
	const pending = new Map();
	/** @type {import('../../src/engine/contracts/session').SessionCommand[]} */
	const commands = [];
	/** @type {import('../../src/engine/contracts/ui').UiScene | null} */
	let scene = null;
	/** @type {import('../../src/engine/contracts/ui').UiSemantics | null} */
	let semantics = null;
	let requestId = 0, frame = 0;
	/** @type {import('../../src/engine/contracts/ui').UiView} */
	let view = {
		session: { phase: "world", revision: 1, character: "Player" },
		gameplay: {
			revision: 1,
			localGid: 1,
			pose: null,
			authoritativePose: null,
			pendingMoves: 0,
			acknowledgedMove: 0,
			target: 0,
			targetPending: 0,
			inventory: [],
			inventoryPending: false,
			vitals: [],
			casts: [],
			error: null
		},
		entities: [ {
			gid: 1,
			refObjId: 1,
			kind: "player",
			regionId: 1,
			x: 0,
			y: 0,
			z: 0,
			heading: 0,
			name: "Player"
		} ],
		width: 1600,
		height: 900,
		worldReady: true
	};
	const ui = createUi(
		{
			available: () => 8,
			/*
================
request

Missing optional assets complete as errors instead of leaving a request live.
================
		*/
			request( url ) {
				const id = ++requestId, path = decodeURIComponent( new URL( url ).pathname );
				try {
					const bytes = readFileSync( "../../.generated/client-public" + path );
					if ( path.endsWith( ".png" ) ) {
						pending.set( id, {
							kind: "image",
							id,
							image: {
								width: bytes.readUInt32BE( 16 ),
								height: bytes.readUInt32BE( 20 ),
								/*
================
close

The fixture owns metadata only; no browser bitmap needs releasing.
================
						*/
								close() {}
							}
						} );
					} else pending.set( id, { kind: "bytes", id, buffer: Uint8Array.from( bytes ).buffer } );
				} catch {
					pending.set( id, { kind: "error", id, error: "Fixture asset missing: " + path } );
				}
				return id;
			},
			/*
================
take

Each completion transfers once to its requesting resource owner.
================
		*/
			take( id ) {
				const result = pending.get( id ) ?? null;
				pending.delete( id );
				return result;
			},
			cancel: id => {
				pending.delete( id );
			}
		},
		command => commands.push( command ),
		next => {
			scene = next;
		},
		() => {},
		"https://fixture.invalid/",
		"https://fixture.invalid/"
	);
	/*
================
step

Advance enough frames to finish dependent retail resource requests.
================
	*/
	function step() {
		for ( let i = 0; i < WARM_FRAMES; i++ ) semantics = ui.step( view, frame++ * FRAME_MS ) ?? semantics;
		return semantics;
	}
	/*
================
setGame

Publish a new immutable gameplay snapshot, as the simulation worker does.
================
	*/
	/** @param {Partial<import('../../src/engine/contracts/gameplay').GameplayState>} patch */
	function setGame( patch ) {
		assert.ok( view.gameplay );
		view = { ...view, gameplay: { ...view.gameplay, ...patch } };
		return step();
	}
	/*
================
setEntities

Replace the visible entity snapshot without inventing UI-private name state.
================
	*/
	/** @param {import('../../src/engine/contracts/world').EntityState[]} entities */
	function setEntities( entities ) {
		view = { ...view, entities };
		return step();
	}
	step();
	return { ui, commands, step, setGame, setEntities, scene: () => scene };
}

test("pet window rejects empty and dead rosters and closes when its last pet disappears", () => {
	const f = createFixture();
	const pet = { gid: 7, refObjId: 100, band: 4, hp: 100, mp: 0, status: 0, dead: false };
	const controls = () => f.step()?.controls ?? [];
	const opened = () => controls().some( c => c.id.startsWith( "cos-tab:" ) || c.id.startsWith( "cos-slot:" ) );
	try {
		f.ui.event( { kind: "key", code: "KeyW" } );
		assert.equal( opened(), false );
		f.setGame( { cosRecords: [ { ...pet, dead: true } ] } );
		f.ui.event( { kind: "activate", id: "open-window:COS inventory" } );
		assert.equal( opened(), false );
		f.setGame( { cosRecords: [ pet ] } );
		f.ui.event( { kind: "key", code: "KeyW" } );
		assert.equal( opened(), true );
		f.setGame( { cosRecords: [ pet, { ...pet, gid: 8 } ] } );
		f.setGame( { cosRecords: [ { ...pet, gid: 8 } ] } );
		assert.equal( opened(), true, "another active pet keeps the shared window admitted" );
		f.setGame( { cosRecords: [] } );
		assert.equal( opened(), false );
	} finally {
		f.ui.dispose();
	}
});

test("party registration submits formation options even when an empty social snapshot exists", () => {
	const f = createFixture();
	try {
		f.setGame( {
			social: emptySocial(),
			partyMatching: {
				page: 0,
				pages: 1,
				rows: [],
				own: null,
				request: null,
				pending: null,
				result: null,
				auto: []
			}
		} );
		f.ui.event( { kind: "activate", id: "party-option:1" } );
		f.ui.event( { kind: "activate", id: "party-option:2" } );
		f.ui.event( { kind: "key", code: "KeyE" } );
		f.step();
		f.ui.event( { kind: "activate", id: "party-match:18" } );
		f.step();
		f.ui.event( { kind: "edit", id: "party-form-title", value: "Hunting", start: 7, end: 7, composing: false } );
		f.ui.event( { kind: "activate", id: "party-form-confirm" } );
		const command = f.commands.at( -1 );
		assert.ok( command?.kind === "gameplay" && command.command.kind === "party-match-register" );
		assert.equal( command.command.registration.type, 7 );
	} finally {
		f.ui.dispose();
	}
});

test("ground-drop warnings resolve through the same system catalog as their confirmation", () => {
	const f = createFixture();
	try {
		f.setGame( {
			inventorySlotCount: 45,
			equipmentSlotCount: 13,
			inventory: [ {
				slot: 13,
				refObjId: 1,
				typeFlags: 0x6c,
				quantity: 3,
				name: "Potion",
				plus: 0,
				durability: 0,
				variance: "0",
				magic: []
			} ]
		} );
		f.ui.event( { kind: "key", code: "KeyI" } );
		f.step();
		f.ui.event( { kind: "drag", id: "slot:13", dx: 100, dy: 0 } );
		f.ui.event( { kind: "drag-end", id: "slot:13", x: 700, y: 300 } );
		assert.ok( f.step()?.controls.some( c => c.id === "ground-drop-confirm" ) );
		assert.equal( hasText( f.scene(), "UIIT_MSG_DROP_WARNING_1" ), false );
		assert.equal( hasText( f.scene(), "UIIT_MSG_DROP_WARNING_2" ), false );
		assert.ok( hasText( f.scene(), "If the item is dropped, the ownership of the item will be gone" ) );
		assert.ok( hasText( f.scene(), "and other people will be able to grab it." ) );
		assert.deepEqual( f.commands, [], "showing localized warnings cannot submit a drop" );
	} finally {
		f.ui.dispose();
	}
});

test("party-monster mark is published beside its owner and removed when the spawn flag changes", () => {
	const f = createFixture();
	const monster = {
		gid: 2,
		refObjId: 10,
		kind: "monster",
		regionId: 1,
		x: 10,
		y: 0,
		z: 0,
		heading: 0,
		name: "Graesp",
		rarity: 4,
		rarityAuxIcon: 1
	};
	try {
		f.setEntities( [ monster ] );
		const mark = f.scene()?.quads.find( q => q.texture.endsWith( "/europe_partymob.png" ) );
		assert.ok( mark, "the native auxiliary flag admits the generated icon resource" );
		assert.equal( mark.characterAnchor, monster.gid );
		assert.deepEqual( mark.rect.slice( 2 ), [ 16, 16 ] );
		f.setEntities( [ { ...monster, rarityAuxIcon: 0 } ] );
		assert.equal( f.scene()?.quads.some( q => q.texture.endsWith( "/europe_partymob.png" ) ), false );
	} finally {
		f.ui.dispose();
	}
});
