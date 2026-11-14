import { messageBox } from "./message-box";
import { textBoxLines } from "./text-lines";
import { noticeText } from "./notice-text";
// An answered dialog's two buttons: wide enough for a short phrase each.
const ANSWER_WIDTH = 104;
const ANSWER_GAP = 4;
// 52A0F0 sets line spacing 23. 52DB20 wraps plain text at 600 before
// 52BCF0 expands the 360x151 minimum. 52E720 positions the body at 30,65.
export function noticeDialog(
	width: number,
	height: number,
	value: string,
	measure: ( value: string ) => number,
	position: readonly [number, number] | null = null,
	answered = false
) {
	const lines = textBoxLines( value, 600, measure ),
		bodyWidth = Math.max( 300, ...lines.map( measure ) ),
		bodyHeight = Math.max( 29, lines.length * 23 );
	const box = messageBox( width, height, bodyWidth + 60, bodyHeight + 122, position ), [x, y, w, h] = box.frame;
	return {
		...box,
		lines,
		body: [ x + 30, y + 65, bodyWidth, bodyHeight ] as const,
		// An answered dialog (port-only) sets two buttons side by side under
		// the body where the native one stands centred.
		confirm: answered ?
			[ x + Math.trunc( w / 2 ) - ANSWER_WIDTH - ANSWER_GAP, y + h - 37, ANSWER_WIDTH, 24 ] as const :
			[ x + Math.trunc( w / 2 ) - 38, y + h - 37, 76, 24 ] as const,
		cancel: [ x + Math.trunc( w / 2 ) + ANSWER_GAP, y + h - 37, ANSWER_WIDTH, 24 ] as const
	};
}

/*
================
noticeDialogLines

The text a notice dialog shows: each line key's copy with the
notice's arguments applied, localized arguments included (the
fortress position completion's one line names the member and the
position title). Lines join with newlines for noticeDialog's wrap.
Formatting is noticeText's, the same projection the guide uses.
================
*/
export function noticeDialogLines(
	copy: ( key: string ) => string,
	dialog: {
		readonly lines: readonly string[];
		readonly arguments?: readonly string[];
		readonly localizedArguments?: readonly (string | null)[];
	}
): string {
	return dialog.lines
		.map( key =>
			noticeText( copy, {
				key,
				value: 0,
				arguments: dialog.arguments,
				localizedArguments: dialog.localizedArguments
			} )
		)
		.join( "\n" );
}
