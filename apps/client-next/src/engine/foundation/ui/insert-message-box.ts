/*
===========================================================================

insert-message-box.ts - MsgBoxInsertMsg's geometry per input mode

CIFMessageBox_SetMessage (52E4F0) kind 4 loads the authored MsgBoxInsertMsg
section; CIFMessageBox_SetInputMode (52C870) then moves its prompt, edit
frame, edit and buttons for one use. The stall's title and greeting boxes
and the guild manager's name box are three of those modes. This module owns
their rects so the draw code has one table to read, not one per caller.

Rects are relative to the box's origin and keyed by the authored control
ID: 10 prompt, 11 and 12 the edit's frame children, 15 the edit, 205 the
Confirm button, 206 Cancel.

===========================================================================
*/

/*
================
InsertMessageMode

The 52C870 modes this port draws: 0 stall title, 1 stall greeting,
2 guild name, 4 friend name, 6 pet name, 7 block entry.
================
*/
export type InsertMessageMode = 0 | 1 | 2 | 4 | 6 | 7;

/*
================
INSERT_MESSAGE_GEOMETRY

Sizes come from the window's creation: 5A1DF0 for the stall boxes, and
CIFNPCTalk_ShowGuildCreateInput 5D2540 (0x118 x 0x90) for the guild box.
Mode 2's prompt, frame children and edit length are set at 52CD73..52CE7E;
it then joins mode 0's tail at 52CAD4, which places the edit at
(0x12, 0x46, 0xF3 x 0x14) and the buttons at (0x37, 0x68) and (0x8F, 0x68).
The buttons keep their authored 76x24; 52C870 only moves them.

Modes 4, 6 and 7 draw their prompt in 0xFFEFDAA4 and size the frame
children 0x119 x 0x19 and 0x111 x 0x11; 4 and 7 share the tail at 52D1AA
(edit at 0x13, 0x49, 0x10A x 0x14), 6 resizes the box to 0x132 x 0x97 and
moves its edit to (0x18, 0x4E, 0xF5 x 0x10). All three put the buttons at
(0x48, 0x72) and (0xA0, 0x72). The friend and block boxes keep the
306x151 the port has always opened them at.
================
*/
export const INSERT_MESSAGE_GEOMETRY = {
	0: {
		size: [ 280, 144 ],
		rects: {
			10: [ 27, 47, 225, 14 ],
			11: [ 11, 68, 257, 25 ],
			12: [ 15, 72, 249, 17 ],
			15: [ 18, 70, 243, 20 ],
			205: [ 55, 104, 76, 24 ],
			206: [ 143, 104, 76, 24 ]
		}
	},
	1: {
		size: [ 420, 144 ],
		rects: {
			10: [ 0, 48, 420, 12 ],
			11: [ 9, 66, 400, 28 ],
			12: [ 13, 70, 392, 20 ],
			15: [ 16, 68, 386, 20 ],
			205: [ 128, 104, 76, 24 ],
			206: [ 216, 104, 76, 24 ]
		}
	},
	2: {
		size: [ 280, 144 ],
		rects: {
			10: [ 27, 47, 225, 14 ],
			11: [ 11, 68, 260, 28 ],
			12: [ 15, 72, 252, 20 ],
			15: [ 18, 70, 243, 20 ],
			205: [ 55, 104, 76, 24 ],
			206: [ 143, 104, 76, 24 ]
		}
	},
	4: {
		size: [ 306, 151 ],
		rects: {
			10: [ 1, 49, 305, 12 ],
			11: [ 12, 71, 281, 25 ],
			12: [ 16, 75, 273, 17 ],
			15: [ 19, 73, 266, 20 ],
			205: [ 72, 114, 76, 24 ],
			206: [ 160, 114, 76, 24 ]
		}
	},
	6: {
		size: [ 306, 151 ],
		rects: {
			10: [ 16, 49, 275, 11 ],
			11: [ 12, 71, 281, 25 ],
			12: [ 16, 75, 273, 17 ],
			15: [ 24, 78, 245, 16 ],
			205: [ 72, 114, 76, 24 ],
			206: [ 160, 114, 76, 24 ]
		}
	},
	7: {
		size: [ 306, 151 ],
		rects: {
			10: [ 1, 49, 305, 12 ],
			11: [ 12, 71, 281, 25 ],
			12: [ 16, 75, 273, 17 ],
			15: [ 19, 73, 266, 20 ],
			205: [ 72, 114, 76, 24 ],
			206: [ 160, 114, 76, 24 ]
		}
	}
} as const;
