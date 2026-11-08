/*
===========================================================================
survival-effects.ts - port-only fixed-origin arena blast and burning ground.

Cosmetic actors only. The server owns radius, damage and expiry.
===========================================================================
*/
import type { CharacterActor } from "@/engine/contracts/character";
import { radians } from "@/engine/foundation/math/angles";

/*
================
survivalEffectActors - transparent flame tongues and a circular ember boundary.
================
*/
export function survivalEffectActors(
	burst: { cue: number; gids: readonly number[]; pose: CharacterActor["pose"]; },
	age: number
): CharacterActor[] {
	const pit = burst.cue === 10, duration = pit ? 60 : 1.2;
	if ( age < 0 || age >= duration ) return [];
	const actors: CharacterActor[] = [];
	if ( !pit ) {
		// Reuse the bear roar's authored aura and two eased shockwave crests.
		for ( let i = 0; i < 3; i++ ) {
			const aura = i === 0, start = i === 2 ? .08 : 0, life = aura ? .4 : .95;
			const u = (age - start) / life;
			if ( u < 0 || u >= 1 ) continue;
			actors.push( {
				gid: burst.gids[i]!,
				model: aura ? "/assets/effects/challenging-roar-aura" : "/assets/effects/challenging-roar-shockwave",
				clip: "effect",
				time: age,
				loop: false,
				scale: aura ? 1.45 + .85 * u : 6 + 144 * (1 - Math.pow( 1 - u, 2.3 )),
				absoluteEffectScale: true,
				effectEntity: true,
				pickable: false,
				opacity: (aura ? .12 : i === 1 ? .45 : .3) * Math.pow( 1 - u, 1.6 ),
				pose: {
					...burst.pose,
					y: burst.pose.y + (i === 2 ? .6 : 0),
					yaw: radians( age * (i === 2 ? -1.8 : 2.2) )
				}
			} );
		}
		return actors;
	}
	const fade = Math.min( 1, age * 3, (duration - age) / 2 );
	const radius = 300 * Math.min( 1, age );
	for ( let i = 0; i < 20; i++ ) {
		const cracks = i === 18, ring = i === 19, ground = cracks || ring;
		const phase = (age * .65 + i * .61803399) % 1;
		const angle = i * 2.39996323;
		const distance = radius * Math.sqrt( (i + .5) / 18 );
		actors.push( {
			gid: burst.gids[i]!,
			model: ring ?
				"/assets/effects/survival-ember-ring" :
				cracks ?
				"/assets/effects/survival-cracks" :
				"/assets/effects/survival-flame",
			clip: "effect",
			time: age,
			loop: pit,
			scale: ground ? radius : 1.1 + .7 * Math.sin( phase * Math.PI ),
			absoluteEffectScale: true,
			effectEntity: true,
			pickable: false,
			opacity: fade * (ring ? .6 : cracks ?
				.5 + .06 * Math.sin( age * 2.5 ) :
				Math.min( 1, age / 5 ) * (.45 + .2 * Math.sin( phase * Math.PI ))),
			pose: {
				...burst.pose,
				x: burst.pose.x + (ground ? 0 : Math.cos( angle ) * distance),
				z: burst.pose.z + (ground ? 0 : Math.sin( angle ) * distance),
				y: burst.pose.y + (ground ? .7 : .1 + phase * .3),
				yaw: radians( ground ? 0 : angle + age * .15 )
			}
		} );
	}
	return actors;
}
