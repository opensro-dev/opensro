/*
===========================================================================

cos-command.ts - the native COS HUD rules: status icon, command bar, info page

Pure rules ported from the CIFCOSManager family of SRO_Client.exe. The record
class is SCharCosData +0x14, which CCOSDataManager_DeserializeOwnedCOSRecord
(830EC0) derives from the reference's TID4 band: riding mount 0, transport 1,
pickup pet 2, attack pet 3, guild soldier 4. Every branch below keys on that
class, never on the band directly, as the original does.

===========================================================================
*/
import type { CosRecord } from "@/engine/contracts/gameplay";
import type { UiRect } from "@/engine/contracts/ui";

export const COS_CLASS_RIDING = 0;
export const COS_CLASS_TRANSPORT = 1;
export const COS_CLASS_PICKUP = 2;
export const COS_CLASS_ATTACK = 3;
export const COS_CLASS_GUILD = 4;

export const COS_COMMAND_INFO = 0;
export const COS_COMMAND_RIDE = 1;
export const COS_COMMAND_ATTACK = 2;
export const COS_COMMAND_FOLLOW = 3;
export const COS_COMMAND_CANCEL = 4;
export const COS_COMMAND_CLEAN = 5;
export const COS_COMMAND_STANCE = 6;

// CIFCOSStatus placement (CIFCOSManager_LayoutStatusIcons 6F1880).
const STATUS_RIGHT_MARGIN = 172;
const STATUS_STEP = 48;
const STATUS_TOP = 2;
// CIFCOSCommand row geometry (CIFCOSCommand_LayoutRows 6A34F0), relative to
// the under bar's origin.
const COMMAND_ORIGIN_X = 653;
const COMMAND_BUTTON_STEP = 31;
const COMMAND_FRAME_DY = -34;
const COMMAND_SLOT_DX = 4;
const COMMAND_SLOT_DY = -29;
const COMMAND_SLOT_SIZE = 32;
const COMMAND_BOARD_DY = -23;
const COMMAND_TOGGLE_DX = 5;
const COMMAND_TOGGLE_DY = -18;
// SCharCosData +0x10: satiety in 1/10000 (CIFCosInfo_RefreshSatietyGauge).
const SATIETY_FULL = 10000;
const ICON = "/assets/images/Media_extracted/icon/action/";
const ANIMAL = "/assets/images/Media_extracted/interface/animal/";
const OUTLINE = "/assets/images/Media_extracted/icon/etc/";

/*
================
CosReference

The RefObjChar fields the COS HUD reads: +0x154 icon, +0x1B0 max HP and
+0x1D4, which gates the Ride command (non-zero for transports only; a
riding mount is boarded at summon and leaves through Clean).
================
*/
export interface CosReference {
	readonly icon: string;
	readonly maxHp: number;
	readonly rideable: boolean;
}

/*
================
cosClass

830EC0 @0x830FF6..0x831068. A band the original does not classify (quest
companions) gets no HUD.
================
*/
export function cosClass( band: number ): number | null {
	switch ( band ) {
		case 1:
			return COS_CLASS_RIDING;
		case 2:
			return COS_CLASS_TRANSPORT;
		case 4:
			return COS_CLASS_PICKUP;
		case 3:
			return COS_CLASS_ATTACK;
		case 5:
			return COS_CLASS_GUILD;
		default:
			return null;
	}
}

/*
================
cosCommandButtons

CIFCOSCommand_BindCompanion (6A3DF0): the command of each row in order.
================
*/
export function cosCommandButtons( cls: number ): readonly number[] {
	switch ( cls ) {
		case COS_CLASS_RIDING:
		case COS_CLASS_TRANSPORT:
			return [ COS_COMMAND_INFO, COS_COMMAND_RIDE, COS_COMMAND_CLEAN ];
		case COS_CLASS_PICKUP:
			return [ COS_COMMAND_INFO, COS_COMMAND_RIDE, COS_COMMAND_CANCEL, COS_COMMAND_FOLLOW ];
		case COS_CLASS_ATTACK:
			return [
				COS_COMMAND_INFO,
				COS_COMMAND_RIDE,
				COS_COMMAND_CANCEL,
				COS_COMMAND_FOLLOW,
				COS_COMMAND_ATTACK,
				COS_COMMAND_STANCE
			];
		case COS_CLASS_GUILD:
			return [ COS_COMMAND_CLEAN ];
		default:
			return [];
	}
}

/*
================
CosCommandContext

What CosCommand_ResolveIconPathByKind (6A1BE0) reads: the active record,
its reference, the local player's death state (+0x644 bit 1, action state
1) and whether the player rides a vehicle (+0x298).
================
*/
export interface CosCommandContext {
	readonly record: CosRecord | undefined;
	readonly reference: CosReference | undefined;
	readonly ownerDead: boolean;
	readonly mounted: boolean;
}

/*
================
cosCommandEnabled

The enabled arm of each 6A1BE0 case.
================
*/
export function cosCommandEnabled( command: number, context: CosCommandContext ): boolean {
	const { record, reference, ownerDead } = context;
	if ( !record ) return false;
	const cls = cosClass( record.band );
	if ( command === COS_COMMAND_INFO ) return true;
	if ( ownerDead ) return false;
	switch ( command ) {
		case COS_COMMAND_RIDE:
			return !!reference?.rideable;
		case COS_COMMAND_ATTACK:
		case COS_COMMAND_STANCE:
			return cls === COS_CLASS_ATTACK;
		case COS_COMMAND_FOLLOW:
		case COS_COMMAND_CANCEL:
			return cls === COS_CLASS_PICKUP || cls === COS_CLASS_ATTACK;
		case COS_COMMAND_CLEAN:
			return cls !== COS_CLASS_PICKUP && cls !== COS_CLASS_ATTACK;
		default:
			return false;
	}
}

/*
================
cosCommandIcon

6A1BE0: the icon of a command row, its _disable variant when the arm is off.
The stance icon shows the current mode (+0xAB08: 1 aggressive, 0 defensive).
================
*/
export function cosCommandIcon( command: number, context: CosCommandContext ): string {
	const enabled = cosCommandEnabled( command, context );
	const state = ( name: string ) => ICON + name + (enabled ? "" : "_disable") + ".png";
	switch ( command ) {
		case COS_COMMAND_INFO:
			return state( "cos_cmd_coswindow" );
		case COS_COMMAND_RIDE:
			return enabled ?
				ICON + (context.mounted ? "cos_cmd_disembark" : "cos_cmd_embark") + ".png" :
				ICON + "cos_cmd_embark_disable.png";
		case COS_COMMAND_ATTACK:
			return state( "cos_cmd_skill_page" );
		case COS_COMMAND_FOLLOW:
			return state( "cos_cmd_follower" );
		case COS_COMMAND_CANCEL:
			return state( "cos_cmd_unsummon" );
		case COS_COMMAND_CLEAN:
			return state( "cos_cmd_ai_destruction" );
		case COS_COMMAND_STANCE: {
			const mode = context.record?.commandMode;
			if ( !enabled || (mode !== 0 && mode !== 1) ) return ICON + "cos_cmd_aggressive_disable.png";
			return ICON + (mode === 1 ? "cos_cmd_aggressive" : "cos_cmd_defensive") + ".png";
		}
		default:
			return ICON + "cos_cmd_coswindow_disable.png";
	}
}

/*
================
cosCommandLabel

CosCommand_GetCommandLabel (6A2910): the help text key of a command row.
================
*/
export function cosCommandLabel( command: number, context: CosCommandContext ): string {
	switch ( command ) {
		case COS_COMMAND_INFO:
			return "UIIT_STT_COSNEWUI_INFO";
		case COS_COMMAND_RIDE:
			return context.mounted ? "UIIT_STT_COS_DISEMBARK" : "UIIT_STT_COS_RIDE";
		case COS_COMMAND_ATTACK:
			return "UIIT_STT_COS_ATTACK";
		case COS_COMMAND_FOLLOW:
			return "UIIT_STT_COSNEWUI_FOLLOW";
		case COS_COMMAND_CANCEL:
			return "UIIT_STT_COSNEWUI_SUMMONCANCEL";
		case COS_COMMAND_CLEAN:
			return "UIIT_STT_COS_CLEAN";
		case COS_COMMAND_STANCE:
			return context.record?.commandMode === 1 ? "UIIT_STT_COS_AGGRESSIVE" : "UIIT_STT_COS_DEFENSIVE";
		default:
			return "";
	}
}

/*
================
cosStanceToggle

CICCos_ExecuteActionCommand (6A2350) case 6 with no explicit mode: any mode
other than aggressive becomes aggressive, aggressive becomes defensive.
================
*/
export function cosStanceToggle( mode: number | undefined ): number {
	return mode !== 1 ? 1 : 0;
}

/*
================
cosStatusChrome

CIFCOSStatus_BindCompanion (6AA290): an attack pet draws the tall
am_cos_window with its HGP gauge and outline 1; every other class the short
am_window and outline 2. A guild soldier also hides its HP gauge.
================
*/
export function cosStatusChrome( cls: number ) {
	const attack = cls === COS_CLASS_ATTACK;
	return {
		frame: ANIMAL + (attack ? "am_cos_window.png" : "am_window.png"),
		size: attack ? [ 44, 68 ] as const : [ 44, 56 ] as const,
		outline: OUTLINE + (attack ? "cos_outline_1.png" : "cos_outline_2.png"),
		outlineSize: attack ? [ 68, 86 ] as const : [ 65, 75 ] as const,
		showHp: cls !== COS_CLASS_GUILD,
		showHgp: attack
	};
}

/*
================
cosStatusRect

6F1880: the i-th status icon hangs from the top right corner, sized by
its class's frame.
================
*/
export function cosStatusRect( screenWidth: number, index: number, cls: number ): UiRect {
	const [width, height] = cosStatusChrome( cls ).size;
	return [ screenWidth - STATUS_RIGHT_MARGIN - STATUS_STEP * index, STATUS_TOP, width, height ];
}

/*
================
cosStatusRatios

CIFCOSStatus_OnUpdate (6A9C50): HP over the reference's max HP and, for an
attack pet, satiety over 10000. A missing max HP leaves the gauge alone.
================
*/
export function cosStatusRatios( record: CosRecord, reference: CosReference | undefined ) {
	const hp = reference && reference.maxHp > 0 ? Math.min( 1, record.hp / reference.maxHp ) : null;
	const hgp = cosClass( record.band ) === COS_CLASS_ATTACK && record.satiety !== undefined ?
		Math.min( 1, record.satiety / SATIETY_FULL ) :
		null;
	return { hp, hgp };
}

/*
================
cosCommandLayout

6A34F0 lays n rows out leftward from the under bar: each row is a frame
piece (front, middle..., end) with its 32x32 slot, then the board and the
open/close toggle.
================
*/
export function cosCommandLayout( underBarX: number, underBarY: number, count: number ) {
	const x0 = underBarX - COMMAND_BUTTON_STEP * count + COMMAND_ORIGIN_X;
	const rows = Array.from( { length: count }, ( _, i ) => {
		const x = x0 + COMMAND_BUTTON_STEP * i;
		const piece = i === 0 ? "front" : i === count - 1 ? "end" : "middle";
		return {
			frame: ANIMAL + "am_ctrl_window_" + piece + ".png",
			at: [ x, underBarY + COMMAND_FRAME_DY ] as const,
			slot: [ x + COMMAND_SLOT_DX, underBarY + COMMAND_SLOT_DY, COMMAND_SLOT_SIZE, COMMAND_SLOT_SIZE ] as UiRect
		};
	} );
	return {
		rows,
		board: [ x0 + COMMAND_BUTTON_STEP * count, underBarY + COMMAND_BOARD_DY ] as const,
		toggle: [ x0 + COMMAND_BUTTON_STEP * count + COMMAND_TOGGLE_DX, underBarY + COMMAND_TOGGLE_DY ] as const
	};
}

/*
================
cosHpText

CIFCosInfo_RefreshHP (6A4280): "%d/%d (%d%%)", or "100%" without a max HP.
================
*/
export function cosHpText( record: CosRecord, reference: CosReference | undefined ): string {
	if ( !reference || reference.maxHp <= 0 ) return "100%";
	const ratio = record.hp / reference.maxHp;
	return `${record.hp}/${reference.maxHp} (${Math.trunc( Math.fround( ratio ) * 100 )}%)`;
}

/*
================
cosSatietyText

CIFCosInfo_RefreshSatietyGauge (6A43E0): "%d%% (%d)". The percent is
1 - ftol(gauge * -100.0) capped at 100 (@0x6A445C..0x6A447B), one above the
truncated percent, read back from the gauge's float.
================
*/
export function cosSatietyText( satiety: number ): string {
	const ratio = satiety === SATIETY_FULL ? 1 : Math.fround( satiety / SATIETY_FULL );
	const percent = Math.min( 100, 1 - Math.trunc( ratio * -100 ) );
	return `${percent}% (${satiety})`;
}

/*
================
cosInfoSections

CIFCOSInfo_SetCompanion (6A69F0): which info page blocks a class shows. The
HGP/EXP rows and the ability table belong to attack pets; the rent time
replaces the HP row for pickup pets.
================
*/
export function cosInfoSections( cls: number ) {
	return {
		hp: cls !== COS_CLASS_PICKUP,
		rentTime: cls === COS_CLASS_PICKUP,
		growth: cls === COS_CLASS_ATTACK
	};
}

/*
================
decodeCosReferences

/assets/data/cosPresentation.json (buildCosPresentationAsset.mjs):
refObjId -> [icon, maxHp, rideable]. Malformed rows reject the catalog.
================
*/
export function decodeCosReferences( raw: unknown ): ReadonlyMap<number, CosReference> {
	const value = raw as { format?: unknown; rows?: unknown; };
	if ( value?.format !== "sro-cos-presentation" || !value.rows || typeof value.rows !== "object" ) {
		throw Error( "Invalid COS presentation catalog" );
	}
	const references = new Map<number, CosReference>();
	for ( const [key, row] of Object.entries( value.rows as Record<string, unknown> ) ) {
		const id = Number( key );
		if (
			!Number.isSafeInteger( id ) || id <= 0 || !Array.isArray( row ) || row.length !== 3 ||
			typeof row[0] !== "string" || !Number.isSafeInteger( row[1] ) || row[1] < 0 || typeof row[2] !== "boolean"
		) throw Error( "Invalid COS presentation row " + key );
		references.set( id, { icon: row[0], maxHp: row[1], rideable: row[2] } );
	}
	return references;
}

/*
================
cosExperienceText

CIFCosInfo_RefreshExperience (6A44C0): "%I64d/%I64d (%.2f%%)" over the
record's u64 experience (+0x50) and its level's Exp_C (+0x58, from the level
data payload +0x8). The float percent is held at 99.99 or below.
================
*/
export function cosExperienceText( current: bigint, max: bigint ): { text: string; ratio: number; } {
	const ratio = Math.fround( Number( current ) / Number( max ) );
	const percent = Math.min( ratio * 100, Math.fround( 99.99 ) );
	return { text: `${current}/${max} (${Math.fround( percent ).toFixed( 2 )}%)`, ratio };
}

/*
================
cosRentText

CIFCOSInfo rent time (6A4FD0): the summoner item's remaining rent split into
days, hours and minutes, or zeros once it has run out.
================
*/
export function cosRentText( remainingMs: number, day: string, hour: string, minute: string ): string {
	const DAY_MS = 86400000, HOUR_MS = 3600000, MINUTE_MS = 60000;
	if ( remainingMs <= 0 ) return `0${day} 0${hour} 0${minute}`;
	const rest = remainingMs % DAY_MS;
	return `${Math.trunc( remainingMs / DAY_MS )}${day} ${Math.trunc( rest / HOUR_MS )}${hour} ` +
		`${Math.trunc( rest % HOUR_MS / MINUTE_MS )}${minute}`;
}
