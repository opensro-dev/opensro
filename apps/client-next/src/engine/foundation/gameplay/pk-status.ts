/*
===========================================================================

pk-status.ts - the local player's PK counters and their mini-info tooltip

v1.150 keeps three counters on the local player: the daily PK count
(CICPlayer +0x1888, u8), the total PK count (+0x188A, u16) and the PK
penalty (+0x188C, u32). The character data seeds them at world entry
(863880 reads them right after the visual-flags byte); three packets
replace one each afterwards: 0x33C4 the daily count, 0x3647 the total and
0x30F2 the penalty (CPSMission_OnPkDailyCount0x33C4 / ..OnPkLevelUpdate0x3647
/ ..OnPkPenaltySeconds0x30F2).

CIFPlayerInfo_RefreshPKTooltip (6B5150) shows the mini-info's GDR_PMI_PK
(child 0x32) only while one of the three is non-zero, with two lines:
UIIT_TOOLTIP_PK_DAY (daily, the fixed daily limit 15) and
UIIT_TOOLTIP_PK_PENALTY_TIME (penalty, total).

===========================================================================
*/
import type { WireFrame } from "@/engine/contracts/network";

// The daily limit 6B5150 prints as the second argument of the first line.
export const PK_DAILY_LIMIT = 15;

export const OP_PK_PENALTY = 0x30f2;
export const OP_PK_DAILY = 0x33c4;
export const OP_PK_TOTAL = 0x3647;

/*
================
PkStatus
================
*/
export interface PkStatus {
	readonly daily: number;
	readonly total: number;
	readonly penalty: number;
}

/*
================
pkStatusBootstrap

The world bootstrap's character record carries the persisted PK record;
a character that never took part in PK has none, which is all zero.
================
*/
export function pkStatusBootstrap( value: unknown ): PkStatus {
	const record =
		(value as { character?: { pk?: { dailyCount?: unknown; totalCount?: unknown; penalty?: unknown; }; }; })
			.character?.pk;
	const daily = record?.dailyCount ?? 0, total = record?.totalCount ?? 0, penalty = record?.penalty ?? 0;
	if ( !isCount( daily, 0xff ) || !isCount( total, 0xffff ) || !isCount( penalty, 0xffffffff ) ) {
		throw Error( "Invalid PK record" );
	}
	return { daily, total, penalty };
}

/*
================
pkStatusPacket

The next status for one of the three counter packets, or null when the
frame is not one of them.
================
*/
export function pkStatusPacket( status: PkStatus, frame: WireFrame ): PkStatus | null {
	const p = frame.payload, v = new DataView( p.buffer, p.byteOffset, p.byteLength );
	switch ( frame.opcode ) {
		case OP_PK_DAILY:
			if ( p.length !== 1 ) throw Error( "Invalid PK daily count" );
			return { ...status, daily: p[0]! };
		case OP_PK_TOTAL:
			if ( p.length !== 2 ) throw Error( "Invalid PK total count" );
			return { ...status, total: v.getUint16( 0, true ) };
		case OP_PK_PENALTY:
			if ( p.length !== 4 ) throw Error( "Invalid PK penalty" );
			return { ...status, penalty: v.getUint32( 0, true ) };
		default:
			return null;
	}
}

/*
================
pkStatusTooltip

The GDR_PMI_PK tooltip, or null while the control is hidden (all three
counters zero). text resolves a UIIT symbol to its format string.
================
*/
export function pkStatusTooltip( status: PkStatus, text: ( symbol: string ) => string ): string | null {
	if ( status.daily === 0 && status.total === 0 && status.penalty === 0 ) return null;
	const daily = format( text( "UIIT_TOOLTIP_PK_DAY" ), [ status.daily, PK_DAILY_LIMIT ] );
	const penalty = format( text( "UIIT_TOOLTIP_PK_PENALTY_TIME" ), [ status.penalty, status.total ] );
	return daily + "\n" + penalty;
}

/*
================
format

swprintf's %d in order; the text tables use no other conversion here.
================
*/
function format( template: string, values: readonly number[] ): string {
	let index = 0;
	return template.replace( /%d/g, token => index < values.length ? String( values[index++] ) : token );
}

/*
================
isCount
================
*/
function isCount( value: unknown, max: number ): value is number {
	return typeof value === "number" && Number.isInteger( value ) && value >= 0 && value <= max;
}
