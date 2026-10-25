/*
===========================================================================

unique-rank-hud.ts - the unique damage ranking window (port-only, not native)

Opens once for every ranking the worker publishes (0x3FBC, sent only with
SRO_UNIQUE_DAMAGE_RANK on) and stays open until the player closes it. A
newer kill replaces the open ranking. The original has no such window.
It shows only with the Experimental "Unique damage ranking" row on.

Which rankings are new is decided by the worker's publication alone, never
by the UI phase: a teleport leaves and re-enters the world phase while the
worker still publishes the last ranking, so resetting on the phase
reopened it after every teleport.

===========================================================================
*/
import type { GameplayState } from "@/engine/contracts/gameplay";
import type { UniqueDamageRank } from "@/engine/contracts/unique-rank";

/*
================
createUniqueRankHud
================
*/
export function createUniqueRankHud() {
	let shown: UniqueDamageRank | null = null;
	let seen = 0;
	return {
		/*
		================
		state
		================
		*/
		state: () => shown,
		/*
		================
		reconcile

		A ranking newer than the last one opens the window; one already seen
		never reopens it after the player closed it. True when it opened.
		The worker publishes null only once its state is cleared (a session
		reset, or a new world session whose numbering starts from 1 again),
		so only null forgets what was seen. With the row off a ranking is
		still marked seen, so turning it on never opens an old kill, and an
		open window closes.
		================
		*/
		reconcile( gameplay: Pick<GameplayState, "uniqueDamageRank"> | null | undefined, enabled: boolean ): boolean {
			const next = gameplay?.uniqueDamageRank;
			if ( next === null ) seen = 0;
			if ( !enabled ) shown = null;
			if ( !next || next.sequence <= seen ) return false;
			seen = next.sequence;
			if ( !enabled ) return false;
			shown = next;
			return true;
		},
		/*
		================
		close
		================
		*/
		close() {
			shown = null;
		},
		/*
		================
		reset

		Leaving the world closes the window. It keeps what was seen: the
		same ranking is still published when the player arrives again.
		================
		*/
		reset() {
			shown = null;
		}
	};
}
