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
	type MacroKind,
	macroOn,
	type MacroSettings,
	potionSlot
} from "@/engine/foundation/gameplay/sro-macro";
import type { UiAuthoredFrame } from "../frame/frame-contract";
import {
	MACRO_ACQUIRED_ROWS,
	MACRO_HUNT_CHECKS,
	MACRO_HUNT_GROUPS,
	MACRO_ID,
	MACRO_PERCENT_DEFAULT,
	MACRO_POTION_ROWS,
	MACRO_SKILL_GRID,
	type MacroHudState
} from "./sro-macro-hud";

const IFCOMMON = "/assets/images/Media_extracted/interface/ifcommon/";
const CHECK_SIZE = 16;
const BUTTON_HEIGHT = 25;
const ROW = 26;
const MARGIN = 16;
const TITLE = "SRO Macro";
const MENU_WIDTH = 300;
const MENU_HEIGHT = 168;
const MENU_TOP = 44;
const MENU_PITCH = 36;
const OPTIONS_WIDTH = 460;
const OPTIONS_HEIGHT = 470;
const TAB_TOP = 40;
const BODY_TOP = 76;
const TAB_WIDTH = 136;
const STEP_WIDTH = 26;
const GOLD: UiQuad["color"] = [ 252 / 255, 210 / 255, 100 / 255, 1 ];
const DIM: UiQuad["color"] = [ 170 / 255, 170 / 255, 160 / 255, 1 ];
const LINE: UiQuad["color"] = [ .55, .45, .24, .3 ];
const PANEL: UiQuad["color"] = [ .02, .03, .025, .82 ];
// An acquired skill's colour: white attacks (a target required), gold buffs.
const ATTACK: UiQuad["color"] = [ 1, 1, 1, 1 ];

// The v1.696 section names (UIIT_STT_SRO_MACRO_AUTO_POTION/SKILL/HUNTING).
const SECTION_NAMES: Readonly<Record<MacroKind, string>> = {
	[MACRO_POTION]: "Auto-Potion",
	[MACRO_SKILL]: "Auto-Skills",
	[MACRO_HUNT]: "Auto-Hunt"
};
const SECTIONS = [ MACRO_POTION, MACRO_SKILL, MACRO_HUNT ] as const;

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
	const [px, py] = paint.windowOrigin( frame, {
		name: "SroMacroOptions",
		initial: [
			Math.max( 0, (frame.w - OPTIONS_WIDTH) / 2 ),
			Math.max( 0, (frame.h - OPTIONS_HEIGHT) / 3 ),
			OPTIONS_WIDTH,
			OPTIONS_HEIGHT
		],
		id: "window-drag:SroMacroOptions"
	} );
	paint.windowBox( frame, { title: TITLE, x: px, y: py, width: OPTIONS_WIDTH, height: OPTIONS_HEIGHT } );
	paint.closeButton( frame, px + OPTIONS_WIDTH - 26, py + 10, "sro-macro-close" );
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
	const body: UiRect = [ px + MARGIN, py + BODY_TOP, OPTIONS_WIDTH - MARGIN * 2, OPTIONS_HEIGHT - BODY_TOP - 52 ];
	pen.panel( body );
	if ( view.hud.tab === MACRO_POTION ) potionTab( pen, view, draft, body );
	else if ( view.hud.tab === MACRO_SKILL ) skillTab( pen, view, draft, body );
	else huntTab( pen, draft, body );
	const footer = py + OPTIONS_HEIGHT - 42;
	pen.button( "save", "Save", px + OPTIONS_WIDTH / 2 - 110, footer, 100 );
	pen.button( "cancel", "Cancel", px + OPTIONS_WIDTH / 2 + 10, footer, 100 );
	paint.endWindow( frame, admission, "sro-macro" );
}

/*
================
potionTab

CNIFMacroAutoPotionWnd: Character and Pet pages of rows. A gauge row is
a check box and its percent with - and +; the others a check box.
================
*/
function potionTab( pen: Pen, view: MacroWindowView, draft: MacroSettings, body: UiRect ) {
	const [x, y, width] = body;
	const page = view.hud.potionPage;
	pen.button( "page:character", "Character", x + 8, y + 8, 110, false, page === "character" );
	pen.button( "page:pet", "Pet", x + 124, y + 8, 110, false, page === "pet" );
	MACRO_POTION_ROWS[page].forEach( ( row, i ) => {
		const top = y + 48 + i * (ROW + 8);
		const slot = potionSlot( draft.potion, row.slot ), on = slot.a !== 0;
		pen.check( `check:${row.slot}`, row.caption, [ x + 12, top, row.gauge ? 200 : width - 24, ROW ], on );
		if ( !row.gauge ) return;
		const stepX = x + width - 150;
		pen.button( `percent:${row.slot}:-1`, "-", stepX, top, STEP_WIDTH, !on );
		pen.text( `${slot.b || MACRO_PERCENT_DEFAULT}%`, [ stepX + STEP_WIDTH, top, 60, ROW ], on ? GOLD : DIM, 1 );
		pen.button( `percent:${row.slot}:1`, "+", stepX + STEP_WIDTH + 60, top, STEP_WIDTH, !on );
	} );
}

/*
================
skillTab

CNIFMacroAutoSkillWnd: the attack and buff lists (click removes) over
the acquired skills (click assigns: a skill that needs a target attacks,
the rest buff).
================
*/
function skillTab( pen: Pen, view: MacroWindowView, draft: MacroSettings, body: UiRect ) {
	const [x, y, width] = body;
	const column = (width - 24) / 2;
	const lists = [
		[ "attack", "Configure Attack Skills", draft.attack ],
		[ "buff", "Configure Buff Skills", draft.buff ]
	] as const;
	const listRow = 20;
	lists.forEach( ( [key, caption, list], c ) => {
		const left = x + 8 + c * (column + 8);
		pen.heading( caption, left, y + 4, column );
		for ( let i = 0; i < MACRO_SKILL_GRID; i++ ) {
			const r: UiRect = [ left, y + 30 + i * listRow, column, listRow ];
			const entry = list[i];
			if ( entry ) {
				pen.row(
					`skill-remove:${key}:${i}`,
					view.skillIndex?.get( entry.skill )?.name ?? `#${entry.skill}`,
					r
				);
			} else pen.text( "-", [ r[0] + 4, r[1], r[2], r[3] ], DIM );
		}
	} );
	const top = y + 30 + MACRO_SKILL_GRID * listRow + 10;
	pen.heading( "Acquired Skills", x + 8, top, width - 16 );
	const page = view.hud.acquiredPage;
	const shown = view.acquired.slice( page * MACRO_ACQUIRED_ROWS, (page + 1) * MACRO_ACQUIRED_ROWS );
	shown.forEach( ( skill, i ) => {
		const r: UiRect = [ x + 8 + (i % 2) * (column + 8), top + 26 + Math.floor( i / 2 ) * listRow, column, listRow ];
		pen.row( `skill-add:${skill.id}`, skill.name, r, skill.targetRequired ? ATTACK : GOLD );
	} );
	const pages = Math.max( 1, Math.ceil( view.acquired.length / MACRO_ACQUIRED_ROWS ) );
	const pagerY = top + 26 + (MACRO_ACQUIRED_ROWS / 2) * listRow + 4;
	pen.button( "acquired:-1", "<", x + width / 2 - 70, pagerY, STEP_WIDTH, page === 0 );
	pen.text( `${page + 1} / ${pages}`, [ x + width / 2 - 40, pagerY, 80, BUTTON_HEIGHT ], DIM, 1 );
	pen.button( "acquired:1", ">", x + width / 2 + 44, pagerY, STEP_WIDTH, page >= pages - 1 );
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
