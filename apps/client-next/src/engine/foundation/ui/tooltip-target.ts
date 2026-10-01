/*
===========================================================================

tooltip-target.ts - resolve item and action help through its owning container

Mall inventory borrows the ordinary inventory rows. A reference ID alone never
identifies an instance; slot and container ownership determine the tooltip.

===========================================================================
*/
import type { GameplayState, InventoryItem } from "@/engine/contracts/gameplay";
import type { ActionSlot } from "./action-layout";
import { quickSlotItemSlot } from "../gameplay/quickslots";

// Resolve the owning container, not an arbitrary item with the same reference.
// Shop instances are supplied by the authoritative offer/buyback projection.
/*
================
tooltipItems
================
*/
export function tooltipItems( id: string, game: GameplayState, cosGid: number ): readonly InventoryItem[] {
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
	} else if ( id.startsWith( "cos-slot:" ) ) {
		item = game.cosRecords?.find( row => row.gid === cosGid )?.inventory?.find( row =>
			row.slot === Number( id.slice( 9 ) )
		);
	} else if ( id.startsWith( "shop-offer:" ) ) return game.shop?.offers[Number( id.slice( 11 ) )]?.items ?? [];
	else if ( id.startsWith( "shop-buyback:" ) ) item = game.shop?.buyback?.[Number( id.slice( 13 ) )]?.item;
	else if ( id.startsWith( "hotbar:" ) ) {
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
