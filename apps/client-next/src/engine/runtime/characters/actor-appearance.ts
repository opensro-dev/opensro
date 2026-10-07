/*
===========================================================================

actor-appearance.ts - an actor's resource, worn appearance and dress

The first half of the per-actor pass: skill objects and ground items are
written straight into frame.next; for a character it resolves the resource,
equipment, fortress and avatar dress and their commits. Moved verbatim from
the per-actor loop: a loop `continue` is `return null` here.

===========================================================================
*/
import { fortressAppearance } from "@/engine/foundation/animation/fortress-appearance";
import { defaultWearFrozen } from "@/engine/foundation/animation/default-wear-policy";
import { assembleEquipmentAppearance } from "@/engine/foundation/animation/equipment-appearance";
import type { ModelParticle } from "@/engine/foundation/animation/model-particles";
import { groundVisualClock, advanceGroundVisual } from "@/engine/foundation/animation/ground-visual";
import { referenceAppearanceItems } from "@/engine/foundation/animation/reference-appearance";
import { characterHeadingYaw } from "@/engine/foundation/math/angles";
import type { EntityState } from "@/engine/contracts/world";
import type { ActorAppearance, ActorFrame, ActorOwner, ActorPass, Auxiliary } from "./internal/presentation-contract";

/*
================
createActorAppearance
================
*/
export function createActorAppearance( owner: ActorOwner ) {
	/*
	================
	wornSignature

	The slot, item and plus of every worn visual slot (0..8), as the appearance
	signature compares them. Built every frame, because an in-place edit of
	the list must still change the signature, but with one string and no
	intermediate arrays.
	================
	*/
	function wornSignature( equipment: readonly { slot: number; refObjId: number; plus: number; }[] ) {
		let text = "";
		for ( const item of equipment ) {
			// The original positive test: a NaN slot is not a visual slot.
			if ( !(item.slot >= 0 && item.slot < 9) ) continue;
			if ( text ) text += ";";
			text += item.slot + "," + item.refObjId + "," + item.plus;
		}
		return text;
	}
	/*
	================
	avatarSignature

	The avatar item ids, built like wornSignature.
	================
	*/
	function avatarSignature( avatars: readonly { refObjId: number; }[] ) {
		let text = "";
		for ( let index = 0; index < avatars.length; index++ ) {
			text += (index ? ";" : "") + avatars[index]!.refObjId;
		}
		return text;
	}
	return {
		/*
		================
		resolve

		Null when the original loop moved on to the next entity.
		================
		*/
		resolve( entity: EntityState, frame: ActorFrame, pass: ActorPass ): ActorAppearance | null {
			const {
				activeSkin,
				allocateActor,
				appearances,
				concealmentSkills,
				displayedDependencies,
				effects,
				groundClocks,
				output,
				published,
				referenceAppearances,
				renderer,
				resourceFor,
				resources,
				skillObjects,
				states,
				wornEquipment,
				presentationState
			} = owner;
			const { gameplay, nativeServerName, next, normalFortressClothes, seconds } = frame;
			const { localEntity, particleHolders } = pass;
			if ( entity.skillObject ) {
				const visual = skillObjects.frame( entity, seconds, resources, {
					localGid: gameplay?.localGid ?? 0,
					effects: gameplay?.attachedEffects ?? [],
					skill: concealmentSkills( gameplay?.skillCatalog )
				} );
				if ( visual ) {
					next.set( entity.gid, visual.actor );
					displayedDependencies.set( entity.gid, visual.paths );
					if ( visual.particles.length ) {
						particleHolders.push( { actor: visual.actor, particles: visual.particles } );
					}
				}
				return null;
			}
			if ( entity.groundItem ) {
				const item = published.items[String( entity.refObjId )],
					drop = published.dropModels[item?.dropModelPath ?? ""];
				if ( !drop ) {
					if ( published.manifest === published.manifests.length ) {
						throw Error( "Missing authored drop model for item " + entity.refObjId );
					}
					return null;
				}
				const gold = (entity.groundItem.typeFlags & 0x60) === 0x60 &&
					(entity.groundItem.typeFlags & 0x780) === 0x280;
				const fanfare = gold && entity.groundItem.appear !== undefined ?
					published.dropModels["item/etc/drop_ch_money_ing.bsr"] :
					undefined;
				if ( gold && entity.groundItem.appear !== undefined && !fanfare ) {
					throw Error( "Missing native gold fanfare model" );
				}
				const paths = fanfare ? [ fanfare.glb, drop.glb ] : [ drop.glb ];
				const ready = paths.map( path => resources.ready( path ) ).every( Boolean );
				if ( !ready || !resources.plan( paths ) ) return null;
				let clock = groundClocks.get( entity.gid );
				if ( !clock ) {
					clock = {
						...groundVisualClock( seconds, !!fanfare ),
						modifierId: allocateActor(),
						duration: fanfare ? resources.duration( fanfare.glb, "stand" ) : 0
					};
					groundClocks.set( entity.gid, clock );
					advanceGroundVisual( clock, seconds, !!entity.groundItem.claimantGid, clock.duration );
				}
				if ( entity.groundItem.claimantGid ) return null;
				// 86DB40 event 100 schedules state 1 for the next 1-ms timer.
				const model = fanfare && clock.pendingModel ? fanfare : drop;
				next.set( entity.gid, {
					groundItem: true,
					modifierId: clock.modifierId,
					gid: entity.gid,
					model: model.glb,
					clip: model.clips.includes( "stand" ) ? "stand" : "",
					time: clock.time,
					loop: model.clipLoop,
					scale: 1,
					pose: {
						regionId: entity.regionId,
						x: entity.x,
						y: entity.y,
						z: entity.z,
						yaw: characterHeadingYaw( entity.heading )
					}
				} );
				displayedDependencies.set( entity.gid, paths );
				if ( model.ambientParticles?.length ) {
					particleHolders.push( {
						actor: next.get( entity.gid )!,
						particles: model.ambientParticles
					} );
				}
				return null;
			}
			// Character-info supplies native height, hit anchors and audio.
			// A retried manifest can leave models resident first; do not
			// publish incomplete bodies to next frame's effect owner.
			if ( published.manifest < published.manifests.length ) return null;
			const resource = resourceFor( entity );
			if ( !resource ) return null;
			if ( !resources.ready( resource.glb ) ) {
				const previous = output.displayed.get( entity.gid ),
					paths = displayedDependencies.get( entity.gid );
				if ( previous && paths && resources.plan( paths ) ) {
					next.set( entity.gid, previous );
					displayedDependencies.set( entity.gid, paths );
				}
				return null;
			}
			// Resolve authored EFP dependencies before starting the holder clock.
			// A cold decoder must not consume and lose a time-zero BAN key.
			let particlesReady = true;
			for ( const path of resource.animationParticlePaths ?? [] ) {
				if ( !resources.ready( path ) ) particlesReady = false;
			}
			if ( !particlesReady ) return null;
			let model = resource.glb;
			let dependencies: readonly string[] = [ resource.glb ];
			let auxiliaryCommit: readonly Auxiliary[] | undefined = [];
			let particleCommit: readonly ModelParticle[] | undefined = resource.ambientParticles ?? [];
			let overrideCommit: readonly number[] | undefined = [];
			let defaultWearCommit: readonly string[] | undefined = [];
			const fallback = () => {
				const previous = output.displayed.get( entity.gid ),
					paths = displayedDependencies.get( entity.gid );
				if ( previous && paths && resources.plan( paths ) ) {
					auxiliaryCommit = undefined;
					overrideCommit = undefined;
					particleCommit = undefined;
					defaultWearCommit = undefined;
					dependencies = paths;
					return previous.model;
				}
				return resource.glb;
			};
			try {
				const disguise = referenceAppearances.get( entity.gid );
				const skin = activeSkin( entity ), avatars = skin ? [] : entity.avatars ?? [];
				if ( skin && !skin.player ) {
					// 85C060: a monster skin is its own body; the wearer's items are set aside.
				} else if ( disguise ) {
					// CICharactor_EquipReferenceAppearance: the random look's items
					// go through the ordinary slot visuals and compound refresh.
					const parts = assembleEquipmentAppearance( {
							resource,
							dress: published.dress,
							equipment: referenceAppearanceItems(
								disguise,
								resource.codename.includes( "_MAN_" ),
								published.itemIds
							),
							avatars: [],
							hwanHair: false,
							mounted: entity.mountedOn !== undefined,
							weaponHidden: false,
							attachmentsHidden: false,
							fortressIndex: -1,
							player: entity.kind === "player" || entity.kind === "local-player",
							ownerless: false,
							committedWear: [],
							freezeWear: nativeServerName !== undefined &&
								defaultWearFrozen( published.dress.defaultWearLanguage ?? 4, nativeServerName )
						} ).parts,
						paths = [ resource.glb, ...parts.map( p => p.model ) ];
					if ( resources.plan( paths ) ) {
						model = `assembly:disguise:${resource.glb}:${JSON.stringify( parts )}`;
						renderer.setCharacterAssembly( model, resource.glb, parts );
						dependencies = paths;
					} else model = fallback();
				} else if (
					(skin || entity.gid === gameplay?.localGid || entity.equipment || avatars.length) &&
					published.manifest >= 3
				) {
					const equipment = wornEquipment( entity, gameplay );
					const weaponHidden = effects.appearance( entity.gid ).weaponHidden;
					const hwanHair = entity.appearanceState?.[2] === 1 &&
						resource.codename.startsWith( "CHAR_CH_" );
					const freezeWear = defaultWearFrozen(
						published.dress.defaultWearLanguage ?? 4,
						nativeServerName
					);
					const player = entity.kind === "player" || entity.kind === "local-player",
						local = localEntity;
					const fortressIndex = player && local ?
						fortressAppearance(
							states.get( entity.gid )?.fortressIndex ?? -1,
							local.arenaTeam ?? 255,
							entity.arenaTeam ?? 255,
							normalFortressClothes,
							gameplay?.fortress,
							gameplay?.social?.guild?.id ?? 0,
							entity.gid === gameplay?.localGid ?
								gameplay?.social?.guild?.id ?? 0 :
								entity.guildId ?? 0,
							gameplay?.social?.alliances?.map( a => a.id ) ?? []
						) :
						-1;
					const visualState = states.get( entity.gid );
					if ( visualState ) visualState.fortressIndex = fortressIndex;
					const signature = fortressIndex + ":" + Number( freezeWear ) + ":" +
						Number( entity.mountedOn !== undefined ) + ":" + Number( hwanHair ) + ":" +
						Number( weaponHidden ) + ":" +
						Number( !!presentationState.idleStates.get( entity.gid )?.attachmentsHidden ) + ":" +
						wornSignature( equipment ) + "|" + avatarSignature( avatars );
					let appearance = appearances.get( entity.gid );
					if (
						!appearance || appearance.resource !== resource ||
						appearance.dress !== published.dress ||
						appearance.items !== published.items || appearance.signature !== signature
					) {
						const committedWear = states.get( entity.gid )?.defaultWear;
						const assembly = assembleEquipmentAppearance( {
							resource,
							dress: published.dress,
							equipment,
							avatars,
							hwanHair,
							mounted: entity.mountedOn !== undefined,
							weaponHidden,
							attachmentsHidden: !!presentationState.idleStates.get( entity.gid )
								?.attachmentsHidden,
							fortressIndex,
							player,
							ownerless: false,
							committedWear: committedWear?.resource === resource ? committedWear.keys : [],
							freezeWear
						} );
						const { parts, auxiliary, defaultWear } = assembly;
						const particles: ModelParticle[] = [
							...(resource.ambientParticles ?? []),
							...assembly.particles
						];
						appearance = {
							resource,
							dress: published.dress,
							items: published.items,
							signature,
							defaultWear,
							particles,
							parts,
							auxiliary,
							avatarIds: assembly.avatarIds,
							model: `assembly:${resource.glb}:${JSON.stringify( parts )}`,
							dependencies: [ resource.glb, ...parts.map( part => part.model ) ]
						};
						appearances.set( entity.gid, appearance );
					}
					const parts = appearance.parts;
					let ready = true;
					for ( const part of parts ) {
						if ( !resources.ready( part.model ) ) {
							ready = false;
						}
					}
					if ( !ready ) {
						model = fallback();
					} else {
						auxiliaryCommit = appearance.auxiliary;
						overrideCommit = appearance.avatarIds;
						particleCommit = appearance.particles;
						defaultWearCommit = appearance.defaultWear;
					}
					if ( ready && parts.length ) {
						model = appearance.model;
						renderer.setCharacterAssembly( model, resource.glb, parts );
						dependencies = appearance.dependencies;
					}
				}
			} catch ( error ) {
				output.failure = String( error );
				model = fallback();
			}
			return {
				resource,
				model,
				dependencies,
				auxiliaryCommit,
				particleCommit,
				overrideCommit,
				defaultWearCommit
			};
		}
	};
}
