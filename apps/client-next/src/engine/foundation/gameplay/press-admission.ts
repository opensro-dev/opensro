/*
===========================================================================

press-admission.ts - whether the server will admit the local caster's press

The native press animates only on the server's answer
(CGInterface_ExecuteSelectedActionAtTarget 6FCD50). The port starts the
cast and stands its cooldown in at the press, so every press the server
then refuses played and snapped back (BUG-066 stun, a wrong weapon in
BR-261007-0624). This module replays the caster's side of
Skill_ValidatePrerequisitesAndCost (58D8F0) in the server's order, from
the row's published inputs (SkillUiAdmit, enterworld/skillcatalogue.go)
and the caster's own state. The press is always sent; only its prediction
depends on the answer.

Gates the client cannot read (an rpkt buff, the qest area, a transform
mode, the dance selector, a knocked-down target, battle state, line of
sight) answer "unknown", and an unknown press is not predicted. The
cooldown (decidePress) and the target groups (skillAdmitsPredictedTarget)
have their own owners and run beside this one.

===========================================================================
*/

// CASTER_DISABLED_ABNORMAL is freeze, sleep and stun (g_adwAbnormalStatusBit
// 0x1, 0x40, 0x4000), the set 58DAEF refuses; ROOT_ABNORMAL is the root bit
// 58E010 reads (+0xD34 0x80).
export const CASTER_DISABLED_ABNORMAL = 0x4041;
const ROOT_ABNORMAL = 0x80;
// Body states (state channel 4): berserk and stealth.
const BODY_BERSERK = 1, BODY_STEALTH = 6;
// 58DF8C compares current HP with maximum HP times the widened float 0.3f.
const LOW_HP_RATIO = 0.30000001192092896;
// Equipment sockets: the primary weapon, and the shield socket ammunition
// shares; armourSocket maps armour index 1..6.
const PRIMARY_SOCKET = 6, SHIELD_SOCKET = 7;
// 0xFF/0xFF weapon kinds admit any weapon (58D480); a bare hand counts as 1;
// TID4 16 is a fortress weapon, which needs a siege context (0x3047).
const ANY_WEAPON = 0xff, BARE_HAND = 1, FORTRESS_WEAPON = 16;
// Refusal codes the gates below return.
const DISABLED = 0x3009, BERSERK_HIDE = 0x3031, NOT_LOW_HP = 0x3036, NOT_STEALTHED = 0x3034;
const WRONG_WEAPON = 0x300d, FORTRESS_ONLY = 0x3047, BROKEN = 0x300f;
const NO_HP = 0x3013, NO_MP = 0x3004, NO_AMMUNITION = 0x300e;

/*
================
PressAdmit

A row's published 58D8F0 inputs (SkillUiAdmit).
================
*/
export interface PressAdmit {
	readonly nmf?: boolean;
	readonly serverOnly?: boolean;
	readonly berserk?: boolean;
	readonly lowHp?: boolean;
	readonly stealthStrike?: boolean;
	readonly teleports?: boolean;
	readonly weaponKinds: readonly [number, number];
	readonly reqi?: { readonly all: boolean; readonly pairs: readonly (readonly [number, number])[]; };
	readonly hp?: number;
	readonly hpPercent?: number;
	readonly ammunition?: boolean;
}

/*
================
EquippedItem

One equipped row (InventoryItem): slot is its equipment socket.
================
*/
export interface EquippedItem {
	readonly slot: number;
	readonly typeFlags: number;
	readonly durability: number;
	readonly quantity: number;
}

/*
================
PressSkill

The pressed row's inputs: its published admission, the ao/pw cast gate
and its MP cost at the caster's maximum MP (skillMpCost).
================
*/
export interface PressSkill {
	readonly admit?: PressAdmit;
	readonly needsFooting?: boolean;
	readonly mpCost: number;
}

/*
================
PressCaster

The local caster's state the gates read.
================
*/
export interface PressCaster {
	readonly abnormal: number;
	readonly hp: number;
	readonly maxHp: number;
	readonly mp: number;
	readonly maxMp: number;
	// State channel 4 (appearanceState[2]).
	readonly body: number;
	readonly mounted: boolean;
	readonly seated: boolean;
	readonly equipped: readonly EquippedItem[];
}

type Equipped = PressCaster["equipped"];

export type PressAdmission =
	| { readonly kind: "admit"; }
	| { readonly kind: "refuse"; readonly code: number; }
	| { readonly kind: "unknown"; readonly gate: string; };

/*
================
parsePressAdmit

Strict parse of a published SkillUiAdmit.
================
*/
export function parsePressAdmit( value: unknown ): PressAdmit {
	const v = value as Record<string, unknown>;
	const flag = ( key: string ) => {
		if ( v[key] !== undefined && typeof v[key] !== "boolean" ) throw Error( `Invalid admission flag ${key}` );
		return v[key] === true;
	};
	const byte = ( n: unknown ) => {
		if ( typeof n !== "number" || !Number.isInteger( n ) || n < 0 || n > 0xff ) {
			throw Error( "Invalid weapon kind" );
		}
		return n;
	};
	const count = ( n: unknown, max: number ) => {
		if ( n === undefined ) return 0;
		if ( typeof n !== "number" || !Number.isInteger( n ) || n < 0 || n > max ) {
			throw Error( "Invalid admission cost" );
		}
		return n;
	};
	if ( !v || typeof v !== "object" || !Array.isArray( v.weaponKinds ) || v.weaponKinds.length !== 2 ) {
		throw Error( "Invalid skill admission inputs" );
	}
	let reqi: PressAdmit["reqi"];
	if ( v.reqi !== undefined ) {
		const r = v.reqi as { all?: unknown; pairs?: unknown; };
		if (
			!r || !Array.isArray( r.pairs ) || r.pairs.length > 5 || r.all !== undefined && typeof r.all !== "boolean"
		) {
			throw Error( "Invalid reqi pairs" );
		}
		reqi = {
			all: r.all === true,
			pairs: r.pairs.map( pair => {
				if ( !Array.isArray( pair ) || pair.length !== 2 ) throw Error( "Invalid reqi pair" );
				return [ count( pair[0], 0xffffffff ), count( pair[1], 0xffffffff ) ] as const;
			} )
		};
	}
	return {
		nmf: flag( "nmf" ),
		serverOnly: flag( "serverOnly" ),
		berserk: flag( "berserk" ),
		lowHp: flag( "lowHp" ),
		stealthStrike: flag( "stealthStrike" ),
		teleports: flag( "teleports" ),
		weaponKinds: [ byte( v.weaponKinds[0] ), byte( v.weaponKinds[1] ) ],
		...(reqi ? { reqi } : {}),
		hp: count( v.hp, 0xffffffff ),
		hpPercent: count( v.hpPercent, 0xffff ),
		ammunition: flag( "ammunition" )
	};
}

/*
================
pressAdmission

58D8F0's caster gates in the server's order (skilladmit.go
contextSkillAdmission). A row without admission inputs is unknown. The
cooldown (0x3005) and the target (0x3006) sit between these gates server
side; their owners answer them beside this one, and any refusal means the
same thing to the caller: do not predict.
================
*/
export function pressAdmission( skill: PressSkill, caster: PressCaster ): PressAdmission {
	const admit = skill.admit;
	if ( !admit ) return { kind: "unknown", gate: "no admission inputs" };
	// 58DA3A / 58DAEF: frozen, asleep or stunned, unless nmf.
	if ( !admit.nmf && (caster.abnormal & CASTER_DISABLED_ABNORMAL) !== 0 ) return { kind: "refuse", code: DISABLED };
	if ( admit.serverOnly ) return { kind: "unknown", gate: "server-only state" };
	// 58DF20: a hide gate without a trap refuses a berserk caster.
	if ( admit.berserk && caster.body === BODY_BERSERK ) return { kind: "refuse", code: BERSERK_HIDE };
	// 58DF8C: the row is kept for a caster at or below 30 % HP.
	if ( admit.lowHp && caster.maxHp * LOW_HP_RATIO < caster.hp ) return { kind: "refuse", code: NOT_LOW_HP };
	// 58DFE0: the press must be issued in stealth.
	if ( admit.stealthStrike && caster.body !== BODY_STEALTH ) return { kind: "refuse", code: NOT_STEALTHED };
	// 58E010: a rooted caster cannot teleport.
	if ( admit.teleports && (caster.abnormal & ROOT_ABNORMAL) !== 0 ) return { kind: "refuse", code: DISABLED };
	// 58E0BF: ao or pw while riding or seated. Standing behind a wall is the
	// server's (motion 0x11): a wall is the caster's own Force cast, which
	// already holds the caster in its action state.
	if ( skill.needsFooting && (caster.mounted || caster.seated) ) return { kind: "refuse", code: DISABLED };
	const equipment = equipmentRefusal( admit, caster.equipped );
	if ( equipment ) return { kind: "refuse", code: equipment };
	// 58E1AC: HP first, then MP. The MP rate (parameter 0x8D) starts at 100
	// and only a dcmp buff lowers it, so a cost the full rate affords is
	// admitted; a dcmp caster short of the full cost is merely not predicted.
	const hpCost = (admit.hp ?? 0) + percentOf( caster.maxHp, admit.hpPercent ?? 0 );
	if ( hpCost > 0 && caster.hp < hpCost ) return { kind: "refuse", code: NO_HP };
	if ( caster.maxMp && skill.mpCost > caster.mp ) return { kind: "refuse", code: NO_MP };
	// 58E32D: a stack of the weapon's ammunition in the shield socket.
	if ( admit.ammunition && !ammunitionReady( caster.equipped ) ) return { kind: "refuse", code: NO_AMMUNITION };
	return { kind: "admit" };
}

/*
================
percentOf

vitalPercent: the integer vital times percent / 100, truncated (crtFtol).
================
*/
function percentOf( vital: number, percent: number ): number {
	return percent ? Math.trunc( (vital | 0) * (percent / 100) ) : 0;
}

/*
================
equipmentBroken

495980, the derived +0x190 mark: durability 0 breaks an equipment item
unless its family (TID3) is 5, 12, 13 or 14.
================
*/
export function equipmentBroken( typeFlags: number, durability: number ): boolean {
	const family = (typeFlags >> 7) & 15;
	const equipment = (typeFlags & 2) === 0 && (typeFlags & 0x1c) === 0xc && (typeFlags & 0x60) === 0x20;
	return durability === 0 && !(equipment && (family === 5 || family === 12 || family === 13 || family === 14));
}

/*
================
equipmentRefusal

Skill_ValidateEquipmentRequirements (58D480): reqi pairs alone decide when
present, else the primary weapon's TID4 against the row's two kinds; a
broken primary weapon is 0x300F whatever the kind said.
================
*/
function equipmentRefusal( admit: PressAdmit, equipped: Equipped ): number {
	if ( admit.reqi ) return reqiRefusal( admit.reqi, equipped );
	const [first, second] = admit.weaponKinds;
	if ( first === ANY_WEAPON && second === ANY_WEAPON ) return 0;
	const tid4 = weaponTid( equipped, PRIMARY_SOCKET ) >> 11 & 0x1f || BARE_HAND;
	let code = 0;
	if ( tid4 !== first && tid4 !== second ) code = WRONG_WEAPON;
	else if ( tid4 === FORTRESS_WEAPON ) code = FORTRESS_ONLY;
	if ( !socketUsable( equipped, PRIMARY_SOCKET ) ) return BROKEN;
	return code;
}

/*
================
reqiRefusal

combat.ReqiRefusal (58D4E3..58D689): without reqn a match ends the walk;
under reqn every pair must match and five matched pairs still fail.
================
*/
function reqiRefusal( reqi: NonNullable<PressAdmit["reqi"]>, equipped: Equipped ): number {
	let code = 0, matched = false, count = 0, i = 0;
	for ( ; i < reqi.pairs.length; i++ ) {
		if ( matched ) break;
		[matched, code] = testPair( reqi.pairs[i]!, code, equipped );
		if ( reqi.all ) {
			if ( !matched ) return code || WRONG_WEAPON;
			matched = false;
			count++;
		}
	}
	const stoppedEarly = i < 5;
	if ( stoppedEarly && count !== 0 && count === i ) return code;
	if ( matched ) return code;
	return code || WRONG_WEAPON;
}

/*
================
testPair

One reqi pair (testPair): the recorded code, cleared, set to 0x300F or kept.
================
*/
function testPair(
	[kind, value]: readonly [number, number],
	code: number,
	equipped: Equipped
): [boolean, number] {
	const kase = reqiCase( kind );
	if ( kase === "armour" ) {
		if ( value !== 0 ) {
			// 58D578: armour index value must hold TID4 == value.
			const socket = armourSocket( value );
			const usable = socket >= 0 && socketUsable( equipped, socket );
			return weaponTid( equipped, socket ) >> 11 === value && usable ? [ true, 0 ] : [ false, code ];
		}
		// 58D5B5: indexes 1..6 in order must carry TID3 == kind.
		let matched = false;
		for ( let index = 1; index <= 6; index++ ) {
			const socket = armourSocket( index );
			if ( (weaponTid( equipped, socket ) >> 7 & 0xf) !== kind ) return [ matched, 0 ];
			// Unreachable as on the server: weaponTid reads a broken piece as TID 0.
			if ( !socketUsable( equipped, socket ) ) return [ false, BROKEN ];
			matched = true;
		}
		return [ matched, code ];
	}
	if ( kase === "primary" || kase === "secondary" ) {
		// 58D513 / 58D542: the socket's TID4 must equal value; broken is 0x300F.
		const socket = kase === "primary" ? PRIMARY_SOCKET : SHIELD_SOCKET;
		if ( weaponTid( equipped, socket ) >> 11 !== value ) return [ false, code ];
		return socketUsable( equipped, socket ) ? [ true, 0 ] : [ false, BROKEN ];
	}
	// The avatar socket 4 does not exist in v1.150; other kinds test nothing.
	return [ false, code ];
}

/*
================
reqiCase

The byte table at 58D78C, indexed by reqi kind - 1.
================
*/
function reqiCase( kind: number ): "armour" | "secondary" | "primary" | "avatar" | "none" {
	switch ( kind ) {
		case 1:
		case 2:
		case 3:
		case 9:
		case 10:
		case 11:
			return "armour";
		case 4:
			return "secondary";
		case 6:
			return "primary";
		case 14:
			return "avatar";
	}
	return "none";
}

/*
================
armourSocket

data_C63F44: armour index 1..6 (4EADC0) to its equipment socket; -1 past it.
================
*/
function armourSocket( index: number ): number {
	switch ( index ) {
		case 1:
			return 0;
		case 2:
			return 2;
		case 3:
			return 1;
		case 4:
			return 4;
		case 5:
			return 3;
		case 6:
			return 5;
	}
	return -1;
}

/*
================
itemIn
================
*/
function itemIn( equipped: Equipped, socket: number ): EquippedItem | undefined {
	return equipped.find( item => item.slot === socket );
}

/*
================
weaponTid

4EAD40 / 4EAD80: the socket's packed TID, 0 when empty or broken.
================
*/
function weaponTid( equipped: Equipped, socket: number ): number {
	const item = socket < 0 ? undefined : itemIn( equipped, socket );
	return !item || equipmentBroken( item.typeFlags, item.durability ) ? 0 : item.typeFlags;
}

/*
================
socketUsable

4EC5A0: an empty socket passes; an equipped item must not be broken.
================
*/
function socketUsable( equipped: Equipped, socket: number ): boolean {
	const item = itemIn( equipped, socket );
	return !item || !equipmentBroken( item.typeFlags, item.durability );
}

/*
================
ammunitionReady

planEquippedAmmunition: a bow (TID4 6) wants arrows (TID 3.3.4.1), a
crossbow (12) bolts (3.3.4.2), as a stack in the shield socket; other
weapons need none.
================
*/
function ammunitionReady( equipped: Equipped ): boolean {
	const weapon = weaponTid( equipped, PRIMARY_SOCKET ) >> 11 & 0x1f;
	const wanted = weapon === 6 ? 1 : weapon === 12 ? 2 : 0;
	if ( !wanted ) return true;
	const ammo = itemIn( equipped, SHIELD_SOCKET );
	if ( !ammo || ammo.quantity < 1 ) return false;
	const t = ammo.typeFlags;
	return (t >> 2 & 7) === 3 && (t >> 5 & 3) === 3 && (t >> 7 & 0xf) === 4 && (t >> 11 & 0x1f) === wanted;
}
