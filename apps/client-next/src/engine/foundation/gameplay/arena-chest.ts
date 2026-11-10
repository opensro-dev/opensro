/*
===========================================================================
arena-chest.ts - port-only server-owned survival checkpoint projection.
===========================================================================
*/
import type { InventoryItem } from "@/engine/contracts/gameplay";
import type { CommercePreview } from "./commerce";
import { commerceInteger } from "./commerce";

export const ARENA_CHEST_OPCODE = 0x3fca;
export const ARENA_DECISION_OPCODE = 0x7fca;

/*
================
ArenaChest
================
*/
export interface ArenaChest {
	readonly id: string;
	readonly stage: number;
	readonly kills: number;
	readonly phase: "closed" | "checkpoint" | "countdown" | "claimable" | "recoverable";
	readonly remainingMs: number;
	readonly decisionEndsAt: number;
	readonly gold: string;
	readonly items: readonly InventoryItem[];
	readonly error?: string;
	readonly skillCooldowns?: readonly { skill: number; remainingMs: number; durationMs: number; }[];
}

/*
================
createArenaChest - reject a malformed projection as a whole, never grant locally.
================
*/
export function createArenaChest( preview: ( row: CommercePreview ) => InventoryItem ) {
	let state: ArenaChest | undefined;
	let requested = false;
	return {
		/*
		================
		receive
		================
		*/
		receive( payload: Uint8Array, now: number ) {
			if ( payload.length > 524288 ) throw Error( "Arena chest exceeds projection limit" );
			const p = JSON.parse( new TextDecoder( "utf-8", { fatal: true } ).decode( payload ) );
			if (
				!p || p.version !== 1 || typeof p.id !== "string" || p.id.length > 128 ||
				![ "closed", "checkpoint", "countdown", "claimable", "recoverable" ].includes( p.phase ) ||
				typeof p.gold !== "string" || !/^\d{1,16}$/.test( p.gold ) ||
				!Array.isArray( p.items ) || p.items.length > 200 ||
				p.error !== undefined && (typeof p.error !== "string" || p.error.length > 256)
			) {
				throw Error( "Invalid arena chest" );
			}
			const items: InventoryItem[] = [], seen = new Set<number>();
			for ( const row of p.items ) {
				if (
					!row || typeof row.name !== "string" || row.name.length > 256 ||
					!Array.isArray( row.body ) || row.body.length > 2048
				) throw Error( "Invalid arena reward" );
				const slot = commerceInteger( row.slot, 199 );
				commerceInteger( row.refObjId, 0xffffffff, 1 );
				commerceInteger( row.typeFlags, 65535 );
				for ( const b of row.body ) commerceInteger( b, 255 );
				if ( seen.has( slot ) ) throw Error( "Duplicate arena reward slot" );
				seen.add( slot );
				const item = preview( row );
				if ( item.refObjId !== row.refObjId ) throw Error( "Arena reward reference mismatch" );
				items.push( { ...item, slot } );
			}
			const skillCooldowns = p.skillCooldowns == null ? undefined : p.skillCooldowns;
			if ( skillCooldowns !== undefined ) {
				if ( !Array.isArray( skillCooldowns ) || skillCooldowns.length > 4096 ) {
					throw Error( "Invalid arena cooldowns" );
				}
				const ids = new Set<number>();
				for ( const row of skillCooldowns ) {
					if ( !row || ids.has( row.skill ) ) throw Error( "Invalid arena cooldown" );
					commerceInteger( row.skill, 0xffffffff, 1 );
					commerceInteger( row.remainingMs, 0xffffffff, 1 );
					commerceInteger( row.durationMs, 0xffffffff, 1 );
					ids.add( row.skill );
				}
			}
			state = {
				id: p.id,
				stage: commerceInteger( p.stage, 5 ),
				kills: commerceInteger( p.kills, 1000000 ),
				phase: p.phase,
				remainingMs: commerceInteger( p.remainingMs, 1500000 ),
				decisionEndsAt: now + commerceInteger( p.decisionMs, 300000 ),
				gold: p.gold,
				items,
				error: p.error,
				skillCooldowns
			};
		},
		/*
		================
		state
		================
		*/
		state: () => state,
		/*
		================
		request - one inquiry per admitted character lifetime.
		================
		*/
		request() {
			if ( requested ) return false;
			requested = true;
			return true;
		},
		/*
		================
		dismiss - a saved town entitlement can close without claiming.
		================
		*/
		dismiss() {
			if ( state?.phase === "claimable" || state?.phase === "recoverable" ) state = { ...state, phase: "closed" };
		},
		/*
		================
		clear
		================
		*/
		clear() {
			state = undefined;
			requested = false;
		}
	};
}
