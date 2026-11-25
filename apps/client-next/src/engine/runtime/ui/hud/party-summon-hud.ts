/*
===========================================================================

party-summon-hud.ts - the party summon window's visibility

The roster itself lives in the social state (0xB4C6 kind 1); this
owner holds only whether the window is up and where it was dragged.
The leader asks for the roster from the party window; the window opens
when the answer lands. It closes when the party goes away (nothing is
left to summon into) or the leader closes it; a refreshed roster while
it is up just redraws it.

===========================================================================
*/
import type { SocialState } from "@/engine/foundation/gameplay/social";

/*
================
createPartySummonHud
================
*/
export function createPartySummonHud() {
	let shown = false;
	let awaiting = false;
	let position: readonly [number, number] | null = null;
	return {
		/*
================
request

The party window's Summon click: the roster is on its way.
================
		*/
		request() {
			awaiting = true;
		},
		/*
================
sync

Reconcile with the social state: open on the awaited roster, close
when the party (or the roster) goes away. Reports a visibility change.
================
		*/
		sync( social: SocialState | null | undefined ): boolean {
			const was = shown;
			if ( !social?.leader || !social.summon ) {
				shown = false;
				awaiting = false;
			} else if ( awaiting ) {
				shown = true;
				awaiting = false;
			}
			return shown !== was;
		},
		/*
================
visible
================
		*/
		visible() {
			return shown;
		},
		/*
================
close
================
		*/
		close() {
			shown = false;
			awaiting = false;
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
