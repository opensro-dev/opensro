/*
===========================================================================

gacha-select-layout.ts - CIFGhachaSelectWnd's layout and the page spinner

v1.150 ships neither resinfo\ifghachaselectwnd.txt nor ifghacha.txt's
GhachaSelect section and GACHA_SECOND_VER spinner, so the asset build has no
layout for them. These are the v1.696 Media.pk2 records transcribed as
authored controls (rects, colours, alignment, ids and text keys). No art is
new: every texture they name is already published for v1.150 windows
(msgbox2_window_, opt_inner_box_, com_bg_tile_b, com_button).

Port note: shipping the later resinfo files through the layout build would
also work; transcribing a dozen numbers keeps the build reading only the
v1.150 tree.

===========================================================================
*/
import type { AuthoredControl, AuthoredLayout } from "./authored-layout";
import type { UiRect } from "@/engine/contracts/ui";

const MEDIA = "/assets/images/Media_extracted/";
const BYTE = 255;
const ALIGN_LEFT = 0;
const ALIGN_CENTER = 1;
const ALIGN_RIGHT = 2;

// ifghacha.txt GDR_GHACHA_SPIN_PAGE (CIFSpinButtonCtrl, id 700, under
// GACHA_SECOND_VER): drawn with the ifspincontrol.txt children, as every
// CIFSpinButtonCtrl is.
export const GACHA_SPIN_PAGE_RECT = [ 222, 137, 50, 16 ] as const;

// The Start button and the three combo boxes (ifghachaselectwnd.txt ids).
export const GACHA_SELECT_START_ID = 10;
export const GACHA_SELECT_CAPTION_IDS = [ 610, 611, 612 ] as const;

/*
================
control

One transcribed record with the defaults every control here shares.
================
*/
function control(
	name: string,
	id: number,
	type: string,
	rect: UiRect,
	extra: Partial<AuthoredControl> = {}
): AuthoredControl {
	return {
		name,
		id,
		type,
		rect,
		client: [ 0, 0, 0, 0 ],
		uv: [ 0, 0, 1, 1 ],
		texture: "",
		size: [ 0, 0 ],
		text: "",
		color: [ 1, 1, 1, 1 ],
		fontIndex: 0,
		hAlign: ALIGN_LEFT,
		vAlign: 0,
		...extra
	};
}

/*
================
caption / buttonText

FontColor 255,239,218,164 (the box captions) and 255,255,245,218 (Start).
================
*/
function caption(): AuthoredControl["color"] {
	return [ 239 / BYTE, 218 / BYTE, 164 / BYTE, 1 ];
}
function buttonText(): AuthoredControl["color"] {
	return [ 1, 245 / BYTE, 218 / BYTE, 1 ];
}

/*
================
gachaSelectRoot

ifghacha.txt section GhachaSelect: GDR_GHACHA_SELECT_WND, window 600, a
319x257 msgbox2 frame.
================
*/
export function gachaSelectRoot(): AuthoredControl {
	return control( "GDR_GHACHA_SELECT_WND", 600, "CIFGhachaSelectWnd", [ 0, 0, 319, 257 ], {
		client: [ 0, 2, 0, 0 ],
		texture: MEDIA + "interface/messagebox/msgbox2_window_"
	} );
}

/*
================
gachaSelectLayout

ifghachaselectwnd.txt section Create, in file order (authoredPaintOrder
paints it reversed, as 783F80 creates it).
================
*/
export function gachaSelectLayout(): AuthoredLayout {
	return Object.fromEntries( [
		control( "GDR_GHACHA_SELECT_COMBOBOX3", 43, "CIFComboBox", [ 90, 179, 166, 20 ] ),
		control( "GDR_GHACHA_SELECT_COMBOBOX2", 42, "CIFComboBox", [ 90, 128, 166, 20 ] ),
		control( "GDR_GHACHA_SELECT_COMBOBOX1", 41, "CIFComboBox", [ 90, 77, 166, 20 ] ),
		control( "GDR_GHACHA_SELECT_START_BTN", GACHA_SELECT_START_ID, "CIFButton", [ 116, 222, 76, 24 ], {
			client: [ 0, 6, 0, 0 ],
			texture: MEDIA + "interface/ifcommon/com_button.png",
			size: [ 76, 24 ],
			text: "UIIT_STT_GACHA_GAME_START",
			color: buttonText(),
			hAlign: ALIGN_CENTER
		} ),
		control( "GDR_GHACHA_SELECT_SMALL", 612, "CIFStatic", [ 16, 183, 66, 14 ], {
			text: "UIIT_STT_WARENETWORK_SCAN_SMALL",
			color: caption(),
			hAlign: ALIGN_RIGHT
		} ),
		control( "GDR_GHACHA_SELECT_MID", 611, "CIFStatic", [ 16, 132, 66, 14 ], {
			text: "UIIT_STT_WARENETWORK_SCAN_MIDDLE",
			color: caption(),
			hAlign: ALIGN_RIGHT
		} ),
		control( "GDR_GHACHA_SELECT_BIG", 610, "CIFStatic", [ 16, 81, 66, 14 ], {
			text: "UIIT_STT_WARENETWORK_SCAN_BIG",
			color: caption(),
			hAlign: ALIGN_RIGHT
		} ),
		control( "GDR_GHACHA_SELECT_FRAME_TITLE", 604, "CIFStatic", [ 79, 47, 162, 14 ], {
			text: "UIIT_STT_GACHA_ITEM_CHOICE",
			hAlign: ALIGN_CENTER
		} ),
		control( "GDR_GHACHA_SELECT_INNER_BOX", 603, "CIFFrame", [ 14, 43, 291, 171 ], {
			texture: MEDIA + "interface/option/opt_inner_box_"
		} ),
		control( "GDR_GHACHA_SELECT_TITLE", 602, "CIFStatic", [ 69, 12, 162, 14 ], {
			text: "UIIT_STT_GACHA_WIN_ITEM_CHOICE",
			hAlign: ALIGN_CENTER
		} ),
		control( "GDR_GHACHA_SELECT_BG", 601, "CIFNormalTile", [ 16, 40, 298, 204 ], {
			texture: MEDIA + "interface/ifcommon/bg_tile/com_bg_tile_b.png",
			size: [ 128, 128 ]
		} )
	].map( node => [ node.name, node ] ) );
}
