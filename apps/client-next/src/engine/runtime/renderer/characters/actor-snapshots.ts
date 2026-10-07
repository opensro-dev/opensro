/*
===========================================================================

actor-snapshots.ts - the renderer's own copies of published character actors

Producers may reuse and mutate their actor records after publishing them, so
the renderer admits each frame into snapshots it owns: one row per gid,
updated in place, holding the model references that drive residency. Every
optional field is copied explicitly, so a field a producer drops is dropped
here too instead of surviving from an older frame.

===========================================================================
*/
import type { CharacterActor, CharacterLayer } from "@/engine/contracts/character";
import { CHARACTER_ACTORS } from "@/engine/foundation/animation/character-budget";
type Mutable<T> = { -readonly [K in keyof T]: T[K]; };
type Actor = Mutable<CharacterActor>;
/*
================
copyVector

Copy a three-vector into an owned target, allocating only when there is none.
================
*/
function copyVector(
	target: readonly [number, number, number] | undefined,
	source: readonly [number, number, number] | undefined
): readonly [number, number, number] | undefined {
	if ( !source ) return undefined;
	if ( !target ) return [ source[0], source[1], source[2] ];
	const owned = target as [number, number, number];
	owned[0] = source[0];
	owned[1] = source[1];
	owned[2] = source[2];
	return target;
}
/*
================
createActorSnapshots

Renderer-owned snapshots. Producers may reuse/mutate their records after admission.
================
*/
export function createActorSnapshots() {
	const index = new Map<number, Actor>(), seen = new Set<number>(), rows: Actor[] = [];
	// Snapshot admission owns model references. Pose/order updates cannot change
	// residency; only first/last references advance its invalidation revision.
	const modelReferences = new Map<string, number>();
	let created = 0, modelRevision = 0;
	/*
	================
	reference

	Count one model reference up or down; first and last references bump
	the revision.
	================
	*/
	function reference( model: string, delta: number ) {
		const count = (modelReferences.get( model ) ?? 0) + delta;
		if ( count === 0 ) {
			modelReferences.delete( model );
			modelRevision++;
		} else {
			modelReferences.set( model, count );
			if ( count === 1 && delta === 1 ) modelRevision++;
		}
	}
	/*
	================
	copy

	Bring one owned snapshot up to its source actor, reusing nested storage.
	================
	*/
	function copy( target: Actor, source: CharacterActor ) {
		target.modelAnimation = source.modelAnimation ?
			{
				...source.modelAnimation,
				restarted: source.modelAnimation.restarted.map( s => ({ ...s }) ),
				selected: source.modelAnimation.selected ? { ...source.modelAnimation.selected } : null,
				dispatch: source.modelAnimation.dispatch.map( row => ({
					selector: { ...row.selector },
					ranges: row.ranges.map( range => [ range[0], range[1] ] as const )
				}) )
			} :
			undefined;
		target.animationLod = source.animationLod ? { ...source.animationLod } : undefined;
		target.deferredParticle = source.deferredParticle ? { ...source.deferredParticle } : undefined;
		target.modifierId = source.modifierId;
		target.groundItem = source.groundItem;
		target.previewClip = source.previewClip;
		target.gid = source.gid;
		target.model = source.model;
		target.clip = source.clip;
		target.time = source.time;
		target.loop = source.loop;
		target.scale = source.scale;
		target.shadowSize = source.shadowSize;
		target.shadowAttachment = source.shadowAttachment;
		target.blindable = source.blindable;
		target.drawGeometry = source.drawGeometry;
		target.height = source.height;
		target.pickable = source.pickable;
		target.bodyVolume = source.bodyVolume ? { ...source.bodyVolume } : undefined;
		target.opacity = source.opacity;
		target.effectEntity = source.effectEntity;
		target.mountedOn = source.mountedOn;
		target.emissionEnd = source.emissionEnd;
		Object.assign( target.pose, source.pose );
		if ( source.layers ) {
			const layers = (target.layers ?? []) as Mutable<CharacterLayer>[];
			for ( let i = 0; i < source.layers.length; i++ ) {
				const layer = source.layers[i]!;
				if ( layers[i] ) Object.assign( layers[i]!, layer, { activation: layer.activation } );
				else layers[i] = { ...layer };
			}
			layers.length = source.layers.length;
			target.layers = layers;
		} else target.layers = undefined;
		if ( source.attachment ) {
			const old = target.attachment,
				rotation = source.attachment.rotation ? (old?.rotation ?? new Float32Array( 16 )) : undefined,
				offset = old?.offset as [number, number, number] | undefined;
			if ( rotation ) rotation.set( source.attachment.rotation! );
			if ( old && offset ) {
				for ( let i = 0; i < 3; i++ ) offset[i] = source.attachment.offset[i]!;
				Object.assign( old, {
					...source.attachment,
					basis: source.attachment.basis,
					modelScale: source.attachment.modelScale,
					root: source.attachment.root,
					rootIfMissing: source.attachment.rootIfMissing,
					keepRotation: source.attachment.keepRotation,
					ground: source.attachment.ground,
					facing: source.attachment.facing,
					rotation,
					offset
				} );
			} else target.attachment = { ...source.attachment, rotation, offset: [ ...source.attachment.offset ] };
		} else target.attachment = undefined;
		if ( source.effectBasis ) {
			if ( target.effectBasis ) {
				const basis = target.effectBasis as Mutable<NonNullable<CharacterActor["effectBasis"]>>;
				for ( let i = 0; i < 9; i++ ) basis[i] = source.effectBasis[i]!;
			} else target.effectBasis = [ ...source.effectBasis ];
		} else target.effectBasis = undefined;
		if ( source.effectRotation ) {
			if ( target.effectRotation ) Object.assign( target.effectRotation, source.effectRotation );
			else target.effectRotation = { ...source.effectRotation };
		} else target.effectRotation = undefined;
		if ( source.pointLight ) {
			const value = source.pointLight,
				old = target.pointLight as Mutable<NonNullable<CharacterActor["pointLight"]>> | undefined;
			if ( old ) {
				Object.assign( old.pose, value.pose );
				copyVector( old.ambient, value.ambient );
				copyVector( old.diffuse, value.diffuse );
				old.attenuation = value.attenuation;
				old.range = value.range;
			} else {
				target.pointLight = {
					...value,
					pose: { ...value.pose },
					ambient: [ ...value.ambient ],
					diffuse: [ ...value.diffuse ]
				};
			}
		} else target.pointLight = undefined;
		if ( source.bloodEffects ) {
			if ( target.bloodEffects ) {
				const blood = target.bloodEffects as [string | null, string | null];
				blood[0] = source.bloodEffects[0];
				blood[1] = source.bloodEffects[1];
			} else target.bloodEffects = [ ...source.bloodEffects ];
		} else target.bloodEffects = undefined;
		target.materialTint = copyVector( target.materialTint, source.materialTint );
		target.heightFactor = source.heightFactor;
		target.effectBaseScale = source.effectBaseScale;
		target.absoluteEffectScale = source.absoluteEffectScale;
		if ( source.effectAnchor ) {
			const old = target.effectAnchor as Mutable<NonNullable<CharacterActor["effectAnchor"]>> | undefined;
			if ( old ) {
				old.bone = source.effectAnchor.bone;
				copyVector( old.offset, source.effectAnchor.offset );
			} else target.effectAnchor = { ...source.effectAnchor, offset: [ ...source.effectAnchor.offset ] };
		} else target.effectAnchor = undefined;
	}
	/*
	================
	finitePointLight

	The same finiteness test as spreading every value into one array, without
	building that array for each lit actor on every publication.
	================
	*/
	function finitePointLight( light: NonNullable<CharacterActor["pointLight"]> ) {
		if ( !Number.isFinite( light.attenuation ) || !Number.isFinite( light.range ) ) return false;
		for ( const value of light.ambient ) if ( !Number.isFinite( value ) ) return false;
		for ( const value of light.diffuse ) if ( !Number.isFinite( value ) ) return false;
		// Own properties only, as Object.values read them.
		for ( const key in light.pose ) {
			if ( !Object.hasOwn( light.pose, key ) ) continue;
			if ( !Number.isFinite( light.pose[key as keyof typeof light.pose] ) ) return false;
		}
		return true;
	}
	return {
		index,
		/*
		================
		update

		Validate a whole publication, then admit it in order up to the budget.
		================
		*/
		update( value: readonly CharacterActor[] ) {
			seen.clear();
			// Validate the entire publication before changing any admitted snapshot.
			for ( const actor of value ) {
				if (
					actor.deferredParticle &&
					(!Number.isInteger( actor.deferredParticle.offset ) || actor.deferredParticle.offset < 0 ||
						actor.deferredParticle.offset > 255 ||
						actor.deferredParticle.nightOnly !== undefined &&
							typeof actor.deferredParticle.nightOnly !== "boolean" ||
						actor.deferredParticle.lodHidden !== undefined &&
							typeof actor.deferredParticle.lodHidden !== "boolean")
				) throw Error( "Invalid deferred particle descriptor" );
			}
			for ( const actor of value ) {
				if (
					actor.animationLod &&
					(!Number.isFinite( actor.animationLod.fraction ) || actor.animationLod.fraction < 0 ||
						actor.animationLod.fraction > 1 || typeof actor.animationLod.crowded !== "boolean")
				) throw Error( "Invalid animation LOD" );
				if ( actor.modifierId !== undefined && !Number.isSafeInteger( actor.modifierId ) ) {
					throw Error( "Invalid modifier owner" );
				}
				const rotation = actor.attachment?.rotation;
				if (
					rotation &&
					(!(rotation instanceof Float32Array) || rotation.length !== 16 ||
						!rotation.every( Number.isFinite ))
				) throw Error( "Invalid particle attachment rotation" );
				if ( actor.pointLight && !finitePointLight( actor.pointLight ) ) {
					throw Error( "Invalid character point light" );
				}
				if (
					actor.materialTint &&
					(actor.materialTint.length !== 3 ||
						actor.materialTint.some( v => !Number.isFinite( v ) || v < 0 || v > 1 ))
				) throw Error( "Invalid character material tint" );
				if (
					actor.emissionEnd !== undefined && (!Number.isFinite( actor.emissionEnd ) || actor.emissionEnd < 0)
				) throw Error( "Invalid effect emission end" );
				if (
					actor.bodyVolume &&
					(!Number.isFinite( actor.bodyVolume.index ) || actor.bodyVolume.index < 0 ||
						actor.bodyVolume.index > 4 || typeof actor.bodyVolume.female !== "boolean")
				) throw Error( "Invalid character volume" );
				if (
					actor.opacity !== undefined &&
					(!Number.isFinite( actor.opacity ) || actor.opacity < 0 || actor.opacity > 1)
				) throw Error( "Invalid character opacity" );
				if ( seen.has( actor.gid ) ) throw Error( "Character identity violation" );
				seen.add( actor.gid );
			}
			const count = Math.min( value.length, CHARACTER_ACTORS );
			seen.clear();
			for ( let i = 0; i < count; i++ ) {
				const source = value[i]!;
				let row = index.get( source.gid );
				if ( !row ) {
					row = {
						...source,
						pose: { ...source.pose },
						layers: undefined,
						attachment: undefined,
						effectBasis: undefined,
						effectRotation: undefined,
						pointLight: undefined,
						bloodEffects: undefined,
						materialTint: undefined,
						effectAnchor: undefined
					};
					index.set( source.gid, row );
					created++;
					reference( source.model, 1 );
				} else if ( row.model !== source.model ) {
					reference( row.model, -1 );
					reference( source.model, 1 );
				}
				copy( row, source );
				rows[i] = row;
				seen.add( source.gid );
			}
			rows.length = count;
			for ( const gid of index.keys() ) {
				if ( !seen.has( gid ) ) {
					reference( index.get( gid )!.model, -1 );
					index.delete( gid );
				}
			}
			return rows;
		},
		modelRevision: () => modelRevision,
		stats: () => ({ created }),
		/*
		================
		reset
		================
		*/
		reset() {
			modelReferences.clear();
			modelRevision++;
			index.clear();
			seen.clear();
			rows.length = 0;
		}
	};
}
