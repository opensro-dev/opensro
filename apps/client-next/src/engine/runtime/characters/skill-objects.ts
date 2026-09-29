/*
===========================================================================

skill-objects.ts - presentation lifetime for native stationary skill objects

World entities own existence and pose. This owner joins their skill identity
to authored resources and starts clocks only after resource admission. It
does not infer captures from animations or keep objects alive after despawn.

===========================================================================
*/
import type { CharacterActor } from "@/engine/contracts/character";
import type { EntityState } from "@/engine/contracts/world";
import { characterHeadingYaw } from "@/engine/foundation/math/angles";
import { modelAmbientParticles } from "@/engine/foundation/animation/model-emission";
import type { ModelParticle } from "@/engine/foundation/animation/model-particles";

export const SKILL_OBJECT_MANIFESTS = [
	"/assets/skillfx/manifest.json"
] as const;

/*
================
ObjectResource

The native record's +B0 discriminator distinguishes BSR and EFP resources.
================
*/
interface ObjectResource {
	kind: "model" | "effect";
	path: string;
}

/*
================
ObjectModel

Publication keeps a skill object's own animation and attached emitters.
================
*/
interface ObjectModel {
	glb: string;
	clips: readonly string[];
	clipLoop: boolean;
	particles: readonly ModelParticle[];
}

/*
================
ObjectClock

Resource replacement is a new visual installation, even under the same GID.
================
*/
interface ObjectClock {
	model: string;
	started: number;
	modifierId: number;
}

/*
================
SkillObjectResources

The parent resource owner admits residency; this owner never performs IO.
================
*/
interface SkillObjectResources {
	ready( path: string ): boolean;
	plan( paths: readonly string[] ): boolean;
}

/*
================
recordObject

Validate the JSON boundary before publishing a replacement lookup table.
================
*/
function recordObject( value: unknown ): Record<string, unknown> {
	if ( !value || typeof value !== "object" || Array.isArray( value ) ) {
		throw new Error( "Invalid skill-object resource record" );
	}
	return value as Record<string, unknown>;
}

/*
================
createSkillObjects

86C440 installs state zero at unit scale. Object pose and heading come from
the common native spawn record, independently of the owner's later movement.
================
*/
export function createSkillObjects( allocate: () => number ) {
	const records = new Map<number, ObjectResource>();
	const models = new Map<string, ObjectModel>();
	const clocks = new Map<number, ObjectClock>();
	const loaded = new Set<string>();
	return {
		/*
		================
		nextManifest

		Stationary objects request their catalogs only when present in the
		world. Ordinary character readiness has no dependency on these files.
		================
		*/
		nextManifest( entities: readonly EntityState[] ) {
			if ( !entities.some( entity => entity.skillObject ) ) return undefined;
			return SKILL_OBJECT_MANIFESTS.find( path => !loaded.has( path ) );
		},
		/*
		================
		catalog

		Build each replacement separately so malformed assets retain live state.
		================
		*/
		catalog( path: string, value: unknown ) {
			if ( path !== SKILL_OBJECT_MANIFESTS[0] ) return;
			const manifest = recordObject( value );
			if ( manifest.format !== "sro-skill-stage-models" ) {
				throw new Error( "Invalid skill-object model catalog" );
			}
			const next = new Map<number, ObjectResource>();
			for ( const [key, entry] of Object.entries( recordObject( manifest.objects ) ) ) {
				const row = recordObject( entry );
				if (
					!Number.isInteger( Number( key ) ) || (row.kind !== "model" && row.kind !== "effect") ||
					typeof row.path !== "string" || !row.path || row.path.includes( ".." ) ||
					row.path.includes( "\\" )
				) {
					throw new Error( "Invalid native skill-object resource" );
				}
				next.set( Number( key ), { kind: row.kind, path: row.path } );
			}
			const nextModels = new Map<string, ObjectModel>();
			for ( const [key, value] of Object.entries( recordObject( manifest.models ) ) ) {
				const row = recordObject( value );
				if ( row.error ) continue;
				if (
					typeof row.glb !== "string" || !row.glb.startsWith( "/assets/skillfx/" ) ||
					row.glb.includes( ".." ) || row.glb.includes( "\\" ) || !Array.isArray( row.clips ) ||
					row.clips.some( clip => typeof clip !== "string" ) || typeof row.clipLoop !== "boolean"
				) {
					throw new Error( "Invalid skill-object model publication" );
				}
				nextModels.set( key, {
					glb: row.glb,
					clips: row.clips,
					clipLoop: row.clipLoop,
					particles: modelAmbientParticles( row.particleModifiers )
				} );
			}
			models.clear();
			for ( const [key, row] of nextModels ) models.set( key, row );
			records.clear();
			for ( const [key, row] of next ) records.set( key, row );
			loaded.add( path );
		},
		/*
		================
		retain

		Distance culling can pause presentation, but world removal ends a clock.
		================
		*/
		retain( entities: readonly EntityState[] ) {
			const alive = new Set( entities.filter( entity => entity.skillObject ).map( entity => entity.gid ) );
			for ( const gid of clocks.keys() ) if ( !alive.has( gid ) ) clocks.delete( gid );
		},
		/*
		================
		frame

		Use the same resource budget as characters and drops. A cold resource
		does not consume its initial animation while waiting for admission.
		================
		*/
		frame( entity: EntityState, seconds: number, resources: SkillObjectResources ) {
			const resource = entity.skillObject && records.get( entity.skillObject.skillId );
			if ( !resource ) return null;
			const model: ObjectModel | undefined = resource.kind === "effect" ?
				{
					glb: "/assets/effects/programs.json#" + encodeURIComponent( resource.path ),
					clips: [ "effect" ],
					clipLoop: true,
					particles: []
				} :
				models.get( resource.path );
			if ( !model ) return null;
			const paths = [ model.glb ];
			if ( !resources.ready( model.glb ) || !resources.plan( paths ) ) return null;
			let clock = clocks.get( entity.gid );
			if ( !clock || clock.model !== model.glb ) {
				clock = { model: model.glb, started: seconds, modifierId: allocate() };
				clocks.set( entity.gid, clock );
			}
			const actor: CharacterActor = {
				gid: entity.gid,
				modifierId: clock.modifierId,
				model: model.glb,
				clip: model.clips.includes( "stand" ) ? "stand" : model.clips[0] ?? "",
				time: Math.max( 0, seconds - clock.started ),
				loop: model.clipLoop,
				scale: 1,
				pickable: false,
				pose: {
					regionId: entity.regionId,
					x: entity.x,
					y: entity.y,
					z: entity.z,
					yaw: characterHeadingYaw( entity.heading )
				}
			};
			return { actor, paths, particles: model.particles };
		},
		/*
		================
		reset

		Session reset retains immutable catalogs and retires all installations.
		================
		*/
		reset() {
			clocks.clear();
		},
		/*
		================
		dispose
		================
		*/
		dispose() {
			loaded.clear();
			clocks.clear();
			records.clear();
			models.clear();
		}
	};
}
