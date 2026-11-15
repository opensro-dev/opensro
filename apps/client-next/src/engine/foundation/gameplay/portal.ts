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
	readonly destinations: readonly { id: PortalId; ref: number; name: string; code: string; }[];
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
		const code = typeof r.codeName === "string" ? r.codeName : "";
		return { id, ref: integer( r.npcRefObjId ), name: r.nameSymbol, code };
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
PORTAL_GROUPS

Port-only, not native: destinations a gate folds under one row whose
click opens their own page (SRO_JANGAN_TOMB_TELEPORT, the Jangan gate's
Qin-Shi Tomb floors); End conversation closes it as any menu. Only a
gate outside the group folds: the tomb's own gates keep the native flat
menu. No retail link leads from outside the tomb to a GATE_JINSI_ row,
so a native catalogue never folds.

corners tells apart the rows that share one zone name: floor 4's four
corner passages all read SN_ZONE_26004, so each shows as
"Ch'in Tomb 4F(Northwest)". No native table holds a bare compass word,
so the four are port-only English text.
================
*/
const PORTAL_GROUPS = [ {
	key: "qinshi-tomb",
	codePrefix: "GATE_JINSI_",
	caption: "SN_ZONE_23001",
	corners: {
		126: "Northeast",
		127: "Southeast",
		128: "Southwest",
		129: "Northwest"
	}
} ] as const;

/*
================
PortalMenuView

taxRate: the gate's fortress tax, added to each fee. group: the open
PORTAL_GROUPS key, "" for the gate's top page.
================
*/
export interface PortalMenuView {
	readonly taxRate?: number;
	readonly group?: string;
}

/*
================
portalMenu

5D7040: the destination rows for the selected gate. It uses the NPC's
reference identity, never its runtime GID. A port-only group
(PORTAL_GROUPS) shows as one "npc-portal-group:" row on the top page
and as its own rows once opened.
================
*/
export function portalMenu(
	catalog: PortalCatalog,
	ref: number,
	copy: ( key: string ) => string,
	view: PortalMenuView = {}
) {
	const source = catalog.destinations.find( r => r.ref === ref );
	if ( !source ) return [];
	const taxRate = view.taxRate ?? 0, open = view.group ?? "";
	const groupOf = ( code: string ) =>
		PORTAL_GROUPS.find( g => code.startsWith( g.codePrefix ) && !source.code.startsWith( g.codePrefix ) );
	const rows: { id: string; label: string; }[] = [], shown = new Set<string>();
	for ( const link of catalog.links ) {
		if ( link.source !== source.id ) continue;
		const destination = catalog.destinations.find( r => r.id === link.target )!;
		const group = groupOf( destination.code );
		if ( (group?.key ?? "") !== open ) {
			if ( group && !open && !shown.has( group.key ) ) {
				shown.add( group.key );
				rows.push( { id: "npc-portal-group:" + group.key, label: copy( group.caption ) } );
			}
			continue;
		}
		const corners: Readonly<Record<number, string>> | undefined = group?.corners;
		const corner = corners?.[destination.id];
		const format = copy( link.fee ? "UIIT_CTL_TELEPORT_RESULT" : "UIIT_CTL_TELEPORT_FREE_RESULT" );
		let argument = 0;
		const label = format.replace(
			/%s|%I64[du]|%[ld]*[du]/g,
			() =>
				argument++ === 0 ?
					copy( destination.name ) + (corner ? "(" + corner + ")" : "") :
					String( link.fee + Math.trunc( taxRate / 100 * link.fee ) )
		);
		rows.push( { id: "npc-portal:" + link.target, label } );
	}
	return rows;
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
