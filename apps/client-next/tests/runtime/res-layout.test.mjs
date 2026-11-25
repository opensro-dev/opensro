/*
===========================================================================

res-layout.test.mjs - the res_ui layout window's chrome (#4245)

The quest list window drew only its slots on the live site: the frame
ring named "<stem>.png<part>.png" paths, the body tile's art sits in its
frame word, the ring has no interior and the caption was the archive's
Korean text. These tests draw a questlist-shaped layout into a stub
frame and read back the quads it produced.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import path from "node:path";
import { root } from "../../tools/project.mjs";

const load = relative => import( sourceFileUrl( path.join( root, relative ) ).href );
const { decodeResLayout, resLayoutFramePrefix } = await load( "src/engine/foundation/ui/res-layout.ts" );
const { createResLayoutWindow, RES_LAYOUT_WINDOW_BODY } = await load(
	"src/engine/runtime/ui/res-layout/res-layout.ts"
);

const IMAGES = "/assets/images/Media_extracted/";

const control = ( key, parent, type, rect, fields = {} ) => ({
	key,
	id: Number( key ),
	parent,
	children: [],
	type,
	className: "",
	rect,
	frame: "",
	texture: "",
	text: "",
	color: 4294967295,
	flags: 0,
	rootWord: 0,
	...fields
});

// The live questlist.json's chrome: root window, inner framed panel,
// body tile whose art is filed under its frame word.
const layout = decodeResLayout( {
	format: "res-ui-layout",
	version: 1,
	name: "questlist",
	root: "76",
	controls: [
		{
			...control( "76", null, 0, [ 198, 193, 624, 369 ], {
				frame: "interface/frame/mframe_wnd_",
				text: " 퀘스트 목록"
			} ),
			children: [ "2", "3" ]
		},
		control( "2", "76", 1, [ 209, 231, 603, 322 ], { frame: "interface/equipment/equip_window_" } ),
		control( "3", "76", 2, [ 236, 261, 547, 284 ], { frame: "interface/ifcommon/bg_tile/com_bg_tile_a.ddj" } )
	],
	order: [ "76", "2", "3" ]
} );

/*
================
stubFrame

Every image is resident at 8x8; text records the strings it was asked
to draw as one quad each.
================
*/
function stubFrame() {
	/** @type {string[]} */
	const drawn = [];
	/** @type {string[]} */
	const paths = [];
	/** @type {{ rect: number[]; texture: string; color: readonly number[]; }[]} */
	const quads = [];
	/** @type {unknown[]} */
	const controls = [];
	return {
		drawn,
		paths,
		quads,
		controls,
		full: [ 0, 0, 1024, 768 ],
		white: [ 1, 1, 1, 1 ],
		hover: null,
		pressed: null,
		resources: { has: () => true, size: () => [ 8, 8 ] },
		text: {
			quads( value, rect ) {
				drawn.push( value );
				return [ { rect, texture: "text", uv: [ 0, 0, 1, 1 ], color: [ 1, 1, 1, 1 ], clip: rect } ];
			}
		}
	};
}

test("frame prefix: the ring's parts are the stem's own files", () => {
	assert.equal( resLayoutFramePrefix( "interface/frame/mframe_wnd_" ), IMAGES + "interface/frame/mframe_wnd_" );
	assert.equal( resLayoutFramePrefix( "" ), "" );
	const frame = stubFrame();
	createResLayoutWindow( layout ).draw( frame, 0, 0 );
	const textures = new Set( frame.quads.map( q => q.texture ) );
	assert.ok( textures.has( IMAGES + "interface/frame/mframe_wnd_left_up.png" ) );
	assert.ok( textures.has( IMAGES + "interface/equipment/equip_window_mid_down.png" ) );
	assert.ok( ![ ...textures ].some( t => t.includes( ".png" ) && t.indexOf( ".png" ) !== t.length - 4 ) );
});

test("body: the root window lays a flat body under its ring, children do not", () => {
	const frame = stubFrame();
	createResLayoutWindow( layout ).draw( frame, 10, 20 );
	const bodies = frame.quads.filter( q => q.texture === "" );
	assert.equal( bodies.length, 1 );
	assert.deepEqual( bodies[0].rect, [ 10, 20, 624, 369 ] );
	assert.deepEqual( bodies[0].color, RES_LAYOUT_WINDOW_BODY );
	// Drawn before the ring, so the ring sits on top.
	assert.ok( frame.quads.indexOf( bodies[0] ) < frame.quads.findIndex( q => q.texture.includes( "mframe_wnd_" ) ) );
});

test("tile: art filed under the frame word still draws", () => {
	const frame = stubFrame();
	createResLayoutWindow( layout ).draw( frame, 0, 0 );
	const tile = IMAGES + "interface/ifcommon/bg_tile/com_bg_tile_a.png";
	assert.ok( frame.paths.includes( tile ) );
	assert.ok( frame.quads.some( q => q.texture === tile ) );
});

test("caption: the owner's text replaces the authored Korean", () => {
	const authored = stubFrame();
	const window = createResLayoutWindow( layout );
	window.draw( authored, 0, 0 );
	assert.deepEqual( authored.drawn, [ "퀘스트 목록" ] );
	window.setText( "76", "Quest list" );
	const replaced = stubFrame();
	window.draw( replaced, 0, 0 );
	assert.deepEqual( replaced.drawn, [ "Quest list" ] );
});
