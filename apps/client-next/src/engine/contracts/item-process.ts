/*
===========================================================================

item-process.ts - the item process windows' commands and published state

Alchemy, Magic POP, the smith's avatar magic option grant and the player
exchange: the commands the UI sends the inventory owner and the window
state it publishes back.

===========================================================================
*/
export type AlchemyMode = "reinforce" | "attribute" | "magic" | "compound" | "advanced" | "dissolve";
export type ItemProcessCommand =
	| { readonly kind: "alchemy-open"; }
	| { readonly kind: "alchemy-close"; }
	| { readonly kind: "alchemy-cancel"; }
	| {
		readonly kind: "alchemy-start";
		readonly mode: AlchemyMode;
		readonly slots: readonly number[];
		readonly quantity?: number;
	}
	| { readonly kind: "gacha-open"; readonly gid: number; }
	| { readonly kind: "gacha-close"; }
	| { readonly kind: "gacha-roll"; readonly entry: number; readonly slot: number; }
	| { readonly kind: "magic-option-open"; readonly gid: number; }
	| { readonly kind: "magic-option-close"; }
	| { readonly kind: "magic-option-take"; readonly slot: number; }
	| { readonly kind: "magic-option-grant"; readonly codename: string; };

/*
================
AlchemyState
================
*/
export interface AlchemyState {
	// The last finished reinforcement, for the window's effect and chat
	// lines (alchemy-result.ts); sequence tells a new one from a republish.
	readonly outcome?: AlchemyOutcome;
	readonly locked?: boolean;
	readonly remaining?: number;
	readonly total?: number;
	readonly lockUntil?: number;
	readonly cancelled?: boolean;
	readonly visible: boolean;
	readonly pending: boolean;
	readonly mode: AlchemyMode;
	readonly flags: number;
	readonly error: number | null;
	readonly slot: number | null;
}

/*
================
AlchemyOutcome

One reinforcement's result as 62B0B0 reads it: the flag bits and the
item's enhancement level and durability before and after.
================
*/
export interface AlchemyOutcome {
	readonly sequence: number;
	readonly flags: number;
	readonly plus: number;
	readonly previousPlus: number;
	readonly durability: number;
	readonly previousDurability: number;
}

/*
================
GachaState
================
*/
export interface GachaState {
	readonly visible: boolean;
	readonly phase: "closed" | "opening" | "idle" | "rolling" | "waiting" | "result";
	readonly npc: number;
	readonly slot: number | null;
	readonly entry: number;
	readonly started: number;
	readonly result: "win" | "lose" | null;
	readonly reward?: { readonly refObjId: number; readonly quantity: number; };
	readonly error: number | null;
}

/*
================
MagicOptionGrantState

The smith's grant window: visible once B338 locks 0x80000000; waiting
while a 0x361A is in flight. item is the bag slot dropped on the window;
parts are the options each avatar part takes
(foundation/gameplay/avatar-magic-option.ts).
================
*/
export interface MagicOptionGrantState {
	readonly visible: boolean;
	readonly phase: "closed" | "opening" | "idle" | "waiting";
	readonly npc: number;
	readonly item: number | null;
	readonly error: number | null;
	readonly parts: readonly import("@/engine/foundation/gameplay/avatar-magic-option").AvatarMagicOptionPart[];
}

/*
================
ExchangeOffer

One offer in the open player exchange (foundation/gameplay/exchange.ts),
by exchange slot; the bag slot only on the own side.
================
*/
export interface ExchangeOffer {
	readonly slot: number;
	readonly bagSlot?: number;
	readonly item: import("./gameplay").InventoryItem;
}

/*
================
ExchangeState
================
*/
export interface ExchangeState {
	readonly open: boolean;
	readonly partner: number;
	readonly own: readonly ExchangeOffer[];
	readonly theirs: readonly ExchangeOffer[];
	readonly ownGold: number;
	readonly theirGold: number;
	readonly ownLocked: boolean;
	readonly theirLocked: boolean;
	readonly approved: boolean;
	readonly requesting: boolean;
}
