/*
===========================================================================

inventory.ts - inventory authority publications and serialized item commands

Owns player and avatar slots, reference projections and pending native moves.
Child owners handle process-specific state while this owner commits item rows.

===========================================================================
*/
import { cosItemUseTail, type CosItemUseContext } from "@/engine/foundation/gameplay/cos-item-use";
import { createMall } from "./mall/mall";
import type { MallPurchase } from "@/engine/foundation/gameplay/item-mall-wire";
import {
	itemCooldown,
	recoveryCategory,
	recoveryCooldownMs,
	type ItemCooldown
} from "@/engine/foundation/gameplay/item-cooldowns";
import {
	itemTooltipReference,
	itemMagicReferences,
	type ItemTooltipReference
} from "@/engine/foundation/gameplay/item-tooltip-reference";
import { planCosTransfer, cosTransferRequest } from "@/engine/foundation/gameplay/cos-transfer";
import {
	storageMoveRequest,
	storageMoveResult,
	STORAGE_MOVE_ROOM,
	STORAGE_MOVE_DEPOSIT,
	STORAGE_MOVE_WITHDRAW,
	type StorageMove,
	type StorageRoom
} from "@/engine/foundation/gameplay/storage-room";
import { createAlchemy } from "./alchemy/alchemy";
import { REPAIR_ONE_SLOT, REPAIR_RESPONSE_OPCODE, repairRequest } from "@/engine/foundation/gameplay/repair";
import { createGacha } from "./gacha/gacha";
import { itemStateDelta } from "@/engine/foundation/gameplay/item-state-delta";
import { itemSlotFlashKinds } from "@/engine/foundation/ui/item-slot-effects";
import type { ItemProcessCommand } from "@/engine/contracts/item-process";
import {
	saleResult,
	soldInventory,
	commerceJson,
	commerceInteger,
	shopCatalog,
	buybackEntries
} from "@/engine/foundation/gameplay/commerce";
import { decodeInventoryItem } from "@/engine/foundation/gameplay/inventory-item";
import { equipDurabilityWarning } from "@/engine/foundation/audio/item-sounds";
import type { InventoryItem } from "@/engine/contracts/gameplay";
/*
================
createInventory
================
*/
export function createInventory(
	send: ( frame: import("@/engine/contracts/network").WireFrame ) => void,
	play: ( handle: import("@/engine/foundation/ui/sound-catalog").UiSoundHandle ) => void = () => {},
	playItem: ( cue: import("@/engine/contracts/audio").ItemSoundRequest ) => void = () => {}
) {
	const alchemy = createAlchemy(), gacha = createGacha(), mall = createMall();
	let mallDelivery: { prepared: ReturnType<typeof decodeShopItems>; slots: number[]; } | null = null;
	let avatars = new Map<number, InventoryItem>();
	let slots = new Map<number, InventoryItem>(),
		pending: {
			receivedSlots?: readonly number[];
			cosGid?: number;
			opcode: number;
			source: number;
			destination?: number;
			movementType?: number;
			amount?: number;
			npc?: number;
			tab?: number;
			quantity?: number;
			snapshot?: boolean;
			buybackId?: number;
			// A warehouse move (storage-room.ts); settled by storageSettle.
			storage?: boolean;
			buybackIndex?: number;
			deadline: number;
		} | null = null,
		error: string | null = null;
	let itemCooldowns: readonly ItemCooldown[] = [];
	let timedOut = false;
	let bindingMoves: import("@/engine/foundation/gameplay/quickslot-inventory").QuickslotInventoryMove[] = [];
	let shop: import("@/engine/foundation/gameplay/commerce").ShopState | undefined;
	let shopCompletionRevision = 0;
	let shopSource: typeof shop, shopPresentation: typeof shop;
	/*
================
preview
================
	*/
	function preview( row: import("@/engine/foundation/gameplay/commerce").CommercePreview ): InventoryItem {
		const bytes = Uint8Array.from( row.body ), types = new Map( refs );
		if ( types.has( row.refObjId ) && types.get( row.refObjId ) !== row.typeFlags ) {
			throw Error( "Conflicting commerce preview type" );
		}
		types.set( row.refObjId, row.typeFlags );
		const decoded = decodeInventoryItem( bytes, 0, types, objRefs );
		if ( !decoded.item || decoded.next !== bytes.length ) throw Error( "Invalid commerce preview body" );
		return { ...decoded.item, name: row.name };
	}
	/*
================
presentShop
================
	*/
	function presentShop() {
		if ( !shop ) {
			shopSource = undefined;
			shopPresentation = undefined;
			return undefined;
		}
		if ( shopSource !== shop ) {
			shopSource = shop;
			shopPresentation = {
				...shop,
				offers: shop.offers.map( row => ({
					...row,
					icon: icons.get( row.refObjId ),
					items: row.items?.map( present )
				}) ),
				buyback: shop.buyback?.map( row => ({
					...row,
					icon: icons.get( row.refObjId ),
					item: row.item ? present( row.item ) : undefined
				}) )
			};
		}
		return shopPresentation;
	}
	let published: readonly InventoryItem[] | null = null;
	// One-shot slot flashes the 0x3645 item-state update raised (7654B0); each
	// lasts under 1.3 s, so older entries are dropped on the next update.
	let itemFlashes: readonly { readonly slot: number; readonly kind: "changed" | "life"; readonly atMs: number; }[] =
		[];
	const FLASH_RETENTION_MS = 2000;
	const objRefs = new Map<number, number>();
	const useCooldowns = new Map<number, number>();
	const refs = new Map<number, number>(), names = new Map<number, string>();
	const icons = new Map<number, string>();
	const tooltipRefs = new Map<number, ItemTooltipReference>();
	let magicRefs = itemMagicReferences( undefined );
	let presentations = new WeakMap<InventoryItem, InventoryItem>();
	/*
================
present
================
	*/
	function present( item: InventoryItem ): InventoryItem {
		const cached = presentations.get( item );
		if ( cached ) return cached;
		const tooltip = tooltipRefs.get( item.refObjId ),
			degree = Math.floor( ((tooltip?.fields.itemClass ?? 1) - 1) / 3 ) + 1,
			definitions = new Map<
				number,
				import("@/engine/foundation/gameplay/item-tooltip-reference").ItemMagicReference
			>();
		for ( const encoded of item.magic ) {
			const ref = magicRefs.get( Number( BigInt( encoded ) & 0xffffn ) );
			if ( !ref ) continue;
			definitions.set( ref.paramId, ref );
			for ( const candidate of magicRefs.values() ) {
				if ( candidate.optionName === ref.optionName && candidate.degree === degree ) {
					definitions.set( candidate.paramId, candidate );
				}
			}
		}
		const result = {
			...item,
			name: names.get( item.refObjId ) ?? item.name,
			icon: icons.get( item.refObjId ) ?? item.icon,
			tooltip,
			magicReferences: [ ...definitions.values() ]
		};
		presentations.set( item, result );
		return result;
	}
	let inventorySlotCount: number | undefined, equipmentSlotCount: number | undefined;
	/*
================
slot
================
	*/
	function slot( n: number ) {
		if ( !Number.isInteger( n ) || n < 0 || n > 255 ) {
			throw new Error( "Invalid inventory slot" );
		}
		return n;
	}
	/*
================
body
================
	*/
	function body( p: Uint8Array, index: number ): InventoryItem | null {
		const { item, next } = decodeInventoryItem( p, index, refs, objRefs );
		if ( next !== p.length ) throw new Error( "Invalid inventory item body" );
		return item ? { ...item, name: names.get( item.refObjId ), icon: icons.get( item.refObjId ) } : null;
	}

	/*
================
decodeShopItems
================
	*/
	function decodeShopItems(
		items: unknown,
		target: ReadonlyMap<number, InventoryItem> = slots,
		capacity = inventorySlotCount ?? 256
	) {
		if ( !Array.isArray( items ) || items.length > 256 ) throw Error( "Invalid shop inventory" );
		const next = new Map( target ),
			seen = new Set<number>(),
			nextRefs = new Map( refs ),
			nextNames = new Map( names );
		for ( const row of items ) {
			if ( !row || typeof row !== "object" ) throw Error( "Invalid shop inventory row" );
			const n = commerceInteger( row.slot, capacity - 1 ),
				id = commerceInteger( row.refObjId, 0xffffffff, 1 ),
				flags = commerceInteger( row.typeFlags, 65535 );
			if (
				seen.has( n ) || typeof row.name !== "string" || row.name.length > 256 || !Array.isArray( row.body ) ||
				row.body.length > 2048
			) throw Error( "Invalid shop inventory row" );
			seen.add( n );
			for ( const b of row.body ) commerceInteger( b, 255 );
			if ( nextRefs.has( id ) && nextRefs.get( id ) !== flags ) throw Error( "Conflicting shop item reference" );
			nextRefs.set( id, flags );
			nextNames.set( id, row.name );
			const bytes = Uint8Array.from( row.body ), decoded = decodeInventoryItem( bytes, 0, nextRefs, objRefs );
			if ( !decoded.item || decoded.item.refObjId !== id || decoded.next !== bytes.length ) {
				throw Error( "Invalid shop item body" );
			}
			next.set( n, { ...decoded.item, slot: n, name: row.name } );
		}
		return { next, nextRefs, nextNames };
	}
	/*
================
applyShopItems
================
	*/
	function applyShopItems( items: unknown ) {
		const { next, nextRefs, nextNames } = decodeShopItems( items );
		slots = next;
		refs.clear();
		for ( const [id, flags] of nextRefs ) refs.set( id, flags );
		names.clear();
		for ( const [id, name] of nextNames ) names.set( id, name );
		published = null;
	}
	/*
================
busy
================
	*/
	function busy() {
		return pending !== null || mall.pending() || timedOut || alchemy.state().pending ||
			[ "rolling", "waiting" ].includes( gacha.state().phase );
	}
	/*
================
transfer
================
	*/
	function transfer( next: Map<number, InventoryItem>, source: number, destination: number, quantity: number ) {
		if ( source === destination ) {
			throw new Error( "Same-slot inventory result" );
		}
		const a = next.get( source ), b = next.get( destination );
		if ( !a ) {
			throw new Error( "Inventory result references empty source" );
		}
		const stack = (a.typeFlags & 0x60) === 0x60;
		// Equipment moves swap whole records, including ammunition. Only bag-to-bag
		// moves may split/merge counts; companion moves obey the same rule.
		if (
			stack && source >= (equipmentSlotCount ?? 13) && destination >= (equipmentSlotCount ?? 13) &&
			(!b || b.refObjId === a.refObjId)
		) {
			if ( quantity < 1 || quantity > a.quantity || (b?.quantity ?? 0) + quantity > 65535 ) {
				throw new Error( "Invalid inventory stack transfer" );
			}
			next.set( destination, { ...a, slot: destination, quantity: (b?.quantity ?? 0) + quantity } );
			if ( quantity === a.quantity ) {
				next.delete( source );
			} else {
				next.set( source, { ...a, quantity: a.quantity - quantity } );
			}
		} else {
			next.set( destination, { ...a, slot: destination } );
			if ( b ) {
				next.set( source, { ...b, slot: source } );
			} else {
				next.delete( source );
			}
		}
	}
	return {
		/*
================
bindCompanion

830EC0 -> 59C2E0 binds the private COS record to its summoner inventory
slot. The separate 3645 receipt owns the native active/dead state byte.
================
 */
		bindCompanion( record: import("@/engine/contracts/gameplay").CosRecord ) {
			if ( record.inventorySlot === undefined ) return;
			const previous = slots.get( record.inventorySlot );
			// Native 59C2E0 ignores an absent slot; it does not create an item.
			if ( !previous ) return;
			if ( (previous.typeFlags & 0x7fe) !== 0xcc ) {
				throw Error( "Companion record has no summoner item" );
			}
			slots.set( record.inventorySlot, {
				...previous,
				summon: {
					...previous.summon,
					state: record.dead ? 4 : 2,
					refObjId: record.refObjId,
					name: record.name,
					rentals: previous.summon?.rentals ?? []
				}
			} );
			published = null;
		},
		/*
================
openMall
================
		*/
		openMall( now: number ) {
			if ( busy() ) throw Error( "Inventory command unavailable" );
			const frame = mall.open( now );
			send( frame );
			return frame;
		},
		/*
================
purchaseMall
================
		*/
		purchaseMall( request: MallPurchase, now: number ) {
			if ( busy() ) throw Error( "Inventory command unavailable" );
			const frame = mall.purchase( request, now );
			send( frame );
			return frame;
		},
		/*
================
takeBindingMoves
================
		*/
		takeBindingMoves() {
			const result = bindingMoves;
			bindingMoves = [];
			return result;
		},
		/*
================
nameItem
================
		*/
		nameItem() {
			const row = slots.get( 8 );
			return row ? { refObjId: row.refObjId, typeFlags: row.typeFlags } : undefined;
		},
		/*
================
useType
================
		*/
		useType( slot: number ) {
			return slots.get( slot )?.typeFlags;
		},
		/*
================
useCooldown
================
		*/
		useCooldown( slot: number ) {
			const item = slots.get( slot );
			return item ? useCooldowns.get( item.refObjId ) ?? 0 : undefined;
		},
		present,
		/*
================
references
================
		*/
		references( rows: readonly import("@/engine/foundation/gameplay/commerce").CommerceItemReference[] ) {
			for ( const row of rows ) {
				if ( refs.has( row.refObjId ) && refs.get( row.refObjId ) !== row.typeFlags ) {
					throw Error( "Conflicting inventory item reference" );
				}
			}
			for ( const row of rows ) {
				refs.set( row.refObjId, row.typeFlags );
				names.set( row.refObjId, row.name );
				if ( row.icon !== undefined ) icons.set( row.refObjId, row.icon );
				if ( row.tooltip ) tooltipRefs.set( row.refObjId, row.tooltip );
			}
			published = null;
			presentations = new WeakMap();
			shopSource = undefined;
		},
		/*
================
bootstrap
================
		*/
		bootstrap( value: unknown ) {
			bindingMoves = [];
			itemCooldowns = [];
			presentations = new WeakMap();
			alchemy.reset();
			gacha.reset();
			mall.reset();
			mallDelivery = null;
			tooltipRefs.clear();
			magicRefs = itemMagicReferences( (value as { magicOptionSnapshot?: unknown; }).magicOptionSnapshot );
			const b = value as {
				inventorySlotCount?: number;
				equipmentSlotCount?: number;
				refItemSnapshot?: {
					icon?: string;
					name?: string;
					nativeFields?: { useCooldownDuration528?: number; };
					descriptionSymbol?: string;
					refObjId: number;
					typeFlags: number;
				}[];
				avatarItems?: { slot: number; refObjId: number; body: number[]; }[];
				equipItems?: {
					slot: number;
					refObjId: number;
					body: number[];
				}[];
			};
			if (
				b.inventorySlotCount !== undefined &&
				(!Number.isInteger( b.inventorySlotCount ) || b.inventorySlotCount < 1 || b.inventorySlotCount > 256 ||
					!Number.isInteger( b.equipmentSlotCount ) || b.equipmentSlotCount! < 0 ||
					b.equipmentSlotCount! > b.inventorySlotCount)
			) throw new Error( "Invalid inventory capacity" );
			inventorySlotCount = b.inventorySlotCount;
			equipmentSlotCount = b.equipmentSlotCount;
			names.clear();
			icons.clear();
			objRefs.clear();
			for (
				const row of (value as { refObjSnapshot?: { refObjId: number; tidWord: number; }[]; }).refObjSnapshot ??
					[]
			) objRefs.set( row.refObjId, row.tidWord );
			refs.clear();
			useCooldowns.clear();
			slots.clear();
			avatars.clear();
			pending = null;
			shop = undefined;
			timedOut = false;
			error = null;
			for ( const r of b.refItemSnapshot ?? [] ) {
				const tooltip = itemTooltipReference( r.nativeFields, r.descriptionSymbol );
				if ( tooltip ) tooltipRefs.set( r.refObjId, tooltip );
				if ( r.icon !== undefined ) {
					if ( typeof r.icon !== "string" || r.icon.length > 256 ) throw Error( "Invalid item icon" );
					icons.set( r.refObjId, r.icon );
				}
				const cooldown = r.nativeFields?.useCooldownDuration528 ?? 0;
				if ( !Number.isInteger( cooldown ) || cooldown < 0 || cooldown > 0x7fffffff ) {
					throw Error( "Invalid item cooldown authority" );
				}
				useCooldowns.set( r.refObjId, cooldown );
				refs.set( r.refObjId, r.typeFlags );
				if ( typeof r.name === "string" ) names.set( r.refObjId, r.name );
			}
			const next = new Map<number, InventoryItem>(), seenSlots = new Set<number>();
			for ( const row of b.equipItems ?? [] ) {
				const n = slot( row.slot );
				if ( seenSlots.has( n ) ) {
					throw new Error( "Duplicate inventory slot" );
				}
				seenSlots.add( n );
				const item = body( Uint8Array.from( row.body ), 0 );
				if ( (item?.refObjId ?? 0) !== row.refObjId ) {
					throw new Error( "Inventory reference mismatch" );
				}
				if ( item ) next.set( n, { ...item, slot: n } );
			}
			const nextAvatars = new Map<number, InventoryItem>();
			for ( const row of b.avatarItems ?? [] ) {
				const n = slot( row.slot );
				if ( n >= 4 || nextAvatars.has( n ) ) throw Error( "Invalid avatar storage slot" );
				const item = body( Uint8Array.from( row.body ), 0 );
				if ( !item || item.refObjId !== row.refObjId || (item.typeFlags & 0x7fe) !== 0x6ac ) {
					throw Error( "Invalid avatar item" );
				}
				nextAvatars.set( n, { ...item, slot: n } );
			}
			avatars = nextAvatars;
			slots = next;
			published = null;
		},
		/*
================
process
================
		*/
		process( command: ItemProcessCommand, now: number ) {
			if ( command.kind === "alchemy-open" ) {
				alchemy.open();
				return null;
			}
			if ( command.kind === "alchemy-close" || command.kind === "alchemy-cancel" ) {
				const frame = alchemy.cancel();
				if ( frame ) send( frame );
				if ( command.kind === "alchemy-close" ) alchemy.close();
				return frame;
			}
			if ( command.kind === "gacha-close" ) {
				gacha.close();
				return null;
			}
			if ( busy() ) throw Error( "Inventory process unavailable" );
			if ( command.kind === "gacha-roll" ) {
				for ( const cue of gacha.start( command.entry, command.slot, slots.get( command.slot ), now ) ) {
					play( cue );
				}
				return null;
			}
			const frame = command.kind === "gacha-open" ?
				gacha.open( command.gid ) :
				alchemy.start( command.mode, command.slots, slots, now, command.quantity );
			send( frame );
			return frame;
		},
		/*
================
cosTrade
================
		*/
		cosTrade(
			record: import("@/engine/contracts/gameplay").CosRecord,
			buy: boolean,
			slot: number,
			quantity: number,
			tab: number,
			now: number
		) {
			if (
				busy() || !shop || shop.error || record.dead || record.hp === 0 || !record.inventory ||
				record.status < 1
			) throw Error( "COS shop unavailable" );
			commerceInteger( slot, 255 );
			commerceInteger( quantity, 65535, 1 );
			commerceInteger( tab, 255 );
			if ( buy && !shop.offers.some( row => row.tab === tab && row.slot === slot ) ) {
				throw Error( "Unknown shop offer" );
			}
			if ( !buy && !record.inventory.some( row => row.slot === slot && row.quantity >= quantity ) ) {
				throw Error( "COS sale source unavailable" );
			}
			const payload = new Uint8Array( buy ? 13 : 12 ), v = new DataView( payload.buffer );
			payload[0] = buy ? 19 : 20;
			v.setUint32( 1, record.gid, true );
			if ( buy ) {
				payload[5] = tab;
				payload[6] = slot;
			} else payload[5] = slot;
			v.setUint16( buy ? 7 : 6, quantity, true );
			v.setUint32( buy ? 9 : 8, shop.npc, true );
			const frame = { opcode: 0x706d, payload };
			send( frame );
			pending = {
				opcode: 0xb06d,
				cosGid: record.gid,
				source: slot,
				npc: shop.npc,
				tab,
				quantity,
				movementType: buy ? 19 : 20,
				deadline: now + 10000
			};
			error = null;
			return frame;
		},
		/*
================
cosShopSnapshot
================
		*/
		cosShopSnapshot( record: import("@/engine/contracts/gameplay").CosRecord, p: Uint8Array ) {
			const r = commerceJson( p );
			if (
				pending?.movementType !== 19 || pending.snapshot || r.cosGid !== record.gid ||
				pending.cosGid !== record.gid || r.npc !== pending.npc || r.tab !== pending.tab ||
				r.slot !== pending.source || r.quantity !== pending.quantity
			) throw Error( "Unexpected COS shop snapshot" );
			const { next } = decodeShopItems(
				r.items,
				new Map( record.inventory!.map( row => [ row.slot, row ] ) ),
				record.status
			);
			pending = {
				...pending,
				snapshot: true,
				receivedSlots: (r.items as { slot: number; }[]).map( row => row.slot )
			};
			return { ...record, inventory: [ ...next.values() ] };
		},
		/*
================
cosPurchase
================
		*/
		cosPurchase( p: Uint8Array ) {
			if ( p.length < 11 || p[0] !== 1 || p[1] !== 19 || p.length !== 11 + p[8]! ) {
				throw Error( "Invalid COS purchase result" );
			}
			const v = new DataView( p.buffer, p.byteOffset, p.byteLength );
			if (
				pending?.movementType !== 19 || !pending.snapshot || pending.cosGid !== v.getUint32( 2, true ) ||
				pending.tab !== p[6] || pending.source !== p[7] ||
				pending.quantity !== v.getUint16( p.length - 2, true )
			) throw Error( "Unmatched COS purchase result" );
			const destinations = [ ...p.subarray( 9, p.length - 2 ) ];
			if (
				!destinations.length || destinations.some( slot => !pending!.receivedSlots?.includes( slot ) ) ||
				pending.receivedSlots?.some( slot => !destinations.includes( slot ) )
			) throw Error( "COS purchase destinations differ from snapshot" );
			pending = null;
			error = null;
		},
		/*
================
cosSold
================
		*/
		cosSold( gid: number, slot: number, quantity: number, npc: number ) {
			if ( !pending ) return; // Retain authoritative sale publications with no local intent.
			if (
				pending?.movementType !== 20 || pending.cosGid !== gid || pending.source !== slot ||
				pending.quantity !== quantity || pending.npc !== npc
			) throw Error( "Unmatched COS sale result" );
			pending = null;
			error = null;
		},
		/*
================
storageMove

Plan the move against the current bag and room before it reaches the wire,
so an impossible request never leaves the client.
================
		*/
		storageMove( room: StorageRoom, move: StorageMove, now: number, caps: ReadonlyMap<number, number> ) {
			if ( busy() ) throw Error( "Inventory command unavailable" );
			if ( room.phase !== "open" ) throw Error( "Storage room is not open" );
			const bagMove = move.type === STORAGE_MOVE_DEPOSIT || move.type === STORAGE_MOVE_WITHDRAW;
			const bagSlot = move.type === STORAGE_MOVE_DEPOSIT ? move.source : move.destination;
			if (
				bagMove && (bagSlot < equipmentSlotCount! || bagSlot >= inventorySlotCount!) ||
				move.type !== STORAGE_MOVE_ROOM && !bagMove && move.gold < 1
			) throw Error( "Invalid storage move" );
			storageMoveResult(
				{ opcode: 0xb06d, payload: Uint8Array.of( 1, move.type ) },
				room,
				[ ...slots.values() ],
				move,
				caps
			);
			const frame = storageMoveRequest( room.npc, move );
			send( frame );
			pending = {
				opcode: 0xb06d,
				movementType: move.type,
				source: move.source,
				destination: move.destination,
				quantity: move.quantity,
				amount: move.gold,
				npc: room.npc,
				storage: true,
				deadline: now + 10000
			};
			error = null;
			return frame;
		},
		/*
================
storageSettle

The room after the pending warehouse move's success echo, or null when the
frame is not that echo (a rejection takes the generic receive path).
================
		*/
		storageSettle( room: StorageRoom, p: Uint8Array, caps: ReadonlyMap<number, number> ): StorageRoom | null {
			if ( !pending?.storage || p[0] !== 1 || p[1] !== pending.movementType ) return null;
			const v = new DataView( p.buffer, p.byteOffset, p.byteLength );
			const echo = pending.movementType === STORAGE_MOVE_ROOM ?
				p.length === 6 && p[2] === pending.source && p[3] === pending.destination &&
				v.getUint16( 4, true ) === pending.quantity :
				pending.movementType === STORAGE_MOVE_DEPOSIT || pending.movementType === STORAGE_MOVE_WITHDRAW ?
				p.length === 4 && p[2] === pending.source && p[3] === pending.destination :
				p.length === 6 && v.getUint32( 2, true ) === pending.amount;
			if ( !echo ) throw Error( "Unmatched storage move result" );
			const move: StorageMove = {
				type: pending.movementType!,
				source: pending.source,
				destination: pending.destination ?? 0,
				quantity: pending.quantity ?? 0,
				gold: pending.amount ?? 0
			};
			const next = storageMoveResult( { opcode: 0xb06d, payload: p }, room, [ ...slots.values() ], move, caps )!;
			slots = new Map( next.bag.map( row => [ row.slot, row ] ) );
			published = null;
			pending = null;
			return next.room;
		},
		/*
================
transferCos
================
		*/
		transferCos(
			record: import("@/engine/contracts/gameplay").CosRecord,
			toCos: boolean,
			source: number,
			destination: number,
			now: number,
			caps: ReadonlyMap<number, number>
		) {
			if ( busy() ) throw Error( "Inventory command unavailable" );
			planCosTransfer(
				record,
				[ ...slots.values() ],
				toCos,
				source,
				destination,
				inventorySlotCount!,
				equipmentSlotCount!,
				caps
			);
			const frame = cosTransferRequest( record.gid, toCos, source, destination );
			send( frame );
			pending = {
				opcode: 0xb06d,
				cosGid: record.gid,
				source,
				destination,
				movementType: toCos ? 0x1b : 0x1a,
				deadline: now + 10000
			};
			error = null;
			return frame;
		},
		/*
================
cosTransferred
================
		*/
		cosTransferred(
			record: import("@/engine/contracts/gameplay").CosRecord,
			p: Uint8Array,
			caps: ReadonlyMap<number, number>
		) {
			if ( timedOut ) throw Error( "Inventory transaction timed out; reconnect to resynchronize" );
			if ( p.length !== 8 || p[0] !== 1 || (p[1] !== 0x1a && p[1] !== 0x1b) ) {
				throw Error( "Invalid COS transfer result" );
			}
			const gid = new DataView( p.buffer, p.byteOffset, p.byteLength ).getUint32( 2, true );
			if (
				!pending || pending.opcode !== 0xb06d || pending.movementType !== p[1] || pending.cosGid !== gid ||
				record.gid !== gid || pending.source !== p[6] || pending.destination !== p[7]
			) throw Error( "Unmatched COS transfer result" );
			const next = planCosTransfer(
				record,
				[ ...slots.values() ],
				p[1] === 0x1b,
				p[6]!,
				p[7]!,
				inventorySlotCount!,
				equipmentSlotCount!,
				caps
			);
			slots = new Map( next.player.map( row => [ row.slot, row ] ) );
			published = null;
			pending = null;
			return next.cos;
		},
		/*
================
cosGround
================
		*/
		cosGround( frame: import("@/engine/contracts/network").WireFrame, now: number ) {
			if ( busy() ) throw Error( "Inventory command unavailable" );
			const p = frame.payload, v = new DataView( p.buffer, p.byteOffset, p.byteLength );
			if ( frame.opcode !== 0x706d || !(p[0] === 0x11 && p.length === 9 || p[0] === 0x12 && p.length === 6) ) {
				throw Error( "Invalid COS ground request" );
			}
			send( frame );
			pending = {
				opcode: 0xb06d,
				cosGid: v.getUint32( 1, true ),
				source: p[0] === 0x12 ? p[5]! : 0,
				movementType: p[0],
				deadline: now + 10000
			};
			error = null;
			return frame;
		},
		/*
================
cosGrounded
================
		*/
		cosGrounded( p: Uint8Array ) {
			const gid = new DataView( p.buffer, p.byteOffset, p.byteLength ).getUint32( 2, true );
			if (
				!pending || pending.cosGid !== gid || pending.movementType !== p[1] ||
				p[1] === 0x12 && pending.source !== p[6]
			) throw Error( "Unmatched COS ground result" );
			pending = null;
			error = null;
		},
		/*
================
cosMove
================
		*/
		cosMove( frame: import("@/engine/contracts/network").WireFrame, now: number ) {
			if ( busy() ) throw Error( "Inventory command unavailable" );
			const p = frame.payload, v = new DataView( p.buffer, p.byteOffset, p.byteLength );
			if ( frame.opcode !== 0x706d || p.length !== 9 || p[0] !== 0x10 ) {
				throw Error( "Invalid COS inventory command" );
			}
			send( frame );
			pending = {
				opcode: 0xb06d,
				cosGid: v.getUint32( 1, true ),
				source: p[5]!,
				destination: p[6]!,
				quantity: v.getUint16( 7, true ),
				movementType: 0x10,
				deadline: now + 10000
			};
			error = null;
			return frame;
		},
		/*
================
cosMoved
================
		*/
		cosMoved( p: Uint8Array ) {
			const v = new DataView( p.buffer, p.byteOffset, p.byteLength );
			if (
				p.length !== 10 || pending?.movementType !== 0x10 || pending.cosGid !== v.getUint32( 2, true ) ||
				pending.source !== p[6] || pending.destination !== p[7] || pending.quantity !== v.getUint16( 8, true )
			) throw Error( "Unmatched COS inventory result" );
			pending = null;
			error = null;
		},
		/*
================
avatarMove
================
		*/
		avatarMove( equip: boolean, source: number, destination: number, now: number ) {
			if ( busy() ) throw Error( "Inventory is busy" );
			slot( source );
			slot( destination );
			const from = equip ? slots : avatars, to = equip ? avatars : slots, item = from.get( source );
			if (
				!item || (item.typeFlags & 0x7fe) !== 0x6ac || (equip && to.has( destination )) || (equip ?
					(source < (equipmentSlotCount ?? 13) || source >= (inventorySlotCount ?? 0) || destination >= 4) :
					(source >= 4 || destination < (equipmentSlotCount ?? 13) ||
						destination >= (inventorySlotCount ?? 0)))
			) throw Error( "Invalid avatar transfer" );
			const movementType = equip ? 0x24 : 0x23,
				frame = { opcode: 0x706d, payload: Uint8Array.of( movementType, source, destination ) };
			send( frame );
			pending = { opcode: 0xb06d, source, destination, movementType, deadline: now + 10000 };
			error = null;
			return frame;
		},
		/*
================
move
================
		*/
		move( source: number, destination: number, quantity: number, now = 0 ) {
			if ( timedOut ) throw Error( "Inventory transaction timed out; reconnect to resynchronize" );
			slot( source );
			slot( destination );
			if (
				inventorySlotCount !== undefined && (source >= inventorySlotCount || destination >= inventorySlotCount)
			) throw new Error( "Inventory slot exceeds capacity" );
			if (
				busy() || !slots.has( source ) || source === destination || !Number.isInteger( quantity ) ||
				quantity < 0 || quantity > 65535
			) {
				throw new Error( "Inventory command unavailable" );
			}
			const p = new Uint8Array( 5 );
			p[1] = source;
			p[2] = destination;
			new DataView( p.buffer ).setUint16( 3, quantity, true );
			const frame = { opcode: 0x706d, payload: p };
			send( frame );
			pending = { opcode: 0xb06d, source, destination, movementType: 0, deadline: now + 10000 };
			error = null;
			return frame;
		},
		/*
================
drop
================
		*/
		drop( n: number, now = 0 ) {
			if ( timedOut ) throw Error( "Inventory transaction timed out; reconnect to resynchronize" );
			const item = slots.get( slot( n ) );
			if ( busy() || !item || n < (equipmentSlotCount ?? 13) ) throw Error( "Item drop unavailable" );
			const frame = { opcode: 0x706d, payload: Uint8Array.of( 7, n ) };
			send( frame );
			pending = { opcode: 0xb06d, source: n, movementType: 7, deadline: now + 10000 };
			error = null;
			return frame;
		},
		/*
================
dropGold
================
		*/
		dropGold( amount: number, now = 0 ) {
			if ( timedOut ) throw Error( "Inventory transaction timed out; reconnect to resynchronize" );
			if ( busy() || !Number.isInteger( amount ) || amount < 1 || amount > 100000000 ) {
				throw Error( "Gold drop unavailable" );
			}
			const payload = new Uint8Array( 5 );
			payload[0] = 10;
			new DataView( payload.buffer ).setUint32( 1, amount, true );
			const frame = { opcode: 0x706d, payload };
			send( frame );
			pending = { opcode: 0xb06d, source: 0, movementType: 10, amount, deadline: now + 10000 };
			error = null;
			return frame;
		},
		/*
================
openShop
================
		*/
		openShop( gid: number, now: number ) {
			if ( busy() ) throw Error( "Inventory command unavailable" );
			commerceInteger( gid, 0xffffffff, 1 );
			const payload = new Uint8Array( 8 ), v = new DataView( payload.buffer );
			v.setUint32( 0, gid, true );
			v.setUint32( 4, 1, true );
			const frame = { opcode: 0x7338, payload };
			send( frame );
			if ( shop?.npc !== gid ) shop = undefined;
			pending = { opcode: 11, source: 0, npc: gid, deadline: now + 10000 };
			return frame;
		},
		/*
================
trade
================
		*/
		trade( buy: boolean, n: number, quantity: number, tab: number, now: number ) {
			if ( busy() || !shop || shop.error ) throw Error( "Shop is unavailable" );
			commerceInteger( n, 255 );
			commerceInteger( quantity, 65535, 1 );
			commerceInteger( tab, 255 );
			if ( buy && !shop.offers.some( o => o.tab === tab && o.slot === n ) ) throw Error( "Unknown shop offer" );
			if ( !buy ) {
				const item = slots.get( n );
				if ( !item || n < (equipmentSlotCount ?? 13) || quantity > item.quantity ) {
					throw Error( "Sale is unavailable" );
				}
			}
			const payload = new Uint8Array( buy ? 9 : 8 ), v = new DataView( payload.buffer );
			payload[0] = buy ? 8 : 9;
			if ( buy ) {
				payload[1] = tab;
				payload[2] = n;
			} else payload[1] = n;
			v.setUint16( buy ? 3 : 2, quantity, true );
			v.setUint32( buy ? 5 : 4, shop.npc, true );
			const frame = { opcode: 0x706d, payload };
			send( frame );
			pending = {
				opcode: 0xb06d,
				source: n,
				movementType: buy ? 8 : 9,
				npc: shop.npc,
				tab,
				quantity,
				deadline: now + 10000
			};
			error = null;
			return frame;
		},
		/*
================
buyback
================
		*/
		buyback( id: number, now: number ) {
			if ( busy() || !shop || shop.error || !shop.buyback?.some( e => e.id === id ) ) {
				throw Error( "Buyback unavailable" );
			}
			commerceInteger( id, 0xffffffff, 1 );
			const entry = shop.buyback.find( e => e.id === id )!;
			const payload = new Uint8Array( 5 ), v = new DataView( payload.buffer );
			v.setUint32( 0, shop.npc, true );
			payload[4] = entry.index;
			const frame = { opcode: 0x77e7, payload };
			send( frame );
			pending = {
				opcode: 13,
				source: 0,
				npc: shop.npc,
				buybackId: id,
				buybackIndex: entry.index,
				quantity: entry.quantity,
				deadline: now + 10000
			};
			error = null;
			return frame;
		},
		/*
================
repair

0x746F at the shop's smith: one inventory slot, or every item. The answer
is 0xB46F; each repaired item's durability and the balance ride their own
packets.
================
		*/
		repair( mode: 1 | 2, slot: number, now: number ) {
			if ( busy() || !shop || shop.error ) throw Error( "Repair is unavailable" );
			const frame = repairRequest( shop.npc, mode, mode === REPAIR_ONE_SLOT ? slot : 0 );
			send( frame );
			pending = { opcode: REPAIR_RESPONSE_OPCODE, source: slot, npc: shop.npc, deadline: now + 10000 };
			error = null;
			return frame;
		},
		/*
================
use
================
		*/
		use( n: number, now = 0, context?: CosItemUseContext ) {
			if ( timedOut ) throw Error( "Inventory transaction timed out; reconnect to resynchronize" );
			const item = slots.get( slot( n ) );
			if ( !item || busy() ) {
				throw new Error( "Item use unavailable" );
			}
			if ( itemCooldown( itemCooldowns, item.typeFlags, now ) ) return null;
			const tail = cosItemUseTail( item.typeFlags, [ ...slots.values() ], context );
			const p = new Uint8Array( 3 + tail.length );
			p.set( tail, 3 );
			p[0] = n;
			new DataView( p.buffer ).setUint16( 1, item.typeFlags, true );
			const frame = { opcode: 0x75bd, payload: p };
			send( frame );
			pending = { opcode: 0xb5bd, source: n, deadline: now + 10000 };
			error = null;
			return frame;
		},
		/*
================
receive
================
		*/
		receive( op: number, p: Uint8Array, now = 0, recovery?: { country: number | undefined; abnormal: number; } ) {
			if ( timedOut ) throw Error( "Inventory transaction timed out; reconnect to resynchronize" );
			if ( op === 15 ) {
				const items = mall.projection( p );
				if ( items !== undefined ) {
					const prepared = decodeShopItems( items );
					const received = (items as { slot: number; }[]).map( row => row.slot );
					if ( received.some( slot => slot < (equipmentSlotCount ?? 13) ) ) {
						throw Error( "Mall delivery references equipment" );
					}
					mallDelivery = { prepared, slots: received };
				}
				return true;
			}
			if ( op === 0xb06d && p[0] === 1 && p[1] === 0x18 ) {
				if ( !mallDelivery ) throw Error( "Mall receipt lacks delivery" );
				mall.acknowledge( p, mallDelivery.slots );
				const { next, nextRefs, nextNames } = mallDelivery.prepared;
				slots = next;
				refs.clear();
				for ( const [id, flags] of nextRefs ) refs.set( id, flags );
				names.clear();
				for ( const [id, name] of nextNames ) names.set( id, name );
				published = null;
				mallDelivery = null;
				return true;
			}
			if ( op === 0xb06d && p[0] === 2 && mall.reject( p ) ) return true;
			if ( op === 0xb338 ) {
				// 75AE50 kind 2: the NPC refused the function request (category 13,
				// e.g. code 4 too far). No catalogue follows a refused shop open, so
				// release it now; gameplay owns the notice.
				if ( p[0] === 2 && pending?.opcode === 11 ) {
					pending = null;
					shop = undefined;
					shopCompletionRevision++;
					error = null;
					return true;
				}
				return gacha.opened( p );
			}
			if ( op === 0xb053 ) {
				for ( const cue of gacha.result( p, slots.get( gacha.state().slot ?? -1 ) ) ) play( cue );
				return true;
			}
			if ( op === 0x3645 ) {
				const before = slots.get( p[0]! ), next = itemStateDelta( p, slots, refs, names );
				const kinds = itemSlotFlashKinds( p[1]!, before?.summon?.state, next.item?.summon?.state );
				if ( kinds.length ) {
					itemFlashes = [
						...itemFlashes.filter( f => now - f.atMs < FLASH_RETENTION_MS ),
						...kinds.map( kind => ({ slot: next.slot, kind, atMs: now }) )
					];
				}
				if ( next.item ) slots.set( next.slot, next.item );
				else slots.delete( next.slot );
				published = null;
				return true;
			}
			if ( op === 0xb16f || op === 0x3359 || op === 0xb549 ) {
				const success = p[0] === 1, arming = op !== 0xb549 && p[1] === 1;
				const length = success ? (op === 0xb549 ? 1 : arming ? 2 : 6) : p[0] === 2 ? 2 : 1;
				if ( p.length !== length ) throw Error( "Invalid compound result" );
				if ( success && arming ) return true;
				for (
					const cue of alchemy.compound(
						success,
						success && op !== 0xb549 ?
							new DataView( p.buffer, p.byteOffset, p.byteLength ).getUint32( 2, true ) :
							undefined,
						p[0] === 2 ? p[1]! : null
					)
				) play( cue );
				return true;
			}
			if ( op === 0xb373 || op === 0xb651 ) {
				if ( p[0] !== 1 ) {
					if ( p.length !== (p[0] === 2 ? 2 : 1) ) throw Error( "Invalid alchemy rejection" );
					for (
						const cue of alchemy.result(
							0,
							op === 0xb651 && p[0] === 2 && p[1] === 0x23 ? 0x80 : 0,
							p[0] === 2 ? p[1]! : null
						)
					) play( cue );
					return true;
				}
				if ( p.length < 3 || p[2]! < 13 ) throw Error( "Invalid alchemy slot" );
				const n = p[2]!, old = slots.get( n );
				if ( !old ) throw Error( "Alchemy result references absent target" );
				let flags = 0x10, next: InventoryItem | null;
				if ( op === 0xb373 && p[1] === 0 ) {
					if ( p.length < 4 ) throw Error( "Truncated alchemy failure" );
					if ( p[3] ) {
						if ( p.length !== 4 ) throw Error( "Invalid destroyed item result" );
						flags = 0x40;
						next = null;
					} else {
						next = body( p, 4 );
						flags = 0x20 | (next?.plus !== old.plus ? 1 : 0) |
							(next?.durability !== old.durability ? 2 : 0);
					}
				} else {
					next = body( p, 3 );
					flags = p[1] ? 0x10 : 0x20;
				}
				if ( next ) slots.set( n, { ...next, slot: n } );
				else slots.delete( n );
				published = null;
				for ( const cue of alchemy.result( n, flags ) ) play( cue );
				return true;
			}
			if ( op === 0x31e8 ) {
				if ( p.length !== 5 ) throw Error( "Invalid durability result" );
				const n = p[0]!;
				if ( n >= 0x3d ) return true;
				const item = slots.get( n );
				if ( !item ) throw Error( "Durability result references absent item" );
				const value = new DataView( p.buffer, p.byteOffset, p.byteLength ).getInt32( 1, true ),
					old = item.durability | 0;
				const cue = value > old ?
					(old === 0 ? "SND_REVIVE" : "SND_REPAIR") :
					n < 13 ?
					(value === 0 ? "SND_EQBREAK" : value === 6 ? "SND_EQDANGER" : null) :
					null;
				slots.set( n, { ...item, durability: value } );
				published = null;
				if ( cue ) play( cue );
				return true;
			}
			if ( op === 11 ) {
				const parsed = shopCatalog( p ),
					next = {
						...parsed,
						offers: parsed.offers.map( row => ({ ...row, items: row.previews?.map( preview ) }) ),
						buyback: parsed.buyback?.map( row => ({
							...row,
							item: row.preview ? preview( row.preview ) : undefined
						}) )
					};
				if ( pending?.opcode !== 11 || pending.npc !== next.npc ) return true;
				shop = next;
				pending = null;
				shopCompletionRevision++;
				error = next.error ?? null;
				return true;
			}
			if ( op === 13 ) {
				const r = commerceJson( p ),
					id = commerceInteger( r.id ),
					npc = commerceInteger( r.npc, 0xffffffff, 1 ),
					entries = buybackEntries( r.entries ).map( row => ({
						...row,
						item: row.preview ? preview( row.preview ) : undefined
					}) );
				if ( r.error !== undefined && (typeof r.error !== "string" || r.error.length > 256) ) {
					throw Error( "Invalid buyback error" );
				}
				if ( id === 0 ) {
					if ( !Array.isArray( r.items ) || r.items.length || r.error ) {
						throw Error( "Invalid buyback publication" );
					}
					if ( shop?.npc === npc ) shop = { ...shop, buyback: entries };
					return true;
				}
				if (
					pending?.opcode !== 13 || pending.snapshot || pending.npc !== npc || pending.buybackId !== id ||
					shop?.npc !== npc
				) throw Error( "Unexpected buyback result" );
				if ( r.error ) {
					if ( !Array.isArray( r.items ) || r.items.length ) {
						throw Error( "Rejected buyback changed inventory" );
					}
				} else {
					if ( !Array.isArray( r.items ) || r.items.length !== 1 ) {
						throw Error( "Buyback lacks complete item" );
					}
					applyShopItems( r.items );
				}
				shop = { ...shop, buyback: entries };
				pending = r.error ?
					null :
					{ ...pending, snapshot: true, destination: (r.items as { slot: number; }[])[0]!.slot };
				error = r.error as string | undefined ?? null;
				return true;
			}
			if ( op === 0xb7e7 ) {
				if ( p[0] === 1 && p.length === 1 ) return true;
				if ( p[0] !== 2 || p.length !== 2 ) throw Error( "Invalid buyback acknowledgement" );
				if ( pending?.buybackId !== undefined ) {
					if ( pending.snapshot ) throw Error( "Buyback rejection follows committed inventory" );
					pending = null;
					error = `Buyback rejected: ${p[1]}`;
				}
				return true;
			}
			if ( op === 0xb06d && p[0] === 1 && p[1] === 0x22 ) {
				if (
					p.length !== 6 || !pending || pending.buybackIndex !== p[3] || !pending.snapshot ||
					new DataView( p.buffer, p.byteOffset, p.byteLength ).getUint16( 4, true ) !== pending.quantity ||
					p[2] !== pending.destination || slots.get( p[2]! )?.quantity !== pending.quantity
				) throw Error( "Unmatched native buyback restore" );
				const item = slots.get( p[2]! )!;
				pending = null;
				playItem( { handle: "SND_EQUIP", typeFlags: item.typeFlags } );
				return true;
			}
			if ( op === 12 ) {
				const r = commerceJson( p );
				if (
					!pending || pending.movementType !== 8 || pending.snapshot || r.npc !== pending.npc ||
					r.tab !== pending.tab || r.slot !== pending.source || r.quantity !== pending.quantity
				) throw Error( "Unexpected shop inventory" );
				applyShopItems( r.items );
				pending = { ...pending, snapshot: true };
				return true;
			}
			if ( op === 0x3752 ) {
				if ( p.length !== 2 ) {
					throw new Error( "Invalid ammunition update" );
				}
				const item = slots.get( 7 ), quantity = new DataView( p.buffer, p.byteOffset, 2 ).getUint16( 0, true );
				if ( item ) {
					published = null;
					if ( quantity ) {
						slots.set( 7, { ...item, quantity } );
					} else {
						slots.delete( 7 );
					}
				}
				return true;
			}
			if ( op === REPAIR_RESPONSE_OPCODE ) {
				if ( p[0] === 1 ? p.length !== 1 : p.length !== 2 || p[0] !== 2 ) {
					throw Error( "Invalid repair result" );
				}
				if ( pending?.opcode === REPAIR_RESPONSE_OPCODE ) pending = null;
				return true;
			}
			if ( op !== 0xb06d && op !== 0xb5bd ) {
				return false;
			}
			const v = new DataView( p.buffer, p.byteOffset, p.byteLength );
			if ( p[0] === 2 ) {
				if ( p.length !== 2 ) {
					throw new Error( "Invalid inventory rejection" );
				}
				if ( pending?.opcode === op ) {
					pending = null;
				}
				error = `Inventory rejected: ${p[1]}`;
				return true;
			}
			if ( p[0] !== 1 ) {
				throw new Error( "Invalid inventory result" );
			}
			if ( op === 0xb06d && (p[1] === 0x23 || p[1] === 0x24) ) {
				if (
					p.length < 7 || p.length !== 7 + p[6]! * 5 || pending?.movementType !== p[1] ||
					pending.source !== p[2] || (p[1] === 0x24 && pending.destination !== p[3])
				) throw Error( "Unmatched avatar transfer result" );
				const nextBag = new Map( slots ),
					nextAvatar = new Map( avatars ),
					cues: import("@/engine/contracts/audio").ItemSoundRequest[] = [];
				const apply = ( type: number, source: number, destination: number, quantity: number ) => {
					if ( type !== 0x23 && type !== 0x24 ) throw Error( "Invalid avatar submove family" );
					const equip = type === 0x24,
						from = equip ? nextBag : nextAvatar,
						to = equip ? nextAvatar : nextBag,
						item = from.get( source );
					if (
						quantity !== 1 || !item || (item.typeFlags & 0x7fe) !== 0x6ac || to.has( destination ) ||
						(equip ?
							(source < (equipmentSlotCount ?? 13) || source >= (inventorySlotCount ?? 0) ||
								destination >= 4) :
							(source >= 4 || destination < (equipmentSlotCount ?? 13) ||
								destination >= (inventorySlotCount ?? 0)))
					) throw Error( "Avatar transfer occupancy mismatch" );
					from.delete( source );
					to.set( destination, { ...item, slot: destination } );
					cues.push( { handle: "SND_EQUIP", typeFlags: item.typeFlags } );
				};
				apply( p[1]!, p[2]!, p[3]!, v.getUint16( 4, true ) );
				for ( let o = 7; o < p.length; o += 5 ) {
					apply( p[o]!, p[o + 1]!, p[o + 2]!, v.getUint16( o + 3, true ) );
				}
				slots = nextBag;
				avatars = nextAvatar;
				published = null;
				pending = null;
				error = null;
				for ( const cue of cues ) playItem( cue );
				return true;
			}
			const sale = saleResult( op, p );
			if ( sale ) {
				if ( sale.cosGid !== undefined ) return false;
				if ( sale.slot < (equipmentSlotCount ?? 13) ) throw Error( "Sale result references equipment slot" );
				const next = soldInventory( [ ...slots.values() ], sale.slot, sale.quantity );
				slots = new Map( next.map( row => [ row.slot, row ] ) );
				published = null;
				if (
					pending?.movementType === 9 && pending.source === sale.slot && pending.quantity === sale.quantity
				) pending = null;
				return true;
			}
			if ( op === 0xb06d && p[1] === 8 ) {
				if ( p.length < 7 || p.length !== 7 + p[4]! ) throw Error( "Invalid purchase result" );
				const quantity = v.getUint16( p.length - 2, true );
				if (
					!pending || pending.movementType !== 8 || pending.tab !== p[2] || pending.source !== p[3] ||
					pending.quantity !== quantity || !pending.snapshot
				) throw Error( "Purchase lacks matching inventory snapshot" );
				pending = null;
				return true;
			}
			const next = new Map( slots );
			const committedMoves: typeof bindingMoves = [];
			const moveCues: { item: InventoryItem; warn: boolean; }[] = [];
			const applyMove = ( source: number, destination: number, quantity: number ) => {
				const a = next.get( source ), b = next.get( destination );
				const stacking = !!a && (a.typeFlags & 0x60) === 0x60 && source >= (equipmentSlotCount ?? 13) &&
					destination >= (equipmentSlotCount ?? 13) && (!b || b.refObjId === a.refObjId);
				transfer( next, source, destination, quantity );
				committedMoves.push( {
					source,
					destination,
					sourceRemains: stacking && next.has( source ),
					destinationMoves: !stacking && !!b
				} );
				const item = next.get( destination )!;
				moveCues.push( {
					item,
					warn: source >= (equipmentSlotCount ?? 13) && destination < (equipmentSlotCount ?? 13) &&
						equipDurabilityWarning( item.typeFlags, item.durability )
				} );
			};
			if ( op === 0xb5bd ) {
				if ( p.length !== 6 ) {
					throw new Error( "Invalid item use result" );
				}
				const n = p[1]!, item = next.get( n ), quantity = v.getUint16( 2, true );
				if ( !item || item.typeFlags !== v.getUint16( 4, true ) ) {
					throw new Error( "Stale item use result" );
				}
				const category = recoveryCategory( item.typeFlags );
				if ( category ) {
					if ( quantity !== item.quantity - 1 ) throw Error( "Stale recovery item use result" );
					// Read the reference before last-stack removal; failed receipts never reach here.
					{
						if ( recovery?.country === undefined ) throw Error( "Missing recovery cooldown country" );
						const durationMs = recoveryCooldownMs(
							category,
							tooltipRefs.get( item.refObjId )?.fields ?? {},
							recovery.country,
							recovery.abnormal
						);
						itemCooldowns = [ ...itemCooldowns.filter( row => row.category !== category ), {
							category,
							startedAtMs: now,
							durationMs
						} ];
					}
				}
				if ( quantity ) {
					next.set( n, { ...item, quantity } );
				} else {
					next.delete( n );
				}
			} else if ( p[1] === 0 ) {
				if ( p.length < 7 || p.length !== 7 + p[6]! * 5 ) {
					throw new Error( "Invalid inventory move result" );
				}
				applyMove( p[2]!, p[3]!, v.getUint16( 4, true ) );
				for ( let o = 7; o < p.length; o += 5 ) {
					if ( p[o] !== 0 ) {
						throw new Error( "Unsupported inventory submove" );
					}
					applyMove( p[o + 1]!, p[o + 2]!, v.getUint16( o + 3, true ) );
				}
			} else if ( p[1] === 6 && p[2] === 254 ) {
				if ( p.length !== 7 ) throw Error( "Invalid gold pickup result" );
				// Amount granted, not a bag slot or the absolute balance.
				// 30B3 type 1 owns the balance; gameplay prints the gain notice;
				// ANI_PICK owns sound, avoiding duplicates.
			} else if ( p[1] === 6 && p[2] !== 254 ) {
				const n = slot( p[2]! );
				if ( n < (equipmentSlotCount ?? 13) || n >= (inventorySlotCount ?? 256) ) {
					throw Error( "Pickup references a non-bag slot" );
				}
				const item = body( p, 3 );
				if ( item ) next.set( n, { ...item, slot: n } );
				else next.delete( n );
			} else if ( p[1] === 14 || p[1] === 15 ) {
				if ( p.length < 4 ) throw Error( "Truncated quest inventory result" );
				const n = slot( p[2]! );
				if ( n < (equipmentSlotCount ?? 13) || n >= (inventorySlotCount ?? 256) ) {
					throw Error( "Quest item references non-bag slot" );
				}
				if ( p[1] === 15 ) {
					if ( p.length !== 4 || !next.has( n ) ) throw Error( "Invalid quest item removal" );
					next.delete( n );
				} else {
					const item = body( p, 4 );
					if ( !item ) throw Error( "Empty quest item grant" );
					const old = next.get( n );
					if ( old && old.refObjId !== item.refObjId ) throw Error( "Quest grant overwrites another item" );
					next.set( n, { ...item, slot: n } );
				}
			} else if ( p[1] === 7 ) {
				if ( p.length !== 3 ) {
					throw new Error( "Invalid item removal" );
				}
				// 47 -> 0 at 757F6A retains the removed equipment snapshot's
				// TID for its 75803D sound request; the bag removal leg is silent.
				const item = next.get( p[2]! );
				if ( item && p[2]! < (equipmentSlotCount ?? 13) ) moveCues.push( { item, warn: false } );
				next.delete( p[2]! );
			} else if ( p[1] === 10 ) {
				if ( p.length !== 6 || v.getUint32( 2, true ) < 1 || v.getUint32( 2, true ) > 100000000 ) {
					throw Error( "Invalid gold drop result" );
				}
			} else {
				return false;
			}
			slots = next;
			bindingMoves.push( ...committedMoves );
			published = null;
			// Each native result must match the pending operation family and identity.
			if ( pending?.opcode === op ) {
				const matches = op === 0xb5bd ?
					pending.source === p[1] :
					pending.movementType === p[1] && (p[1] === 10 ?
						pending.amount === v.getUint32( 2, true ) :
						pending.source === p[2] && (p[1] !== 0 || pending.destination === p[3]));
				if ( matches ) pending = null;
			}
			if ( op === 0xb5bd ) {
				const word = v.getUint16( 4, true ), family = word >>> 7 & 15;
				if (
					(word & 2) === 0 && (word & 0x1c) === 0x0c && (word & 0x60) === 0x60 &&
					(family === 1 || family === 2)
				) play( "SND_POTION" );
			}
			// Publication follows complete packet validation and slot commit.
			// 756CF0 emits per apply row, including bag splits/merges and swaps.
			for ( const cue of moveCues ) {
				if ( cue.warn ) play( "SND_EQDANGER" );
				playItem( { handle: "SND_EQUIP", typeFlags: cue.item.typeFlags } );
			}
			return true;
		},
		/*
================
step
================
		*/
		step( now: number ) {
			mall.step( now );
			const active = itemCooldowns.filter( row => now < row.startedAtMs + row.durationMs ),
				expired = active.length !== itemCooldowns.length;
			if ( expired ) itemCooldowns = active;
			const unlocked = alchemy.step( now );
			const phase = gacha.state().phase, roll = gacha.step( now, slots.get( gacha.state().slot ?? -1 ) );
			if ( roll ) {
				send( roll );
				return true;
			}
			if ( phase !== gacha.state().phase ) return true;
			if ( pending?.opcode === 11 && now >= pending.deadline ) {
				pending = null;
				shop = undefined;
				shopCompletionRevision++;
				error = "Merchant did not open the shop";
				return true;
			}
			// Native replies have no transaction ID. After timeout a late response
			// could acknowledge a new request, so reconnect instead of reopening it.
			if ( timedOut || pending && now >= pending.deadline ) {
				timedOut = true;
				throw Error( "Inventory transaction timed out; reconnect to resynchronize" );
			}
			return unlocked || expired;
		},
		/*
================
state
================
		*/
		state() {
			return {
				itemMall: mall.state(),
				itemCooldowns,
				avatarInventory: [ ...avatars.values() ].map( present ),
				alchemy: alchemy.state(),
				gacha: gacha.state(),
				shop: presentShop(),
				shopCompletionRevision,
				inventorySlotCount,
				equipmentSlotCount,
				inventory: published ?? (published = [ ...slots.values() ].map( present )),
				itemFlashes,
				inventoryPending: pending !== null || mall.pending() || alchemy.state().pending ||
					[ "rolling", "waiting" ].includes( gacha.state().phase ),
				error
			};
		},
		/*
================
clear
================
		*/
		clear() {
			bindingMoves = [];
			itemCooldowns = [];
			shopCompletionRevision = 0;
			presentations = new WeakMap();
			tooltipRefs.clear();
			magicRefs = itemMagicReferences( undefined );
			alchemy.reset();
			gacha.reset();
			mall.reset();
			mallDelivery = null;
			shop = undefined;
			timedOut = false;
			objRefs.clear();
			names.clear();
			icons.clear();
			inventorySlotCount = undefined;
			equipmentSlotCount = undefined;
			itemFlashes = [];
			published = null;
			slots.clear();
			avatars.clear();
			refs.clear();
			useCooldowns.clear();
			pending = null;
			error = null;
		}
	};
}
