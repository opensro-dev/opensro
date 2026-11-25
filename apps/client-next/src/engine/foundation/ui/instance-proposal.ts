/*
===========================================================================

instance-proposal.ts - the quest instance invitation box: line and position

0x3393 type 7 (0x7644E0's type-7 arm) opens the simple confirm box tagged
simple_msgbox_type 4 (the box site at 0x75C160): caption
UIIT_STT_CONFIRM_BOX, one question line, and a yes/no answer the parent
forwards as {1, button}. The box has its own social slot and so owns its
dragged position, separate from the invitation and resurrection boxes
beside which it can be open.

Geometry is the simple message box's: 7644E0 opens it through
CGInterface_ShowSimpleMessageBox (6888C0, Yes/No type 1), which creates
the box and lays it out by its text (CIFSimpleMessageBox_LayoutControls
52E720, at least 360x151: textMessageBoxLayout), not the party proposal
frame #2334 had inferred (#4403).

===========================================================================
*/
import type { UiRect } from "@/engine/contracts/ui";
import { messageBoxLines, textMessageBoxLayout } from "./text-message-box";

/*
================
instanceQuestion

The box's one question line.
================
*/
export function instanceQuestion(): readonly string[] {
	return [ "UIIT_MSG_QUEST_ENTER_INSTANCE" ];
}

/*
================
instanceBoxLayout

The simple message box around the question, wrapped and sized by its
text as every CGInterface_ShowSimpleMessageBox box is. measure is the
HUD font's run width; question is the resolved line.
================
*/
export function instanceBoxLayout( view: InstanceBoxView, position: readonly [number, number] | null ): {
	frame: UiRect;
	drag: UiRect;
	title: UiRect;
	background: UiRect;
	lines: readonly { text: string; rect: UiRect; }[];
	accept: UiRect;
	refuse: UiRect;
} {
	const lines = messageBoxLines( [ view.question ], view.measure ),
		box = textMessageBoxLayout( view.width, view.height, lines.map( view.measure ), position );
	return {
		frame: box.frame,
		drag: box.drag,
		title: box.title,
		background: box.background,
		lines: lines.map( ( text, i ) => ({ text, rect: box.lines[i]! }) ),
		accept: box.accept,
		refuse: box.refuse
	};
}

/*
================
InstanceBoxView

What the layout needs from the screen: its size, the resolved question
and the HUD font's measure.
================
*/
export interface InstanceBoxView {
	readonly width: number;
	readonly height: number;
	readonly question: string;
	readonly measure: ( value: string ) => number;
}

/*
================
createInstancePrompt

The dragged position of the open invitation. An invitation from another
leader (or the next one after it closes) opens centred again; 0 means
none is open.
================
*/
export function createInstancePrompt() {
	let leader = 0;
	let position: readonly [number, number] | null = null;
	return {
		/*
================
sync
================
		*/
		sync( gid: number ) {
			if ( gid !== leader ) {
				leader = gid;
				position = null;
			}
		},
		/*
================
position
================
		*/
		position(): readonly [number, number] | null {
			return position;
		},
		/*
================
opens

Whether gid's invitation is new: another leader, or the first since none.
================
		*/
		opens( gid: number ): boolean {
			return gid !== 0 && gid !== leader;
		},
		/*
================
place
================
		*/
		place( frame: readonly [number, number] ) {
			position = frame;
		}
	};
}
