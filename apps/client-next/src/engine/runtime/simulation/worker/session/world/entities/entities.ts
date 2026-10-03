/*
===========================================================================

entities.ts - The entity journal owns wire admission and ordered lifecycle publication to presentation.

===========================================================================
*/
import {
	refreshNameColor,
	NAME_COLOR_WHITE,
	isNameColorGuard,
	equipmentHoldType,
	type NameColorContext
} from "@/engine/foundation/gameplay/name-color";
import { decodeMovementSpeeds } from "@/engine/foundation/gameplay/native-movement";
import { spawnSkillReferences, entrySpawnSkills } from "@/engine/foundation/gameplay/spawn-skills";
import { merchantBranches } from "@/engine/foundation/gameplay/merchant-branches";
import { decodeCharacterSpawn } from "@/engine/foundation/gameplay/character-spawn";
import { decodeGroundItem } from "@/engine/foundation/gameplay/ground-item";
import {
	decodeSkillObject,
	DYNAMIC_OBJECT_REFERENCE,
	SKILL_OBJECT_TYPE
} from "@/engine/foundation/gameplay/skill-object";
import {
	decodePeerAppearance,
	equipmentBand,
	equipmentSlot,
	transformSkinTail
} from "@/engine/foundation/gameplay/peer-appearance";
import { createEntityMotion } from "./motion/motion";
import type { EntityState, WorldBatch, WorldEvent } from "@/engine/contracts/world";
import { journalCost } from "@/engine/foundation/gameplay/journal-cost";
import type { WireFrame } from "@/engine/contracts/network";
// Wire authorities: server enterworld/{register,bootstrap,wire}.go,
// world/simulation/{npc,monster}.go and item/wire/objectmove.go.
/*
================
createEntities
================
*/
export function createEntities(
	surface?: import("@/engine/contracts/navigation").SurfaceResolver,
	lifecycle?: ( event: Extract<WorldEvent, { kind: "spawn" | "despawn"; }> ) => void,
	nameContext?: ( entity?: EntityState ) => NameColorContext | undefined
) {
	// A 16 MiB admitted server bootstrap can require twice that in UTF-16
	// accounting. Static catalogues publish once; event count stays bounded.
	const journalByteLimit = 32 << 20, objectListByteLimit = 8 << 20;
	let skillRefs = spawnSkillReferences( [] );
	const motion = createEntityMotion( surface );
	const itemRefs = new Map<number, number>(), itemNames = new Map<number, string>();
	const entities = new Map<number, EntityState>(),
		refs = new Map<number, {
			teleport?: EntityState["teleport"];
			kind: string;
			tidWord: number;
			name?: string;
			level?: number;
			maxHp?: number;
			countryByte9c?: number;
			merchantBranches?: EntityState["merchantBranches"];
		}>();
	let receivedAt = 0;
	let epoch = 0, sequence = 0, inflight: WorldBatch | null = null, bytes = 0;
	let events: WorldEvent[] = [], staged: WorldEvent[] | null = null, remaining = 0, stagedBytes = 0;
	// Only locally sampled coordinates are replaceable. Wire state/lifecycle
	// events remain reliable barriers; never mutate an already offered batch.
	const pendingPoses = new Map<number, { index: number; size: number; }>();
	let stagedMode = 1, removalTail = new Uint8Array( 0 );
	let synchronized = false, localName = "";
	let localSkills: readonly import("@/engine/foundation/gameplay/spawn-skills").SpawnSkill[] = [];
	let localAvatars: import("@/engine/contracts/world").EntityEquipment[] = [];
	let local: Record<string, unknown> | null = null;
	/*
	================
	cost
	================
	*/
	function cost( event: WorldEvent ) {
		return event.kind === "native" ? event.payload.byteLength : journalCost( event );
	}
	/*
	================
	append
	================
	*/
	function append( event: WorldEvent, size = cost( event ) ) {
		if ( events.length + (staged?.length ?? 0) >= 8192 || bytes + stagedBytes + size > journalByteLimit ) {
			// Failure-only census distinguishes a stalled consumer from an
			// unsupported packet stream without adding a per-frame telemetry walk.
			const kinds: Record<string, number> = {};
			for ( const queued of events ) {
				const key = queued.kind === "native" ? `native:${queued.opcode.toString( 16 )}` : queued.kind;
				kinds[key] = (kinds[key] ?? 0) + 1;
			}
			throw new Error(
				`Reliable world journal backlog exceeded: ${event.kind} adds ${size} bytes to ${bytes + stagedBytes}; ${
					events.length + (staged?.length ?? 0)
				} events queued; awaiting batch ${
					inflight?.sequence ?? "none"
				}; ${pendingPoses.size} replaceable poses; ${JSON.stringify( kinds )}`
			);
		}
		bytes += size;
		events.push( event );
		if ( event.kind === "state" || event.kind === "spawn" ) pendingPoses.delete( event.entity.gid );
		else if ( event.kind === "despawn" ) pendingPoses.delete( event.gid );
		else if ( event.kind === "reset" || event.kind === "native" ) pendingPoses.clear();
	}
	/*
	================
	samplePose
	================
	*/
	function samplePose( entity: EntityState ) {
		const event: WorldEvent = { kind: "state", entity },
			pending = pendingPoses.get( entity.gid ),
			size = cost( event );
		if ( pending === undefined ) {
			append( event, size );
			pendingPoses.set( entity.gid, { index: events.length - 1, size } );
		} else {
			const nextBytes = bytes - pending.size + size;
			if ( nextBytes + stagedBytes > journalByteLimit ) {
				throw Error( "Reliable world journal pose byte capacity exceeded" );
			}
			events[pending.index] = event;
			bytes = nextBytes;
			pending.size = size;
		}
		entities.set( entity.gid, entity );
	}
	/*
	================
	groundedSpawn
	================
	*/
	function groundedSpawn( entity: EntityState ): EntityState {
		if (
			!surface ||
			(!entity.groundItem && ![ "npc", "monster", "cos", "player", "local-player" ].includes( entity.kind ))
		) return entity;
		const pose = { regionId: entity.regionId, x: entity.x, y: entity.y, z: entity.z, angle: entity.heading };
		const y = surface( pose, pose ).y;
		return y === entity.y ? entity : Object.freeze( { ...entity, y } );
	}
	/*
	================
	apply
	================
	*/
	function apply( event: WorldEvent ) {
		if ( event.kind === "spawn" ) {
			event = { ...event, entity: groundedSpawn( event.entity ) };
			const e = event.entity, c = nameContext?.( e );
			const refresh = e.kind === "player" || e.kind === "local-player" || isNameColorGuard( e ) ||
				e.kind === "cos" && e.pvpState !== undefined;
			if ( c && refresh ) event = { ...event, entity: { ...e, nameColor: refreshNameColor( e, c ) } };
			if ( entities.has( event.entity.gid ) ) {
				throw new Error( "Duplicate entity spawn" );
			}
			if ( entities.size >= 16384 ) {
				throw new Error( "Entity limit exceeded" );
			}
			entities.set( event.entity.gid, event.entity );
			motion.spawn( event.entity, receivedAt );
		} else if ( event.kind === "state" ) {
			entities.set( event.entity.gid, event.entity );
		} else if ( event.kind === "despawn" ) {
			entities.delete( event.gid );
			motion.remove( event.gid );
			for ( const rider of entities.values() ) {
				if ( rider.mountedOn === event.gid ) {
					apply( { kind: "state", entity: { ...rider, mountedOn: undefined } } );
				}
			}
		}
		append( event );
		if ( event.kind === "spawn" || event.kind === "despawn" ) lifecycle?.( event );
	}
	/*
	================
	finite
	================
	*/
	function finite( value: unknown ): number {
		if ( typeof value !== "number" || !Number.isFinite( value ) ) {
			throw new Error( "Invalid entity coordinate" );
		}
		return value;
	}
	/*
	================
	position
	================
	*/
	function position( v: DataView, offset: number ) {
		return {
			regionId: v.getUint16( offset, true ),
			x: finite( v.getFloat32( offset + 2, true ) ),
			y: finite( v.getFloat32( offset + 6, true ) ),
			z: finite( v.getFloat32( offset + 10, true ) ),
			heading: v.getUint16( offset + 14, true )
		};
	}
	/*
	================
	raw
	================
	*/
	function raw( frame: WireFrame ): WorldEvent {
		return { kind: "native", opcode: frame.opcode, payload: frame.payload.slice() };
	}
	/*
	================
	spawn
	================
	*/
	function spawn( frame: WireFrame ): WorldEvent {
		const p = frame.payload;
		if ( p.length < 4 ) {
			throw new Error( "Truncated spawn" );
		}
		const v = new DataView( p.buffer, p.byteOffset, p.byteLength ),
			refObjId = v.getUint32( 0, true ),
			ref = refs.get( refObjId );
		if ( refObjId === DYNAMIC_OBJECT_REFERENCE ) {
			if ( p.length < 6 ) throw Error( "Truncated dynamic object discriminator" );
			if ( v.getUint16( 4, true ) !== SKILL_OBJECT_TYPE ) return raw( frame );
			return { kind: "spawn", entity: decodeSkillObject( p, frame.opcode === 0x30d7 ) };
		}
		if ( ref?.kind === "teleport" ) {
			if ( p.length !== 24 || !ref.teleport ) throw Error( "Invalid teleport gate spawn" );
			const gid = v.getUint32( 4, true );
			if ( !gid ) throw Error( "Invalid teleport gate identity" );
			return { kind: "spawn", entity: { ...ref, gid, refObjId, ...position( v, 8 ), name: ref.name ?? "" } };
		}
		const itemType = itemRefs.get( refObjId );
		if ( itemType !== undefined && !(itemType & 2) && (itemType & 0x1c) === 0xc ) {
			return {
				kind: "spawn",
				entity: decodeGroundItem( p, itemType, frame.opcode === 0x30d7, itemNames.get( refObjId ) ?? "" )
			};
		}
		// Other native families stay in the reliable journal until their exact
		// variable spawn schemas are implemented. Never guess offsets.
		if ( ref?.kind === "player" ) {
			return {
				kind: "spawn",
				entity: { ...ref, ...decodePeerAppearance( p, itemRefs, frame.opcode === 0x30d7, refs, skillRefs ) }
			};
		}
		if ( !ref || ![ "npc", "monster", "cos" ].includes( ref.kind ) ) {
			return raw( frame );
		}
		const entity = decodeCharacterSpawn( p, ref.kind, ref.tidWord, frame.opcode === 0x30d7, skillRefs );
		return { kind: "spawn", entity: { ...ref, ...entity, name: entity.name || ref.name || "" } };
	}
	/*
	================
	recolor
	================
	*/
	function recolor( test: ( e: EntityState ) => boolean, white = false, context?: NameColorContext ) {
		const c = context ?? nameContext?.();
		if ( !c ) return;
		for ( const e of entities.values() ) {
			if ( test( e ) ) {
				const nameColor = white && (e.pvpState ?? 0) === 0 ? NAME_COLOR_WHITE : refreshNameColor( e, c );
				if ( nameColor !== e.nameColor ) apply( { kind: "state", entity: { ...e, nameColor } } );
			}
		}
	}
	return {
		recolor,
		/*
		================
		characterCountry

		The country byte (+0x9C) of a character reference: 0 China, 1 Europe.
		Party windows resolve a member's race mark from it whether or not the
		member is in view (5B93A0 reads GetCharCosDataById(member ref)+0x9C).
		================
		*/
		characterCountry( refObjId: number ): number | undefined {
			return refs.get( refObjId )?.countryByte9c;
		},
		/*
		================
		itemReference

		An item reference's flags and display name, as ground drops resolve them.
		================
		*/
		itemReference( refObjId: number ): { typeFlags: number; name: string; } | undefined {
			const typeFlags = itemRefs.get( refObjId );
			return typeFlags === undefined ? undefined : { typeFlags, name: itemNames.get( refObjId ) ?? "" };
		},
		// Spawn initialization uses nearest terrain/object height (85FF38 ->
		// 86D5C0 -> 403D20); late navigation admission repeats that lookup
		// at the current pose, without restarting an active movement segment.
		/*
		================
		groundSpawns
		================
		*/
		groundSpawns() {
			for ( const entity of entities.values() ) {
				const next = groundedSpawn( entity );
				motion.surfaceReference( next );
				if ( next !== entity ) apply( { kind: "state", entity: next } );
			}
		},
		/*
		================
		references
		================
		*/
		references( rows: readonly import("@/engine/foundation/gameplay/commerce").CommerceItemReference[] ) {
			for ( const row of rows ) {
				if ( itemRefs.has( row.refObjId ) && itemRefs.get( row.refObjId ) !== row.typeFlags ) {
					throw Error( "Conflicting public item reference" );
				}
			}
			for ( const row of rows ) {
				itemRefs.set( row.refObjId, row.typeFlags );
				itemNames.set( row.refObjId, row.name );
			}
		},
		/*
		================
		clear
		================
		*/
		clear() {
			entities.clear();
			motion.clear();
			skillRefs = spawnSkillReferences( [] );
			refs.clear();
			itemRefs.clear();
			itemNames.clear();
			local = null;
			localSkills = [];
			localAvatars = [];
			staged = null;
			stagedBytes = 0;
			remaining = 0;
			synchronized = false;
			append( { kind: "reset", epoch: ++epoch } );
		},
		/*
		================
		dispose
		================
		*/
		dispose() {
			entities.clear();
			motion.clear();
			pendingPoses.clear();
			skillRefs = spawnSkillReferences( [] );
			refs.clear();
			itemRefs.clear();
			itemNames.clear();
			local = null;
			localSkills = [];
			localAvatars = [];
			staged = null;
			stagedBytes = 0;
			events = [];
			inflight = null;
			bytes = 0;
		},
		/*
		================
		bootstrap
		================
		*/
		bootstrap( value: unknown, resetAlreadyPublished = false ) {
			if ( !value || typeof value !== "object" ) {
				throw new Error( "Missing world bootstrap" );
			}
			const b = value as Record<string, unknown>;
			if (
				b.protocolVersion !== 2 || b.nativeResult !== 1 || !Array.isArray( b.refObjSnapshot ) ||
				!b.localPlayerEntry
			) {
				throw new Error( "Invalid world bootstrap" );
			}
			skillRefs = spawnSkillReferences(
				(b.refSkillSnapshot ?? []) as { id: number; token: boolean; status: boolean; }[]
			);
			refs.clear();
			itemRefs.clear();
			itemNames.clear();
			for ( const row of b.refObjSnapshot ) {
				if (
					!row || typeof row.refObjId !== "number" || !Number.isInteger( row.refObjId ) ||
					row.refObjId <= 0 || row.refObjId > 0xffffffff || typeof row.kind !== "string" ||
					refs.has( row.refObjId )
				) {
					throw new Error( "Invalid world reference catalog" );
				}
				if (
					row.kind === "cos" && (!Number.isInteger( row.tidWord ) || row.tidWord < 0 || row.tidWord > 65535)
				) throw new Error( "Missing COS type authority" );
				for (
					const [field, max] of [ [ "level", 255 ], [ "maxHp", 0xffffffff ], [
						"countryByte9c",
						255
					] ] as const
				) {
					if (
						row[field] !== undefined &&
						(!Number.isInteger( row[field] ) || row[field] < 0 || row[field] > max)
					) throw Error( "Invalid entity reference " + field );
				}
				if ( row.name !== undefined && typeof row.name !== "string" ) {
					throw Error( "Invalid entity reference name" );
				}
				if (
					row.kind === "teleport" &&
					(!row.teleport || !Number.isFinite( row.teleport.radius ) ||
						!Number.isFinite( row.teleport.height ) || row.teleport.radius <= 0 ||
						row.teleport.height <= 0 ||
						(row.teleport.fortressId !== undefined &&
							(!Number.isInteger( row.teleport.fortressId ) || row.teleport.fortressId < 0 ||
								row.teleport.fortressId > 0xffffffff)))
				) throw Error( "Missing teleport gate bounds" );
				refs.set( row.refObjId, {
					...(row.kind === "teleport" ?
						{
							teleport: {
								radius: row.teleport.radius,
								height: row.teleport.height,
								fortressId: row.teleport.fortressId
							}
						} :
						{}),
					kind: row.kind,
					tidWord: row.tidWord ?? 0,
					name: row.name,
					level: row.level,
					maxHp: row.maxHp,
					countryByte9c: row.countryByte9c,
					...(row.kind === "npc" ? { merchantBranches: merchantBranches( row.npcTalkStoreGroups ) } : {})
				} );
			}
			for (
				const row of (b.refItemSnapshot ?? []) as {
					refObjId: number;
					typeFlags: number;
					name?: string;
					codename?: string;
				}[]
			) {
				if (
					!Number.isInteger( row.refObjId ) || row.refObjId <= 0 || !Number.isInteger( row.typeFlags ) ||
					row.typeFlags < 0 || row.typeFlags > 65535 || itemRefs.has( row.refObjId )
				) throw new Error( "Invalid peer item catalog" );
				itemRefs.set( row.refObjId, row.typeFlags );
				itemNames.set( row.refObjId, row.name ?? row.codename ?? String( row.refObjId ) );
			}
			local = b.localPlayerEntry as Record<string, unknown>;
			if ( local.countryByte9c !== undefined && (local.countryByte9c !== 0 && local.countryByte9c !== 1) ) {
				throw Error( "Invalid local country authority" );
			}
			for ( const key of [ "arenaTeam", "pvpState" ] ) {
				const value = local[key];
				if (
					value !== undefined &&
					(typeof value !== "number" || !Number.isInteger( value ) || value < 0 || value > 255)
				) throw Error( "Invalid local name state" );
			}
			localSkills = entrySpawnSkills( local.spawnSkills, skillRefs );
			if (
				local.visualFlags !== undefined &&
				(!Number.isInteger( local.visualFlags ) || Number( local.visualFlags ) < 0 ||
					Number( local.visualFlags ) > 3)
			) throw Error( "Invalid local visual flags" );
			const character = b.character as {
				name?: unknown;
			} | undefined;
			localName = typeof character?.name === "string" ? character.name : "";
			const avatarRows = (b.character as
				| { avatarInventory?: { rows?: { slot: number; refObjId: number; plus: number; }[]; }; }
				| undefined)?.avatarInventory?.rows ?? [];
			if ( avatarRows.length > 5 ) throw new Error( "Avatar capacity exceeded" );
			const seen = new Set<number>();
			localAvatars = avatarRows.map( row => {
				const typeFlags = itemRefs.get( row.refObjId );
				if (
					!Number.isInteger( row.slot ) || row.slot < 0 || row.slot > 4 || seen.has( row.slot ) ||
					typeFlags === undefined || !equipmentBand( typeFlags ) || (typeFlags & 0x780) !== 0x680 ||
					!Number.isInteger( row.plus ) || row.plus < 0 || row.plus > 255
				) throw new Error( "Invalid local avatar" );
				seen.add( row.slot );
				return { slot: row.slot, refObjId: row.refObjId, typeFlags, plus: row.plus };
			} );
			entities.clear();
			motion.clear();
			staged = null;
			stagedBytes = 0;
			remaining = 0;
			synchronized = false;
			// A re-entry reset has already revoked the old scene. Its delayed
			// reference/bootstrap completion belongs to that same transaction.
			if ( !resetAlreadyPublished ) append( { kind: "reset", epoch: ++epoch } );
			// Reference tables and authoritative inventory belong to the worker. The
			// presentation has no consumers for them; forwarding the complete
			// bootstrap duplicates megabytes beside the gameplay catalogue and
			// prevents a real EnterWorld burst from fitting the bounded journal.
			append( {
				kind: "bootstrap",
				value: { protocolVersion: b.protocolVersion, character: { name: localName }, localPlayerEntry: local }
			} );
		},
		/*
		================
		receive
		================
		*/
		receive( frame: WireFrame, now = 0 ) {
			receivedAt = now;
			const p = frame.payload, v = new DataView( p.buffer, p.byteOffset, p.byteLength );
			if ( frame.opcode === 0x3449 ) {
				if ( p.length !== 8 ) throw Error( "Invalid external item effect" );
				const source = entities.get( v.getUint32( 0, true ) ), item = v.getUint32( 4, true );
				if ( item === 0 || !source || source.groundItem ) return;
				const typeFlags = itemRefs.get( item );
				if ( typeFlags === undefined ) throw Error( "Missing external item reference " + item );
				append( { kind: "item-effect", source: { ...source }, item, typeFlags } );
				return;
			}
			if ( frame.opcode === 0x31e2 ) {
				// 777af0 clears CIItem owner flag/JID, preserving the item itself.
				if ( p.length !== 4 ) throw new Error( "Invalid ground ownership expiry" );
				const entity = entities.get( v.getUint32( 0, true ) );
				if ( entity?.groundItem ) {
					apply( {
						kind: "state",
						entity: { ...entity, groundItem: { ...entity.groundItem, ownerJid: undefined } }
					} );
				}
				return;
			}
			if ( frame.opcode === 0x323a ) {
				// CPSMission_OnCharacterSkinChangeSuccess0x323A (7641D0): u32 gid, u32 skin.
				if ( p.length < 8 ) throw Error( "Invalid skin change" );
				const entity = entities.get( v.getUint32( 0, true ) ), refObjId = v.getUint32( 4, true );
				const tid = refObjId ? refs.get( refObjId )?.tidWord : 0;
				if ( tid === undefined ) throw Error( "Unknown skin reference" );
				const tail = refObjId ?
					transformSkinTail( p, 8, refObjId, tid, itemRefs, (entity?.transformSkin?.revision ?? 0) + 1 ) :
					undefined;
				if ( (tail?.next ?? 8) !== p.length ) throw Error( "Invalid skin change length" );
				const skin = tail?.skin;
				if ( entity ) apply( { kind: "state", entity: { ...entity, transformSkin: skin } } );
				return;
			}
			if ( frame.opcode === 0x324b ) {
				if ( p.length !== 5 ) throw Error( "Invalid emote broadcast" );
				const entity = entities.get( v.getUint32( 0, true ) );
				if ( entity ) {
					apply( {
						kind: "state",
						entity: {
							...entity,
							emote: { action: p[4]!, revision: (entity.emote?.revision ?? 0) + 1, atMs: now }
						}
					} );
				}
				return;
			}
			if ( frame.opcode === 0xb683 || frame.opcode === 0x35c7 ) {
				if ( p.length !== 5 ) throw new Error( "Invalid visual/motion packet" );
				const entity = entities.get( v.getUint32( 0, true ) );
				if ( !entity ) return;
				if ( frame.opcode === 0xb683 ) apply( { kind: "state", entity: { ...entity, visualFlags: p[4]! } } );
				else if ( !entity.mountedOn ) {
					apply( {
						kind: "state",
						entity: { ...entity, heading: p[4]! * 257, pickupRevision: (entity.pickupRevision ?? 0) + 1 }
					} );
				}
				return;
			}
			if ( frame.opcode === 0x3314 || frame.opcode === 0x377c ) {
				if ( p.length < 9 ) throw new Error( "Truncated equipment visual" );
				const gid = v.getUint32( 0, true ),
					refObjId = v.getUint32( 5, true ),
					typeFlags = itemRefs.get( refObjId );
				const equip = frame.opcode === 0x3314;
				if ( equip && typeFlags === undefined ) throw new Error( "Unknown peer equipment reference" );
				// 777980: a missing reference is the ordinary clear-by-slot arm.
				// Avatar removal alone is addressed by an existing item reference.
				const avatar = typeFlags !== undefined && equipmentBand( typeFlags ) && (typeFlags & 0x780) === 0x680;
				if ( p.length !== (equip && equipmentBand( typeFlags! ) ? 10 : 9) || equip && p[4] !== 0 ) {
					throw new Error( "Invalid equipment visual" );
				}
				const entity = entities.get( gid );
				if ( !entity ) return;
				const key = avatar ? "avatars" : "equipment",
					slot = avatar ? -1 : equip ? equipmentSlot( typeFlags! ) : p[4]!;
				// 868a80 returns -1 for non-visual equipment (including jewelry).
				// 8e8210 rejects that model write without failing the packet;
				// the B06D inventory result already owns the equipped item.
				if ( equip && !avatar && slot === -1 ) return;
				if ( !avatar && (slot < 0 || slot > 12) ) throw Error( "Invalid equipment visual slot" );
				const equipment = [ ...(entity[key] ?? []) ];
				const index = equipment.findIndex( item => avatar ? item.refObjId === refObjId : item.slot === slot );
				if ( index >= 0 ) equipment.splice( index, 1 );
				if ( equip ) equipment.push( { slot, refObjId, typeFlags: typeFlags!, plus: p[9] ?? 0 } );
				apply( {
					kind: "state",
					entity: {
						...entity,
						[key]: equipment,
						...(!avatar && slot === 8 ?
							{ holdType: equipmentHoldType( equip ? typeFlags : undefined ) } :
							{})
					}
				} );
				return;
			}
			if ( frame.opcode === 0xb4b5 ) {
				// 777F60 routes refusals to notification category 14 without
				// changing either actor. Gameplay owns that notification.
				if ( p.length === 2 && p[0] === 2 ) return;
				if ( p.length !== 10 || p[0] !== 1 || p[5]! > 1 ) throw new Error( "Invalid ride state" );
				const rider = entities.get( v.getUint32( 1, true ) ), mount = v.getUint32( 6, true );
				if ( !rider || p[5] === 1 && (!entities.has( mount ) || mount === rider.gid) ) {
					throw new Error( "Ride state references absent entity" );
				}
				if (
					p[5] === 1 &&
					(entities.get( mount )?.mountedOn ||
						[ ...entities.values() ].some( entity => entity.mountedOn === rider.gid ))
				) throw new Error( "Nested mount state is unsupported" );
				apply( { kind: "state", entity: { ...rider, mountedOn: p[5] === 1 ? mount : undefined } } );
				return;
			}
			if ( frame.opcode === 0x3369 || frame.opcode === 0x366a ) {
				if ( p.length !== 2 ) throw Error( "Invalid reset region packet" );
				entities.clear();
				motion.clear();
				staged = null;
				stagedBytes = 0;
				remaining = 0;
				synchronized = false;
				append( { kind: "reset", epoch: ++epoch } );
				return;
			}
			if ( frame.opcode === 0x32a6 ) {
				if ( p.length !== 8 || !local || v.getUint32( 0, true ) === 0 ) {
					throw new Error( "Invalid local entity latch" );
				}
				const pose = local.startProfile as Record<string, unknown>;
				const walkSpeed = finite( local.walkSpeed ?? 20 ), runSpeed = finite( local.runSpeed ?? 50 );
				if ( walkSpeed <= 0 || runSpeed <= 0 ) throw Error( "Invalid local movement speeds" );
				apply( {
					kind: "spawn",
					entity: Object.freeze( {
						gid: v.getUint32( 0, true ),
						refObjId: finite( local.modelRef ),
						kind: "local-player",
						...(local.countryByte9c !== undefined ? { countryByte9c: Number( local.countryByte9c ) } : {}),
						walkSpeed,
						runSpeed,
						spawnSkills: localSkills,
						avatars: localAvatars,
						arenaTeam: Number( local.arenaTeam ?? 255 ),
						pvpState: Number( local.pvpState ?? 0 ),
						...(local.visualFlags !== undefined ? { visualFlags: Number( local.visualFlags ) } : {}),
						regionId: finite( pose.regionId ),
						x: finite( pose.x ),
						y: finite( pose.y ),
						z: finite( pose.z ),
						heading: finite( pose.angle ),
						name: localName
					} )
				} );
				return;
			}
			if ( frame.opcode === 0x30cb ) {
				if ( staged || p.length !== 3 || (p[0] !== 1 && p[0] !== 2) ) {
					throw new Error( "Invalid object-list start" );
				}
				remaining = v.getUint16( 1, true );
				if ( remaining > 4096 ) {
					throw new Error( "Object list exceeds bound" );
				}
				staged = [];
				stagedBytes = 0;
				stagedMode = p[0]!;
				removalTail = new Uint8Array( 0 );
				return;
			}
			if ( frame.opcode === 0x3417 ) {
				// Native 0x777370 mode 2 consumes count calls to 0x777310:
				// four-byte identities, buffered across arbitrary chunks.
				// The server emits this bracket when a region ring is evicted.
				if ( staged && stagedMode === 2 ) {
					if ( p.length + removalTail.length > remaining * 4 ) {
						throw new Error( "Object removal chunk exceeds declared count" );
					}
					const data = new Uint8Array( removalTail.length + p.length );
					data.set( removalTail );
					data.set( p, removalTail.length );
					const view = new DataView( data.buffer ),
						count = Math.floor( data.length / 4 ),
						next: WorldEvent[] = [];
					let size = 0;
					for ( let i = 0; i < count; i++ ) {
						const event: WorldEvent = { kind: "despawn", gid: view.getUint32( i * 4, true ) };
						next.push( event );
						size += cost( event );
					}
					if (
						events.length + staged.length + count + 1 > 8192 ||
						stagedBytes + size + cost( { kind: "synchronized", epoch } ) > objectListByteLimit ||
						bytes + stagedBytes + size + cost( { kind: "synchronized", epoch } ) > journalByteLimit
					) throw new Error( "Object list exceeds staging capacity" );
					staged.push( ...next );
					stagedBytes += size;
					remaining -= count;
					removalTail = data.slice( count * 4 );
					return;
				}
				if ( !staged || remaining <= 0 ) {
					throw new Error( "Unexpected object-list chunk" );
				}
				// Reserve raw payload capacity before spawn can copy an unknown family.
				// Keep room for the synchronization marker until the list commits.
				const markerBytes = cost( { kind: "synchronized", epoch } );
				if (
					events.length + staged.length + 2 > 8192 ||
					stagedBytes + p.byteLength + markerBytes > objectListByteLimit ||
					bytes + stagedBytes + p.byteLength + markerBytes > journalByteLimit
				) {
					staged = null;
					stagedBytes = 0;
					remaining = 0;
					throw new Error( "Object list exceeds staging capacity" );
				}
				const event = spawn( frame ), size = cost( event );
				if (
					stagedBytes + size + markerBytes > objectListByteLimit ||
					bytes + stagedBytes + size + markerBytes > journalByteLimit
				) {
					staged = null;
					stagedBytes = 0;
					remaining = 0;
					throw new Error( "Object list exceeds staging capacity" );
				}
				stagedBytes += size;
				staged.push( event );
				remaining--;
				return;
			}
			if ( frame.opcode === 0x330a ) {
				if ( !staged || remaining || p.length ) {
					throw new Error( "Incomplete object list" );
				}
				const ids = new Set<number>();
				for ( const event of staged ) {
					if ( event.kind === "spawn" ) {
						if ( ids.has( event.entity.gid ) || entities.has( event.entity.gid ) ) {
							throw new Error( "Duplicate object-list identity" );
						}
						ids.add( event.entity.gid );
					}
				}
				if (
					entities.size + ids.size > 16384 || events.length + staged.length + 1 > 8192 ||
					bytes + staged.reduce( ( sum, event ) => sum + cost( event ), 0 ) +
								cost( { kind: "synchronized", epoch } ) > journalByteLimit
				) {
					throw new Error( "Object list exceeds journal capacity" );
				}
				const committed = staged;
				staged = null;
				stagedBytes = 0;
				for ( const event of committed ) {
					apply( event );
				}
				staged = null;
				append( { kind: "synchronized", epoch } );
				return;
			}
			if ( frame.opcode === 0x30d7 ) {
				const event = spawn( frame );
				apply( event );
				// 86DC10 suppresses drop fanfare for grouped visibility lists.
				// Every single item reaches ITEM/snd_dropitem. The authored
				// category lookup decides whether it has a sound (v1.150: GOLD).
				if ( event.kind === "spawn" && event.entity.groundItem ) {
					append( {
						kind: "item-sound",
						cue: { handle: "SND_DROPITEM", typeFlags: event.entity.groundItem.typeFlags },
						at: receivedAt
					} );
				}
				if ( event.kind === "spawn" && event.entity.kind === "cos" && p.at( -1 ) === 1 ) {
					const tid = refs.get( event.entity.refObjId )?.tidWord ?? 0, band = tid >>> 11;
					if ( (tid & 0x7fe) === 0x1c6 && (band === 3 || band === 4) ) {
						append( { kind: "ui-sound", handle: "SND_COS_SUMMON", at: receivedAt } );
					}
				}
				return;
			}
			if ( frame.opcode === 0x36ab ) {
				if ( p.length !== 4 ) {
					throw new Error( "Invalid despawn" );
				}
				apply( { kind: "despawn", gid: v.getUint32( 0, true ) } );
				return;
			}
			if ( frame.opcode === 0x3204 ) {
				if ( p.length < 5 ) throw Error( "Truncated hold/PvP state" );
				const flags = p[4]!, expected = 5 + (flags & 1 ? 1 : 0) + (flags & 2 ? 1 : 0);
				if ( p.length !== expected ) throw Error( "Invalid hold/PvP state length" );
				const e = entities.get( v.getUint32( 0, true ) );
				if ( !e ) return;
				let o = 5;
				const next = {
					...e,
					...(flags & 1 ? { holdType: p[o++]! } : {}),
					...(flags & 2 ? { pvpState: p[o++]! } : {})
				};
				const c = nameContext?.();
				if ( flags & 2 && c ) next.nameColor = refreshNameColor( next, c );
				apply( { kind: "state", entity: next } );
				return;
			}
			if ( frame.opcode === 0x3122 ) {
				if ( p.length !== 6 ) {
					throw new Error( "Invalid entity state channel" );
				}
				const entity = entities.get( v.getUint32( 0, true ) );
				if ( entity && p[4] === 7 ) {
					const next = { ...entity, pvpState: p[5]! }, c = nameContext?.();
					if ( c ) next.nameColor = refreshNameColor( next, c );
					apply( { kind: "state", entity: next } );
					return;
				}
				// 777B60 channel 0 stores the life state independently of HP.
				// Preserve the spawn tuple so initial death and later revival
				// use the same presentation authority.
				if ( entity && p[4] === 0 ) {
					const appearanceState = [ ...(entity.appearanceState ?? [ 1, 0, 0 ]) ];
					appearanceState[0] = p[5]!;
					const stopped = p[5] === 2 ? motion.stopForDeath( entity, now ) : null;
					apply( {
						kind: "state",
						entity: Object.freeze( {
							...entity,
							...stopped,
							appearanceState: Object.freeze( appearanceState )
						} )
					} );
					return;
				}
				if ( entity && p[4] === 1 ) {
					if ( ![ 0, 2, 3, 4 ].includes( p[5]! ) ) {
						throw new Error( "Invalid movement mode" );
					}
					const next = { ...entity, movementMode: p[5]! };
					const pose = motion.mode( next, now );
					apply( { kind: "state", entity: Object.freeze( { ...next, ...pose } ) } );
					return;
				}
				// 777B60 channel 4 -> 85EC00: the third spawn status byte
				// owns berserk (1) and transparent state (4), independently of LIFE.
				if ( entity && p[4] === 4 ) {
					// 85EC10 returns before side effects for repeated state.
					if ( (entity.appearanceState?.[2] ?? 0) === p[5] ) return;
					const appearanceState = [ ...(entity.appearanceState ?? [ 1, 0, 0 ]) ];
					appearanceState[2] = p[5]!;
					apply( {
						kind: "state",
						entity: Object.freeze( { ...entity, appearanceState: Object.freeze( appearanceState ) } )
					} );
					return;
				}
			}
			if ( frame.opcode === 0x376f ) {
				const channels = decodeMovementSpeeds( p ), source = entities.get( channels.gid );
				// CPSMission_OnEntitySpeedUpdate0x376F (0x775E40) consumes server speeds;
				// CCharactor_GetActiveMoverEntity (0x85E000) resolves the riding actor.
				const entity = source?.mountedOn ? entities.get( source.mountedOn ) : source;
				if ( entity ) {
					const next = { ...entity, walkSpeed: channels.walkSpeed, runSpeed: channels.runSpeed };
					apply( {
						kind: "state",
						entity: Object.freeze( { ...next, ...motion.speeds( entity, next, now ) } )
					} );
				}
				return;
			}
			if ( frame.opcode === 0xb738 ) {
				if ( p.length < 4 ) {
					throw new Error( "Truncated movement broadcast" );
				}
				const source = entities.get( v.getUint32( 0, true ) );
				// CCharactor_GetActiveMoverEntity (0x85E000): a rider's movement moves
				// its mount, and the rider sits on it. The local player's own movement
				// is the gameplay owner's, which drives its mount in presentation.
				const entity = source?.mountedOn ? entities.get( source.mountedOn ) ?? source : source;
				if (
					entity && source?.kind !== "local-player" && entity.kind !== "local-player" &&
					entity.appearanceState?.[0] !== 2
				) {
					const movementPath = motion.receive( p, entity, now );
					apply( {
						kind: "state",
						entity: { ...entity, movementPath, movementRevision: (entity.movementRevision ?? 0) + 1 }
					} );
					return;
				}
			}
			// 0xB2CF [u32 gid][u16 heading] (0x775A90): another mover's steer.
			// The client ignores its own gid; its walk already turned.
			if ( frame.opcode === 0xb2cf ) {
				if ( p.length !== 6 ) throw new Error( "Invalid entity steer" );
				const source = entities.get( v.getUint32( 0, true ) );
				if ( !source || source.kind === "local-player" || source.appearanceState?.[0] === 2 ) return;
				// CCharactor_GetActiveMoverEntity (0x85E000): a rider steers its mount.
				const entity = source.mountedOn ? entities.get( source.mountedOn ) : source;
				if ( !entity ) return;
				const steered = motion.steer( entity, v.getUint16( 4, true ), now );
				if ( steered ) {
					apply( {
						kind: "state",
						entity: Object.freeze( {
							...entity,
							...steered,
							movementRevision: (entity.movementRevision ?? 0) + 1
						} )
					} );
				}
				return;
			}
			if ( frame.opcode === 0x30e3 || frame.opcode === 0xb2f5 ) {
				if ( p.length !== 20 ) {
					throw new Error( "Invalid entity position" );
				}
				const gid = v.getUint32( frame.opcode === 0x30e3 ? 16 : 0, true ), source = entities.get( gid );
				// A rider's position belongs to its active mover, the mount (0x85E000).
				const entity = source?.mountedOn ? entities.get( source.mountedOn ) ?? source : source;
				// Source movement is advisory travel; LIFE-dead retires it.
				// B2F5 remains admissible: present rebirth corrects before LIFE-alive.
				if ( frame.opcode === 0x30e3 && entity?.appearanceState?.[0] === 2 ) return;
				if ( entity ) {
					const corrected = position( v, frame.opcode === 0x30e3 ? 0 : 4 );
					apply( {
						kind: "state",
						entity: Object.freeze( {
							...entity,
							movementRevision: (entity.movementRevision ?? 0) + 1,
							...(frame.opcode === 0x30e3 ?
								motion.source( entity, { ...corrected, angle: corrected.heading }, now ) :
								motion.correct( entity, { ...corrected, angle: corrected.heading } ))
						} )
					} );
					return;
				}
			}
			append( raw( frame ) );
		},
		/*
		================
		take
		================
		*/
		take(): WorldBatch | null {
			if ( inflight || !events.length ) {
				return null;
			}
			inflight = { sequence: ++sequence, events };
			events = [];
			pendingPoses.clear();
			bytes = 0;
			return inflight;
		},
		/*
		================
		ack
		================
		*/
		ack( value: number ) {
			if ( !inflight || value !== inflight.sequence ) {
				throw new Error( "Invalid world acknowledgement" );
			}
			synchronized ||= inflight.events.some( event => event.kind === "synchronized" && event.epoch === epoch );
			inflight = null;
		},
		synchronized: () => synchronized,
		/*
		================
		step
		================
		*/
		step( now: number ) {
			// 86DB70/858310: claimant must be a live CICharactor (state-0 bit in +644).
			for ( const entity of entities.values() ) {
				if ( entity.groundItem?.claimantGid ) {
					const claimant = entities.get( entity.groundItem.claimantGid );
					if (
						!claimant || ![ "local-player", "player", "npc", "monster", "cos" ].includes( claimant.kind ) ||
						claimant.appearanceState?.[0] === 2
					) {
						apply( {
							kind: "state",
							entity: { ...entity, groundItem: { ...entity.groundItem, claimantGid: undefined } }
						} );
					}
				}
			}
			for ( const pose of motion.step( now ) ) {
				const current = entities.get( pose.gid );
				if ( current ) samplePose( Object.freeze( { ...current, ...pose } ) );
			}
		},
		read: ( gid: number ) => entities.get( gid ),
		/*
		================
		die
		================
		*/
		die( gid: number ) {
			const entity = entities.get( gid );
			if ( !entity || entity.appearanceState?.[0] === 2 ) return;
			motion.remove( gid );
			const appearanceState = [ ...(entity.appearanceState ?? [ 1, 0, 0 ]) ];
			appearanceState[0] = 2;
			apply( { kind: "state", entity: { ...entity, moving: false, appearanceState } } );
		},
		castArrival: motion.castArrival,
		/*
		================
		displace
		================
		*/
		displace( command: import("@/engine/contracts/gameplay").CastDisplacement, now: number ) {
			const entity = entities.get( command.gid );
			if ( entity ) apply( { kind: "state", entity: { ...entity, ...motion.displace( entity, command, now ) } } );
		},
		/*
		================
		cancelCast
		================
		*/
		cancelCast( token: number, now: number ) {
			for ( const pose of motion.cancelCast( token, now ) ) {
				const entity = entities.get( pose.gid );
				if ( entity ) apply( { kind: "state", entity: { ...entity, ...pose } } );
			}
		},
		/*
		================
		publish
		================
		*/
		publish( event: WorldEvent ) {
			append( event );
		},
		count: () => entities.size
	};
}
