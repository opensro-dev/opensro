/*
===========================================================================

stall.ts - the street stall window and stall network's state and wire

CIFStall (interface child 0x21) is the own stall after 0xB049 [1]
(74F7A0) or another player's after 0xB61F [1][u32 owner][wstr greeting]
[u8 open][u8 mode] offers [u8 n][u32 gid]... (751720). Offers are [u8
slot][CSOItem][u8 bag slot][u16 count][u32 price] until a 0xFF slot
(74F9D0). 0xB1A8 [1][kind] carries an edit to everyone at the stall
(751A60); 0x3260 a visitor coming (2) or going (1), or a slot sold (3, 4
through the network) with the offers left (755960). 0xB3F9 [1][slot]
hands a buyer the slot's item into its first empty bag slot (5A3870);
the owner's bag gives the sold count up on 0x3260 3/4. 0xB42C and
0xB6E7 close the window, as does 0x33D1 for the stall the reader stands
at (74F8F0).

The stall network (CIFStallNetwork): 0xB6F9 [1][u8 rows][u8 pages] rows
of [CSOItem][u32 owner][u8 slot][u16 count][u8][u64 price][u64 serial]
(766FC0, 5AEDC0); 0xB2CA [1] hands the bought row's item into the first
empty bag slot (5AE020).

Action 1009 (695420 case 9) opens the title prompt first ("naming");
its OK (CIFStall_OnTitleDialogResult 5A2890) sends 0x7049 and then the
greeting edit: the one last typed (CICUser+7E4), else the formatted
UIIT_STT_STALL_DEFAULT_OWNERMSG.

===========================================================================
*/
import type { WireFrame } from "@/engine/contracts/network";
import type { InventoryItem } from "@/engine/contracts/gameplay";
import { decodeInventoryItem } from "./inventory-item";

export const STALL_SLOTS = 10;
// 0x7367/0x3667 type 9: the stall window's own chat box (CIFChatModule
// 545EF0); its lines show only there (ClientChat_PresentChannelMessage
// 752800 -> CGInterface_AppendChatModuleLine 67AEE0), as "name:text".
export const STALL_CHAT_CHANNEL = 9;
export const STALL_NETWORK_PAGE_ROWS = 15;
// CGInterface_ShowSystemNotification categories of the stall refusals.
export const STALL_NOTICE_CATEGORY = 0x0a;
const STALL_NETWORK_SUPPLIER_CATEGORY = 0x1b;
// 695420 case 9: partyActiveJob's class without a job suit (856920 +4F5);
// any other refuses 0x0A/0x3B, an open alchemy window 0x12/0x16.
export const STALL_NO_JOB_CLASS = 4;
export const STALL_JOB_SUIT_CODE = 0x3b;
export const STALL_ALCHEMY_CATEGORY = 0x12;
export const STALL_ALCHEMY_CODE = 0x16;
const OFFERS_END = 0xff;
// 0x71A8 edit kinds.
const EDIT_MODIFY = 1;
const EDIT_ADD = 2;
const EDIT_REMOVE = 3;
const EDIT_MODE = 4;
const EDIT_OPEN = 5;
const EDIT_GREETING = 6;
const EDIT_TITLE = 7;
// 0x3260 events.
const EVENT_LEFT = 1;
const EVENT_CAME = 2;
const EVENT_SOLD = 3;
const EVENT_NETWORK_SOLD = 4;
const NETWORK_REGISTERED = 1;

/*
================
StallOffer
================
*/
export interface StallOffer {
	readonly slot: number;
	readonly item: InventoryItem;
	readonly bagSlot: number;
	readonly quantity: number;
	readonly price: number;
}

/*
================
StallListing
================
*/
export interface StallListing {
	readonly item: InventoryItem;
	readonly owner: number;
	readonly slot: number;
	readonly quantity: number;
	readonly price: bigint;
	readonly serial: bigint;
}

/*
================
StallState
================
*/
export interface StallState {
	readonly phase: "none" | "naming" | "owner" | "visitor";
	// Local identity of an accepted naming action, retained across worker publications.
	readonly namingSequence: number;
	readonly owner: number;
	readonly title: string;
	readonly greeting: string;
	// CICUser+7E4: the greeting last typed, kept across stalls.
	readonly savedGreeting: string;
	readonly open: boolean;
	readonly mode: number;
	readonly networkRegistered: boolean;
	readonly offers: readonly StallOffer[];
	readonly visitors: readonly number[];
	readonly network: {
		readonly open: boolean;
		readonly rows: readonly StallListing[];
		readonly pages: number;
		readonly page: number;
		readonly category: number;
		readonly buying: number | null;
	};
}

/*
================
emptyStall
================
*/
export function emptyStall( namingSequence = 0 ): StallState {
	return {
		phase: "none",
		namingSequence,
		owner: 0,
		title: "",
		greeting: "",
		savedGreeting: "",
		open: false,
		mode: 0,
		networkRegistered: false,
		offers: [],
		visitors: [],
		network: { open: false, rows: [], pages: 0, page: 0, category: 0, buying: null }
	};
}

/*
================
StallOutcome

The next state, a notice (category and code), and the bag changes the
frame makes: items to place in the first empty bag slots, bag slots to
reduce by a count.
================
*/
export interface StallOutcome {
	readonly state: StallState;
	readonly notice?: { readonly category: number; readonly code: number; };
	readonly receive?: readonly InventoryItem[];
	readonly give?: readonly { readonly bagSlot: number; readonly quantity: number; }[];
	readonly message?: { readonly key: string; readonly args: readonly string[]; };
}

/*
================
StallContext
================
*/
export interface StallContext {
	readonly localGid: number;
	readonly refs: ReadonlyMap<number, number>;
	readonly objRefs?: ReadonlyMap<number, number>;
}

/*
================
stallFrame

Folds one frame; null for frames the stall does not own.
================
*/
export function stallFrame( state: StallState, frame: WireFrame, ctx: StallContext ): StallOutcome | null {
	const p = frame.payload, v = new DataView( p.buffer, p.byteOffset, p.byteLength );
	let o = 0;
	const take = ( n: number ) => {
		if ( o + n > p.length ) throw Error( "Truncated stall frame" );
		const at = o;
		o += n;
		return at;
	};
	const u8 = () => p[take( 1 )]!,
		u16 = () => v.getUint16( take( 2 ), true ),
		u32 = () => v.getUint32( take( 4 ), true ),
		u64 = () => v.getBigUint64( take( 8 ), true );
	const wstr = () => {
		const n = u16(), at = take( n * 2 );
		return new TextDecoder( "utf-16le" ).decode( p.subarray( at, at + n * 2 ) );
	};
	const str = () => {
		const n = u16(), at = take( n );
		return String.fromCharCode( ...p.subarray( at, at + n ) );
	};
	const item = () => {
		const decoded = decodeInventoryItem( p, o, ctx.refs, ctx.objRefs );
		o = decoded.next;
		if ( !decoded.item ) throw Error( "Empty stall item" );
		return decoded.item;
	};
	const offers = () => {
		const out: StallOffer[] = [];
		for ( let slot = u8(); slot !== OFFERS_END; slot = u8() ) {
			const body = item(), bagSlot = u8(), quantity = u16(), price = u32();
			out.push( { slot, item: { ...body, quantity }, bagSlot, quantity, price } );
		}
		return out;
	};
	const done = ( outcome: StallOutcome ) => {
		if ( o !== p.length ) throw Error( "Trailing stall bytes" );
		return outcome;
	};
	const refusal = ( category = STALL_NOTICE_CATEGORY ): StallOutcome => {
		if ( p.length !== 2 ) throw Error( "Invalid stall refusal" );
		return { state, notice: { category, code: p[1]! } };
	};
	switch ( frame.opcode ) {
		case 0xb049:
			if ( p[0] !== 1 ) return refusal();
			o = 1;
			return done( {
				state: {
					...emptyStall( state.namingSequence ),
					network: state.network,
					savedGreeting: state.savedGreeting,
					phase: "owner",
					owner: ctx.localGid,
					title: state.title
				}
			} );
		case 0xb42c:
		case 0xb6e7:
			if ( p[0] !== 1 ) return refusal();
			o = 1;
			return done( {
				state: {
					...emptyStall( state.namingSequence ),
					network: state.network,
					savedGreeting: state.savedGreeting
				}
			} );
		case 0xb61f: {
			if ( p[0] !== 1 ) return refusal();
			o = 1;
			const owner = u32(), greeting = wstr(), open = u8() === 1, mode = u8(), rows = offers();
			const visitors = Array.from( { length: u8() }, u32 );
			return done( {
				state: {
					...emptyStall( state.namingSequence ),
					network: state.network,
					savedGreeting: state.savedGreeting,
					phase: "visitor",
					owner,
					greeting,
					open,
					mode,
					offers: rows,
					visitors
				}
			} );
		}
		case 0xb3f9: {
			if ( p[0] !== 1 ) return refusal();
			o = 1;
			const slot = u8(), offer = state.offers.find( row => row.slot === slot );
			return done( { state, ...(offer ? { receive: [ offer.item ] } : {}) } );
		}
		case 0xb1a8: {
			if ( p[0] !== 1 ) return refusal();
			o = 1;
			const kind = u8();
			let next = state, notice: StallOutcome["notice"];
			if ( kind === EDIT_MODIFY ) {
				const slot = u8(), quantity = u16(), price = u32(), code = u8();
				next = {
					...state,
					offers: state.offers.map( row =>
						row.slot === slot ? { ...row, quantity, price, item: { ...row.item, quantity } } : row
					)
				};
				if ( code !== 0 && code !== 4 ) notice = { category: STALL_NOTICE_CATEGORY, code };
			} else if ( kind === EDIT_ADD || kind === EDIT_REMOVE ) {
				const code = u8();
				next = { ...state, offers: offers() };
				if ( code !== 0 && code !== (kind === EDIT_ADD ? 2 : 5) ) {
					notice = { category: STALL_NOTICE_CATEGORY, code };
				}
			} else if ( kind === EDIT_MODE ) next = { ...state, mode: u8() };
			else if ( kind === EDIT_OPEN ) {
				const open = u8() === 1, code = u8();
				next = { ...state, open, networkRegistered: open && code === NETWORK_REGISTERED };
				if ( code !== 0 && code !== NETWORK_REGISTERED && code !== 2 && code !== 3 ) {
					notice = { category: STALL_NOTICE_CATEGORY, code };
				}
			} else if ( kind === EDIT_GREETING ) next = { ...state, greeting: wstr() };
			return done( { state: next, ...(notice ? { notice } : {}) } );
		}
		case 0x3260: {
			if ( state.phase === "none" ) return null;
			const kind = u8();
			if ( kind === EVENT_LEFT || kind === EVENT_CAME ) {
				const gid = u32();
				return done( {
					state: {
						...state,
						visitors: kind === EVENT_CAME ?
							[ ...state.visitors.filter( g => g !== gid ), gid ] :
							state.visitors.filter( g => g !== gid )
					}
				} );
			}
			if ( kind !== EVENT_SOLD && kind !== EVENT_NETWORK_SOLD ) throw Error( "Unknown stall event" );
			const slot = u8(), buyer = str(), sold = state.offers.find( row => row.slot === slot ), rows = offers();
			const mine = state.phase === "owner";
			return done( {
				state: { ...state, offers: rows },
				...(mine && sold ? { give: [ { bagSlot: sold.bagSlot, quantity: sold.quantity } ] } : {}),
				...(mine && sold ?
					{
						message: {
							key: kind === EVENT_SOLD ?
								"UIIT_MSG_STREET_STORE_SELL_COMMODITY" :
								"UIIT_MSG_WARENETWORK_BUY_SUCCESS",
							args: [ buyer, sold.item.name ?? "" ]
						}
					} :
					{})
			} );
		}
		case 0x33d1: {
			const gid = u32();
			u8();
			if ( state.phase === "visitor" && state.owner === gid ) {
				return done( {
					state: {
						...emptyStall( state.namingSequence ),
						network: state.network,
						savedGreeting: state.savedGreeting
					}
				} );
			}
			return null;
		}
		case 0xb6f9: {
			if ( p[0] !== 1 ) {
				if ( p.length !== 2 ) throw Error( "Invalid stall network refusal" );
				return {
					state: p[1] === 0x49 ? { ...state, network: { ...state.network, open: false } } : state,
					notice: { category: STALL_NOTICE_CATEGORY, code: p[1]! }
				};
			}
			o = 1;
			const count = u8(), pages = u8(), rows: StallListing[] = [];
			for ( let i = 0; i < count; i++ ) {
				const body = item(), owner = u32(), slot = u8(), quantity = u16();
				u8();
				const price = u64(), serial = u64();
				rows.push( { item: { ...body, quantity }, owner, slot, quantity, price, serial } );
			}
			return done( { state: { ...state, network: { ...state.network, rows, pages } } } );
		}
		case 0xb2ca: {
			const buying = state.network.buying;
			const network = { ...state.network, buying: null };
			if ( p[0] !== 1 ) {
				if ( p.length !== 2 ) throw Error( "Invalid stall network purchase" );
				const code = p[1]!;
				return {
					state: { ...state, network },
					notice: {
						category: code > 0 && code <= 5 ? STALL_NETWORK_SUPPLIER_CATEGORY : STALL_NOTICE_CATEGORY,
						code
					}
				};
			}
			const row = buying === null ? undefined : state.network.rows[buying];
			o = 1;
			return done( {
				state: {
					...state,
					network: { ...network, rows: state.network.rows.filter( ( _, i ) => i !== buying ) }
				},
				...(row ? { receive: [ row.item ] } : {})
			} );
		}
	}
	return null;
}

/*
================
StallCommand
================
*/
export type StallCommand =
	| { readonly kind: "stall-name"; readonly alchemy?: boolean; }
	| { readonly kind: "stall-name-cancel"; }
	| { readonly kind: "stall-create"; readonly title: string; readonly greeting: string; }
	| { readonly kind: "stall-close"; }
	| { readonly kind: "stall-visit"; readonly gid: number; }
	| { readonly kind: "stall-leave"; }
	| { readonly kind: "stall-buy"; readonly slot: number; }
	| {
		readonly kind: "stall-add";
		readonly slot: number;
		readonly bagSlot: number;
		readonly quantity: number;
		readonly price: number;
	}
	| { readonly kind: "stall-modify"; readonly slot: number; readonly quantity: number; readonly price: number; }
	| { readonly kind: "stall-remove"; readonly slot: number; }
	| { readonly kind: "stall-open"; readonly open: boolean; readonly network: boolean; }
	| { readonly kind: "stall-greeting"; readonly text: string; }
	| { readonly kind: "stall-title"; readonly text: string; }
	| { readonly kind: "stall-network-open"; readonly open: boolean; }
	| {
		readonly kind: "stall-network-search";
		readonly category: number;
		readonly page: number;
		readonly degree: number;
	}
	| { readonly kind: "stall-network-buy"; readonly row: number; };

/*
================
stallRequest

The frames a command sends (none for a command that only changes the
window), and the state it leaves.
================
*/
export function stallRequest(
	state: StallState,
	command: StallCommand
): { readonly frames: readonly WireFrame[]; readonly state: StallState; } {
	const frames: WireFrame[] = [];
	let bytes: number[] = [];
	const u8 = ( value: number ) => bytes.push( value & 0xff );
	const u16 = ( value: number ) => bytes.push( value & 0xff, value >> 8 & 0xff );
	const u32 = ( value: number ) => {
		for ( let i = 0; i < 4; i++ ) bytes.push( value >>> i * 8 & 0xff );
	};
	const u64 = ( value: bigint ) => {
		for ( let i = 0n; i < 8n; i++ ) bytes.push( Number( value >> i * 8n & 0xffn ) );
	};
	const wstr = ( value: string ) => {
		if ( value.length === 0 || value.length > 64 ) throw Error( "Stall text length" );
		u16( value.length );
		for ( let i = 0; i < value.length; i++ ) u16( value.charCodeAt( i ) );
	};
	const send = ( opcode: number, next = state ) => {
		frames.push( { opcode, payload: Uint8Array.from( bytes ) } );
		bytes = [];
		return { frames, state: next };
	};
	const owner = state.phase === "owner";
	switch ( command.kind ) {
		case "stall-name":
			if ( state.phase !== "none" ) throw Error( "Already at a stall" );
			// A cancel and another action may coalesce into one naming publication.
			return { frames, state: { ...state, phase: "naming", namingSequence: state.namingSequence + 1 } };
		case "stall-name-cancel":
			if ( state.phase !== "naming" ) throw Error( "No stall is being named" );
			return { frames, state: { ...state, phase: "none" } };
		case "stall-create":
			if ( state.phase !== "naming" ) throw Error( "No stall is being named" );
			wstr( command.title );
			send( 0x7049 );
			u8( EDIT_GREETING );
			wstr( command.greeting );
			return send( 0x71a8, { ...state, phase: "none", title: command.title } );
		case "stall-close":
			if ( !owner ) throw Error( "No stall to close" );
			return send( 0x742c );
		case "stall-visit":
			if ( state.phase !== "none" ) throw Error( "Already at a stall" );
			u32( command.gid );
			return send( 0x761f );
		case "stall-leave":
			if ( state.phase !== "visitor" ) throw Error( "Not at a stall" );
			return send( 0x76e7 );
		case "stall-buy":
			if ( state.phase !== "visitor" || !state.open ) throw Error( "The stall is not open" );
			u8( command.slot );
			return send( 0x73f9 );
		case "stall-add":
			if ( !owner || state.open ) throw Error( "Close the stall to change it" );
			u8( EDIT_ADD );
			u8( command.slot );
			u8( command.bagSlot );
			u16( command.quantity );
			u32( command.price );
			u32( 0 );
			u8( 0 );
			return send( 0x71a8 );
		case "stall-modify":
			if ( !owner || state.open ) throw Error( "Close the stall to change it" );
			u8( EDIT_MODIFY );
			u8( command.slot );
			u16( command.quantity );
			u32( command.price );
			u8( 0 );
			return send( 0x71a8 );
		case "stall-remove":
			if ( !owner || state.open ) throw Error( "Close the stall to change it" );
			u8( EDIT_REMOVE );
			u8( command.slot );
			u8( 0 );
			return send( 0x71a8 );
		case "stall-open":
			if ( !owner ) throw Error( "No stall" );
			u8( EDIT_OPEN );
			u8( command.open ? 1 : 0 );
			u8( command.network ? 1 : 0 );
			return send( 0x71a8 );
		case "stall-greeting":
			if ( !owner ) throw Error( "No stall" );
			u8( EDIT_GREETING );
			wstr( command.text );
			return send( 0x71a8, { ...state, savedGreeting: command.text } );
		case "stall-title":
			if ( !owner || state.open ) throw Error( "Close the stall to change it" );
			u8( EDIT_TITLE );
			wstr( command.text );
			return send( 0x71a8, { ...state, title: command.text } );
		case "stall-network-open":
			return { frames, state: { ...state, network: { ...state.network, open: command.open } } };
		case "stall-network-search":
			u8( 0 );
			u8( command.page );
			u32( command.category );
			u8( command.degree );
			return send( 0x76f9, {
				...state,
				network: { ...state.network, category: command.category, page: command.page }
			} );
		case "stall-network-buy": {
			const row = state.network.rows[command.row];
			if ( !row || state.network.buying !== null ) throw Error( "No such stall network row" );
			u32( row.owner );
			u8( row.slot );
			u64( row.price );
			u16( row.quantity );
			u8( 0 );
			u64( row.serial );
			return send( 0x72ca, { ...state, network: { ...state.network, buying: command.row } } );
		}
	}
}
