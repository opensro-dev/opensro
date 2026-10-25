/*
===========================================================================

npc-panel.ts - NPC window visibility, destinations and scroll presentation

The click hides the native parent before the worker sends its service request.
Versioned worker receipts acknowledge that hide or show a new selection, even
when close and reselection coalesce. Panel navigation never shows this window.
Packet, service-lock and quest authority stay in gameplay.

===========================================================================
*/
import type { NpcConversation } from "@/engine/foundation/gameplay/npc-dialogue";
import type { GameplayState } from "@/engine/contracts/gameplay";
import type { UiEvent, UiRect } from "@/engine/contracts/ui";

/*
================
createNpcPanel
================
*/
export function createNpcPanel() {
	let destinations = false, hidden = false;
	let gid = 0, windowRevision = 0;
	let key = "", top = 0, range = 0, travel = 0, bounds: UiRect = [ 0, 0, 0, 0 ];
	/*
	================
	clamp
	================
	*/
	const clamp = ( value: number ) => Math.max( 0, Math.min( range, value ) );
	return {
		/*
		================
		observe

		A stale publication must not undo the click's hide. Only a new window
		receipt or a changed conversation identity owns visibility; dialogue
		content changes own scrolling alone.
		================
		*/
		observe( state: NpcConversation | undefined, window?: GameplayState["npcWindow"] ) {
			const nextGid = state && state.phase !== "closed" ? state.gid : 0;
			const revision = window?.revision ?? 0;
			const visibilityChanged = nextGid !== gid || revision !== windowRevision;
			if ( visibilityChanged ) {
				gid = nextGid;
				windowRevision = revision;
				hidden = !!nextGid && (window?.hidden ?? false);
			}
			const next = state && state.phase !== "closed" ?
				JSON.stringify( [
					state.gid,
					state.dialogueRevision,
					state.phase === "menu" ? null : state.dialogue
				] ) :
				"";
			if ( next !== key || visibilityChanged && !hidden ) {
				key = next;
				destinations = false;
				top = 0;
				range = 0;
				return true;
			}
			return visibilityChanged;
		},
		/*
		================
		hide

		5DA7D6 and 5DAFA9 hide the parent before the storage request. The
		worker receipt preserves this through subsequent panel switches.
		================
		*/
		hide() {
			hidden = true;
		},
		/*
		================
		hidden
		================
		*/
		hidden: () => hidden,
		/*
		================
		destinations
		================
		*/
		destinations: () => destinations,
		/*
		================
		top
		================
		*/
		top: () => top,
		/*
		================
		geometry
		================
		*/
		geometry( value: { range: number; travel: number; bounds: UiRect; } ) {
			range = value.range;
			travel = value.travel;
			bounds = value.bounds;
			top = clamp( top );
		},
		/*
		================
		event
		================
		*/
		event( event: UiEvent ) {
			if ( !key || hidden ) return false;
			if ( event.kind === "activate" && event.id === "npc-portal-open" ) {
				destinations = true;
				top = 0;
				return true;
			}
			if ( event.kind === "activate" && (event.id === "npc-scroll-up" || event.id === "npc-scroll-down") ) {
				top = clamp( Math.round( top ) + (event.id === "npc-scroll-up" ? -1 : 1) );
				return true;
			}
			if ( event.kind === "drag" && event.id === "npc-scroll-thumb" ) {
				if ( travel > 0 ) top = clamp( top + event.dy * range / travel );
				return true;
			}
			if (
				event.kind === "scroll" && event.x >= bounds[0] && event.x < bounds[0] + bounds[2] + 30 &&
				event.y >= bounds[1] && event.y < bounds[1] + bounds[3]
			) {
				top = clamp( Math.round( top ) + Math.sign( event.delta ) * 3 );
				return true;
			}
			return false;
		}
	};
}
