/*
===========================================================================

party-proposal.ts - the geometry of the peer proposal boxes

The party, guild and union proposal boxes CIFMessageBox_ConfigureByType
(52F460) builds over the MsgBoxINIF geometry: frame size, the name and
question lines and the two answer buttons.

===========================================================================
*/
import type { UiRect } from "@/engine/contracts/ui";
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

// REVERSE_RETURN_ROW is the reverse return box's row pitch (26 px).
const REVERSE_RETURN_ROW = 26;

/*
================
reverseReturnLayout

The reverse return box: guildProposalLayout's 308 x 148 box for its two
native rows. A port-only third row (the map, reverse-return-map.ts) grows
the box and moves Cancel down by one row pitch.
================
*/
export function reverseReturnLayout( width: number, height: number, rows: 2 | 3 ) {
	const extra = (rows - 2) * REVERSE_RETURN_ROW;
	const box = messageBox( width, height, 308, 148 + extra ), [x, y] = box.frame;
	return { ...box, refuse: [ x + 152, y + 99 + extra, 76, 24 ] as UiRect };
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
