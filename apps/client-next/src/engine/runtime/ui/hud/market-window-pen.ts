/*
===========================================================================

market-window-pen.ts - the consignment window's drawing kit

Port-only, not native (SRO_CONSIGNMENT). What both columns of the window
(market-window.ts, market-window-side.ts) draw with: the frame's paint
contract and the view one frame reads, the pen bound to the window's
origin, the window's size and its two columns, the palette, and the
shared pieces every list uses (prices, item captions, pagers, check-box
choices, the delivery pair). It lays nothing out itself.

===========================================================================
*/
import type { InventoryItem } from "@/engine/contracts/gameplay";
import type { MarketDestination, MarketRow, MarketState } from "@/engine/contracts/market";
import type { UiQuad, UiRect } from "@/engine/contracts/ui";
import { marketAmount, marketCurrencyText } from "@/engine/foundation/gameplay/market";
import type { UiAuthoredFrame } from "../frame/frame-contract";
import { marketBusy, marketWriting, type MarketHudState } from "./market-hud";

// The window's size and its inner margin.
export const WIDTH = 1000;
export const HEIGHT = 600;
export const MARGIN = 14;
// The left column: the listings.
export const LEFT_WIDTH = 680;
// The native interface art the window draws, and its sizes.
export const IFCOMMON = "/assets/images/Media_extracted/interface/ifcommon/";
export const ARROW_SIZE = 16;
export const ARROW_INSET = 6;
export const CHECK_SIZE = 16;
// The right column: List an item, the account's pages, the rules.
export const RIGHT_LEFT = MARGIN + LEFT_WIDTH + MARGIN;
export const RIGHT_WIDTH = WIDTH - RIGHT_LEFT - MARGIN;
// The native button's height, for rows that hold one.
export const BUTTON_HEIGHT = 25;
export const GOLD: UiQuad["color"] = [ 252 / 255, 210 / 255, 100 / 255, 1 ];
export const DIM: UiQuad["color"] = [ 170 / 255, 170 / 255, 160 / 255, 1 ];
export const OK: UiQuad["color"] = [ 150 / 255, 230 / 255, 130 / 255, 1 ];
export const REFUSED: UiQuad["color"] = [ 255 / 255, 130 / 255, 110 / 255, 1 ];
export const SILK: UiQuad["color"] = [ 130 / 255, 200 / 255, 255 / 255, 1 ];
export const PLUSSED: UiQuad["color"] = [ 140 / 255, 180 / 255, 255 / 255, 1 ];
export const PANEL: UiQuad["color"] = [ .02, .03, .025, .82 ];
export const EDGE: UiQuad["color"] = [ .55, .45, .24, .85 ];
export const LINE: UiQuad["color"] = [ .55, .45, .24, .3 ];
export const STRIPE: UiQuad["color"] = [ 1, 1, 1, .03 ];
export const PICKED: UiQuad["color"] = [ 252 / 255, 210 / 255, 100 / 255, .14 ];

/*
================
MarketPaint

The frame's primitives, handed in by ui.ts, their owner: the window,
the native buttons and close box, a plain rect, and an item slot with its
tooltip control (bound to ui.ts's slot context).
================
*/
export type Mark = readonly [number, number, number, number];
export interface MarketPaint {
	beginWindow( frame: UiAuthoredFrame ): Mark;
	endWindow( frame: UiAuthoredFrame, mark: Mark, owner: string ): void;
	windowOrigin(
		frame: UiAuthoredFrame,
		spec: { name: string; initial: [number, number, number, number]; id: string; }
	): UiRect;
	windowBox(
		frame: UiAuthoredFrame,
		spec: { title: string; x: number; y: number; width: number; height: number; tile?: string; }
	): void;
	closeButton( frame: UiAuthoredFrame, x: number, y: number, id: "market-close" ): void;
	button(
		frame: UiAuthoredFrame,
		spec: {
			id: string;
			value: string;
			x: number;
			y: number;
			width: number;
			disabled?: boolean;
			selected?: boolean;
		}
	): void;
	rect( frame: UiAuthoredFrame, spec: { r: UiRect; color: UiQuad["color"]; } ): void;
	// A native interface image (ifcommon art) at its own size.
	image( frame: UiAuthoredFrame, spec: { r: UiRect; path: string; } ): void;
	// The option window's combo box: the black field and the native drop
	// arrow (com_qst_downarrow_button), with its focus and press art.
	comboBox(
		frame: UiAuthoredFrame,
		spec: { r: UiRect; id: string; label: string; value: string; }
	): void;
	nativeItem(
		frame: UiAuthoredFrame,
		spec: { id: string; item: InventoryItem | undefined; r: UiRect; selected?: boolean; }
	): void;
}

/*
================
MarketWindowView

What one frame of the window reads: the HUD's drafts, the worker's
market and bag, the character, the clock and the caret.
================
*/
export interface MarketWindowView {
	readonly hud: MarketHudState;
	readonly market: MarketState | undefined;
	readonly bag: readonly InventoryItem[];
	readonly gold: number;
	readonly localName: string;
	readonly now: number;
	readonly focus: string | null;
	readonly caretVisible: boolean;
}

// ============================================================================

/*
================
Pen

The window's painters, bound to its origin.
================
*/
export type Pen = ReturnType<typeof penOf>;

/*
================
penOf
================
*/
export function penOf(
	frame: UiAuthoredFrame,
	frameDraw: MarketPaint,
	view: MarketWindowView,
	px: number,
	py: number
) {
	const busy = marketBusy( view.market ), writing = marketWriting( view.market );
	return {
		frame,
		frameDraw,
		view,
		px,
		py,
		busy,
		writing,
		/*
		================
		text
		================
		*/
		text( value: string, r: UiRect, color: UiQuad["color"] = frame.white, hAlign: 0 | 1 | 2 = 0 ) {
			frame.quads.push( ...frame.text.quads( value, r, r, color, { hAlign, vAlign: 1, overflow: "ellipsis" } ) );
		},
		/*
		================
		panel

		A dark inset with the window's gilt edge.
		================
		*/
		panel( r: UiRect, edge: UiQuad["color"] = EDGE ) {
			frameDraw.rect( frame, { r, color: PANEL } );
			for (
				const line of [
					[ r[0], r[1], r[2], 1 ],
					[ r[0], r[1] + r[3] - 1, r[2], 1 ],
					[ r[0], r[1], 1, r[3] ],
					[ r[0] + r[2] - 1, r[1], 1, r[3] ]
				] as UiRect[]
			) frameDraw.rect( frame, { r: line, color: edge } );
		},
		/*
		================
		button
		================
		*/
		button( id: string, value: string, x: number, y: number, width: number, disabled = false, selected = false ) {
			frameDraw.button( frame, { id, value, x, y, width, disabled, selected } );
		},
		/*
		================
		item

		An item slot: the icon, its count and its tooltip control.
		================
		*/
		item( id: string, item: InventoryItem | undefined, r: UiRect, selected = false ) {
			frameDraw.rect( frame, { r, color: [ 0, 0, 0, .55 ] } );
			frameDraw.nativeItem( frame, { id, item, r, selected } );
		},
		/*
		================
		field

		A typed field: its frame, the text, a placeholder while empty, and
		the caret while focused.
		================
		*/
		field( id: string, value: string, r: UiRect, placeholder: string, maxLength: number ) {
			this.panel( r, view.focus === id ? GOLD : EDGE );
			const inner: UiRect = [ r[0] + 6, r[1], r[2] - 12, r[3] ];
			frame.controls.push( { id, label: placeholder, kind: "text", value, rect: r, maxLength } );
			if ( value ) this.text( value, inner );
			else if ( view.focus !== id ) this.text( placeholder, inner, DIM );
			if ( view.focus === id && view.caretVisible ) {
				const x = inner[0] + Math.min( inner[2], frame.text.run( value ).width );
				frameDraw.rect( frame, { r: [ x, r[1] + 5, 1, r[3] - 10 ], color: frame.white } );
			}
		},
		/*
		================
		row

		A list row's stripe, rule and pick control; picked rows are gilded.
		================
		*/
		row( id: string, r: UiRect, index: number, picked: boolean, label: string ) {
			if ( picked ) frameDraw.rect( frame, { r, color: PICKED } );
			else if ( index % 2 ) frameDraw.rect( frame, { r, color: STRIPE } );
			frameDraw.rect( frame, { r: [ r[0], r[1] + r[3] - 1, r[2], 1 ], color: LINE } );
			frame.controls.push( { id, label, rect: r, kind: "button", selected: picked } );
		}
	};
}

/*
================
priceText
================
*/
export function priceText( price: number, currency: MarketRow["currency"] ): string {
	return `${marketAmount( price )} ${marketCurrencyText( currency )}`;
}

/*
================
priceColor
================
*/
export function priceColor( currency: MarketRow["currency"] ): UiQuad["color"] {
	return currency === "silk" ? SILK : GOLD;
}

/*
================
itemCaption

An item's name with its plus, as the bag's help names it.
================
*/
export function itemCaption( item: InventoryItem ): string {
	return (item.name ?? "") + (item.plus > 0 ? ` (+${item.plus})` : "");
}

/*
================
itemColor

A plussed item reads blue, as the site's list and the bag's help do.
================
*/
export function itemColor( pen: Pen, item: InventoryItem ): UiQuad["color"] {
	return item.plus > 0 ? PLUSSED : pen.frame.white;
}

/*
================
pager

The Previous / n of m / Next pair every list shares. The arrows are the
game's own (com_left_arrow, com_right_arrow, with their disabled art);
the compact pair is the arrows alone. A single page draws nothing.
================
*/
export function pager(
	pen: Pen,
	prefix: string,
	page: number,
	pages: number,
	right: number,
	y: number,
	compact = false
) {
	// One page has nowhere to go: no dead buttons.
	if ( pages <= 1 ) return;
	const step = compact ? 30 : 74, middle = compact ? 46 : 56;
	const left = right - (2 * step + middle + 8);
	pagerButton( pen, prefix + "prev", compact ? "" : "Previous", left, y, step, page <= 0, "left" );
	pen.text(
		`${page + 1} / ${Math.max( 1, pages )}`,
		[ left + step + 4, y, middle, BUTTON_HEIGHT ],
		pen.frame.white,
		1
	);
	pagerButton(
		pen,
		prefix + "next",
		compact ? "" : "Next",
		left + step + middle + 8,
		y,
		step,
		page + 1 >= pages,
		"right"
	);
}

/*
================
pagerButton

A pager button with the native arrow on its outer side (centred when it
has no caption); the caption fills the rest.
================
*/
export function pagerButton(
	pen: Pen,
	id: string,
	caption: string,
	x: number,
	y: number,
	width: number,
	disabled: boolean,
	side: "left" | "right"
) {
	pen.button( id, "", x, y, width, disabled );
	const arrowX = !caption ?
		x + (width - ARROW_SIZE) / 2 :
		side === "left" ?
		x + ARROW_INSET :
		x + width - ARROW_INSET - ARROW_SIZE;
	pen.frameDraw.image( pen.frame, {
		r: [ arrowX, y + (BUTTON_HEIGHT - ARROW_SIZE) / 2, ARROW_SIZE, ARROW_SIZE ],
		path: IFCOMMON + `com_${side}_arrow${disabled ? "_disable" : ""}.png`
	} );
	if ( !caption ) return;
	// The caption centres in what the arrow leaves.
	const room = width - ARROW_INSET - ARROW_SIZE;
	const textX = side === "left" ? x + ARROW_INSET + ARROW_SIZE : x;
	pen.text( caption, [ textX, y, room, BUTTON_HEIGHT ], disabled ? DIM : pen.frame.white, 1 );
}

/*
================
choice

One option of a pick-one choice: the game's check box (com_checkbutton)
and its caption, the whole line one control.
================
*/
export function choice( pen: Pen, id: string, caption: string, r: UiRect, on: boolean ) {
	pen.frameDraw.image( pen.frame, {
		r: [ r[0], r[1] + (r[3] - CHECK_SIZE) / 2, CHECK_SIZE, CHECK_SIZE ],
		path: IFCOMMON + `com_checkbutton_${on ? "on" : "off"}.png`
	} );
	pen.text( caption, [ r[0] + CHECK_SIZE + 4, r[1], r[2] - CHECK_SIZE - 4, r[3] ], on ? pen.frame.white : DIM );
	pen.frame.controls.push( { id, label: caption, kind: "button", rect: r, selected: on } );
}

/*
================
destinationButtons

The inventory or warehouse pair every delivery asks for.
================
*/
export function destinationButtons(
	pen: Pen,
	prefix: string,
	to: MarketDestination,
	x: number,
	y: number,
	width = 108
) {
	pen.button( prefix + "bag", width < 100 ? "Inventory" : "To inventory", x, y, width, false, to === "bag" );
	pen.button(
		prefix + "warehouse",
		width < 100 ? "Warehouse" : "To warehouse",
		x + width + 4,
		y,
		width,
		false,
		to === "warehouse"
	);
}
