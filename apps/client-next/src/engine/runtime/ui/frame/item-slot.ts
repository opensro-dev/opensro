/*
===========================================================================

item-slot.ts - an item slot's icon, overlays, count and control

The painters every item grid shares (inventory, storage, exchange, pet
bags, shops): the icon, the equipment-requirement overlay, the slot
effects (dead companion wash, animated sheets, 0x3645 flashes), the count
and the slot's control. What they read beyond the frame - the gameplay
view, the slot effect clock, the equipment warning blink, whether a carry
may start - comes in one explicit ItemSlotContext the coordinator builds
each step, never as a closure capture. An internal of frame.ts, which
re-exports it.

===========================================================================
*/
import type { GameplayState, InventoryItem } from "@/engine/contracts/gameplay";
import type { UiRect } from "@/engine/contracts/ui";
import type { UiFrame } from "./frame-contract";
import { EXPANSION_LEVEL_CAP } from "@/engine/foundation/gameplay/level-cap";
import { itemCountQuads } from "@/engine/foundation/ui/item-count";
import { equipmentWarningUv, itemEquipmentOverlay } from "@/engine/foundation/ui/item-equipment-overlay";
import { itemSlotOverlays, itemSlotWash, slotSeed } from "@/engine/foundation/ui/item-slot-effects";
import { image, rect } from "./draw";

const ICON_ROOT = "/assets/images/Media_extracted/icon/";
const EQUIPMENT_WARNING = "icon_item_warning";

// The control ids an item can be carried from. A stall offer lifts for its
// owner while the stall is closed for modification: CIFInventory_OnSlotTransfer
// (59B1D0) takes a stall slot (window 0x21) released on the bag back out.
export const ITEM_SLOT_PREFIXES = [
	"slot:",
	"avatar:",
	"storage-slot:",
	"cos-slot:",
	"gacha-bag:",
	"gacha-card",
	"stall-slot:"
] as const;

/*
================
ItemSlotContext

What the item painters read beyond the frame. gameplay is the latest
view in any phase, the same view the painters read before the move (no
phase gate): the requirement overlay, the flashes, the level cap and the
unlimited items all come from it.
================
*/
export interface ItemSlotContext {
	readonly gameplay: GameplayState | null | undefined;
	// The character's gender as the session lists it.
	readonly gender: number | undefined;
	readonly simulationTimeMs: number;
	readonly warningPhase: number;
	iconPath( icon: string | undefined ): string | null;
	// No repair cursor and no pet item-target cursor: a slot may start a carry.
	carryFree(): boolean;
	touchInspect(): boolean;
	// An animated slot effect was drawn this step (the slot effect clock).
	markEffect(): void;
	// A blinking equipment warning was drawn this step.
	warningShown(): void;
}

/*
================
equipmentOverlay
================
*/
export function equipmentOverlay(
	frame: UiFrame,
	slots: ItemSlotContext,
	{ item, r }: { item: InventoryItem | undefined; r: UiRect; }
) {
	const gameplay = slots.gameplay;
	if ( !item || !gameplay ) return;
	const gender = slots.gender;
	const overlay = itemEquipmentOverlay( item, gameplay.progression ?? { masteries: [] }, {
		country: gameplay.guide?.country,
		sex: gender === 0 ? 1 : gender === 1 ? 0 : undefined
	}, gameplay.inventory );
	if ( overlay ) {
		const warning = overlay === EQUIPMENT_WARNING;
		if ( warning ) slots.warningShown();
		image( frame, {
			r,
			path: ICON_ROOT + overlay + ".png",
			uv: warning ? equipmentWarningUv( slots.warningPhase ) : [ 0, 0, 1, 1 ]
		} );
	}
}

/*
================
itemEffects

Item-slot painters share the same overlays and animation clock.
================
*/
export function itemEffects(
	frame: UiFrame,
	slots: ItemSlotContext,
	{ id, owned, r }: { id: string; owned: InventoryItem; r: UiRect; }
) {
	// CIFSlotWithHelp's item effects (item-slot-effects.ts): the dead
	// companion wash, then the animated sheets. 0x3645 flashes target
	// the inventory and equipment slots.
	const game = slots.gameplay;
	const wash = itemSlotWash( owned );
	if ( wash ) rect( frame, { r, color: wash } );
	const flashes = id.startsWith( "slot:" ) ?
		(game?.itemFlashes ?? []).filter( f => f.slot === owned.slot ) :
		[];
	const overlays = itemSlotOverlays(
		owned,
		r,
		slotSeed( id + ":" + owned.refObjId ),
		slots.simulationTimeMs,
		flashes,
		game?.levelCap === EXPANSION_LEVEL_CAP
	);
	for ( const overlay of overlays ) {
		image( frame, { r: overlay.rect, path: overlay.path, uv: overlay.uv } );
		slots.markEffect();
	}
}

/*
================
nativeItem
================
*/
export function nativeItem(
	frame: UiFrame,
	slots: ItemSlotContext,
	{ id, item, r, disabled = false, selected = false }: {
		id: string;
		item: { readonly icon?: string; readonly name?: string; readonly quantity?: number; } | undefined;
		r: UiRect;
		disabled?: boolean;
		selected?: boolean;
	}
) {
	const paths = frame.paths;
	const path = slots.iconPath( item?.icon ), drawn = paths.length;
	if ( path ) {
		image( frame, { r, path } );
		if ( item && "typeFlags" in item ) {
			const owned = item as InventoryItem;
			equipmentOverlay( frame, slots, { item: owned, r } );
			itemEffects( frame, slots, { id, owned, r } );
		}
	}
	for ( const content of paths.slice( drawn ) ) frame.contentImages.add( content );
	frame.controls.push( {
		id,
		label: item?.name ?? "",
		rect: r,
		kind: "button",
		disabled,
		selected,
		rightActivate: !!item && (id.startsWith( "slot:" ) || id.startsWith( "storage-slot:" )),
		draggable: !!item && slots.carryFree(),
		carry: slots.carryFree() && !!item && ITEM_SLOT_PREFIXES.some( prefix => id.startsWith( prefix ) ),
		inspect: !!item && slots.touchInspect()
	} );
	itemCount( frame, slots, { item, r } );
}

/*
================
itemCount
================
*/
export function itemCount(
	frame: UiFrame,
	slots: ItemSlotContext,
	{ item, r }: {
		item: { readonly quantity?: number; readonly typeFlags?: number; readonly refObjId?: number; } | undefined;
		r: UiRect;
	}
) {
	const unlimited = item?.refObjId !== undefined && !!slots.gameplay?.unlimitedItems?.includes( item.refObjId );
	for ( const q of itemCountQuads( item, r, frame.full, unlimited ) ) {
		// The infinity sign is solid quads; digit sprites are resources.
		if ( !q.texture ) {
			frame.quads.push( q );
			continue;
		}
		frame.paths.push( q.texture );
		if ( frame.resources.has( q.texture ) ) frame.quads.push( q );
	}
}
