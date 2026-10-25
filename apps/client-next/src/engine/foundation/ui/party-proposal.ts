/*
===========================================================================

party-proposal.ts - the geometry of the peer proposal boxes

The party, guild and union proposal boxes CIFMessageBox_ConfigureByType
(52F460) builds over the MsgBoxINIF geometry: frame size, the name and
question lines and the two answer buttons.

===========================================================================
*/
import type { UiRect } from "@/engine/contracts/ui";
import type { AuthoredControl } from "./authored-layout";
import { frameParts } from "./frame-ring";
import { messageBox } from "./message-box";

export const MESSAGE_FRAME = "/assets/images/Media_extracted/interface/messagebox/msgbox2_window_";
export const MESSAGE_TILE = "/assets/images/Media_extracted/interface/ifcommon/bg_tile/com_bg_tile_b.png";
export const PARTY_OPTION = "/assets/images/Media_extracted/interface/messagebox/msgbox_blackbox.png";

// 52F460 case 0x1C: the union box resizes to 470x144 and moves the name
// line by 0x4C and both buttons by 0x51 from the guild box's places.
const UNION_BOX_WIDTH = 470;
const UNION_BOX_HEIGHT = 144;
const UNION_LINE_SHIFT = 0x4c;
const UNION_BUTTON_SHIFT = 0x51;

/*
================
partyProposalAssets
================
*/
export function partyProposalAssets() {
	return [ MESSAGE_TILE, PARTY_OPTION, ...frameParts().map( p => MESSAGE_FRAME + p + ".png" ) ];
}

/*
================
partyProposalLayout

52F460 case 7 (5305D0..530988), 525D60 and MsgBoxINIF. 7644E0 passes
zero option bits for inbound type-1 invitations. The option art's natural
extent and (3,7,3,6) client insets are native; the old port's 100x14
stretched option rectangles are not.
================
*/
export function partyProposalLayout(
	width: number,
	height: number,
	optionSize: readonly [number, number] | undefined,
	position: readonly [number, number] | null = null
) {
	const box = messageBox( width, height, 300, 176, position ), [x, y] = box.frame;
	const at = ( dx: number, dy: number, w: number, h: number ): UiRect => [ x + dx, y + dy, w, h ];
	return {
		...box,
		name: at( 16, 48, 300, 12 ),
		question: at( 16, 64, 300, 12 ),
		options: optionSize ?
			[ 44, 156 ].map( dx => ({
				image: at( dx, 93, ...optionSize ),
				text: at( dx + 3, 100, optionSize[0] - 6, optionSize[1] - 13 )
			}) ) :
			[],
		accept: at( 72, 139, 76, 24 ),
		refuse: at( 152, 139, 76, 24 )
	};
}

/*
================
guildProposalLayout

7644E0 type 5 -> kind 15; 52F460 retains the MsgBoxINIF geometry.
================
*/
export function guildProposalLayout(
	width: number,
	height: number,
	position: readonly [number, number] | null = null
) {
	const box = messageBox( width, height, 308, 148, position ), [x, y] = box.frame;
	const at = ( dx: number, dy: number, w: number, h: number ): UiRect => [ x + dx, y + dy, w, h ];
	return {
		...box,
		name: at( 0, 52, 300, 12 ),
		question: at( 6, 70, 300, 12 ),
		options: [],
		accept: at( 72, 99, 76, 24 ),
		refuse: at( 152, 99, 76, 24 )
	};
}

// The message's room in guildProposalLayout: from its top (52) to the
// buttons (99) with a 4-pixel gap, or to the alias field (70) above it.
const QUESTION_MESSAGE_ROOM = 43;
const QUESTION_MESSAGE_ROOM_ABOVE_FIELD = 18;
// The message's left inset within the 308-pixel box.
const QUESTION_MESSAGE_INSET = 4;

/*
================
guildQuestionLayout

guildProposalLayout grown to fit a message measureHeight tall, as
CIFSimpleMessageBox_ResizeToText (52BCF0) grows the native box to its text:
the box gains the overflow, and the field and buttons below move down by
it. A message that fits leaves the port's 308 x 148 box unchanged (the
native box is 360 x 151, 6888C0).
================
*/
export function guildQuestionLayout( width: number, height: number, messageHeight: number, field: boolean ) {
	const room = field ? QUESTION_MESSAGE_ROOM_ABOVE_FIELD : QUESTION_MESSAGE_ROOM;
	const extra = Math.max( 0, Math.ceil( messageHeight ) - room );
	const box = messageBox( width, height, 308, 148 + extra ), [x, y] = box.frame;
	const at = ( dx: number, dy: number, w: number, h: number ): UiRect => [ x + dx, y + dy, w, h ];
	return {
		...box,
		// Inset like the native message control (52E720 places it inside the frame).
		name: at( QUESTION_MESSAGE_INSET, 52, 300, Math.max( 12, Math.ceil( messageHeight ) ) ),
		question: at( 6, 70 + extra, 300, 12 ),
		options: [],
		accept: at( 72, 99 + extra, 76, 24 ),
		refuse: at( 152, 99 + extra, 76, 24 )
	};
}

// 52F460 types 0x21 and 0x24 (53259C..5327AE) give the MsgBoxINIF box
// three stacked buttons in this art: ids 200 and 202 the two points, 201
// Cancel. CIFButton's texture setter (541100, slot 0x34 of its +0x84
// vtable) derives the _focus, _press and _disable siblings.
export const REVERSE_RETURN_BUTTON = "/assets/images/Media_extracted/interface/ifcommon/com_return_button.png";
// The box (5325CF), the question line (id 1, 5325EF and 532608) and the
// buttons (53264A..5326DA): 252 x 24 at x 25, from y 76 at a 31 px pitch.
const REVERSE_RETURN_WIDTH = 300;
const REVERSE_RETURN_HEIGHT = 180;
const REVERSE_RETURN_BUTTON_X = 25;
const REVERSE_RETURN_BUTTON_Y = 76;
const REVERSE_RETURN_BUTTON_WIDTH = 252;
const REVERSE_RETURN_BUTTON_HEIGHT = 24;
const REVERSE_RETURN_ROW = 31;

/*
================
reverseReturnLayout

The reverse return box: the question, then one button per point and
Cancel under them. A port-only third point (the map, reverse-return-map.ts)
takes Cancel's place, and Cancel and the box move down by one row pitch.
================
*/
export function reverseReturnLayout( width: number, height: number, rows: 2 | 3 ) {
	const extra = (rows - 2) * REVERSE_RETURN_ROW;
	const box = messageBox( width, height, REVERSE_RETURN_WIDTH, REVERSE_RETURN_HEIGHT + extra ),
		[x, y] = box.frame;
	const row = ( index: number ): UiRect => [
		x + REVERSE_RETURN_BUTTON_X,
		y + REVERSE_RETURN_BUTTON_Y + index * REVERSE_RETURN_ROW,
		REVERSE_RETURN_BUTTON_WIDTH,
		REVERSE_RETURN_BUTTON_HEIGHT
	];
	return {
		...box,
		question: [ x + 27, y + 48, 246, 20 ] as UiRect,
		points: Array.from( { length: rows }, ( _, index ) => row( index ) ),
		refuse: row( rows )
	};
}

/*
================
reverseReturnButton

One of the box's buttons at r: a MsgBoxINIF button (GDR_MBIN_BTN_OK,
_CERTIFY or _CANCEL, all authored alike: font 0, colour 254,251,216,
client inset 0,6,0,0, centred, top-aligned) wearing the return art.
================
*/
export function reverseReturnButton( r: UiRect ): AuthoredControl {
	return {
		name: "",
		id: 0,
		type: "CIFButton",
		rect: r,
		client: [ 0, 6, 0, 0 ],
		uv: [ 0, 0, 1, 1 ],
		texture: REVERSE_RETURN_BUTTON,
		size: [ REVERSE_RETURN_BUTTON_WIDTH, REVERSE_RETURN_BUTTON_HEIGHT ],
		text: "",
		color: [ 254 / 255, 251 / 255, 216 / 255, 1 ],
		fontIndex: 0,
		hAlign: 1,
		vAlign: 0
	};
}

/*
================
unionProposalLayout

7644E0 type 6 -> kind 0x1D: one line carries the whole question, the
second line stays unset.
================
*/
export function unionProposalLayout(
	width: number,
	height: number,
	position: readonly [number, number] | null = null
) {
	const box = messageBox( width, height, UNION_BOX_WIDTH, UNION_BOX_HEIGHT, position ), [x, y] = box.frame;
	const at = ( dx: number, dy: number, w: number, h: number ): UiRect => [ x + dx, y + dy, w, h ];
	return {
		...box,
		name: at( UNION_LINE_SHIFT, 52, 300, 12 ),
		question: at( 6, 70, 0, 0 ),
		options: [],
		accept: at( 72 + UNION_BUTTON_SHIFT, 99, 76, 24 ),
		refuse: at( 152 + UNION_BUTTON_SHIFT, 99, 76, 24 )
	};
}

/*
================
proposalLayout

The box an inbound 0x3393 kind opens: 5 the guild box, 6 the union box,
the party box otherwise.
================
*/
export function proposalLayout(
	type: number | undefined,
	width: number,
	height: number,
	optionSize: readonly [number, number] | undefined,
	position: readonly [number, number] | null = null
) {
	if ( type === 5 ) return guildProposalLayout( width, height, position );
	if ( type === 6 ) return unionProposalLayout( width, height, position );
	return partyProposalLayout( width, height, optionSize, position );
}
