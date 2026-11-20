/*
===========================================================================

sro-macro.ts - the worker's SRO Macro lane (port-only 0x3555/0xB554/0x7554)

Port-only, not native (#3907): the v1.696 macro's settings conversation.
The server (SRO_AUTO_MACRO) replays the stored settings in 0x3555 at
login; each switch or save goes out in 0x7554 and only becomes live when
0xB554 answers it with result 1, as the v1.696 handler commits its
pending copy (00cfca30). Nothing arrives with the server flag off, and
nothing is sent while the client's Experimental row is off (the HUD
never issues a command).

The codec lives in foundation/gameplay/sro-macro.ts; this lane owns the
live settings and the pending saves.

===========================================================================
*/
import type { WireFrame } from "@/engine/contracts/network";
import type { SystemNotice } from "@/engine/foundation/gameplay/system-notices";
import {
	commitMacroSave,
	decodeMacroAnswer,
	decodeMacroConfig,
	defaultMacroSettings,
	encodeMacroSave,
	encodeMacroSwitch,
	MACRO_ACTION_SWITCH,
	MACRO_HUNT,
	MACRO_POTION,
	MACRO_RESULT_OK,
	MACRO_RESULT_SWITCH_FAILED,
	MACRO_SKILL,
	macroAvailable,
	type MacroKind,
	macroOn,
	type MacroSettings,
	OP_MACRO_ANSWER,
	OP_MACRO_CONFIG,
	OP_MACRO_REQUEST,
	withMacroOn
} from "@/engine/foundation/gameplay/sro-macro";

// The v1.696 client's chat lines for a failed answer (00cfcbb3 and the
// save branch). v1.150 carries no UIIT_STT_SRO_MACRO_* rows.
const SWITCH_ERROR_TEXT = "ON/OFF Error";
const SWITCH_ERROR_OTHER_TEXT = "ON/OFF Error!";
const SAVE_ERROR_TEXT = "Save Error";
const NOTICE_TYPE_SYSTEM = 5;

/*
================
isMacroKind
================
*/
function isMacroKind( kind: number ): kind is MacroKind {
	return kind === MACRO_POTION || kind === MACRO_SKILL || kind === MACRO_HUNT;
}

/*
================
createSroMacro
================
*/
export function createSroMacro() {
	let settings = defaultMacroSettings();
	let received = false;
	let pending: Partial<Record<MacroKind, MacroSettings>> = {};
	let notice: Omit<SystemNotice, "sequence"> | undefined;
	const fail = ( text: string ) => {
		notice = { key: "", value: 0, text, nativeType: NOTICE_TYPE_SYSTEM };
	};
	return {
		/*
================
receive

Consumes a config or answer frame; false for any other opcode. A
malformed frame is a protocol error.
================
		*/
		receive( frame: WireFrame ): boolean {
			if ( frame.opcode === OP_MACRO_CONFIG ) {
				settings = decodeMacroConfig( frame.payload, settings );
				received = true;
				pending = {};
				return true;
			}
			if ( frame.opcode !== OP_MACRO_ANSWER ) return false;
			const answer = decodeMacroAnswer( frame.payload );
			if ( !isMacroKind( answer.kind ) ) throw new Error( "Invalid SRO Macro answer kind" );
			if ( answer.action === MACRO_ACTION_SWITCH ) {
				if ( answer.result === MACRO_RESULT_OK ) {
					settings = withMacroOn( settings, answer.kind, answer.state === true );
				} else {
					fail( answer.result === MACRO_RESULT_SWITCH_FAILED ? SWITCH_ERROR_TEXT : SWITCH_ERROR_OTHER_TEXT );
				}
				return true;
			}
			const waiting = pending[answer.kind];
			delete pending[answer.kind];
			if ( answer.result !== MACRO_RESULT_OK ) {
				fail( SAVE_ERROR_TEXT );
			} else if ( waiting ) {
				settings = commitMacroSave( settings, answer.kind, waiting );
			}
			return true;
		},
		/*
================
switchFrame

The 0x7554 switch, or null when the kind is not available (006947f0
reads the availability byte first) or already in that state.
================
		*/
		switchFrame( kind: MacroKind, on: boolean ): WireFrame | null {
			if ( !received || !macroAvailable( settings, kind ) ) return null;
			if ( macroOn( settings, kind ) === on ) return null;
			return { opcode: OP_MACRO_REQUEST, payload: encodeMacroSwitch( kind, on ) };
		},
		/*
================
saveFrame

The 0x7554 save, or null when nothing changed. The settings wait as the
kind's pending copy until the answer.
================
		*/
		saveFrame( kind: MacroKind, next: MacroSettings ): WireFrame | null {
			if ( !received || !macroAvailable( settings, kind ) ) return null;
			const payload = encodeMacroSave( kind, next, settings );
			if ( !payload ) return null;
			pending = { ...pending, [kind]: next };
			return { opcode: OP_MACRO_REQUEST, payload };
		},
		/*
================
takeNotice
================
		*/
		takeNotice(): Omit<SystemNotice, "sequence"> | undefined {
			const value = notice;
			notice = undefined;
			return value;
		},
		// The live settings, undefined until the server's first replay: a
		// shard without SRO_AUTO_MACRO never sends one.
		view: (): MacroSettings | undefined => received ? settings : undefined,
		/*
================
clear
================
		*/
		clear() {
			settings = defaultMacroSettings();
			received = false;
			pending = {};
			notice = undefined;
		}
	};
}
