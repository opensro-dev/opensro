/*
===========================================================================

tooltip-target.ts - resolve item and action help through its owning container

Mall inventory borrows the ordinary inventory rows. A reference ID alone never
identifies an instance; slot and container ownership determine the tooltip.
The exchange table, the Magic Pang bag/card/prizes and the fortress forge
list bind item slots the same way (CIFSlot_SetBoundItem 56D190), so their
hovers resolve through the same owners (BUG-084).

===========================================================================
*/
import type { GameplayState, InventoryItem } from "@/engine/contracts/gameplay";
import type { ActionSlot } from "./action-layout";
import { quickSlotItemSlot } from "../gameplay/quickslots";

// Resolve the owning container, not an arbitrary item with the same reference.
// Shop instances are supplied by the authoritative offer/buyback projection.
// gachaCard is the one slot whose item the UI (not the worker) tracks: the
// card a player has placed on the Magic Pang card slot but not yet played.
/*
================
tooltipItems
================
*/
export function tooltipItems(
	id: string,
	game: GameplayState,
	cosGid: number,
	gachaCard?: InventoryItem
): readonly InventoryItem[] {
	let item: InventoryItem | undefined;
	if ( id.startsWith( "slot:" ) ) item = game.inventory.find( row => row.slot === Number( id.slice( 5 ) ) );
	else if ( id.startsWith( "item-mall-slot:" ) ) {
		item = game.inventory.find( row => row.slot === Number( id.slice( "item-mall-slot:".length ) ) );
	} else if ( id.startsWith( "avatar:" ) ) {
		item = game.avatarInventory?.find( row => row.typeFlags >>> 11 === Number( id.slice( 7 ) ) );
	} else if ( id.startsWith( "cos-player:" ) ) {
		item = game.inventory.find( row => row.slot === Number( id.slice( 11 ) ) );
	} else if ( id.startsWith( "alchemy-slot:" ) ) {
		item = game.inventory.find( row => row.slot === Number( id.slice( 13 ) ) );
	} else if ( id.startsWith( "storage-slot:" ) ) {
		// CIFStorageRoom binds each room item to its slot (5B10A0, 5B17F0
		// CIFSlot_SetBoundItem) as the bag does, so it shows the same help.
		item = game.storage?.items.find( row => row.slot === Number( id.slice( 13 ) ) );
	} else if ( id.startsWith( "cos-slot:" ) ) {
		item = game.cosRecords?.find( row => row.gid === cosGid )?.inventory?.find( row =>
			row.slot === Number( id.slice( 9 ) )
		);
	} else if ( id.startsWith( "exchange-my:" ) ) {
		// The exchange table's two sides are bound-item slots natively
		// (CIFExchange, 6B20A0/6B2DA0), as the bag's are.
		item = game.exchange?.own.find( row => row.slot === Number( id.slice( 12 ) ) )?.item;
	} else if ( id.startsWith( "exchange-their:" ) ) {
		item = game.exchange?.theirs.find( row => row.slot === Number( id.slice( 15 ) ) )?.item;
	} else if ( id.startsWith( "gacha-bag:" ) ) {
		// The Magic Pang window's own bag grid (CIFGhaCha_BindInventorySlotEntry):
		// ordinary bag rows under another window's slot ids.
		item = game.inventory.find( row => row.slot === Number( id.slice( 10 ) ) );
	} else if ( id === "gacha-card" ) {
		item = gachaCard;
	} else if ( id.startsWith( "gacha-prize:" ) ) {
		// CIFGhaCha_LoadNpcPrizeEntries binds each prize record to its slot.
		item = game.gacha?.prizes?.find( row => row.entry === Number( id.slice( 12 ) ) )?.item;
	} else if ( id.startsWith( "fortress-production-item:" ) ) {
		// CIFFortressMakeItemWndSlot_SetItem binds the forge row's record.
		item = game.fortressForge?.find( row => row.refObjId === Number( id.slice( 25 ) ) )?.item;
	} else if ( id.startsWith( "shop-offer:" ) ) return game.shop?.offers[Number( id.slice( 11 ) )]?.items ?? [];
	else if ( id.startsWith( "shop-buyback:" ) ) item = game.shop?.buyback?.[Number( id.slice( 13 ) )]?.item;
	else if ( id.startsWith( "stall-slot:" ) ) {
		item = game.stall?.offers.find( row => row.slot === Number( id.slice( "stall-slot:".length ) ) )?.item;
	} else if ( id.startsWith( "stall-net-row:" ) ) {
		item = game.stall?.network.rows[Number( id.slice( "stall-net-row:".length ) )]?.item;
	} else if ( id.startsWith( "notice-item:" ) ) {
		// Port-only: the item a chat-log notice names (notice-links.ts).
		const sequence = parseInt( id.slice( "notice-item:".length ), 10 );
		item = game.chat?.lines.find( line => line.sequence === sequence )?.notice?.item;
	} else if ( id.startsWith( "hotbar:" ) ) {
		const binding = game.quickSlots?.find( row => row.slot === Number( id.slice( 7 ) ) );
		if ( binding && (binding.kind === 0x46 || binding.kind === 0x47) ) {
			item = game.inventory.find( row => row.slot === quickSlotItemSlot( binding ) );
		}
	}
	return item ? [ item ] : [];
}

// 58B720 explicitly excludes action 1015; 58BBC0 / 573B70 replace the
// sit/run action help together with its icon as movement mode changes.
/*
================
actionTooltipKey
================
*/
export function actionTooltipKey(
	id: string,
	game: GameplayState,
	actions: readonly ActionSlot[],
	movementMode: number | undefined
): string | undefined {
	const actionId = id.startsWith( "action:" ) ? Number( id.slice( 7 ) ) : id.startsWith( "hotbar:" ) ?
		(() => {
			const binding = game.quickSlots?.find( row => row.slot === Number( id.slice( 7 ) ) );
			return binding?.kind === 0x4a ? binding.payload & 0xffffff : undefined;
		})() :
		undefined;
	if ( actionId === 1015 ) return undefined;
	if ( actionId === 1000 ) return movementMode === 4 ? "UIIT_CTL_STAND" : "UIIT_CTL_SIT";
	if ( actionId === 1001 ) return movementMode === 2 ? "UIIT_CTL_RUN" : "UIIT_CTL_WALK";
	return actions.find( row => row.id === actionId )?.name;
}
