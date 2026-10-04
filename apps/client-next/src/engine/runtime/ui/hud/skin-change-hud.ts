/*
===========================================================================

skin-change-hud.ts - the CIFChangePlayerModel window's state

Owns the open window: the scroll's inventory slot, the body it started
from, the slider draft (foundation/gameplay/skin-change.ts) and the preview
yaw the rotate buttons turn. The UI draws from this owner every frame and
the mannequin follows preview().

===========================================================================
*/
import {
	initialSkinDraft,
	setSkinDraft,
	skinDraftChanged,
	skinDraftChoice,
	type PlayerModel,
	type SkinChoice,
	type SkinDraft,
	type SkinDraftKey
} from "@/engine/foundation/gameplay/skin-change";

// The rotate buttons' step: a twelfth of a turn per press.
const SKIN_ROTATE_STEP = Math.PI / 6;

/*
================
SkinWindow
================
*/
interface SkinWindow {
	readonly slot: number;
	readonly models: readonly PlayerModel[];
	readonly model: number;
	readonly shape: number;
	readonly draft: SkinDraft;
	readonly yaw: number;
}

/*
================
createSkinChangeHud
================
*/
export function createSkinChangeHud() {
	let open: SkinWindow | null = null;
	return {
		/*
		================
		open

		The scroll in slot was used; the draft starts from the worn body.
		================
		*/
		open( slot: number, models: readonly PlayerModel[], model: number, shape: number ) {
			open = { slot, models, model, shape, draft: initialSkinDraft( models, model, shape ), yaw: 0 };
		},
		/*
		================
		close
		================
		*/
		close() {
			open = null;
		},
		/*
		================
		state
		================
		*/
		state(): SkinWindow | null {
			return open;
		},
		/*
		================
		set
		================
		*/
		set( key: SkinDraftKey, value: number ) {
			if ( open ) open = { ...open, draft: setSkinDraft( open.models, open.draft, key, value ) };
		},
		/*
		================
		rotate

		direction -1 or 1 turns the preview; 0 resets it.
		================
		*/
		rotate( direction: number ) {
			if ( open ) open = { ...open, yaw: direction === 0 ? 0 : open.yaw + direction * SKIN_ROTATE_STEP };
		},
		/*
		================
		changed
		================
		*/
		changed(): boolean {
			return !!open && skinDraftChanged( open.models, open.draft, open.model, open.shape );
		},
		/*
		================
		choice
		================
		*/
		choice(): SkinChoice | null {
			return open ? skinDraftChoice( open.models, open.draft ) : null;
		},
		/*
		================
		preview

		The body the mannequin wears, or null with the window closed.
		================
		*/
		preview(): SkinChoice | null {
			return this.choice();
		}
	};
}
