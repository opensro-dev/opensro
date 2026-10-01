/*
===========================================================================

portal.ts - teleport catalogue, the gate destination menu and its refusals

Decodes the published teleportdata/teleportlink rows, builds the menu a
selected gate offers and maps the 0xB495 refusal to its native notice.
Walking into a gate's reach belongs to interaction-approach.ts.

===========================================================================
*/
import { constantNativeNotice } from "./native-notice";
import type { SystemNotice } from "./system-notices";
export type PortalId = number & { readonly __portalId: unique symbol; };
export interface PortalCatalog {
	readonly destinations: readonly { id: PortalId; ref: number; name: string; }[];
	readonly links: readonly { source: PortalId; target: PortalId; fee: number; }[];
}
/*
================
decodePortalCatalog

Validate the published catalogue: unique destinations, resolved links.
================
*/
export function decodePortalCatalog( value: unknown ): PortalCatalog {
	const o = value as { format?: unknown; version?: unknown; teleportRows?: unknown; linkRows?: unknown; };
	if (
		o?.format !== "sro-teleportdata" || o.version !== 1 || !Array.isArray( o.teleportRows ) ||
		!Array.isArray( o.linkRows ) || o.teleportRows.length > 4096 || o.linkRows.length > 16384
	) throw Error( "Invalid portal catalog" );
	const integer = ( v: unknown, min = 0 ) => {
		if ( typeof v !== "number" || !Number.isSafeInteger( v ) || v < min || v > 0xffffffff ) {
			throw Error( "Invalid portal identity/value" );
		}
		return v;
	};
	const ids = new Set<number>();
	const destinations = o.teleportRows.map( r => {
		if ( !r || typeof r.nameSymbol !== "string" || !r.nameSymbol ) throw Error( "Invalid portal caption" );
		const id = integer( r.id, 1 ) as PortalId;
		if ( ids.has( id ) ) throw Error( "Duplicate portal identity" );
		ids.add( id );
		return { id, ref: integer( r.npcRefObjId ), name: r.nameSymbol };
	} );
	const seen = new Set<string>();
	const links = o.linkRows.map( r => {
		if ( !r ) throw Error( "Invalid portal link" );
		const source = integer( r.sourceId, 1 ) as PortalId,
			target = integer( r.destinationId, 1 ) as PortalId,
			fee = integer( r.fee );
		const key = source + ":" + target;
		if ( !ids.has( source ) || !ids.has( target ) || seen.has( key ) ) {
			throw Error( "Unresolved/duplicate portal link" );
		}
		seen.add( key );
		return { source, target, fee };
	} );
	return { destinations, links };
}
/*
================
portalMenu

5D7040: the destination rows for the selected gate. It uses the NPC's
reference identity, never its runtime GID.
================
*/
export function portalMenu( catalog: PortalCatalog, ref: number, copy: ( key: string ) => string, taxRate = 0 ) {
	const source = catalog.destinations.find( r => r.ref === ref );
	if ( !source ) return [];
	return catalog.links.filter( r => r.source === source.id ).map( link => {
		const destination = catalog.destinations.find( r => r.id === link.target )!;
		const format = copy( link.fee ? "UIIT_CTL_TELEPORT_RESULT" : "UIIT_CTL_TELEPORT_FREE_RESULT" );
		let argument = 0;
		const label = format.replace(
			/%s|%I64[du]|%[ld]*[du]/g,
			() =>
				argument++ === 0 ?
					copy( destination.name ) :
					String( link.fee + Math.trunc( taxRate / 100 * link.fee ) )
		);
		return { id: "npc-portal:" + link.target, label };
	} );
}
/*
================
portalNotice

75B930 -> 689420 category 13. Preserve silent native codes and route
authored refusals through the shared guide/banner owner.
================
*/
export function portalNotice( opcode: number, p: Uint8Array ): SystemNotice | null {
	if ( opcode !== 0xb495 ) return null;
	if ( p[0] === 1 ) {
		if ( p.length !== 1 ) throw Error( "Invalid portal success" );
		return null;
	}
	if ( p.length !== 2 || p[0] !== 2 ) throw Error( "Invalid portal refusal" );
	return constantNativeNotice( 13, p[1]! );
}
