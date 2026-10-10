/*
===========================================================================

stall-hud.ts - the stall windows' prompts and their drafts

CIFStall raises one message box at a time (CIFStall_ShowConfirmBox
5A1DF0): the title entry (mode 1, 280x144), the greeting entry (mode 2,
420x144) and the price entry for an offer (CIFStall_OpenTextInput 5A1A40,
308x148, quantity and price). The purchase questions of the stall and the
stall network are the same kind of box. This module owns the open prompt
and its typed text; the UI draws from it and sends the command it yields.

Opening the stall asks whether to list it on the stall network
(CIFStall_OnTradingStateButton 5A26F0: UIIT_MSG_WARENETWORK_REGIST_01..03,
the 1% commission notice); the answer is the [net] byte of 0x71A8 kind 5,
and an open stall closes for modification without asking.

===========================================================================
*/
import type { StallCommand, StallListing, StallState } from "@/engine/foundation/gameplay/stall";
import type { ChatLine } from "@/engine/contracts/gameplay";

// CIFStallSlot_UpdateContents 0x5B0500 and Clear 0x5B06F0 select these backplates.
export const STALL_SLOT_IMAGES = {
	occupied: "/assets/images/Media_extracted/interface/stall/stl_slot_02.png",
	empty: "/assets/images/Media_extracted/interface/stall/stl_slot_05.png"
} as const;

// CIFStall_SetTradingState uses byte alpha 128 for a visitor's closed stall.
const CLOSED_VISITOR_SLOT_ALPHA = 128 / 255;

/*
================
stallTradingPresentation

Native 0x5A2140 selects both status resources and the owner/visitor gates.
The resource file's unpadded icon name is replaced before the window paints.
================
*/
export function stallTradingPresentation( owner: boolean, open: boolean ) {
	return {
		icon: "/assets/images/Media_extracted/interface/stall/stl_condition_icon_" + (open ? "01" : "02") + ".png",
		status: open ? "UIIT_STT_TRADING_NOW" : "UIIT_STT_STALL_MODIFYING",
		toggle: open ? "UIIT_STT_END_STALL" : "UIIT_STT_START_STALL",
		canModify: owner && !open,
		slotAlpha: !owner && !open ? CLOSED_VISITOR_SLOT_ALPHA : 1
	};
}

// 52C870 submodes 0/1: title holds 40 units; greeting holds 64.
export const STALL_TITLE_LIMIT = 40;
export const STALL_TEXT_LIMIT = 64;
// A chat line holds at most 100 UTF-16 units (ChatMessageMaxWideChars).
export const STALL_CHAT_LIMIT = 100;
// CIFChatModule's message textbox retains 50 logical entries (545BE0 / 53A150).
const STALL_HISTORY_LIMIT = 50;
const STALL_CHANNEL = 9;
// CIFStall OnCreate 5A2F76..5A2FA0 configures its display manager: 6px inset,
// two columns, 206px column pitch and 41px row pitch. Layout 6F3D43..6F3D81
// adds the inset to the display origin; slot art's final 3px are transparent.
export const STALL_CELL_INSET = 6;
export const STALL_CELL_PITCH_X = 206;
export const STALL_CELL_PITCH_Y = 41;
// 5A1DF0 / 5A1A40: the prompt boxes' sizes; the questions share the title's.
export const STALL_PROMPT_SIZE = {
	title: [ 280, 144 ],
	greeting: [ 420, 144 ],
	price: [ 308, 148 ],
	buy: [ 280, 144 ],
	"network-buy": [ 280, 144 ],
	register: [ 308, 148 ]
} as const;
// 5A1A40: the price edit takes ten digits, the quantity five.
const PRICE_DIGITS = 10;
const QUANTITY_DIGITS = 5;
// 5A1D85 -> 5203B0: the native price editor's upper bound.
const MAX_PRICE = 1000000000;

export type StallPrompt =
	| { readonly kind: "title"; readonly text: string; }
	| { readonly kind: "greeting"; readonly text: string; }
	| {
		readonly kind: "price";
		readonly slot: number;
		readonly bagSlot: number;
		readonly carried: number;
		readonly quantity: string;
		readonly price: string;
		readonly modify: boolean;
	}
	| { readonly kind: "buy"; readonly slot: number; }
	| { readonly kind: "network-buy"; readonly row: number; }
	| { readonly kind: "register"; };

export type StallPromptField = "text" | "quantity" | "price";

/*
================
stallPromptLive

False once the stall the prompt asks about has moved on (closed, opened
for business, left), so the prompt closes with it.
================
*/
export function stallPromptLive( prompt: StallPrompt, stall: StallState | undefined ): boolean {
	if ( !stall ) return false;
	switch ( prompt.kind ) {
		case "title":
			return stall.phase === "naming" || stall.phase === "owner" && !stall.open;
		case "greeting":
		case "register":
			return stall.phase === "owner";
		case "price":
			return stall.phase === "owner" && !stall.open;
		case "buy":
			return stall.phase === "visitor" && stall.open;
		case "network-buy":
			return stall.network.open && !!stall.network.rows[prompt.row];
	}
}

// CIFStallNetwork's combos: large (41), medium (42) and small (43)
// categories; 0 is none open.
export const STALL_COMBO_LARGE = 41;
export const STALL_COMBO_MEDIUM = 42;
export const STALL_COMBO_DEGREE = 43;

/*
================
StallNetworkDraft

The search being composed: indexes into the category tree (-1 for
none), the degree (0 for any), the open combo and the chosen result row.
================
*/
export interface StallNetworkDraft {
	readonly large: number;
	readonly medium: number;
	readonly degree: number;
	readonly combo: number;
	readonly row: number;
	readonly sort: StallNetworkSort;
	readonly ascending: boolean;
}

// The result columns' sort buttons (ids 60..64).
export type StallNetworkSort = "number" | "name" | "quantity" | "level" | "price";

// UIIT_MSG_WARENETWORK_SCAN_TOOLTIP: a search waits ten seconds after the last.
export const STALL_SEARCH_COOLDOWN_MS = 10000;

const EMPTY_NETWORK: StallNetworkDraft = {
	large: -1,
	medium: -1,
	degree: 0,
	combo: 0,
	row: -1,
	sort: "number",
	ascending: true
};

/*
================
stallNetworkOrder

The result rows' indexes in the chosen column order; the number column
is the server's order. levels holds each row's required level.
================
*/
export function stallNetworkOrder(
	rows: readonly StallListing[],
	draft: StallNetworkDraft,
	levels: readonly number[]
): number[] {
	const key = ( row: StallListing, index: number ): number | string | bigint => {
		switch ( draft.sort ) {
			case "name":
				return row.item.name ?? "";
			case "quantity":
				return row.quantity;
			case "level":
				return levels[index] ?? 0;
			case "price":
				return row.price;
			case "number":
				return 0;
		}
	};
	const order = rows.map( ( _, index ) => index );
	if ( draft.sort === "number" ) return draft.ascending ? order : order.reverse();
	return order.sort( ( a, b ) => {
		const x = key( rows[a]!, a ), y = key( rows[b]!, b );
		if ( x === y ) return a - b;
		return (x < y ? -1 : 1) * (draft.ascending ? 1 : -1);
	} );
}

/*
================
stallPromptCommand

The command a prompt's answer sends, or null when the answer sends
nothing (a refusal, an empty or zero entry, a cancelled title prompt that
the caller turns into stall-name-cancel).
================
*/
export function stallPromptCommand(
	prompt: StallPrompt,
	stall: StallState,
	accept: boolean,
	defaultGreeting: string
): StallCommand | null {
	switch ( prompt.kind ) {
		case "title": {
			const title = prompt.text.trim();
			if ( stall.phase === "naming" ) {
				if ( !accept || !title ) return { kind: "stall-name-cancel" };
				// 5A2890: the greeting last typed, else the formatted default.
				return { kind: "stall-create", title, greeting: stall.savedGreeting || defaultGreeting };
			}
			return accept && title ? { kind: "stall-title", text: title } : null;
		}
		case "greeting": {
			const text = prompt.text.trim();
			return accept && text ? { kind: "stall-greeting", text } : null;
		}
		case "price": {
			const quantity = Number( prompt.quantity || 0 ), price = Number( prompt.price || 0 );
			if ( !accept || quantity <= 0 || price <= 0 ) return null;
			return prompt.modify ?
				{ kind: "stall-modify", slot: prompt.slot, quantity, price } :
				{ kind: "stall-add", slot: prompt.slot, bagSlot: prompt.bagSlot, quantity, price };
		}
		case "buy":
			return accept ? { kind: "stall-buy", slot: prompt.slot } : null;
		case "network-buy":
			return accept ? { kind: "stall-network-buy", row: prompt.row } : null;
		case "register":
			return { kind: "stall-open", open: true, network: accept };
	}
}

/*
================
createStallHud
================
*/
export function createStallHud() {
	let prompt: StallPrompt | null = null;
	let network = EMPTY_NETWORK;
	let searchedAt = -Infinity;
	let chat = "";
	let chatOwner: number | undefined;
	let observedLine: ChatLine | undefined;
	let messages: readonly ChatLine[] = [];
	return {
		/*
		================
		observeChat

		5457C0 clears the module when a stall is left. Global chat survives
		that exit; only newly delivered channel-9 lines belong to this module.
		Sequence numbers also survive cloned worker snapshots.
		================
		*/
		observeChat( stall: StallState | undefined, lines: readonly ChatLine[] ) {
			const owner = stall?.phase === "owner" || stall?.phase === "visitor" ? stall.owner : undefined,
				cleared = owner !== chatOwner,
				last = lines.at( -1 ),
				same = ( line: ChatLine | undefined ) =>
					line === observedLine ||
					line?.sequence !== undefined && line.sequence === observedLine?.sequence;
			if ( cleared ) {
				chat = "";
				messages = [];
			}
			const added = owner !== undefined && !same( last ) ?
				lines.slice( observedLine ? lines.findIndex( same ) + 1 : 0 ).filter( line =>
					line.channel === STALL_CHANNEL
				) :
				[];
			if ( added.length ) messages = [ ...messages, ...added ].slice( -STALL_HISTORY_LIMIT );
			chatOwner = owner;
			observedLine = last;
			return { cleared, appended: added.length > 0 };
		},
		/*
		================
		messages
		================
		*/
		messages(): readonly ChatLine[] {
			return messages;
		},
		/*
		================
		chat

		The stall chat box's draft (CIFChatModule, GDR_STALL_CHAT id 3).
		================
		*/
		chat(): string {
			return chat;
		},
		/*
		================
		typeChat
		================
		*/
		typeChat( raw: string ) {
			chat = raw.slice( 0, STALL_CHAT_LIMIT );
		},
		/*
		================
		searchReady
		================
		*/
		searchReady( now: number ): boolean {
			return now - searchedAt >= STALL_SEARCH_COOLDOWN_MS;
		},
		/*
		================
		searched
		================
		*/
		searched( now: number ) {
			searchedAt = now;
		},
		/*
		================
		network
		================
		*/
		network(): StallNetworkDraft {
			return network;
		},
		/*
		================
		toggleCombo
		================
		*/
		toggleCombo( combo: number ) {
			network = { ...network, combo: network.combo === combo ? 0 : combo };
		},
		/*
		================
		choose

		Picks an entry of the open combo; a new large category clears the
		medium one and the degree, a new medium category the degree.
		================
		*/
		choose( index: number ) {
			if ( network.combo === STALL_COMBO_LARGE ) network = { ...network, large: index, medium: -1, degree: 0 };
			else if ( network.combo === STALL_COMBO_MEDIUM ) network = { ...network, medium: index, degree: 0 };
			else if ( network.combo === STALL_COMBO_DEGREE ) network = { ...network, degree: index };
			network = { ...network, combo: 0 };
		},
		/*
		================
		selectRow
		================
		*/
		selectRow( row: number ) {
			network = { ...network, row };
		},
		/*
		================
		sortBy

		A column's button sorts by it; pressed again it reverses.
		================
		*/
		sortBy( sort: StallNetworkSort ) {
			network = { ...network, sort, ascending: network.sort === sort ? !network.ascending : true, row: -1 };
		},
		/*
		================
		resetNetwork
		================
		*/
		resetNetwork() {
			network = EMPTY_NETWORK;
		},
		/*
		================
		prompt
		================
		*/
		prompt(): StallPrompt | null {
			return prompt;
		},
		/*
		================
		open
		================
		*/
		open( next: StallPrompt ) {
			prompt = next;
		},
		/*
		================
		type

		Text is clipped to the field; the quantity is clamped to the carried
		stack and the price to a u32.
		================
		*/
		type( field: StallPromptField, raw: string ) {
			if ( !prompt ) return;
			if ( field === "text" && (prompt.kind === "title" || prompt.kind === "greeting") ) {
				prompt = {
					...prompt,
					text: raw.slice( 0, prompt.kind === "title" ? STALL_TITLE_LIMIT : STALL_TEXT_LIMIT )
				};
			} else if ( field === "quantity" && prompt.kind === "price" ) {
				const digits = raw.replace( /[^0-9]/g, "" ).slice( 0, QUANTITY_DIGITS );
				prompt = { ...prompt, quantity: digits ? String( Math.min( prompt.carried, Number( digits ) ) ) : "" };
			} else if ( field === "price" && prompt.kind === "price" ) {
				const digits = raw.replace( /[^0-9]/g, "" ).slice( 0, PRICE_DIGITS );
				prompt = { ...prompt, price: digits ? String( Math.min( MAX_PRICE, Number( digits ) ) ) : "" };
			}
		},
		/*
		================
		close
		================
		*/
		close() {
			prompt = null;
		}
	};
}
