/*
===========================================================================

actor-presentation.ts - each selected entity becomes the actor the renderer draws

The per-actor phase of the character presentation frame: skill objects and
ground items, then for each character its resource, worn appearance and
dress, and the animation layers of death, posture, actions, locomotion, hits
and idles, with their sounds. It writes every actor into frame.next and
returns the particle and animation-emission holders the frame finishes with.

It owns no state that outlives a frame. characters.ts owns the maps and
services in ActorOwner and calls present once per frame, between the
presentation-state phase and finalization.

===========================================================================
*/
import type { AnimationParticleSet } from "@/engine/foundation/animation/animation-emission";
import type { ModelParticle } from "@/engine/foundation/animation/model-particles";
import type { CharacterActor } from "@/engine/contracts/character";
import { createActorAppearance } from "./actor-appearance";
import { createActorMotion } from "./actor-motion";
import type { ActorFrame, ActorOwner, ActorPass } from "./internal/presentation-contract";
import type { EntityState } from "@/engine/contracts/world";

/*
================
undroppedLoot

The selected entities without the drops still waiting on their monster's
death. The server publishes a victim's drops at its death transition
(CGObjMob_CreditKillerOnDeath 4C42F0), but the client holds that death until
the killing hit plays; the drops wait with it, unseen and unpickable, so the
item lands when the monster falls instead of before the last blow.
================
*/
export function undroppedLoot( selected: readonly EntityState[], pendingDeaths: ReadonlySet<number> ) {
	if ( !pendingDeaths.size ) return selected;
	return selected.filter( entity => !pendingDeaths.has( entity.groundItem?.dropperGid ?? 0 ) );
}

/*
================
createActorPresentation
================
*/
export function createActorPresentation( owner: ActorOwner ) {
	const actorAppearance = createActorAppearance( owner ), actorMotion = createActorMotion( owner );
	return {
		/*
		================
		present

		Runs each selected entity through appearance.resolve, then motion.present,
		under one per-entity catch that reports the failure and moves on.
		================
		*/
		present( frame: ActorFrame ) {
			const {
				appearances,
				output
			} = owner;
			const {
				active,
				entities,
				gameplay,
				pendingDeaths
			} = frame;
			const selected = undroppedLoot( frame.selected, pendingDeaths );
			const appearanceActive = new Set( selected.map( entity => entity.gid ) );
			for ( const gid of appearances.keys() ) if ( !appearanceActive.has( gid ) ) appearances.delete( gid );
			const animationHolders: { actor: CharacterActor; sets: readonly AnimationParticleSet[]; }[] = [];
			const particleHolders: { actor: CharacterActor; particles: readonly ModelParticle[]; }[] = [];
			// Fortress clothing compares every player with the local one; find it
			// once per frame, not once per actor (a linear scan each, so O(n^2).
			const localEntity = entities.find( e => e.gid === gameplay?.localGid );
			const pass: ActorPass = { animationHolders, particleHolders, localEntity };
			for ( const entity of selected ) {
				active.add( entity.gid );
				try {
					const appearance = actorAppearance.resolve( entity, frame, pass );
					if ( !appearance ) continue;
					actorMotion.present( entity, appearance, frame, pass );
				} catch ( error ) {
					output.failure = String( error );
				}
			}
			return { animationHolders, particleHolders };
		}
	};
}
