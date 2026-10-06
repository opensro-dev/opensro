/*
===========================================================================

peer-appearance.ts - native remote player spawn and equipment projection

Decodes v1.150 CICUser appearance, including the shared character action
speed and transform-skin equipment. It owns no live character state.

===========================================================================
*/
import { animationRate } from "../animation/animation-rate";
import { equipmentHoldType } from "./name-color";
import { decodeSpawnSkills, type SpawnSkillReference } from "./spawn-skills";
import type { EntityEquipment, EntityState } from "@/engine/contracts/world";
// Native sub_868a80 slot table; wire authority: server/item/wire/playerspawnrow.go.
/*
================
equipmentBand
================
*/
export function equipmentBand( t: number ) {
	return !(t & 2) && (t & 0x1c) === 0xc && (t & 0x60) === 0x20;
}
/*
================
equipmentSlot
================
*/
export function equipmentSlot( t: number ) {
	if ( !equipmentBand( t ) ) return -1;
	const group = (t >>> 7) & 15, sub = t >>> 11;
	if ( [ 1, 2, 3, 9, 10, 11 ].includes( group ) ) return [ -1, 0, 2, 1, 4, 3, 5 ][sub] ?? -1;
	if ( group === 6 ) return 6;
	if ( group === 4 && (sub === 1 || sub === 2) ) return 7;
	if ( group === 7 ) return 8;
	return -1;
}
// TransformSkin_WriteBlock (4DD6B0) after the RefObj: a player skin (TID
// 1/1, a Duplicate) carries a discarded byte and the copied worn items;
// 85C060 dresses only equipment-band items with a slot, at plus 0.
/*
================
transformSkinTail
================
*/
export function transformSkinTail(
	p: Uint8Array,
	offset: number,
	refObjId: number,
	tidWord: number,
	itemRefs: ReadonlyMap<number, number>,
	revision: number
): { skin: NonNullable<EntityState["transformSkin"]>; next: number; } {
	const v = new DataView( p.buffer, p.byteOffset, p.byteLength );
	let o = offset;
	function take( n: number ) {
		if ( o + n > p.length ) throw new Error( "Truncated transform skin" );
		const at = o;
		o += n;
		return at;
	}
	const player = !!(tidWord & 2) && (tidWord & 0x1c) === 4 && (tidWord & 0x60) === 0x20,
		equipment: EntityEquipment[] = [];
	if ( player ) {
		take( 1 );
		const n = v.getUint8( take( 1 ) );
		for ( let i = 0; i < n; i++ ) {
			const id = v.getUint32( take( 4 ), true ), typeFlags = itemRefs.get( id );
			if ( typeFlags === undefined ) throw new Error( "Unknown skin equipment reference" );
			const slot = equipmentSlot( typeFlags );
			if ( slot >= 0 ) equipment.push( { slot, refObjId: id, typeFlags, plus: 0 } );
		}
	}
	return { skin: { refObjId, player, equipment, revision }, next: o };
}
/*
================
decodePeerAppearance
================
*/
export function decodePeerAppearance(
	p: Uint8Array,
	refs: ReadonlyMap<number, number>,
	appear: boolean,
	objRefs: ReadonlyMap<number, { tidWord: number; }> = new Map(),
	skillRefs: ReadonlyMap<number, SpawnSkillReference> = new Map()
): EntityState {
	const v = new DataView( p.buffer, p.byteOffset, p.byteLength );
	let o = 0;
	function take( n: number ) {
		if ( o + n > p.length ) throw new Error( "Truncated peer appearance" );
		const at = o;
		o += n;
		return at;
	}
	const u8 = () => v.getUint8( take( 1 ) ),
		u16 = () => v.getUint16( take( 2 ), true ),
		u32 = () => v.getUint32( take( 4 ), true );
	function f32() {
		const n = v.getFloat32( take( 4 ), true );
		if ( !Number.isFinite( n ) ) throw new Error( "Invalid peer coordinate" );
		return n;
	}
	function str() {
		const n = u16(), at = take( n );
		return Array.from( p.subarray( at, at + n ), c => String.fromCharCode( c ) ).join( "" );
	}
	const refObjId = u32(), bodyShape = u8(), visualFlags = u8();
	function items( avatar: boolean ) {
		u8();
		const count = u8(), out: EntityEquipment[] = [];
		for ( let i = 0; i < count; i++ ) {
			const refObjId = u32(), typeFlags = refs.get( refObjId );
			if ( typeFlags === undefined ) throw new Error( "Unknown peer equipment reference" );
			const plus = equipmentBand( typeFlags ) && (!avatar || (typeFlags & 0x780) === 0x680) ? u8() : 0;
			const slot = avatar ? -1 : equipmentSlot( typeFlags );
			const old = out.findIndex( item => item.slot === slot && slot >= 0 );
			if ( old >= 0 ) out.splice( old, 1 );
			out.push( { slot, refObjId, typeFlags, plus } );
		}
		return out;
	}
	const equipment = items( false ), avatars = items( true );
	let transformSkin: EntityState["transformSkin"];
	// 86B14F..86B265: the msch 1 skin; a player skin (TID 1/1) brings its equipment.
	if ( u8() ) {
		const refObjId = u32(), ref = objRefs.get( refObjId );
		if ( !ref ) throw new Error( "Missing peer skin reference authority" );
		const tail = transformSkinTail( p, o, refObjId, ref.tidWord, refs, 1 );
		transformSkin = tail.skin;
		o = tail.next;
	}

	const gid = u32(), regionId = u16(), x = f32(), y = f32(), z = f32(), heading = u16();
	if ( !gid ) throw new Error( "Invalid peer GID" );
	const moving = u8(), movementMode = u8();
	let spawnDestination: EntityState["spawnDestination"];
	if ( moving ) {
		const regionId = u16(),
			x = v.getInt16( take( 2 ), true ),
			y = v.getInt16( take( 2 ), true ),
			z = v.getInt16( take( 2 ), true );
		spawnDestination = { regionId, x, y, z, angle: heading };
	} else {
		u8();
		u16();
	}
	if (
		spawnDestination && ((regionId | spawnDestination.regionId) & 0x8000) && regionId !== spawnDestination.regionId
	) throw new Error( "Dungeon spawn movement requires teleport" );
	const lifeState = u8(), motionState = u8(), status = u8();
	const walkSpeed = f32(), runSpeed = f32(), scaleDenom = f32();
	if (
		walkSpeed < 0 || runSpeed < 0 || scaleDenom <= 0 || moving && (movementMode === 2 ? walkSpeed : runSpeed) <= 0
	) throw new Error( "Invalid peer speed/scale" );
	const decodedSkills = decodeSpawnSkills( p, o, skillRefs );
	o = decodedSkills.next;
	const name = str(), jobType = u8(), jobGrade = u8(), appearance = u8(), ride = u8(), appearance2 = u8();
	const mountedOn = ride === 1 ? u32() : undefined;
	if ( mountedOn === gid ) throw new Error( "Self mounted peer" );
	const appearance3 = u8(), titleMode = u8(), appearance4 = u8(), guildName = str();
	let guildId: number | undefined,
		guildGrantName: string | undefined,
		guildCrests: readonly [number, number, number] | undefined,
		guildWarTeam: number | undefined;
	// The member block is gated by the last visual slot-8 TID, not the weapon in slot 6.
	const hold = equipment.find( item => item.slot === 8 )?.typeFlags;
	if ( hold === undefined || ![ 0x800, 0x1000, 0x1800 ].includes( hold & 0xf800 ) ) {
		guildId = u32();
		guildGrantName = str();
		guildCrests = [ u32(), u32(), u32() ];
		guildWarTeam = u8();
	}
	let titleText: string | undefined, titleId: number | undefined;
	// 5e2ba0 reads signed-u16 UTF-16 length and returns the SAME stream; 869df0 then reads the title ID.
	if ( titleMode === 4 ) {
		const n = v.getInt16( take( 2 ), true );
		if ( n < 0 ) throw new Error( "Invalid peer title length" );
		const at = take( n * 2 );
		titleText = new TextDecoder( "utf-16le", { fatal: true } ).decode( p.subarray( at, at + n * 2 ) );
		titleId = u32();
	}
	const actionProgress = u8(), arenaTeam = u8();
	const spawnAppearance = appear ? u8() : undefined;
	if ( o !== p.length ) throw new Error( "Invalid peer spawn length" );
	return {
		spawnAppearance,
		spawnSkills: decodedSkills.skills,
		titleText,
		titleId,
		transformSkin,
		spawnDestination,
		mountedOn,
		jobType,
		jobGrade,
		guildName,
		guildId,
		guildGrantName,
		guildCrests,
		guildWarTeam,
		arenaTeam,
		pvpState: appearance,
		holdType: equipmentHoldType( hold ),
		appearanceState: [
			lifeState,
			motionState,
			status,
			appearance,
			appearance2,
			appearance3,
			titleMode,
			appearance4,
			actionProgress
		],
		gid,
		refObjId,
		kind: "player",
		regionId,
		x,
		y,
		z,
		heading,
		name,
		walkSpeed,
		runSpeed,
		animationRate: animationRate( scaleDenom ),
		movementMode,
		bodyShape,
		visualFlags,
		equipment,
		avatars
	};
}
