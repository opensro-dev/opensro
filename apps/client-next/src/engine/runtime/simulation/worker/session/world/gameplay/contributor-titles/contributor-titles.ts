/*
===========================================================================

contributor-titles.ts - the shown Contributor roster (port-only 0x3FC1, not native)

Under SRO_CONTRIBUTOR_TITLES (#3358) the server pushes the character
names currently shown with the Contributor title: the online characters
whose account holds the Discord Contributor role and has not hidden
the title. The nameplate draws the title row over a rostered player
(overhead-layout.ts) and chat tags a rostered sender (chat-layout.ts).
Nothing arrives with the flag off, and a client that predates the
frame drops the unknown opcode.

Body, little-endian: u16 count, then count x { u16 len, ascii name }.

===========================================================================
*/
import type { WireFrame } from "@/engine/contracts/network";
import type { SystemNotice } from "@/engine/foundation/gameplay/system-notices";

export const OP_CONTRIBUTOR_TITLES = 0x3fc1;
// The login prompt (port-only 0x3FC2, owner 2026-11-14): one byte, its
// kind. Elsewhere asks an account without the title whether to move it
// here; holder tells the account that carries it.
export const OP_CONTRIBUTOR_PROMPT = 0x3fc2;
export const CONTRIBUTOR_PROMPT_ELSEWHERE = 1;
export const CONTRIBUTOR_PROMPT_HOLDER = 2;
const MAX_CONTRIBUTORS = 1024;

/*
================
decodeName

One ascii name off the wire. The contract carries ascii only; a byte
above it can never equal a character name, so it is kept as its code
point and simply never matches.
================
*/
function decodeName( payload: Uint8Array, offset: number, length: number ): string {
	let name = "";
	for ( let i = 0; i < length; i++ ) {
		name += String.fromCharCode( payload[offset + i]! );
	}
	return name;
}

/*
================
createContributorTitles
================
*/
export function createContributorTitles() {
	let names: readonly string[] = [];
	let prompt: number | undefined;
	return {
		/*
================
receive

Consumes a roster frame; false for any other opcode. The roster
replaces the last one wholesale; a malformed roster is a protocol
error, never a partial list.
================
		*/
		receive( frame: WireFrame ): boolean {
			if ( frame.opcode === OP_CONTRIBUTOR_PROMPT ) {
				const kind = frame.payload.length === 1 ? frame.payload[0] : undefined;
				if ( kind !== CONTRIBUTOR_PROMPT_ELSEWHERE && kind !== CONTRIBUTOR_PROMPT_HOLDER ) {
					throw new Error( "Invalid contributor prompt" );
				}
				prompt = kind;
				return true;
			}
			if ( frame.opcode !== OP_CONTRIBUTOR_TITLES ) return false;
			const p = frame.payload, v = new DataView( p.buffer, p.byteOffset, p.byteLength );
			if ( p.length < 2 ) throw new Error( "Invalid contributor titles" );
			const count = v.getUint16( 0, true );
			if ( count > MAX_CONTRIBUTORS ) throw new Error( "Invalid contributor title count" );
			const next: string[] = [];
			let offset = 2;
			for ( let i = 0; i < count; i++ ) {
				if ( offset + 2 > p.length ) throw new Error( "Invalid contributor title name" );
				const length = v.getUint16( offset, true );
				offset += 2;
				if ( offset + length > p.length ) throw new Error( "Invalid contributor title name" );
				next.push( decodeName( p, offset, length ) );
				offset += length;
			}
			if ( offset !== p.length ) throw new Error( "Invalid contributor titles" );
			names = next;
			return true;
		},
		names: () => names,
		/*
================
takePrompt

The login prompt's notice, once: the dialog the HUD raises. Elsewhere
answers "/contributor here" or "/contributor keep"; the holder's OK
answers "keep", so the server stops asking.
================
		*/
		takePrompt(): Omit<SystemNotice, "sequence"> | undefined {
			const kind = prompt;
			prompt = undefined;
			if ( kind === undefined ) return undefined;
			const elsewhere = kind === CONTRIBUTOR_PROMPT_ELSEWHERE;
			return {
				key: "",
				value: 0,
				bannerOnly: true,
				dialog: {
					title: "UIIT_PORT_CONTRIBUTOR_TITLE",
					lines: [ elsewhere ? "UIIT_PORT_CONTRIBUTOR_ELSEWHERE" : "UIIT_PORT_CONTRIBUTOR_HOLDER" ],
					answers: elsewhere ?
						{
							confirmLabel: "UIIT_PORT_CONTRIBUTOR_MOVE",
							confirmChat: "/contributor here",
							cancelLabel: "UIIT_PORT_CONTRIBUTOR_KEEP",
							cancelChat: "/contributor keep"
						} :
						{ confirmLabel: "UIIT_CTL_CONFIRM", confirmChat: "/contributor keep" }
				}
			};
		},
		/*
================
clear
================
		*/
		clear() {
			names = [];
			prompt = undefined;
		}
	};
}
