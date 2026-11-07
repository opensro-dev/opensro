/*
===========================================================================
penguin-effects.ts - port-only crystal storms, shattering ice and ranged bolts.

Own meshes replace the wizard snowstorm. Cosmetics follow admitted casts;
they never delay, apply or invent damage. Clocks use presentation seconds.
===========================================================================
*/
import type { CharacterActor } from "@/engine/contracts/character";
import type { CastState } from "@/engine/contracts/gameplay";
import type { EntityState } from "@/engine/contracts/world";
import { nativeHeadingYaw, radians } from "@/engine/foundation/math/angles";
import { worldDelta } from "@/engine/foundation/world/world-space";
import { projectileSpace, sampleProjectile } from "./projectile-time";

export const PENGUIN_ICE_SHARD = "/assets/effects/penguin-ice-shard";
export const PENGUIN_ICE_LANCE = "/assets/effects/penguin-ice-lance";
export const PENGUIN_FROST_SIGIL = "/assets/effects/penguin-frost-sigil";
export const PENGUIN_BLIZZARD_SECONDS = 5;
export const PENGUIN_BLIZZARD_WIND = "/assets/effects/penguin-blizzard-wind";
export const PENGUIN_BLIZZARD_SNOW = "/assets/effects/penguin-blizzard-snow";
export const PENGUIN_BLIZZARD_ICE = "/assets/effects/penguin-blizzard-ice";
type Position = CharacterActor["pose"];

/*
================
penguinEffectActors

Four storm actors combine frost, mist ribbons, snow and crystal clusters.
The field holds for the server's five one-second damage beats.
================
*/
export function penguinEffectActors(
	action: number,
	age: number,
	origin: Position,
	gids: readonly number[]
): CharacterActor[] {
	const casting = action >= 34 && action <= 36, storm = action >= 0xd3 && action <= 0xd5;
	const life = storm ? PENGUIN_BLIZZARD_SECONDS : casting ? 1.25 : .55;
	if ( (!casting && (action < 0xd0 || action > 0xd6)) || age < 0 || age >= life ) return [];
	const radius = storm ? (action === 0xd3 ? 50 : 70) : action === 0xd6 ? 50 : 12;
	const fade = storm || casting ? Math.min( 1, age * 8 ) * Math.min( 1, (life - age) * 2 ) : 1 - age / .55;
	if ( storm || casting ) {
		const reach = casting ? 18 : radius;
		const base = {
			clip: "effect",
			time: age,
			absoluteEffectScale: true,
			effectEntity: true,
			pickable: false,
			pose: { ...origin, y: origin.y + .5 }
		};
		const bloom = Math.min( 1, age * 2.5 );
		return [
			{
				...base,
				gid: gids[0]!,
				model: PENGUIN_FROST_SIGIL,
				loop: false,
				scale: reach * (.15 + .85 * bloom),
				opacity: fade * .52,
				materialTint: [ .65, .9, 1 ]
			},
			{
				...base,
				gid: gids[1]!,
				model: PENGUIN_BLIZZARD_WIND,
				loop: true,
				scale: reach,
				opacity: fade * .48,
				materialTint: [ .6, .85, 1 ],
				effectRotation: { axis: "y", angle: radians( age * .8 ) }
			},
			{
				...base,
				gid: gids[2]!,
				model: PENGUIN_BLIZZARD_SNOW,
				loop: true,
				scale: reach,
				opacity: fade * .85,
				effectRotation: { axis: "y", angle: radians( -age * .35 ) }
			},
			...(!casting ?
				[ {
					...base,
					gid: gids[3]!,
					model: PENGUIN_BLIZZARD_ICE,
					loop: false,
					scale: reach,
					opacity: fade * .85
				} ] :
				[])
		];
	}
	const actors: CharacterActor[] = [];

	for ( let i = 0; i < (storm ? 12 : 8); i++ ) {
		const phase = (age + i / 12) % 1;
		const angle = i * Math.PI * 2 / (storm ? 12 : 8) + (storm ? age * 1.7 : age * .8);
		const reach = storm ? radius * (.25 + .65 * ((i * 7 % 12) / 11)) : radius * age / .55;
		actors.push( {
			gid: gids[i + 1]!,
			model: PENGUIN_ICE_SHARD,
			clip: "effect",
			time: age,
			loop: false,
			scale: storm ? 1.5 + i % 3 * .4 : 1.5,
			absoluteEffectScale: true,
			effectEntity: true,
			pickable: false,
			opacity: fade * (storm ? Math.sin( phase * Math.PI ) * .8 : .85),
			effectRotation: { axis: "x", angle: radians( storm ? Math.PI / 2 : -.5 ) },
			pose: {
				...origin,
				x: origin.x + Math.cos( angle ) * reach,
				y: origin.y + (storm ? 5 + 50 * (1 - phase) : 5 + Math.sin( age / .55 * Math.PI ) * 12),
				z: origin.z + Math.sin( angle ) * reach,
				yaw: radians( -angle )
			}
		} );
	}
	return actors;
}

/*
================
createPenguinBolts

One bounded bolt per admitted token, including independent echoes. Retain
completed entries until their cast retires so snapshots cannot replay them.
================
*/
export function createPenguinBolts( allocate: () => number ) {
	const rows = new Map<
		number,
		{ at: number; target: number; from: Position; to: Position; delay: number; lance: boolean; gids: number[]; }
	>();
	return {
		/*
		================
		step
		================
		*/
		step(
			casts: readonly CastState[],
			pet: number,
			entities: ReadonlyMap<number, EntityState>,
			presented: ReadonlyMap<number, CharacterActor>,
			now: number,
			queuedSkill = 0
		): CharacterActor[] {
			if ( pet === 0 ) {
				rows.clear();
				return [];
			}
			const current = new Set( casts.map( cast => cast.token ) );
			for ( const token of rows.keys() ) if ( !current.has( token ) ) rows.delete( token );
			const result: CharacterActor[] = [];
			for ( const cast of casts ) {
				// Penguin attacks have one native row per level (25214 = level 1).
				if (
					cast.caster !== pet || cast.skill < 25214 || cast.skill > 25353 || cast.cancelledAtMs !== undefined
				) continue;
				let row = rows.get( cast.token );
				if ( !row ) {
					const source = entities.get( cast.caster ), target = entities.get( cast.target );
					if (
						!source || !target || !projectileSpace( source.regionId, target.regionId ) || rows.size >= 64
					) continue;
					const from = presented.get( source.gid )?.pose ??
						{
							regionId: source.regionId,
							x: source.x,
							y: source.y,
							z: source.z,
							yaw: nativeHeadingYaw( source.heading )
						};
					const to = presented.get( target.gid )?.pose ??
						{
							regionId: target.regionId,
							x: target.x,
							y: target.y,
							z: target.z,
							yaw: nativeHeadingYaw( target.heading )
						};
					row = {
						at: now,
						target: cast.target,
						from: { ...from, y: from.y + 12 },
						to: { ...to, y: to.y + 10 },
						delay: cast.shotAtMs === undefined ? .18 : 0,
						// The training queue survives the admitted windup; latch it before release clears it.
						lance: queuedSkill >= 31 && queuedSkill <= 33 || cast.shotAtMs !== undefined &&
								[ ...rows.values() ].some( previous =>
									previous.lance && now - previous.at < 1 &&
									previous.target === cast.target
								),
						gids: Array.from( { length: 9 }, allocate )
					};
					rows.set( cast.token, row );
				}
				const age = now - row.at;
				const [dx, dz] = worldDelta( row.from, row.to );
				const distance = Math.hypot( dx, row.to.y - row.from.y, dz );
				const sample = sampleProjectile( row.from, row.to, Math.max( 1, distance / .42 ), row.delay, age );
				if ( sample.phase === "delay" ) continue;
				if ( sample.phase === "arrived" ) {
					result.push( ...penguinEffectActors( 0xd0, age - row.delay - .42, row.to, row.gids ) );
					continue;
				}
				const bolt: CharacterActor = {
					gid: row.gids[0]!,
					model: row.lance ? PENGUIN_ICE_LANCE : PENGUIN_ICE_SHARD,
					clip: "effect",
					time: age,
					loop: false,
					scale: row.lance ? 5 : 2,
					absoluteEffectScale: true,
					effectEntity: true,
					pickable: false,
					pose: { ...sample.pose, yaw: radians( Math.atan2( dx, dz ) ) }
				};
				result.push( bolt );
				if ( row.lance ) {
					for ( let i = 1; i <= 3; i++ ) {
						const trail = sampleProjectile(
							row.from,
							row.to,
							Math.max( 1, distance / .42 ),
							row.delay,
							age - i * .035
						);
						if ( trail.phase !== "travel" ) continue;
						result.push( {
							...bolt,
							gid: row.gids[i]!,
							model: PENGUIN_ICE_SHARD,
							scale: 2 - i * .35,
							opacity: .6 - i * .12,
							pose: { ...trail.pose, yaw: radians( Math.atan2( dx, dz ) ) }
						} );
					}
				}
			}
			return result;
		},
		/*
		================
		reset
		================
		*/
		reset() {
			rows.clear();
		}
	};
}

export const PENGUIN_EFFECT_SOUND: Readonly<
	Record<number, { readonly path: string; readonly at: number; readonly gain: number; readonly shake: boolean; }>
> = {
	0xd0: { path: "/assets/audio/sfx/prim/snd/skill2/usk_wizard_cold_hit_a.wav", at: 0, gain: .55, shake: false },
	0xd1: { path: "/assets/audio/sfx/prim/snd/skill2/usk_wizard_cold_hit_a.wav", at: 0, gain: .55, shake: false },
	0xd2: { path: "/assets/audio/sfx/prim/snd/skill2/usk_wizard_cold_hit_a.wav", at: 0, gain: .55, shake: false },
	0xd3: { path: "/assets/audio/sfx/prim/snd/skill2/usk_wizard_cold_area_a.wav", at: 0, gain: .55, shake: false },
	0xd4: { path: "/assets/audio/sfx/prim/snd/skill2/usk_wizard_cold_area_a.wav", at: 0, gain: .55, shake: false },
	0xd5: { path: "/assets/audio/sfx/prim/snd/skill2/usk_wizard_cold_area_a.wav", at: 0, gain: .55, shake: false },
	0xd6: { path: "/assets/audio/sfx/prim/snd/skill2/usk_wizard_cold_hit_a.wav", at: 0, gain: .55, shake: false }
} as const;
