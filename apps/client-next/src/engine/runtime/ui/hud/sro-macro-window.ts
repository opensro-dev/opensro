/*
===========================================================================

sro-macro-window.ts - the SRO Macro windows, drawn (port-only, not native)

Port-only, not native (#3907): the v1.696 menu (macro_start.2dt) and
options window (macro_1.2dt, macro_2.2dt, macro_3.2dt) are not in the
v1.150 data, so both are built from the client's own chrome: the framed
window, the native buttons and com_checkbutton. Sizes are this port's
own. sro-macro-hud.ts owns every value shown and every click.

	menu     one row per section: its name, ON/OFF and Options
	options  Auto-Potion / Auto-Skills / Auto-Hunt tabs over the draft;
	         Save and Cancel

===========================================================================
*/
import type { UiQuad, UiRect } from "@/engine/contracts/ui";
import type { SkillMetadata } from "@/engine/foundation/gameplay/skill-catalog";
import {
	MACRO_HUNT,
	MACRO_POTION,
	MACRO_SKILL,
	macroAvailable,
	type MacroGearKind,
	type MacroKind,
	macroGearBagSlot,
	macroOn,
	type MacroSettings,
	potionRowOn,
	potionSlot
} from "@/engine/foundation/gameplay/sro-macro";
import type { UiAuthoredFrame } from "../frame/frame-contract";
import {
	MACRO_HUNT_CHECKS,
	MACRO_HUNT_GROUPS,
	MACRO_ID,
	MACRO_PERCENT_DEFAULT,
	MACRO_POTION_ROWS,
	MACRO_SKILL_GRID,
	macroSkillBound,
	macroSkillFitsPage,
	type MacroHudState,
	type MacroSkillPage
} from "./sro-macro-hud";

const IFCOMMON = "/assets/images/Media_extracted/interface/ifcommon/";
const CHAT_WND = "/assets/images/Media_extracted/interface/chattingwnd/";
// Stock CIFVerticalScroll faces (same art as chat / the M-window guide).
const SCROLL_TRACK = IFCOMMON + "com_scroll_bar.png";
const SCROLL_UP = CHAT_WND + "chat_arrow_up.png";
const SCROLL_DOWN = CHAT_WND + "chat_arrow_down.png";
const SCROLL_THUMB = IFCOMMON + "com_scroll_button.png";
const CHECK_SIZE = 16;
const BUTTON_HEIGHT = 25;
const ROW = 26;
const MARGIN = 16;
const TITLE = "SRO Macro";
const MENU_WIDTH = 300;
const MENU_HEIGHT = 168;
const MENU_TOP = 44;
const MENU_PITCH = 36;
// Options chrome is shared; Potion / Hunt hug their rows. Skill matches
// CNIFMacroAutoSkillWnd: acquired grid | Weapon/Shield + skills-to-use.
const OPTIONS_WIDTH = 460;
const OPTIONS_WIDTH_SKILL = 560;
const OPTIONS_FOOTER = 52;
const OPTIONS_HEIGHT_SKILL = 420;
const TAB_TOP = 40;
const BODY_TOP = 76;
const TAB_WIDTH = 136;
const STEP_WIDTH = 26;
const ITEM_SLOT = 32;
const GEAR_SLOT = 40;
// CIFSlotWithHelpEx in macro_2.2dt: icon slots only; the name is the help tip.
const SKILL_ICON = 32;
const SKILL_GAP = 2;
const SKILL_SCROLL = 16;
const ROW_GAP = 6;
const BAG_FIRST_SLOT = 13;
const GOLD: UiQuad["color"] = [ 252 / 255, 210 / 255, 100 / 255, 1 ];
const DIM: UiQuad["color"] = [ 170 / 255, 170 / 255, 160 / 255, 1 ];
const LINE: UiQuad["color"] = [ .55, .45, .24, .3 ];
const PANEL: UiQuad["color"] = [ .02, .03, .025, .82 ];
// Gear / skill wells sit on PANEL; a lighter pit and stronger rim keep empty
// Weapon / Shield slots readable against the body.
const SLOT_WELL: UiQuad["color"] = [ .08, .09, .07, .92 ];
const SLOT_RIM: UiQuad["color"] = [ .72, .58, .28, .85 ];
const SLOT_SHADE: UiQuad["color"] = [ 0, 0, 0, .45 ];
const SKILL_HELP = "Assign the skills and weapons (shields) to use the skills automatically.";
const SKILL_PAGES: readonly { readonly id: MacroSkillPage; readonly caption: string; }[] = [
	{ id: "attack", caption: "Configure Attack Skills" },
	{ id: "buff", caption: "Configure Buff Skills" }
];

// The v1.696 section names (UIIT_STT_SRO_MACRO_AUTO_POTION/SKILL/HUNTING).
const SECTION_NAMES: Readonly<Record<MacroKind, string>> = {
	[MACRO_POTION]: "Auto-Potion",
	[MACRO_SKILL]: "Auto-Skills",
	[MACRO_HUNT]: "Auto-Hunt"
};
const SECTIONS = [ MACRO_POTION, MACRO_SKILL, MACRO_HUNT ] as const;

/*
================
optionsSize

Potion and Hunt size to their rows; Skill uses the two-column macro_2.2dt
layout (acquired left, Weapon/Shield + skills-to-use right).
================
*/
function optionsSize(
	tab: MacroKind,
	potionPage: keyof typeof MACRO_POTION_ROWS
): { readonly width: number; readonly height: number; } {
	const chrome = BODY_TOP + OPTIONS_FOOTER;
	if ( tab === MACRO_POTION ) {
		const rows = MACRO_POTION_ROWS[potionPage].length;
		const pitch = Math.max( ROW, ITEM_SLOT ) + ROW_GAP;
		return {
			width: OPTIONS_WIDTH,
			height: chrome + 8 + BUTTON_HEIGHT + 12 + rows * pitch + 8
		};
	}
	if ( tab === MACRO_HUNT ) {
		return {
			width: OPTIONS_WIDTH,
			height: chrome + 8 + MACRO_HUNT_GROUPS.length * (ROW + 4) + 8 + MACRO_HUNT_CHECKS.length * ROW + 8
		};
	}
	return { width: OPTIONS_WIDTH_SKILL, height: OPTIONS_HEIGHT_SKILL };
}

/*
================
MacroPaint

The frame's primitives, handed in by ui.ts, their owner.
================
*/
export type MacroMark = readonly [number, number, number, number];
export interface MacroPaint {
	beginWindow( frame: UiAuthoredFrame ): MacroMark;
	endWindow( frame: UiAuthoredFrame, mark: MacroMark, owner: string ): void;
	windowOrigin(
		frame: UiAuthoredFrame,
		spec: { name: string; initial: [number, number, number, number]; id: string; }
	): UiRect;
	windowBox(
		frame: UiAuthoredFrame,
		spec: { title: string; x: number; y: number; width: number; height: number; }
	): void;
	closeButton( frame: UiAuthoredFrame, x: number, y: number, id: "sro-macro-close" ): void;
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
	image( frame: UiAuthoredFrame, spec: { r: UiRect; path: string; } ): void;
}

/*
================
MacroWindowView

What one frame of the windows reads.
================
*/
export interface MacroWindowView {
	readonly hud: MacroHudState;
	readonly live: MacroSettings;
	readonly acquired: readonly SkillMetadata[];
	readonly skillIndex: ReadonlyMap<number, SkillMetadata> | undefined;
	// The bag's stock by RefItem ID: an icon path and the summed count.
	readonly potionStock: ReadonlyMap<number, MacroPotionStock>;
	// Bag (and worn) items for the Weapon / Shield slot icons.
	readonly inventory: readonly {
		readonly slot: number;
		readonly icon?: string;
		readonly name?: string;
	}[];
	// Resolves a skill or item icon name to a drawable path.
	readonly iconPath: ( icon: string | undefined ) => string | null;
}

/*
================
MacroPotionStock
================
*/
export interface MacroPotionStock {
	readonly icon: string | undefined;
	readonly quantity: number;
}

/*
================
macroPotionStock

The bag's items by RefItem ID, the equipment sockets left out.
================
*/
export function macroPotionStock(
	inventory: readonly {
		readonly slot: number;
		readonly refObjId: number;
		readonly quantity: number;
		readonly icon?: string;
	}[],
	iconPath: ( icon: string | undefined ) => string | null
): Map<number, MacroPotionStock> {
	const stock = new Map<number, MacroPotionStock>();
	for ( const item of inventory ) {
		if ( item.slot < BAG_FIRST_SLOT ) continue;
		const prior = stock.get( item.refObjId );
		stock.set( item.refObjId, {
			icon: prior?.icon ?? iconPath( item.icon ) ?? undefined,
			quantity: (prior?.quantity ?? 0) + item.quantity
		} );
	}
	return stock;
}

type Pen = ReturnType<typeof penOf>;

/*
================
penOf
================
*/
function penOf( frame: UiAuthoredFrame, paint: MacroPaint ) {
	return {
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
		button
		================
		*/
		button( id: string, value: string, x: number, y: number, width: number, disabled = false, selected = false ) {
			paint.button( frame, { id: MACRO_ID + id, value, x, y, width, disabled, selected } );
		},
		/*
		================
		check

		The game's check box (com_checkbutton) and its caption, the whole
		line one control.
		================
		*/
		check( id: string, caption: string, r: UiRect, on: boolean ) {
			paint.image( frame, {
				r: [ r[0], r[1] + (r[3] - CHECK_SIZE) / 2, CHECK_SIZE, CHECK_SIZE ],
				path: IFCOMMON + `com_checkbutton_${on ? "on" : "off"}.png`
			} );
			this.text( caption, [ r[0] + CHECK_SIZE + 6, r[1], r[2] - CHECK_SIZE - 6, r[3] ], on ? frame.white : DIM );
			frame.controls.push( { id: MACRO_ID + id, label: caption, kind: "button", rect: r, selected: on } );
		},
		/*
		================
		row

		A clickable list row with its rule.
		================
		*/
		row( id: string, caption: string, r: UiRect, color: UiQuad["color"] = frame.white ) {
			paint.rect( frame, { r: [ r[0], r[1] + r[3] - 1, r[2], 1 ], color: LINE } );
			this.text( caption, [ r[0] + 4, r[1], r[2] - 8, r[3] ], color );
			frame.controls.push( { id: MACRO_ID + id, label: caption, kind: "button", rect: r } );
		},
		/*
		================
		skillSlot

		One CIFSlotWithHelpEx cell (macro_2.2dt): the skill icon only. The
		name is the control label (helper tip), not drawn beside the icon.
		================
		*/
		skillSlot( id: string | undefined, caption: string, r: UiRect, icon: string | undefined ) {
			paint.rect( frame, { r, color: PANEL } );
			paint.rect( frame, { r: [ r[0], r[1] + r[3] - 1, r[2], 1 ], color: LINE } );
			if ( icon ) paint.image( frame, { r: [ r[0] + 2, r[1] + 2, r[2] - 4, r[3] - 4 ], path: icon } );
			if ( id ) frame.controls.push( { id: MACRO_ID + id, label: caption, kind: "button", rect: r } );
		},
		/*
		================
		itemSlot

		A potion row's item: its icon and bag count, a dim square while
		nothing is bound or the bag has none. One control, so a drop and a
		click both land on it.
		================
		*/
		itemSlot( id: string, r: UiRect, item: number, stock: ReadonlyMap<number, MacroPotionStock> ) {
			const held = item ? stock.get( item ) : undefined;
			paint.rect( frame, { r, color: PANEL } );
			paint.rect( frame, { r: [ r[0], r[1] + r[3] - 1, r[2], 1 ], color: LINE } );
			// A bound RefItem with no bag stack (scrubbed equipment, spent
			// potion) paints as empty — a dim "0" looked like a broken icon.
			if ( held?.icon ) {
				paint.image( frame, { r: [ r[0] + 2, r[1] + 2, r[2] - 4, r[3] - 4 ], path: held.icon } );
			}
			if ( held ) {
				this.text( String( held.quantity ), [ r[0], r[1] + r[3] - 12, r[2] - 2, 12 ], frame.white, 2 );
			}
			const label = held ? "Clear the potion" : "Drop a potion from the inventory";
			// Occupied slots are draggable: releasing outside clears the bind
			// (CIFSlot drag-out; click still clears without a drag).
			frame.controls.push( {
				id: MACRO_ID + id,
				label,
				kind: "button",
				rect: r,
				draggable: !!held
			} );
		},
		/*
		================
		gearSlot

		Weapon / Shield (macro_2.2dt): one CIFSlot for the absolute bag
		slot stored in skillFlags tags 3 / 4. Icon from the bag item; empty
		when the slot is free or scrubbed.
		================
		*/
		gearSlot(
			kind: MacroGearKind,
			r: UiRect,
			bagSlot: number,
			inventory: MacroWindowView["inventory"],
			iconPath: MacroWindowView["iconPath"]
		) {
			const held = bagSlot ? inventory.find( row => row.slot === bagSlot ) : undefined;
			const icon = held ? iconPath( held.icon ) ?? undefined : undefined;
			const [x, y, w, h] = r;
			paint.rect( frame, { r, color: SLOT_WELL } );
			// Inner shade so an empty well reads as a recess, not a flat panel.
			paint.rect( frame, { r: [ x + 1, y + 1, w - 2, h - 2 ], color: SLOT_SHADE } );
			paint.rect( frame, { r: [ x, y, w, 1 ], color: SLOT_RIM } );
			paint.rect( frame, { r: [ x, y + h - 1, w, 1 ], color: SLOT_RIM } );
			paint.rect( frame, { r: [ x, y, 1, h ], color: SLOT_RIM } );
			paint.rect( frame, { r: [ x + w - 1, y, 1, h ], color: SLOT_RIM } );
			if ( icon ) paint.image( frame, { r: [ x + 3, y + 3, w - 6, h - 6 ], path: icon } );
			const caption = kind === "weapon" ? "Weapon" : "Shield";
			const label = held ?
				`Clear the ${caption.toLowerCase()}` :
				`Drop a ${caption.toLowerCase()} from the inventory`;
			frame.controls.push( {
				id: MACRO_ID + `gear:${kind}`,
				label: held?.name ?? label,
				kind: "button",
				rect: r,
				draggable: !!held
			} );
		},
		/*
		================
		panel
		================
		*/
		panel( r: UiRect ) {
			paint.rect( frame, { r, color: PANEL } );
		},
		/*
		================
		heading
		================
		*/
		heading( value: string, x: number, y: number, width: number ) {
			this.text( value, [ x, y, width, ROW ], GOLD );
		},
		/*
		================
		scrollBar

		Stock CIFVerticalScroll beside a skill grid: com_scroll_bar track,
		chat_arrow up/down, and com_scroll_button thumb. The game font has
		no ▲/▼ glyphs, so Unicode arrows painted as "?".
		================
		*/
		scrollBar( id: string, r: UiRect, scroll: number, maxScroll: number ) {
			const [x, y, w, h] = r;
			const arrow = Math.min( SKILL_SCROLL, Math.floor( h / 3 ) );
			const travel = Math.max( arrow, h - arrow * 2 );
			const up: UiRect = [ x, y, w, arrow ];
			const rail: UiRect = [ x, y + arrow, w, travel ];
			const down: UiRect = [ x, y + h - arrow, w, arrow ];
			// Track art spans the rail plus the down arrow's slot, as chat's
			// CIFVerticalScroll stretches com_scroll_bar under the thumb run.
			paint.image( frame, { r: [ x, y + arrow, w, travel + arrow ], path: SCROLL_TRACK } );
			paint.image( frame, { r: up, path: SCROLL_UP } );
			paint.image( frame, { r: down, path: SCROLL_DOWN } );
			if ( maxScroll > 0 ) {
				const run = Math.max( 0, travel - arrow );
				const thumbTop = rail[1] + Math.round( (scroll * run) / maxScroll );
				paint.image( frame, { r: [ x, thumbTop, w, arrow ], path: SCROLL_THUMB } );
			}
			if ( scroll > 0 ) {
				frame.controls.push( {
					id: MACRO_ID + `${id}:-1`,
					label: "Scroll up",
					kind: "button",
					rect: up
				} );
			}
			if ( scroll < maxScroll ) {
				frame.controls.push( {
					id: MACRO_ID + `${id}:1`,
					label: "Scroll down",
					kind: "button",
					rect: down
				} );
			}
		},
		/*
		================
		wheelZone

		Invisible hit target so the wheel can scroll the grid under it.
		================
		*/
		wheelZone( id: string, r: UiRect ) {
			frame.controls.push( { id: MACRO_ID + id, label: "", kind: "button", rect: r } );
		}
	};
}

/*
================
drawSroMacro
================
*/
export function drawSroMacro( frame: UiAuthoredFrame, paint: MacroPaint, view: MacroWindowView ) {
	if ( view.hud.window === "menu" ) drawMenu( frame, paint, view );
	else if ( view.hud.window === "options" && view.hud.draft ) drawOptions( frame, paint, view, view.hud.draft );
}

/*
================
drawMenu

CNIFMacroMenuWnd: a row per section. An unavailable section shows the
v1.696 "Unavailable" (UIIT_STT_SRO_MACRO_HUNT_NOT) and no controls.
================
*/
function drawMenu( frame: UiAuthoredFrame, paint: MacroPaint, view: MacroWindowView ) {
	const admission = paint.beginWindow( frame );
	const [px, py] = paint.windowOrigin( frame, {
		name: "SroMacroMenu",
		initial: [
			Math.max( 0, frame.w - MENU_WIDTH - 40 ),
			Math.max( 0, (frame.h - MENU_HEIGHT) / 3 ),
			MENU_WIDTH,
			MENU_HEIGHT
		],
		id: "window-drag:SroMacroMenu"
	} );
	paint.windowBox( frame, { title: TITLE, x: px, y: py, width: MENU_WIDTH, height: MENU_HEIGHT } );
	paint.closeButton( frame, px + MENU_WIDTH - 26, py + 10, "sro-macro-close" );
	const pen = penOf( frame, paint );
	SECTIONS.forEach( ( kind, i ) => {
		const y = py + MENU_TOP + i * MENU_PITCH;
		const available = macroAvailable( view.live, kind ), on = macroOn( view.live, kind );
		pen.text( SECTION_NAMES[kind], [ px + MARGIN, y, 110, BUTTON_HEIGHT ], available ? frame.white : DIM );
		if ( !available ) {
			pen.text( "Unavailable", [ px + 130, y, 150, BUTTON_HEIGHT ], DIM );
			return;
		}
		pen.button( `switch:${kind}`, on ? "ON" : "OFF", px + 130, y, 64, false, on );
		pen.button( `options:${kind}`, "Options", px + 200, y, 84 );
	} );
	paint.endWindow( frame, admission, "sro-macro" );
}

/*
================
drawOptions

CNIFMacroWnd: the section tabs over the draft, then Save and Cancel.
================
*/
function drawOptions( frame: UiAuthoredFrame, paint: MacroPaint, view: MacroWindowView, draft: MacroSettings ) {
	const admission = paint.beginWindow( frame );
	const { width, height } = optionsSize( view.hud.tab, view.hud.potionPage );
	const [px, py] = paint.windowOrigin( frame, {
		name: "SroMacroOptions",
		initial: [
			Math.max( 0, (frame.w - width) / 2 ),
			Math.max( 0, (frame.h - height) / 3 ),
			width,
			height
		],
		id: "window-drag:SroMacroOptions"
	} );
	paint.windowBox( frame, { title: TITLE, x: px, y: py, width, height } );
	paint.closeButton( frame, px + width - 26, py + 10, "sro-macro-close" );
	const pen = penOf( frame, paint );
	SECTIONS.forEach( ( kind, i ) => {
		pen.button(
			`tab:${kind}`,
			SECTION_NAMES[kind],
			px + MARGIN + i * (TAB_WIDTH + 6),
			py + TAB_TOP,
			TAB_WIDTH,
			!macroAvailable( view.live, kind ),
			view.hud.tab === kind
		);
	} );
	const body: UiRect = [ px + MARGIN, py + BODY_TOP, width - MARGIN * 2, height - BODY_TOP - OPTIONS_FOOTER ];
	pen.panel( body );
	if ( view.hud.tab === MACRO_POTION ) potionTab( pen, view, draft, body );
	else if ( view.hud.tab === MACRO_SKILL ) skillTab( pen, view, draft, body );
	else huntTab( pen, draft, body );
	const footer = py + height - 42;
	pen.button( "save", "Save", px + width / 2 - 110, footer, 100 );
	pen.button( "cancel", "Cancel", px + width / 2 + 10, footer, 100 );
	paint.endWindow( frame, admission, "sro-macro" );
}

/*
================
potionTab

CNIFMacroAutoPotionWnd: Character and Pet pages of rows. Each row is an
item slot (CNIFMacroAutoPotionSlot: drop a bag item on it, click to
clear), a check box and, on a gauge row, its percent with - and +.
================
*/
function potionTab( pen: Pen, view: MacroWindowView, draft: MacroSettings, body: UiRect ) {
	const [x, y, width] = body;
	const page = view.hud.potionPage;
	const pitch = Math.max( ROW, ITEM_SLOT ) + ROW_GAP;
	const rowsTop = y + 8 + BUTTON_HEIGHT + 12;
	pen.button( "page:character", "Character", x + 8, y + 8, 110, false, page === "character" );
	pen.button( "page:pet", "Pet", x + 124, y + 8, 110, false, page === "pet" );
	MACRO_POTION_ROWS[page].forEach( ( row, i ) => {
		const top = rowsTop + i * pitch;
		const slot = potionSlot( draft.potion, row.slot ), on = potionRowOn( draft.potion, row.slot );
		pen.itemSlot( `item:${row.slot}`, [ x + 12, top, ITEM_SLOT, ITEM_SLOT ], slot.item, view.potionStock );
		const checkX = x + 12 + ITEM_SLOT + 8;
		const labelTop = top + (ITEM_SLOT - ROW) / 2;
		pen.check(
			`check:${row.slot}`,
			row.caption,
			[ checkX, labelTop, row.gauge ? 170 : x + width - 12 - checkX, ROW ],
			on
		);
		if ( !row.gauge ) return;
		const stepX = x + width - 150;
		pen.button( `percent:${row.slot}:-1`, "-", stepX, labelTop, STEP_WIDTH, !on );
		pen.text(
			`${slot.a || MACRO_PERCENT_DEFAULT}%`,
			[ stepX + STEP_WIDTH, labelTop, 60, ROW ],
			on ? GOLD : DIM,
			1
		);
		pen.button( `percent:${row.slot}:1`, "+", stepX + STEP_WIDTH + 60, labelTop, STEP_WIDTH, !on );
	} );
}

/*
================
skillTab

CNIFMacroAutoSkillWnd (macro_2.2dt): Attack / Buff sub-tabs, then a
two-column body — Acquired Skills (left, scrollable) and Weapon / Shield
over Skills to Use (right, scrollable). Click an acquired skill to append
it to the active list; click a bound slot to remove it.
================
*/
function skillTab( pen: Pen, view: MacroWindowView, draft: MacroSettings, body: UiRect ) {
	const [x, y, width, height] = body;
	const pitch = SKILL_ICON + SKILL_GAP;
	const page = view.hud.skillPage;
	const tabW = Math.floor( (width - 24) / SKILL_PAGES.length );
	SKILL_PAGES.forEach( ( entry, i ) => {
		pen.button(
			`skill-page:${entry.id}`,
			entry.caption,
			x + 8 + i * (tabW + 8),
			y + 6,
			tabW,
			false,
			page === entry.id
		);
	} );
	pen.text( SKILL_HELP, [ x + 8, y + 34, width - 16, 18 ], DIM );
	const gridTop = y + 56;
	const gridBottom = y + height - 8;
	const colGap = 12;
	const colW = Math.floor( (width - 16 - colGap) / 2 );
	const left = x + 8;
	const right = left + colW + colGap;
	pen.heading( "Acquired Skills", left, gridTop, colW );
	const acquiredTop = gridTop + 20;
	const acquiredH = gridBottom - acquiredTop;
	const acquiredCols = Math.max( 1, Math.floor( (colW - SKILL_SCROLL + SKILL_GAP) / pitch ) );
	const acquiredRows = Math.max( 1, Math.floor( acquiredH / pitch ) );
	const acquiredTotalRows = Math.max( 1, Math.ceil( view.acquired.length / acquiredCols ) );
	const acquiredMax = Math.max( 0, acquiredTotalRows - acquiredRows );
	const acquiredScroll = Math.min( view.hud.acquiredScroll, acquiredMax );
	const acquiredOrigin = acquiredScroll * acquiredCols;
	const acquiredGrid: UiRect = [
		left,
		acquiredTop,
		acquiredCols * pitch - SKILL_GAP,
		acquiredRows * pitch - SKILL_GAP
	];
	pen.wheelZone( "wheel:acquired", acquiredGrid );
	for ( let i = 0; i < acquiredCols * acquiredRows; i++ ) {
		const skill = view.acquired[acquiredOrigin + i];
		const r: UiRect = [
			left + (i % acquiredCols) * pitch,
			acquiredTop + Math.floor( i / acquiredCols ) * pitch,
			SKILL_ICON,
			SKILL_ICON
		];
		if ( skill ) {
			const bound = macroSkillBound( draft, skill, view.skillIndex );
			const fits = macroSkillFitsPage( skill, page );
			const blocked = bound || !fits;
			pen.skillSlot(
				blocked ? undefined : `skill-add:${skill.id}`,
				bound ?
					`${skill.name} (assigned)` :
					!fits ?
					`${skill.name} (${page === "attack" ? "buff" : "attack"} only)` :
					skill.name,
				r,
				view.iconPath( skill.icon ) ?? undefined
			);
		} else pen.skillSlot( undefined, "", r, undefined );
	}
	if ( acquiredMax > 0 ) {
		pen.scrollBar(
			"acquired-scroll",
			[ left + acquiredGrid[2] + 2, acquiredTop, SKILL_SCROLL, acquiredGrid[3] ],
			acquiredScroll,
			acquiredMax
		);
	}

	const gearKinds: readonly MacroGearKind[] = [ "weapon", "shield" ];
	const gearPitch = GEAR_SLOT + 48;
	gearKinds.forEach( ( kind, i ) => {
		const gx = right + i * gearPitch;
		pen.gearSlot(
			kind,
			[ gx, gridTop, GEAR_SLOT, GEAR_SLOT ],
			macroGearBagSlot( draft.skillFlags, kind ),
			view.inventory,
			view.iconPath
		);
		pen.text(
			kind === "weapon" ? "Weapon" : "Shield",
			[ gx, gridTop + GEAR_SLOT + 2, GEAR_SLOT + 8, 16 ],
			DIM,
			1
		);
	} );
	const useTop = gridTop + GEAR_SLOT + 22;
	pen.heading( "Skills to Use", right, useTop, colW );
	const useGridTop = useTop + 20;
	const useH = gridBottom - useGridTop;
	const useCols = Math.max( 1, Math.floor( (colW - SKILL_SCROLL + SKILL_GAP) / pitch ) );
	const useRows = Math.max( 1, Math.floor( useH / pitch ) );
	const useTotalRows = Math.ceil( MACRO_SKILL_GRID / useCols );
	const useMax = Math.max( 0, useTotalRows - useRows );
	const useScroll = Math.min( view.hud.skillScroll, useMax );
	const useOrigin = useScroll * useCols;
	const list = page === "attack" ? draft.attack : draft.buff;
	const useGrid: UiRect = [
		right,
		useGridTop,
		useCols * pitch - SKILL_GAP,
		useRows * pitch - SKILL_GAP
	];
	pen.wheelZone( "wheel:use", useGrid );
	for ( let i = 0; i < useCols * useRows; i++ ) {
		const index = useOrigin + i;
		if ( index >= MACRO_SKILL_GRID ) break;
		const r: UiRect = [
			right + (i % useCols) * pitch,
			useGridTop + Math.floor( i / useCols ) * pitch,
			SKILL_ICON,
			SKILL_ICON
		];
		const entry = list[index];
		if ( entry ) {
			const skill = view.skillIndex?.get( entry.skill );
			pen.skillSlot(
				`skill-remove:${page}:${index}`,
				skill?.name ?? `#${entry.skill}`,
				r,
				view.iconPath( skill?.icon ) ?? undefined
			);
		} else pen.skillSlot( undefined, "", r, undefined );
	}
	if ( useMax > 0 ) {
		pen.scrollBar(
			"skill-scroll",
			[ right + useGrid[2] + 2, useGridTop, SKILL_SCROLL, useGrid[3] ],
			useScroll,
			useMax
		);
	}
}

/*
================
huntTab

CNIFMacroAutoHunting: each option group cycles its choice on click; the
check boxes below.
================
*/
function huntTab( pen: Pen, draft: MacroSettings, body: UiRect ) {
	const [x, y, width] = body;
	MACRO_HUNT_GROUPS.forEach( ( group, i ) => {
		const top = y + 8 + i * (ROW + 4);
		pen.text( group.caption, [ x + 12, top, width - 200, ROW ] );
		const choice = group.choices[draft.hunt[group.byte] ?? 0] ?? group.choices[0]!;
		pen.button( `hunt-group:${group.byte}`, choice, x + width - 184, top, 172 );
	} );
	const checksTop = y + 8 + MACRO_HUNT_GROUPS.length * (ROW + 4) + 8;
	MACRO_HUNT_CHECKS.forEach( ( check, i ) => {
		pen.check(
			`hunt-check:${check.byte}`,
			check.caption,
			[ x + 12, checksTop + i * ROW, width - 24, ROW ],
			(draft.hunt[check.byte] ?? 0) !== 0
		);
	} );
}
