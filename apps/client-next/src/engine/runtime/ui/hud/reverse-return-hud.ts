/*
===========================================================================

reverse-return-hud.ts - the bag scroll's pending destination choice

Owns the slot and character bound to message box 0x1E. Reconcile before
input and painting so a delayed answer cannot use a replacement item or
survive a world transition. A busy bag is one item round trip, not an
invalidation: the box keeps the choice and the answer waits for it.

Port-only, not native: when the server publishes a map table
(SRO_REVERSE_RETURN_MAP), the box has a third row. It hands the same
pending scroll to the world map, where a published point is the answer
(reverse-return-map.ts).

Later-native (#3903): the box's party row hands the scroll to the party
window (a member is the answer) and its user row to the user settings
window (its enroll and move buttons are the answers). The three windows
are one pending scroll in three postures, like the map posture.

===========================================================================
*/
import type { GameplayCommand, GameplayState, InventoryItem } from "@/engine/contracts/gameplay";
import type { UiView } from "@/engine/contracts/ui";
import { isReverseReturnScroll } from "@/engine/foundation/gameplay/cos-item-use";
import { REVERSE_RETURN_LAST_DEATH, REVERSE_RETURN_LAST_RECALL } from "@/engine/foundation/gameplay/count-job";
import { REVERSE_RETURN_MAP } from "@/engine/foundation/gameplay/reverse-return-map";
import {
	REVERSE_RETURN_ENROLL,
	REVERSE_RETURN_PARTY,
	REVERSE_RETURN_USER
} from "@/engine/foundation/gameplay/reverse-user-point";

/*
================
createReverseReturnHud
================
*/
export function createReverseReturnHud() {
	let pending: { slot: number; refObjId: number; gid: number; } | null = null;
	// mapping: the box handed the pending scroll to the world map.
	let mapping = false;
	// window: the box handed the pending scroll to its party or user
	// window (#3903); enroll asks before it overwrites a saved location.
	let window: "party" | "user" | null = null;
	let confirmEnroll = false;
	/*
	================
	reconcile

	The choice stands while the same character holds the same reverse scroll
	in the same slot, in the world and not travelling. A busy bag is only a
	round trip in flight, so it never drops the choice.

	INFERENCE: nothing in the native box's flow (6971B0 case 0x1E) ties it
	to an unrelated item's answer, so a busy bag does not close it either.
	================
	*/
	function reconcile( view: UiView | null ): boolean {
		if ( !pending ) return false;
		const game = view?.gameplay;
		const row = game?.inventory.find( row => row.slot === pending?.slot );
		const stands = view?.session?.phase === "world" && !view.travel && game?.localGid === pending.gid &&
			!game.reverseReturnChoice && row?.refObjId === pending.refObjId &&
			isReverseReturnScroll( row.typeFlags ) && row.quantity > 0;
		if ( stands ) {
			// The party window lists members; the last one leaving takes
			// it back to the box rather than leaving an empty window (#4391).
			if ( window === "party" && !partyOffered( game ) ) {
				window = null;
				return true;
			}
			return false;
		}
		pending = null;
		mapping = false;
		window = null;
		confirmEnroll = false;
		return true;
	}
	/*
	================
	partyOffered

	The party row's offer (#4391): another member exists to move to.
	The box's row and the party window share this one test, so the row
	is never offered when its window would list nobody.
	================
	*/
	function partyOffered( game: GameplayState | null | undefined ): boolean {
		return (game?.social?.members ?? []).some( member => member.id !== game?.localGid );
	}
	return {
		reconcile,
		partyOffered,
		/*
		================
		open
		================
		*/
		open( item: InventoryItem, gid: number ) {
			pending = { slot: item.slot, refObjId: item.refObjId, gid };
			mapping = false;
			window = null;
			confirmEnroll = false;
		},
		/*
		================
		active

		The box is up: a scroll waits and no other posture has taken it.
		================
		*/
		active() {
			return pending !== null && !mapping && window === null;
		},
		/*
		================
		mapping

		The world map holds the pending scroll (port-only).
		================
		*/
		mapping() {
			return pending !== null && mapping;
		},
		/*
		================
		openMap

		The box's map row: the scroll waits for a point on the world map.
		================
		*/
		openMap( view: UiView | null ): boolean {
			reconcile( view );
			if ( !pending || !view?.gameplay?.reverseMapPoints?.length ) return false;
			mapping = true;
			return true;
		},
		/*
		================
		choosePoint

		A published map point answers the pending scroll (choice 7).
		================
		*/
		choosePoint( id: number, view: UiView | null ): GameplayCommand | null {
			reconcile( view );
			if ( !pending || !mapping || !view?.gameplay?.reverseMapPoints?.some( point => point.id === id ) ) {
				return null;
			}
			// A busy bag is a round trip in flight (the worker refuses item
			// commands while one is pending); hold the choice so the answer
			// cannot race it, and let the player click again when it clears.
			if ( view.gameplay.inventoryPending ) return null;
			const slot = pending.slot;
			pending = null;
			mapping = false;
			window = null;
			confirmEnroll = false;
			return { kind: "item-use", slot, reverseChoice: REVERSE_RETURN_MAP, reverseMapPoint: id };
		},
		/*
		================
		party

		The party window holds the pending scroll (#3903).
		================
		*/
		party() {
			return pending !== null && window === "party";
		},
		/*
		================
		openParty

		The box's party row: the scroll waits for a member. With no other
		member the window never opens: in the v1.696 client the party
		window's own show (CNIFReversePortalParty sub_E11F40) reads a
		state byte (sub_FD5580) and sets its visibility from it, so the
		window does not appear unconditionally. That the row itself
		hides while the party is empty is inferred from that gate and
		from this box's offered rows (the map row); what sets the
		native byte was not isolated.
		================
		*/
		openParty( view: UiView | null ): boolean {
			reconcile( view );
			if ( !pending ) return false;
			if ( !partyOffered( view?.gameplay ) ) return false;
			window = "party";
			return true;
		},
		/*
		================
		chooseParty

		A member answers the pending scroll (choice 4, the member's gid).
		================
		*/
		chooseParty( gid: number, view: UiView | null ): GameplayCommand | null {
			reconcile( view );
			if ( !pending || window !== "party" ) return null;
			// A busy bag holds the answer, as the box's rows do.
			if ( view?.gameplay?.inventoryPending ) return null;
			const slot = pending.slot;
			pending = null;
			window = null;
			return { kind: "item-use", slot, reverseChoice: REVERSE_RETURN_PARTY, reversePartyGid: gid };
		},
		/*
		================
		user

		The user settings window holds the pending scroll (#3903).
		================
		*/
		user() {
			return pending !== null && window === "user";
		},
		/*
		================
		openUser

		The box's user row: the scroll waits for the window's buttons.
		================
		*/
		openUser( view: UiView | null ): boolean {
			reconcile( view );
			if ( !pending ) return false;
			window = "user";
			confirmEnroll = false;
			return true;
		},
		/*
		================
		confirmEnroll

		The window asks before its enroll overwrites a saved location
		(textuisystem id 4214).
		================
		*/
		confirmEnroll() {
			return confirmEnroll;
		},
		/*
		================
		askEnroll

		The enroll button with a location already saved: ask first.
		================
		*/
		askEnroll( view: UiView | null ): boolean {
			reconcile( view );
			if ( !pending || window !== "user" || !view?.gameplay?.reverseUserPoint ) return false;
			confirmEnroll = true;
			return true;
		},
		/*
		================
		enroll

		The enroll button (choice 6): record where the player stands.
		Spends nothing on the server; the answer closes the window.
		================
		*/
		enroll( view: UiView | null ): GameplayCommand | null {
			reconcile( view );
			if ( !pending || window !== "user" ) return null;
			if ( view?.gameplay?.inventoryPending ) return null;
			// With a location saved, the button asks before it overwrites.
			if ( view?.gameplay?.reverseUserPoint && !confirmEnroll ) {
				confirmEnroll = true;
				return null;
			}
			const slot = pending.slot;
			pending = null;
			window = null;
			confirmEnroll = false;
			return { kind: "item-use", slot, reverseChoice: REVERSE_RETURN_ENROLL };
		},
		/*
		================
		chooseUser

		The move button (choice 5): return to the saved location. With
		none saved the button is not offered (id 4218 says why).
		================
		*/
		chooseUser( view: UiView | null ): GameplayCommand | null {
			reconcile( view );
			if ( !pending || window !== "user" || !view?.gameplay?.reverseUserPoint ) return null;
			if ( view?.gameplay?.inventoryPending ) return null;
			const slot = pending.slot;
			pending = null;
			window = null;
			confirmEnroll = false;
			return { kind: "item-use", slot, reverseChoice: REVERSE_RETURN_USER };
		},
		/*
		================
		back

		A sub-window's close: the choice box takes the scroll back.
		================
		*/
		back() {
			window = null;
			confirmEnroll = false;
		},
		/*
		================
		close
		================
		*/
		close() {
			pending = null;
			mapping = false;
			window = null;
			confirmEnroll = false;
		},
		/*
		================
		choose
		================
		*/
		choose( choice: number, view: UiView | null ): GameplayCommand | null {
			reconcile( view );
			if (
				!pending || window !== null ||
				choice !== REVERSE_RETURN_LAST_RECALL && choice !== REVERSE_RETURN_LAST_DEATH
			) {
				return null;
			}
			// A busy bag is a round trip in flight (the worker refuses item
			// commands while one is pending); hold the choice so the answer
			// cannot race it, and let the player click again when it clears.
			if ( view?.gameplay?.inventoryPending ) return null;
			const slot = pending.slot;
			pending = null;
			mapping = false;
			return { kind: "item-use", slot, reverseChoice: choice };
		}
	};
}
