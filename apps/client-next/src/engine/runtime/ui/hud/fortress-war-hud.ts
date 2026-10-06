/*
===========================================================================

fortress-war-hud.ts - the fortress war application window's state

The official's row asks for the war status; each answer (a new
fortressApplication sequence) opens or refreshes CIFFortressWarApplyWnd
for that official (754A40 -> 69EA10, 663200). A slot button raises its
question (6649C0); a confirmed question becomes the 0x71E1 the UI sends.
The UI draws from this owner every frame.

===========================================================================
*/
import type { FortressWarQuestion } from "@/engine/foundation/ui/fortress-war-apply";
import type { FortressState } from "@/engine/foundation/gameplay/fortress";

export const FORTRESS_SCHEDULE_ROWS = 7;

/*
================
createFortressScheduleHud

A query is bound to its selected NPC and reply sequence. Changing target
discards the pending open; errors never reopen an old successful answer.
================
*/
export function createFortressScheduleHud() {
	let npc: number | null = null, seen = 0, pending = false, open = false, top = 0;
	return {
		/*
		================
		request
		================
		*/
		request( gid: number, sequence: number ) {
			npc = gid;
			seen = sequence;
			pending = true;
			open = false;
			top = 0;
		},
		/*
		================
		observe
		================
		*/
		observe( state: FortressState | undefined, target: number | undefined ) {
			if ( target !== npc ) {
				npc = null;
				pending = false;
				open = false;
			}
			if ( !pending || state?.serviceSequence === seen || state?.service?.action !== 5 ) return;
			seen = state.serviceSequence ?? 0;
			pending = false;
			open = state.service.result === 1;
		},
		/*
		================
		isOpen
		================
		*/
		isOpen() {
			return open;
		},
		/*
		================
		offset
		================
		*/
		offset() {
			return top;
		},
		/*
		================
		page
		================
		*/
		page( direction: number, count: number ) {
			top = Math.max(
				0,
				Math.min( Math.max( 0, count - FORTRESS_SCHEDULE_ROWS ), top + direction * FORTRESS_SCHEDULE_ROWS )
			);
		},
		/*
		================
		close
		================
		*/
		close() {
			npc = null;
			pending = false;
			open = false;
			top = 0;
		}
	};
}

/*
================
FortressWarAsk
================
*/
interface FortressWarAsk {
	readonly question: FortressWarQuestion;
	readonly fortress: number;
}

/*
================
createFortressWarHud
================
*/
export function createFortressWarHud() {
	let npc: number | null = null, requested: number | null = null, seen = 0, open = false;
	let ask: FortressWarAsk | null = null;
	return {
		/*
		================
		request

		The official's row was clicked: the next answer opens his window.
		================
		*/
		request( gid: number ) {
			requested = gid;
		},
		/*
		================
		observe

		Follows the worker's answers. An answer after a request opens the
		window; later answers refresh it while it is open.
		================
		*/
		observe( sequence: number | undefined ) {
			if ( sequence === undefined || sequence === seen ) return;
			seen = sequence;
			if ( requested !== null ) {
				npc = requested;
				requested = null;
				open = true;
			}
		},
		/*
		================
		npc

		The official whose window is open, or null.
		================
		*/
		npc(): number | null {
			return open ? npc : null;
		},
		/*
		================
		ask
		================
		*/
		ask( question: FortressWarQuestion, fortress: number ) {
			ask = { question, fortress };
		},
		/*
		================
		question
		================
		*/
		question(): FortressWarAsk | null {
			return ask;
		},
		/*
		================
		takeQuestion
		================
		*/
		takeQuestion(): FortressWarAsk | null {
			const taken = ask;
			ask = null;
			return taken;
		},
		/*
		================
		close
		================
		*/
		close() {
			open = false;
			npc = null;
			requested = null;
			ask = null;
		}
	};
}

/*
================
createFortressStaffHud

5D7AD0 opens the hire submenu; 5D8930 asks before sending each hire.
A reply cannot revive a menu after its NPC conversation has ended.
================
*/
export function createFortressStaffHud() {
	let target: { gid: number; fortress: number; } | null = null;
	let question: { gid: number; fortress: number; flag: number; } | null = null;
	return {
		/*
  ================
  open
  ================
  */
		open( gid: number, fortress: number ) {
			target = { gid, fortress };
			question = null;
		},
		/*
  ================
  observe
  ================
  */
		observe( gid: number | undefined ) {
			if ( gid !== target?.gid ) {
				target = null;
				question = null;
			}
		},
		/*
  ================
  target
  ================
  */
		target() {
			return target;
		},
		/*
  ================
  ask
  ================
  */
		ask( flag: number, flags: number, master: boolean ) {
			if ( target && master && [ 1, 2, 4 ].includes( flag ) && !(flags & flag) ) question = { ...target, flag };
		},
		/*
  ================
  question
  ================
  */
		question() {
			return question;
		},
		/*
  ================
  takeQuestion
  ================
  */
		takeQuestion() {
			const asked = question;
			question = null;
			return asked;
		}
	};
}
