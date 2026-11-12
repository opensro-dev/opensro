/*
===========================================================================

map-drawer.ts - the world map's paged list panel (issues #3167, #3239)

The panel both monster lists draw: the region and monster drawer at the
world page's right edge and the shown floor's list at a dungeon page's
left edge. One panel, painted the same on both: its backing, a title,
an optional back button, the rows of the current page, a note when
there are none, and the pager. The map tools own what the rows are and
which page is shown; this file owns only how a page of rows is laid out.

===========================================================================
*/
import type { UiControl, UiQuad, UiRect } from "@/engine/contracts/ui";
import type { GlyphStyle } from "@/engine/foundation/rendering/ui-glyphs";

export const DRAWER_WIDTH = 196;
const ROW_HEIGHT = 18;
// The title strip above the rows and the pager strip below them.
const DRAWER_CHROME_HEIGHT = 56;
const WHITE = [ 1, 1, 1, 1 ] as const;
const GOLD = [ .96, .83, .55, 1 ] as const;
const DIM = [ .72, .7, .63, 1 ] as const;
const BACKING = [ .05, .06, .055, .97 ] as const;

/*
================
DrawerText

Only the font operation the panel uses; the UI retains font ownership.
================
*/
interface DrawerText {
	quads( value: string, rect: UiRect, clip: UiRect, color: UiQuad["color"], style?: GlyphStyle ): UiQuad[];
}

/*
================
DrawerRow
================
*/
export interface DrawerRow {
	id: string;
	label: string;
	selected: boolean;
}

/*
================
DrawerPanel

One panel's frame: where it sits, what it lists, the page asked for.
================
*/
export interface DrawerPanel {
	text: DrawerText;
	rect: UiRect;
	full: UiRect;
	title: string;
	// The "All regions" button at the title's right, when the world list
	// is showing one region's monsters.
	back: boolean;
	rows: readonly DrawerRow[];
	page: number;
	// The note drawn when there are no rows.
	empty: string;
}

/*
================
paintDrawer

Paints one panel into quads and controls. Returns the page it showed:
the page asked for, held to the last page when the rows shrank under
it, so the caller's paging state never points past the list.
================
*/
export function paintDrawer( panel: DrawerPanel, quads: UiQuad[], controls: UiControl[] ): number {
	const text = panel.text, full = panel.full;
	const [dx, dy, dw, dh] = panel.rect;
	quads.push( { texture: "", rect: [ dx, dy, dw, dh ], uv: [ 0, 0, 1, 1 ], color: BACKING, clip: full } );
	quads.push( ...text.quads( panel.title, [ dx + 10, dy + 6, dw - 20, 18 ], full, GOLD, {
		fontStyle: 2,
		overflow: "clip"
	} ) );
	if ( panel.back ) {
		controls.push( {
			id: "map-drawer-back",
			kind: "button",
			label: "All regions",
			rect: [ dx + dw - 58, dy + 4, 52, 18 ]
		} );
		quads.push( ...text.quads( "< Back", [ dx + dw - 58, dy + 6, 52, 18 ], full, WHITE, {
			hAlign: 2,
			vAlign: 1
		} ) );
	}
	const rows = panel.rows;
	const visible = Math.max( 1, Math.floor( (dh - DRAWER_CHROME_HEIGHT) / ROW_HEIGHT ) );
	const pages = Math.max( 1, Math.ceil( rows.length / visible ) );
	const page = Math.min( panel.page, pages - 1 );
	if ( rows.length === 0 ) {
		quads.push( ...text.quads( panel.empty, [ dx + 10, dy + 30, dw - 20, 18 ], full, DIM, { overflow: "clip" } ) );
	}
	rows.slice( page * visible, page * visible + visible ).forEach( ( row, index ) => {
		const top = dy + 28 + index * ROW_HEIGHT;
		controls.push( {
			id: row.id,
			kind: "button",
			label: row.label,
			rect: [ dx + 4, top, dw - 8, ROW_HEIGHT ],
			selected: row.selected
		} );
		quads.push(
			...text.quads( row.label, [ dx + 10, top, dw - 16, ROW_HEIGHT ], full, row.selected ? GOLD : WHITE, {
				vAlign: 1,
				overflow: "clip"
			} )
		);
	} );
	if ( pages > 1 ) {
		controls.push( {
			id: "map-drawer-prev",
			kind: "button",
			label: "Previous page",
			rect: [ dx + 4, dy + dh - 22, 52, 18 ]
		} );
		controls.push( {
			id: "map-drawer-next",
			kind: "button",
			label: "Next page",
			rect: [ dx + dw - 56, dy + dh - 22, 52, 18 ]
		} );
		quads.push(
			...text.quads( "< Prev", [ dx + 10, dy + dh - 22, 52, 18 ], full, WHITE, { vAlign: 1 } ),
			...text.quads( "Next >", [ dx + dw - 62, dy + dh - 22, 52, 18 ], full, WHITE, { hAlign: 2, vAlign: 1 } ),
			...text.quads( (page + 1) + "/" + pages, [ dx + 56, dy + dh - 22, dw - 112, 18 ], full, DIM, {
				hAlign: 1,
				vAlign: 1
			} )
		);
	}
	return page;
}
