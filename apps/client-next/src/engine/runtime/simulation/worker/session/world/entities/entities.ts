/*
===========================================================================

entities.ts - The entity journal owns wire admission and ordered lifecycle publication to presentation.

===========================================================================
*/
import { petNameUpdate } from "@/engine/foundation/gameplay/pet-name";
import { animationRate, decodeAnimationSpeed } from "@/engine/foundation/animation/animation-rate";
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
import { decodeGroundItem, sunSealDrop } from "@/engine/foundation/gameplay/ground-item";
import { fortressStructureState } from "@/engine/foundation/gameplay/fortress";
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
import { featuresAt } from "@/engine/foundation/world/plane";
import { journalCost } from "@/engine/foundation/gameplay/journal-cost";
import { mergeGameplaySnapshots } from "@/engine/foundation/gameplay/gameplay-snapshot";
import type { Degradation, WireFrame } from "@/engine/contracts/network";
import { SYSTEM_PET_APPEAR } from "@/engine/contracts/orb";
import { STALL_TITLE_MODE } from "@/engine/foundation/gameplay/interaction-approach";
import { JOB_SUIT_SLOT, partyActiveJob } from "@/engine/foundation/gameplay/party-matching";
// The kinds whose spawn builds a CICharactor (players, NPCs, monsters, COS,
// fortress structures); ground items and skill objects are not characters.
const CHARACTER_KINDS = new Set( [ "player", "local-player", "npc", "monster", "cos", "structure" ] );
// Peer appearance slot 6 selects the overhead title; STALL_TITLE_MODE owns a stall.
const STALL_TITLE_SLOT = 6;
// Skipped cosmetic steps held between world takes (degrade).
const MAX_PENDING_DEGRADATIONS = 64;
// The one-shot natives a compaction keeps for its resync (dropQueuedJournal):
// bounded on their own so the rebuilt batch stays small next to the
// presentation's native backlog bound (8 MiB / 8192 events).
const MAX_RETAINED_NATIVES = 1024;
const RETAINED_NATIVE_BYTE_LIMIT = 1 << 20;

/*
================
localAppearanceRow

The local latch has no peer appearance row, so it starts from the alive,
idle defaults of one. LIFE (0x3122 channel 0) is then the local player's
only death authority, as natively (777B60 sets +263 and the dead flag;
8E64F0 plays the death from LIFE and the fatal impact, never from HP).
Without a row, presentation fell back to the predicted vitals HP and could
play a death, with its cry, that the next HP update stood back up.
================
*/
function localAppearanceRow( pvpState: number ): number[] {
	return [ 1, 0, 0, pvpState, 0, 0, 0, 0, 0 ];
}
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
	nameContext?: ( entity?: EntityState ) => NameColorContext | undefined,
	clip?: import("./motion/motion").MotionClip,
	block?: (
		pose: import("@/engine/contracts/gameplay").Pose,
		owner?: import("@/engine/foundation/navigation/dungeon-ownership").NavOwner
	) => number | undefined
) {
	// A 16 MiB admitted server bootstrap can require twice that in UTF-16
	// accounting. Static catalogues publish once; event count stays bounded.
	const journalByteLimit = 32 << 20, objectListByteLimit = 8 << 20;
	let skillRefs = spawnSkillReferences( [] );
	const motion = createEntityMotion( surface, clip );
	const itemRefs = new Map<number, number>(), itemNames = new Map<number, string>();
	// Item codenames by ref: the Sun drop marker's admission derives the
	// seal from the name (ground-item's sunSealDrop), as /LOADMONSTER does.
	const itemCodenames = new Map<number, string>();
	// /LOADMONSTER resolves a typed codename here, as the native client reads
	// its own character data (GlobalDataManager_GetItemRecordByCodeName).
	const monsterCodenames = new Map<string, { readonly refObjId: number; readonly monsterType: number; }>();
	// Port-only (#1640): the Experimental Sun drop marker's admission state:
	// the row's byte and the local player's gid (its drops are the marked).
	let sunMarkerOn = false, ownGid = 0;
	const entities = new Map<number, EntityState>(),
		refs = new Map<number, {
			teleport?: EntityState["teleport"];
			kind: string;
			tidWord: number;
			name?: string;
			level?: number;
			maxHp?: number;
			countryByte9c?: number;
			sexSelector1ac?: number;
			merchantBranches?: EntityState["merchantBranches"];
		}>();
	let receivedAt = 0;
	// Simulation time the oldest event of the unpublished queue arrived;
	// take() stamps the span on the batch so the presentation can drop
	// one-shot events that outlived their natural lifetime in the queue.
	let queuedAt = 0, queueStamped = false, journalNow = 0;
	// The monster whose LIFE dead frame came last: the drops that follow it
	// directly are its own (the server's per-victim publication order).
	let dropSource = 0;
	// The /GetPos byte (+CEAD84): the handler flips its own byte and writes
	// it as the local player's body visual state, whatever the server last
	// set there. It outlives any one entity state write by design.
	let debugBodyVisualOn = false;
	let epoch = 0, sequence = 0, inflight: WorldBatch | null = null, bytes = 0;
	// When the batch awaiting its acknowledgement was taken: a compaction
	// report gives the wait, telling a stalled main thread from a burst.
	let inflightAt = 0;
	// A dropped queue awaiting its rebuilt publication (dropQueuedJournal):
	// take() republishes the world from the entity map on a new epoch, as
	// world entry does, instead of the journal growing fatal behind a
	// consumer that stopped acknowledging (a hidden or frozen tab).
	let resyncPending = false, resyncs = 0;
	// Queued native events a compaction kept, oldest first, awaiting the
	// resync publication (dropQueuedJournal); the trim bounds them by
	// MAX_RETAINED_NATIVES / RETAINED_NATIVE_BYTE_LIMIT.
	let retainedNatives: Extract<WorldEvent, { kind: "native"; }>[] = [];
	// The entry publication's protocol word, republished with a resync.
	let bootstrapProtocol = 0;
	let events: WorldEvent[] = [], staged: WorldEvent[] | null = null, remaining = 0, stagedBytes = 0;
	// Cosmetic steps skipped since the world owner last took them (Degradation).
	let degradations: Degradation[] = [];
	// Only locally sampled coordinates are replaceable. Wire state/lifecycle
	// events remain reliable barriers; never mutate an already offered batch.
	const pendingPoses = new Map<number, { index: number; size: number; }>();
	// The queued gameplay snapshot a newer one merges into (supersedeGameplay).
	let pendingGameplay: { index: number; size: number; } | undefined;
	// Values the queued batch has already counted (journal-cost.ts): the batch
	// is one structured clone, so a walking path shared by many state events
	// travels once. Replaceable events (poses, the gameplay snapshot) are
	// costed against their own graph instead, so replacing one never uncounts
	// a shared value.
	let sharedArrays = new WeakSet<object>();
	let stagedMode = 1, removalTail = new Uint8Array( 0 );
	let synchronized = false, localName = "", localActiveJob = 4;
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
	snapshotCost

	One gameplay snapshot is one coalesced slot in the batch: its own
	internal graph is costed with a fresh shared set, and the static
	bootstrap reference tables (skillCatalog, gmCatalog) that ride once on
	that coalesced slot are not charged against the dynamic world-event
	backlog. Without this, the 27,835-row skill catalogue (~27.1 MB in
	UTF-16) plus GM catalogue (~5.8 MB) consumed 33 MB of the 32 MiB
	journal budget on entry and the first cave spawn burst behind it broke
	the limit (F11-DC1).
	================
	*/
	function snapshotCost( event: Extract<WorldEvent, { kind: "gameplay"; }> ) {
		const { skillCatalog: _skills, gmCatalog: _gm, ...dynamic } = event.state;
		return journalCost( { kind: "gameplay", state: dynamic }, new WeakSet<object>() );
	}
	/*
	================
	batchCost

	A reliable event's cost within the queued batch: values an earlier event
	of the batch carries count once (sharedArrays).
	================
	*/
	function batchCost( event: WorldEvent ) {
		return event.kind === "native" ? event.payload.byteLength : journalCost( event, sharedArrays );
	}
	/*
	================
	degrade

	Records a skipped cosmetic step; the queue is bounded so an undrained
	owner cannot grow it (the world reports each code once anyway).
	================
	*/
	function degrade( degradation: Degradation ) {
		if ( degradations.length < MAX_PENDING_DEGRADATIONS ) degradations.push( degradation );
	}
	/*
	================
	dropQueuedJournal

	The bound on the reliable journal: when the queued events behind an
	unacknowledged batch would pass the byte or count budget, the queue is
	dropped instead of throwing. Every entity transition is already in the
	entity map (apply sets it before it appends), so take() can republish
	the world whole on a new epoch, as world entry does, and the session
	survives a consumer that stopped acknowledging - a hidden or frozen
	tab, a stalled frame - where the fatal throw killed it (2026-10-24).
	The census that told a stalled consumer from an unsupported packet
	stream rides the degradation instead of the failure.

	One class is kept across the drop (#1482): native events. A native is
	an opcode no owner here models - the worker forwards it raw precisely
	because nothing decodes it into entity state, a gameplay snapshot or
	the bootstrap - so the rebuilt snapshot cannot supersede it, and
	dropping it loses a one-shot (a notice, a sound cue) the player was
	meant to see or hear. Every modeled kind is state the snapshot
	rebuilds and stays dropped. Kept natives are bounded on their own,
	newest first; what the bound sheds is counted in the census, keyed by
	opcode as before, so an opcode that proves replaceable in the field is
	visible there and gets a decoder (and stops being native) rather than
	a drop rule here.
	================
	*/
	function dropQueuedJournal() {
		const kinds: Record<string, number> = {};
		for ( const queued of events ) {
			const key = queued.kind === "native" ? `native:${queued.opcode.toString( 16 )}` : queued.kind;
			kinds[key] = (kinds[key] ?? 0) + 1;
			if ( queued.kind === "native" ) retainedNatives.push( queued );
		}
		// Trim from the newest back: the oldest one-shots are the ones a
		// returning consumer can most afford to miss.
		const kept: typeof retainedNatives = [];
		let keptBytes = 0, shed = 0;
		for ( let i = retainedNatives.length - 1; i >= 0; i-- ) {
			const event = retainedNatives[i]!, size = event.payload.byteLength;
			if ( kept.length >= MAX_RETAINED_NATIVES || keptBytes + size > RETAINED_NATIVE_BYTE_LIMIT ) {
				shed++;
				continue;
			}
			kept.push( event );
			keptBytes += size;
		}
		kept.reverse();
		retainedNatives = kept;
		degrade( {
			code: "world-journal-resync",
			message: `Reliable world journal backlog compacted: ${bytes + stagedBytes} bytes, ${
				events.length + (staged?.length ?? 0)
			} events dropped; awaiting batch ${inflight?.sequence ?? "none"}${
				inflight ? ` for ${Math.round( performance.now() - inflightAt )} ms` : ""
			}; ${pendingPoses.size} replaceable poses; ${
				JSON.stringify( kinds )
			}; ${retainedNatives.length} one-shot natives retained (${shed} shed past the retention bound)`
		} );
		events = [];
		bytes = 0;
		pendingPoses.clear();
		pendingGameplay = undefined;
		sharedArrays = new WeakSet();
		queueStamped = false;
		synchronized = false;
		resyncPending = true;
		resyncs++;
	}
	/*
	================
	resyncEvents

	The dropped queue rebuilt whole: a reset on a new epoch, the retained
	entry bootstrap, one spawn per live entity and the synchronization
	marker - the entry publication - then the one-shot natives a
	compaction kept (dropQueuedJournal), then whatever queued behind the
	drop that a snapshot does not supersede. Entity transitions stay
	behind: the entity map already holds their net effect. Gameplay,
	native, hp and travel events keep their order after the rebuilt
	world, and the kept natives ahead of them: they are older.
	================
	*/
	function resyncEvents(): WorldEvent[] {
		// The batch is a journal rebuild, not a world entry: its reset and
		// republished spawns carry resync so the presentation updates the
		// characters it already holds in place (#1481).
		const rebuilt: WorldEvent[] = [ { kind: "reset", epoch: ++epoch, resync: true } ];
		if ( local ) {
			rebuilt.push( {
				kind: "bootstrap",
				value: { protocolVersion: bootstrapProtocol, character: { name: localName }, localPlayerEntry: local }
			} );
		}
		for ( const entity of entities.values() ) rebuilt.push( { kind: "spawn", entity, resync: true } );
		rebuilt.push( { kind: "synchronized", epoch } );
		rebuilt.push( ...retainedNatives );
		retainedNatives = [];
		for ( const event of events ) {
			if ( event.kind !== "spawn" && event.kind !== "state" && event.kind !== "despawn" ) {
				rebuilt.push( event );
			}
		}
		return rebuilt;
	}
	/*
	================
	append
	================
	*/
	function append( event: WorldEvent, size = batchCost( event ) ) {
		if ( events.length + (staged?.length ?? 0) >= 8192 || bytes + stagedBytes + size > journalByteLimit ) {
			// A stalled consumer must not kill the session: the queue
			// compacts into the resync the next take publishes.
			dropQueuedJournal();
		}
		if ( !queueStamped ) {
			queuedAt = journalNow;
			queueStamped = true;
		}
		bytes += size;
		events.push( event );
		if ( event.kind === "state" || event.kind === "spawn" ) pendingPoses.delete( event.entity.gid );
		else if ( event.kind === "despawn" ) pendingPoses.delete( event.gid );
		else if ( event.kind === "reset" || event.kind === "native" ) pendingPoses.clear();
		// Presentation drops the gameplay state at a reset; never merge across one.
		if ( event.kind === "reset" ) pendingGameplay = undefined;
	}
	/*
	================
	supersedeGameplay

	Presentation keeps only the last gameplay snapshot of a batch, carrying
	the skill catalogue, social state and shop forward from earlier ones
	(presentation.ts apply). A snapshot published while the previous one is
	still queued therefore merges into it the same way: the batch publishes
	the same state, with one copy to clone instead of one per worker tick of
	a slow presentation frame (#339).
	================
	*/
	function supersedeGameplay( event: Extract<WorldEvent, { kind: "gameplay"; }> ) {
		const queued = pendingGameplay && events[pendingGameplay.index];
		if ( !pendingGameplay || queued?.kind !== "gameplay" ) {
			const size = snapshotCost( event );
			append( event, size );
			pendingGameplay = { index: events.length - 1, size };
			return;
		}
		const state = mergeGameplaySnapshots( queued.state, event.state );
		const merged: WorldEvent = { kind: "gameplay", state }, size = snapshotCost( merged );
		const nextBytes = bytes - pendingGameplay.size + size;
		if ( nextBytes + stagedBytes > journalByteLimit ) {
			// The merged snapshot alone outgrows the queue behind a stalled
			// consumer: compact, then queue this snapshot on its own.
			dropQueuedJournal();
			const own = snapshotCost( event );
			append( event, own );
			pendingGameplay = { index: events.length - 1, size: own };
			return;
		}
		events[pendingGameplay.index] = merged;
		bytes = nextBytes;
		pendingGameplay.size = size;
	}
	/*
	================
	samplePose
	================
	*/
	function samplePose( entity: EntityState ) {
		entity = withBlock( entity );
		const event: WorldEvent = { kind: "state", entity },
			pending = pendingPoses.get( entity.gid ),
			size = cost( event );
		if ( pending === undefined ) {
			append( event, size );
			pendingPoses.set( entity.gid, { index: events.length - 1, size } );
		} else {
			const nextBytes = bytes - pending.size + size;
			if ( nextBytes + stagedBytes > journalByteLimit ) {
				// The pose replaces a queued one, but the queue behind a
				// stalled consumer is already past its budget: compact,
				// then queue this pose on its own.
				dropQueuedJournal();
				append( event, size );
				pendingPoses.set( entity.gid, { index: events.length - 1, size } );
			} else {
				events[pending.index] = event;
				bytes = nextBytes;
				pending.size = size;
			}
		}
		entities.set( entity.gid, entity );
	}
	/*
	================
	withBlock

	Port-only, not native: the entity with the dungeon room its navmesh owner
	stands in (navigation.block, the same lookup that gives the local player's
	camera block). The entity itself when the room is unchanged, so an outdoor
	or settled mover costs no new journal state. Every journal path that can
	move an entity funnels through here - spawn and state events in apply,
	and every motion pose in samplePose - so a pose a wire event rewrites
	(a snap, a displacement, a cancelled cast) never trails its room.
	================
	*/
	function withBlock( entity: EntityState ): EntityState {
		if ( !block || !featuresAt( entity.regionId ).rooms ) return entity;
		const pose = { regionId: entity.regionId, x: entity.x, y: entity.y, z: entity.z, angle: entity.heading };
		const dungeonBlock = block( pose, motion.owner( entity.gid ) );
		return dungeonBlock === entity.dungeonBlock ? entity : Object.freeze( { ...entity, dungeonBlock } );
	}
	/*
	================
	withSunMarker

	Port-only (#1640): stamp the drops the Experimental marker admits -
	the local player's own degree-9+ Seal of Sun, while the row is on.
	Recomputed at every apply, so an ownership expiry (0x31e2) unmarks.
	================
	*/
	function withSunMarker( entity: EntityState ): EntityState {
		if ( !entity.groundItem ) return entity;
		const marked = sunMarkerOn && ownGid !== 0 && entity.groundItem.ownerJid === ownGid &&
			sunSealDrop( itemCodenames.get( entity.refObjId ) ?? "" );
		const sunMarker = marked ? true : undefined;
		return sunMarker === entity.groundItem.sunMarker ?
			entity :
			Object.freeze( { ...entity, groundItem: { ...entity.groundItem, sunMarker } } );
	}
	/*
	================
	restampSunMarkers

	The row flipped or the local player arrived: re-admit every drop on
	the ground through apply, which recomputes the stamp.
	================
	*/
	function restampSunMarkers() {
		for ( const entity of entities.values() ) {
			if ( entity.groundItem && withSunMarker( entity ) !== entity ) {
				apply( { kind: "state", entity } );
			}
		}
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
		return withBlock( y === entity.y ? entity : Object.freeze( { ...entity, y } ) );
	}
	/*
	================
	retire

	Removes the entity a duplicate spawn replaces. Presentation sees the
	despawn so the old actor fades; gameplay does not, because native only
	overwrites the gid's slot: a selected target, a queued skill, an open NPC
	dialog and the HP record all survive, and riders keep mountedOn.
	================
	*/
	function retire( gid: number ) {
		entities.delete( gid );
		motion.remove( gid );
		append( { kind: "despawn", gid } );
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
			// groundedSpawn stamps only the kinds it grounds; stamp the rest
			// here so every dungeon spawn carries its room.
			event = { ...event, entity: withSunMarker( withBlock( event.entity ) ) };
			if ( event.entity.kind === "local-player" && ownGid !== event.entity.gid ) {
				ownGid = event.entity.gid;
				restampSunMarkers();
			}
			// CIGIDObject_RegisterGid (852CA0) only debug-asserts a gid it
			// already holds, then g_sHashGID keeps the newer object: a second
			// spawn replaces the first and never ends the session. The old
			// entity is retired so its visual and motion do not linger; riders
			// keep mountedOn because the same gid returns at once.
			if ( entities.has( event.entity.gid ) ) retire( event.entity.gid );
			if ( entities.size >= 16384 ) {
				throw new Error( "Entity limit exceeded" );
			}
			entities.set( event.entity.gid, event.entity );
			motion.spawn( event.entity, receivedAt );
		} else if ( event.kind === "state" ) {
			event = { ...event, entity: withSunMarker( withBlock( event.entity ) ) };
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
			const appearance = decodePeerAppearance( p, itemRefs, frame.opcode === 0x30d7, refs, skillRefs );
			// 86AFB0 classifies the spawn's slot-8 suit (868D00); a later 0x3314
			// leaves the class until the next spawn row, as natively.
			return {
				kind: "spawn",
				entity: { ...ref, ...appearance, activeJob: partyActiveJob( appearance.equipment ?? [] ) }
			};
		}
		if ( !ref || ![ "npc", "monster", "cos", "structure" ].includes( ref.kind ) ) {
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
	/*
	================
	refreshHoverAttack

	Republishes each player's and pet's hover attack verdict (6875F0) when
	it changes; verdict judges one entity.
	================
	*/
	function refreshHoverAttack( verdict: ( e: EntityState ) => number ) {
		for ( const e of entities.values() ) {
			if ( e.kind !== "player" && e.kind !== "cos" ) continue;
			const hoverAttack = verdict( e );
			if ( hoverAttack !== (e.hoverAttack ?? 0) ) apply( { kind: "state", entity: { ...e, hoverAttack } } );
		}
	}
	return {
		recolor,
		refreshHoverAttack,
		/*
		================
		characterCountry

		The country byte (+0x9C) of a character reference: 0 China, 1 Europe.
		Party windows resolve a member's race mark from it whether or not the
		member is in view (5B93A0 reads GetCharCosDataById(member ref)+0x9C).
		================
		*/
		/*
		================
		monsterReference

		The monster row a GM typed by codename, with its static type byte
		(client record +0xA0), or undefined for anything else.
		================
		*/
		monsterReference( codename: string ): { readonly refObjId: number; readonly monsterType: number; } | undefined {
			return monsterCodenames.get( codename );
		},
		characterCountry( refObjId: number ): number | undefined {
			return refs.get( refObjId )?.countryByte9c;
		},
		/*
		================
		playerModels

		The player models of one country in reference order, each with its
		sex selector (+0x1AC): the lists CIFChangePlayerModel_BuildModelLists
		(6D14C0) reads from the global data manager.
		================
		*/
		playerModels( country: number ): readonly { readonly refObjId: number; readonly sex: number; }[] {
			const models: { refObjId: number; sex: number; }[] = [];
			for ( const [refObjId, row] of refs ) {
				if ( row.kind === "player" && row.countryByte9c === country && row.sexSelector1ac !== undefined ) {
					models.push( { refObjId, sex: row.sexSelector1ac } );
				}
			}
			return models.sort( ( a, b ) => a.refObjId - b.refObjId );
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
			// A fresh entry publication supersedes a pending resync.
			resyncPending = false;
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
			queueStamped = false;
			pendingGameplay = undefined;
			sharedArrays = new WeakSet();
			inflight = null;
			bytes = 0;
			resyncPending = false;
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
			monsterCodenames.clear();
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
					], [ "sexSelector1ac", 255 ] ] as const
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
					row.monsterType !== undefined &&
					(!Number.isInteger( row.monsterType ) || row.monsterType < 0 || row.monsterType > 255)
				) throw Error( "Invalid entity reference monsterType" );
				if ( row.kind === "monster" && typeof row.codename === "string" && row.codename ) {
					monsterCodenames.set( row.codename, { refObjId: row.refObjId, monsterType: row.monsterType ?? 0 } );
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
					sexSelector1ac: row.sexSelector1ac,
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
				if ( row.codename ) itemCodenames.set( row.refObjId, row.codename );
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
			// CICPlayer_OnSpawnInitialize (869160) classifies the suit worn at
			// entry; it holds until the next entry, like the alias board below.
			const suit = ((b as { equipItems?: { slot: number; refObjId: number; }[]; }).equipItems ?? []).find(
				row => row.slot === JOB_SUIT_SLOT
			);
			localActiveJob = partyActiveJob(
				suit ? [ { slot: JOB_SUIT_SLOT, typeFlags: itemRefs.get( suit.refObjId ) ?? 0 } ] : []
			);
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
			// A re-entry publication supersedes a pending resync, and the
			// resync republishes this bootstrap's protocol word.
			resyncPending = false;
			bootstrapProtocol = Number( b.protocolVersion );
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
			journalNow = now;
			// Port-only position feedback is journaled independently of the retiring clone.
			if ( frame.opcode === 0x3fba ) {
				const p = frame.payload;
				if ( p.length !== 18 ) throw Error( "Invalid brood wind length" );
				const v = new DataView( p.buffer, p.byteOffset, p.byteLength );
				const gid = v.getUint32( 0, true ), regionId = v.getUint16( 4, true );
				const x = v.getFloat32( 6, true ), y = v.getFloat32( 10, true ), z = v.getFloat32( 14, true );
				if ( gid < 0x08000000 || gid >= 0x0a800000 || !regionId || ![ x, y, z ].every( Number.isFinite ) ) {
					throw Error( "Invalid brood wind position" );
				}
				append( { kind: "pet-crow-effect", source: { regionId, x, y, z } } );
				return;
			}
			if ( frame.opcode === 0x3fbb ) {
				const p = frame.payload;
				if ( p.length !== 20 ) throw Error( "Invalid crow effect length" );
				const v = new DataView( p.buffer, p.byteOffset, p.byteLength );
				const gid = v.getUint32( 0, true ), cue = p[4]!, book = p[5]!, regionId = v.getUint16( 6, true );
				const x = v.getFloat32( 8, true ), y = v.getFloat32( 12, true ), z = v.getFloat32( 16, true );
				if (
					!gid || cue < 1 || cue > 8 || book < 1 || book > 3 || !regionId ||
					![ x, y, z ].every( Number.isFinite )
				) {
					throw Error( "Invalid crow effect position or kind" );
				}
				append( { kind: "pet-crow-effect", cue, book, source: { regionId, x, y, z } } );
				return;
			}
			// Port-only Resolve state; exact, bounded packet independent of native animations.
			if ( frame.opcode === 0x3fb5 ) {
				const p = frame.payload;
				if ( p.length !== 10 ) throw Error( "Invalid pet Resolve length" );
				const v = new DataView( p.buffer, p.byteOffset, p.byteLength );
				const gid = v.getUint32( 0, true ),
					stacks = v.getUint8( 4 ),
					tier = v.getUint8( 5 ),
					duration = v.getUint32( 6, true );
				if ( !gid || stacks > 10 || tier > 3 || (stacks > 0 && !tier) || duration > 15000 ) {
					throw Error( "Invalid pet Resolve state" );
				}
				const entity = entities.get( gid );
				if ( entity ) {
					samplePose( { ...entity, petResolve: { stacks, tier, atMs: now, untilMs: now + duration } } );
				}
				return;
			}
			// Port-only Guardian's Bond cue; independent of emotes and native casts.
			if ( frame.opcode === 0x3fb4 ) {
				const p = frame.payload;
				if ( p.length !== 9 ) throw Error( "Invalid pet bond pulse length" );
				const v = new DataView( p.buffer, p.byteOffset, p.byteLength );
				const gid = v.getUint32( 0, true ), bear = v.getUint32( 4, true ), kind = v.getUint8( 8 );
				if ( !gid || !bear || (kind !== 1 && kind !== 2) ) throw Error( "Invalid pet bond pulse" );
				const entity = entities.get( gid );
				if ( entity ) {
					samplePose( {
						...entity,
						petBondPulse: {
							revision: (entity.petBondPulse?.revision ?? 0) + 1,
							atMs: now,
							bear,
							kind
						}
					} );
				}
				return;
			}
			// Port-only retaliation cue; separate from emotes so it cannot interrupt a cast.
			if ( frame.opcode === 0x3fb3 ) {
				const p = frame.payload;
				if ( p.length !== 4 ) throw Error( "Invalid pet counter pulse length" );
				const gid = new DataView( p.buffer, p.byteOffset, p.byteLength ).getUint32( 0, true );
				if ( !gid ) throw Error( "Invalid pet counter pulse recipient" );
				const entity = entities.get( gid );
				if ( entity ) {
					samplePose( {
						...entity,
						petCounterPulse: { revision: (entity.petCounterPulse?.revision ?? 0) + 1, atMs: now }
					} );
				}
				return;
			}
			// Port-only absorption is separate from native health and max-health packets.
			if ( frame.opcode === 0x3fb2 ) {
				const p = frame.payload;
				if ( p.length !== 21 ) throw Error( "Invalid shield state length" );
				const v = new DataView( p.buffer, p.byteOffset, p.byteLength );
				const gid = v.getUint32( 0, true ),
					amount = v.getUint32( 4, true ),
					maxHp = v.getUint32( 8, true ),
					duration = v.getUint32( 12, true );
				if (
					!gid || !maxHp || amount > Math.floor( maxHp * 1.5 ) || duration > 5000 || p[20]! > 1 ||
					(p[20] && (amount || duration))
				) throw Error( "Invalid shield state" );
				const entity = entities.get( gid );
				if ( entity ) {
					samplePose( {
						...entity,
						shield: {
							amount,
							maxHp,
							atMs: now,
							untilMs: now + duration,
							revision: (entity.shield?.revision ?? 0) + 1,
							burst: p[20] === 1
						}
					} );
				}
				return;
			}
			receivedAt = now;
			// Any frame but the dead monster's own drops ends its publication.
			const followsDeath = dropSource;
			dropSource = 0;
			const p = frame.payload, v = new DataView( p.buffer, p.byteOffset, p.byteLength );
			if ( frame.opcode === 0x3449 ) {
				if ( p.length !== 8 ) throw Error( "Invalid external item effect" );
				const source = entities.get( v.getUint32( 0, true ) ), item = v.getUint32( 4, true );
				if ( item === 0 || !source || source.groundItem ) return;
				const typeFlags = itemRefs.get( item );
				if ( typeFlags === undefined ) {
					// The effect is cosmetic and the payload a fixed 8 bytes: skip the
					// sparkle, keep the session, and report it (native 74F540 would
					// crash on the unresolved record; the port degrades instead).
					degrade( {
						code: "item-use-visual-unknown-ref",
						message: "Missing external item reference " + item,
						frame
					} );
					return;
				}
				append( { kind: "item-effect", source: { ...source }, item, typeFlags } );
				return;
			}
			const structureState = fortressStructureState( frame );
			if ( structureState ) {
				// 76C870 case 0xB -> CICATStruct_OnFortressWarState (4F7BF0).
				const entity = entities.get( structureState.gid );
				if ( entity?.kind === "structure" ) {
					apply( { kind: "state", entity: { ...entity, structureState: structureState.state } } );
				}
				return;
			}
			if ( frame.opcode === 0x30df || frame.opcode === 0x34b7 || frame.opcode === 0x33d1 ) {
				// 751430 opens a stall (title mode 4, its title and decoration),
				// 751520 renames it, 74F8F0 takes it down (CICharactor_SetStallState).
				const entity = entities.get( v.getUint32( 0, true ) );
				if ( !entity ) return;
				// The local latch has no peer appearance row (localAppearanceRow),
				// so the owner's booth reaches presentation too.
				const appearanceState = [ ...(entity.appearanceState ?? localAppearanceRow( entity.pvpState ?? 0 )) ];
				if ( frame.opcode === 0x33d1 ) {
					appearanceState[STALL_TITLE_SLOT] = 0;
					apply( {
						kind: "state",
						entity: {
							...entity,
							titleText: undefined,
							titleId: undefined,
							appearanceState
						}
					} );
					return;
				}
				const n = v.getUint16( 4, true );
				if ( 6 + n * 2 > p.length ) throw Error( "Invalid stall title" );
				const titleText = new TextDecoder( "utf-16le" ).decode( p.subarray( 6, 6 + n * 2 ) );
				const titleId = frame.opcode === 0x30df ? v.getUint32( 6 + n * 2, true ) : entity.titleId;
				appearanceState[STALL_TITLE_SLOT] = STALL_TITLE_MODE;
				apply( {
					kind: "state",
					entity: { ...entity, titleText, titleId, appearanceState }
				} );
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
			// 77A570 case 5: a pet's new name, on its nameplate for every viewer.
			const named = petNameUpdate( frame );
			if ( named ) {
				const entity = entities.get( named.gid );
				if ( entity ) apply( { kind: "state", entity: { ...entity, name: named.name } } );
				return;
			}
			if ( frame.opcode === 0x3508 && p[4] === 7 ) {
				// 77A570 case 7 calls the entity's reference setter: the pet's
				// next form replaces its model for every viewer.
				if ( p.length !== 9 ) throw Error( "Invalid COS reference change" );
				const entity = entities.get( v.getUint32( 0, true ) ), refObjId = v.getUint32( 5, true );
				const tid = refs.get( refObjId )?.tidWord;
				if ( tid === undefined ) throw Error( "Unknown COS reference " + refObjId );
				if ( entity ) apply( { kind: "state", entity: { ...entity, refObjId, tidWord: tid } } );
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
			if ( frame.opcode === 0x3434 || frame.opcode === 0x3514 ) {
				// 75D130 starts a kind-2 action bar ([u32 gid][2][1|2][u8 seconds]);
				// 75D1E0 clears it ([u32 gid], the strip cancel).
				const entity = entities.get( v.getUint32( 0, true ) );
				if ( frame.opcode === 0x3434 ) {
					if ( p.length !== 7 || p[4] !== 2 ) return;
					if ( entity ) {
						apply( {
							kind: "state",
							entity: { ...entity, actionProgress: { seconds: p[6]!, startedAtMs: now } }
						} );
					}
					return;
				}
				if ( p.length !== 4 ) throw Error( "Invalid strip cancel" );
				if ( entity?.actionProgress ) {
					apply( { kind: "state", entity: { ...entity, actionProgress: undefined } } );
				}
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
				// 777F60 stores the mount gid at +0x298 without resolving it: a ride
				// may precede its vehicle's spawn, and 85E000/85D870 fall back to
				// the rider until the vehicle arrives.
				if ( !rider || p[5] === 1 && (mount === 0 || mount === rider.gid) ) {
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
						animationRate: animationRate( finite( local.actionSpeed ?? 100 ) ),
						spawnSkills: localSkills,
						avatars: localAvatars,
						arenaTeam: Number( local.arenaTeam ?? 255 ),
						pvpState: Number( local.pvpState ?? 0 ),
						appearanceState: Object.freeze( localAppearanceRow( Number( local.pvpState ?? 0 ) ) ),
						...(local.visualFlags !== undefined ? { visualFlags: Number( local.visualFlags ) } : {}),
						...(local.bodyShape !== undefined ? { bodyShape: Number( local.bodyShape ) } : {}),
						localJob: {
							type: Number( local.jobType ?? 0 ),
							grade: Number( local.jobGrade ?? 0 ),
							exp: Number( local.jobExp ?? 0 ),
							alias: String( local.jobAlias ?? "" ),
							contribution: Number( local.jobContribution ?? 0 )
						},
						regionId: finite( pose.regionId ),
						x: finite( pose.x ),
						y: finite( pose.y ),
						z: finite( pose.z ),
						heading: finite( pose.angle ),
						name: localName,
						activeJob: localActiveJob,
						// 8675F0: a player entering in job mode names its own board
						// with the job alias (vtable +0x74 with +0x1898).
						...(localActiveJob !== 4 && local.jobAlias ? { boardName: String( local.jobAlias ) } : {})
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
				let event = spawn( frame );
				if ( event.kind === "spawn" && event.entity.groundItem && followsDeath ) {
					event = {
						...event,
						entity: {
							...event.entity,
							groundItem: { ...event.entity.groundItem, dropperGid: followsDeath }
						}
					};
					dropSource = followsDeath;
				}
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
						// CICCos_DeserializeSpawnSubState (854CD0): a fresh combat or
						// fellowship pet also plays SYSTEM_PET_APPEAR (0x80000021).
						append( { kind: "system-effect", gid: event.entity.gid, effect: SYSTEM_PET_APPEAR } );
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
					if ( p[5] === 2 && entity.kind === "monster" ) dropSource = entity.gid;
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
					// 777B60 applies the gait to CCharactor_GetActiveMoverEntity: a
					// rider's walk/run switches its vehicle, which carries the path.
					const vehicle = p[5] === 2 || p[5] === 3 ?
						next.mountedOn === undefined ? undefined : entities.get( next.mountedOn ) :
						undefined;
					if ( vehicle ) {
						const ridden = { ...vehicle, movementMode: p[5]! };
						apply( {
							kind: "state",
							entity: Object.freeze( { ...ridden, ...motion.mode( ridden, now ) } )
						} );
					}
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
			if ( frame.opcode === 0x3453 ) {
				const update = decodeAnimationSpeed( p ), source = entities.get( update.gid );
				// 775EB0 resolves the riding actor before setting +4DC. Existing
				// installations keep their captured rate (8DF527, ADF030).
				const entity = source?.mountedOn ? entities.get( source.mountedOn ) ?? source : source;
				if ( entity ) {
					apply( { kind: "state", entity: Object.freeze( { ...entity, animationRate: update.rate } ) } );
				}
				return;
			}
			if ( frame.opcode === 0x376f ) {
				const channels = decodeMovementSpeeds( p ), source = entities.get( channels.gid );
				// CPSMission_OnEntitySpeedUpdate0x376F (0x775E40) consumes server speeds;
				// CCharactor_GetActiveMoverEntity (0x85E000) resolves the riding actor.
				const entity = source?.mountedOn ? entities.get( source.mountedOn ) ?? source : source;
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
				const entity = source.mountedOn ? entities.get( source.mountedOn ) ?? source : source;
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
				if ( frame.opcode === 0x30e3 && entity?.appearanceState?.[0] === 2 ) return;
				// 775B50 (0x775C1A): once the mover is resolved (85E000), a
				// monster or COS mover whose life state (+0x263) is 2 (dead)
				// ignores its B2F5 - no halt, no reseed. Players are not
				// checked: a dead player's B2F5 remains admissible, as
				// present rebirth corrects before LIFE-alive.
				if (
					frame.opcode === 0xb2f5 && entity?.appearanceState?.[0] === 2 &&
					(entity.kind === "monster" || entity.kind === "cos")
				) return;
				if ( entity ) {
					const corrected = position( v, frame.opcode === 0x30e3 ? 0 : 4 );
					// The local mover has one navigation owner in gameplay. Its
					// entity row does not follow B738 or prediction; projecting a
					// correction from that stale row can replace the arena floor
					// with terrain before core passes it to the actual mover.
					// Native 775B50/775CB0 pass wire XYZ to that mover's controller.
					const localMover = source?.kind === "local-player" || entity.kind === "local-player";
					if ( localMover && frame.opcode === 0xb2f5 ) motion.remove( entity.gid );
					apply( {
						kind: "state",
						entity: Object.freeze( {
							...entity,
							movementRevision: (entity.movementRevision ?? 0) + 1,
							...(localMover ?
								{
									...corrected,
									...(frame.opcode === 0xb2f5 ? { moving: false, movementPath: undefined } : {})
								} :
								frame.opcode === 0x30e3 ?
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
		takeDegradations

		The cosmetic steps skipped since the last take, for the world owner's
		non-fatal reports.
		================
		*/
		takeDegradations(): Degradation[] {
			const taken = degradations;
			degradations = [];
			return taken;
		},
		/*
		================
		take
		================
		*/
		take(): WorldBatch | null {
			if ( inflight || (!events.length && !resyncPending) ) {
				return null;
			}
			if ( resyncPending ) {
				events = resyncEvents();
				resyncPending = false;
			}
			inflight = {
				sequence: ++sequence,
				events,
				spanMs: queueStamped ? Math.max( 0, journalNow - queuedAt ) : 0
			};
			queueStamped = false;
			inflightAt = performance.now();
			events = [];
			pendingPoses.clear();
			pendingGameplay = undefined;
			sharedArrays = new WeakSet();
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
			journalNow = now;
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
		renameStall

		An accepted owner title edit changes only the existing stall's text.
		The open broadcast remains the authority for title mode and decoration.
		================
		*/
		renameStall( gid: number, titleText: string ) {
			const entity = entities.get( gid );
			if ( !entity || entity.appearanceState?.[STALL_TITLE_SLOT] !== STALL_TITLE_MODE ) return;
			apply( { kind: "state", entity: { ...entity, titleText } } );
		},
		/*
		================
		debugBodyVisual

		/GetPos (690C40 case 3): flip the +CEAD84 byte and write it as the
		entity's body visual state (85EC00), the channel-4 state byte at
		appearanceState[2]. The flipped byte is the handler's own, not the
		entity's standing state, so a server-set berserk visual survives
		the first flip and falls to the second. A repeated write changes
		nothing, as 85EC00 returns before side effects for repeated state
		(the channel-4 handler above keeps the same guard).
		================
		*/
		debugBodyVisual( gid: number ) {
			debugBodyVisualOn = !debugBodyVisualOn;
			const entity = entities.get( gid );
			if ( !entity ) return;
			const state = debugBodyVisualOn ? 1 : 0;
			if ( (entity.appearanceState?.[2] ?? 0) === state ) return;
			const appearanceState = [ ...(entity.appearanceState ?? [ 1, 0, 0 ]) ];
			appearanceState[2] = state;
			apply( {
				kind: "state",
				entity: Object.freeze( { ...entity, appearanceState: Object.freeze( appearanceState ) } )
			} );
		},
		/*
		================
		debugLocalVisible

		/showme and /hideme (690C40 cases 112/113): write the shown byte
		(CICPlayer +0xB8) on the local player's record. The byte lives on
		the record, not in a handler-side flag like debugBodyVisualOn:
		the native byte is the character's own, read by its animation
		advance (853600) and its peace-state predicate, and the record is
		its only carrier here. A repeated write changes nothing.
		================
		*/
		debugLocalVisible( gid: number, visible: boolean ) {
			const entity = entities.get( gid );
			if ( !entity ) return;
			if ( (entity.debugHidden ?? false) === !visible ) return;
			apply( {
				kind: "state",
				entity: Object.freeze( { ...entity, debugHidden: !visible } )
			} );
		},
		/*
		================
		rider

		The entity riding the horse `gid` (CICharactor_GetMountedHorseOrVehicle).
		================
		*/
		rider( gid: number ) {
			for ( const entity of entities.values() ) if ( entity.mountedOn === gid ) return entity;
			return undefined;
		},
		/*
		================
		playerNamed

		The visible player of this name (EntityManager_FindGidByName, as the
		chat commands read it).
		================
		*/
		playerNamed( name: string ) {
			for ( const entity of entities.values() ) {
				if ( entity.kind === "player" && entity.name === name ) return entity;
			}
			return undefined;
		},
		/*
		================
		groundItems

		The ground items in the table now, every despawn already applied.
		================
		*/
		groundItems(): EntityState[] {
			return [ ...entities.values() ].filter( entity => entity.kind === "ground-item" );
		},
		/*
		================
		monsters

		The monsters in the table now (861580's CICMonster list).
		================
		*/
		monsters(): EntityState[] {
			return [ ...entities.values() ].filter( entity => entity.kind === "monster" );
		},
		/*
		================
		npcs

		The NPCs and guards in the table now (the non-user list StartGroundMove
		walks, 878600, narrowed to its CICNPC kinds by npc-click-refusal.ts).
		================
		*/
		npcs(): EntityState[] {
			return [ ...entities.values() ].filter( entity => entity.kind === "npc" );
		},
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
		markCaptured

		7786E0 attaches the capture mark only to a character (CICharactor).
		================
		*/
		markCaptured( gid: number ) {
			const entity = entities.get( gid );
			if ( !entity || !CHARACTER_KINDS.has( entity.kind ) ) return;
			apply( { kind: "state", entity: { ...entity, captureMark: true } } );
		},
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
			if ( event.kind === "gameplay" ) supersedeGameplay( event );
			else append( event );
		},
		// How many queues compaction dropped into a resync (dropQueuedJournal):
		// the world core republishes the gameplay catalogue when this moves.
		resyncs: () => resyncs,
		// Port-only (#1640): the Experimental Sun drop marker row, applied
		// to the drops already on the ground and to every later one.
		sunDropMarker( enabled: boolean ) {
			if ( sunMarkerOn === enabled ) return;
			sunMarkerOn = enabled;
			restampSunMarkers();
		},
		count: () => entities.size
	};
}
