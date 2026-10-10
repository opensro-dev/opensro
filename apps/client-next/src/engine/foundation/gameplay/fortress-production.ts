/*
===========================================================================

fortress-production.ts - the fortress smith's and trainer's production

CIFFortressMakeItemWnd serves both staff members (+0x7DF: 1 smith, 2
trainer). Its item list is the forge group's vectors (sub_64b440 +0x00
smith, sub_64b450 +0x10 trainer); a row's price and time come from the
client's own siegefortressitemforge row (sub_64d3e0: +0xC gold, +0x10
guild points, +0x14 minutes), which the bootstrap carries here. The
0xB1E1 answers (754A40 cases 0x0D..0x14) fill one order per staff member.

===========================================================================
*/
import type { FortressServiceReply } from "./fortress-services";

// The smith's actions; the trainer's are these plus four (754A40 pairs
// 0x0D/0x11, 0x0E/0x12, 0x0F/0x13, 0x10/0x14).
export const FORTRESS_PRODUCTION_QUERY = 0x0d;
export const FORTRESS_PRODUCTION_START = 0x0e;
export const FORTRESS_PRODUCTION_CANCEL = 0x0f;
export const FORTRESS_PRODUCTION_COLLECT = 0x10;
const TRAINER_OFFSET = 4;
// 632660 refuses more than twenty items in one order.
export const FORTRESS_PRODUCTION_MAX_COUNT = 20;
// The fortress role a staff member's discount needs exactly
// (GuildData_FindSmithMember 825DB0: 8; GuildData_FindTrainerMember 825D40:
// 0x10) and the x87 factor it applies.
const ROLE_SMITH = 0x08;
const ROLE_TRAINER = 0x10;
// float32( 0.85 ), the x87 constant 65A280 loads.
const DISCOUNT = 0.8500000238418579;

export type FortressStaff = "smith" | "trainer";

/*
================
FortressForgeItem

One producible item and its per-item price and minutes. The worker adds
the reference's name, icon and stack limit once the references arrive.
================
*/
export interface FortressForgeItem {
	readonly refObjId: number;
	readonly gold: number;
	readonly gp: number;
	readonly minutes: number;
	readonly staff: FortressStaff;
	readonly name?: string;
	readonly icon?: string;
	readonly maxStack?: number;
}

/*
================
FortressProductionOrder

The staff member's order: the item, how many remain, whether it is done,
and when it ends on the local clock (the answer's remaining seconds
anchored at receipt).
================
*/
export interface FortressProductionOrder {
	readonly refObjId: number;
	readonly count: number;
	readonly done: boolean;
	readonly endsAtMs: number;
}

/*
================
fortressForgeCatalog

The bootstrap's siegeItemForgeGroups in file order, each item tagged with
its staff member. Rows outside both vectors are not producible.
================
*/
export function fortressForgeCatalog( value: unknown ): FortressForgeItem[] {
	const groups = (value as {
		siegeItemForgeGroups?: {
			smithItemRefs?: number[];
			trainerItemRefs?: number[];
			items?: { refObjId: number; gold: number; gp: number; minutes: number; }[];
		}[];
	}).siegeItemForgeGroups ?? [];
	const out: FortressForgeItem[] = [];
	for ( const group of groups ) {
		const smith = new Set( group.smithItemRefs ?? [] ), trainer = new Set( group.trainerItemRefs ?? [] );
		for ( const item of group.items ?? [] ) {
			if (
				![ item.refObjId, item.gold, item.gp, item.minutes ].every( n =>
					Number.isInteger( n ) && n >= 0 && n <= 0xffffffff
				)
			) throw Error( "Invalid fortress forge row" );
			const staff = smith.has( item.refObjId ) ? "smith" : trainer.has( item.refObjId ) ? "trainer" : null;
			if ( staff ) out.push( { ...item, staff } );
		}
	}
	return out;
}

/*
================
fortressProductionAction

The wire action for a staff member's step (the smith's number, plus four
for the trainer).
================
*/
export function fortressProductionAction( staff: FortressStaff, smithAction: number ): number {
	return staff === "trainer" ? smithAction + TRAINER_OFFSET : smithAction;
}

/*
================
fortressProductionStaff

The staff member an answer belongs to, or null for other actions.
================
*/
export function fortressProductionStaff( action: number ): FortressStaff | null {
	if ( action >= FORTRESS_PRODUCTION_QUERY && action <= FORTRESS_PRODUCTION_COLLECT ) return "smith";
	if (
		action >= FORTRESS_PRODUCTION_QUERY + TRAINER_OFFSET && action <= FORTRESS_PRODUCTION_COLLECT + TRAINER_OFFSET
	) {
		return "trainer";
	}
	return null;
}

/*
================
fortressProductionFactor

0.85 when a guild member holds exactly the staff member's fortress role,
else 1 (65A280 tests the guild through GuildData_FindSmithMember 825DB0
or GuildData_FindTrainerMember 825D40).
================
*/
export function fortressProductionFactor(
	members: readonly { readonly role?: number; }[],
	staff: FortressStaff
): number {
	const role = staff === "smith" ? ROLE_SMITH : ROLE_TRAINER;
	return members.some( member => member.role === role ) ? DISCOUNT : 1;
}

/*
================
fortressProductionPrice

The gold, guild points and seconds of count items, truncated as 632660
does (unsigned products, then the x87 factor).
================
*/
export function fortressProductionPrice( item: FortressForgeItem, count: number, factor: number ) {
	return {
		gold: Math.trunc( ((item.gold * count) >>> 0) * factor ),
		gp: Math.trunc( ((item.gp * count) >>> 0) * factor ),
		seconds: Math.trunc( factor * ((item.minutes * count * 60) >>> 0) )
	};
}

/*
================
fortressProductionOrder

The order after one of this staff member's answers; undefined when the
answer is not a success for that staff member (a refusal leaves it).
================
*/
export function fortressProductionOrder(
	order: FortressProductionOrder | null,
	reply: FortressServiceReply,
	staff: FortressStaff,
	nowMs: number
): FortressProductionOrder | null | undefined {
	if ( reply.result !== 1 || fortressProductionStaff( reply.action ) !== staff ) return undefined;
	const step = staff === "trainer" ? reply.action - TRAINER_OFFSET : reply.action;
	const ends = ( seconds: string | undefined ) => nowMs + Number( BigInt( seconds ?? "0" ) ) * 1000;
	switch ( step ) {
		case FORTRESS_PRODUCTION_QUERY:
			return reply.producing ?
				{
					refObjId: reply.reference ?? 0,
					count: reply.quantity ?? 0,
					done: !!reply.ready,
					endsAtMs: ends( reply.productionTime )
				} :
				null;
		case FORTRESS_PRODUCTION_START:
			return {
				refObjId: reply.reference ?? 0,
				count: reply.quantity ?? 0,
				done: false,
				endsAtMs: ends( reply.productionTime )
			};
		case FORTRESS_PRODUCTION_CANCEL:
			return null;
		case FORTRESS_PRODUCTION_COLLECT: {
			// 65AA10 subtracts the collected count; nothing left closes the order.
			if ( !order ) return null;
			const count = order.count - (reply.quantity ?? 0);
			return count > 0 ? { ...order, count } : null;
		}
	}
	return undefined;
}

/*
================
fortressProductionRemaining

Whole seconds left, never below zero, and zero once the order is done.
================
*/
export function fortressProductionRemaining( order: FortressProductionOrder, nowMs: number ): number {
	if ( order.done ) return 0;
	return Math.max( 0, Math.ceil( (order.endsAtMs - nowMs) / 1000 ) );
}

/*
================
fortressProductionMayOperate

CIFFortressMakeItemWnd_ApplyStaffPermissions (659D50): the make, complete
and cancel buttons answer the guild master, or the member who holds
exactly the staff member's role (827E40 smith 8, 827E70 trainer 0x10).
================
*/
export function fortressProductionMayOperate(
	member: { readonly grade?: number; readonly role?: number; } | undefined,
	staff: FortressStaff
): boolean {
	if ( !member ) return false;
	return member.grade === 0 || member.role === (staff === "smith" ? ROLE_SMITH : ROLE_TRAINER);
}

/*
================
fortressProductionCount

The count edit of MsgBoxMakeItem (52C870 mode 0xA): two digits at most,
numeric, capped at twenty (CIFEdit_SetNumericLimit64 0x14).
================
*/
export function fortressProductionCount( draft: string ): string {
	const digits = draft.replace( /[^0-9]/g, "" ).slice( 0, 2 ).replace( /^0+(?=\d)/, "" );
	if ( !digits ) return "";
	return String( Math.min( Number( digits ), FORTRESS_PRODUCTION_MAX_COUNT ) );
}

/*
================
fortressProductionTimeText

CIFFortressMakeItemWnd_OnTimer (65A5E0): the remaining time in days,
hours and minutes; under an hour it rounds the minutes up, and under a
minute it counts seconds.
================
*/
export function fortressProductionTimeText( seconds: number, copy: ( key: string ) => string ): string {
	const minutes = Math.trunc( seconds / 60 ), hours = Math.trunc( minutes / 60 ), days = Math.trunc( hours / 24 );
	const remain = copy( "UIIT_STT_REMAIN_TIME" ), hour = copy( "UIIT_STT_HOUR" ), minute = copy( "UIIT_STT_MINUTE" );
	if ( days ) {
		return `${remain} : ${days}${copy( "PARAM_DAY" )} ${hours - days * 24}${hour} ${
			minutes - hours * 60
		}${minute} `;
	}
	if ( hours ) return `${remain} : ${hours}${hour} ${minutes - hours * 60}${minute} `;
	if ( !minutes ) return `${remain} : ${seconds} ${copy( "PARAM_SECOND" )}`;
	return `${remain} : ${minutes + 1} ${minute}`;
}
