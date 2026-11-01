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
import type { EntityState, WorldEvent } from "@/engine/contracts/world";
import { travelMode, resetTravelRegion, gateRequest, isReturnScroll } from "@/engine/foundation/gameplay/travel";
import { commerceReferences } from "@/engine/foundation/gameplay/commerce";
import {
	cosInteraction,
	hoverAttack,
	petCosAttack,
	petPlayerAttack,
	playerInteraction,
	skillTargetAdmission
} from "@/engine/foundation/gameplay/player-attack";
import { resolveNativeNotice } from "@/engine/foundation/gameplay/native-notice";
import { partyInviteTarget, partyProposalPendingNotice } from "@/engine/foundation/gameplay/party-invite";
import { createEntities } from "./entities/entities";
import { createGameplay } from "./gameplay/gameplay";
import type { WireFrame } from "@/engine/contracts/network";
import type { GameplayCommand } from "@/engine/contracts/gameplay";

// PLAYER_ATTACK_LEVEL_NOTICE is 693F1A's ShowSystemNotification(4, 0x16).
const PLAYER_ATTACK_LEVEL_NOTICE = 0x16;
// MOUNT_LIFE_DEAD is the life-state byte (+0x263) of a dead object; 775B50
// leaves a dead COS's mover alone.
const MOUNT_LIFE_DEAD = 2;
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
			( from, to, query ) => gameplay.clipMovement( from, to, query ),
			( pose, owner ) => gameplay.entityBlock( pose, owner )
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
		monster: codename => entities.monsterReference( codename ),
		npcs: () => entities.npcs()
	} );

	const capeTeams = new Map<number, number>();
	let nameTimer: number | undefined, nameClock = 0;
	// Port-only, not native: the Experimental portal-arrival cue's switch
	// (experimental-options.ts); off is the native silent arrival.
	let warpArrivalSound = false;
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
cosAttack

6935F0 for a double click on a pet: the attack to issue (from the ride
when the local player is mounted, as a monster attack is), or nothing.
================
	*/
	function cosAttack( gid: number, alt: boolean ): GameplayCommand | undefined {
		const target = entities.read( gid ), context = nameContext(), local = entities.read( gameplay.localIdentity() );
		if ( !target || !context || !local ) return undefined;
		if ( !cosInteraction( target, context, alt, { entity: entities.read, rider: entities.rider } ) ) {
			return undefined;
		}
		return { kind: local.mountedOn ? "cos-attack" : "attack", gid };
	}
	/*
================
petAttackAdmitted

6A2350 case 2 for a player or pet target; a refusal publishes its notice.
================
	*/
	function petAttackAdmitted( gid: number, alt: boolean ): boolean {
		const target = entities.read( gid ), context = nameContext();
		if ( !target || !context ) return false;
		const decision = target.kind === "cos" ?
			petCosAttack( target, context, alt, { entity: entities.read, rider: entities.rider } ) :
			petPlayerAttack( target, context, alt );
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
	let bootstrapRegion: number | undefined;
	let pendingTravel: import("@/engine/contracts/world").WorldTravel | null = null;
	let awaitingTravelBootstrap = false;
	// Compactions the entity journal has dropped into a resync, as last seen
	// by take(): a moved count puts the full gameplay snapshot and the
	// pending travel inside the resync batch (see take).
	let seenResyncs = 0;
	let publishedShop: import("@/engine/contracts/gameplay").GameplayState["shop"];
	let publishedCatalog: import("@/engine/contracts/gameplay").GameplayState["skillCatalog"],
		retainedCatalog: import("@/engine/contracts/gameplay").GameplayState["skillCatalog"],
		seenCatalog: import("@/engine/contracts/gameplay").GameplayState["skillCatalog"],
		publishedGmCatalog: import("@/engine/contracts/gameplay").GameplayState["gmCatalog"],
		retainedGmCatalog: import("@/engine/contracts/gameplay").GameplayState["gmCatalog"],
		seenGmCatalog: import("@/engine/contracts/gameplay").GameplayState["gmCatalog"],
		publishedSocial: import("@/engine/contracts/gameplay").GameplayState["social"];
	/*
================
invalidateProjection
Forces the next gameplay publish to resend the large, rarely changing
fields (shop, skill catalog, GM catalog, social).
================
	*/
	function invalidateProjection() {
		publishedShop = undefined;
		publishedCatalog = undefined;
		publishedGmCatalog = undefined;
		publishedSocial = undefined;
	}
	/*
	================
	gameplayEvent

	Compose the next gameplay publication against the retained catalogues:
	the large fields (shop, skill and GM catalogues, social) ride only
	when they change, unless the projection was invalidated. Consumes the
	pending state, so a state is published exactly once - by step into
	the journal queue, or by take into a resync batch.
	================
	*/
	function gameplayEvent(): Extract<WorldEvent, { kind: "gameplay"; }> | null {
		const state = gameplay.take();
		if ( !state ) return null;
		const { skillCatalog, gmCatalog, social, shop, ...dynamic } = state;
		const catalog = skillCatalog === seenCatalog ?
			retainedCatalog ?? skillCatalog :
			retainedCatalog && sameSkillCatalog( skillCatalog, retainedCatalog ) ?
			retainedCatalog :
			skillCatalog;
		seenCatalog = skillCatalog;
		retainedCatalog = catalog;
		const catalogChanged = catalog !== publishedCatalog;
		const gm = !gmCatalog ?
			undefined :
			gmCatalog === seenGmCatalog ?
			retainedGmCatalog ?? gmCatalog :
			retainedGmCatalog && sameSkillCatalog( gmCatalog, retainedGmCatalog ) ?
			retainedGmCatalog :
			gmCatalog;
		if ( gmCatalog ) seenGmCatalog = gmCatalog;
		if ( gm ) retainedGmCatalog = gm;
		const gmChanged = Boolean( gm ) && gm !== publishedGmCatalog;
		const event: Extract<WorldEvent, { kind: "gameplay"; }> = {
			kind: "gameplay",
			state: {
				...dynamic,
				...(shop !== publishedShop ? { shop } : {}),
				...(catalogChanged ? { skillCatalog: catalog } : {}),
				...(gmChanged && gm ? { gmCatalog: gm } : {}),
				...(social !== publishedSocial ? { social } : {})
			}
		};
		publishedShop = shop;
		if ( catalogChanged ) publishedCatalog = catalog;
		if ( gmChanged ) publishedGmCatalog = gm;
		publishedSocial = social;
		return event;
	}
	/*
================
sameSkillCatalog

A bootstrap rebuilds the skill catalogue from the same reference
snapshot: new objects, the same rows. Content equality is what
"unchanged" means for the projection, so a portal entry does not
resend the catalogue beside the destination's spawn burst (F11-DC1).
The catalogue is plain data; the walk short-circuits on the first
row that differs.
================
	*/
	function sameSkillCatalog( a: unknown, b: unknown ): boolean {
		if ( a === b ) return true;
		if ( Array.isArray( a ) && Array.isArray( b ) ) {
			return a.length === b.length && a.every( ( row, i ) => sameSkillCatalog( row, b[i] ) );
		}
		if ( a && b && typeof a === "object" && typeof b === "object" ) {
			const aKeys = Object.keys( a ), bKeys = Object.keys( b );
			return aKeys.length === bKeys.length &&
				aKeys.every( key =>
					sameSkillCatalog( (a as Record<string, unknown>)[key], (b as Record<string, unknown>)[key] )
				);
		}
		return false;
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
			gameplay.referenceProgress( now );
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
			const gid = v.getUint32( frame.opcode === 0x30e3 ? 16 : 0, true ), e = entities.read( gid );
			// 775B50 halts the named object's active mover, so a correction
			// naming the mount the local player rides (a stunned transport,
			// cosabnormal StopMove) stops the rider as surely as one naming
			// the rider. A dead mount (+0x263 == 2, 777B60 case 0) is skipped.
			const local = e?.kind === "local-player" ? e : entities.read( localGid );
			const ridden = e !== undefined && e !== local && local?.kind === "local-player" &&
				local.mountedOn === gid && e.appearanceState?.[0] !== MOUNT_LIFE_DEAD;
			if ( e?.kind === "local-player" || ridden ) {
				const rider = local!;
				// A rider's position belongs to its mount (0x85E000): entities
				// applied the correction to the mount's row, and the rider's own
				// row still holds where it mounted. Correct from the mover.
				const mover = rider.mountedOn ? entities.read( rider.mountedOn ) : undefined;
				gameplay.correct(
					mover ?
						{
							...rider,
							regionId: mover.regionId,
							x: mover.x,
							y: mover.y,
							z: mover.z,
							heading: mover.heading
						} :
						rider,
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
		bootstrap( value: unknown, resumed = false, receivedAtMs = 0 ) {
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
			const entryRegion = (value as {
				localPlayerEntry?: { startProfile?: { regionId?: unknown; }; };
			})?.localPlayerEntry?.startProfile?.regionId;
			bootstrapRegion = typeof entryRegion === "number" && Number.isInteger( entryRegion ) && entryRegion >= 1 &&
					entryRegion <= 65535 ?
				entryRegion :
				undefined;
			const travel = awaitingTravelBootstrap;
			if ( !travel && !resumed ) {
				retainedCatalog = undefined;
				seenCatalog = undefined;
				retainedGmCatalog = undefined;
				seenGmCatalog = undefined;
			}
			invalidateProjection();
			entities.bootstrap( value, travel );
			awaitingTravelBootstrap = false;
			gameplay.bootstrap( value, travel || resumed, receivedAtMs );
			if ( pendingTravel ) entities.publish( { kind: "travel", travel: pendingTravel } );
		},
		receive,
		outstandingSince: gameplay.outstandingSince,
		noteInput: gameplay.noteInput,
		notice: gameplay.notice,
		chatBlocks: gameplay.chatBlocks,
		options: gameplay.options,
		experimental( value: import("@/engine/foundation/ui/experimental-options").ExperimentalGameplay ) {
			gameplay.experimental( value );
			entities.sunDropMarker( value.sunDropMarker === true );
			warpArrivalSound = value.warpArrivalSound === true;
		},
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
			if ( command.kind === "cos-interact" ) {
				const attack = cosAttack( command.gid, command.alt );
				if ( !attack ) return;
				command = attack;
			}
			// 6A2350 case 2: a pet sent at a player or a pet needs the owner's
			// own admission (player-attack.ts); a monster needs none.
			if (
				command.kind === "cos-pet-attack" &&
				(entities.read( command.gid )?.kind === "player" || entities.read( command.gid )?.kind === "cos") &&
				!petAttackAdmitted( command.gid, command.alt ?? false )
			) {
				return;
			}
			// /GetPos writes only the entity owner's body visual byte; the
			// gameplay lane never sees the command.
			if ( command.kind === "debug-body-visual" ) {
				entities.debugBodyVisual( gameplay.localIdentity() );
				return;
			}
			// /showme and /hideme write only the entity owner's shown byte;
			// the gameplay lane never sees the command.
			if ( command.kind === "debug-local-visible" ) {
				entities.debugLocalVisible( gameplay.localIdentity(), command.visible );
				return;
			}
			const itemType = command.kind === "item-use" ? gameplay.itemUseType( command.slot ) : undefined;
			// The UI's snapshot target trails a fresh click by one grant round trip.
			if ( command.kind === "skill" ) {
				const { gid: _snapshot, ...press } = command, gid = gameplay.skillTarget();
				command = gid ? { ...press, gid } : press;
				if ( gid && !skillAtTargetAdmitted( gid, command.skillId, command.alt ?? false ) ) return;
			}
			// 6FD830 / 6FDA50: an invitation goes to a player, a ridden horse's
			// rider for the horse, and the proposer's line names the invitee.
			let invitee: EntityState | null = null;
			if ( command.kind === "party-invite-named" ) {
				// 6AD990: the named player must be in view; otherwise nothing is sent.
				const named = entities.playerNamed( command.name );
				if ( !named ) return;
				command = { kind: "party-invite", gid: named.gid, options: command.options };
			}
			if ( command.kind === "party-invite" ) {
				invitee = partyInviteTarget( command.gid, gameplay.localIdentity(), {
					entity: entities.read,
					rider: entities.rider
				} );
				if ( !invitee ) return;
				command = { ...command, gid: invitee.gid };
			}
			const target = "gid" in command && command.gid ? entities.read( command.gid ) : undefined;
			gameplay.command( command, now, target, entities.read( gameplay.localIdentity() ) );
			if ( invitee ) gameplay.notice( partyProposalPendingNotice( invitee.name ) );
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
			const event = gameplayEvent();
			if ( event ) entities.publish( event );
		},
		// The owners' skipped cosmetic steps, for the world's degraded reports.
		takeDegradations: () => entities.takeDegradations(),
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
			// Port-only, not native: SND_WARP is the table's portal-out row
			// (포탈 나올 때 - the sound of coming out of the portal), but no
			// traced native caller reaches it: the gate at 0x68eafd in
			// 0x68e530 never takes the traced path to the row, so the native
			// arrival is silent. The Experimental warp-arrival row opts
			// into the cue here; off (the default) keeps that silence. The
			// cue rides the same ui-sound channel the packet handlers use;
			// the bootstrap's own packets refresh nameClock right before
			// this fires, so the cue reads fresh.
			if ( warpArrivalSound ) {
				entities.publish( { kind: "ui-sound", handle: "SND_WARP", at: nameClock } );
			}
			gameplay.enterMusic();
			gameplay.worldReady();
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
			bootstrapRegion = undefined;
			pendingTravel = null;
			loadingMode = 0;
			retainedCatalog = undefined;
			seenCatalog = undefined;
			retainedGmCatalog = undefined;
			seenGmCatalog = undefined;
			invalidateProjection();
			gameplay.reset();
			entities.clear();
		},
		/*
================
region
================
		*/
		region() {
			if ( pendingTravel ) return pendingTravel.region;
			const local = entities.read( gameplay.localIdentity() );
			const mover = local?.mountedOn ? entities.read( local.mountedOn ) : local;
			return mover?.regionId ?? local?.regionId ?? bootstrapRegion;
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
			const batch = entities.take();
			if ( !batch || entities.resyncs() === seenResyncs ) return batch;
			seenResyncs = entities.resyncs();
			// A compacted journal rebuilt the world publication whole, and
			// the batch opens with a reset: what the reset nulls must ride
			// inside this batch, not the next one. The full gameplay
			// snapshot (invalidateProjection + invalidate force every
			// field) and the pending travel are appended - on a composed
			// batch, the taken one is sealed - unless the batch already
			// carries them from after the drop.
			invalidateProjection();
			gameplay.invalidate();
			const extra: WorldEvent[] = [];
			const event = gameplayEvent();
			if ( event ) extra.push( event );
			if (
				pendingTravel &&
				!batch.events.some( e => e.kind === "travel" && e.travel === pendingTravel )
			) extra.push( { kind: "travel", travel: pendingTravel } );
			return extra.length ? { ...batch, events: [ ...batch.events, ...extra ] } : batch;
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
