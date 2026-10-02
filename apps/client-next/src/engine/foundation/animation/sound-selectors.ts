/*
===========================================================================

sound-selectors.ts - which effectsound.txt rows a character cue plays

The native sound body (CGEffSoundBody, dispatch table BBDE90) builds an
exact lookup key per cue from the character's profile, the cue, the skill,
the weapon label and the hit strength. This module computes those keys and
the weapon and skill parts that feed them.

===========================================================================
*/
/*
================
CharacterSoundContext
================
*/
export interface CharacterSoundContext {
	readonly player: boolean;
	readonly weapon?: string;
	readonly skill?: string;
	readonly berserk?: boolean;
	readonly critical?: boolean;
}
/*
================
weaponSoundLabel

CCObjCharacter_GetWeaponSoundLabel (8ED800) -> CItemData_GetSoundLabel
(8ED490), its weapon arm: weapon class (TID word >> 11) -> the
effectsound.txt object label. An unlisted class has an empty label, so it
matches no row. The caller passes PUNCH when slot 6 is empty.
================
*/
export function weaponSoundLabel( typeFlags: number ): string {
	switch ( (typeFlags >>> 11) & 31 ) {
		case 2:
		case 7:
			return "SWORD";
		case 3:
			return "BLADE";
		case 4:
			return "SPEAR";
		case 5:
			return "TBLADE";
		case 6:
			return "BOW";
		case 8:
			return "TSWORD";
		case 9:
			return "DUELAXE";
		case 10:
		case 15:
			return "WAND";
		case 11:
			return "STAFF";
		case 12:
			return "CROSSBOW";
		case 13:
			return "DAGGER";
		case 14:
			return "HARP";
		case 16:
			return "HAMMER";
		default:
			return "";
	}
}
/*
================
skillSoundRoots

Native 8EDFA0 resolves the requested skill only. Admission must not traverse
unrelated overrides: the authored table contains an unused self-reference.
Cache successful paths so shared ancestry is resolved once per admitted table.
================
*/
export function skillSoundRoots(
	rows: readonly string[]
): Pick<ReadonlyMap<number, readonly [string, string]>, "get"> {
	const records = new Map<number, { parent: number; name: string; group: string; }>(),
		roots = new Map<number, readonly [string, string]>();
	for ( const row of rows ) {
		if ( typeof row !== "string" ) throw Error( "Invalid skill sound row" );
		const [id, parent, name, group] = row.split( "\t" );
		if (
			!/^\d+$/.test( id ?? "" ) || !/^\d+$/.test( parent ?? "" ) || !name || !group || records.has( Number( id ) )
		) throw Error( "Invalid skill sound row" );
		records.set( Number( id ), { parent: Number( parent ), name, group } );
	}
	return {
		get( id: number ) {
			if ( !records.has( id ) ) return undefined;
			const cached = roots.get( id );
			if ( cached ) return cached;
			const path = new Set<number>();
			let current = id, result: readonly [string, string];
			for ( ;; ) {
				const known = roots.get( current );
				if ( known ) {
					result = known;
					break;
				}
				if ( path.has( current ) ) throw Error( "Cyclic skill sound parent" );
				path.add( current );
				const row = records.get( current );
				if ( !row ) {
					result = [ "-", "-" ];
					break;
				}
				if ( !row.parent ) {
					result = [ row.name, row.group ];
					break;
				}
				current = row.parent;
			}
			for ( const member of path ) roots.set( member, result );
			return result;
		}
	};
}
/*
================
characterSoundKeys

Native CGEffSoundBody dispatch table (BBDE90), handlers 8F99A0–8FAA20.
Each entry is an ordered exact lookup. A missing specialized row is not a
wildcard.
================
*/
export function characterSoundKeys(
	profile: string,
	cue: string,
	context: CharacterSoundContext,
	surface?: string
): readonly string[] {
	const { player, berserk } = context,
		weapon = context.weapon ?? "PUNCH",
		skill = context.skill ?? "-",
		strength = context.critical ? "CRITYCAL" : "NORMAL";
	const key = ( object: string, handle = cue, id = "-", one = "-", two = "-", three = "-" ) =>
		[ object, handle, id, one, two, three ].join( ":" );
	// 8F98B0: ANI_PICK carries this cue, but its selector belongs to ITEM,
	// independent of the picker profile, weapon, skill and critical state.
	if ( cue === "SND_PICKUP" ) return [ key( "ITEM" ) ];
	if ( /^SND_(WALK|RUN)[12]$/.test( cue ) ) {
		return player ?
			(surface ? [ key( "PLAYER", cue, "-", "FIELD", surface ) ] : []) :
			[ key( profile ) ];
	}
	if ( cue === "SND_BLOCKING" ) return player ? [ key( "PLAYER", cue, "-", "-", strength ) ] : [];
	if ( cue === "SND_CRIDMG" ) {
		return [ player ? key( "PLAYER", cue, "-", berserk ? "HWAN" : "-" ) : key( profile, cue, skill ) ];
	}
	if ( cue === "SND_DMG" || cue === "SND_DDMG" ) {
		if ( !player ) return [ key( profile, cue, skill ) ];
		if ( berserk ) return [ key( "PLAYER", cue, "-", "HWAN" ) ];
		const primary = key( "PLAYER", cue, skill );
		return cue === "SND_DDMG" ? [ primary, key( "PLAYER", cue, "-", weapon, "-", "NORMAL" ) ] : [ primary ];
	}
	if ( /^SND_SWING[1-4]$/.test( cue ) ) {
		return [
			player ?
				(berserk ? key( "PLAYER", "SND_SWING3", "-", "HWAN", weapon ) : key( "PLAYER", cue, "-", weapon )) :
				key( profile, cue, skill )
		];
	}
	if ( /^SND_SWING_S[1-4]$/.test( cue ) ) return [ key( player ? "PLAYER" : profile, cue, skill ) ];
	if ( cue === "SND_ACTIVATE" ) return player ? [ key( "PLAYER", cue, skill ) ] : [];
	if ( cue === "VOC_MOAN" ) return [ key( profile, cue, "-", strength ) ];
	if ( /^VOC_SHOUT[12]$/.test( cue ) ) return [ key( profile, cue, player ? "-" : skill ) ];
	return [ key( profile ) ];
}
