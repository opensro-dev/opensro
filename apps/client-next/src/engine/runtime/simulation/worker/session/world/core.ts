/*
===========================================================================

core.ts - the admitted-world owner: entities plus gameplay, one command path

Deterministic: time, bootstrap, commands and packets are supplied by the
parent session; this owner has no transport, wall clock or randomness. It
routes each server frame to gameplay first and entities second, and each UI
command through gameplay with its target resolved from the entity table.

===========================================================================
*/
import {
	isNameColorGuard,
	jobItemType,
	equipmentHoldType,
	type NameColorContext
} from "@/engine/foundation/gameplay/name-color";
import { fortressActive } from "@/engine/foundation/gameplay/fortress";
import type { EntityState } from "@/engine/contracts/world";
import { travelMode, resetTravelRegion, gateRequest, isReturnScroll } from "@/engine/foundation/gameplay/travel";
import { commerceReferences } from "@/engine/foundation/gameplay/commerce";
import {
	hoverAttack,
	petPlayerAttack,
	playerInteraction,
	skillTargetAdmission
} from "@/engine/foundation/gameplay/player-attack";
import { resolveNativeNotice } from "@/engine/foundation/gameplay/native-notice";
import { createEntities } from "./entities/entities";
import { createGameplay } from "./gameplay/gameplay";
import type { WireFrame } from "@/engine/contracts/network";
import type { GameplayCommand } from "@/engine/contracts/gameplay";

// PLAYER_ATTACK_LEVEL_NOTICE is 693F1A's ShowSystemNotification(4, 0x16).
const PLAYER_ATTACK_LEVEL_NOTICE = 0x16;
/*
================
createWorldCore
================
*/
export function createWorldCore( send: ( frame: WireFrame ) => void ) {
	const entities = createEntities(
			( pose, reference, cursor ) => gameplay.surface( pose, reference, undefined, cursor ),
			event => gameplay.entityLifecycle( event ),
			nameContext,
			( from, to, query ) => gameplay.clipMovement( from, to, query )
		),
		gameplay = createGameplay(
			send,
			( handle, at ) => entities.publish( { kind: "ui-sound", handle, at } ),
			event => entities.publish( event ),
			gid => entities.read( gid ),
			( cue, at ) => entities.publish( { kind: "item-sound", cue, at } )
		);
	gameplay.bindReferences( {
		country: refObjId => entities.characterCountry( refObjId ),
		playerModels: country => entities.playerModels( country ),
		item: refObjId => entities.itemReference( refObjId ),
		monster: codename => entities.monsterReference( codename )
	} );

	const capeTeams = new Map<number, number>();
	let nameTimer: number | undefined, nameClock = 0;
	/*
================
nameContext
The name-colour inputs for the local player, or undefined before it spawns.
================
	*/
	/*
================
playerAttack

693E50 for a click on another player, against the selection before the
click: the attack to issue, or nothing (a refusal notice is published).
A mounted player issues no attack on a player.
================
	*/
	function playerAttack( gid: number, alt: boolean ): GameplayCommand | undefined {
		const target = entities.read( gid ), context = nameContext(), local = entities.read( gameplay.localIdentity() );
		if ( !target || !context || !local || local.mountedOn ) return undefined;
		const decision = playerInteraction( target, context, {
			selected: gameplay.skillTarget() === gid,
			alt,
			guildWar: (context.social.wars?.length ?? 0) > 0
		} );
		if ( decision.kind === "low-level" ) {
			const notice = resolveNativeNotice( 4, PLAYER_ATTACK_LEVEL_NOTICE, { pkProhibited: false } );
			if ( notice.kind === "notice" ) gameplay.notice( notice.notice );
			return undefined;
		}
		return decision.kind === "attack" ? { kind: "attack", gid } : undefined;
	}
	/*
================
petAttackAdmitted

6A2350 case 2 for a player target; a refusal publishes its notice.
================
	*/
	function petAttackAdmitted( gid: number ): boolean {
		const target = entities.read( gid ), context = nameContext();
		if ( !target || !context ) return false;
		const decision = petPlayerAttack( target, context, false );
		if ( decision.kind === "low-level" ) {
			const notice = resolveNativeNotice( 4, PLAYER_ATTACK_LEVEL_NOTICE, { pkProhibited: false } );
			if ( notice.kind === "notice" ) gameplay.notice( notice.notice );
		}
		return decision.kind === "attack";
	}
	/*
================
skillAtTargetAdmitted

6FCD50 for an offensive skill aimed at a player or a pet; a refusal
publishes its category-4 notice (player-attack.ts).
================
	*/
	function skillAtTargetAdmitted( gid: number, skill: number, alt: boolean ): boolean {
		const target = entities.read( gid ), context = nameContext();
		if ( !target || !context ) return true;
		const decision = skillTargetAdmission( target, context, alt, gameplay.offensiveSkill( skill ), {
			entity: entities.read,
			rider: entities.rider
		} );
		if ( decision.kind === "notice" ) {
			const notice = resolveNativeNotice( 4, decision.code, { pkProhibited: false } );
			if ( notice.kind === "notice" ) gameplay.notice( notice.notice );
		}
		return decision.kind === "cast";
	}
	function nameContext( spawning?: EntityState ): NameColorContext | undefined {
		const local = spawning?.kind === "local-player" ? spawning : entities.read( gameplay.localIdentity() );
		if ( !local ) return;
		const { localLevel, ...inputs } = gameplay.nameInputs( nameClock );
		return {
			...inputs,
			local: {
				...local,
				// 693EF9 and 6A2350 read CICUser +0x820, which every level-up
				// updates; the entity's own level is its last spawn row's.
				level: localLevel ?? local.level,
				holdType: local.holdType ?? equipmentHoldType( inputs.localItem?.typeFlags )
			},
			capeTeam: id => capeTeams.get( id )
		};
	}
	/*
================
admitCape
ItemParam2 is family-dependent (it may be fractional or negative for other
items). Only PvP capes own a team value; keep strict validation for them.
================
	*/
	function admitCape( id: number, typeFlags: number, value: unknown ) {
		if ( jobItemType( typeFlags ) !== 5 || value === undefined ) return;
		if ( typeof value !== "number" || !Number.isInteger( value ) || value < 0 || value > 0xffffffff ) {
			throw Error( "Invalid PvP cape parameter" );
		}
		capeTeams.set( id, value );
	}
	let loadingMode: import("@/engine/contracts/world").WorldTravel["mode"] = 0;
	let travelRevision = 0;
	let pendingTravel: import("@/engine/contracts/world").WorldTravel | null = null;
	let awaitingTravelBootstrap = false;
	let publishedShop: import("@/engine/contracts/gameplay").GameplayState["shop"];
	let publishedCatalog: import("@/engine/contracts/gameplay").GameplayState["skillCatalog"],
		publishedSocial: import("@/engine/contracts/gameplay").GameplayState["social"];
	/*
================
invalidateProjection
Forces the next gameplay publish to resend the large, rarely changing
fields (shop, skill catalog, social).
================
	*/
	function invalidateProjection() {
		publishedShop = undefined;
		publishedCatalog = undefined;
		publishedSocial = undefined;
	}
	/*
================
receive
One server frame: travel resets, gameplay, then entities, then the
cross-owner follow-ups (name colours, displacements, cancellations).
================
	*/
	function receive( frame: WireFrame, now: number ) {
		nameClock = now;
		if ( frame.opcode === 14 ) {
			const refs = commerceReferences( frame.payload );
			gameplay.references( refs );
			entities.references( refs );
			for ( const row of refs ) admitCape( row.refObjId, row.typeFlags, row.tooltip?.fields.itemParam2_2a0 );
			return;
		}
		const region = resetTravelRegion( frame );
		if ( region !== null ) {
			nameTimer = undefined;
			awaitingTravelBootstrap = true;
			gameplay.resetWorld();
			entities.receive( frame, now );
			pendingTravel = { mode: loadingMode, region, revision: ++travelRevision };
			entities.publish( { kind: "travel", travel: pendingTravel } );
			return;
		}
		const localGid = gameplay.localIdentity(),
			itemCooldown = frame.opcode === 0xb5bd ? gameplay.itemUseCooldown( frame.payload[1]! ) : undefined;
		const chatSender =
			frame.opcode === 0x3667 && frame.payload.length >= 5 && (frame.payload[0] === 1 || frame.payload[0] === 3) ?
				entities.read(
					new DataView( frame.payload.buffer, frame.payload.byteOffset, frame.payload.byteLength ).getUint32(
						1,
						true
					)
				) :
				undefined;
		const oldNameContext = nameContext(), oldSocial = gameplay.nameInputs().social;
		const stallReceipt: { title?: string; } | undefined = frame.opcode === 0xb1a8 ? {} : undefined;
		if ( !gameplay.receive( frame, now, chatSender, stallReceipt ) ) {
			entities.receive( frame, now );
		}
		// The owner's edit receipt carries no title; gameplay resolves the
		// accepted request before the entity journal publishes it.
		if ( stallReceipt?.title !== undefined ) entities.renameStall( localGid, stallReceipt.title );
		const social = gameplay.nameInputs().social;
		if ( frame.opcode === 0x35d6 ) entities.recolor( e => social.members.some( m => m.name === e.name ) );
		if ( frame.opcode === 0x3e58 ) {
			const type = frame.payload[0];
			if ( type === 1 || type === 3 && social.leader === 0 ) {
				entities.recolor( e => oldSocial.members.some( m => m.name === e.name ), true, oldNameContext );
			} else if ( type === 2 ) {
				entities.recolor( e =>
					social.members.some( m => m.name === e.name && !oldSocial.members.some( old => old.id === m.id ) )
				);
			} else if ( type === 3 ) {
				entities.recolor( e =>
					oldSocial.members.some( m => m.name === e.name && !social.members.some( next => next.id === m.id ) )
				);
			}
		}
		if (
			frame.opcode === 0x32bb ||
			frame.opcode === 0x3b29 && (frame.payload[0] === 0x19 || frame.payload[0] === 0x1c)
		) entities.recolor( e => e.kind === "player" || e.kind === "local-player" );
		if ( frame.opcode === 0x376f ) {
			const local = entities.read( localGid ),
				effective = local?.mountedOn ? entities.read( local.mountedOn ) : local;
			if ( effective ) gameplay.speeds( effective, now );
		}
		if (
			frame.opcode === 0x3122 && frame.payload.length === 6 && frame.payload[4] === 0 && frame.payload[5] === 2
		) {
			gameplay.die(
				new DataView( frame.payload.buffer, frame.payload.byteOffset, 6 ).getUint32( 0, true ),
				now
			);
		}
		if ( frame.opcode === 0x314d ) {
			// CPSMission_OnTargetActionState0x314D (7786E0): quest id, then the
			// capture result (1 caught, 2 failed; v1.188 sends it as 0x30DC).
			if ( frame.payload.length !== 5 ) throw Error( "Invalid capture result" );
			if ( frame.payload[4] === 1 ) entities.markCaptured( gameplay.selectedTarget() );
		}
		for ( const displacement of gameplay.takeDisplacements() ) {
			const entity = entities.read( displacement.gid );
			if ( !entity ) continue;
			const resolved = gameplay.constrainDisplacement( displacement, {
				regionId: entity.regionId,
				x: entity.x,
				y: entity.y,
				z: entity.z,
				angle: entity.heading
			}, now );
			entities.displace( resolved, now );
			const localArrival = gameplay.displace( resolved, now );
			if ( resolved.kind === 8 ) {
				const arrival = localArrival ?? entities.castArrival( resolved.token );
				if ( arrival !== undefined ) gameplay.guidedArrival( resolved.token, arrival );
			}
		}
		for ( const token of gameplay.takeCancellations() ) {
			entities.cancelCast( token, now );
			gameplay.cancelCast( token, now );
		}
		const selectedMode = travelMode( frame, localGid, itemCooldown );
		if ( selectedMode !== null ) loadingMode = selectedMode;
		if ( frame.opcode === 0x30e3 || frame.opcode === 0xb2f5 ) {
			const v = new DataView( frame.payload.buffer, frame.payload.byteOffset, frame.payload.byteLength );
			const e = entities.read( v.getUint32( frame.opcode === 0x30e3 ? 16 : 0, true ) );
			if ( e?.kind === "local-player" ) {
				// A rider's position belongs to its mount (0x85E000): entities
				// applied the correction to the mount's row, and the rider's own
				// row still holds where it mounted. Correct from the mover.
				const mover = e.mountedOn ? entities.read( e.mountedOn ) : undefined;
				gameplay.correct(
					mover ?
						{ ...e, regionId: mover.regionId, x: mover.x, y: mover.y, z: mover.z, heading: mover.heading } :
						e,
					now
				);
			}
		}
		if ( frame.opcode === 0x35c7 ) {
			const entity = entities.read(
				new DataView( frame.payload.buffer, frame.payload.byteOffset, 4 ).getUint32( 0, true )
			);
			if ( entity?.kind === "local-player" && !entity.mountedOn ) gameplay.heading( entity.heading );
		}
		if ( frame.opcode === 0x32a6 ) {
			const gid = new DataView( frame.payload.buffer, frame.payload.byteOffset, frame.payload.byteLength )
				.getUint32( 0, true );
			const entity = entities.read( gid );
			if ( entity ) {
				gameplay.seed( entity );
				nameTimer = now + 3000;
			}
		}
	}
	return {
		/*
================
bootstrap

resumed: the transport resumed this character's session (world.ts), so
the entry continues it like the one after a world transfer.
================
		*/
		bootstrap( value: unknown, resumed = false ) {
			nameTimer = undefined;
			capeTeams.clear();
			for (
				const row of (value as {
					refItemSnapshot?: {
						refObjId: number;
						typeFlags: number;
						nativeFields?: Record<string, number>;
					}[];
				}).refItemSnapshot ?? []
			) admitCape( row.refObjId, row.typeFlags, row.nativeFields?.itemParam2_2a0 );
			if ( !pendingTravel ) loadingMode = 0;
			invalidateProjection();
			const travel = awaitingTravelBootstrap;
			entities.bootstrap( value, travel );
			awaitingTravelBootstrap = false;
			gameplay.bootstrap( value, travel || resumed );
			if ( pendingTravel ) entities.publish( { kind: "travel", travel: pendingTravel } );
		},
		receive,
		notice: gameplay.notice,
		chatBlocks: gameplay.chatBlocks,
		options: gameplay.options,
		/*
================
command
UI and quickslot commands. A skill aims at the newest selection intent
(gameplay.skillTarget) before its target is read from the entity table.
================
		*/
		command( command: GameplayCommand, now: number ) {
			if ( command.kind === "travel-gate" || command.kind === "travel-instance" ) {
				const frame = gateRequest( command );
				if ( !entities.read( command.gid ) ) throw Error( "Travel source is not admitted" );
				loadingMode = 2;
				send( frame );
				return;
			}
			// The worker chooses the shortcut's item: its table already holds every
			// despawn and grant the server sent before the press (pickup-nearest.ts).
			if ( command.kind === "pickup-nearest" ) {
				const gid = gameplay.pickupNearest( entities.groundItems() );
				if ( !gid ) return;
				command = { kind: "pickup", gid };
			}
			if ( command.kind === "player-interact" ) {
				const attack = playerAttack( command.gid, command.alt );
				if ( !attack ) return;
				command = attack;
			}
			// 6A2350 case 2: a pet sent at a player needs the owner's own
			// admission (player-attack.ts); a monster needs none.
			if (
				command.kind === "cos-pet-attack" && entities.read( command.gid )?.kind === "player" &&
				!petAttackAdmitted( command.gid )
			) {
				return;
			}
			const itemType = command.kind === "item-use" ? gameplay.itemUseType( command.slot ) : undefined;
			// The UI's snapshot target trails a fresh click by one grant round trip.
			if ( command.kind === "skill" ) {
				const { gid: _snapshot, ...press } = command, gid = gameplay.skillTarget();
				command = gid ? { ...press, gid } : press;
				if ( gid && !skillAtTargetAdmitted( gid, command.skillId, command.alt ?? false ) ) return;
			}
			const target = "gid" in command && command.gid ? entities.read( command.gid ) : undefined;
			gameplay.command( command, now, target, entities.read( gameplay.localIdentity() ) );
			if ( command.kind === "navigation" ) entities.groundSpawns();
			if ( itemType !== undefined && isReturnScroll( itemType ) ) loadingMode = 2;
		},
		/*
================
step
Advances entities and gameplay, then publishes the changed gameplay state.
================
		*/
		step( now: number, advance = true ) {
			if ( advance ) {
				nameClock = now;
				if ( nameTimer !== undefined && now >= nameTimer ) {
					const war = fortressActive( gameplay.nameInputs().fortress );
					entities.recolor( e =>
						e.kind === "local-player" || e.kind === "player" ||
						war && ([ "cos", "monster" ].includes( e.kind ) || isNameColorGuard( e ))
					);
					nameTimer = now + 3000;
				}
				entities.step( now );
				gameplay.step( now, entities.read( gameplay.localIdentity() ) );
				// 6875F0 judges the hovered player or pet every tick; the verdicts
				// follow party, PvP state and the attacked-name window as they change.
				const hoverContext = nameContext();
				if ( hoverContext ) {
					entities.refreshHoverAttack( e =>
						hoverAttack( e, hoverContext, { entity: entities.read, rider: entities.rider } )
					);
				}
				for ( const token of gameplay.takeCancellations() ) {
					entities.cancelCast( token, now );
					gameplay.cancelCast( token, now );
				}
			}
			const state = gameplay.take();
			if ( state ) {
				const { skillCatalog, social, shop, ...dynamic } = state;
				entities.publish( {
					kind: "gameplay",
					state: {
						...dynamic,
						...(shop !== publishedShop ? { shop } : {}),
						...(skillCatalog !== publishedCatalog ? { skillCatalog } : {}),
						...(social !== publishedSocial ? { social } : {})
					}
				} );
				publishedShop = shop;
				publishedCatalog = skillCatalog;
				publishedSocial = social;
			}
		},
		readyRevision: () => pendingTravel?.revision ?? 0,
		/*
================
travelReady
Retail 0x72902a restores destination mode after the loading UI retires.
================
		*/
		travelReady() {
			awaitingTravelBootstrap = false;
			pendingTravel = null;
			loadingMode = 2;
			gameplay.enterMusic();
		},
		/*
================
clear
================
		*/
		clear() {
			awaitingTravelBootstrap = false;
			nameTimer = undefined;
			capeTeams.clear();
			pendingTravel = null;
			loadingMode = 0;
			invalidateProjection();
			gameplay.reset();
			entities.clear();
		},
		/*
================
synchronized
================
		*/
		synchronized() {
			return entities.synchronized();
		},
		/*
================
count
================
		*/
		count() {
			return entities.count();
		},
		/*
================
take
================
		*/
		take() {
			return entities.take();
		},
		/*
================
ack
================
		*/
		ack( sequence: number ) {
			entities.ack( sequence );
		},
		/*
================
dispose
================
		*/
		dispose() {
			nameTimer = undefined;
			capeTeams.clear();
			entities.dispose();
			gameplay.dispose();
		}
	};
}
