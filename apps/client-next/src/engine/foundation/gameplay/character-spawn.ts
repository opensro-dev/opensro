import { isNameColorGuard } from "./name-color";
import { decodeSpawnSkills, type SpawnSkillReference } from "./spawn-skills";
import type { EntityState } from "@/engine/contracts/world";

/*
===========================================================================

character-spawn.ts - one NPC, monster, COS or structure spawn row

CICharactor 85FB20 (the shared block), packed movement 776170, then each
class's tail: COS 854FA0 / 8554E0, monster 861B00, fortress structure
4FA0B0 (whose fields precede the shared block).

===========================================================================
*/

const STRUCTURE_HEADQUARTERS_BAND = 5;

/*
================
decodeCharacterSpawn
================
*/
export function decodeCharacterSpawn(
	p: Uint8Array,
	kind: string,
	tidWord: number,
	appear: boolean,
	skillRefs: ReadonlyMap<number, SpawnSkillReference> = new Map()
): EntityState {
	const v = new DataView( p.buffer, p.byteOffset, p.byteLength );
	let o = 0;
	function take( n: number ) {
		if ( o + n > p.length ) throw new Error( "Truncated character spawn" );
		const at = o;
		o += n;
		return at;
	}
	const u8 = () => v.getUint8( take( 1 ) ),
		u16 = () => v.getUint16( take( 2 ), true ),
		i16 = () => v.getInt16( take( 2 ), true ),
		u32 = () => v.getUint32( take( 4 ), true ),
		f32 = () => {
			const n = v.getFloat32( take( 4 ), true );
			if ( !Number.isFinite( n ) ) throw new Error( "Invalid character coordinate" );
			return n;
		},
		str = () => {
			const n = u16(), at = take( n );
			return new TextDecoder( "utf-8", { fatal: true } ).decode( p.subarray( at, at + n ) );
		};
	const refObjId = u32();
	// 4FA0B0: a fortress structure's hit points, event zone and state precede the shared block.
	const structureHp = kind === "structure" ? u32() : undefined,
		eventStructId = kind === "structure" ? u32() : undefined,
		structureState = kind === "structure" ? u16() : undefined;
	const gid = u32(), regionId = u16(), x = f32(), y = f32(), z = f32(), heading = u16();
	if ( !gid ) throw new Error( "Invalid entity GID" );
	const moving = u8(), movementMode = u8();
	let spawnDestination: EntityState["spawnDestination"];
	if ( moving ) spawnDestination = { regionId: u16(), x: i16(), y: i16(), z: i16(), angle: heading };
	else {
		u8();
		u16();
	}
	if (
		spawnDestination && ((regionId | spawnDestination.regionId) & 0x8000) && regionId !== spawnDestination.regionId
	) throw new Error( "Dungeon spawn movement requires teleport" );
	const life = u8(), motion = u8(), status = u8(), walkSpeed = f32(), runSpeed = f32(), scale = f32();
	if ( walkSpeed < 0 || runSpeed < 0 || scale < 0 || moving && (movementMode === 2 ? walkSpeed : runSpeed) <= 0 ) {
		throw new Error( "Invalid character movement scalar" );
	}
	const decodedSkills = decodeSpawnSkills( p, o, skillRefs );
	o = decodedSkills.next;
	let guildId: number | undefined, guildName: string | undefined, nameColor: number | undefined;
	let rarity: number | undefined, rarityAuxIcon: number | undefined, tradeVariant: number | undefined;
	const mask = u8();
	let name = mask & 1 ? str() : "",
		ownerName: string | undefined,
		pvpState: number | undefined,
		holdType: number | undefined,
		ownerGid: number | undefined,
		cosAppearanceRefObjId: number | undefined;
	const attackFlags = mask & 2 ? u32() : kind === "monster" ? 0x10 : 0;
	if ( (kind === "npc" || kind === "structure") && name ) nameColor = 0xff9ed0ff; // 859DEB: custom NPC name (CICATStruct is a CICNPC).
	if ( isNameColorGuard( { kind, tidWord } ) ) {
		guildId = u32();
		guildName = str();
	}
	if ( kind === "cos" ) {
		const band = tidWord >>> 11;
		if ( [ 2, 3, 4, 5, 6 ].includes( band ) ) {
			if ( band === 3 || band === 4 ) name = str();
			ownerName = str();
			if ( band !== 6 ) {
				holdType = u8();
				if ( band !== 4 ) pvpState = u8();
			}
		}
		if ( band === 5 ) cosAppearanceRefObjId = u32();
		if ( band !== 1 ) ownerGid = u32();
	} else if ( kind === "structure" && (tidWord >>> 11) === STRUCTURE_HEADQUARTERS_BAND ) {
		guildId = u32();
		if ( guildId ) guildName = str(); // 4FA0B0: a headquarters' guild.
	} else if ( kind === "monster" ) {
		const grade = u8();
		rarity = grade & 15;
		rarityAuxIcon = grade >>> 4;
		if ( (tidWord & 0x7fe) === 0xc6 && [ 0x1000, 0x1800 ].includes( tidWord & 0xf800 ) ) tradeVariant = u8();
	}
	const spawnAppearance = appear ? u8() : undefined;
	if ( o !== p.length ) throw new Error( "Invalid character spawn length" );
	return {
		...(kind === "structure" ? { structureHp, eventStructId, structureState } : {}),
		spawnAppearance,
		guildId,
		guildName,
		nameColor,
		tidWord,
		attackFlags,
		rarity,
		rarityAuxIcon,
		tradeVariant,
		spawnSkills: decodedSkills.skills,
		cosAppearanceRefObjId,
		gid,
		refObjId,
		kind,
		regionId,
		x,
		y,
		z,
		heading,
		name,
		walkSpeed,
		runSpeed,
		movementMode,
		spawnDestination,
		ownerName,
		ownerGid,
		pvpState,
		holdType,
		appearanceState: [ life, motion, status ]
	};
}
