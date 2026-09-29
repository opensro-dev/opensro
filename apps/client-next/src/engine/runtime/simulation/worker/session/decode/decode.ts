/*
===========================================================================

decode.ts - strict decoding of the title-session HTTP replies

Server lists and character rosters arrive as JSON from the Agent. Every
field is validated here, once, before the session owner publishes it; a
malformed reply is an error, never a partially trusted record.

A roster row mirrors the native character-list record (SCharacterInfo_ReadFromPacket):
the worn items and avatar items as (RefItemID, plus) pairs. The client
dresses the character from those items through the same per-item catalog
as the world (SCharacterInfo_BuildDisplayActor -> CCObjCharacter_SetEquipSlotVisual).

===========================================================================
*/
import type { CharacterItem, CharacterRecord, ServerRecord } from "@/engine/contracts/session";

// Roster contract 2: loadouts carry items, not presentation set keys.
const CHARACTER_ROSTER_CONTRACT_VERSION = 2;
const MAX_STRING_LENGTH = 4096;
const MAX_ARRAY_LENGTH = 256;
const MAX_SERVER_ROWS = 4096;
// Nine worn slots (SCharacterInfo_BuildDisplayActor) and the avatar sockets.
const MAX_WORN_ITEMS = 9;
const MAX_AVATAR_ITEMS = 8;
const MAX_PLUS = 255;
const DELETION_BLOCKERS = [ "guild-master", "guild-member", "academy-guardian", "academy-student" ];

/*
================
createSessionDecoder
================
*/
export function createSessionDecoder() {
	/*
	================
	object
	================
	*/
	function object( value: unknown ): Record<string, unknown> {
		if ( !value || typeof value !== "object" || Array.isArray( value ) ) {
			throw new Error( "Expected response object" );
		}
		return value as Record<string, unknown>;
	}

	/*
	================
	string
	================
	*/
	function string( value: unknown ): string {
		if ( typeof value !== "string" || !value || value.length > MAX_STRING_LENGTH ) {
			throw new Error( "Invalid response string" );
		}
		return value;
	}

	/*
	================
	number
	================
	*/
	function number( value: unknown ): number {
		if ( typeof value !== "number" || !Number.isFinite( value ) || value < 0 ) {
			throw new Error( "Invalid response number" );
		}
		return value;
	}

	/*
	================
	integer
	================
	*/
	function integer( value: unknown ): number {
		const result = number( value );
		if ( !Number.isSafeInteger( result ) ) throw new Error( "Invalid response integer" );
		return result;
	}

	/*
	================
	boolean
	================
	*/
	function boolean( value: unknown ): boolean {
		if ( typeof value !== "boolean" ) throw new Error( "Invalid response boolean" );
		return value;
	}

	/*
	================
	array
	================
	*/
	function array( value: unknown, limit = MAX_ARRAY_LENGTH ): unknown[] {
		if ( !Array.isArray( value ) || value.length > limit ) throw new Error( "Invalid response array" );
		return value;
	}

	/*
	================
	items

	(RefItemID, plus) pairs; a RefItemID appears once per list.
	================
	*/
	function items( value: unknown, limit: number ): readonly CharacterItem[] {
		const seen = new Set<number>();
		return Object.freeze(
			array( value, limit ).map( row => {
				const v = object( row ), refObjId = integer( v.refObjId ), plus = integer( v.plus );
				if ( refObjId === 0 || plus > MAX_PLUS ) throw new Error( "Invalid character item" );
				if ( seen.has( refObjId ) ) throw new Error( "Duplicate character item" );
				seen.add( refObjId );
				return Object.freeze( { refObjId, plus } );
			} )
		);
	}

	/*
	================
	character
	================
	*/
	function character( value: unknown ): CharacterRecord {
		const v = object( value ), loadout = object( v.visualLoadout );
		const deletionBlocker = v.deletionBlocker;
		if (
			deletionBlocker !== undefined &&
			(typeof deletionBlocker !== "string" || !DELETION_BLOCKERS.includes( deletionBlocker ))
		) throw Error( "Invalid deletion blocker" );
		const numeric: Record<string, number> = {};
		for (
			const key of [
				"id",
				"level",
				"raceIndex",
				"gender",
				"figureIndex",
				"heightIndex",
				"volumeIndex",
				"weaponIndex",
				"protectorIndex",
				"maxHp",
				"maxMp"
			]
		) numeric[key] = integer( v[key] );
		for ( const key of [ "bodyShapeByte", "experiencePercent", "skillPoints", "currentHp", "currentMp" ] ) {
			if ( v[key] !== undefined ) numeric[key] = number( v[key] );
		}
		const height = number( loadout.heightScale ), volume = number( loadout.volumeScale );
		if ( height === 0 || volume === 0 ) throw new Error( "Invalid character scale" );
		return Object.freeze( {
			...numeric,
			...(deletionBlocker !== undefined ? { deletionBlocker } : {}),
			name: string( v.name ),
			armorSelected: boolean( v.armorSelected ),
			weaponSelected: boolean( v.weaponSelected ),
			deletePending: boolean( v.deletePending ),
			...(v.deleteReservedAt !== undefined ? { deleteReservedAt: string( v.deleteReservedAt ) } : {}),
			visualLoadout: Object.freeze( {
				modelCodename: string( loadout.modelCodename ),
				animationSetName: string( loadout.animationSetName ),
				items: items( loadout.items, MAX_WORN_ITEMS ),
				avatars: items( loadout.avatars, MAX_AVATAR_ITEMS ),
				heightScale: height,
				volumeScale: volume
			} )
		} ) as unknown as CharacterRecord;
	}

	return {
		/*
		================
		servers
		================
		*/
		servers( value: unknown ): readonly ServerRecord[] {
			const ids = new Set<string>();
			return Object.freeze(
				array( value, MAX_SERVER_ROWS ).map( item => {
					const v = object( item ), id = string( v.id );
					if ( ids.has( id ) ) throw new Error( "Duplicate server identity" );
					ids.add( id );
					return Object.freeze( {
						id,
						name: string( v.name ),
						onlinePlayers: integer( v.onlinePlayers ),
						capacity: integer( v.capacity ),
						nativeServerId: integer( v.nativeServerId ),
						nativeFarmId: integer( v.nativeFarmId ),
						isTest: boolean( v.isTest ),
						operating: boolean( v.operating ),
						transportUrl: string( v.transportUrl )
					} );
				} )
			);
		},

		/*
		================
		roster
		================
		*/
		roster( value: unknown ): readonly CharacterRecord[] {
			const v = object( value );
			if (
				v.characterRosterContractVersion !== CHARACTER_ROSTER_CONTRACT_VERSION || v.action !== 2 ||
				v.nativeResult !== 1
			) throw new Error( "Unsupported character roster response" );
			const ids = new Set<number>();
			return Object.freeze(
				array( v.characters ).map( row => {
					const result = character( row );
					if ( ids.has( result.id ) ) throw new Error( "Duplicate character identity" );
					ids.add( result.id );
					return result;
				} )
			);
		}
	};
}
