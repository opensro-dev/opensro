/*
===========================================================================

orbs.ts - berserk (Hwan) orb flight and indraft absorption presentation

Owns the client presentation of berserk energy orbs (blue, yellow/red, and
violet): queues wire orb-feedback bursts until the source entity's skeleton is
settled, advances active orb trajectories toward the recipient's Spine1 bone,
plays the arrival sound, and advances the displayed berserk gauge as violet
orbs arrive.

===========================================================================
*/

import type { SoundEvent } from "@/engine/contracts/audio";
import type { CharacterActor } from "@/engine/contracts/character";
import type { BerserkGauge, OrbFeedback } from "@/engine/contracts/orb";
import type { PresentationRandom } from "@/engine/contracts/presentation-random";
import type { EntityState } from "@/engine/contracts/world";
import { advanceOrb, type OrbMover, type OrbPoint } from "@/engine/foundation/animation/orb-mover";
import { createPresentationIds } from "@/engine/foundation/animation/presentation-ids";
import { radians } from "@/engine/foundation/math/angles";
import { worldAudioPosition } from "@/engine/foundation/world/world-space";

/*
================
createOrbs
================
*/
export function createOrbs(
	play: ( event: SoundEvent ) => void,
	random: PresentationRandom,
	allocate: () => number = createPresentationIds()
) {
	const launch = [ "hwan_g", "hwan_y", "hwan_v", "hwan_g" ],
		indraft = [ "hwn_blue_indraft", "hwn_red_indraft", "hwn_violet_indraft", "hwn_blue_indraft" ];
	const model = ( name: string ) =>
		"/assets/effects/programs.json#" + encodeURIComponent( "battle/" + name + ".efp" );
	let authoritative = 0, displayed = 0, waiting = 0;

	/*
	================
	consume
	================
	*/
	function consume( count: number ) {
		if ( count <= 0 ) return;
		waiting = Math.max( 0, waiting - count );
		displayed = waiting ? Math.min( authoritative, 5, displayed + count ) : authoritative;
	}

	/*
	================
	clearViolet
	================
	*/
	function clearViolet() {
		waiting = 0;
		displayed = authoritative;
		for ( const [id, orb] of active ) {
			if ( orb.color === 2 ) active.delete( id );
		}
		for ( const row of pending.values() ) {
			row.delete( 2 );
		}
	}

	let last: number | null = null;
	const pending = new Map<number, Map<number, { count: number; target: number; }>>(),
		active = new Map<
			number,
			{ color: number; target: number; mover: OrbMover; started: number | null; arrival: number | null; }
		>();

	return {
		/*
		================
		receive
		================
		*/
		receive( events: readonly OrbFeedback[], entities: readonly EntityState[] ) {
			const live = new Map( entities.map( e => [ e.gid, e ] ) );
			for ( const event of events ) {
				if ( event.kind === "orb-gauge" ) {
					authoritative = event.value;
					continue;
				}
				if ( event.kind === "orb-clear" ) {
					clearViolet();
					continue;
				}
				const source = live.get( event.source );
				if ( !source || ![ "npc", "monster", "player", "local-player", "cos" ].includes( source.kind ) ) {
					if ( event.color === 2 ) clearViolet();
					continue;
				}
				if ( event.color === 2 ) waiting = (waiting + event.count) & 255;
				let row = pending.get( event.source );
				if ( !row ) {
					row = new Map();
					pending.set( event.source, row );
				}
				row.set( event.color, {
					count: ((row.get( event.color )?.count ?? 0) + event.count) & 255,
					target: event.target
				} );
			}
		},
		/*
		================
		step
		================
		*/
		step(
			entities: readonly EntityState[],
			now: number,
			settled: ReadonlySet<number>,
			socket: ( gid: number, bone: string ) => CharacterActor["pose"] | null,
			ready: ( path: string ) => boolean,
			duration: ( path: string, clip: string ) => number,
			detail = 3,
			failed: ( path: string ) => boolean = () => false
		) {
			const tick = Math.trunc( now * 1000 ), delta = last === null ? 0 : Math.max( 0, tick - last );
			last = tick;
			const live = new Map( entities.map( e => [ e.gid, e ] ) );

			/*
			================
			anchor
			================
			*/
			function anchor( gid: number, launching = false ): OrbPoint | null {
				const e = live.get( gid );
				if ( !e ) return null;
				const p = socket( gid, launching ? "Bip01" : "Bip01 Spine1" ) ??
					{ ...e, y: e.y + (launching ? 0 : 10) };
				return worldAudioPosition( p );
			}

			for ( const [gid, rows] of pending ) {
				const source = live.get( gid );
				if ( !source ) {
					consume( rows.get( 2 )?.count ?? 0 );
					pending.delete( gid );
					continue;
				}
				if ( source.kind !== "npc" && !settled.has( gid ) ) continue;
				const start = anchor( gid, true );
				if ( detail <= 1 ) consume( rows.get( 2 )?.count ?? 0 );
				if ( start && detail > 1 ) {
					for ( const [color, row] of rows ) {
						const target = anchor( row.target );
						if ( !target ) {
							if ( color === 2 ) consume( row.count );
							continue;
						}
						for ( let i = 0; i < row.count; i++ ) {
							if ( active.size >= 2048 ) throw Error( "Orb presentation capacity exceeded" );
							const mover = random.orb( start, target );
							active.set( allocate(), {
								color,
								target: row.target,
								mover,
								started: null,
								arrival: null
							} );
						}
					}
				}
				pending.delete( gid );
			}
			const actors: CharacterActor[] = [];
			for ( const [id, orb] of active ) {
				if ( orb.started === null ) {
					// Admit flight and absorption together: neither clock may run invisibly
					// while the worker is still compiling their particle programs.
					const flight = model( launch[orb.color]! ), arrival = model( indraft[orb.color]! );
					const flightReady = ready( flight ), arrivalReady = ready( arrival );
					if ( failed( flight ) || failed( arrival ) ) {
						if ( orb.color === 2 ) consume( 1 );
						active.delete( id );
						continue;
					}
					if ( !flightReady || !arrivalReady ) continue;
					orb.started = now;
				}
				if ( orb.arrival === null ) {
					const target = anchor( orb.target );
					if ( target ) orb.mover.target = target;
					if ( !advanceOrb( orb.mover, orb.started === now ? 0 : delta ) ) {
						orb.arrival = now;
						if ( orb.color === 2 ) consume( 1 );
						const [x, y, z] = orb.mover.position;
						play( {
							id: "orb:" + id,
							path: "/assets/audio/sfx/prim/snd/ui/hyanget.wav",
							gain: 1,
							spatial: true,
							x,
							y,
							z,
							expires: now + 0.5
						} );
					}
				}
				const path = model( (orb.arrival === null ? launch : indraft)[orb.color]! ),
					time = now - (orb.arrival ?? orb.started),
					available = ready( path );
				if (
					orb.arrival !== null &&
					(available && time >= duration( path, "effect" ) || !available && failed( path ))
				) {
					active.delete( id );
					continue;
				}
				if ( !available ) continue;
				const [x, y, z] = orb.mover.position;
				actors.push( {
					gid: id,
					pickable: false,
					model: path,
					pose: { regionId: 0, x, y, z, yaw: radians( 0 ) },
					clip: "effect",
					time,
					loop: orb.arrival === null,
					scale: 1
				} );
			}
			return actors;
		},
		/*
		================
		gauge
		================
		*/
		gauge: (): BerserkGauge => ({ authoritative, displayed, pending: waiting }),
		/*
		================
		reset
		================
		*/
		reset() {
			authoritative = displayed = waiting = 0;
			pending.clear();
			active.clear();
			last = null;
		}
	};
}
