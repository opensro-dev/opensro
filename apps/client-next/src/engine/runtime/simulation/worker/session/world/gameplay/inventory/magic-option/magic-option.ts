/*
===========================================================================

magic-option.ts - the smith's avatar grant window session

Owns the CIFGrantMagicAttributeWnd session: the 0x7338 [npc][0x80000000]
claim, its B338 lock, the avatar item dropped on the window, the one
0x361A in flight and the 0x32D9 answer's status, plus the per-part option
lists the enter payload ships. The inventory owner applies the answer's
item body (inventory.ts); refusals the client makes itself come back as
notice symbols for the gameplay owner to print.

===========================================================================
*/
import {
	AVATAR_MAGIC_OPTION_FUNCTION,
	avatarMagicOptionCount,
	avatarMagicOptionParts,
	avatarMagicOptionRequest,
	grantableAvatarPart,
	type AvatarMagicOptionPart
} from "@/engine/foundation/gameplay/avatar-magic-option";
import type { ItemMagicReference } from "@/engine/foundation/gameplay/item-tooltip-reference";
import type { InventoryItem } from "@/engine/contracts/gameplay";
import type { MagicOptionGrantState } from "@/engine/contracts/item-process";

const OP_NPC_ACTION = 0x7338;

/*
================
closedGrant
================
*/
function closedGrant( parts: readonly AvatarMagicOptionPart[] ): MagicOptionGrantState {
	return { visible: false, phase: "closed", npc: 0, item: null, error: null, parts };
}

/*
================
createMagicOptionGrant
================
*/
export function createMagicOptionGrant() {
	let state = closedGrant( [] );
	return {
		/*
		================
		bootstrap

		The enter payload's assignment rows, resolved through its option
		definitions.
		================
		*/
		bootstrap( value: unknown, references: ReadonlyMap<number, ItemMagicReference> ) {
			state = closedGrant( avatarMagicOptionParts( value, references ) );
		},
		/*
		================
		open

		Row 0x2F's click (5DA1B0): 0x7338 [npc][0x80000000].
		================
		*/
		open( gid: number ) {
			if ( !Number.isInteger( gid ) || gid <= 0 || gid > 0xffffffff || state.phase === "waiting" ) {
				throw Error( "Magic option grant unavailable" );
			}
			const payload = new Uint8Array( 8 ), view = new DataView( payload.buffer );
			view.setUint32( 0, gid, true );
			view.setUint32( 4, AVATAR_MAGIC_OPTION_FUNCTION, true );
			state = { ...state, visible: false, phase: "opening", npc: gid, item: null, error: null };
			return { opcode: OP_NPC_ACTION, payload };
		},
		/*
		================
		opened

		B338: lock 0x80000000 shows the window; a refusal to this claim
		closes it. Other locks and refusals belong to other owners.
		================
		*/
		opened( p: Uint8Array ) {
			if ( state.phase !== "opening" ) return false;
			if ( p[0] !== 1 ) {
				if ( p.length !== 2 ) throw Error( "Invalid NPC interaction rejection" );
				state = { ...state, phase: "closed", error: p[1]! };
				return true;
			}
			if ( p.length !== 5 ) throw Error( "Invalid NPC interaction result" );
			const mask = new DataView( p.buffer, p.byteOffset, p.byteLength ).getUint32( 1, true );
			if ( !(mask & AVATAR_MAGIC_OPTION_FUNCTION) ) return false;
			state = { ...state, visible: true, phase: "idle" };
			return true;
		},
		/*
		================
		close
		================
		*/
		close() {
			state = closedGrant( state.parts );
		},
		/*
		================
		take

		An item dropped on the window (6EB570): an avatar hat, dress or
		attachment from the bag becomes the window's item; anything else is
		refused with UIIT_STT_AVATAR_MAGICOPTION_ONLY_AVATAR and the window
		keeps what it had.
		================
		*/
		take( item: InventoryItem | undefined ): string | null {
			if ( !state.visible || !item ) throw Error( "Magic option window unavailable" );
			if ( item.slot < 13 || grantableAvatarPart( item.typeFlags ) === null ) {
				return "UIIT_STT_AVATAR_MAGICOPTION_ONLY_AVATAR";
			}
			state = { ...state, item: item.slot };
			return null;
		},
		/*
		================
		grant

		The confirm (6EBB10): an item with a free option slot sends one of its
		part's options; a full one prints UIIT_MSG_AVATAR_MAGICOPTION_ADD_ERORR
		without a request.
		================
		*/
		grant( codename: string, item: InventoryItem | undefined ) {
			const part = item ? grantableAvatarPart( item.typeFlags ) : null;
			if (
				!state.visible || state.phase !== "idle" || !item || item.slot !== state.item || part === null ||
				!state.parts.find( p => p.part === part )?.options.some( o => o.codename === codename )
			) throw Error( "Magic option grant unavailable" );
			if ( avatarMagicOptionCount( item ) >= (item.tooltip?.fields.maxMagicOptions51c ?? 0) ) {
				return { notice: "UIIT_MSG_AVATAR_MAGICOPTION_ADD_ERORR" };
			}
			state = { ...state, phase: "waiting", error: null };
			return { frame: avatarMagicOptionRequest( item.slot, codename ) };
		},
		/*
		================
		result

		0x32D9: the granted slot, or null after a refusal (whose notice the
		gameplay owner raises).
		================
		*/
		result( p: Uint8Array ): number | null {
			if ( p[0] === 1 ) {
				if ( p.length < 4 || p[1] === 0 || p[2]! < 13 ) throw Error( "Invalid magic option grant" );
				state = { ...state, phase: state.visible ? "idle" : "closed", error: null };
				return p[2]!;
			}
			if ( p[0] !== 2 || p.length !== 2 ) throw Error( "Invalid magic option grant refusal" );
			state = { ...state, phase: state.visible ? "idle" : "closed", error: p[1]! };
			return null;
		},
		/*
		================
		state
		================
		*/
		state: () => state,
		/*
		================
		reset
		================
		*/
		reset() {
			state = closedGrant( [] );
		}
	};
}
