/*
===========================================================================

party-map-hover.test.mjs - world map roster hover names (57CE80)

Exercise the HUD owner with the production projection: the inset hover
area over each party and academy sign, and the member's name drawn on the
sign's top edge right after that sign.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import test from "node:test";
import assert from "node:assert/strict";
const { experimentalOptions } = await import( "../../src/engine/foundation/ui/experimental-options.ts" );
const { createWorldMapHud } = await import( "../../src/engine/runtime/ui/hud/world-map.ts" );

const GLYPH_WIDTH = 6, GLYPH_HEIGHT = 12;

/*
================
fixture
================
*/
function fixture() {
	const pose = { regionId: 97 * 256 + 160, x: 960, y: 0, z: 960, angle: 0 };
	const game = {
		pose,
		social: {
			self: 1,
			localName: "Me",
			members: [
				{ id: 1, name: "Me", region: pose.regionId, x: 960, y: 0, z: 960, war: 65537 },
				{ id: 2, name: "Friend", region: pose.regionId, x: 1200, y: 0, z: 960, war: 65537 }
			]
		},
		academy: {
			member: true,
			members: [ { id: 3, name: "Pupil", regionId: pose.regionId, x: 600, y: 0, z: 960, war: 65537 } ]
		}
	};
	const hud = createWorldMapHud( {
		assets: { available: () => 0, request: () => 0, take: () => null, cancel: () => {} },
		base: "http://fixture.invalid",
		experimental: { state: () => ({ saved: experimentalOptions() }), restore: () => {} }
	} );
	/** @type {any} */
	const input = {
		next: { entities: [] },
		game,
		pose,
		panel: "Map",
		w: 1200,
		h: 900,
		full: [ 0, 0, 1200, 900 ],
		hover: null,
		hudData: { map: {} },
		trackedQuest: 0,
		minimapResources: { positions: () => undefined },
		mapTeleport: { view: () => {} },
		reverseScrollHud: { mapping: () => false },
		text: {
			path: () => null,
			run: ( /** @type {string} */ value ) => ({ width: value.length * GLYPH_WIDTH }),
			extentHeight: () => GLYPH_HEIGHT,
			quads: ( /** @type {string} */ value, /** @type {number[]} */ rect, /** @type {number[]} */ clip ) => [ {
				texture: "glyph:" + value,
				rect,
				uv: [ 0, 0, 1, 1 ],
				color: [ 1, 1, 1, 1 ],
				clip
			} ]
		},
		resources: { has: () => true, size: () => undefined },
		paths: [],
		quads: [],
		controls: [],
		hudCopy: ( /** @type {string} */ k ) => k,
		beginWindow: () => [ 0, 0, 0, 0 ],
		endWindow: () => {},
		windowBox: () => {},
		authoredButton: () => {},
		authoredLabeledButton: () => {},
		closeButton: () => {},
		image: () => {}
	};
	/*
	================
	paint

	Present one frame and return the roster hover areas it registered.
	================
	*/
	const paint = () => {
		input.paths = [];
		input.quads = [];
		input.controls = [];
		hud.present( input );
		return input.controls.filter( ( /** @type {any} */ c ) => c.id.startsWith( "map-roster:" ) );
	};
	/*
	================
	quadIndex
	================
	*/
	const quadIndex = ( /** @type {string} */ suffix ) =>
		input.quads.findIndex( ( /** @type {any} */ q ) => q.texture.endsWith( suffix ) );
	return { hud, input, paint, quadIndex };
}

test("each roster sign takes a hover area inset by 2 and no tooltip", () => {
	const { hud, input, paint, quadIndex } = fixture();
	try {
		const areas = paint();
		assert.deepEqual( areas.map( c => c.id ), [ "map-roster:apprenticeship:Pupil", "map-roster:party:Friend" ] );
		const sign = input.quads[quadIndex( "wmap_sign_party.png" )].rect;
		const party = areas[1];
		assert.deepEqual( party.rect, [ sign[0] + 2, sign[1] + 2, 12, 12 ] );
		assert.equal( party.helpText, undefined );
		assert.equal( party.kind, "region" );
		assert.equal( party.draggable, true );
		assert.equal( hud.surface( party.id ), true );
		assert.equal( quadIndex( "glyph:Friend" ), -1, "no name without hover" );
	} finally {
		hud.dispose();
	}
});

test("a hovered member's name sits centred on its sign's top edge, painted right after that sign", () => {
	const { hud, input, paint, quadIndex } = fixture();
	try {
		input.hover = "map-roster:party:Friend";
		paint();
		const signAt = quadIndex( "wmap_sign_party.png" ), sign = input.quads[signAt].rect;
		const labelAt = quadIndex( "glyph:Friend" ), width = "Friend".length * GLYPH_WIDTH;
		assert.equal( labelAt, signAt + 1 );
		assert.ok( quadIndex( "mm_sign_character.png" ) > labelAt, "the local player still paints over the name" );
		assert.deepEqual( input.quads[labelAt].rect, [
			Math.trunc( sign[0] + 8 - width / 2 ),
			Math.trunc( sign[1] - GLYPH_HEIGHT ),
			width,
			GLYPH_HEIGHT
		] );
		assert.equal( quadIndex( "glyph:Pupil" ), -1, "only the hovered member is named" );
		input.hover = "map-roster:apprenticeship:Pupil";
		paint();
		assert.equal( quadIndex( "glyph:Pupil" ), quadIndex( "wmap_sign_apprenticeship.png" ) + 1 );
		assert.equal( quadIndex( "glyph:Friend" ), -1 );
	} finally {
		hud.dispose();
	}
});

test("the name follows a live pose and disappears on leave or map close", () => {
	const { hud, input, paint, quadIndex } = fixture();
	try {
		input.hover = "map-roster:party:Friend";
		paint();
		const before = input.quads[quadIndex( "glyph:Friend" )].rect;
		input.next.entities = [ {
			gid: 2,
			kind: "player",
			name: "Friend",
			regionId: input.pose.regionId,
			x: 1800,
			y: 0,
			z: 960
		} ];
		paint();
		assert.notDeepEqual( input.quads[quadIndex( "glyph:Friend" )].rect, before );
		input.panel = "";
		assert.equal( paint().length, 0 );
		assert.equal( quadIndex( "glyph:Friend" ), -1 );
		input.panel = "Map";
		input.game.social.members = [];
		paint();
		assert.equal( quadIndex( "glyph:Friend" ), -1 );
	} finally {
		hud.dispose();
	}
});

test("roster hover areas pan with the map and clip to its visible area", () => {
	const { hud, input, paint } = fixture();
	try {
		hud.activate( "map-follow", input.pose );
		const before = paint()[1];
		hud.drag( { kind: "drag", id: before.id, dx: 20, dy: 10 } );
		const after = paint()[1];
		assert.deepEqual( after.rect, [ before.rect[0] + 20, before.rect[1] + 10, 12, 12 ] );
		const clip = input.controls.find( ( /** @type {any} */ c ) => c.id === "map-pan" ).rect;
		hud.drag( { kind: "drag", id: after.id, dx: clip[0] + clip[2] - after.rect[0] - 8, dy: 0 } );
		const edge = paint().find( c => c.id === after.id );
		assert.equal( edge.rect[0] + edge.rect[2], clip[0] + clip[2] );
		assert.equal( edge.rect[2], 8 );
		hud.drag( { kind: "drag", id: edge.id, dx: 20, dy: 0 } );
		assert.equal( paint().some( c => c.id === after.id ), false );
	} finally {
		hud.dispose();
	}
});
