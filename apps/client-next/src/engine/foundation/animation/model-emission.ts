/*
===========================================================================

model-emission.ts - ambient particle emitters carried by a model resource

A BSR's ambient modifier set names .efp programs bound to its bones. This
module selects that set, owns each holder's emitter identities and clocks,
and is the one place that turns a model particle into a renderer
attachment, so every producer (equipment, drops, skill-stage meshes) draws
the same 'bsr' particle transform.

===========================================================================
*/
import { ambientModelParticles, type ModelParticle } from "./model-particles";
import type { CharacterActor } from "@/engine/contracts/character";

/*
================
modelAmbientParticles

Ambient is a native modifier-set selector, not a filter on effect filenames.
Named and animation sets remain in the manifest for their separate producers.
================
*/
export function modelAmbientParticles( value: unknown ): readonly ModelParticle[] {
	if ( value === undefined ) return [];
	if ( !Array.isArray( value ) ) throw Error( "Invalid model modifier sets" );
	for ( const row of value ) {
		if (
			!row || ![ 0, 1, 2 ].includes( row.kind ) || !Number.isInteger( row.stateId ) ||
			typeof row.animationSetName !== "string" || !Array.isArray( row.entries )
		) throw Error( "Invalid model modifier selector" );
	}
	return ambientModelParticles( value.filter( row => row.kind === 2 ) );
}

/*
================
modelParticleAttachment

The attachment of one model particle on its holder: the BSR particle
transform (bsrParticleAttachment) with the holder's model scale and the
particle's authored rotation override.
================
*/
export function modelParticleAttachment(
	holder: number,
	particle: ModelParticle,
	modelScale: number
): NonNullable<CharacterActor["attachment"]> {
	return {
		gid: holder,
		bone: particle.bone,
		root: particle.root,
		offset: particle.offset,
		basis: "bsr",
		modelScale,
		rotation: particle.rotation
	};
}

/*
================
createModelEmission

The presentation owner supplies admitted holders. Each emitter identity
lives with that holder/resource, independently of its changing animation
clip.
================
*/
export function createModelEmission( allocate: () => number ) {
	const rows = new Map<
		number,
		{
			model: string;
			scale: number;
			particles: readonly ModelParticle[];
			ids: number[];
			started: (number | null)[];
		}
	>();
	return {
		/*
		================
		transfer
		================
		*/
		transfer( from: number, to: number ): readonly ModelParticle[] {
			const row = rows.get( from );
			if ( !row ) return [];
			rows.delete( from );
			rows.set( to, row );
			return row.particles;
		},
		/*
		================
		step
		================
		*/
		step(
			holders: readonly { actor: CharacterActor; particles: readonly ModelParticle[]; }[],
			seconds: number,
			ready: ( path: string ) => boolean,
			capacity: number,
			lod: ( gid: number ) => number = () => 0
		): readonly CharacterActor[] {
			const keep = new Set<number>(), out: CharacterActor[] = [];
			for ( const { actor, particles } of holders ) {
				if ( !particles.length ) continue;
				keep.add( actor.gid );
				let row = rows.get( actor.gid );
				if ( !row || row.model !== actor.model || row.particles !== particles ) {
					row = {
						model: actor.model,
						scale: actor.scale,
						particles,
						ids: particles.map( () => allocate() ),
						started: particles.map( () => null )
					};
					rows.set( actor.gid, row );
				}
				for ( let i = 0; i < particles.length; i++ ) {
					const particle = particles[i]!,
						model = "/assets/effects/programs.json#" + encodeURIComponent( particle.effectPath );
					if ( out.length >= capacity || !ready( model ) ) continue;
					if ( row.started[i] === null || seconds < row.started[i]! ) row.started[i] = seconds;
					out.push( {
						gid: row.ids[i]!,
						deferredParticle: {
							offset: particle.deferred ?? 0,
							lodHidden: particle.source !== "equipment" && !!actor.modelAnimation?.selected?.override ||
								lod( actor.gid ) > Math.fround( .9 )
						},
						model,
						pose: actor.pose,
						clip: "effect",
						time: seconds - row.started[i]!,
						loop: true,
						scale: 1,
						pickable: false,
						attachment: modelParticleAttachment( actor.gid, particle, particle.scale ?? row.scale )
					} );
				}
			}
			for ( const id of rows.keys() ) if ( !keep.has( id ) ) rows.delete( id );
			return out;
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
