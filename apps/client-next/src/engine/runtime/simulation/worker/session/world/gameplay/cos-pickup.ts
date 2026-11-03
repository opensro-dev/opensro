/*
===========================================================================

cos-pickup.ts - native cash-pet pickup selection and acknowledgement lifetime

77D7D0 admits drops, 77DBA0 selects the nearest candidate, 77DFA0 sends tag 8,
and 77DD10 releases pending commands. The server still owns range, inventory,
reservation and grant validation. Simulation time owns every retry deadline.

Port-only, not native (#841): the Experimental pickup-pet item filter may
narrow which drops this loop proposes, above the server-owned category
byte in commandMode. It never widens a proposal, never touches the byte,
and manual pickup commands do not pass through here.

===========================================================================
*/
import type { CosRecord } from "@/engine/contracts/gameplay";
import { isGoldHeapType, pickupItemKind } from "@/engine/foundation/gameplay/ground-item";
import type { WireFrame } from "@/engine/contracts/network";
import type { EntityState, WorldEvent } from "@/engine/contracts/world";
import { groundItemDistance } from "@/engine/foundation/gameplay/ground-item";
import { sameCoordinateSpace } from "@/engine/foundation/world/world-space";

const PICKUP_ENABLED = 0x80;
const PICKUP_PUBLIC = 0x40;
const PICKUP_GOLD = 1;
const PICKUP_EQUIPMENT = 2;
const PICKUP_OTHER = 4;
const PICKUP_RANGE = 500;
const DROP_RETRY_MS = 5000;
const PET_RETRY_MS = 3000;
const DISABLE_RETRY_MS = 1000;

/*
================
PickupFrame
================
*/
interface PickupFrame {
	readonly now: number;
	readonly local?: EntityState;
	readonly records: Iterable<CosRecord>;
	readonly sharedOwners: ReadonlySet<number>;
	readonly read: ( gid: number ) => EntityState | undefined;
	readonly smartPetPickup?: boolean;
	// Port-only (#841): the Experimental filter's kind mask
	// (ground-item's PICKUP_KIND bits). Undefined is the native loop:
	// every natively admitted drop may be proposed.
	readonly pickupKinds?: number;
}

/*
================
PickupResult
================
*/
interface PickupResult {
	readonly gid: number;
	readonly itemGid?: number;
	readonly selector: number;
	readonly subtype: number;
	readonly result?: number;
}

/*
================
pickupCategory

77D7EA excludes quest-special drops before categorization. Gold and equipment
use the same RefObj type bits as the shared ground-item decoder.
================
*/
function pickupCategory( type: number ): number {
	if ( (type & 0x60) === 0x60 && (type & 0x780) === 0x400 ) return 0;
	if ( isGoldHeapType( type ) ) return PICKUP_GOLD;
	return (type & 0x60) === 0x20 ? PICKUP_EQUIPMENT : PICKUP_OTHER;
}

/*
================
cosPickupRequest
================
*/
export function cosPickupRequest( gid: number, target: number ): WireFrame {
	if ( ![ gid, target ].every( value => Number.isInteger( value ) && value > 0 && value <= 0xffffffff ) ) {
		throw Error( "Invalid COS pickup identity" );
	}
	const payload = new Uint8Array( 9 ), view = new DataView( payload.buffer );
	view.setUint32( 0, gid, true );
	payload[4] = 8;
	view.setUint32( 5, target, true );
	return { opcode: 0x769e, payload };
}

/*
================
createCosPickup
================
*/
export function createCosPickup() {
	const drops = new Set<number>();
	// Targets a refusal retired (77DD10 retire class): out of the
	// proposal set until the item actually leaves the world. A spawn
	// republication of the same entity must not resurrect them.
	const retired = new Set<number>();
	const pending = new Map<number, number>();
	const receipts = new Set<number>();
	const dropRetry = new Map<number, number>();
	const petRetry = new Map<number, number>();
	const bagFull = new Set<number>();
	return {
		/*
================
track
================
		*/
		track( event: Extract<WorldEvent, { kind: "spawn" | "despawn"; }> ) {
			if ( event.kind === "spawn" ) {
				if ( event.entity.groundItem ) drops.add( event.entity.gid );
				return;
			}
			drops.delete( event.gid );
			retired.delete( event.gid );
			dropRetry.delete( event.gid );
			pending.delete( event.gid );
			receipts.delete( event.gid );
			petRetry.delete( event.gid );
			bagFull.delete( event.gid );
			for ( const [gid, target] of pending ) {
				if ( target === event.gid ) {
					pending.delete( gid );
					receipts.delete( gid );
				}
			}
		},
		/*
================
step

One request per pet and one claimant per drop. Sorting preserves the native
ordered-map tie break independently of the browser's spawn arrival order.
================
		*/
		step( frame: PickupFrame ): WireFrame[] {
			if ( frame.smartPetPickup === false && bagFull.size > 0 ) bagFull.clear();
			const local = frame.local;
			if ( !local || local.appearanceState?.[0] === 2 ) return [];
			const frames: WireFrame[] = [], claimed = new Set( pending.values() );
			for ( const record of frame.records ) {
				const mode = record.commandMode ?? 0, pet = frame.read( record.gid );
				if (
					record.band !== 4 || record.dead || !record.hp || !(mode & PICKUP_ENABLED) ||
					!pet || pet.ownerGid !== local.gid || pet.refObjId !== record.refObjId || pet.moving ||
					pending.has( record.gid ) || (petRetry.get( record.gid ) ?? 0) > frame.now
				) continue;
				let target = 0, best = PICKUP_RANGE;
				for ( const gid of [ ...drops ].sort( ( a, b ) => a - b ) ) {
					const item = frame.read( gid ), drop = item?.groundItem;
					const category = drop ? pickupCategory( drop.typeFlags ) : 0;
					if (
						!item || !drop || drop.claimantGid || claimed.has( gid ) || retired.has( gid ) ||
						(dropRetry.get( gid ) ?? 0) > frame.now || !(mode & category)
					) continue;
					// Port-only (#841): the item filter narrows the native
					// admission above; it can only ever refuse a proposal.
					if (
						frame.pickupKinds !== undefined &&
						(frame.pickupKinds & pickupItemKind( drop.typeFlags )) === 0
					) continue;
					if ( frame.smartPetPickup !== false && bagFull.has( record.gid ) && category !== PICKUP_GOLD ) {
						continue;
					}
					const owned = drop.ownerJid === local.gid ||
						!!drop.ownerJid && frame.sharedOwners.has( drop.ownerJid );
					if ( drop.ownerJid && !owned || !owned && !(mode & PICKUP_PUBLIC) ) continue;
					if ( !sameCoordinateSpace( item.regionId, pet.regionId ) ) continue;
					const distance = groundItemDistance( item, pet );
					if ( distance < best ) {
						best = distance;
						target = gid;
					}
				}
				if ( !target ) continue;
				pending.set( record.gid, target );
				receipts.delete( record.gid );
				claimed.add( target );
				frames.push( cosPickupRequest( record.gid, target ) );
			}
			return frames;
		},
		/*
================
receipt

The committed B06D grant precedes B69E completion. It updates the pet bag
without releasing a concurrently pending manual inventory transaction.
Accept one receipt per submitted automatic command, never a replay.
================
		*/
		receipt( gid: number ): boolean {
			if ( !pending.has( gid ) || receipts.has( gid ) ) return false;
			receipts.add( gid );
			return true;
		},
		/*
================
result

The return value asks gameplay to submit the native disable-setting request
for full/disabled inventory feedback; settings change only on server receipt.
Port-only, not native: when smartPetPickup is enabled, 0xB4/0xB5 keeps bit
0x80 enabled and pauses non-gold item pickup for that pet until spaceChanged
fires so gold pickup continues without spamming 0x769E.
================
		*/
		result( result: PickupResult, now: number, smartPetPickup = false ): boolean {
			if ( result.selector !== 8 || !result.itemGid ) return false;
			const target = pending.get( result.gid );
			if ( target !== undefined && target !== result.itemGid ) return false;
			pending.delete( result.gid );
			receipts.delete( result.gid );
			const code = result.subtype === 1 ? 1 : result.result;
			if ( [ 1, 2, 3, 0x12, 0x39, 0xb1 ].includes( code ?? 0 ) ) {
				drops.delete( result.itemGid );
				retired.add( result.itemGid );
			} else if ( code === 0x13 || code === 0x99 ) dropRetry.set( result.itemGid, now + DROP_RETRY_MS );
			else if ( code === 0x14 || code === 0x9c ) petRetry.set( result.gid, now + PET_RETRY_MS );
			else if ( code === 0xb4 || code === 0xb5 ) {
				if ( smartPetPickup ) {
					bagFull.add( result.gid );
					dropRetry.set( result.itemGid, now + DROP_RETRY_MS );
					return false;
				}
				bagFull.delete( result.gid );
				petRetry.set( result.gid, now + DISABLE_RETRY_MS );
				return true;
			}
			return false;
		},
		/*
================
spaceChanged

Clears the smartPetPickup full-bag pause when pet or player bag space changes.
================
		*/
		spaceChanged( gid?: number ) {
			if ( gid !== undefined ) bagFull.delete( gid );
			else bagFull.clear();
			dropRetry.clear();
		},
		/*
================
clear
================
		*/
		isRetired( gid: number ) {
			return retired.has( gid );
		},
		clear() {
			drops.clear();
			retired.clear();
			pending.clear();
			receipts.clear();
			dropRetry.clear();
			petRetry.clear();
			bagFull.clear();
		}
	};
}
