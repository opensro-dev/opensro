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
import { isGoldHeapType, isSurvivalPowerEgg } from "@/engine/foundation/gameplay/ground-item";
import { defaultWearFrozen } from "@/engine/foundation/animation/default-wear-policy";
import { assembleEquipmentAppearance } from "@/engine/foundation/animation/equipment-appearance";
import { readyThenWarm, suitBagKey, suitSocketParts } from "@/engine/foundation/animation/suit-socket";
import { stallAttachments } from "@/engine/foundation/animation/emote";
import { STALL_TITLE_MODE } from "@/engine/foundation/gameplay/interaction-approach";
import type { ModelParticle } from "@/engine/foundation/animation/model-particles";
import { groundVisualClock, advanceGroundVisual } from "@/engine/foundation/animation/ground-visual";
import { referenceAppearanceItems } from "@/engine/foundation/animation/reference-appearance";
import { characterHeadingYaw } from "@/engine/foundation/math/angles";
import type { EntityState } from "@/engine/contracts/world";
import type { ActorAppearance, ActorFrame, ActorOwner, ActorPass, Auxiliary } from "./internal/presentation-contract";

// How long a character that was never drawn waits, unseen, for its worn
// parts before it shows its bare body after all. The original loads a
// character's parts with the character (synchronously), so it never shows
// one undressed; this port loads them one at a time, and a crowd seen for
// the first time stood naked for seconds. A part that keeps failing, or a
// residency budget that never admits it, must not hide the wearer for good.
const OUTFIT_WAIT_SECONDS = 5;
// Waits of characters that left before they were drawn are dropped once the
// book holds this many, oldest first.
const MAX_OUTFIT_WAITS = 512;

/*
================
createActorAppearance
================
*/
export function createActorAppearance( owner: ActorOwner ) {
	// The local wearer's suit-socket candidates' parts (suitSocketParts),
	// rebuilt with its appearance and warmed every frame.
	let socketWarm: readonly string[] = [];
	// When each character still waiting for its first dressed frame began
	// to wait (outfitWait), by gid.
	const outfitWaits = new Map<number, number>();
	/*
	================
	outfitWait

	True while a character that has never been drawn should stay unseen
	until its worn parts are in: none of them has failed, and it has waited
	less than OUTFIT_WAIT_SECONDS.
	================
	*/
	function outfitWait( gid: number, failing: boolean, seconds: number ) {
		let since = outfitWaits.get( gid );
		if ( since === undefined ) {
			if ( outfitWaits.size >= MAX_OUTFIT_WAITS ) outfitWaits.delete( outfitWaits.keys().next().value! );
			since = seconds;
			outfitWaits.set( gid, since );
		}
		if ( seconds - since < OUTFIT_WAIT_SECONDS && !failing ) return true;
		outfitWaits.delete( gid );
		return false;
	}

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
					egg = isSurvivalPowerEgg( entity.refObjId, entity.regionId, gameplay?.fortress?.worldId ?? 0 ) ?
						published.catalog.get( 5000001 ) :
						undefined,
					baseDrop = published.dropModels[item?.dropModelPath ?? ""],
					drop = egg ? { glb: egg.glb, clips: egg.clips, clipLoop: true, ambientParticles: [] } : baseDrop;
				if ( !drop ) {
					if ( published.manifest === published.manifests.length ) {
						throw Error( "Missing authored drop model for item " + entity.refObjId );
					}
					return null;
				}
				const gold = isGoldHeapType( entity.groundItem.typeFlags );
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
				// Port-only sealed pet gear keeps its collar mesh and uses the native rare chest glow.
				const glow = /^ITEM_COS_PET_.*_(STAR|MOON|SUN)$/.test( item?.codename ?? "" ) ?
					published.dropModels["item/etc/drop_ch_equip_rare.bsr"] :
					model;
				if ( !glow ) throw Error( "Missing native sealed pet drop glow" );
				if ( glow.ambientParticles?.length ) {
					particleHolders.push( {
						actor: next.get( entity.gid )!,
						particles: glow.ambientParticles
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
					// An emote or a stall keeper's greeting empties the hands (emote.ts).
					const attachmentsHidden = !!presentationState.idleStates.get( entity.gid )?.attachmentsHidden ||
						stallAttachments(
							player,
							entity.appearanceState?.[6] === STALL_TITLE_MODE,
							!!states.get( entity.gid )?.dead
						);
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
					const enchantSparks = renderer.enchantSparks?.() === true;
					// Port-only, not native: the server's SRO_JOB_SUIT_HIDES_ARMOR.
					const jobSuitHidesArmor = gameplay?.jobSuitHidesArmor === true;
					// Port-only, not native: the server's SRO_MOONLIT_THIEF_SUIT.
					const moonlitThiefSuit = gameplay?.moonlitThiefSuit === true;
					const signature = fortressIndex + ":" + Number( freezeWear ) + ":" + Number( enchantSparks ) + ":" +
						Number( jobSuitHidesArmor ) + ":" + Number( moonlitThiefSuit ) + ":" +
						Number( entity.mountedOn !== undefined ) + ":" + Number( hwanHair ) + ":" +
						Number( weaponHidden ) + ":" +
						Number( attachmentsHidden ) + ":" +
						wornSignature( equipment ) + "|" + avatarSignature( avatars ) +
						(entity.gid === gameplay?.localGid ? "|" + suitBagKey( equipment ) : "");
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
							attachmentsHidden,
							fortressIndex,
							player,
							ownerless: false,
							committedWear: committedWear?.resource === resource ? committedWear.keys : [],
							freezeWear,
							enchantSparks,
							jobSuitHidesArmor,
							moonlitThiefSuit
						} );
						// The wearer's suit and cape candidates: the parts each would
						// wear in the suit socket are warmed now, so a dress swaps the
						// outfit the moment it lands instead of a load later.
						if ( entity.gid === gameplay?.localGid ) {
							socketWarm = suitSocketParts( equipment, candidate =>
								assembleEquipmentAppearance( {
									resource,
									dress: published.dress,
									equipment: candidate,
									avatars,
									hwanHair,
									mounted: entity.mountedOn !== undefined,
									weaponHidden,
									attachmentsHidden,
									fortressIndex,
									player,
									ownerless: false,
									committedWear: [],
									freezeWear,
									enchantSparks,
									jobSuitHidesArmor,
									moonlitThiefSuit
								} ).parts.map( part => part.model ) );
						}
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
						// The wearer's worn parts own the next free load
						// slot: a fresh equip must not wait behind
						// unrelated character assets.
						if ( entity.gid === gameplay?.localGid ) resources.prioritize( appearance.dependencies );
					}
					const parts = appearance.parts;
					// The worn parts first: the wearer's suit-socket candidates are
					// warmed only once its own outfit is ready (suit-socket.ts).
					const ready = readyThenWarm(
						resources,
						parts.map( part => part.model ),
						entity.gid === gameplay?.localGid ? socketWarm : []
					);
					// A character never drawn before waits unseen for its parts
					// instead of showing its bare body (outfitWait); one already
					// drawn keeps its previous look (fallback).
					let failing = false;
					for ( const part of parts ) if ( !failing && resources.failed( part.model ) ) failing = true;
					if ( !ready && !output.displayed.has( entity.gid ) && outfitWait( entity.gid, failing, seconds ) ) {
						return null;
					}
					if ( ready ) outfitWaits.delete( entity.gid );
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
				output.failureCause = error;
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
