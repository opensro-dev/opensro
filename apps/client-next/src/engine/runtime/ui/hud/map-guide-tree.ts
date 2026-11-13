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
264,56 and its down arrow at 264,234. This module paints one frame of
the tree; the map tools own which group is open, the scroll and the
rows themselves.

===========================================================================
*/
import type { UiControl, UiQuad, UiRect } from "@/engine/contracts/ui";
import type { GlyphStyle } from "@/engine/foundation/rendering/ui-glyphs";
import { chatScrollbar } from "@/engine/foundation/ui/chat-scrollbar";

const GUIDE = "/assets/images/Media_extracted/interface/guide/";
const GROUP_BAR = GUIDE + "gd_long_index_open.png";
const LEAF_BAR = GUIDE + "gd_long_contents_event.png";
// The toggle's faces: right (closed, the art's own) and down (open).
const TOGGLE_CLOSED = GUIDE + "gd_index_button_close.png";
const TOGGLE_OPEN = GUIDE + "gd_index_button_open.png";
const LAMP = GUIDE + "gd_contents_lamp.png";

const ROW_LEFT = 14, ROW_TOP = 37, ROW_PITCH = 30;
const ROW_WIDTH = 248, ROW_HEIGHT = 28;
export const GUIDE_TREE_ROWS = 8;
const TOGGLE: UiRect = [ 3, 3, 24, 24 ];
const LAMP_RECT: UiRect = [ 34, 7, 8, 16 ];
// The label stands centred on the bar right of the toggle's box (both
// bars leave their first 28 columns to the toggle or the lamp's slot).
const LABEL_LEFT = 28;
// The scrollbar's track: the up arrow sits above it, the down arrow
// below, the thumb travels its height (CIFVerticalScroll's layout).
const SCROLL_TRACK: UiRect = [ 264, 72, 16, 146 ];
export const GUIDE_SCROLL_ID = "map-guide-scroll";

// map1's text: gold group names, white species, and a species above the
// character's level in red.
const COLORS = {
	group: [ 239 / 255, 218 / 255, 164 / 255, 1 ],
	leaf: [ 1, 1, 1, 1 ],
	high: [ 239 / 255, 130 / 255, 130 / 255, 1 ]
} as const;
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
	text: {
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
	out.paths.push( GROUP_BAR, LEAF_BAR, TOGGLE_CLOSED, TOGGLE_OPEN, LAMP );
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
		const label: UiRect = [ rect[0] + LABEL_LEFT, rect[1], ROW_WIDTH - LABEL_LEFT, ROW_HEIGHT ];
		out.quads.push(
			...tree.text.quads( row.label, label, tree.full, COLORS[row.color], { hAlign: 1, vAlign: 1 } )
		);
		out.controls.push( {
			id: row.id,
			kind: "button",
			label: row.label,
			rect,
			selected: row.group ? row.open === true : row.lamp === true
		} );
	} );
	const bar = chatScrollbar(
		GUIDE_SCROLL_ID,
		[ left + SCROLL_TRACK[0], top + SCROLL_TRACK[1], SCROLL_TRACK[2], SCROLL_TRACK[3] ],
		tree.rows.length,
		GUIDE_TREE_ROWS,
		// The bar counts its offset up from the end (a chat log's
		// tail); the tree's scroll counts down from the first row.
		range - scroll,
		tree.size,
		tree.full,
		null,
		null
	);
	out.quads.push( ...bar.quads );
	out.paths.push( ...bar.paths );
	// The thumb is a drag handle in the chat; here the arrows step.
	out.controls.push( ...bar.controls.filter( control => control.id !== GUIDE_SCROLL_ID + "-thumb" ) );
	return scroll;
}
