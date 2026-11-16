/*
===========================================================================

map-guide-tree.ts - the M window guide's folder tree

v1.188 builds the guide panel's list in code (worldmapguide.2dt holds
only the panel's frames): group rows on the guide's long index bar with
a toggle button, the open group's species on the long contents bar with
a lamp, eight rows at a time beside the stock vertical scrollbar. Every
offset here was matched to the pixel against the v1.188 screenshot of
an open guide (map1): rows at 14,37 of the panel on a 30 pitch, the
toggle at 3,3 of its row, the lamp at 34,7, the scrollbar's up arrow at
264,56 and its down arrow at 264,234. The scrollbar is the stock one
(CNIFTreeView's CNIFScrollBar: worldmapguide_renewal2.2dt names no scroll
art), drawn with the chat window's faces: the com_scroll_bar track, the
gold chat_arrow_up / _down arrows and the com_scroll_button thumb, which
drags. The owner's v1.696 screenshot of the Monster tab shows exactly
these; the bone-knot gd_scroll_* art (#3392) is not what the guide draws.
The wheel over the panel steps one row a notch (0xB48B20, issue #3515).
This module paints one frame of the tree and owns its scroll
(createGuideScroll); the map tools own which group is open and the rows
themselves.

===========================================================================
*/
import type { UiControl, UiQuad, UiRect } from "@/engine/contracts/ui";
import { containsPoint } from "@/engine/foundation/ui/hit-test";
import type { GlyphStyle } from "@/engine/foundation/rendering/ui-glyphs";
import type { GuideRow } from "@/engine/foundation/ui/worldmap-guide";

const GUIDE = "/assets/images/Media_extracted/interface/guide/";
const GROUP_BAR = GUIDE + "gd_long_index_open.png";
const LEAF_BAR = GUIDE + "gd_long_contents_event.png";
// The toggle's faces: right (closed, the art's own) and down (open).
const TOGGLE_CLOSED = GUIDE + "gd_index_button_close.png";
const TOGGLE_OPEN = GUIDE + "gd_index_button_open.png";
const LAMP = GUIDE + "gd_contents_lamp.png";
const INTERFACE = "/assets/images/Media_extracted/interface/";
const SCROLL_LINE = INTERFACE + "ifcommon/com_scroll_bar.png";
const SCROLL_UP = INTERFACE + "chattingwnd/chat_arrow_up";
const SCROLL_DOWN = INTERFACE + "chattingwnd/chat_arrow_down";
const SCROLL_THUMB = INTERFACE + "ifcommon/com_scroll_button";
// A button's faces: normal, hover, pressed.
const FACES = [ ".png", "_focus.png", "_press.png" ] as const;

/*
================
GUIDE_TREE_ART

Every image the tree paints, in every face: the group and contents bars,
the toggle's two states, the lamp, the scrollbar's track and the three
buttons' normal, hover and pressed faces. The shut M window demands it
(world-map.ts) so a guide drawer opens on the frame its tab or row is
pressed.
================
*/
export const GUIDE_TREE_ART: readonly string[] = [
	GROUP_BAR,
	LEAF_BAR,
	TOGGLE_CLOSED,
	TOGGLE_OPEN,
	LAMP,
	SCROLL_LINE,
	...[ SCROLL_UP, SCROLL_DOWN, SCROLL_THUMB ].flatMap( art => FACES.map( face => art + face ) )
];

const ROW_LEFT = 14, ROW_TOP = 37, ROW_PITCH = 30;
const ROW_WIDTH = 248, ROW_HEIGHT = 28;
export const GUIDE_TREE_ROWS = 8;
const TOGGLE: UiRect = [ 3, 3, 24, 24 ];
const LAMP_RECT: UiRect = [ 34, 7, 8, 16 ];
// The label stands centred on the bar right of the toggle's box (both
// bars leave their first 28 columns to the toggle or the lamp's slot).
const LABEL_LEFT = 28;
// The scrollbar's track: the up arrow sits above it, the down arrow
// below, the thumb's top travels its height (CIFVerticalScroll's
// layout), and the track fills the 16 px column.
const SCROLL_TRACK: UiRect = [ 264, 72, 16, 146 ];
const SCROLL_BUTTON = 16;
// A main row's id prefix, in both trees.
export const GUIDE_MAIN = "map-guide-main:";
const GUIDE_SCROLL_ID = "map-guide-scroll";
const GUIDE_SCROLL_UP = GUIDE_SCROLL_ID + "-up";
const GUIDE_SCROLL_DOWN = GUIDE_SCROLL_ID + "-down";
export const GUIDE_SCROLL_THUMB = GUIDE_SCROLL_ID + "-thumb";
// The thumb's travel in pixels, for turning a drag into rows.
const GUIDE_SCROLL_TRAVEL = SCROLL_TRACK[3];
// A hair under a whole row of drag still counts as one, so a drag across
// the whole travel reaches the last row.
const DRAG_ROW_SLACK = 1e-6;
// The badge's room at a row's right end (port-only instance badge).
const BADGE_WIDTH = 44;
const BADGE_TEXT = "Inst.";

// map1's gold group names, and sub_e6cb90's level colours: white
// within three levels, 0xffef8282 four to six above, 0xffff0000 more,
// 0xff80c0e5 below.
const COLORS = {
	group: [ 239 / 255, 218 / 255, 164 / 255, 1 ],
	leaf: [ 1, 1, 1, 1 ],
	high: [ 239 / 255, 130 / 255, 130 / 255, 1 ],
	far: [ 1, 0, 0, 1 ],
	low: [ 128 / 255, 192 / 255, 229 / 255, 1 ]
} as const;
const BADGE_COLOR = [ .96, .83, .55, 1 ] as const;
const WHITE = [ 1, 1, 1, 1 ] as const;
const FULL_UV: UiRect = [ 0, 0, 1, 1 ];

/*
================
GuideTreeRow

One row: a group (with its toggle) or a species under the open group.
================
*/
export interface GuideTreeRow {
	id: string;
	label: string;
	group: boolean;
	open?: boolean;
	lamp?: boolean;
	color: keyof typeof COLORS;
	badge?: boolean;
}

/*
================
treeRow

A guide row as the tree paints it.
================
*/
export function treeRow( row: GuideRow, prefix: string, open: number | null, picked: number | null ): GuideTreeRow {
	return {
		id: (row.main ? GUIDE_MAIN : prefix) + row.index,
		label: row.label,
		group: row.main,
		open: row.main ? row.index === open : undefined,
		lamp: !row.main && row.index === picked,
		color: row.color,
		badge: row.badge
	};
}

/*
================
GuideTree

One frame's tree: the guide panel's rect, the rows, the first shown
row, and the text shown when there are no rows.
================
*/
export interface GuideTree {
	panel: UiRect;
	full: UiRect;
	rows: readonly GuideTreeRow[];
	scroll: number;
	empty: string;
	hover: string | null;
	pressed: string | null;
	text: {
		run( value: string, style: number, font: number ): { width: number; };
		quads( value: string, rect: UiRect, clip: UiRect, color: UiQuad["color"], style?: GlyphStyle ): UiQuad[];
	};
	size( path: string ): readonly [number, number] | undefined;
}

/*
================
createGuideScroll

The tree's first shown row and every input that moves it: an arrow a
row, the thumb's drag by the rows its pixels stand for over the travel,
and the wheel over the open panel one row a notch whatever the delta
(v1.696 steps its bar by the line step on the delta's sign alone,
0xB48B20, issue #3515). The map tools report the panel's rect each
frame and the paint the rows; the position never passes the last page.
================
*/
export function createGuideScroll() {
	let first = 0, range = 0, carry = 0;
	let bounds: UiRect | null = null;
	const clamp = ( value: number ) => Math.max( 0, Math.min( range, value ) );
	return {
		first: () => first,
		reset() {
			first = 0;
			carry = 0;
		},
		// The open panel's rect, or null while it is shut.
		place( rect: UiRect | null ) {
			bounds = rect;
		},
		// The painted rows' count; the position clamps to what they leave.
		rows( count: number ) {
			range = Math.max( 0, count - GUIDE_TREE_ROWS );
			first = clamp( first );
		},
		// An arrow's click; false for other ids.
		activate( id: string ): boolean {
			if ( id !== GUIDE_SCROLL_UP && id !== GUIDE_SCROLL_DOWN ) return false;
			first = clamp( first + (id === GUIDE_SCROLL_DOWN ? 1 : -1) );
			return true;
		},
		// The thumb's drag; false for other ids.
		drag( id: string, dy: number ): boolean {
			if ( id !== GUIDE_SCROLL_THUMB ) return false;
			if ( range <= 0 ) return true;
			carry += (dy * range) / GUIDE_SCROLL_TRAVEL;
			const step = Math.trunc( carry + (carry > 0 ? DRAG_ROW_SLACK : -DRAG_ROW_SLACK) );
			carry -= step;
			const next = first + step;
			first = clamp( next );
			// Past either end the thumb holds there, and so does the drag:
			// pixels spent beyond the end are not owed back on the return.
			if ( next !== first ) carry = 0;
			return true;
		},
		// The wheel; false when it is not over the open panel.
		wheel( x: number, y: number, delta: number ): boolean {
			if ( !bounds || delta === 0 || !containsPoint( bounds, x, y ) ) return false;
			first = clamp( first + Math.sign( delta ) );
			carry = 0;
			return true;
		}
	};
}

/*
================
paintGuideTree

Paints the shown rows and the scrollbar. Returns the scroll clamped to
the rows, so the owner never keeps a position past the end.
================
*/
export function paintGuideTree(
	tree: GuideTree,
	out: { quads: UiQuad[]; controls: UiControl[]; paths: string[]; }
): number {
	const [left, top] = tree.panel;
	const range = Math.max( 0, tree.rows.length - GUIDE_TREE_ROWS );
	const scroll = Math.max( 0, Math.min( range, tree.scroll ) );
	const image = ( texture: string, rect: UiRect ) => {
		out.paths.push( texture );
		out.quads.push( { texture, rect, uv: FULL_UV, color: WHITE, clip: tree.full } );
	};
	out.paths.push( GROUP_BAR, LEAF_BAR, TOGGLE_CLOSED, TOGGLE_OPEN, LAMP, SCROLL_LINE );
	if ( tree.rows.length === 0 ) {
		const rect: UiRect = [ left + ROW_LEFT, top + ROW_TOP, ROW_WIDTH, ROW_HEIGHT ];
		out.quads.push( ...tree.text.quads( tree.empty, rect, tree.full, WHITE, { hAlign: 1, vAlign: 1 } ) );
	}
	tree.rows.slice( scroll, scroll + GUIDE_TREE_ROWS ).forEach( ( row, index ) => {
		const rect: UiRect = [ left + ROW_LEFT, top + ROW_TOP + index * ROW_PITCH, ROW_WIDTH, ROW_HEIGHT ];
		image( row.group ? GROUP_BAR : LEAF_BAR, rect );
		if ( row.group ) {
			image( row.open ? TOGGLE_OPEN : TOGGLE_CLOSED, [
				rect[0] + TOGGLE[0],
				rect[1] + TOGGLE[1],
				TOGGLE[2],
				TOGGLE[3]
			] );
		} else if ( row.lamp ) {
			image( LAMP, [ rect[0] + LAMP_RECT[0], rect[1] + LAMP_RECT[1], LAMP_RECT[2], LAMP_RECT[3] ] );
		}
		// The label keeps the whole bar right of the toggle: the badge
		// takes its room only while the name still fits beside it, and a
		// name wider than the bar ends in an ellipsis with the whole name
		// on hover, never cut through a letter.
		const room = ROW_WIDTH - LABEL_LEFT, width = tree.text.run( row.label, 0, 0 ).width;
		const badge = row.badge && width <= room - BADGE_WIDTH ? BADGE_WIDTH : 0;
		const label: UiRect = [ rect[0] + LABEL_LEFT, rect[1], room - badge, ROW_HEIGHT ];
		const fits = width <= label[2];
		out.quads.push(
			...tree.text.quads( row.label, label, tree.full, COLORS[row.color], {
				hAlign: 1,
				vAlign: 1,
				overflow: fits ? "clip" : "ellipsis"
			} )
		);
		if ( badge ) {
			const box: UiRect = [ rect[0] + ROW_WIDTH - badge, rect[1], badge, ROW_HEIGHT ];
			out.quads.push( ...tree.text.quads( BADGE_TEXT, box, tree.full, BADGE_COLOR, { hAlign: 1, vAlign: 1 } ) );
		}
		out.controls.push( {
			id: row.id,
			kind: "button",
			label: row.label,
			helpText: fits ? undefined : row.label,
			rect,
			selected: row.group ? row.open === true : row.lamp === true
		} );
	} );
	paintScrollbar( tree, range, scroll, out );
	return scroll;
}

/*
================
paintScrollbar

The stock bar: com_scroll_bar from the track's top to the down
arrow, the arrows above and below, and the thumb, whose top travels
the track as the scroll runs from the first row to the last.
================
*/
function paintScrollbar(
	tree: GuideTree,
	range: number,
	scroll: number,
	out: { quads: UiQuad[]; controls: UiControl[]; paths: string[]; }
) {
	const x = tree.panel[0] + SCROLL_TRACK[0], y = tree.panel[1] + SCROLL_TRACK[1];
	const height = SCROLL_TRACK[3] + SCROLL_BUTTON;
	// The track art stretches over the column, as chatScrollbar's does.
	out.quads.push( {
		texture: SCROLL_LINE,
		rect: [ x, y, SCROLL_BUTTON, height ],
		uv: FULL_UV,
		color: WHITE,
		clip: tree.full
	} );
	const button = ( id: string, art: string, label: string, rect: UiRect, draggable: boolean ) => {
		// The hover and press faces come with the later client's art
		// only (the v1.188 media has the plain face): one is asked for
		// while it applies and drawn once it is there, so a build without
		// it never holds the window for a face that will not come.
		const normal = art + FACES[0];
		const state = tree.pressed === id ? art + FACES[2] : tree.hover === id ? art + FACES[1] : normal;
		out.paths.push( normal );
		if ( state !== normal ) out.paths.push( state );
		const face = tree.size( state ) ? state : normal;
		out.quads.push( { texture: face, rect, uv: FULL_UV, color: WHITE, clip: tree.full } );
		out.controls.push( { id, kind: "button", label, rect, draggable } );
	};
	const thumb = range > 0 ? Math.round( (scroll * SCROLL_TRACK[3]) / range ) : 0;
	const size = SCROLL_BUTTON;
	button( GUIDE_SCROLL_ID + "-up", SCROLL_UP, "Scroll up", [ x, y - size, size, size ], false );
	button( GUIDE_SCROLL_ID + "-down", SCROLL_DOWN, "Scroll down", [ x, y + height, size, size ], false );
	button( GUIDE_SCROLL_THUMB, SCROLL_THUMB, "Scroll the guide", [ x, y + thumb, size, size ], true );
}
