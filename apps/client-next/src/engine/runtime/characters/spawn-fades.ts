/*
===========================================================================

spawn-fades.ts - the CIDecoAppear spawn ramps of drawn characters

Every spawned player, monster and COS fades in over its first drawable
frames (spawn-fade.ts holds the native curve). This owner keeps the armed
ramps between frames: one per spawned entity, plus one for a monster's
linked ride, which natively gets its own CIDecoAppear (861EE2).

The ride gids belong to the auxiliary presentation; this owner only reads
them. characters.ts calls apply from finalization, at the point where the
other owners have already chosen each actor's opacity.

===========================================================================
*/
import { spawnFadeAlpha, spawnFadeKind } from "@/engine/foundation/animation/spawn-fade";
import type { CharacterActor } from "@/engine/contracts/character";
import type { EntityState } from "@/engine/contracts/world";

/*
================
fadeActor

Advances one armed ramp, keyed by its spawned entity, onto one drawn actor:
the clock starts on the actor's first drawable frame and retires at 1.
================
*/
function fadeActor(
	ramps: Map<number, number | null>,
	key: number,
	gid: number,
	next: Map<number, CharacterActor>,
	seconds: number
) {
	const start = ramps.get( key ), actor = next.get( gid );
	if ( start === undefined || !actor ) return;
	if ( start === null ) ramps.set( key, seconds );
	const alpha = spawnFadeAlpha( seconds - (start ?? seconds) );
	if ( alpha >= 1 ) {
		ramps.delete( key );
		return;
	}
	next.set( gid, { ...actor, opacity: (actor.opacity ?? 1) * alpha } );
}

/*
================
createSpawnFades
================
*/
export function createSpawnFades( linkedRides: ReadonlyMap<number, number> ) {
	// CIDecoAppear (spawn-fade.ts): each spawned character's ramp start, null
	// while armed and waiting for its first drawable frame. fadeSeen holds the
	// gids already armed, so one present the whole time fades only once.
	const spawnFades = new Map<number, number | null>(), fadeSeen = new Set<number>(), fadePresent = new Set<number>();
	// The linked ride's own ramp, keyed by its rider's entity gid.
	const rideFades = new Map<number, number | null>();
	return {
		/*
		================
		apply

		CIDecoAppear for every spawned player, monster and COS (spawn-fade.ts),
		scaling whatever opacity the other owners already chose. The ramp starts
		on the actor's first drawable frame: natively the model exists at spawn,
		here it may still be loading, and a ramp spent on an unloaded model would
		pop in. A monster's linked ride carries its own equal ramp (861EE2).
		================
		*/
		apply( entities: readonly EntityState[], next: Map<number, CharacterActor>, seconds: number ) {
			fadePresent.clear();
			for ( const entity of entities ) {
				fadePresent.add( entity.gid );
				if ( !spawnFadeKind( entity.kind ) ) continue;
				if ( !fadeSeen.has( entity.gid ) ) {
					fadeSeen.add( entity.gid );
					spawnFades.set( entity.gid, null );
					rideFades.set( entity.gid, null );
				}
				fadeActor( spawnFades, entity.gid, entity.gid, next, seconds );
				// 861EE2 gives the linked ride its own CIDecoAppear: its ramp starts
				// when the ride itself can draw, which may be after the rider's ends.
				const rideGid = linkedRides.get( entity.gid );
				if ( rideGid !== undefined ) fadeActor( rideFades, entity.gid, rideGid, next, seconds );
			}
			for ( const gid of fadeSeen ) {
				if ( fadePresent.has( gid ) ) continue;
				fadeSeen.delete( gid );
				spawnFades.delete( gid );
				rideFades.delete( gid );
			}
		},
		/*
		================
		respawn

		A respawn under a live gid is a new CICharactor: fade it again.
		================
		*/
		respawn( gid: number ) {
			fadeSeen.delete( gid );
		},
		/*
		================
		reset
		================
		*/
		reset() {
			spawnFades.clear();
			rideFades.clear();
			fadeSeen.clear();
		}
	};
}
