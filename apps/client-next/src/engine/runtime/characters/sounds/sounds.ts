/*
===========================================================================

sounds.ts - character sound cues: authored rules, impact gating, clip cursors

Owns the effectsound rule table (keyed object:handle:skill:event1:2:3) and
plays at most one rule per cue. Each rule keeps the native skip countdown:
a rule with skip N plays once every N + 1 triggers, counted across every
actor that uses it (a cat's looping stand meows once in 24 loops). Impact sounds wait out the native 100 ms
gate before they play; animation sound events are cursored per clip so a
stalled frame never replays a burst of old sounds.

===========================================================================
*/
import type { SoundEvent } from "@/engine/contracts/audio";
import { characterSoundKeys, type CharacterSoundContext } from "@/engine/foundation/animation/sound-selectors";
import type { AnimationActivation } from "@/engine/foundation/animation/animation-activation";
import type { SoundRule } from "../internal/presentation-contract";

// 8FB490 reads the skip count as an int16.
const MAX_SKIP = 0x7fff;
// 8F9280 drops a positional sound beyond 600 units of the listener (it
// compares the squared distance with 360000) before it counts the trigger.
const MAX_RULE_DISTANCE_SQ = 360000;
// 8FB490 clamps the volume column to 0..100 before dividing by 100.
const MAX_RULE_VOLUME = 100;
/*
================
AnimationSounds

A clip's authored sound events, by cursor time within one cycle.
================
*/
export interface AnimationSounds {
	readonly durationMs: number;
	readonly soundEvents: readonly {
		readonly cursorMs: number;
		readonly cue: string;
	}[];
}
/*
================
createCharacterSounds
================
*/
export function createCharacterSounds(
	play: ( event: SoundEvent ) => void,
	choose: ( min: number, max: number ) => number = () => 0,
	listener: () => readonly [number, number, number] | null | undefined = () => null
) {
	let serial = 0;
	const cursors = new Map<
		number,
		Map<string | AnimationActivation, {
			id: number;
			clip: string;
			started: number;
			time: number;
		}>
	>();
	let rules = new Map<string, SoundRule[]>();
	// 8F9280: each rule's countdown (+2), reset to its skip count (+0) on a
	// play. A rule starts at 0, so its first trigger plays.
	let countdowns = new Map<SoundRule, number>();
	const impacts = new Map<
		string,
		{ gid: number; profile: string; cues: readonly string[]; context: CharacterSoundContext; at: number; }
	>();
	/*
	================
	emit

	Play the first cue that has a matching rule; several matches pick one at
	random. A matching rule without a published sound plays nothing.
	================
	*/
	function emit(
		id: string,
		profile: string,
		cues: readonly string[],
		context: CharacterSoundContext,
		position: readonly [number, number, number],
		now: number,
		surface?: string
	): boolean {
		const at = listener();
		for ( const cue of cues ) {
			const nonPositional = cue === "SND_PICKUP"; // 8F994A passes null position.
			if ( !nonPositional && at ) {
				const dx = position[0] - at[0], dy = position[1] - at[1], dz = position[2] - at[2];
				// Native returns "handled" here: the trigger never reaches a countdown.
				if ( dx * dx + dy * dy + dz * dz > MAX_RULE_DISTANCE_SQ ) return true;
			}
			const matches = characterSoundKeys( profile, cue, context, surface ).map( key =>
				rules.get( key )
			).find( rows => rows?.length ) ?? [];
			if ( !matches.length ) {
				continue;
			}
			const rule = matches[matches.length === 1 ? 0 : choose( 0, matches.length )]!;
			// 8F9280 decrements before it plays: while the countdown stays at or
			// above zero the trigger is swallowed, otherwise it resets and plays.
			const left = (countdowns.get( rule ) ?? 0) - 1;
			if ( left >= 0 ) {
				countdowns.set( rule, left );
				return true;
			}
			countdowns.set( rule, rule.skip ?? 0 );
			if ( !rule.publicPath ) {
				return false;
			}
			play( {
				id,
				path: rule.publicPath,
				gain: nonPositional ?
					1 :
					Math.min( MAX_RULE_VOLUME, Math.max( 0, rule.volume ?? MAX_RULE_VOLUME ) ) / MAX_RULE_VOLUME,
				x: position[0],
				y: position[1],
				z: position[2],
				expires: now + .25,
				...(nonPositional ? { spatial: false } : {})
			} );
			return true;
		}
		return false;
	}
	return {
		emit,
		/*
		================
		impact

		Queue an impact sound; flush plays it once the native gate has passed.
		================
		*/
		impact(
			id: string,
			gid: number,
			profile: string,
			cues: readonly string[],
			context: CharacterSoundContext,
			at: number
		) {
			if ( impacts.size >= 2048 ) throw Error( "Impact sound capacity exceeded" );
			impacts.set( id, { gid, profile, cues, context, at } );
		},
		/*
		================
		flush
		================
		*/
		flush( now: number, position: ( gid: number ) => readonly [number, number, number] | undefined ) {
			for ( const [id, row] of impacts ) {
				// 8D59C0 uses a strict >100 ms gate. Never replay expired
				// combat audio after a suspended frame or a removed source.
				const age = Math.trunc( now * 1000 ) - Math.trunc( row.at * 1000 );
				if ( age <= 100 ) continue;
				impacts.delete( id );
				const point = position( row.gid );
				if ( point && age <= 350 ) emit( id, row.profile, row.cues, row.context, point, now );
			}
		},
		/*
		================
		catalog

		Replace the whole rule table at once; an invalid row rejects the manifest.
		================
		*/
		catalog( value: readonly SoundRule[] ) {
			const replacement = new Map<string, SoundRule[]>();
			if ( !Array.isArray( value ) ) throw new Error( "Invalid sound catalog" );
			for ( const candidate of value ) {
				const rule: SoundRule = candidate;
				if (
					!rule || typeof rule.object !== "string" || typeof rule.handle !== "string" ||
					typeof rule.event1 !== "string" ||
					(rule.publicPath !== undefined &&
						(typeof rule.publicPath !== "string" || !rule.publicPath.startsWith( "/assets/audio/" ) ||
							rule.publicPath.includes( ".." ))) ||
					(rule.volume !== undefined && !Number.isFinite( rule.volume )) ||
					(rule.skip !== undefined &&
						(!Number.isInteger( rule.skip ) || rule.skip < 0 || rule.skip > MAX_SKIP))
				) throw new Error( "Invalid sound rule" );
				if (
					[ rule.skillId, rule.event2, rule.event3 ].some( value =>
						value !== undefined && typeof value !== "string"
					)
				) throw new Error( "Invalid sound selector" );
				const key = [
					rule.object,
					rule.handle,
					rule.skillId ?? "-",
					rule.event1,
					rule.event2 ?? "-",
					rule.event3 ?? "-"
				].join( ":" );
				let list = replacement.get( key );
				if ( !list ) {
					list = [];
					replacement.set( key, list );
				}
				list.push( rule );
			}
			rules = replacement;
			countdowns = new Map();
		},
		/*
		================
		advance

		Play the sound events a clip crossed since this lane's last cursor.
		================
		*/
		advance(
			gid: number,
			clip: string,
			started: number,
			time: number,
			loop: boolean,
			definition: AnimationSounds | undefined,
			now: number,
			source: () => {
				profile: string;
				position: readonly [number, number, number];
				surface?: string;
				context: CharacterSoundContext;
			},
			lane: string | AnimationActivation = "pose"
		) {
			let lanes = cursors.get( gid );
			if ( !lanes ) {
				lanes = new Map();
				cursors.set( gid, lanes );
			}
			// AE0280 calls A67D00 (lower_bound) at both endpoints: [old,new).
			// A newly constructed native cursor is zero, never a negative sentinel.
			time = Math.max( 0, Math.trunc( time * 1000 ) );
			const old = lanes.get( lane ), previous = old?.clip === clip && old.started === started ? old.time : 0;
			if ( old ) {
				old.clip = clip;
				old.started = started;
				old.time = time;
			} else lanes.set( lane, { id: ++serial, clip, started, time } );
			if ( !definition || !definition.durationMs || time <= previous ) {
				return;
			}
			const duration = definition.durationMs,
				first = loop ? Math.max( 0, Math.floor( previous / duration ) ) : 0,
				last = loop ? Math.floor( time / duration ) : 0;
			let resolved: ReturnType<typeof source> | undefined;
			// Expired sounds do not burst after tab suspension. Only the newest cycle is audible.
			for ( let cycle = Math.max( first, last - 1 ); cycle <= last; cycle++ ) {
				for ( let index = 0; index < definition.soundEvents.length; index++ ) {
					const event = definition.soundEvents[index]!;
					if ( event.cursorMs >= definition.durationMs ) {
						continue;
					}
					const at = cycle * duration + event.cursorMs;
					if ( at < previous || at >= time || time - at > 250 ) {
						continue;
					}
					resolved ??= source();
					emit(
						`${gid}:${
							typeof lane === "string" ? lane : lanes.get( lane )!.id
						}:${started}:${clip}:${cycle}:${index}`,
						resolved.profile,
						[ event.cue.toUpperCase() ],
						resolved.context,
						resolved.position,
						now,
						resolved.surface
					);
				}
			}
		},
		/*
		================
		retainActivations

		Drop the cursors of an actor's activations that are no longer playing.
		================
		*/
		retainActivations( gid: number, active: ReadonlySet<AnimationActivation> ) {
			const lanes = cursors.get( gid );
			if ( !lanes ) return;
			for ( const key of lanes.keys() ) if ( typeof key !== "string" && !active.has( key ) ) lanes.delete( key );
		},
		/*
		================
		retain

		Forget cursors and queued impacts of actors no longer presented.
		================
		*/
		retain( gids: ReadonlySet<number> ) {
			for ( const gid of cursors.keys() ) {
				if ( !gids.has( gid ) ) {
					cursors.delete( gid );
				}
			}
			for ( const [key, row] of impacts ) if ( !gids.has( row.gid ) ) impacts.delete( key );
		},
		/*
		================
		reset

		The rule table survives: it belongs to the admitted manifest, not the world.
		================
		*/
		reset() {
			cursors.clear();
			impacts.clear();
			serial = 0;
		}
	};
}
