/*
===========================================================================

character-create.ts - character creation choices and their starter items

The native creation window keeps one item list per choice and dresses its
preview through CharacterCreatePreview_SetChoice -> CCObjCharacter_SetEquipSlotVisual,
the same slot visuals as every other actor. The preview here resolves the
same starter items (the _DEF rows the server grants) through the item
catalog, so what a player previews is what the new character wears.

Europe protector rule (CPSCharacterCreateEurope_OnProtectorChanged 0x730690,
by weapon class): sword, two-hand sword and axe take heavy or light armor;
darkstaff, two-hand staff and harp take the robe only; crossbow and dagger
take light armor only; the one-hand staff takes light armor or the robe.

===========================================================================
*/
import type { CharacterItem, CharacterRecord } from "@/engine/contracts/session";
import type { CreationSelection } from "@/engine/contracts/frontend";

const RACE_EUROPE = 0;
const GENDER_MALE = 0;
const FIRST_FIGURE = 1;
const LAST_FIGURE = 13;
const LAST_SCALE_INDEX = 4;
const SCALE_BASE = .94;
const SCALE_STEP = .03;

// Figure names by choice index, as the native creation window lists them.
const EUROPE_MALE_FIGURES =
	"NOBLE EXORCIST NECROMENCER MERCHANT PRIEST KNIGHT WARRIOR GLADIATOR BARBARIAN ADVENTURER ANGEL DEVIL WEREWOLF";
const EUROPE_FEMALE_FIGURES =
	"NOBLE WITCH SUMMONER MERCHANT ORACLE CRUSADER AMAZONESS KNIGHT ADVENTURER GLADIATOR ANGEL DEVIL SUCCUBUS";
const CHINA_MALE_FIGURES =
	"NOBLEBOY SCHOLAR PERFORMER MERCHANT WARRIOR MONK ADVENTURER FIGHTER NECROMANCER PRIEST BOGY MONKEY TATTOO";
const CHINA_FEMALE_FIGURES =
	"NOBLEGIRL SCHOLAR KISAENG MERCHANT ASSASSIN WARRIOR ADVENTURER FIGHTER NECROMENCERW NECROMENCERB FOX BOGY KANGSI";

/*
================
weaponKinds

Weapon choice index -> weapon kind; index 0 is "no weapon".
================
*/
function weaponKinds( s: CreationSelection ): readonly string[] {
	return s.race === RACE_EUROPE ?
		[ "", "DAGGER", "SWORD", "TSWORD", "AXE", "CROSSBOW", "DARKSTAFF", "TSTAFF", "HARP", "STAFF" ] :
		[ "", "SWORD", "BLADE", "SPEAR", "TBLADE", "BOW" ];
}

/*
================
europeProtectorKinds

The native Europe protector rule by weapon kind (header comment).
================
*/
function europeProtectorKinds( weapon: string ): readonly string[] {
	switch ( weapon ) {
		case "SWORD":
		case "TSWORD":
		case "AXE":
			return [ "HEAVY", "LIGHT" ];
		case "DARKSTAFF":
		case "TSTAFF":
		case "HARP":
			return [ "CLOTHES" ];
		case "CROSSBOW":
		case "DAGGER":
			return [ "LIGHT" ];
		case "STAFF":
			return [ "LIGHT", "CLOTHES" ];
		default:
			return [];
	}
}

/*
================
hasStarterShield

One-hand kinds whose starter set includes a shield.
================
*/
function hasStarterShield( kind: string ): boolean {
	return kind === "CH_SWORD" || kind === "CH_BLADE" || kind === "EU_SWORD" || kind === "EU_STAFF";
}

/*
================
weaponAnimationSet

ItemTypeWord_ToAnimationSetName: weapon kind -> animation set. The
darkstaff (class 0x0a) shares the one-hand staff set.
================
*/
function weaponAnimationSet( kind: string ): string {
	switch ( kind ) {
		case "CH_SWORD":
		case "CH_BLADE":
			return "sword";
		case "CH_SPEAR":
		case "CH_TBLADE":
			return "spear";
		case "CH_BOW":
		case "EU_CROSSBOW":
			return "bow";
		case "EU_SWORD":
			return "onehand_sword";
		case "EU_TSWORD":
			return "twohand_sword";
		case "EU_AXE":
			return "dual_axe";
		case "EU_DARKSTAFF":
		case "EU_STAFF":
			return "onehand_staff";
		case "EU_TSTAFF":
			return "twohand_staff";
		case "EU_DAGGER":
			return "dagger";
		case "EU_HARP":
			return "harf";
		default:
			throw Error( "Unknown creation weapon " + kind );
	}
}

/*
================
initialCreation
================
*/
export function initialCreation( race: 0 | 1 ): CreationSelection {
	return { race, gender: 0, figure: 1, height: 2, volume: 2, weapon: 0, protector: 0, name: "" };
}

/*
================
creationProtectors

The protector kinds offered for the current weapon choice.
================
*/
export function creationProtectors( s: CreationSelection ): readonly string[] {
	return s.race === RACE_EUROPE ?
		europeProtectorKinds( weaponKinds( s )[s.weapon] ?? "" ) :
		[ "HEAVY", "LIGHT", "CLOTHES" ];
}

/*
================
creationRange
================
*/
export function creationRange(
	s: CreationSelection,
	key: "figure" | "height" | "volume" | "weapon" | "protector"
): readonly [number, number] {
	if ( key === "figure" ) return [ FIRST_FIGURE, LAST_FIGURE ];
	if ( key === "weapon" ) return [ 0, weaponKinds( s ).length - 1 ];
	if ( key === "protector" ) return [ 0, creationProtectors( s ).length ];
	return [ 0, LAST_SCALE_INDEX ];
}

/*
================
creationStarterCodenames

The _DEF starter items a creation choice grants, in slot order: chest,
legs, feet, weapon, shield. No protector grants no garments; the ownerless
default wear covers the body, as in the native preview.
================
*/
export function creationStarterCodenames( s: CreationSelection ): readonly string[] {
	const race = s.race === RACE_EUROPE ? "EU" : "CH", sex = s.gender === GENDER_MALE ? "M" : "W";
	const weapon = weaponKinds( s )[s.weapon] ?? "";
	const armor = creationProtectors( s )[s.protector - 1];
	const codenames: string[] = [];
	if ( armor ) {
		// Chest, legs and feet: the three protector slots.
		for ( const part of [ "BA", "LA", "FA" ] ) codenames.push( `ITEM_${race}_${sex}_${armor}_01_${part}_A_DEF` );
	}
	if ( weapon ) {
		codenames.push( `ITEM_${race}_${weapon}_01_A_DEF` );
		if ( hasStarterShield( race + "_" + weapon ) ) codenames.push( `ITEM_${race}_SHIELD_01_A_DEF` );
	}
	return codenames;
}

/*
================
creationModelCodename

The CharacterData model a figure choice selects.
================
*/
export function creationModelCodename( s: CreationSelection ): string {
	const europe = s.race === RACE_EUROPE, male = s.gender === GENDER_MALE;
	const figures = europe ?
		(male ? EUROPE_MALE_FIGURES : EUROPE_FEMALE_FIGURES) :
		(male ? CHINA_MALE_FIGURES : CHINA_FEMALE_FIGURES);
	const figure = figures.split( " " )[s.figure - 1];
	if ( !figure ) throw Error( "Invalid creation figure" );
	return `CHAR_${europe ? "EU" : "CH"}_${male ? "MAN" : "WOMAN"}_${figure}`;
}

/*
================
creationLoadout

The preview loadout of a creation selection. Items resolve through the
catalog's codename index; a starter item missing from the catalog is an
error, never an undressed preview.
================
*/
export function creationLoadout(
	s: CreationSelection,
	itemIds: ReadonlyMap<string, number>
): CharacterRecord["visualLoadout"] {
	const europe = s.race === RACE_EUROPE, race = europe ? "EU" : "CH";
	const weapon = weaponKinds( s )[s.weapon] ?? "";
	const items: CharacterItem[] = creationStarterCodenames( s ).map( codename => {
		const refObjId = itemIds.get( codename );
		if ( refObjId === undefined ) throw Error( "Missing creation item " + codename );
		return { refObjId, plus: 0 };
	} );
	return {
		modelCodename: creationModelCodename( s ),
		items,
		avatars: [],
		animationSetName: weapon ? weaponAnimationSet( race + "_" + weapon ) : "default",
		heightScale: SCALE_BASE + s.height * SCALE_STEP,
		volumeScale: SCALE_BASE + s.volume * SCALE_STEP
	};
}

export interface NameRules {
	allowed: ReadonlySet<number>;
	forbidden: readonly string[];
	wholeWords: readonly string[];
}

/*
================
creationNameRules

Parses the native name filter: #ALLOW_ID_TABLE code point ranges, then
substring (type 2) and whole-word (type 1) forbidden entries.
================
*/
export function creationNameRules( bytes: ArrayBuffer ): NameRules {
	const raw = new Uint8Array( bytes ),
		encoding = raw[0] === 255 && raw[1] === 254 || raw[1] === 0 ? "utf-16le" : "utf-8",
		allowed = new Set<number>(),
		forbidden: string[] = [],
		wholeWords: string[] = [];
	for ( const line of new TextDecoder( encoding, { fatal: true } ).decode( raw ).split( /\r?\n/ ) ) {
		const fields = line.trim().split( "\t" );
		if ( fields[0] === "#ALLOW_ID_TABLE" ) {
			const a = parseInt( fields[1] ?? "", 16 ), b = parseInt( fields[2] ?? "", 16 );
			if ( a >= 0 && b <= 65535 ) { for ( let n = a; n <= b; n++ ) allowed.add( n ); }
		} else if (
			!fields[0]?.startsWith( "#" ) && fields[0] && fields[0] !== "\\n" &&
			[ 1, 2 ].includes( Number( fields[1] ) )
		) {
			(Number( fields[1] ) === 1 ? wholeWords : forbidden).push(
				fields[0].replace( /[A-Z]/g, c => c.toLowerCase() )
			);
		}
	}
	if ( !allowed.size ) throw Error( "Missing native name character table" );
	return { allowed, forbidden, wholeWords };
}

/*
================
creationNameError
================
*/
export function creationNameError( name: string, rules: NameRules ): string | null {
	if ( name.length < 2 || name.length > 12 ) return "UIO_MSG_ERROR_CHARACTER_NAME_STRING";
	return checkedNameError( name, rules );
}

/*
================
checkedNameError

78E510 ASCII fold; 78FE50 substring list versus 790730 space-token lookup.
================
*/
export function checkedNameError( name: string, rules: NameRules ): string | null {
	const normalized = name.replace( /[A-Z]/g, c => c.toLowerCase() );
	if (
		Array.from( normalized ).some( c => !rules.allowed.has( c.codePointAt( 0 )! ) ) || !textAllowed( name, rules )
	) return "UIO_MSG_ERROR_CHARACTER_WRONGSTRING";
	return null;
}

/*
================
textAllowed

CStringCheck_IsTextAllowed (790B60): the ASCII-folded text holds no
forbidden substring and no forbidden space-separated word. Free text (a
stall title) is not held to the name character table.
================
*/
export function textAllowed( value: string, rules: NameRules ): boolean {
	const normalized = value.replace( /[A-Z]/g, c => c.toLowerCase() );
	return !rules.forbidden.some( word => normalized.includes( word ) ) &&
		!normalized.split( / +/ ).some( word => rules.wholeWords.includes( word ) );
}
