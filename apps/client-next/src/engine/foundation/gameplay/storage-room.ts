/*
===========================================================================

storage-room.ts - the NPC warehouse (CIFStorageRoom) state and wire

The talk menu's storage row (CIFNpcTalk_ExecuteMenuAction case 3) sends
0x72C3 [u32 npc][u8 0] while the room is unloaded, then, once the gold
(0x3126) and the list (0x321A) arrived, the storage function itself:
0x7338 [u32 npc][u32 4]. B338 [1][u32 4] opens the room. A loaded room is
not listed again in the session (+0x7BC); the next visit asks for the
function directly.

Moves ride 0x706D (ItemMoveRequest_Serialize) and answer on 0xB06D:

	0x01 room to room       [src][dst][u16 count][u32 npc] -> [src][dst][u16 count]
	0x02 bag to room        [src][dst][u32 npc]            -> [src][dst]
	0x03 room to bag        [src][dst][u32 npc]            -> [src][dst]
	0x0B gold room to bag   [u32 amount]                    -> [u32 amount]
	0x0C gold bag to room   [u32 amount]                    -> [u32 amount]

The guild warehouse (window 0x91) at a guild manager takes another road:
0x7338 [npc][0x4000] -> B338 lock 0x4000 -> 0x7515 [npc] (B515 [1], or
[2][0x48][holder]) -> 0x733D [npc] -> gold 0x34A9, list 0x3363, B33D [1].
It is listed on every visit, and hiding it sends 0x7428 [npc]. Its moves
are the same five with the types 0x1D, 0x1E, 0x1F, 0x20 (gold in) and 0x21
(gold out); this owner keeps the personal kinds and maps them on the wire.

The remote warehouse ticket (3/3/13/10) has no native rule: v1.150 only
starts cooldown 0x1A on its 0xB5BD success. INFERENCE (the server's
storage.go owns it): the spent ticket opens this room with the player's own
gid standing in for the NPC, through the same requests.

===========================================================================
*/
import type { InventoryItem } from "@/engine/contracts/gameplay";
import type { WireFrame } from "@/engine/contracts/network";
import { planContainerMove, planWholeTransfer } from "./container-transfer";
import { decodeInventoryItem } from "./inventory-item";

export const OP_STORAGE_LIST_REQUEST = 0x72c3;
export const OP_STORAGE_GOLD = 0x3126;
export const OP_STORAGE_LIST = 0x321a;
const OP_NPC_ACTION = 0x7338;
const OP_NPC_INTERACTION = 0xb338;
const OP_ITEM_MOVE = 0x706d;
const OP_ITEM_MOVE_RESULT = 0xb06d;
// The storage capability bit and B338 lock value.
export const STORAGE_FUNCTION = 4;
// CIFStorageRoom pages its slots in rows of 30 (0x1E per page).
export const STORAGE_PAGE_SLOTS = 30;

export const STORAGE_MOVE_ROOM = 0x01;
export const STORAGE_MOVE_DEPOSIT = 0x02;
export const STORAGE_MOVE_WITHDRAW = 0x03;
export const STORAGE_GOLD_WITHDRAW = 0x0b;
export const STORAGE_GOLD_DEPOSIT = 0x0c;

// The guild warehouse's function mask and requests.
export const GUILD_STORAGE_FUNCTION = 0x4000;
const OP_GUILD_STORAGE_CLAIM = 0x7515;
const OP_GUILD_STORAGE_CLAIMED = 0xb515;
const OP_GUILD_STORAGE_LIST_REQUEST = 0x733d;
const OP_GUILD_STORAGE_LISTED = 0xb33d;
const OP_GUILD_STORAGE_RELEASE = 0x7428;
const OP_GUILD_STORAGE_RELEASED = 0xb428;
export const OP_GUILD_STORAGE_GOLD = 0x34a9;
export const OP_GUILD_STORAGE_LIST = 0x3363;
/*
================
storageWireType

The 0x706D type of a move kind in a personal or the guild warehouse.
================
*/
export function storageWireType( type: number, guild?: boolean ): number {
	if ( !guild ) return type;
	switch ( type ) {
		case STORAGE_MOVE_ROOM:
			return 0x1d;
		case STORAGE_MOVE_DEPOSIT:
			return 0x1e;
		case STORAGE_MOVE_WITHDRAW:
			return 0x1f;
		case STORAGE_GOLD_DEPOSIT:
			return 0x20;
		case STORAGE_GOLD_WITHDRAW:
			return 0x21;
	}
	return type;
}

/*
================
StorageRoom

phase: listing (0x72C3 / 0x733D sent), opening (0x7338 sent) or open
(B338 / B33D seen); a guild room passes function (0x7338 [0x4000] sent)
and claiming (0x7515 sent) first.
================
*/
export interface StorageRoom {
	readonly npc: number;
	readonly guild?: boolean;
	readonly phase: "function" | "claiming" | "listing" | "opening" | "open";
	readonly capacity: number;
	readonly gold: string;
	readonly items: readonly InventoryItem[];
}

/*
================
StorageMove

The request awaiting its 0xB06D answer.
================
*/
export interface StorageMove {
	readonly type: number;
	readonly source: number;
	readonly destination: number;
	readonly quantity: number;
	readonly gold: number;
}

// The ticket's packed type word: 3/3/13 (0x6EC) with type 4 = 10.
const WAREHOUSE_TICKET_FAMILY = 0x6ec;
const WAREHOUSE_TICKET_TYPE = 10;

/*
================
isWarehouseTicket
================
*/
export function isWarehouseTicket( word: number ): boolean {
	return !(word & 2) && (word & 0x7fc) === WAREHOUSE_TICKET_FAMILY && word >>> 11 === WAREHOUSE_TICKET_TYPE;
}

/*
================
storageListRequest
================
*/
export function storageListRequest( npc: number ): WireFrame {
	const payload = new Uint8Array( 5 );
	new DataView( payload.buffer ).setUint32( 0, npc, true );
	return { opcode: OP_STORAGE_LIST_REQUEST, payload };
}

/*
================
storageOpenRequest
================
*/
export function storageOpenRequest( npc: number ): WireFrame {
	const payload = new Uint8Array( 8 ), v = new DataView( payload.buffer );
	v.setUint32( 0, npc, true );
	v.setUint32( 4, STORAGE_FUNCTION, true );
	return { opcode: OP_NPC_ACTION, payload };
}

/*
================
storageMoveRequest
================
*/
export function storageMoveRequest( npc: number, move: StorageMove, guild?: boolean ): WireFrame {
	if ( move.type === STORAGE_GOLD_WITHDRAW || move.type === STORAGE_GOLD_DEPOSIT ) {
		if ( !Number.isInteger( move.gold ) || move.gold < 1 || move.gold > 0xffffffff ) {
			throw Error( "Invalid storage gold" );
		}
		const payload = new Uint8Array( 5 );
		payload[0] = storageWireType( move.type, guild );
		new DataView( payload.buffer ).setUint32( 1, move.gold, true );
		return { opcode: OP_ITEM_MOVE, payload };
	}
	if ( ![ move.source, move.destination ].every( slot => Number.isInteger( slot ) && slot >= 0 && slot <= 255 ) ) {
		throw Error( "Invalid storage slot" );
	}
	const room = move.type === STORAGE_MOVE_ROOM;
	const payload = new Uint8Array( room ? 9 : 7 ), v = new DataView( payload.buffer );
	payload[0] = storageWireType( move.type, guild );
	payload[1] = move.source;
	payload[2] = move.destination;
	if ( room ) v.setUint16( 3, move.quantity, true );
	v.setUint32( room ? 5 : 3, npc, true );
	return { opcode: OP_ITEM_MOVE, payload };
}

/*
================
decodeStorageList

[u8 capacity][u8 count] count x {[u8 slot][CSOItem]}.
================
*/
export function decodeStorageList( p: Uint8Array, refs: ReadonlyMap<number, number> ) {
	if ( p.length < 2 ) throw Error( "Invalid storage list" );
	const capacity = p[0]!, count = p[1]!, items: InventoryItem[] = [];
	if ( capacity < 1 ) throw Error( "Invalid storage capacity" );
	let o = 2;
	for ( let i = 0; i < count; i++ ) {
		if ( o >= p.length ) throw Error( "Truncated storage list" );
		const slot = p[o]!;
		const decoded = decodeInventoryItem( p, o + 1, refs );
		if ( !decoded.item || slot >= capacity || items.some( row => row.slot === slot ) ) {
			throw Error( "Invalid storage row" );
		}
		items.push( { ...decoded.item, slot } );
		o = decoded.next;
	}
	if ( o !== p.length ) throw Error( "Invalid storage list length" );
	return { capacity, items: items.sort( ( a, b ) => a.slot - b.slot ) };
}

/*
================
storageMoveResult

The bag and room after an acknowledged move, or null when the frame is not
a storage move. A refusal ([2][code]) returns the unchanged planes.
================
*/
export function storageMoveResult(
	frame: WireFrame,
	room: StorageRoom,
	bag: readonly InventoryItem[],
	pending: StorageMove,
	caps: ReadonlyMap<number, number>
): { readonly room: StorageRoom; readonly bag: readonly InventoryItem[]; } | null {
	const p = frame.payload;
	if (
		frame.opcode !== OP_ITEM_MOVE_RESULT || p.length < 2 || p[1] !== storageWireType( pending.type, room.guild )
	) {
		return null;
	}
	if ( p[0] !== 1 ) return { room, bag };
	if ( pending.type === STORAGE_MOVE_ROOM ) {
		const items = planContainerMove( room.items, pending, caps, "storage" );
		return { room: { ...room, items }, bag };
	}
	if ( pending.type === STORAGE_MOVE_DEPOSIT ) {
		const moved = planWholeTransfer( bag, room.items, pending.source, pending.destination, caps );
		return { room: { ...room, items: moved.to }, bag: moved.from };
	}
	if ( pending.type === STORAGE_MOVE_WITHDRAW ) {
		const moved = planWholeTransfer( room.items, bag, pending.source, pending.destination, caps );
		return { room: { ...room, items: moved.from }, bag: moved.to };
	}
	const gold = BigInt( room.gold ), amount = BigInt( pending.gold );
	return {
		room: { ...room, gold: (pending.type === STORAGE_GOLD_DEPOSIT ? gold + amount : gold - amount).toString() },
		bag
	};
}

/*
================
storageOpened

B338 [1][u32 4] for the storage function.
================
*/
export function storageOpened( frame: WireFrame ): boolean {
	const p = frame.payload;
	return frame.opcode === OP_NPC_INTERACTION && p.length === 5 && p[0] === 1 &&
		new DataView( p.buffer, p.byteOffset, p.byteLength ).getUint32( 1, true ) === STORAGE_FUNCTION;
}

/*
================
npcRequest

A request whose body is the NPC alone.
================
*/
function npcRequest( opcode: number, npc: number ): WireFrame {
	const payload = new Uint8Array( 4 );
	new DataView( payload.buffer ).setUint32( 0, npc, true );
	return { opcode, payload };
}

/*
================
guildStorageFunctionRequest

0x7338 [u32 npc][u32 0x4000]: the warehouse row (5DA1B0 case 0x1D).
================
*/
export function guildStorageFunctionRequest( npc: number ): WireFrame {
	const payload = new Uint8Array( 8 ), v = new DataView( payload.buffer );
	v.setUint32( 0, npc, true );
	v.setUint32( 4, GUILD_STORAGE_FUNCTION, true );
	return { opcode: OP_NPC_ACTION, payload };
}

/*
================
createStorageRoom

The owner of the local player's warehouse: the session's loaded flag
(+0x7BC), the room rows and gold, and the open phase. Moves change the room
only through apply(), fed by the inventory transaction that owns them.
================
*/
export function createStorageRoom( send: ( frame: WireFrame ) => void ) {
	let room: StorageRoom | null = null;
	let loaded: { readonly capacity: number; readonly gold: string; readonly items: readonly InventoryItem[]; } | null =
		null;
	let gold = "0";
	return {
		/*
================
open

The talk menu's storage row: list the room once, then ask for the function.
================
		*/
		open( npc: number ) {
			if ( !Number.isInteger( npc ) || npc < 1 || npc > 0xffffffff ) throw Error( "Invalid storage NPC" );
			if ( loaded ) {
				room = { npc, phase: "opening", ...loaded };
				send( storageOpenRequest( npc ) );
				return;
			}
			room = { npc, phase: "listing", capacity: 0, gold, items: [] };
			send( storageListRequest( npc ) );
		},
		/*
================
openGuild

The guild manager's warehouse row: ask for the function, then the room.
================
		*/
		openGuild( npc: number ) {
			if ( !Number.isInteger( npc ) || npc < 1 || npc > 0xffffffff ) throw Error( "Invalid guild manager" );
			room = { npc, guild: true, phase: "function", capacity: 0, gold: "0", items: [] };
			send( guildStorageFunctionRequest( npc ) );
		},
		/*
================
receive

True when the frame belonged to the warehouse.
================
		*/
		receive( frame: WireFrame, refs: ReadonlyMap<number, number> ): boolean {
			const p = frame.payload;
			if ( room?.guild ) {
				const v = new DataView( p.buffer, p.byteOffset, p.byteLength );
				// B338 lock 0x4000 sends the claim itself (75AE50).
				if (
					frame.opcode === OP_NPC_INTERACTION && p.length === 5 && p[0] === 1 &&
					v.getUint32( 1, true ) === GUILD_STORAGE_FUNCTION
				) {
					if ( room.phase === "function" ) {
						room = { ...room, phase: "claiming" };
						send( npcRequest( OP_GUILD_STORAGE_CLAIM, room.npc ) );
					}
					return true;
				}
				if ( frame.opcode === OP_GUILD_STORAGE_CLAIMED ) {
					// A refusal releases the room; the social owner names it.
					if ( p[0] !== 1 ) {
						room = null;
						return false;
					}
					if ( p.length !== 1 ) throw Error( "Invalid guild storage claim" );
					if ( room.phase === "claiming" ) {
						room = { ...room, phase: "listing" };
						send( npcRequest( OP_GUILD_STORAGE_LIST_REQUEST, room.npc ) );
					}
					return true;
				}
				if ( frame.opcode === OP_GUILD_STORAGE_GOLD ) {
					if ( p.length !== 8 ) throw Error( "Invalid guild storage gold" );
					room = { ...room, gold: v.getBigUint64( 0, true ).toString() };
					return true;
				}
				if ( frame.opcode === OP_GUILD_STORAGE_LIST ) {
					room = { ...room, ...decodeStorageList( p, refs ) };
					return true;
				}
				if ( frame.opcode === OP_GUILD_STORAGE_LISTED ) {
					if ( p[0] === 1 && room.phase === "listing" ) room = { ...room, phase: "open" };
					else if ( p[0] !== 1 ) room = null;
					return true;
				}
			}
			if ( frame.opcode === OP_GUILD_STORAGE_RELEASED ) return true;
			if ( frame.opcode === OP_STORAGE_GOLD ) {
				if ( frame.payload.length !== 8 ) throw Error( "Invalid storage gold" );
				gold = new DataView( frame.payload.buffer, frame.payload.byteOffset, 8 ).getBigUint64( 0, true )
					.toString();
				if ( room ) room = { ...room, gold };
				return true;
			}
			if ( frame.opcode === OP_STORAGE_LIST ) {
				const list = decodeStorageList( frame.payload, refs );
				loaded = { ...list, gold };
				if ( room?.phase === "listing" ) {
					room = { ...room, phase: "opening", ...loaded };
					send( storageOpenRequest( room.npc ) );
				}
				return true;
			}
			if ( storageOpened( frame ) ) {
				if ( room?.phase === "opening" ) room = { ...room, phase: "open" };
				return true;
			}
			// 75AE50 kind 2: the warehouse refused (code 4, too far). A room that
			// never opened is released; the caller still shows the notice.
			if ( frame.opcode === OP_NPC_INTERACTION && frame.payload[0] === 2 && room && room.phase !== "open" ) {
				room = null;
			}
			return false;
		},
		/*
================
apply

The room after an acknowledged move; the session copy follows it.
================
		*/
		apply( next: StorageRoom ) {
			room = next;
			if ( next.guild ) return;
			loaded = { capacity: next.capacity, gold: next.gold, items: next.items };
			gold = next.gold;
		},
		/*
================
close

Leaving the warehouse keeps the session copy (+0x7BC stays set).
================
		*/
		close() {
			if ( room?.guild && room.phase !== "function" ) send( npcRequest( OP_GUILD_STORAGE_RELEASE, room.npc ) );
			room = null;
		},
		/*
================
reset

World leave: the next session lists again.
================
		*/
		reset() {
			room = null;
			loaded = null;
			gold = "0";
		},
		/*
================
state
================
		*/
		state() {
			return room;
		}
	};
}
