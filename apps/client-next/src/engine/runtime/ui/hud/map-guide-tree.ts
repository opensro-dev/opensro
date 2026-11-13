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
264,56 and its down arrow at 264,234. The scrollbar is the guide's own
(interface/guide/gd_scroll_*, issue #3392): the line, the arrows and a
thumb that drags. This module paints one frame of the tree; the map
tools own which group is open, the scroll and the rows themselves.

===========================================================================
*/
import type { UiControl, UiQuad, UiRect } from "@/engine/contracts/ui";
import type { GlyphStyle } from "@/engine/foundation/rendering/ui-glyphs";

const GUIDE = "/assets/images/Media_extracted/interface/guide/";
const GROUP_BAR = GUIDE + "gd_long_index_open.png";
const LEAF_BAR = GUIDE + "gd_long_contents_event.png";
// The toggle's faces: right (closed, the art's own) and down (open).
const TOGGLE_CLOSED = GUIDE + "gd_index_button_close.png";
const TOGGLE_OPEN = GUIDE + "gd_index_button_open.png";
const LAMP = GUIDE + "gd_contents_lamp.png";
const SCROLL_LINE = GUIDE + "gd_scroll_line.png";
const SCROLL_UP = GUIDE + "gd_scroll_up";
const SCROLL_DOWN = GUIDE + "gd_scroll_down";
const SCROLL_THUMB = GUIDE + "gd_scroll_button";
// A button's faces: normal, hover, pressed.
const FACES = [ ".png", "_focus.png", "_press.png" ] as const;

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
// layout), and the 12 px line stands centred in the 16 px column.
const SCROLL_TRACK: UiRect = [ 264, 72, 16, 146 ];
const SCROLL_BUTTON = 16;
const SCROLL_LINE_WIDTH = 12;
const SCROLL_LINE_HEIGHT = 292;
export const GUIDE_SCROLL_ID = "map-guide-scroll";
export const GUIDE_SCROLL_THUMB = GUIDE_SCROLL_ID + "-thumb";
// The thumb's travel in pixels, for turning a drag into rows.
export const GUIDE_SCROLL_TRAVEL = SCROLL_TRACK[3];
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

The guide's own bar: gd_scroll_line from the track's top to the down
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
	// The line art is taller than the track: its top part shows rather
	// than a squeezed whole, so its knots keep their spacing.
	out.quads.push( {
		texture: SCROLL_LINE,
		rect: [ x + (SCROLL_BUTTON - SCROLL_LINE_WIDTH) / 2, y, SCROLL_LINE_WIDTH, height ],
		uv: [ 0, 0, 1, Math.min( 1, height / SCROLL_LINE_HEIGHT ) ],
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
