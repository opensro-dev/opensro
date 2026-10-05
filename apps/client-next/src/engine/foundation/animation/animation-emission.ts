/*
===========================================================================

animation-emission.ts - particle emitters keyed to a model's animation events

A BSR's animation modifier sets fire .efp programs at authored clip times.
This module selects those sets, starts an emitter when its trigger falls in
a dispatched range, and keeps a detached tail at its last world matrix when
the animation restarts, so particles already born finish where they were.

===========================================================================
*/
import { animationModelParticles, type ModelParticle } from "./model-particles";
import { modelParticleAttachment } from "./model-emission";
import { radians } from "@/engine/foundation/math/angles";
import type { ModifierSelector } from "./model-animation";
import type { CharacterActor } from "@/engine/contracts/character";
export interface AnimationParticleSet extends ModifierSelector {
	readonly particles: readonly ModelParticle[];
	readonly override: boolean;
	readonly flags: number;
}
/*
================
modelAnimationParticles

The animation-keyed modifier sets of one model resource.
================
*/
export function modelAnimationParticles( value: unknown ): readonly AnimationParticleSet[] {
	if ( value === undefined ) return [];
	if ( !Array.isArray( value ) ) throw Error( "Invalid animation particle sets" );
	return value.filter( row => row.kind === 1 ).map( row => {
		if (
			typeof row.animationSetName !== "string" || !Number.isInteger( row.stateId ) ||
			!Array.isArray( row.baseWords ) || row.baseWords.length !== 6
		) throw Error( "Invalid animation particle selector" );
		return {
			set: row.animationSetName,
			state: row.stateId,
			override: row.baseWords[4] !== 0,
			flags: row.baseWords[2],
			particles: animationModelParticles( [ row ] )
		};
	} );
}
/*
================
matches
================
*/
function matches( a: ModifierSelector | null | undefined, b: ModifierSelector ): boolean {
	return !!a && a.set === b.set && a.state === b.state;
}
/*
================
createAnimationEmission

One wrapper per authored entry, shared by repeat installations of its BAN.
Keys use [from,to); inactive queues retain their wrappers and instances.
================
*/
export function createAnimationEmission( allocate: () => number ) {
	type Instance = {
		actor: CharacterActor;
		started: number;
		owner: number;
		ownerModel: string;
		born?: { matrix: Float32Array; regionId: number; } | null;
	};
	const tails = new Map<number, Instance>();
	const owners = new Map<
		number,
		{
			model: string;
			sets: readonly AnimationParticleSet[];
			keys: { set: AnimationParticleSet; particle: ModelParticle; }[];
			instances: Map<ModelParticle, Instance>;
		}
	>();
	return {
		/*
		================
		step
		================
		*/
		step(
			holders: readonly { actor: CharacterActor; sets: readonly AnimationParticleSet[]; }[],
			seconds: number,
			ready: ( path: string ) => boolean,
			capacity: number,
			night: boolean,
			snapshot: ( gid: number, actor?: CharacterActor ) => { matrix: Float32Array; regionId: number; } | null,
			age: ( gid: number ) => number | undefined,
			duration: ( path: string ) => number
		): readonly CharacterActor[] {
			const keep = new Set<number>(), out: CharacterActor[] = [];
			/*
			================
			detach

			Keep a replaced emitter as a tail at its last world matrix; the
			matrix's basis is the exact world basis of the program.
			================
			*/
			function detach( instance: Instance ) {
				const captured = snapshot( instance.actor.gid ) ?? instance.born;
				if ( !captured ) return;
				const m = captured.matrix;
				const a = instance.actor;
				tails.set( a.gid, {
					started: instance.started,
					owner: instance.owner,
					ownerModel: instance.ownerModel,
					actor: {
						...a,
						attachment: undefined,
						pose: { regionId: captured.regionId, x: m[12]!, y: m[13]!, z: m[14]!, yaw: radians( 0 ) },
						effectBasis: [ m[0]!, m[1]!, m[2]!, m[4]!, m[5]!, m[6]!, m[8]!, m[9]!, m[10]! ],
						deferredParticle: { offset: 0 }
					}
				} );
			}
			for ( const { actor, sets } of holders ) {
				const id = actor.modifierId ?? actor.gid;
				keep.add( id );
				let owner = owners.get( id );
				if ( !owner || owner.model !== actor.model || owner.sets !== sets ) {
					owner = {
						model: actor.model,
						sets,
						keys: sets.flatMap( set => set.particles.map( particle => ({ set, particle }) ) ).sort( (
							a,
							b
						) => a.particle.triggerMs! - b.particle.triggerMs! ),
						instances: new Map()
					};
					owners.set( id, owner );
				}
				for ( const selector of actor.modelAnimation?.restarted ?? [] ) {
					for ( const set of sets ) {
						if ( matches( selector, set ) ) {
							for ( const particle of set.particles ) {
								const old = owner.instances.get( particle );
								if ( old ) {
									detach( old );
									owner.instances.delete( particle );
								}
							}
						}
					}
				}
				for ( const dispatch of actor.modelAnimation?.dispatch ?? [] ) {
					for ( const [from, to] of dispatch.ranges ) {
						for ( const { set, particle } of owner.keys ) {
							if (
								!matches( dispatch.selector, set ) || particle.triggerMs! < from ||
								particle.triggerMs! >= to
							) {
								continue;
							}
							const model = "/assets/effects/programs.json#" + encodeURIComponent( particle.effectPath );
							if (
								ready( model ) && (actor.animationLod?.fraction ?? 0) <= Math.fround( .9 ) &&
								(!particle.nightOnly || night)
							) {
								const old = owner.instances.get( particle );
								if ( old ) detach( old );
								owner.instances.set( particle, {
									started: seconds,
									owner: id,
									ownerModel: actor.model,
									actor: {
										gid: allocate(),
										model,
										pose: actor.pose,
										clip: "effect",
										time: 0,
										loop: false,
										scale: 1,
										pickable: false,
										attachment: modelParticleAttachment( actor.gid, particle, actor.scale )
									}
								} );
								const created = owner.instances.get( particle )!;
								created.born = snapshot( created.actor.gid, created.actor );
							}
						}
					}
				}
				for ( const set of sets ) {
					for ( const particle of set.particles ) {
						const model = "/assets/effects/programs.json#" + encodeURIComponent( particle.effectPath ),
							loaded = ready( model );
						const instance = owner.instances.get( particle );
						if ( !instance || !loaded || out.length >= capacity ) continue;
						out.push( {
							...instance.actor,
							attachment: instance.actor.attachment ?
								{ ...instance.actor.attachment, gid: actor.gid } :
								undefined,
							pose: actor.pose,
							time: seconds - instance.started,
							deferredParticle: {
								offset: particle.deferred ?? 0,
								nightOnly: particle.nightOnly,
								lodHidden: !(set.flags & 32) || !matches( actor.modelAnimation?.selected, set ) ||
									(actor.animationLod?.fraction ?? 0) > Math.fround( .9 )
							}
						} );
					}
				}
			}
			for ( const id of owners.keys() ) if ( !keep.has( id ) ) owners.delete( id );
			for ( const [gid, tail] of tails ) {
				if (
					!keep.has( tail.owner ) || owners.get( tail.owner )?.model !== tail.ownerModel ||
					(age( gid ) ?? 0) >= duration( tail.actor.model )
				) {
					tails.delete( gid );
					continue;
				}
				if ( out.length < capacity && ready( tail.actor.model ) ) {
					out.push( { ...tail.actor, time: seconds - tail.started } );
				}
			}
			return out;
		},
		/*
		================
		reset
		================
		*/
		reset() {
			owners.clear();
			tails.clear();
		}
	};
}
