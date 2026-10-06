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
const { expandTextRuns } = await import( "../../src/engine/foundation/rendering/text-run.ts" );
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
A path ending in a held suffix stays in flight until deliver releases it,
as a slow image decode does in the browser.
================
*/
/** @param {readonly string[]} [held] */
function createFixture( held = [] ) {
	/** @type {Map<number, import('../../src/engine/contracts/assets').AssetResult>} */
	const pending = new Map();
	/** @type {Map<number, string>} */
	const inFlight = new Map();
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
				if ( held.some( suffix => path.endsWith( suffix ) ) ) {
					inFlight.set( id, path );
					return id;
				}
				load( id, path );
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
				inFlight.delete( id );
			}
		},
		command => commands.push( command ),
		// Recorded as drawn: text runs expanded into their glyph quads (text-run.ts).
		next => {
			scene = next && { ...next, quads: expandTextRuns( next.quads ) };
		},
		() => {},
		"https://fixture.invalid/",
		"https://fixture.invalid/"
	);
	/*
	================
	load

	Complete one request from the generated retail catalog.
	================
	*/
	/** @param {number} id @param {string} path */
	function load( id, path ) {
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
	}
	/*
	================
	deliver

	Complete every held request.
	================
	*/
	function deliver() {
		for ( const [id, path] of inFlight ) load( id, path );
		inFlight.clear();
	}
	/*
	================
	step

	Advance enough frames to finish dependent retail resource requests. inspect
	sees every publication, including the frames a resource is still in flight.
	================
	*/
	/** @param {( semantics: import('../../src/engine/contracts/ui').UiSemantics ) => void} [inspect] */
	function step( inspect ) {
		for ( let i = 0; i < WARM_FRAMES; i++ ) {
			const published = ui.step( view, frame++ * FRAME_MS );
			if ( published ) inspect?.( published );
			semantics = published ?? semantics;
		}
		return semantics;
	}
	/*
	================
	setGame

	Publish a new immutable gameplay snapshot, as the simulation worker does.
	================
	*/
	/**
	 * @param {Partial<import('../../src/engine/contracts/gameplay').GameplayState>} patch
	 * @param {( semantics: import('../../src/engine/contracts/ui').UiSemantics ) => void} [inspect]
	 */
	function setGame( patch, inspect ) {
		assert.ok( view.gameplay );
		view = { ...view, gameplay: { ...view.gameplay, ...patch } };
		return step( inspect );
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
	return {
		ui,
		commands,
		step,
		setGame,
		setEntities,
		deliver,
		scene: () => scene,
		/*
  ================
  tick

  Advance simulation time without replacing inventory or forcing an input repaint.
  ================
  */
		tick( milliseconds ) {
			view = { ...view, simulationTimeMs: milliseconds };
			return ui.step( view, milliseconds );
		}
	};
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
			// CIFPartyMatch_RefreshButtons: a partyless creator needs level 5.
			progression: { level: 10, masteries: [] },
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

test("matching filter frames paint above the enclosing background and retain native colors", () => {
	const f = createFixture();
	try {
		f.ui.event( { kind: "activate", id: "open-window:Party Matching" } );
		const semantics = f.step();
		const quads = f.scene()?.quads ?? [];
		const name = semantics?.controls.find( c => c.id === "party-search-name" );
		assert.ok( name );
		let background = -1;
		for ( const [index, quad] of quads.entries() ) {
			if (
				quad.texture.endsWith( "/com_bg_tile_b.png" ) &&
				quad.rect[0] <= name.rect[0] && quad.rect[1] <= name.rect[1] &&
				quad.rect[0] + quad.rect[2] > name.rect[0] &&
				quad.rect[1] + quad.rect[3] > name.rect[1]
			) background = index;
		}
		assert.ok( background >= 0, "the enclosing filter background must exist" );
		for ( const id of [ "party-search-name", "party-search-min", "party-search-max" ] ) {
			const control = semantics?.controls.find( c => c.id === id );
			assert.ok( control );
			const frame = quads.findIndex( q =>
				q.texture.includes( "/com_blacksquare_" ) &&
				q.rect[0] >= control.rect[0] - 8 && q.rect[0] <= control.rect[0] &&
				q.rect[1] >= control.rect[1] - 8 && q.rect[1] <= control.rect[1]
			);
			assert.ok( frame > background, `${id} border must remain above the background` );
		}
		assert.ok( hasText( f.scene(), "~" ), "native initialization supplies the range separator" );
		assert.ok(
			quads.some( q =>
				q.texture === fontAtlas.image &&
				q.color[0] === 239 / 255 && q.color[1] === 218 / 255 && q.color[2] === 164 / 255
			),
			"authored filter captions retain their gold color"
		);
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
	// The mark rides the name board, whose range is measured from the local player.
	const local = {
		gid: 1,
		refObjId: 1907,
		kind: "local-player",
		name: "Local",
		regionId: 1,
		x: 0,
		y: 0,
		z: 0,
		heading: 0
	};
	try {
		f.setEntities( [ local, monster ] );
		const mark = f.scene()?.quads.find( q => q.texture.endsWith( "/europe_partymob.png" ) );
		assert.ok( mark, "the native auxiliary flag admits the generated icon resource" );
		assert.equal( mark.characterAnchor, monster.gid );
		assert.deepEqual( mark.rect.slice( 2 ), [ 16, 16 ] );
		f.setEntities( [ local, { ...monster, rarityAuxIcon: 0 } ] );
		assert.equal( f.scene()?.quads.some( q => q.texture.endsWith( "/europe_partymob.png" ) ), false );
		// Natively the icon pass (85F5FD) outlives the 300-unit name range. Owner's
		// call: an icon never shows alone, so beyond the range both show.
		f.setEntities( [ local, { ...monster, x: 400 } ] );
		const far = f.scene()?.quads ?? [];
		assert.equal( far.some( q => q.texture.endsWith( "/europe_partymob.png" ) ), true );
		assert.ok(
			far.some( q => q.characterAnchor === monster.gid && q.texture === "" ),
			"the far party monster's name board is drawn with its mark"
		);
	} finally {
		f.ui.dispose();
	}
});

test("a companion inventory never replays the standalone inventory's close identity", () => {
	const icon = "icon/item/etc/all_potion_01.png", f = createFixture( [ icon ] );
	const pet = { gid: 7, refObjId: 100, band: 4, hp: 100, mp: 0, status: 0, dead: false };
	const potion = {
		slot: 13,
		refObjId: 4,
		typeFlags: 0,
		quantity: 10,
		plus: 0,
		durability: 0,
		variance: "0",
		magic: [],
		icon: "item/etc/all_potion_01.ddj"
	};
	/** @param {import('../../src/engine/contracts/ui').UiSemantics} semantics */
	const unique = semantics => {
		const ids = semantics.controls.map( c => c.id );
		assert.deepEqual( ids.filter( ( id, i ) => ids.indexOf( id ) !== i ), [], "control identities are unique" );
	};
	try {
		f.setGame( { cosRecords: [ pet ], inventorySlotCount: 32 } );
		f.ui.event( { kind: "activate", id: "open-window:Inventory" } );
		assert.equal( f.step()?.controls.filter( c => c.id === "close" ).length, 1 );
		// The service window admits while a new item's icon is still in flight,
		// so the companion inventory beside it is not yet admitted. Its retained
		// fallback must be its own window, never the standalone inventory, whose
		// close identity the service window now owns.
		f.ui.event( { kind: "activate", id: "open-window:COS inventory" } );
		const waiting = f.setGame( { inventory: [ potion ] }, unique );
		assert.equal( waiting?.controls.filter( c => c.id === "close" ).length, 1 );
		f.deliver();
		const service = f.step( unique );
		assert.equal( service?.controls.filter( c => c.id === "close" ).length, 1 );
		assert.ok( service?.controls.some( c => c.id === "companion-close" ) );
		assert.ok( service?.controls.some( c => c.id === "slot:13" ) );
	} finally {
		f.ui.dispose();
	}
});

/*
================
SOX inventory publication

Exercise the real bag/equipment painter: helper-only tests missed its absent
call to the animated overlay owner.
================
*/
test("SOX bag and equipped icons publish advancing sparkle quads and stop when closed", () => {
	const f = createFixture();
	const sparkle = "/assets/images/Media_extracted/icon/item/etc/icon_edge_rare.png";
	try {
		f.setGame( {
			inventorySlotCount: 45,
			equipmentSlotCount: 13,
			inventory: [ 6, 35 ].map( slot => ({
				slot,
				refObjId: 4161,
				name: "Bronz Bow",
				icon: "item/china/weapon/bow_02.ddj",
				typeFlags: 13100,
				quantity: 1,
				plus: 0,
				durability: 53,
				variance: "0",
				magic: [],
				tooltip: { fields: { rarity: 2, itemClass: 4 } }
			}) )
		} );
		f.ui.event( { kind: "key", code: "KeyI" } );
		f.step();
		const first = f.scene()?.quads.filter( q => q.texture === sparkle );
		assert.equal( first?.length, 2, "both the bag and worn socket draw the SOX overlay" );
		f.tick( 10000 );
		const before = f.scene()?.quads.filter( q => q.texture === sparkle ).map( q => q.uv );
		f.tick( 10040 );
		const after = f.scene()?.quads.filter( q => q.texture === sparkle ).map( q => q.uv );
		assert.notDeepEqual( before, after, "simulation time alone advances the visible frame" );
		f.ui.event( { kind: "key", code: "KeyI" } );
		f.step();
		assert.equal( f.scene()?.quads.filter( q => q.texture === sparkle ).length, 0 );
	} finally {
		f.ui.dispose();
	}
});

test("merchant offers and buyback use their item instances for rare sparkle", () => {
	const f = createFixture();
	const sparkle = "/assets/images/Media_extracted/icon/item/etc/icon_edge_rare.png";
	const bow = {
		slot: 13,
		refObjId: 4161,
		name: "Bronz Bow",
		icon: "item/china/weapon/bow_02.ddj",
		typeFlags: 13100,
		quantity: 1,
		plus: 0,
		durability: 53,
		variance: "0",
		magic: [],
		tooltip: { fields: { rarity: 2, itemClass: 4 } }
	};
	const shop = {
		npc: 42,
		name: "Merchant",
		offers: [ {
			tab: 0,
			slot: 0,
			refObjId: 4161,
			name: "Bronz Bow",
			icon: bow.icon,
			price: "100",
			maxStack: 1,
			items: [ bow ]
		} ],
		buyback: [ {
			index: 0,
			id: 1,
			refObjId: 4161,
			name: "Bronz Bow",
			icon: bow.icon,
			price: "50",
			quantity: 1,
			plus: 0,
			item: bow
		} ]
	};
	try {
		f.setGame( { target: 42, inventorySlotCount: 45 } );
		f.ui.event( { kind: "activate", id: "shop-open" } );
		f.setGame( { shop, shopCompletionRevision: 1 } );
		const controls = f.step()?.controls;
		assert.ok( controls?.some( c => c.id === "shop-offer:0" ) );
		assert.ok( controls?.some( c => c.id === "shop-buyback:0" ) );
		assert.equal( f.scene()?.quads.filter( q => q.texture === sparkle ).length, 2 );
		f.tick( 20000 );
		const before = f.scene()?.quads.filter( q => q.texture === sparkle ).map( q => q.uv );
		f.tick( 20040 );
		assert.notDeepEqual( f.scene()?.quads.filter( q => q.texture === sparkle ).map( q => q.uv ), before );
		const ordinary = { ...bow, tooltip: { fields: { rarity: 0, itemClass: 4 } } };
		f.setGame( {
			shop: {
				...shop,
				offers: [ { ...shop.offers[0], items: [ ordinary ] } ],
				buyback: [ { ...shop.buyback[0], item: ordinary } ]
			}
		} );
		assert.equal( f.scene()?.quads.filter( q => q.texture === sparkle ).length, 0 );
	} finally {
		f.ui.dispose();
	}
});
