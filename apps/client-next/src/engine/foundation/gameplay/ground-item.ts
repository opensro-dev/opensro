import type { EntityState } from "@/engine/contracts/world";
import { worldDelta } from "@/engine/foundation/world/world-space";
/*
================
isGoldGroupType

Group 0x280 (TID 3.3.5) is one wire width: the native readers take a u32
amount for every row of it (CIItem_ParseSpawnPacket 86E63E, CSOItem 78C9E4,
the party loot notice 7511C0).
================
*/
export function isGoldGroupType( typeFlags: number ): boolean {
	return (typeFlags & 0x60) === 0x60 && (typeFlags & 0x780) === 0x280;
}
/*
================
isGoldHeapType

A gold heap (TID 3.3.5.0, ITEM_ETC_GOLD_*): the group without the later shop
tokens (TID4 1, ITEM_ETC_SD_TOKEN_*), which v1.188 excludes the same way
(CGItem_IsGold 483159..483163). Native data holds only heaps; server
wire.IsGoldHeap is the same rule.
================
*/
export function isGoldHeapType( typeFlags: number ): boolean {
	return isGoldGroupType( typeFlags ) && (typeFlags & 0xf800) !== 0x800;
}
// CIItem_ParseSpawnPacket 0x86e4e0; server item/wire/grounditemrow.go.
export function decodeGroundItem( p: Uint8Array, typeFlags: number, appear: boolean, name: string ): EntityState {
	const v = new DataView( p.buffer, p.byteOffset, p.byteLength );
	let o = 0;
	function take( n: number ) {
		if ( o + n > p.length ) throw new Error( "Truncated ground item" );
		const at = o;
		o += n;
		return at;
	}
	const u8 = () => v.getUint8( take( 1 ) ),
		u16 = () => v.getUint16( take( 2 ), true ),
		u32 = () => v.getUint32( take( 4 ), true );
	function f32() {
		const n = v.getFloat32( take( 4 ), true );
		if ( !Number.isFinite( n ) ) throw new Error( "Invalid ground item position" );
		return n;
	}
	if ( (typeFlags & 2) || (typeFlags & 0x1c) !== 0xc ) throw new Error( "Invalid ground item type" );
	const refObjId = u32(), band = typeFlags & 0x60, group = typeFlags & 0x780;
	let goldAmount = 0, suffix = "";
	if ( band === 0x20 ) u8();
	else if ( band === 0x60 && [ 0x400, 0x480 ].includes( group ) ) {
		const n = u16(), at = take( n );
		if ( n ) suffix = new TextDecoder( "windows-1252" ).decode( p.subarray( at, at + n ) ).split( "\0" )[0]!;
	} else if ( isGoldGroupType( typeFlags ) ) goldAmount = u32();
	const gid = u32(),
		regionId = u16(),
		x = f32(),
		y = f32(),
		z = f32(),
		heading = u16(),
		hasOwner = u8(),
		ownerJid = hasOwner ? u32() : undefined,
		tint = u8(),
		appearFlag = appear ? u8() : undefined;
	const claimant = appearFlag === 5 || appearFlag === 6 ? u32() : undefined,
		claimantGid = appearFlag === 5 ? claimant : undefined;
	if ( suffix ) name += (group === 0x400 ? "(*" : "(") + suffix + ")";
	if ( !gid || o !== p.length ) throw new Error( "Invalid ground item length/identity" );
	return {
		gid,
		refObjId,
		kind: "ground-item",
		regionId,
		x,
		y,
		z,
		heading,
		name,
		groundItem: {
			typeFlags,
			goldAmount,
			ownerJid,
			tint,
			appear: appearFlag,
			...(claimantGid ? { claimantGid } : {})
		}
	};
}

// 86E0D0: the pickup shortcut scans unclaimed items within a strict 50-unit 3D range.
export function nearestGroundItem(
	entities: readonly EntityState[],
	origin: import("@/engine/contracts/gameplay").Pose
): number {
	let best = 50, gid = 0;
	for ( const entity of entities ) {
		if ( !entity.groundItem || entity.groundItem.claimantGid || !entity.gid ) continue;
		const distance = groundItemDistance( entity, origin );
		if ( distance < best ) {
			best = distance;
			gid = entity.gid;
		}
	}
	return gid;
}
export function groundItemDistance(
	entity: EntityState,
	origin: Pick<EntityState, "regionId" | "x" | "y" | "z">
): number {
	const [rawDx, rawDz] = worldDelta( origin, entity );
	const dx = Math.fround( rawDx );
	const dz = Math.fround( rawDz ),
		dy = Math.fround( entity.y - origin.y );
	return Math.fround( Math.sqrt( Math.fround( dx * dx + dy * dy + dz * dz ) ) );
}
