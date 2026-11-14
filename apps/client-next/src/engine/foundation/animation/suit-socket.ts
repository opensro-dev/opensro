/*
===========================================================================

suit-socket.ts - warm models for the local wearer's suit-socket candidates

A character keeps its old outfit until every part of the new appearance is
resident (actor-appearance.ts), so a job suit or the free-PvP cape that
landed changed the outfit a load later. The parts each bag candidate would
wear in the suit socket (JOB_SUIT_SLOT) are warmed ahead, and only once the
wearer's own parts are ready: a speculative outfit never takes the
character budget the worn one needs.

===========================================================================
*/
import { JOB_SUIT_SLOT } from "@/engine/foundation/gameplay/party-matching";

// Inventory slots 0..LAST_EQUIPMENT_SLOT are worn; the bag starts after it.
export const LAST_EQUIPMENT_SLOT = 12;

// The job suits' and the free-PvP cape's record family, TID 3.1.7.*.
const SUIT_TID1 = 3, SUIT_TID2 = 1, SUIT_TID3 = 7;

type SocketRow = { readonly slot: number; readonly refObjId: number; readonly typeFlags: number; };

/*
================
isSuitSocketItem
================
*/
export function isSuitSocketItem( row: { readonly typeFlags: number; } ) {
	return (row.typeFlags >> 2 & 7) === SUIT_TID1 && (row.typeFlags >> 5 & 3) === SUIT_TID2 &&
		(row.typeFlags >> 7 & 15) === SUIT_TID3;
}

/*
================
suitBagKey

The bag's suit-socket candidates by reference, so the local appearance (and
its warm parts) rebuild when one is bought, dropped or sold; a move inside
the bag changes nothing the parts depend on.
================
*/
export function suitBagKey( equipment: readonly SocketRow[] ) {
	let key = "";
	for ( const row of equipment ) {
		if ( row.slot > LAST_EQUIPMENT_SLOT && isSuitSocketItem( row ) ) key += row.refObjId + ",";
	}
	return key;
}

/*
================
suitSocketParts

The model paths the appearance would need with each bag candidate worn in
the suit socket, and the bare socket's while a candidate is worn there.
partsFor assembles one equipment list into its parts' model paths.
================
*/
export function suitSocketParts<Row extends SocketRow>(
	equipment: readonly Row[],
	partsFor: ( equipment: readonly Row[] ) => readonly string[]
): readonly string[] {
	const others = equipment.filter( row => row.slot !== JOB_SUIT_SLOT );
	const models = new Set<string>();
	for ( const row of equipment ) {
		if ( row.slot > LAST_EQUIPMENT_SLOT && isSuitSocketItem( row ) ) {
			// The candidate moves: it leaves its bag slot for the socket.
			const worn = [ ...others.filter( other => other !== row ), { ...row, slot: JOB_SUIT_SLOT } ];
			for ( const model of partsFor( worn ) ) models.add( model );
		}
	}
	if ( equipment.some( row => row.slot === JOB_SUIT_SLOT && isSuitSocketItem( row ) ) ) {
		for ( const model of partsFor( others ) ) models.add( model );
	}
	return [ ...models ];
}

/*
================
readyThenWarm

Asks for every worn part first (each ask is also its load demand) and warms
the speculative parts only when all worn parts are ready. Near the
character budget the worn outfit is admitted and a warm part is the one
refused, never the other way round. Answers whether the worn parts are
ready.
================
*/
export function readyThenWarm(
	resources: { ready( path: string ): boolean; },
	worn: readonly string[],
	warm: readonly string[]
): boolean {
	let ready = true;
	for ( const model of worn ) {
		if ( !resources.ready( model ) ) ready = false;
	}
	if ( ready ) {
		for ( const model of warm ) resources.ready( model );
	}
	return ready;
}
