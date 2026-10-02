/*
===========================================================================

resurrection-proposal.ts - the resurrection question box: lines and position

0x3393 type 4 opens box kind 4 (7644E0): a plain text message box with a
Yes and a No button, laid out by textMessageBoxLayout. The question has its
own social slot and its own box, so it also owns its dragged position,
separate from the invitation box beside which it can be open.

===========================================================================
*/

/*
================
resurrectionQuestion

The system-text keys of the question's lines, top to bottom.

Inferred without the research corpus: these lines are certain only for a
proposal without an rmut skill (Reverse, Grad Reverse, Group Reverse and
Holy Group Reverse). The rmut rows are Reverse Oblation and Reverse
Immolation (REBIRTHA_SPECIAL): rmut names their transformation buff
(10268..10272, 10275), and the text data also has
UIIT_MSG_MSGBOX_ASK_REBIRTH_MUTATION, which describes that altered-form
revival; native most likely shows it for those two. The ported type-4
wire has no field that tells the two apart (the server folds native's
rmut proposal, type 8, into type 4), so no selection rule is made up here.
Settling it needs the 7644E0 / 52F460 box selection and whether the
v1.150 0x3393 has a separate mutation type.
================
*/
export function resurrectionQuestion(): readonly string[] {
	return [
		"UIIT_MSG_MSGBOX_ASK_SKL_RESURRECTION_0",
		"UIIT_MSG_MSGBOX_ASK_SKL_RESURRECTION_1",
		"UIIT_MSG_MSGBOX_ASK_SKL_RESURRECTION_2"
	];
}

/*
================
createResurrectionPrompt

The dragged position of the open question. A question from another caster
(or the next one after it closes) opens centred again; 0 means none is open.
================
*/
export function createResurrectionPrompt() {
	let caster = 0;
	let position: readonly [number, number] | null = null;
	return {
		/*
================
sync
================
		*/
		sync( gid: number ) {
			if ( gid !== caster ) {
				caster = gid;
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
place
================
		*/
		place( frame: readonly [number, number] ) {
			position = frame;
		}
	};
}
