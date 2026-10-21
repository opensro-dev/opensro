/*
===========================================================================

wolf-effects.ts - port-only attack-pet wolf skill visuals

Which retail effect programs each wolf signal plays, when and where. The
server sends the signal as a COS emote (0x324B) on the object the effect
belongs to (petwolfvisual.go); effects.ts draws these parts. The wolf has
its own look: beast howls, wolf strikes, blood and war cries, never the
bear's roar aura or shockwave.

===========================================================================
*/
import { PET_EXECUTE_SOUND, PET_HOWL_SOUND, PET_HUNT_SOUND, PET_REND_SOUND, PET_SWEEP_SOUND } from "./sound-selectors";

// Signals (emote actions): skill IDs for the two actives, 0xF0+ for passives.
export const WOLF_HOWL = [ 1, 2, 3 ] as const;
export const WOLF_REND = [ 4, 5, 6 ] as const;
export const WOLF_EXECUTED = 0xf0;
export const WOLF_SWEEP = 0xf1;
export const WOLF_HUNT = 0xf2;
export const WOLF_HUNT_BURST = 0xf3;
export const WOLF_BLOOD_FULL = 0xf4;

// No wolf signal draws longer than this.
export const WOLF_EFFECT_SECONDS = 4;

export type WolfEffectPart = {
	readonly path: string;
	// Seconds after the signal.
	readonly at: number;
	// self: the signalled object where it was; front: before the wolf's jaws;
	// owner: the wolf's owner; follow: the signalled object as it moves.
	readonly anchor: "self" | "front" | "owner" | "follow";
	readonly scale?: number;
	readonly lift?: number;
	// World x/z offset from the anchor (game units, 10 per metre).
	readonly offset?: readonly [number, number];
};

// The most parts one signal draws (effects.ts reserves one actor per part).
export const WOLF_EFFECT_MAX_PARTS = 17;

/*
================
bloodWave

Execute III's sweep: eruptions racing out in two rings, 5 m then 10 m.
================
*/
function bloodWave(): WolfEffectPart[] {
	const parts: WolfEffectPart[] = [ { path: BLOOD_DEATH, at: 0, anchor: "self", scale: 1.5 } ];
	for ( const [radius, at] of [ [ 50, .08 ], [ 100, .2 ] ] as const ) {
		for ( let i = 0; i < 8; i++ ) {
			const angle = (i + (radius > 50 ? .5 : 0)) * Math.PI / 4;
			parts.push( {
				path: BLOOD_FLOWER,
				at: at + i * .01,
				anchor: "self",
				offset: [ Math.sin( angle ) * radius, Math.cos( angle ) * radius ]
			} );
		}
	}
	return parts;
}

const BEAST_HOWL = "monster/skill_whitetiger_howling.efp",
	WAR_CRY = "skill/europe/warrior_frenzy_warcry_a.efp",
	WOLF_STRIKE = "monster/eu/skill/skill_baruswolf_ac3_push.efp",
	BLOOD_SPRAY = "hiteffect/hit_2_redblood.efp",
	SLASH = "battle/deco_wind_weapon_slash_a.efp",
	BLOOD_DEATH = "monster/dead/dead_redblood.efp",
	BLOOD_FLOWER = "skill/europe/warlock_blood_flower_hit_a.efp",
	BLOOD_FLOWER_SMALL = "skill/europe/warlock_blood_flower_hit_b.efp",
	BLOOD_TOUCH = "skill/europe/warlock_blood_touch_hit_a.efp",
	BLOOD_SPIRAL = "skill/europe/warlock_blood_touch_a.efp",
	BLOOD_POOL = "skill/europe/warlock_blood_flower_b.efp";

/*
================
wolfEffectParts

The parts one signal plays; [] for any other action.
================
*/
export function wolfEffectParts( action: number ): readonly WolfEffectPart[] {
	if ( action >= 1 && action <= 3 ) {
		// The howl's peak (pet-howl muzzle up at 0.55 s); II and III rouse the owner.
		return [
			{ path: BEAST_HOWL, at: .45, anchor: "self", lift: 8 },
			...(action >= 2 ? [ { path: WAR_CRY, at: .65, anchor: "owner" } as const ] : [])
		];
	}
	if ( action >= 4 && action <= 6 ) {
		// The jaws close at 45 % of attack1 (pet-rend); II and III cleave.
		return [
			{ path: WOLF_STRIKE, at: .6, anchor: "front", scale: 1.3 },
			{ path: BLOOD_SPRAY, at: .7, anchor: "front", lift: 6, scale: 1.6 },
			...(action >= 5 ?
				[ { path: SLASH, at: .66, anchor: "front", lift: 6, scale: action === 6 ? 1.8 : 1.3 } as const ] :
				[])
		];
	}
	switch ( action ) {
		case WOLF_EXECUTED:
			return [
				{ path: BLOOD_DEATH, at: 0, anchor: "self" },
				{ path: BLOOD_FLOWER, at: .05, anchor: "self", lift: 6 }
			];
		case WOLF_SWEEP:
			// Ten metres around the wolf or owner that triggered it.
			return bloodWave();
		case WOLF_HUNT:
			return [
				{ path: BLOOD_TOUCH, at: 0, anchor: "self", lift: 6 },
				{ path: BLOOD_SPRAY, at: .05, anchor: "self", lift: 8, scale: 1.4 }
			];
		case WOLF_HUNT_BURST:
			return [ { path: BLOOD_FLOWER_SMALL, at: 0, anchor: "self", lift: 6 } ];
		case WOLF_BLOOD_FULL:
			// A crimson spiral wraps the runner and blood pools at its feet.
			return [
				{ path: BLOOD_SPIRAL, at: 0, anchor: "follow", lift: 10, scale: 1.8 },
				{ path: BLOOD_POOL, at: .05, anchor: "follow" }
			];
		default:
			return [];
	}
}

/*
================
wolfEffectKind

howl: the wolf's slow buildup; victim: drawn where the monster was struck;
strike: a flash at the moment of the hit.
================
*/
export function wolfEffectKind( action: number ): "howl" | "victim" | "strike" {
	if ( action >= 1 && action <= 3 ) return "howl";
	return action === WOLF_EXECUTED || action === WOLF_HUNT || action === WOLF_HUNT_BURST ? "victim" : "strike";
}

// The sound of each signal, when it plays, and whether the camera shakes.
export const WOLF_EFFECT_SOUND: Readonly<
	Record<number, { readonly path: string; readonly at: number; readonly gain: number; readonly shake: boolean; }>
> = {
	1: { path: PET_HOWL_SOUND, at: .55, gain: .8, shake: false },
	2: { path: PET_HOWL_SOUND, at: .55, gain: .8, shake: false },
	3: { path: PET_HOWL_SOUND, at: .55, gain: .8, shake: true },
	4: { path: PET_REND_SOUND, at: .7, gain: .55, shake: false },
	5: { path: PET_REND_SOUND, at: .7, gain: .55, shake: false },
	6: { path: PET_REND_SOUND, at: .7, gain: .6, shake: true },
	[WOLF_EXECUTED]: { path: PET_EXECUTE_SOUND, at: 0, gain: .55, shake: false },
	[WOLF_SWEEP]: { path: PET_SWEEP_SOUND, at: 0, gain: .7, shake: true },
	[WOLF_HUNT]: { path: PET_HUNT_SOUND, at: 0, gain: .6, shake: false }
};
