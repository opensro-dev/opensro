/*
===========================================================================

instance-proposal.ts - the quest instance invitation box: line and position

0x3393 type 7 (0x7644E0's type-7 arm) opens the simple confirm box tagged
simple_msgbox_type 4 (the box site at 0x75C160): caption
UIIT_STT_CONFIRM_BOX, one question line, and a yes/no answer the parent
forwards as {1, button}. The box has its own social slot and so owns its
dragged position, separate from the invitation and resurrection boxes
beside which it can be open.

Geometry is the shared CIFMessageBox proposal geometry (partyProposalLayout
over MsgBoxINIF): the box is the same window family as the other 0x3393
proposals. Its own ConfigureByType case was not separately traced, so the
shared frame is an inference, recorded here; the question, the answer pair
and the dead-confirm gate (0x52EDD0) are traced behaviour, not inference.

===========================================================================
*/
import type { UiRect } from "@/engine/contracts/ui";
import { partyProposalLayout } from "./party-proposal";

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

The shared proposal geometry with the question line only: this box carries
no inviter name line.
================
*/
export function instanceBoxLayout(
	width: number,
	height: number,
	position: readonly [number, number] | null
): {
	frame: UiRect;
	drag: UiRect;
	title: UiRect;
	background: UiRect;
	question: UiRect;
	accept: UiRect;
	refuse: UiRect;
} {
	const box = partyProposalLayout( width, height, undefined, position );
	return {
		frame: box.frame,
		drag: box.drag,
		title: box.title,
		background: box.background,
		question: box.question,
		accept: box.accept,
		refuse: box.refuse
	};
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
