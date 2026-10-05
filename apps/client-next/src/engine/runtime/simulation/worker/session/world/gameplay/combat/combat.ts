/*
===========================================================================

combat.ts - worker combat state and native result publication

Owns cast identities, effect lifetimes and received vitals. Presentation events
leave this owner after complete packet validation; animation never owns HP.

===========================================================================
*/
import { SYSTEM_KNOCKBACK } from "@/engine/contracts/orb";
import {
	appendCastResults,
	castResultStageCount,
	requestCastCancellation
} from "@/engine/foundation/gameplay/cast-results";
import { retireBuffSlots, buffDepartureDurationMs, type BuffSlot } from "@/engine/foundation/gameplay/buff-slots";
import { createSkillCooldowns } from "@/engine/foundation/gameplay/skill-cooldowns";
import { createCastPrediction } from "@/engine/foundation/gameplay/cast-prediction";
import { detectionEffect, huntingMovement, type HuntingPoint } from "@/engine/foundation/gameplay/hunting";
import type { SkillMetadata } from "@/engine/foundation/gameplay/skill-catalog";
import { vitalsUpdate } from "@/engine/foundation/gameplay/vitals";
import {
	attachedEffect,
	attachedEffectReferences,
	endedEffectTokens,
	hawkCommand,
	hawkResult,
	type AttachedEffect,
	type AttachedEffectReference
} from "@/engine/foundation/gameplay/attached-effects";
import type { CastState } from "@/engine/contracts/gameplay";
import { castPhase, skillPulse } from "@/engine/foundation/gameplay/combat-result";
/*
================
createCombat

Allocate one session's combat owner. clear releases every actor lifetime while
optionally retaining immutable reference metadata across reconnect.
================
*/
export function createCombat(
	readEntity: ( gid: number ) => import("@/engine/contracts/world").EntityState | undefined = () => undefined,
	publishHp: (
		event:
			| import("@/engine/contracts/effective-hp").CombatPresentationEvent
			| import("@/engine/contracts/orb").SystemEffectFeedback
	) => void = () => {},
	// One delivery, server to client (skill-queue.ts): a cast-start answer
	// started its cooldown that long ago.
	oneWayMs: () => number = () => 0
) {
	let huntingPoints: readonly HuntingPoint[] = [];
	let attackedName: string | undefined, attackedNameUntil = 0;
	const cooldowns = createSkillCooldowns();
	const prediction = createCastPrediction();
	let localGid = 0;
	let skillMetadata: readonly SkillMetadata[] = [];
	let environmentalDamage: import("@/engine/contracts/combat-feedback").EnvironmentalDamage[] = [],
		feedbackSequence = 0;
	const wireHp = new Map<number, number>();
	let effectRefs: ReadonlyMap<number, AttachedEffectReference> = new Map();
	let attachedEffects: readonly AttachedEffect[] = [];
	let hawkRevision = 0, temporaryToken = 0, instanceSerial = 0;
	const castOrder = new Map<number, number>();
	let effectOrder = new WeakMap<AttachedEffect, number>();
	let buffSlots: readonly BuffSlot[] = [], buffSerial = 0;
	/*
	================
	admitBuff

	Preserve insertion order and the authored primary/secondary board policy.
	================
	*/
	function admitBuff( effect: AttachedEffect, allowZero = false ) {
		if ( !effectOrder.has( effect ) ) effectOrder.set( effect, ++instanceSerial );
		if ( effect.gid !== localGid || (!effect.token && !allowZero) ) return;
		const ref = effectRefs.get( effect.skill );
		if ( ref?.zeroEffectDuration || effect.subject && ref?.hideDetectionBuff ) return; // 6DFF80
		if ( buffSlots.length >= 2048 ) throw Error( "Buff slot capacity exceeded" );
		const secondary = !effect.subject && !!skillMetadata.find( row => row.id === effect.skill )?.buffSecondary;
		const display = ref?.indefiniteBuffTimer ?
			{ ...effect, remainingMs: undefined, durationMs: undefined } :
			effect;
		buffSlots = [ ...buffSlots, { state: "active", serial: ++buffSerial, effect: display, secondary } ];
	}
	/*
	================
	retireBuff

	Departure animation retains the slot after its active instance ends.
	================
	*/
	function retireBuff( skill: number, token: number, now: number ) {
		const result = retireBuffSlots( buffSlots, skill, token, now );
		buffSlots = result.slots;
		if ( result.sounded ) publishHp( { kind: "buff-ended", gid: localGid, at: now } );
	}
	const displacements: import("@/engine/contracts/gameplay").CastDisplacement[] = [];
	const cancellations: number[] = [];
	const guidedArrivals = new Map<number, number | null>();
	/*
	================
	applyPhase

	Apply a validated batch with absolute stage identities shared by all targets.
	================
	*/
	function applyPhase( token: number, caster: number, phase: NonNullable<ReturnType<typeof castPhase>> ) {
		const previousCast = casts.get( token ), offset = previousCast ? castResultStageCount( previousCast ) : 0;
		for ( const row of phase.results ) {
			const damage = row.impacts.reduce( ( sum, impact ) => sum + (impact.type === 7 ? 0 : impact.damage), 0 ),
				fatal = row.impacts.some( impact => impact.fatal ),
				previous = vitals.get( row.target );
			// Fatal receipt marks +29C; only LIFE packets own durable life state.
			// Absolute stage offset is shared by ALL targets, including newly named ones.
			row.impacts.forEach( ( impact, i ) =>
				publishHp( {
					kind: "hp-result",
					gid: row.target,
					key: `${token}:${row.target}:${offset + i}`,
					fatal: impact.fatal
				} )
			);
			if ( fatal || previous?.hp !== undefined ) {
				vitals.set( row.target, {
					...previous,
					gid: row.target,
					hp: fatal ? 0 : Math.max( 0, previous!.hp! - damage )
				} );
			}
			for ( const impact of row.impacts ) {
				if ( impact.displacement ) {
					displacements.push( {
						gid: row.target,
						token,
						kind: impact.type as 4 | 5,
						destination: impact.displacement
					} );
					// CIDecoDamageEffect_Initialize (8D5440): a type-5 knockback is
					// carried by a SYSTEM_KNOCKBACK decoration on the struck
					// character, whose guided trajectory is the 250/s travel.
					if ( impact.type === 5 ) {
						publishHp( { kind: "system-effect", gid: row.target, effect: SYSTEM_KNOCKBACK } );
					}
				}
			}
		}
		if ( phase.travel ) {
			displacements.push( { gid: caster, token, kind: 8, destination: phase.travel } );
		}
		if ( phase.correction ) displacements.push( { gid: caster, token, kind: 2, destination: phase.correction } );
	}
	const casts = new Map<number, CastState>(),
		vitals = new Map<number, import("@/engine/contracts/gameplay").VitalState>();
	let publishedCasts: readonly CastState[] | null = null,
		publishedVitals: readonly import("@/engine/contracts/gameplay").VitalState[] | null = null;
	let error: string | null = null;
	/*
	================
	requestCancellation

	Deferred cancellation can retain pending animation results after a request.
	================
	*/
	function requestCancellation( cast: CastState, now: number ) {
		const next = requestCastCancellation( cast, now );
		casts.set( cast.token, next );
		publishedCasts = null;
		if ( cast.cancelledAtMs === undefined && next.cancelledAtMs !== undefined ) {
			publishHp( { kind: "cast-finalize", cast: next } );
			cancellations.push( cast.token );
		}
	}
	/*
	================
	linkedInstance

	85CB60 scans the caster's decoration list in insertion order, not by the
	incoming packet token. A registered attached effect is also a skill deco.
	================
	*/
	function linkedInstance( caster: number, skill: number ) {
		let cast: CastState | undefined, effect: AttachedEffect | undefined, order = Infinity;
		for ( const candidate of casts.values() ) {
			if ( candidate.caster === caster && candidate.skill === skill && candidate.cancelledAtMs === undefined ) {
				const n = castOrder.get( candidate.token ) ?? Infinity;
				if ( n < order ) {
					cast = candidate;
					effect = undefined;
					order = n;
				}
			}
		}
		for ( const candidate of attachedEffects ) {
			if ( candidate.gid === caster && candidate.skill === skill ) {
				const n = effectOrder.get( candidate ) ?? Infinity;
				if ( n < order ) {
					effect = candidate;
					cast = undefined;
					order = n;
				}
			}
		}
		return { cast, effect };
	}
	/*
	================
	continueCast

	Validate the appended stage capacity before publishing any HP changes.
	================
	*/
	function continueCast( cast: CastState, phase: NonNullable<ReturnType<typeof castPhase>>, shotAtMs?: number ) {
		const continued = appendCastResults(
			{ ...cast, target: cast.target || phase.target },
			phase.results,
			phase.stageCount
		);
		// Validate/allocate before HP, motion or presentation side effects.
		applyPhase( cast.token, cast.caster, phase );
		if ( phase.travel ) guidedArrivals.set( cast.token, null );
		casts.set(
			cast.token,
			shotAtMs === undefined ? continued : { ...continued, shotAtMs: cast.shotAtMs ?? shotAtMs }
		);
	}
	/*
	================
	temporaryResults

	Independent results use local negative identities and never acquire casting
	motion. A periodic result retains its skill for impact-effect presentation.
	================
	*/
	function temporaryResults(
		caster: number,
		phase: NonNullable<ReturnType<typeof castPhase>>,
		now: number,
		source: { skill: number; position?: import("@/engine/contracts/gameplay").Pose; } = { skill: 0 }
	) {
		// Constructor skillId/token remain zero in native. Use a negative LOCAL
		// correlation ID, never the incoming wire token or an active cast slot.
		if ( temporaryToken <= -0xffffffff ) throw Error( "Temporary result identity capacity exceeded" );
		const token = --temporaryToken,
			impacts = phase.results.find( row => row.target === phase.target )?.impacts ?? [];
		applyPhase( token, caster, phase );
		const cast: CastState = {
			token,
			caster,
			skill: source.skill,
			effectPosition: source.position,
			target: phase.target,
			impacts,
			results: phase.results,
			resultStageCount: phase.stageCount,
			damage: impacts.reduce( ( sum, hit ) => sum + hit.damage, 0 ),
			fatal: impacts.some( hit => hit.fatal ),
			receivedAtMs: now,
			cancelledAtMs: now,
			resultOnly: true
		};
		publishHp( { kind: "cast-finalize", cast } );
		// 8DDBD0 installs a trajectory callback ON the temporary deco. Its
		// destruction cancels that trajectory immediately; result-owned type
		// 4/5 displacement and the bit-2 destination are independent.
		if ( phase.travel ) cancellations.push( token );
	}
	return {
		/*
		================
		nameAttack
		Return the still-visible attacked-name notice.
		================
		*/
		nameAttack( now: number ) {
			return now < attackedNameUntil ? attackedName : undefined;
		},
		/*
		================
		cooldownReferences
		Changing the local actor rebuilds its board from retained effects.
		================
		*/
		cooldownReferences( gid: number, rows: readonly SkillMetadata[] ) {
			const changed = localGid !== gid;
			localGid = gid;
			skillMetadata = rows;
			if ( changed ) {
				buffSlots = [];
				for ( const effect of attachedEffects ) admitBuff( effect, !!effect.restored || !!effect.subject );
			}
		},
		/*
		================
		references
		Install immutable packet-layout metadata before processing effects.
		================
		*/
		references( rows: readonly ({ id: number; } & AttachedEffectReference)[] ) {
			effectRefs = attachedEffectReferences( rows );
		},
		/*
		================
		seedEffects
		Replace one spawned actor's effects without disturbing other actors.
		================
		*/
		seedEffects(
			gid: number,
			skills: readonly import("@/engine/foundation/gameplay/spawn-skills").SpawnSkill[],
			now = 0
		) {
			const next = skills.filter( row => row.token !== undefined ).map( row => ({
					gid,
					skill: row.id,
					token: row.token ?? 0,
					phase: row.status,
					...(row.remaining !== undefined ?
						{
							remainingMs: row.remaining,
							durationMs: effectRefs.get( row.id )?.effectDurationMs,
							receivedAtMs: now,
							restored: true
						} :
						{ restored: true })
				})
				),
				retained = attachedEffects.filter( row => row.gid !== gid );
			if ( retained.length + next.length > 2048 ) throw Error( "Attached effect capacity exceeded" );
			attachedEffects = [ ...retained, ...next ];
			buffSlots = buffSlots.filter( row => row.effect.gid !== gid );
			for ( const effect of next ) admitBuff( effect, true );
		},
		/*
		================
		seed
		Entry vitals establish the absolute HP baseline for later result batches.
		================
		*/
		seed(
			gid: number,
			fields: Partial<
				Pick<import("@/engine/contracts/gameplay").VitalState, "hp" | "mp" | "maxHp" | "maxMp" | "abnormal">
			>
		) {
			if ( !Number.isInteger( gid ) || gid <= 0 || gid > 0xffffffff ) throw Error( "Invalid vital identity" );
			for ( const value of Object.values( fields ) ) {
				if ( value !== undefined && (!Number.isInteger( value ) || value < 0 || value > 0xffffffff) ) {
					throw Error( "Invalid entry vitals" );
				}
			}
			if ( fields.hp !== undefined ) {
				wireHp.set( gid, fields.hp );
				publishHp( { kind: "hp-seed", gid, hp: fields.hp } );
			}
			if ( Object.values( fields ).some( value => value !== undefined ) ) {
				vitals.set( gid, { ...vitals.get( gid ), ...fields, gid } );
				publishedVitals = null;
			}
		},
		/*
		================
		attack
		Compose the native basic-attack request.
		================
		*/
		attack( gid: number ) {
			const p = Uint8Array.of( 1, 1, 1, 0, 0, 0, 0 );
			new DataView( p.buffer ).setUint32( 3, gid, true );
			return { opcode: 0x72cd, payload: p };
		},
		/*
		================
		predict

		Start the local press's cast animation now (cast-prediction.ts).
		================
		*/
		predict( skill: number, target: number, now: number, deadlineMs: number ) {
			if ( localGid ) prediction.predict( localGid, skill, target, now, deadlineMs );
		},
		/*
		================
		predicting
		================
		*/
		predicting: () => prediction.open(),
		/*
		================
		pressQueued

		The press was queued behind an open command (B2CD arm, count 2): its
		cast and its cooldown start only when that command ends, so neither
		its prediction nor its cooldown stand-in holds. True when the plane
		changed.
		================
		*/
		pressQueued( now: number ): boolean {
			const standIns = cooldowns.refused();
			return prediction.cancel( now ) || standIns;
		},
		/*
		================
		pressed

		A skill press went out: its cooldown stands in from when the press
		reaches the server (arrivesAtMs) until its answer, or untilMs.
		================
		*/
		pressed( id: number, arrivesAtMs: number, untilMs: number, now: number ) {
			const metadata = skillMetadata.find( row => row.id === id );
			if ( metadata ) cooldowns.pressed( metadata, arrivesAtMs, untilMs, now );
		},
		/*
		================
		skill
		A skill request carries an optional object target, never client damage.
		================
		*/
		skill( id: number, gid?: number ) {
			if ( !Number.isInteger( id ) || id <= 0 || id > 0xffffffff ) {
				throw new Error( "Invalid skill ID" );
			}
			const p = new Uint8Array( gid ? 11 : 7 ), v = new DataView( p.buffer );
			p[0] = 1;
			p[1] = 4;
			v.setUint32( 2, id, true );
			p[6] = gid ? 1 : 0;
			if ( gid ) {
				v.setUint32( 7, gid, true );
			}
			return { opcode: 0x72cd, payload: p };
		},
		/*
		================
		receive
		Dispatch validated native packets to their cast, effect or vital owner.
		================
		*/
		receive( op: number, p: Uint8Array, now = 0 ) {
			if ( op === 0xb3c6 ) {
				const pulse = skillPulse( p, gid => {
					const object = readEntity( gid );
					if ( !object?.skillObject ) return undefined;
					return {
						skill: object.skillObject.skillId,
						position: {
							regionId: object.regionId,
							x: object.x,
							y: object.y,
							z: object.z,
							angle: object.heading
						}
					};
				} );
				if ( !pulse ) return p[0] === 3;
				temporaryResults( pulse.caster, pulse.phase, now, { skill: pulse.skill, position: pulse.position } );
				publishedVitals = null;
				return true;
			}
			if ( op === 0x3122 && p.length === 6 && p[4] === 0 && p[5] === 1 ) {
				publishHp( {
					kind: "hp-revive",
					gid: new DataView( p.buffer, p.byteOffset, p.byteLength ).getUint32( 0, true )
				} );
			}
			// 74D9FD -> 74FE80: periodic damage is display-only. HP and
			// death remain owned by 33A6; this packet never debits HP again.
			if ( op === 0x3128 ) {
				if ( p.length !== 8 ) throw Error( "Invalid periodic damage notification" );
				const v = new DataView( p.buffer, p.byteOffset, p.byteLength ),
					gid = v.getUint32( 0, true ),
					damage = v.getUint32( 4, true );
				if ( readEntity( gid ) && damage ) {
					if ( environmentalDamage.length >= 2048 ) throw Error( "Environmental feedback capacity exceeded" );
					environmentalDamage = [ ...environmentalDamage, {
						sequence: ++feedbackSequence,
						gid,
						damage,
						atMs: now
					} ];
				}
				return true;
			}
			if ( op === 0x30e3 ) {
				huntingPoints = huntingMovement( huntingPoints, p );
				return false;
			}
			if ( op === 0xb5ed ) {
				const effect = detectionEffect( p, effectRefs );
				if ( !localGid ) throw Error( "Detection effect without local character" );
				if ( attachedEffects.length >= 2048 ) throw Error( "Attached effect capacity exceeded" );
				// 7766E6 retains the subject even without hunting (+250).
				// 6E5E26 adds the optional rider to the authored duration;
				// absence of RPBU/STDU does not mean a zero-length effect.
				const duration = ((effectRefs.get( effect.skill )?.effectDurationMs ?? 0) + effect.duration) >>> 0;
				attachedEffects = [ ...attachedEffects, {
					gid: localGid,
					skill: effect.skill,
					token: effect.token,
					phase: 2,
					subject: { gid: effect.owner, name: effect.name },
					receivedAtMs: now,
					...(duration ? { remainingMs: duration, durationMs: duration } : {})
				} ];
				admitBuff( attachedEffects[attachedEffects.length - 1]!, true );
				if ( effect.track && !huntingPoints.some( row => row.gid === effect.owner ) ) {
					const source = readEntity( effect.owner );
					huntingPoints = [ ...huntingPoints, {
						gid: effect.owner,
						token: effect.token,
						name: effect.name,
						regionId: source?.regionId ?? 0,
						x: source?.x ?? 0,
						y: source?.y ?? 0,
						z: source?.z ?? 0,
						angle: source?.heading ?? 0
					} ].sort( ( a, b ) => a.gid - b.gid );
				}
				return true;
			}
			if ( op === 0x357a ) {
				const command = hawkCommand( p ), instance = attachedEffects.find( row => row.token === command.token );
				if ( instance ) {
					const impact = hawkResult( command.damage ), previous = vitals.get( command.target );
					// Hawk damage is applied by the retained arrival result on the main thread.
					if ( impact.fatal || previous?.hp !== undefined ) {
						vitals.set( command.target, {
							...previous,
							gid: command.target,
							hp: impact.fatal ? 0 : Math.max( 0, previous!.hp! - impact.damage )
						} );
						publishedVitals = null;
					}
					const revision = ++hawkRevision;
					// 8E3800 constructs its stack result at impact, outside the
					// outstanding vectors scanned by 8E2840; no HP checkpoint hold here.
					attachedEffects = attachedEffects.map( row => {
						if ( row !== instance ) return row;
						const updated = { ...row, hawk: { revision, target: command.target, damage: command.damage } };
						effectOrder.set( updated, effectOrder.get( row )! );
						return updated;
					} );
				}
				return true;
			}
			// CPSMission_OnAttachedEffectAdd0xB419 (0x776450) identifies each icon by token.
			if ( op === 0xb419 ) {
				const decoded = attachedEffect( p, effectRefs ),
					duration = effectRefs.get( decoded.skill )?.effectDurationMs ?? 0,
					effect = {
						...decoded,
						receivedAtMs: now,
						...(duration && decoded.token ?
							{
								remainingMs: (duration + (decoded.extra ?? 0)) >>> 0,
								durationMs: (duration + (decoded.extra ?? 0)) >>> 0
							} :
							{})
					},
					kept = effect.token ? attachedEffects.filter( row => row.token !== effect.token ) : attachedEffects;
				if ( kept.length >= 2048 ) throw Error( "Attached effect capacity exceeded" );
				attachedEffects = [ ...kept, effect ];
				admitBuff( effect );
				return true;
			}
			// CPSMission_OnEffectTeardownTokens0xB6A0 (0x7759B0) removes only retired tokens.
			if ( op === 0xb6a0 ) {
				const tokens = endedEffectTokens( p );
				for ( const token of tokens ) {
					if ( !token ) continue;
					const effect = attachedEffects.find( row => row.token === token ), cast = casts.get( token );
					// 8E2C69: kind 1 REQUESTS cancellation; kinds 2/3 extinguish.
					// The return's &2 still controls local board feedback even
					// when the kind-1 request is deferred.
					if ( effect ) {
						attachedEffects = attachedEffects.filter( row => row !== effect );
						if ( effect.gid === localGid ) {
							retireBuff( effect.skill, token, now );
							const first = huntingPoints.find( row => row.token === token );
							if ( first ) huntingPoints = huntingPoints.filter( row => row !== first );
						}
						// One token, one native deco: a cast whose effect attached
						// under its own token (Crystal Wall's ice, an aura) ends with
						// it. Kept, it stayed in the cast table for good and its
						// looping visuals stood around the caster forever.
						if ( cast ) requestCancellation( cast, now );
					} else if ( cast ) {
						requestCancellation( cast, now );
						if ( cast.caster === localGid ) retireBuff( cast.skill, token, now );
					}
				}
				return true;
			}
			if ( [ 0xb45a, 0x33a6, 0xb505, 0xb245 ].includes( op ) ) {
				publishedCasts = publishedVitals = null;
			}
			const v = new DataView( p.buffer, p.byteOffset, p.byteLength );
			if ( op === 0xb45a && p.length === 14 && p[0] === 1 && p[5] === 1 ) {
				const gid = v.getUint32( 1, true );
				vitals.set( gid, { ...vitals.get( gid ), gid, hp: v.getUint32( 6, true ) } );
				publishHp( { kind: "hp-seed", gid, hp: v.getUint32( 6, true ) } );
				return true;
			}
			if ( op === 0x33a6 ) {
				const { sourceFlags, ...update } = vitalsUpdate( p ), gid = update.gid;
				if ( !vitals.has( gid ) && vitals.size >= 16384 ) {
					throw new Error( "Vitals capacity exceeded" );
				}
				// 77A080 -> 8E2840 compares the wire baseline, not HP already
				// reduced by a cast. Validate the whole transaction before publishing.
				const previous = wireHp.get( gid ),
					damage = update.hp !== undefined && (sourceFlags & 0x400) && previous !== undefined ?
						Math.max( 0, previous - update.hp ) :
						0;
				if ( damage && environmentalDamage.length >= 2048 ) {
					throw Error( "Environmental feedback capacity exceeded" );
				}
				vitals.set( gid, { ...vitals.get( gid ), ...update } );
				if ( damage ) {
					environmentalDamage = [ ...environmentalDamage, {
						sequence: ++feedbackSequence,
						gid,
						damage,
						atMs: now
					} ];
				}
				if ( update.hp !== undefined ) {
					wireHp.set( gid, update.hp );
					publishHp( { kind: "hp-refresh", gid, hp: update.hp, sourceFlags, atMs: now } );
				}
				return true;
			}
			if ( op === 0xb505 ) {
				if ( p[0] === 1 ) {
					if ( p.length < 10 ) throw new Error( "Truncated cast phase release" );
					const phase = castPhase( p, 5 );
					if ( !phase ) return false;
					const token = v.getUint32( 1, true ), cast = casts.get( token );
					if ( cast && cast.cancelledAtMs === undefined ) {
						continueCast( cast, phase, now );
					}
					return true;
				}
				if ( p.length !== 6 || p[0] !== 2 ) {
					return false;
				}
				const token = v.getUint32( 2, true ), cast = casts.get( token );
				if ( cast ) requestCancellation( cast, now );
				else {
					const effect = attachedEffects.find( row => row.token === token );
					if ( effect ) {
						// B505 always calls RequestCancellation, including on
						// kind-2/3 effects. Unlike B6A0 this is not forced.
						if ( effectRefs.get( effect.skill )?.cancellationDeferred ) {
							const requested = {
								...effect,
								cancellationRequestedAtMs: effect.cancellationRequestedAtMs ?? now
							};
							effectOrder.set( requested, effectOrder.get( effect )! );
							attachedEffects = attachedEffects.map( row => row === effect ? requested : row );
						} else {
							attachedEffects = attachedEffects.filter( row => row !== effect );
							cancellations.push( token );
						}
					}
				}
				return true;
			}
			if ( op !== 0xb245 ) {
				return false;
			}
			if ( p[0] !== 1 ) {
				if ( p.length !== 2 ) {
					throw new Error( "Invalid cast refusal" );
				}
				error = `Cast rejected: ${p[1]}`;
				// The press it answers never started a cooldown or a cast.
				cooldowns.refused();
				prediction.cancel( now );
				return true;
			}
			if ( p.length < 19 ) throw Error( "Truncated cast header" );
			if ( ![ 0, 2 ].includes( p[1]! ) ) return false;
			const phase = castPhase( p, 14 );
			if ( !phase ) return false;
			const token = v.getUint32( 10, true ),
				caster = v.getUint32( 6, true ),
				skill = v.getUint32( 2, true ),
				target = phase.target;
			const linked = effectRefs.get( skill )?.linkedSkillId ?? 0;
			let active: CastState | undefined;
			if ( linked && linked !== skill ) {
				const instance = linkedInstance( caster, linked );
				if ( instance.cast ) {
					continueCast( instance.cast, phase );
					active = casts.get( instance.cast.token );
				} else if ( instance.effect ) {
					// SetupMotionMetadata (kind 2/3) allocates no result-stage
					// vector. 8E0190 consumes an overflowing batch without
					// applying it; it must NOT become an orphan/new cast.
					if ( !phase.stageCount ) applyPhase( instance.effect.token, caster, phase );
				} else temporaryResults( caster, phase, now );
			} else {
				if ( !token ) throw Error( "Invalid cast token" );
				if ( casts.has( token ) ) return true;
				if ( casts.size >= 2048 ) throw Error( "Unfinalized cast capacity exceeded" );
				const impacts = phase.results.find( row => row.target === target )?.impacts ?? [],
					damage = impacts.reduce( ( sum, hit ) => sum + hit.damage, 0 ),
					fatal = impacts.some( hit => hit.fatal );
				applyPhase( token, caster, phase );
				const predictedToken = caster === localGid ? prediction.adopt( caster, skill ) : undefined;
				// Another local cast that holds the caster (anything but a known
				// instant row) opened first: the press waits behind it, so its
				// predicted animation would run early.
				if (
					caster === localGid && predictedToken === undefined &&
					skillMetadata.find( row => row.id === skill )?.haltsWalk !== false
				) prediction.cancel( now );
				active = {
					token,
					caster,
					target,
					skill,
					...(predictedToken === undefined ? {} : { predictedToken }),
					damage,
					fatal,
					impacts,
					results: phase.results,
					resultStageCount: phase.stageCount,
					receivedAtMs: now,
					...(effectRefs.get( skill )?.cancellationDeferred ? { cancellationDeferred: true } : {})
				};
				casts.set( token, active );
				// Ground targeting is a request UI policy. The received travel bit
				// owns arrival for targeted charges and remote casts as well.
				if ( phase.travel ) guidedArrivals.set( token, null );
				castOrder.set( token, ++instanceSerial );
			}
			if ( active && caster === localGid && effectRefs.get( skill )?.nameHit ) {
				const first = [ active.target, ...(active.results ?? []).map( row => row.target ) ].map( readEntity )
					.find( row => row?.kind === "player" || row?.kind === "local-player" );
				if ( first ) {
					attackedName = first.name;
					attackedNameUntil = now + 20000;
				}
			}
			const metadata = caster === localGid ?
				skillMetadata.find( r => r.id === v.getUint32( 2, true ) ) :
				undefined;
			if ( metadata ) cooldowns.accepted( metadata, now - oneWayMs(), now );
			// This snapshot is authority for worker decisions. Effective HP is owned
			// separately by result application and ordered HP checkpoint retirement.
			error = null;
			return true;
		},
		/*
		================
		guidedActive
		Only a live guided trajectory retains the actor's arrival gate.
		================
		*/
		guidedActive( gid: number, now: number ) {
			for ( const [token, at] of guidedArrivals ) {
				const cast = casts.get( token );
				if ( at !== null && at > now && cast?.caster === gid && cast.cancelledAtMs === undefined ) return true;
			}
			return false;
		},
		/*
		================
		cancelGuided
		Cancel all retained guided arrivals belonging to one actor.
		================
		*/
		cancelGuided( gid: number, now: number ) {
			for ( const [token] of guidedArrivals ) {
				const cast = casts.get( token );
				if ( cast?.caster === gid ) {
					guidedArrivals.delete( token );
					requestCancellation( cast, now );
				}
			}
		},
		/*
		================
		guidedArrival
		Only a server-authorized guided trajectory may install an arrival deadline.
		================
		*/
		guidedArrival( token: number, at: number ) {
			const cast = casts.get( token );
			if (
				cast && guidedArrivals.has( token ) && Number.isFinite( at ) &&
				cast.cancelledAtMs === undefined
			) guidedArrivals.set( token, at );
		},
		/*
		================
		takeDisplacements
		Transfer committed movement corrections to the motion owner once.
		================
		*/
		takeDisplacements() {
			return displacements.splice( 0 );
		},
		/*
		================
		takeCancellations
		Transfer trajectory cancellations after their result transaction.
		================
		*/
		takeCancellations() {
			return cancellations.splice( 0 );
		},
		/*
		================
		step
		Advance presentation retention and cooldowns without inventing HP pulses.
		================
		*/
		step( now: number ) {
			let changed = cooldowns.step( now );
			if ( prediction.step( now ) ) changed = true;
			for ( const [token, at] of guidedArrivals ) {
				if ( at !== null && now >= at ) {
					guidedArrivals.delete( token );
					const cast = casts.get( token );
					if ( cast && cast.cancelledAtMs === undefined ) {
						requestCancellation( cast, at );
						changed = true;
					}
				}
			}
			for ( const slot of buffSlots ) {
				if (
					slot.state === "active" && !slot.secondary && slot.effect.skill === 0xc0000000 &&
					slot.effect.remainingMs !== undefined &&
					now - (slot.effect.receivedAtMs ?? now) > slot.effect.remainingMs
				) {
					retireBuff( slot.effect.skill, 0, now );
					changed = true;
				}
			}
			const slots = buffSlots.filter( slot =>
				slot.state !== "departing" || now - slot.endedAtMs < buffDepartureDurationMs
			);
			if ( slots.length !== buffSlots.length ) {
				buffSlots = slots;
				changed = true;
			}
			const retained = environmentalDamage.filter( row => now - row.atMs < 1000 );
			if ( retained.length !== environmentalDamage.length ) {
				environmentalDamage = retained;
				changed = true;
			}
			for ( const [token, cast] of casts ) {
				if ( cast.cancelledAtMs !== undefined && now - cast.cancelledAtMs >= 200 ) {
					casts.delete( token );
					castOrder.delete( token );
					guidedArrivals.delete( token );
					changed = true;
				}
			}
			if ( changed ) publishedCasts = null;
			return changed;
		},
		/*
		================
		remove
		An actor departure retires owned casts and discards deferred results.
		================
		*/
		remove( gid: number, now = 0 ) {
			publishedCasts = publishedVitals = null;
			vitals.delete( gid );
			wireHp.delete( gid );
			buffSlots = buffSlots.filter( row => row.effect.gid !== gid );
			attachedEffects = attachedEffects.filter( row => row.gid !== gid );
			environmentalDamage = environmentalDamage.filter( row => row.gid !== gid );
			for ( const [token, cast] of casts ) {
				if ( cast.caster === gid ) {
					publishHp( {
						kind: "cast-finalize",
						cast: {
							...cast,
							cancelledAtMs: cast.cancelledAtMs ?? now,
							...(cast.cancellationDeferred ? { discardPendingResults: true } : {})
						}
					} );
					casts.delete( token );
					castOrder.delete( token );
					guidedArrivals.delete( token );
				}
			}
		},
		/*
		================
		state
		Publish stable snapshots until an owner mutation invalidates them.
		================
		*/
		state() {
			return {
				buffSlots,
				huntingPoints,
				skillCooldowns: cooldowns.state(),
				castPrediction: prediction.state(),
				attachedEffects,
				environmentalDamage,
				casts: publishedCasts ?? (publishedCasts = [ ...casts.values() ]),
				vitals: publishedVitals ?? (publishedVitals = [ ...vitals.values() ]),
				error
			};
		},
		/*
		================
		clear
		End the session lifetime, including local-only result identities.
		================
		*/
		clear( preserveReferences = false ) {
			guidedArrivals.clear();
			attackedName = undefined;
			attackedNameUntil = 0;
			huntingPoints = [];
			cooldowns.clear();
			prediction.clear();
			temporaryToken = 0;
			instanceSerial = 0;
			castOrder.clear();
			effectOrder = new WeakMap();
			localGid = 0;
			skillMetadata = [];
			if ( !preserveReferences ) effectRefs = new Map();
			attachedEffects = [];
			buffSlots = [];
			buffSerial = 0;
			environmentalDamage = [];
			wireHp.clear();
			cancellations.length = 0;
			displacements.length = 0;
			publishedCasts = publishedVitals = null;
			casts.clear();
			vitals.clear();
			error = null;
		}
	};
}
