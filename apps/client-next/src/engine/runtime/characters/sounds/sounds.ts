import type { SoundEvent } from "@/engine/contracts/audio";
import { characterSoundKeys, type CharacterSoundContext } from "@/engine/foundation/animation/sound-selectors";
import type { AnimationActivation } from "@/engine/foundation/animation/animation-activation";
export interface SoundRule {
	readonly object: string;
	readonly handle: string;
	readonly event1: string;
	readonly skillId?: string;
	readonly event2?: string;
	readonly event3?: string;
	readonly publicPath?: string;
	readonly volume?: number;
}
export interface AnimationSounds {
	readonly durationMs: number;
	readonly soundEvents: readonly {
		readonly cursorMs: number;
		readonly cue: string;
	}[];
}
export function createCharacterSounds(
	play: ( event: SoundEvent ) => void,
	choose: ( min: number, max: number ) => number = () => 0
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
	const impacts = new Map<
		string,
		{ gid: number; profile: string; cues: readonly string[]; context: CharacterSoundContext; at: number; }
	>();
	function emit(
		id: string,
		profile: string,
		cues: readonly string[],
		context: CharacterSoundContext,
		position: readonly [number, number, number],
		now: number,
		surface?: string
	): boolean {
		for ( const cue of cues ) {
			const matches = characterSoundKeys( profile, cue, context, surface ).map( key =>
				rules.get( key )
			).find( rows => rows?.length ) ?? [];
			if ( !matches.length ) {
				continue;
			}
			const rule = matches[matches.length === 1 ? 0 : choose( 0, matches.length )]!;
			if ( !rule.publicPath ) {
				return false;
			}
			const nonPositional = cue === "SND_PICKUP"; // 8F994A passes null position.
			play( {
				id,
				path: rule.publicPath,
				gain: nonPositional ? 1 : (rule.volume ?? 100) / 100,
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
					(rule.volume !== undefined && !Number.isFinite( rule.volume ))
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
		},
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
		retainActivations( gid: number, active: ReadonlySet<AnimationActivation> ) {
			const lanes = cursors.get( gid );
			if ( !lanes ) return;
			for ( const key of lanes.keys() ) if ( typeof key !== "string" && !active.has( key ) ) lanes.delete( key );
		},
		retain( gids: ReadonlySet<number> ) {
			for ( const gid of cursors.keys() ) {
				if ( !gids.has( gid ) ) {
					cursors.delete( gid );
				}
			}
			for ( const [key, row] of impacts ) if ( !gids.has( row.gid ) ) impacts.delete( key );
		},
		reset() {
			cursors.clear();
			impacts.clear();
			serial = 0;
		}
	};
}
